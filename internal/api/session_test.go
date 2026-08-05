package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

	if code := do(t, h, "POST", "/api/session/release", nil).Code; code != http.StatusConflict {
		t.Fatal("release before the delay expires must be refused")
	}
	// Blocks stay fully enforced throughout the countdown.
	if st.Effective.Attribution["preset.adult"] != enforce.Baseline {
		t.Fatal("baseline lost during RELEASING")
	}

	c.tick(session.MinDelay)
	if code := do(t, h, "POST", "/api/session/release", nil).Code; code != http.StatusOK {
		t.Fatal("release after the delay must succeed")
	}
	if got := getState(t, h).Session.State; got != session.Idle {
		t.Fatalf("state %s after release", got)
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
	do(t, h, "POST", "/api/session/release", nil)
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
