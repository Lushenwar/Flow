package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/Lushenwar/Flow/internal/blocklist"
	"github.com/Lushenwar/Flow/internal/session"
)

func custom(t *testing.T, h http.Handler) customView {
	t.Helper()
	var out customView
	if err := json.NewDecoder(do(t, h, "GET", "/api/blocklists", nil).Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func add(t *testing.T, h http.Handler, domains ...string) *httpResult {
	t.Helper()
	rec := do(t, h, "POST", "/api/blocklists", addRequest{Domains: domains})
	var body map[string]any
	json.NewDecoder(rec.Body).Decode(&body)
	return &httpResult{code: rec.Code, body: body}
}

type httpResult struct {
	code int
	body map[string]any
}

func (r *httpResult) err() string {
	s, _ := r.body["error"].(string)
	return s
}

// The point of the feature: a site no preset covers becomes blocked, everywhere,
// immediately.
func TestAddingACustomSiteEnforcesItImmediately(t *testing.T) {
	h, _ := sessionServer(t)

	if got := add(t, h, "https://www.example-forum.com/hot"); got.code != http.StatusOK {
		t.Fatalf("add returned %d: %v", got.code, got.body)
	}

	list := custom(t, h)
	if !slices.Contains(list.Domains, "example-forum.com") {
		t.Fatalf("stored %v — a pasted URL must be normalised to its domain", list.Domains)
	}
	if !list.Enabled {
		t.Fatal("adding must enable the list; a site you just typed should not need a second switch")
	}

	// Reaching the union is what makes it real, and attribution proves it arrived
	// as a baseline rule rather than some fourth kind of source.
	if got := getState(t, h).Effective.Attribution[blocklist.CustomListID]; got != "baseline" {
		t.Fatalf("custom list attributed to %q, want baseline", got)
	}
}

// The extension is the only layer that can see a URL path, so this is also the
// check that a custom domain reaches the browser.
func TestCustomDomainsReachTheExtension(t *testing.T) {
	h, _ := sessionServer(t)
	add(t, h, "example-forum.com")

	got, _ := getRules(t, h, "")
	if !slices.Contains(got.Domains, "example-forum.com") {
		t.Fatalf("custom domain missing from /api/rules: %v", got.Domains)
	}
	if !got.Enforcing {
		t.Fatal("enforcing should be true")
	}
}

// Strengthening is always allowed, mid-session included — the same rule that
// lets a baseline category be switched on during a session.
func TestAddingIsAllowedDuringASession(t *testing.T) {
	h, c := sessionServer(t)
	do(t, h, "POST", "/api/session", commitRequest{DurationMinutes: 30})
	c.tick(session.DefaultGrace + time.Second)

	if got := add(t, h, "example-forum.com"); got.code != http.StatusOK {
		t.Fatalf("add during FOCUS returned %d: %v", got.code, got.body)
	}
	if !slices.Contains(custom(t, h).Domains, "example-forum.com") {
		t.Fatal("not stored")
	}
}

// The asymmetry, applied to content rather than categories. Without this the
// list is an off switch with extra steps: add the site you actually struggle
// with, then delete it the moment you want it back.
func TestRemovingIsRefusedWhileTheListIsEnforced(t *testing.T) {
	h, c := sessionServer(t)
	add(t, h, "example-forum.com")

	rec := do(t, h, "DELETE", "/api/blocklists/example-forum.com", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("remove returned %d, want 409", rec.Code)
	}
	var body map[string]string
	json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "would_weaken" {
		t.Fatalf("error %q", body["error"])
	}
	if !slices.Contains(custom(t, h).Domains, "example-forum.com") {
		t.Fatal("a refused removal must not remove anything")
	}

	// The way out is the ordinary one: turn the row off, which takes fifteen
	// minutes and keeps the site blocked for every one of them.
	if code := do(t, h, "POST", "/api/baseline/"+blocklist.CustomListID+"/disable", nil).Code; code != http.StatusOK {
		t.Fatalf("disable returned %d", code)
	}
	c.tick(14 * time.Minute)
	if do(t, h, "DELETE", "/api/blocklists/example-forum.com", nil).Code != http.StatusConflict {
		t.Fatal("still enforced at 14:00, so removal must still be refused")
	}

	c.tick(2 * time.Minute)
	if code := do(t, h, "DELETE", "/api/blocklists/example-forum.com", nil).Code; code != http.StatusOK {
		t.Fatalf("remove after the delay returned %d", code)
	}
	if len(custom(t, h).Domains) != 0 {
		t.Fatal("still listed after a permitted removal")
	}
}

// A crisis line must not be blockable by any list, including one the user wrote.
func TestTheAllowlistIsRefusedWithAReasonNotSilentlyDropped(t *testing.T) {
	h, _ := sessionServer(t)

	got := add(t, h, "988lifeline.org")
	if got.code != http.StatusBadRequest || got.err() != "allowlisted" {
		t.Fatalf("returned %d %v — this must fail loudly, not sit in the list doing nothing",
			got.code, got.body)
	}
	if len(custom(t, h).Domains) != 0 {
		t.Fatal("allowlisted domain was stored")
	}
}

func TestMalformedEntriesAreRejectedWithTheOffendingText(t *testing.T) {
	h, _ := sessionServer(t)

	got := add(t, h, "not a domain")
	if got.code != http.StatusBadRequest || got.err() != "not_a_domain" {
		t.Fatalf("returned %d %v", got.code, got.body)
	}
	if detail, _ := got.body["detail"].(string); detail == "" {
		t.Fatal("no detail — pasting twenty lines and being told 'invalid' is not a fix you can act on")
	}
}

// All-or-nothing: reporting "3 of 5 added" on a screen whose job is telling you
// what is enforced is worse than refusing the batch.
func TestABadEntryAddsNothingFromTheBatch(t *testing.T) {
	h, _ := sessionServer(t)

	if got := add(t, h, "good-one.com", "!!!broken", "good-two.com"); got.code != http.StatusBadRequest {
		t.Fatalf("returned %d", got.code)
	}
	if got := custom(t, h).Domains; len(got) != 0 {
		t.Fatalf("partial write: %v", got)
	}
}

func TestDuplicatesAndMultiAddAreHandled(t *testing.T) {
	h, _ := sessionServer(t)
	add(t, h, "a-site.com", "b-site.com")
	add(t, h, "https://www.a-site.com/somewhere") // same domain, different spelling

	got := custom(t, h).Domains
	if len(got) != 2 {
		t.Fatalf("got %v, want two entries with no duplicate", got)
	}
	if !slices.Equal(got, []string{"a-site.com", "b-site.com"}) {
		t.Fatalf("got %v, want sorted", got)
	}
}

// The list is a signed row like everything else, and a disable request that
// forgets its countdown across a restart is not friction.
func TestCustomListSurvivesADaemonRestart(t *testing.T) {
	h, _, reopen := sessionServerReopenable(t)
	add(t, h, "example-forum.com")

	h2 := reopen()
	list := custom(t, h2)
	if !slices.Contains(list.Domains, "example-forum.com") {
		t.Fatalf("lost across restart: %v", list.Domains)
	}
	if !list.Enabled {
		t.Fatal("the rule carrying the list did not survive")
	}
	if got := getState(t, h2).Effective.Attribution[blocklist.CustomListID]; got != "baseline" {
		t.Fatalf("restarted daemon did not re-apply the custom list, attribution %q", got)
	}
}
