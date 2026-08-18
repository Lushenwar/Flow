package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/Lushenwar/Flow/internal/blocklist"
	"github.com/Lushenwar/Flow/internal/session"
)

func getRules(t *testing.T, h http.Handler, token string) (rulesResponse, int) {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/rules", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var out rulesResponse
	json.NewDecoder(rec.Body).Decode(&out)
	return out, rec.Code
}

// The extension lives in the browser sandbox and cannot read the token file.
func TestRulesNeedsNoToken(t *testing.T) {
	h, _ := sessionServer(t)

	got, code := getRules(t, h, "")
	if code != http.StatusOK {
		t.Fatalf("unauthenticated rules returned %d", code)
	}
	if !got.Enforcing {
		t.Fatal("baseline is on, so the extension must be told to enforce")
	}

	// Everything else still requires it.
	if _, code := getRules(t, h, ""); code != http.StatusOK {
		t.Fatal("rules should stay open")
	}
	req := httptest.NewRequest("GET", "/api/state", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/api/state without a token returned %d, want 401", rec.Code)
	}
}

func TestRulesTracksTheEffectiveSet(t *testing.T) {
	h, c := sessionServer(t)

	before, _ := getRules(t, h, "")
	if slices.Contains(before.Domains, "youtube.com") {
		t.Fatal("video is not on baseline")
	}

	do(t, h, "POST", "/api/session", commitRequest{
		DurationMinutes: 25, BlocklistIDs: []string{"preset.video"},
	})
	c.tick(session.DefaultGrace + time.Second)

	after, _ := getRules(t, h, "")
	if !slices.Contains(after.Domains, "youtube.com") {
		t.Fatal("session rules must reach the extension")
	}
	if after.Version == before.Version {
		t.Fatal("version must change with the set, or the extension skips the sweep")
	}
}

func TestRulesVersionIsStableWhenNothingChanges(t *testing.T) {
	h, _ := sessionServer(t)
	a, _ := getRules(t, h, "")
	b, _ := getRules(t, h, "")
	if a.Version != b.Version {
		t.Fatal("an unchanged set must not churn the version — it triggers a full tab sweep")
	}
}

func TestRulesCarriesPathExceptions(t *testing.T) {
	h, c := sessionServer(t)
	do(t, h, "POST", "/api/session", commitRequest{
		DurationMinutes: 25, BlocklistIDs: []string{"preset.study"},
	})
	c.tick(session.DefaultGrace + time.Second)

	got, _ := getRules(t, h, "")
	pats, ok := got.AllowPaths["youtube.com"]
	if !ok || len(pats) == 0 {
		t.Fatalf("study must carry YouTube path exceptions, got %v", got.AllowPaths)
	}
	if !slices.Contains(pats, `^/@`) {
		t.Fatalf("channel URLs must be exempt, got %v", pats)
	}
}

// A plain work session must not inherit study's exceptions.
func TestPathExceptionsDoNotLeakAcrossLists(t *testing.T) {
	cat := blocklist.Presets()

	study := cat.ResolvePaths([]string{"preset.study"})
	if len(study["youtube.com"]) == 0 {
		t.Fatal("study should have exceptions")
	}
	work := cat.ResolvePaths([]string{"preset.work"})
	if len(work["youtube.com"]) != 0 {
		t.Fatalf("work must block YouTube outright, got %v", work)
	}

	// Both active at once: the stricter one wins, or "add study to a work
	// session" would quietly unblock channels.
	both := cat.ResolvePaths([]string{"preset.study", "preset.work"})
	if len(both["youtube.com"]) != 0 {
		t.Fatalf("an outright block must beat a path exception, got %v", both)
	}
}

// "*" meant any web page you visited could fetch this and READ it — not merely
// probe for it — learning that you run Flow and whether you block adult content
// or gambling. Not an authority leak; a privacy one, in about the most
// sensitive categories there are.
func TestRulesIsNotReadableByArbitraryWebPages(t *testing.T) {
	h, _ := sessionServer(t)

	req := httptest.NewRequest("GET", "/api/rules", nil)
	req.Header.Set("Origin", "https://some-random-site.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("a web page origin got CORS %q — the browser would hand it the body", got)
	}

	// The extension still works, which is the entire reason this route is open.
	for _, origin := range []string{
		"chrome-extension://abcdefghijklmnop",
		"moz-extension://12345678-1234-1234-1234-123456789abc",
	} {
		req := httptest.NewRequest("GET", "/api/rules", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Errorf("extension origin %q got CORS %q", origin, got)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("extension origin %q got status %d", origin, rec.Code)
		}
	}

	// And a request with no Origin at all — curl, flowctl — is unaffected.
	if _, code := getRules(t, h, ""); code != http.StatusOK {
		t.Fatalf("originless request returned %d", code)
	}
}
