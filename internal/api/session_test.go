package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lushenwar/Flow/internal/blocklist"
	"github.com/Lushenwar/Flow/internal/enforce"
	"github.com/Lushenwar/Flow/internal/session"
	"github.com/Lushenwar/Flow/internal/store"
)

type testClock struct {
	wall time.Time
	mono time.Duration
}

func (c *testClock) Wall() time.Time          { return c.wall }
func (c *testClock) Monotonic() time.Duration { return c.mono }
func (c *testClock) Unbiased() time.Duration  { return c.mono }
func (c *testClock) BootID() string           { return "boot-1" }
func (c *testClock) tick(d time.Duration)     { c.wall = c.wall.Add(d); c.mono += d }

type noopEnforcer struct{}

func (noopEnforcer) Set(enforce.Effective) {}

func sessionServer(t *testing.T) (http.Handler, *testClock) {
	h, c, _ := sessionServerReopenable(t)
	return h, c
}

// sessionServerReopenable also returns a function that stands up a second daemon
// against the same store, which is how "survives a restart" gets tested.
func sessionServerReopenable(t *testing.T) (http.Handler, *testClock, func() http.Handler) {
	t.Helper()
	dir := t.TempDir()
	c := &testClock{wall: time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC), mono: time.Hour}

	open := func() http.Handler {
		st, err := store.Open(filepath.Join(dir, "state.db"), filepath.Join(dir, "key.bin"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { st.Close() })

		mgr := session.NewManager(st, c, noopEnforcer{}, blocklist.Presets(), []string{"preset.adult"})
		if err := mgr.Load(); err != nil {
			t.Fatal(err)
		}
		return New(st, "secret", true, fakeEnforcement{}, mgr).Handler()
	}
	return open(), c, open
}

