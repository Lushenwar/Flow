package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Lushenwar/Flow/internal/session"
)

func getBank(t *testing.T, h http.Handler) bankView {
	t.Helper()
	var out bankView
	if err := json.NewDecoder(do(t, h, "GET", "/api/bank", nil).Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// The exit criterion: 50 locked minutes credits exactly 10.0 recreation minutes,
// once, and it survives a restart.
func TestFiftyMinutesCreditsTenAndSurvivesRestart(t *testing.T) {
	h, c, reopen := sessionServerReopenable(t)

	if got := getBank(t, h).BalanceSeconds; got != 0 {
		t.Fatalf("starting balance %d", got)
	}

	do(t, h, "POST", "/api/session", commitRequest{DurationMinutes: 50})
	c.tick(session.DefaultGrace + 50*time.Minute)
	if st := getState(t, h).Session.State; st != session.Complete {
		t.Fatalf("state %s", st)
	}

	if got := getBank(t, h).BalanceSeconds; got != 600 {
		t.Fatalf("balance %d seconds, want 600 (10.0 minutes)", got)
	}

	// Reading state repeatedly must not pay twice.
	getState(t, h)
	getState(t, h)
	if got := getBank(t, h).BalanceSeconds; got != 600 {
		t.Fatalf("balance %d after extra reads — credit must happen exactly once", got)
	}

	// And a daemon restart must not re-credit the same session.
	h2 := reopen()
	if got := getBank(t, h2).BalanceSeconds; got != 600 {
		t.Fatalf("balance %d after restart", got)
	}
}

func TestAbortedAndEscapedSessionsEarnNothing(t *testing.T) {
	h, c := sessionServer(t)

	// Aborted during the grace window.
	do(t, h, "POST", "/api/session", commitRequest{DurationMinutes: 50})
	do(t, h, "DELETE", "/api/session", nil)
	if got := getBank(t, h).BalanceSeconds; got != 0 {
		t.Fatalf("aborted session paid %d", got)
	}

	// Escaped. "Start, escape immediately" must not be a minute farm.
	do(t, h, "POST", "/api/session", commitRequest{DurationMinutes: 50})
	c.tick(session.DefaultGrace + time.Second)
	do(t, h, "POST", "/api/session/escape", nil)
	c.tick(session.MinDelay)
	ch := getChallenge(t, h)
	do(t, h, "POST", "/api/session/escape/verify", verifyRequest{ChallengeID: ch.ID, Typed: ch.Text})

	if got := getBank(t, h).BalanceSeconds; got != 0 {
		t.Fatalf("escaped session paid %d", got)
	}
}

func TestSpendOpensTheBlocklistThenHardRelocks(t *testing.T) {
	h, c := sessionServer(t)

	// Earn 10 minutes.
	do(t, h, "POST", "/api/session", commitRequest{DurationMinutes: 50})
	c.tick(session.DefaultGrace + 50*time.Minute)
	getState(t, h)
	do(t, h, "POST", "/api/session/ack", nil)

	if _, ok := getState(t, h).Effective.Attribution["preset.adult"]; !ok {
		t.Fatal("baseline should be enforced before the spend")
	}

	if code := do(t, h, "POST", "/api/bank/spend", spendRequest{Minutes: 10}).Code; code != http.StatusOK {
		t.Fatalf("spend returned %d", code)
	}
	if b := getBank(t, h); !b.Spending || b.BalanceSeconds != 0 {
		t.Fatalf("after spend: %+v — the whole balance is deducted up front", b)
	}

	// Baseline is NOT lifted by a spend. Recreation time buys back the things
	// you avoid for productivity, never the things you asked to be permanently
	// protected from.
	if getState(t, h).Effective.Attribution["preset.adult"] != "baseline" {
		t.Fatal("a spend unblocked a baseline rule — earning focus minutes must not open adult content")
	}

	c.tick(10 * time.Minute)
	if getBank(t, h).Spending {
		t.Fatal("window should have closed")
	}
	if _, ok := getState(t, h).Effective.Attribution["preset.adult"]; !ok {
		t.Fatal("enforcement must hard re-lock at expiry")
	}
}

func TestCannotSpendDuringASession(t *testing.T) {
	h, c := sessionServer(t)
	do(t, h, "POST", "/api/session", commitRequest{DurationMinutes: 50})
	c.tick(session.DefaultGrace + 50*time.Minute)
	getState(t, h)
	do(t, h, "POST", "/api/session/ack", nil)

	// Earned, then locked again.
	do(t, h, "POST", "/api/session", commitRequest{DurationMinutes: 25})
	c.tick(session.DefaultGrace + time.Second)

	rec := do(t, h, "POST", "/api/bank/spend", spendRequest{Minutes: 5})
	if rec.Code != http.StatusConflict {
		t.Fatalf("spend during FOCUS returned %d, want 409", rec.Code)
	}
	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "not_idle" {
		t.Fatalf("error %q", body["error"])
	}
}

func TestCannotSpendMoreThanEarnedOverAPI(t *testing.T) {
	h, _ := sessionServer(t)
	if code := do(t, h, "POST", "/api/bank/spend", spendRequest{Minutes: 5}).Code; code != http.StatusBadRequest {
		t.Fatalf("returned %d, want 400", code)
	}
}
