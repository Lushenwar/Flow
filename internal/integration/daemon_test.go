//go:build integration

// Package integration drives a real daemon over HTTP.
//
// Every layer has unit tests with a fake underneath. Nothing tested the stack:
// api.Listen, the token file, the port file, the store on disk, and the manager
// all working together, on the real clock. Build-tagged because it binds a real
// port and waits out a real grace window, which is not something
// `go test ./...` should do by accident.
//
//	go test -tags integration ./internal/integration/
package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Lushenwar/Flow/internal/api"
	"github.com/Lushenwar/Flow/internal/blocklist"
	"github.com/Lushenwar/Flow/internal/enforce"
	"github.com/Lushenwar/Flow/internal/session"
	"github.com/Lushenwar/Flow/internal/store"
)

// noopEnforcer stands in for the OS layers. Everything above them is real.
type noopEnforcer struct{ last enforce.Effective }

func (n *noopEnforcer) Set(e enforce.Effective) { n.last = e }

type fakeEnf struct{}

func (fakeEnf) Status() map[string]string { return map[string]string{"dns": "active"} }
func (fakeEnf) LastReconcile() time.Time  { return time.Now() }

type daemon struct {
	base  string
	token string
	dir   string
	stop  func()
}

func start(t *testing.T, dir string) *daemon {
	t.Helper()

	st, err := store.Open(filepath.Join(dir, "state.db"), filepath.Join(dir, "key.bin"))
	if err != nil {
		t.Fatal(err)
	}
	token, err := api.LoadOrCreateToken(filepath.Join(dir, "token"))
	if err != nil {
		t.Fatal(err)
	}
	ln, err := api.Listen(0, filepath.Join(dir, "port"))
	if err != nil {
		t.Fatal(err)
	}

	mgr := session.NewManager(st, session.SystemClock{}, &noopEnforcer{},
		blocklist.Presets(), []string{"preset.adult"})
	if err := mgr.Load(); err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: api.New(st, token, false, fakeEnf{}, mgr).Handler()}
	go srv.Serve(ln)

	stopped := false
	d := &daemon{
		base:  "http://" + ln.Addr().String(),
		token: token,
		dir:   dir,
	}
	d.stop = func() {
		if stopped {
			return
		}
		stopped = true
		srv.Close()
		st.Close()
	}
	t.Cleanup(d.stop)
	return d
}

