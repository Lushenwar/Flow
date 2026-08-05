package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Lushenwar/Flow/internal/session"
)

func rows(t *testing.T, h http.Handler) map[string]baselineRow {
	t.Helper()
	var got []baselineRow
	if err := json.NewDecoder(do(t, h, "GET", "/api/baseline", nil).Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	out := map[string]baselineRow{}
	for _, r := range got {
		out[r.ID] = r
	}
	return out
}

func TestBaselineEnableIsImmediate(t *testing.T) {
	h, _ := sessionServer(t)

	if code := do(t, h, "POST", "/api/baseline/preset.doomscroll/enable", nil).Code; code != http.StatusOK {
		t.Fatalf("enable returned %d", code)
	}
	r := rows(t, h)["preset.doomscroll"]
	if !r.Enabled || r.PendingDisableAt != nil {
		t.Fatalf("turning a rule ON must be instant: %+v", r)
	}
	if getState(t, h).Effective.Attribution["preset.doomscroll"] != "baseline" {
		t.Fatal("enable did not reach the effective set")
	}
}

// The asymmetry, end to end.
func TestBaselineDisableTakesFifteenMinutesAndStaysBlocked(t *testing.T) {
	h, c := sessionServer(t)

	rec := do(t, h, "POST", "/api/baseline/preset.adult/disable", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable returned %d", rec.Code)
	}
	var body struct {
		PendingDisableAt *time.Time `json:"pendingDisableAt"`
	}
	json.NewDecoder(rec.Body).Decode(&body)
	if body.PendingDisableAt == nil {
		t.Fatal("no pendingDisableAt returned — the UI renders the countdown from it")
	}

	r := rows(t, h)["preset.adult"]
	if !r.Enabled {
		t.Fatal("the rule must read as ON for the whole countdown; it is still enforced")
	}
	if getState(t, h).Effective.Attribution["preset.adult"] != "baseline" {
		t.Fatal("the site must stay blocked during the countdown")
	}

	c.tick(14 * time.Minute)
	if !rows(t, h)["preset.adult"].Enabled {
		t.Fatal("still enforced at 14:00")
	}

	c.tick(2 * time.Minute)
	if rows(t, h)["preset.adult"].Enabled {
		t.Fatal("should have lifted after the delay")
	}
	if _, ok := getState(t, h).Effective.Attribution["preset.adult"]; ok {
		t.Fatal("still in the effective set after the delay fired")
	}
}

func TestBaselineCancelIsInstantAndFree(t *testing.T) {
	h, c := sessionServer(t)
	do(t, h, "POST", "/api/baseline/preset.adult/disable", nil)
	c.tick(14 * time.Minute)

	if code := do(t, h, "DELETE", "/api/baseline/preset.adult/disable", nil).Code; code != http.StatusOK {
		t.Fatal("cancel must always work")
	}
	if r := rows(t, h)["preset.adult"]; r.PendingDisableAt != nil {
		t.Fatal("countdown not cleared")
	}
	c.tick(time.Hour)
	if !rows(t, h)["preset.adult"].Enabled {
		t.Fatal("a cancelled disable must never fire")
	}
}

func TestBaselineReEnableCancelsThePendingDisable(t *testing.T) {
	h, c := sessionServer(t)
	do(t, h, "POST", "/api/baseline/preset.adult/disable", nil)
	c.tick(10 * time.Minute)

	do(t, h, "POST", "/api/baseline/preset.adult/enable", nil)
	if r := rows(t, h)["preset.adult"]; r.PendingDisableAt != nil {
		t.Fatal("re-enabling during the countdown must cancel it")
	}
	c.tick(time.Hour)
	if !rows(t, h)["preset.adult"].Enabled {
		t.Fatal("rule went off after being re-enabled")
	}
}

// Neither surface can weaken the other.
func TestBaselineDisableRejectedDuringASession(t *testing.T) {
	h, c := sessionServer(t)
	do(t, h, "POST", "/api/session", commitRequest{DurationMinutes: 30})
	c.tick(session.DefaultGrace + time.Second)

	rec := do(t, h, "POST", "/api/baseline/preset.adult/disable", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("returned %d, want 409", rec.Code)
	}
	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "would_weaken" {
		t.Fatalf("error %q", body["error"])
	}
	if r := rows(t, h)["preset.adult"]; r.PendingDisableAt != nil {
		t.Fatal("the delayed-disable path must not even start during a session")
	}
}

// Strengthening is always allowed, even mid-session.
func TestBaselineEnableAllowedDuringASession(t *testing.T) {
	h, c := sessionServer(t)
	do(t, h, "POST", "/api/session", commitRequest{DurationMinutes: 30})
	c.tick(session.DefaultGrace + time.Second)

	if code := do(t, h, "POST", "/api/baseline/preset.gambling/enable", nil).Code; code != http.StatusOK {
		t.Fatal("adding a rule mid-session must be allowed")
	}
	if !rows(t, h)["preset.gambling"].Enabled {
		t.Fatal("enable did not take")
	}
}

// The countdown survives an app restart because it lives in the daemon.
func TestPendingDisableSurvivesRestart(t *testing.T) {
	h, c, reopen := sessionServerReopenable(t)
	do(t, h, "POST", "/api/baseline/preset.adult/disable", nil)
	c.tick(5 * time.Minute)

	h2 := reopen()
	r := rows(t, h2)["preset.adult"]
	if r.PendingDisableAt == nil {
		t.Fatal("a disable request that resets when the daemon restarts is not friction")
	}
	if !r.Enabled {
		t.Fatal("still enforced")
	}

	c.tick(10 * time.Minute)
	if rows(t, h2)["preset.adult"].Enabled {
		t.Fatal("countdown did not carry over; the rule should be off by now")
	}
}
