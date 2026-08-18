package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/Lushenwar/Flow/internal/session"
)

func allow(t *testing.T, h http.Handler) allowView {
	t.Helper()
	var out allowView
	if err := json.NewDecoder(do(t, h, "GET", "/api/allowlist", nil).Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// The direction is REVERSED from the blocklist, and that is the whole design:
// under default-deny, adding to the allowlist is what weakens enforcement.
func TestAllowlistAddIsRefusedDuringADefaultDenyWindow(t *testing.T) {
	h, c := sessionServer(t)

	// Free while nothing is inverted — it enforces nothing then.
	if code := do(t, h, "POST", "/api/allowlist", addRequest{
		Domains: []string{"github.com", "https://work-vpn.example/login"},
	}).Code; code != http.StatusOK {
		t.Fatalf("add while idle returned %d", code)
	}
	got := allow(t, h)
	if !slices.Contains(got.Domains, "work-vpn.example") {
		t.Fatalf("a pasted URL must normalise: %v", got.Domains)
	}
	if got.Locked {
		t.Fatal("nothing is inverted yet")
	}

	// Now invert.
	do(t, h, "POST", "/api/session", commitRequest{
		DurationMinutes: 30, BlocklistIDs: []string{"preset.offline"},
	})
	c.tick(session.DefaultGrace + time.Second)

	if !allow(t, h).Locked {
		t.Fatal("an offline session must report the allowlist as locked")
	}

	rec := do(t, h, "POST", "/api/allowlist", addRequest{Domains: []string{"reddit.com"}})
	if rec.Code != http.StatusConflict {
		t.Fatalf("add during a default-deny window returned %d, want 409", rec.Code)
	}
	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "would_weaken" {
		t.Fatalf("error %q", body["error"])
	}
	if slices.Contains(allow(t, h).Domains, "reddit.com") {
		t.Fatal("a refused addition must not be stored — that is 'block everything except the site I want'")
	}

	// Removing is strengthening, so it is free even mid-window.
	if code := do(t, h, "DELETE", "/api/allowlist/github.com", nil).Code; code != http.StatusOK {
		t.Fatalf("remove during a window returned %d — narrowing the escape list strengthens it", code)
	}
	if slices.Contains(allow(t, h).Domains, "github.com") {
		t.Fatal("still listed after removal")
	}
}

// A permanently-allowed domain is redundant here rather than wrong: it is
// already unblockable, so accepting it and doing nothing beats erroring at
// someone who was being careful.
func TestAllowlistAcceptsPermanentlyAllowedDomainsWithoutComplaining(t *testing.T) {
	h, _ := sessionServer(t)
	if code := do(t, h, "POST", "/api/allowlist", addRequest{
		Domains: []string{"988lifeline.org"},
	}).Code; code != http.StatusOK {
		t.Fatalf("returned %d — this is redundant, not invalid", code)
	}
	if code := do(t, h, "POST", "/api/allowlist", addRequest{
		Domains: []string{"not a domain"},
	}).Code; code != http.StatusBadRequest {
		t.Fatalf("garbage returned %d", code)
	}
}

// The mode is only safe because the effective set says so out loud.
func TestStateReportsDefaultDeny(t *testing.T) {
	h, c := sessionServer(t)
	do(t, h, "POST", "/api/session", commitRequest{
		DurationMinutes: 30, BlocklistIDs: []string{"preset.bedtime"},
	})
	c.tick(session.DefaultGrace + time.Second)

	if !allow(t, h).Locked {
		t.Fatal("bedtime inverts the blocklist and nothing said so")
	}
}
