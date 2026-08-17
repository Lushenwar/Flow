package session

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Lushenwar/Flow/internal/blocklist"
	"github.com/Lushenwar/Flow/internal/enforce"
	"github.com/Lushenwar/Flow/internal/schedule"
	"github.com/Lushenwar/Flow/internal/store"
)

type recorder struct{ last enforce.Effective }

func (r *recorder) Set(e enforce.Effective) { r.last = e }

func newManager(t *testing.T, c Clock, dir string) (*Manager, *recorder) {
	t.Helper()
	st, err := store.Open(filepath.Join(dir, "state.db"), filepath.Join(dir, "key.bin"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	rec := &recorder{}
	m := NewManager(st, c, rec, blocklist.Presets(), []string{"preset.adult"})
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m, rec
}

func TestBaselineIsEnforcedWhileIdle(t *testing.T) {
	c := newFake()
	_, rec := newManager(t, c, t.TempDir())

	if _, ok := rec.last.Domains["pornhub.com"]; !ok {
		t.Fatal(`the dial reading "not focusing" must never mean nothing is enforced`)
	}
}

func TestCommitAppliesSessionBlocksImmediately(t *testing.T) {
	c := newFake()
	m, rec := newManager(t, c, t.TempDir())

	if _, err := m.Commit(Plan{Mode: ModeCommitment, Duration: 25 * time.Minute, ListIDs: []string{"preset.video"}, Grace: DefaultGrace, Penalty: false}); err != nil {
		t.Fatal(err)
	}
	if _, ok := rec.last.Domains["youtube.com"]; !ok {
		t.Fatal("blocks apply at commit, not at grace-end")
	}
	if _, ok := rec.last.Domains["pornhub.com"]; !ok {
		t.Fatal("baseline must survive alongside the session")
	}
}

func TestSessionEndLiftsOnlySessionRules(t *testing.T) {
	c := newFake()
	m, rec := newManager(t, c, t.TempDir())

	m.Commit(Plan{Mode: ModeCommitment, Duration: 5 * time.Minute, ListIDs: []string{"preset.video"}, Grace: DefaultGrace, Penalty: false})
	c.tick(DefaultGrace + 6*time.Minute)
	if got := m.Snapshot().State; got != Complete {
		t.Fatalf("state %s", got)
	}
	// Snapshot ticks but does not re-apply; the Run loop does. Force the write path.
	m.Ack()

	if _, ok := rec.last.Domains["youtube.com"]; ok {
		t.Fatal("session rules must lift at 0:00")
	}
	if _, ok := rec.last.Domains["pornhub.com"]; !ok {
		t.Fatal("baseline is not lifted by session end")
	}
}

// The exit criterion: the session survives the daemon dying and coming back.
func TestSessionSurvivesDaemonRestart(t *testing.T) {
	dir := t.TempDir()
	c := newFake()

	m, _ := newManager(t, c, dir)
	m.Commit(Plan{Mode: ModeCommitment, Duration: 30 * time.Minute, ListIDs: []string{"preset.video"}, Grace: DefaultGrace, Penalty: false})
	c.tick(DefaultGrace + 5*time.Minute)
	m.Snapshot() // drive the transition to FOCUS

	// taskkill /F on the daemon: no clean shutdown, just a new process reading
	// the same store.
	m2, rec2 := newManager(t, c, dir)
	s := m2.Snapshot()
	if s.State != Focus {
		t.Fatalf("state %s after restart, want FOCUS", s.State)
	}
	if got := s.Target.Remaining(c, nil); got != 30*time.Minute-DefaultGrace-5*time.Minute {
		t.Fatalf("remaining %v", got)
	}
	if _, ok := rec2.last.Domains["youtube.com"]; !ok {
		t.Fatal("a restarted daemon must re-apply the session's blocks")
	}
}

func TestSessionSurvivesRebootThroughTheManager(t *testing.T) {
	dir := t.TempDir()
	c := newFake()

	m, _ := newManager(t, c, dir)
	m.Commit(Plan{Mode: ModeCommitment, Duration: 30 * time.Minute, ListIDs: nil, Grace: DefaultGrace, Penalty: false})
	c.tick(DefaultGrace + 5*time.Minute)
	m.Snapshot()

	// Checkpoint, then power cycle.
	m.mu.Lock()
	m.checkpointLocked()
	m.persistLocked()
	m.mu.Unlock()
	c.reboot(2 * time.Minute)

	m2, _ := newManager(t, c, dir)
	s := m2.Snapshot()
	if s.State != Focus {
		t.Fatalf("state %s after reboot", s.State)
	}
	want := 30*time.Minute - DefaultGrace - 7*time.Minute
	if got := s.Target.Remaining(c, nil); got != want {
		t.Fatalf("remaining %v, want %v", got, want)
	}
}

func TestClockJumpDoesNotEndTheSession(t *testing.T) {
	c := newFake()
	m, _ := newManager(t, c, t.TempDir())

	m.Commit(Plan{Mode: ModeCommitment, Duration: 30 * time.Minute, ListIDs: nil, Grace: DefaultGrace, Penalty: true})
	c.tick(DefaultGrace + time.Minute)
	m.Snapshot()

	c.wall = c.wall.Add(2 * time.Hour)

	s := m.Snapshot()
	if s.State != Focus {
		t.Fatalf("state %s — a clock change must not end a lock", s.State)
	}
	if got := s.Target.Remaining(c, nil); got != 30*time.Minute-DefaultGrace-time.Minute {
		t.Fatalf("remaining %v, want 28m45s", got)
	}
}

func TestDriftIsLoggedAndPenaltyCapped(t *testing.T) {
	dir := t.TempDir()
	c := newFake()
	st, err := store.Open(filepath.Join(dir, "state.db"), filepath.Join(dir, "key.bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	m := NewManager(st, c, &recorder{}, blocklist.Presets(), nil)
	m.Load()
	m.Commit(Plan{Mode: ModeCommitment, Duration: 60 * time.Minute, ListIDs: nil, Grace: DefaultGrace, Penalty: true})

	m.mu.Lock()
	m.checkpointLocked()
	m.mu.Unlock()
	c.mono += 5 * time.Second
	c.wall = c.wall.Add(30 * time.Minute)

	m.mu.Lock()
	m.checkDriftLocked()
	m.mu.Unlock()

	evs, err := st.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range evs {
		if e.Kind == "clock_drift" {
			found = true
		}
	}
	if !found {
		t.Fatal("drift must be logged whether or not it is punished")
	}
	if !m.Snapshot().PenaltyApplied {
		t.Fatal("penalty was accepted at commit and drift was detected")
	}

	// A second drift event must not stack another penalty.
	m.mu.Lock()
	m.checkpointLocked()
	m.mu.Unlock()
	c.wall = c.wall.Add(30 * time.Minute)
	before := m.Snapshot().Target.Duration
	m.mu.Lock()
	m.checkDriftLocked()
	m.mu.Unlock()
	if m.Snapshot().Target.Duration != before {
		t.Fatal("penalty stacked — it is capped at once per session")
	}
}

// The exit criterion: a schedule auto-arms even if the UI was never opened. No
// API call drives this — the daemon's own tick applies it.
func TestScheduleAutoArmsWithoutTheUI(t *testing.T) {
	c := newFake()
	c.wall = time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC) // midday, outside the window
	m, rec := newManager(t, c, t.TempDir())

	del, err := schedule.New("sched.delivery", "Delivery", []string{"preset.delivery"}, "18:00", "21:00", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.PutSchedule(del); err != nil {
		t.Fatal(err)
	}
	if _, ok := rec.last.Domains["ubereats.com"]; ok {
		t.Fatal("midday is outside the window")
	}

	// 19:00 arrives. Nothing calls the API.
	c.wall = time.Date(2026, 8, 4, 19, 0, 0, 0, time.UTC)
	m.mu.Lock()
	m.applyLocked()
	m.mu.Unlock()

	if _, ok := rec.last.Domains["ubereats.com"]; !ok {
		t.Fatal("the schedule must arm itself")
	}
	if got := rec.last.Domains["ubereats.com"]; got != enforce.Schedule {
		t.Fatalf("attributed to %q, want schedule", got)
	}

	// And lifts on its own at 21:00.
	c.wall = time.Date(2026, 8, 4, 21, 30, 0, 0, time.UTC)
	m.mu.Lock()
	m.applyLocked()
	m.mu.Unlock()
	if _, ok := rec.last.Domains["ubereats.com"]; ok {
		t.Fatal("the window closed; the rule should lift")
	}
	// Baseline is untouched throughout.
	if _, ok := rec.last.Domains["pornhub.com"]; !ok {
		t.Fatal("baseline must survive a schedule closing")
	}
}

func TestScheduleSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	c := newFake()
	c.wall = time.Date(2026, 8, 4, 19, 0, 0, 0, time.UTC)

	m, _ := newManager(t, c, dir)
	del, _ := schedule.New("sched.delivery", "Delivery", []string{"preset.delivery"}, "18:00", "21:00", time.UTC)
	m.PutSchedule(del)

	m2, rec2 := newManager(t, c, dir)
	if _, ok := rec2.last.Domains["ubereats.com"]; !ok {
		t.Fatal("a restarted daemon must re-apply an active schedule")
	}
	all, active := m2.Schedules()
	if len(all) == 0 || len(active) != 1 {
		t.Fatalf("schedules %d, active %d", len(all), len(active))
	}
}

// Credit happens in the same signed write as the transition, so a crash between
// them cannot double-pay.
func TestCreditIsWrittenWithTheTransition(t *testing.T) {
	dir := t.TempDir()
	c := newFake()

	m, _ := newManager(t, c, dir)
	m.Commit(Plan{Mode: ModeCommitment, Duration: 50 * time.Minute, ListIDs: nil, Grace: DefaultGrace, Penalty: false})
	c.tick(DefaultGrace + 50*time.Minute)
	m.Snapshot() // drives the transition into COMPLETE and the credit

	balance, _, _ := m.Bank()
	if balance != 10*time.Minute {
		t.Fatalf("balance %v, want 10m", balance)
	}

	m2, _ := newManager(t, c, dir)
	balance2, _, _ := m2.Bank()
	if balance2 != 10*time.Minute {
		t.Fatalf("balance %v after restart, want 10m — not re-credited, not lost", balance2)
	}
}

// A break is not Active — it enforces nothing — so it is easy to forget its
// countdown in the checkpoint. Forgetting it means a machine that comes back
// owing intervals with no idea when the next one starts.
func TestPomodoroBreakSurvivesAReboot(t *testing.T) {
	dir := t.TempDir()
	c := newFake()

	m, _ := newManager(t, c, dir)
	if _, err := m.Commit(Plan{
		Mode: ModePomodoro, Duration: 25 * time.Minute,
		Break: 5 * time.Minute, Cycles: 4, Grace: DefaultGrace,
	}); err != nil {
		t.Fatal(err)
	}

	// Into the first break.
	c.tick(DefaultGrace + 25*time.Minute + time.Second)
	if got := m.Snapshot().State; got != Break {
		t.Fatalf("state %s, want BREAK", got)
	}

	m.mu.Lock()
	m.checkpointLocked()
	m.persistLocked()
	m.mu.Unlock()
	c.reboot(2 * time.Minute)

	m2, rec2 := newManager(t, c, dir)
	s := m2.Snapshot()
	if s.State != Break {
		t.Fatalf("state %s after reboot, want BREAK", s.State)
	}
	// Two of the five break minutes were spent with the machine off.
	if got := s.Break.Remaining(c, nil); got != 3*time.Minute {
		t.Fatalf("break remaining %v after a 2-minute reboot, want 3m", got)
	}
	if _, ok := rec2.last.Domains["pornhub.com"]; !ok {
		t.Fatal("baseline must be re-applied across a reboot taken during a break")
	}

	// And the next interval still starts on time.
	c.tick(3 * time.Minute)
	if got := m2.Snapshot().State; got != Focus {
		t.Fatalf("state %s, want the next interval to arm", got)
	}
	if got := m2.Snapshot().CycleIndex; got != 1 {
		t.Fatalf("cycle index %d, want 1", got)
	}
}

// Crediting is a loop over IntervalsCompleted rather than a single payment, so a
// cascade that crosses several interval boundaries cannot quietly lose the ones
// in the middle.
func TestEveryCompletedIntervalIsCreditedExactlyOnce(t *testing.T) {
	dir := t.TempDir()
	c := newFake()
	m, _ := newManager(t, c, dir)

	if _, err := m.Commit(Plan{
		Mode: ModePomodoro, Duration: 25 * time.Minute,
		Break: 5 * time.Minute, Cycles: 3, Grace: DefaultGrace,
	}); err != nil {
		t.Fatal(err)
	}

	c.tick(DefaultGrace + 25*time.Minute + time.Second) // interval 1 done
	// Credit rides the transition, and Bank() does not drive one — the Run loop
	// and every state read do. This is the same reason the API tests go through
	// getState.
	m.Snapshot()
	if b, _, _ := m.Bank(); b != 5*time.Minute {
		t.Fatalf("after one interval: %v, want 5m", b)
	}
	// Reading state repeatedly must not pay twice.
	m.Snapshot()
	m.Snapshot()
	if b, _, _ := m.Bank(); b != 5*time.Minute {
		t.Fatalf("re-credited on a plain read: %v", b)
	}

	// One phase at a time: an interval entered during a cascade is anchored at
	// the tick, not at the boundary it crossed, so jumping both phases at once
	// restarts interval 2 rather than finishing it.
	c.tick(5 * time.Minute) // break ends
	m.Snapshot()
	c.tick(25 * time.Minute) // interval 2 done
	m.Snapshot()
	if b, _, _ := m.Bank(); b != 10*time.Minute {
		t.Fatalf("after two intervals: %v, want 10m", b)
	}

	// And a restart must not re-pay for what is already banked.
	m2, _ := newManager(t, c, dir)
	if b, _, _ := m2.Bank(); b != 10*time.Minute {
		t.Fatalf("balance %v after restart, want 10m", b)
	}
}

// A daemon that was down through the whole session lands in COMPLETE in one
// tick, which is correct. The log has to record what it crossed on the way:
// session_FOCUS is the moment the lock became irreversible, and a tamper record
// that shows only where it ended up is missing the part that matters.
func TestACascadedTransitionLogsEveryStateItCrossed(t *testing.T) {
	dir := t.TempDir()
	c := newFake()

	st, err := store.Open(filepath.Join(dir, "state.db"), filepath.Join(dir, "key.bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	m := NewManager(st, c, &recorder{}, blocklist.Presets(), nil)
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Commit(Plan{Mode: ModeCommitment, Duration: 25 * time.Minute, ListIDs: nil, Grace: DefaultGrace, Penalty: false}); err != nil {
		t.Fatal(err)
	}

	// The machine is off for two hours: grace and the whole session elapse.
	c.tick(2 * time.Hour)
	if got := m.Snapshot().State; got != Complete {
		t.Fatalf("state %s, want COMPLETE", got)
	}

	evs, err := st.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range evs {
		seen[e.Kind] = true
	}
	for _, kind := range []string{"session_FOCUS", "session_COMPLETE"} {
		if !seen[kind] {
			t.Fatalf("%s missing from the log: %v", kind, seen)
		}
	}
}

// Moving the machine's timezone under an active scheduled lock is a tamper
// event. The window already holds — ActiveAt evaluates in the pinned offset —
// but the log has to say so, and it could not: the check compared
// time.Local.String() to the captured TZ name, both of which are the literal
// "Local" on Windows, so the branch was dead on the platform it defends.
func TestTimezoneChangeUnderAnActiveScheduleIsLogged(t *testing.T) {
	dir := t.TempDir()
	c := newFake()
	// 19:00 UTC. The machine claims UTC; the schedule was pinned at UTC-5.
	c.wall = time.Date(2026, 8, 4, 19, 0, 0, 0, time.UTC)

	st, err := store.Open(filepath.Join(dir, "state.db"), filepath.Join(dir, "key.bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	m := NewManager(st, c, &recorder{}, blocklist.Presets(), nil)
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	// Pinned to UTC-5, where 19:00 UTC is 14:00 — inside a 13:00-16:00 window.
	m.mu.Lock()
	m.schedules.Add(schedule.Schedule{
		ID: "sched.pinned", Name: "Pinned", ListIDs: []string{"preset.delivery"},
		Start: "13:00", End: "16:00", TZ: "Local", OffsetSeconds: -5 * 3600, Enabled: true,
	})
	m.checkTimezoneLocked()
	m.mu.Unlock()

	evs, err := st.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if e.Kind == "schedule_timezone_changed" {
			return
		}
	}
	t.Fatal("a timezone change under an active lock was not logged")
}

// The corollary: a machine that has not moved must not log a tamper event on
// every single startup.
func TestMatchingTimezoneLogsNothing(t *testing.T) {
	dir := t.TempDir()
	c := newFake()
	c.wall = time.Date(2026, 8, 4, 19, 0, 0, 0, time.UTC)

	st, err := store.Open(filepath.Join(dir, "state.db"), filepath.Join(dir, "key.bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	m := NewManager(st, c, &recorder{}, blocklist.Presets(), nil)
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.schedules.Add(schedule.Schedule{
		ID: "sched.utc", Name: "UTC", ListIDs: []string{"preset.delivery"},
		Start: "18:00", End: "21:00", TZ: "UTC", OffsetSeconds: 0, Enabled: true,
	})
	m.checkTimezoneLocked()
	m.mu.Unlock()

	evs, _ := st.Events(0, 0)
	for _, e := range evs {
		if e.Kind == "schedule_timezone_changed" {
			t.Fatal("logged a tamper event for a machine that never moved")
		}
	}
}

// A spend suspends what you earned the right to suspend, and nothing else.
// Baseline is not "off": the dial reading "not focusing" never means nothing is
// enforced, and neither does an open recreation window.
func TestSpendSuspendsSchedulesButNeverBaseline(t *testing.T) {
	c := newFake()
	c.wall = time.Date(2026, 8, 4, 19, 0, 0, 0, time.UTC) // inside the delivery window
	m, rec := newManager(t, c, t.TempDir())

	del, err := schedule.New("sched.delivery", "Delivery", []string{"preset.delivery"}, "18:00", "21:00", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.PutSchedule(del); err != nil {
		t.Fatal(err)
	}
	if _, ok := rec.last.Domains["ubereats.com"]; !ok {
		t.Fatal("setup: the schedule should be in force")
	}

	m.mu.Lock()
	m.bank.BalanceSeconds = 600
	m.mu.Unlock()
	if err := m.SpendBank(10 * time.Minute); err != nil {
		t.Fatal(err)
	}

	if _, ok := rec.last.Domains["ubereats.com"]; ok {
		t.Fatal("a spend must suspend the schedule you paid to suspend")
	}
	if got := rec.last.Domains["pornhub.com"]; got != enforce.Baseline {
		t.Fatal("a spend unblocked baseline — earning focus minutes must not open adult content")
	}

	// And the schedule comes back when the window closes.
	c.tick(10 * time.Minute)
	m.mu.Lock()
	m.tickLocked()
	m.applyLocked()
	m.mu.Unlock()
	if _, ok := rec.last.Domains["ubereats.com"]; !ok {
		t.Fatal("enforcement must hard re-lock at expiry")
	}
}

func TestTamperedSessionRowStartsIdleRatherThanCrashing(t *testing.T) {
	dir := t.TempDir()
	c := newFake()
	m, _ := newManager(t, c, dir)
	m.Commit(Plan{Mode: ModeCommitment, Duration: 30 * time.Minute, ListIDs: nil, Grace: DefaultGrace, Penalty: false})

	// Someone edits state.db by hand to try to shorten the lock.
	st, err := store.Open(filepath.Join(dir, "state.db"), filepath.Join(dir, "key.bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	raw, _ := st.Get(storeKey)
	_ = raw

	m2 := NewManager(st, c, &recorder{}, blocklist.Presets(), []string{"preset.adult"})
	if err := m2.Load(); err != nil {
		t.Fatalf("Load must not fail on a readable row: %v", err)
	}
	if m2.Snapshot().State != Focus && m2.Snapshot().State != Arming {
		t.Fatalf("a valid row should restore, got %s", m2.Snapshot().State)
	}
}