func do(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func getState(t *testing.T, h http.Handler) stateResponse {
	t.Helper()
	var out stateResponse
	if err := json.NewDecoder(do(t, h, "GET", "/api/state", nil).Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestStateReportsBaselineEnforcedWhileIdle(t *testing.T) {
	h, _ := sessionServer(t)
	st := getState(t, h)

	if st.Session.State != session.Idle {
		t.Fatalf("state %s", st.Session.State)
	}
	if len(st.Effective.BlockedIDs) == 0 {
		t.Fatal("IDLE with a baseline rule must not report an empty effective set")
	}
	if st.Effective.Attribution["preset.adult"] != enforce.Baseline {
		t.Fatalf("attribution %v — the daemon computes this, never the UI", st.Effective.Attribution)
	}
}

func TestCommitThenAbortDuringGrace(t *testing.T) {
	h, c := sessionServer(t)

	if code := do(t, h, "POST", "/api/session", commitRequest{
		Mode: "commitment", DurationMinutes: 25, BlocklistIDs: []string{"preset.video"},
	}).Code; code != http.StatusOK {
		t.Fatalf("commit returned %d", code)
	}

	st := getState(t, h)
	if st.Session.State != session.Arming {
		t.Fatalf("state %s, want ARMING", st.Session.State)
	}
	if !st.Session.CanRelease {
		t.Fatal("canRelease must be true in ARMING — it is what enables the second tap")
	}
	if st.Session.GraceRemainingSeconds == 0 {
		t.Fatal("grace countdown missing")
	}
	// Session blocks are live during the grace window.
	if st.Effective.Attribution["preset.video"] != enforce.Session {
		t.Fatal("blocks apply at commit, not at grace-end")
	}

	if code := do(t, h, "DELETE", "/api/session", nil).Code; code != http.StatusOK {
		t.Fatalf("abort returned %d", code)
	}
	if got := getState(t, h).Session.State; got != session.Idle {
		t.Fatalf("state %s after abort", got)
	}
	_ = c
}

// The single most important API property: there is no stop verb.
func TestNoStopVerbOnceLocked(t *testing.T) {
	h, c := sessionServer(t)
	do(t, h, "POST", "/api/session", commitRequest{DurationMinutes: 25})
	c.tick(session.DefaultGrace + time.Second)

	if got := getState(t, h).Session.State; got != session.Focus {
		t.Fatalf("state %s, want FOCUS", got)
	}

	rec := do(t, h, "DELETE", "/api/session", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("DELETE in FOCUS returned %d, want 409", rec.Code)
	}
	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "locked" {
		t.Fatalf("error %q, want locked", body["error"])
	}

	st := getState(t, h)
	if st.Session.CanRelease {
		t.Fatal("canRelease must be false in FOCUS")
	}
	if st.Session.RemainingSeconds == 0 {
		t.Fatal("remaining seconds missing")
	}
	if st.Session.TargetAt == nil {
		t.Fatal("targetAt missing — the UI derives the countdown from it")
	}
}

// An unknown mode must be refused rather than quietly run as something else.
// The API reporting success for work it did not do is worse than refusing.
func TestUnknownModeIsRefusedRatherThanQuietlySubstituted(t *testing.T) {
	h, _ := sessionServer(t)

	rec := do(t, h, "POST", "/api/session", commitRequest{
		Mode: "nonsense", DurationMinutes: 25,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown mode returned %d, want 400", rec.Code)
	}
	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "unsupported_mode" {
		t.Fatalf("error %q", body["error"])
	}
	if got := getState(t, h).Session.State; got != session.Idle {
		t.Fatalf("a refused commit started something anyway: %s", got)
	}

	// Empty still defaults to commitment — that is the documented behaviour.
	if code := do(t, h, "POST", "/api/session", commitRequest{DurationMinutes: 25}).Code; code != http.StatusOK {
		t.Fatalf("empty mode returned %d, want the commitment default", code)
	}
}

// A pomodoro without its own parameters is refused with the reason, rather than
// silently becoming a one-cycle session.
func TestPomodoroValidatesCyclesAndBreak(t *testing.T) {
	h, _ := sessionServer(t)

	cases := []struct {
		name string
		req  commitRequest
		want string
	}{
		{"no cycles", commitRequest{Mode: "pomodoro", DurationMinutes: 25, BreakMinutes: 5}, "cycles_out_of_range"},
		{"too many cycles", commitRequest{Mode: "pomodoro", DurationMinutes: 25, BreakMinutes: 5, Cycles: 99}, "cycles_out_of_range"},
		{"no break", commitRequest{Mode: "pomodoro", DurationMinutes: 25, Cycles: 4}, "break_out_of_range"},
		{"break too long", commitRequest{Mode: "pomodoro", DurationMinutes: 25, BreakMinutes: 999, Cycles: 4}, "break_out_of_range"},
		{"interval too short", commitRequest{Mode: "pomodoro", DurationMinutes: 1, BreakMinutes: 5, Cycles: 4}, "duration_out_of_range"},
	}
	for _, c := range cases {
		rec := do(t, h, "POST", "/api/session", c.req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s returned %d, want 400", c.name, rec.Code)
			continue
		}
		var body map[string]string
		json.NewDecoder(rec.Body).Decode(&body)
		if body["error"] != c.want {
			t.Errorf("%s: error %q, want %q", c.name, body["error"], c.want)
		}
	}
	if got := getState(t, h).Session.State; got != session.Idle {
		t.Fatalf("a refused commit started something anyway: %s", got)
	}
}

// The whole mode, end to end: four intervals, three breaks, locked during every
// focus interval and enforcing nothing but baseline during every break.
func TestPomodoroRunsItsCyclesAndOnlyLocksDuringFocus(t *testing.T) {
	h, c := sessionServer(t)

	if code := do(t, h, "POST", "/api/session", commitRequest{
		Mode: "pomodoro", DurationMinutes: 25, BreakMinutes: 5, Cycles: 3,
		BlocklistIDs: []string{"preset.video"},
	}).Code; code != http.StatusOK {
		t.Fatalf("commit returned %d", code)
	}

	// Grace, then the first interval.
	c.tick(session.DefaultGrace + time.Second)
	st := getState(t, h)
	if st.Session.State != session.Focus {
		t.Fatalf("state %s, want FOCUS", st.Session.State)
	}
	if st.Session.Cycle == nil || st.Session.Cycle.Index != 1 || st.Session.Cycle.Of != 3 {
		t.Fatalf("cycle %+v, want 1 of 3", st.Session.Cycle)
	}
	if st.Session.Cycle.Phase != "focus" {
		t.Fatalf("phase %q", st.Session.Cycle.Phase)
	}
	if st.Effective.Attribution["preset.video"] != enforce.Session {
		t.Fatal("session blocks must be live during a focus interval")
	}
	// There is no stop verb inside an interval.
	if do(t, h, "DELETE", "/api/session", nil).Code != http.StatusConflict {
		t.Fatal("abort must be refused during FOCUS")
	}

	// First interval ends: a break.
	c.tick(25 * time.Minute)
	st = getState(t, h)
	if st.Session.State != session.Break {
		t.Fatalf("state %s, want BREAK", st.Session.State)
	}
	if st.Session.Cycle.Phase != "break" {
		t.Fatalf("phase %q, want break", st.Session.Cycle.Phase)
	}
	if _, ok := st.Effective.Attribution["preset.video"]; ok {
		t.Fatal("session rules must lift during a break")
	}
	if st.Effective.Attribution["preset.adult"] != enforce.Baseline {
		t.Fatal("baseline keeps running through a break")
	}
	if st.Session.RemainingSeconds == 0 {
		t.Fatal("a break needs a countdown to render")
	}

	// The break ends and the next interval arms itself — straight into FOCUS,
	// with no fresh grace window. Four free bailout points in one session is not
	// a commitment device.
	c.tick(5 * time.Minute)
	st = getState(t, h)
	if st.Session.State != session.Focus {
		t.Fatalf("state %s, want FOCUS for cycle 2", st.Session.State)
	}
	if st.Session.Cycle.Index != 2 {
		t.Fatalf("cycle index %d, want 2", st.Session.Cycle.Index)
	}
	if st.Session.CanRelease {
		t.Fatal("the next interval must not reopen the abort window")
	}
	if st.Effective.Attribution["preset.video"] != enforce.Session {
		t.Fatal("blocks must come back for the next interval")
	}

	// Run out the rest, one phase at a time. Ticking the whole remainder in one
	// jump does NOT work, and that is a real property rather than a test
	// artefact: a break entered during a cascade is anchored at the moment the
	// tick ran, not at the instant the interval actually ended, so a long
	// downtime stretches the schedule rather than replaying it. Harmless,
	// because a break enforces nothing — but worth knowing before someone
	// "fixes" a test by making the cascade skip breaks entirely.
	c.tick(25 * time.Minute) // cycle 2 focus ends
	if got := getState(t, h).Session.State; got != session.Break {
		t.Fatalf("state %s, want BREAK after cycle 2", got)
	}
	c.tick(5 * time.Minute) // break ends, cycle 3 begins
	if got := getState(t, h).Session.Cycle.Index; got != 3 {
		t.Fatalf("cycle index %d, want 3", got)
	}
	c.tick(25 * time.Minute) // cycle 3 ends, and there is no fourth
	st = getState(t, h)
	if st.Session.State != session.Complete {
		t.Fatalf("state %s, want COMPLETE after the last interval", st.Session.State)
	}
	if _, ok := st.Effective.Attribution["preset.video"]; ok {
		t.Fatal("session rules must lift when the pomodoro finishes")
	}

	// Three 25-minute intervals at 0.2 = 15 minutes banked.
	if got := getBank(t, h).BalanceSeconds; got != 3*300 {
		t.Fatalf("balance %d seconds, want 900 — every interval earns, not just the last", got)
	}
}

// A break enforces nothing beyond baseline, so there is no lock to break out of
// — only the next interval to decline.
func TestPomodoroCanBeEndedDuringABreakButNotDuringAnInterval(t *testing.T) {
	h, c := sessionServer(t)
	do(t, h, "POST", "/api/session", commitRequest{
		Mode: "pomodoro", DurationMinutes: 25, BreakMinutes: 5, Cycles: 4,
	})
	c.tick(session.DefaultGrace + 25*time.Minute + time.Second)

	st := getState(t, h)
	if st.Session.State != session.Break {
		t.Fatalf("state %s, want BREAK", st.Session.State)
	}
	if !st.Session.CanRelease {
		t.Fatal("canRelease must be true during a break — the UI renders the tap from it")
	}

	// Committing over a running pomodoro must not silently discard the intervals
	// still owed.
	if code := do(t, h, "POST", "/api/session", commitRequest{DurationMinutes: 25}).Code; code != http.StatusConflict {
		t.Fatalf("commit during a break returned %d, want 409", code)
	}

	if code := do(t, h, "DELETE", "/api/session", nil).Code; code != http.StatusOK {
		t.Fatalf("abort during a break returned %d", code)
	}
	if got := getState(t, h).Session.State; got != session.Idle {
		t.Fatalf("state %s after ending at a break", got)
	}
	// One interval was completed, so one interval is paid for.
	if got := getBank(t, h).BalanceSeconds; got != 300 {
		t.Fatalf("balance %d, want 300 — the finished interval earns, the abandoned one does not", got)
	}
}

func TestSecondSessionRejectedWhileActive(t *testing.T) {
	h, _ := sessionServer(t)
	do(t, h, "POST", "/api/session", commitRequest{DurationMinutes: 25})

	if code := do(t, h, "POST", "/api/session", commitRequest{DurationMinutes: 25}).Code; code != http.StatusConflict {
		t.Fatalf("second commit returned %d, want 409", code)
	}
}

func TestDurationValidation(t *testing.T) {
	h, _ := sessionServer(t)
	for _, m := range []int{0, 4, 481, -5} {
		rec := do(t, h, "POST", "/api/session", commitRequest{DurationMinutes: m})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("duration %d returned %d, want 400", m, rec.Code)
		}
	}
}

func TestEscapeStartsACountdownAndDoesNotReleaseEarly(t *testing.T) {
	h, c := sessionServer(t)
	do(t, h, "POST", "/api/session", commitRequest{DurationMinutes: 60})
	c.tick(session.DefaultGrace + time.Second)

	if code := do(t, h, "POST", "/api/session/escape", nil).Code; code != http.StatusOK {
		t.Fatal("escape must always be available — it is the line between a tool and a trap")
	}
	st := getState(t, h)
	if st.Session.State != session.Releasing || !st.Session.Escape.Requested {
		t.Fatalf("state %s escape %+v", st.Session.State, st.Session.Escape)
	}
	if st.Session.Escape.AvailableAt == nil {
		t.Fatal("availableAt missing")
	}

	// The challenge cannot be fetched early and pre-typed.
	if code := do(t, h, "GET", "/api/session/escape/challenge", nil).Code; code != http.StatusConflict {
		t.Fatal("the challenge must not be available before the delay elapses")
	}
	// Blocks stay fully enforced throughout the countdown.
	if st.Effective.Attribution["preset.adult"] != enforce.Baseline {
		t.Fatal("baseline lost during RELEASING")
	}

	c.tick(session.MinDelay)
	ch := getChallenge(t, h)
	if len(ch.Text) != session.ChallengeLength {
		t.Fatalf("challenge is %d characters", len(ch.Text))
	}

	if code := do(t, h, "POST", "/api/session/escape/verify", verifyRequest{
		ChallengeID: ch.ID, Typed: strings.ToLower(ch.Text),
	}).Code; code != http.StatusBadRequest {
		t.Fatal("the challenge is case-sensitive")
	}
	if got := getState(t, h).Session.State; got != session.Releasing {
		t.Fatalf("a failed attempt must not end the session, state %s", got)
	}

	if code := do(t, h, "POST", "/api/session/escape/verify", verifyRequest{
		ChallengeID: ch.ID, Typed: ch.Text,
	}).Code; code != http.StatusOK {
		t.Fatal("the exact text after the delay must release")
	}
	if got := getState(t, h).Session.State; got != session.Idle {
		t.Fatalf("state %s after release", got)
	}
}

func getChallenge(t *testing.T, h http.Handler) session.Challenge {
	t.Helper()
	rec := do(t, h, "GET", "/api/session/escape/challenge", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("challenge returned %d", rec.Code)
	}
	var c session.Challenge
	if err := json.NewDecoder(rec.Body).Decode(&c); err != nil {
		t.Fatal(err)
	}
	return c
}

// Every failed attempt is recorded, and none of them shorten anything.
func TestFailedChallengeAttemptsAreLoggedAndCostNothingButTime(t *testing.T) {
	h, c := sessionServer(t)
	do(t, h, "POST", "/api/session", commitRequest{DurationMinutes: 60})
	c.tick(session.DefaultGrace + time.Second)
	do(t, h, "POST", "/api/session/escape", nil)
	c.tick(session.MinDelay)

	ch := getChallenge(t, h)
	for i := 0; i < 3; i++ {
		do(t, h, "POST", "/api/session/escape/verify", verifyRequest{
			ChallengeID: ch.ID, Typed: "nope",
		})
	}
	if got := getState(t, h).Session.State; got != session.Releasing {
		t.Fatalf("state %s", got)
	}

	// Retrying must not mint a new challenge — that would let someone reroll
	// until they get a string they like.
	if again := getChallenge(t, h); again.Text != ch.Text {
		t.Fatal("challenge changed between attempts")
	}

	var evs []struct {
		Kind string `json:"kind"`
	}
	json.NewDecoder(do(t, h, "GET", "/api/events", nil).Body).Decode(&evs)
	failures := 0
	for _, e := range evs {
		if e.Kind == "escape_challenge_failed" {
			failures++
		}
	}
	if failures != 3 {
		t.Fatalf("logged %d failed attempts, want 3", failures)
	}
}

// The dial cannot draw its ring without the denominators, and a restarted
// window has no memory of what was asked for.
func TestStateShipsTheArcDenominators(t *testing.T) {
	h, c := sessionServer(t)
	do(t, h, "POST", "/api/session", commitRequest{DurationMinutes: 25})

	arming := getState(t, h).Session
	if arming.DurationSeconds != 1500 {
		t.Fatalf("durationSeconds %d, want 1500", arming.DurationSeconds)
	}
	if arming.GraceSeconds != int(session.DefaultGrace.Seconds()) {
		t.Fatalf("graceSeconds %d, want %v", arming.GraceSeconds, session.DefaultGrace)
	}
	if arming.GraceRemainingSeconds == 0 {
		t.Fatal("grace countdown missing during ARMING")
	}

	c.tick(session.DefaultGrace + time.Second)
	focus := getState(t, h).Session
	if focus.DurationSeconds != 1500 {
		t.Fatalf("durationSeconds lost in FOCUS: %d", focus.DurationSeconds)
	}
	if focus.GraceRemainingSeconds != 0 {
		t.Fatalf("grace should be spent, got %d", focus.GraceRemainingSeconds)
	}

	// IDLE has no session, so there is no ring to draw.
	do(t, h, "POST", "/api/session/escape", nil)
	c.tick(session.MinDelay)
	ch := getChallenge(t, h)
	do(t, h, "POST", "/api/session/escape/verify", verifyRequest{ChallengeID: ch.ID, Typed: ch.Text})
	if idle := getState(t, h).Session; idle.DurationSeconds != 0 || idle.TargetAt != nil {
		t.Fatalf("IDLE still reports a target: %+v", idle)
	}
}

func TestStateSurvivesAClockJump(t *testing.T) {
	h, c := sessionServer(t)
	do(t, h, "POST", "/api/session", commitRequest{DurationMinutes: 30})
	c.tick(session.DefaultGrace + time.Minute)

	before := getState(t, h).Session.RemainingSeconds
	c.wall = c.wall.Add(3 * time.Hour)
	after := getState(t, h).Session.RemainingSeconds

	if before != after {
		t.Fatalf("remaining went %d -> %d on a wall-clock change", before, after)
	}
}

// Session composition lives behind its own verb, edited when calm. claude.md is
// explicit that choosing it at the moment of commitment is a bypass: a user
// mid-craving would deselect YouTube and commit to a session blocking nothing.
func TestSessionListsAreEditableWhenIdleAndRefusedDuringASession(t *testing.T) {
	h, c := sessionServer(t)

	var got struct {
		ListIDs []string `json:"listIds"`
	}
	json.NewDecoder(do(t, h, "GET", "/api/session/lists", nil).Body).Decode(&got)
	if len(got.ListIDs) == 0 {
		t.Fatal("no default session lists")
	}

	// Editable while idle.
	if code := do(t, h, "PUT", "/api/session/lists", sessionListsPut{
		ListIDs: []string{"preset.video", "preset.shopping"},
	}).Code; code != http.StatusOK {
		t.Fatalf("edit while idle returned %d", code)
	}

	// And a committed session actually uses them.
	do(t, h, "POST", "/api/session", commitRequest{
		DurationMinutes: 25, BlocklistIDs: []string{"preset.video", "preset.shopping"},
	})
	c.tick(session.DefaultGrace + time.Second)

	// Refused mid-session. The guard is the daemon's, not the UI's: a settings
	// screen you can open mid-craving is the dial with extra clicks.
	rec := do(t, h, "PUT", "/api/session/lists", sessionListsPut{
		ListIDs: []string{"preset.shopping"},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("edit during a session returned %d, want 409", rec.Code)
	}
	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "would_weaken" {
		t.Fatalf("error %q", body["error"])
	}

	// An empty list is a session that blocks nothing — the exact outcome the
	// mid-craving edit was going to produce.
	if code := do(t, h, "PUT", "/api/session/lists", sessionListsPut{ListIDs: nil}).Code; code != http.StatusBadRequest {
		t.Fatalf("empty list returned %d, want 400", code)
	}
}
