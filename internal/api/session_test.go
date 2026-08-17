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

// ModePomodoro is a constant and two unused Session fields. Accepting it
// returned 200 and ran a commitment session, which is the API reporting success
// for work it did not do.
func TestUnimplementedModeIsRefusedRatherThanQuietlySubstituted(t *testing.T) {
	h, _ := sessionServer(t)

	rec := do(t, h, "POST", "/api/session", commitRequest{
		Mode: "pomodoro", DurationMinutes: 25,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("pomodoro returned %d, want 400 until it is built", rec.Code)
	}
	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "unsupported_mode" {
		t.Fatalf("error %q", body["error"])
	}
	if got := getState(t, h).Session.State; got != session.Idle {
		t.Fatalf("a refused commit started something anyway: %s", got)
	}

	// An unknown mode is refused the same way, rather than falling through.
	if code := do(t, h, "POST", "/api/session", commitRequest{
		Mode: "nonsense", DurationMinutes: 25,
	}).Code; code != http.StatusBadRequest {
		t.Fatalf("unknown mode returned %d", code)
	}

	// Empty still defaults to commitment — that is the documented behaviour.
	if code := do(t, h, "POST", "/api/session", commitRequest{DurationMinutes: 25}).Code; code != http.StatusOK {
		t.Fatalf("empty mode returned %d, want the commitment default", code)
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