func (d *daemon) do(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req, err := http.NewRequest(method, d.base+path, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+d.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

type stateView struct {
	Session struct {
		State            string `json:"state"`
		RemainingSeconds int    `json:"remainingSeconds"`
		CanRelease       bool   `json:"canRelease"`
	} `json:"session"`
	Effective struct {
		Attribution map[string]string `json:"attribution"`
	} `json:"effective"`
}

func (d *daemon) state(t *testing.T) stateView {
	t.Helper()
	code, body := d.do(t, "GET", "/api/state", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /api/state returned %d: %s", code, body)
	}
	var out stateView
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// waitFor polls until the session reaches one of the given states.
func (d *daemon) waitFor(t *testing.T, within time.Duration, states ...string) string {
	t.Helper()
	deadline := time.Now().Add(within)
	var last string
	for time.Now().Before(deadline) {
		last = d.state(t).Session.State
		for _, s := range states {
			if last == s {
				return last
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("still %s after %v, wanted one of %v", last, within, states)
	return last
}

// The whole stack, on the real clock, across a real process restart.
func TestSessionSurvivesTheDaemonBeingReplaced(t *testing.T) {
	dir := t.TempDir()
	d := start(t, dir)

	// The port file is what every other client reads to find us.
	raw, err := os.ReadFile(filepath.Join(dir, "port"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || port == 0 {
		t.Fatalf("port file says %q", raw)
	}
	if !strings.HasSuffix(d.base, strconv.Itoa(port)) {
		t.Fatalf("port file %d does not match the listener at %s", port, d.base)
	}

	// Baseline is enforced before anything is committed.
	if got := d.state(t).Effective.Attribution["preset.adult"]; got != "baseline" {
		t.Fatalf("IDLE reported %q; not focusing never means nothing is enforced", got)
	}

	if code, body := d.do(t, "POST", "/api/session", map[string]any{
		"durationMinutes": 30,
		"blocklistIds":    []string{"preset.video"},
	}); code != http.StatusOK {
		t.Fatalf("commit returned %d: %s", code, body)
	}

	st := d.state(t)
	if st.Session.State != "ARMING" {
		t.Fatalf("state %s, want ARMING", st.Session.State)
	}
	// Blocks are live during the grace window.
	if st.Effective.Attribution["preset.video"] != "session" {
		t.Fatal("blocks apply at commit, not at grace-end")
	}

	// Wait out the real grace window rather than faking it: this test exists
	// precisely to exercise the clock nothing else uses.
	d.waitFor(t, 30*time.Second, "FOCUS")

	// No stop verb.
	if code, _ := d.do(t, "DELETE", "/api/session", nil); code != http.StatusConflict {
		t.Fatalf("DELETE in FOCUS returned %d, want 409", code)
	}
	before := d.state(t).Session.RemainingSeconds

	// taskkill /F, then a new process against the same store.
	d.stop()
	d2 := start(t, dir)

	after := d2.state(t)
	if after.Session.State != "FOCUS" {
		t.Fatalf("state %s after restart, want FOCUS", after.Session.State)
	}
	if diff := before - after.Session.RemainingSeconds; diff < 0 || diff > 30 {
		t.Fatalf("remaining jumped %ds across a restart", diff)
	}
	if after.Effective.Attribution["preset.video"] != "session" {
		t.Fatal("a restarted daemon must re-apply the session's blocks")
	}
	if after.Session.CanRelease {
		t.Fatal("canRelease must still be false")
	}
}

// The escape hatch always exists, and the delay is real.
func TestEscapeRequiresTheDelayAndTheChallenge(t *testing.T) {
	dir := t.TempDir()
	d := start(t, dir)

	d.do(t, "POST", "/api/session", map[string]any{"durationMinutes": 30})
	d.waitFor(t, 30*time.Second, "FOCUS")

	if code, body := d.do(t, "POST", "/api/session/escape", nil); code != http.StatusOK {
		t.Fatalf("escape returned %d: %s; it is the line between a tool and a trap", code, body)
	}
	if got := d.state(t).Session.State; got != "RELEASING" {
		t.Fatalf("state %s, want RELEASING", got)
	}

	// The challenge cannot be fetched early and pre-typed.
	if code, _ := d.do(t, "GET", "/api/session/escape/challenge", nil); code != http.StatusConflict {
		t.Fatal("the challenge must not be available before the delay elapses")
	}
	// And the session is still locked throughout.
	if code, _ := d.do(t, "DELETE", "/api/session", nil); code != http.StatusConflict {
		t.Fatal("RELEASING is not a stop button")
	}
	if d.state(t).Effective.Attribution["preset.adult"] != "baseline" {
		t.Fatal("baseline lost during RELEASING")
	}
}

// /api/rules is the one open route: readable by an extension, not by a web page.
func TestRulesIsReachableWithoutATokenAndNotByAWebPage(t *testing.T) {
	dir := t.TempDir()
	d := start(t, dir)

	get := func(origin string) *http.Response {
		req, _ := http.NewRequest("GET", d.base+"/api/rules", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}

	if resp := get(""); resp.StatusCode != http.StatusOK {
		t.Fatalf("unauthenticated /api/rules returned %d", resp.StatusCode)
	}
	if got := get("https://evil.example").Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("a web page origin got CORS %q", got)
	}
	const ext = "chrome-extension://abcdefghijklmnop"
	if got := get(ext).Header.Get("Access-Control-Allow-Origin"); got != ext {
		t.Fatalf("the extension got CORS %q", got)
	}

	// Everything else still needs the token.
	req, _ := http.NewRequest("GET", d.base+"/api/state", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/api/state without a token returned %d", resp.StatusCode)
	}
}

// api.Listen writes the port file every client reads.
func TestListenerAndPortFileAgree(t *testing.T) {
	dir := t.TempDir()
	start(t, dir)

	raw, err := os.ReadFile(filepath.Join(dir, "port"))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strings.TrimSpace(string(raw)), 2*time.Second)
	if err != nil {
		t.Fatalf("nothing listening on the port we advertised: %v", err)
	}
	conn.Close()
}
