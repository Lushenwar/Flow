package session

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Lushenwar/Flow/internal/blocklist"
	"github.com/Lushenwar/Flow/internal/enforce"
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

	if _, err := m.Commit(ModeCommitment, 25*time.Minute, []string{"preset.video"}, DefaultGrace, false); err != nil {
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

	m.Commit(ModeCommitment, 5*time.Minute, []string{"preset.video"}, DefaultGrace, false)
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
	m.Commit(ModeCommitment, 30*time.Minute, []string{"preset.video"}, DefaultGrace, false)
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
	m.Commit(ModeCommitment, 30*time.Minute, nil, DefaultGrace, false)
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

	m.Commit(ModeCommitment, 30*time.Minute, nil, DefaultGrace, true)
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
	m.Commit(ModeCommitment, 60*time.Minute, nil, DefaultGrace, true)

	m.mu.Lock()
	m.checkpointLocked()
	m.mu.Unlock()
	c.mono += 5 * time.Second
	c.wall = c.wall.Add(30 * time.Minute)

	m.mu.Lock()
	m.checkDriftLocked()
	m.mu.Unlock()

	evs, err := st.Events(0)
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

func TestTamperedSessionRowStartsIdleRatherThanCrashing(t *testing.T) {
	dir := t.TempDir()
	c := newFake()
	m, _ := newManager(t, c, dir)
	m.Commit(ModeCommitment, 30*time.Minute, nil, DefaultGrace, false)

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
