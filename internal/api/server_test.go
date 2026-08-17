package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Lushenwar/Flow/internal/store"
)

func newServer(t *testing.T) (http.Handler, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	st, err := store.Open(dbPath, filepath.Join(dir, "key.bin"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st, "secret", true, fakeEnforcement{}, nil).Handler(), st, dbPath
}

// fakeEnforcement stands in for the real enforcer so API tests do not need WFP.
type fakeEnforcement struct {
	status map[string]string
	last   time.Time
}

func (f fakeEnforcement) Status() map[string]string {
	if f.status == nil {
		return map[string]string{"hosts": "active", "dns": "active"}
	}
	return f.status
}
func (f fakeEnforcement) LastReconcile() time.Time { return f.last }

func get(t *testing.T, h http.Handler, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuthRequired(t *testing.T) {
	h, _, _ := newServer(t)
	for _, tok := range []string{"", "wrong", "secre"} {
		if code := get(t, h, "/api/health", tok).Code; code != http.StatusUnauthorized {
			t.Fatalf("token %q got %d, want 401", tok, code)
		}
	}
	if code := get(t, h, "/api/health", "secret").Code; code != http.StatusOK {
		t.Fatalf("valid token got %d", code)
	}
}

func TestHealthReportsTamper(t *testing.T) {
	h, st, dbPath := newServer(t)
	if err := st.Put("targetAt", "2026-08-04T14:52:11Z"); err != nil {
		t.Fatal(err)
	}

	var before health
	json.NewDecoder(get(t, h, "/api/health", "secret").Body).Decode(&before)
	if before.Status != "ok" || before.Signature != "ok" {
		t.Fatalf("clean store should be ok: %+v", before)
	}

	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`UPDATE kv SET v='1970-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	var after health
	json.NewDecoder(get(t, h, "/api/health", "secret").Body).Decode(&after)
	if after.Status != "degraded" || after.Signature != "tampered" {
		t.Fatalf("edited store should be degraded: %+v", after)
	}
	if len(after.BadRows) != 1 {
		t.Fatalf("want the bad row named, got %v", after.BadRows)
	}
}

func TestEventsSince(t *testing.T) {
	h, st, _ := newServer(t)
	first, _ := st.Append("service_start", "{}")
	st.Append("service_stop", "{}")

	var all []store.Event
	json.NewDecoder(get(t, h, "/api/events", "secret").Body).Decode(&all)
	if len(all) != 2 {
		t.Fatalf("want 2 events, got %d", len(all))
	}
	// Newest first: the limit is applied in SQL, so the ordering is what decides
	// which end a LIMIT keeps.
	if all[0].Kind != "service_stop" {
		t.Fatalf("want newest first, got %+v", all)
	}

	var rest []store.Event
	json.NewDecoder(get(t, h, "/api/events?since="+strconv.FormatInt(first, 10), "secret").Body).Decode(&rest)
	if len(rest) != 1 || rest[0].Kind != "service_stop" {
		t.Fatalf("since filter wrong: %+v", rest)
	}
}

// The history list renders eight rows. This used to return the entire log,
// HMAC-verified row by row, every five seconds, to draw them.
func TestEventsAreBoundedAndCannotBeAskedForInFull(t *testing.T) {
	h, st, _ := newServer(t)
	for i := 0; i < store.DefaultEventLimit+50; i++ {
		if _, err := st.Append("session_commit", "{}"); err != nil {
			t.Fatal(err)
		}
	}

	var unasked []store.Event
	json.NewDecoder(get(t, h, "/api/events", "secret").Body).Decode(&unasked)
	if len(unasked) != store.DefaultEventLimit {
		t.Fatalf("unbounded read returned %d rows, want the %d cap",
			len(unasked), store.DefaultEventLimit)
	}

	var small []store.Event
	json.NewDecoder(get(t, h, "/api/events?limit=5", "secret").Body).Decode(&small)
	if len(small) != 5 {
		t.Fatalf("limit=5 returned %d", len(small))
	}

	// A caller must not be able to talk its way past the cap.
	var greedy []store.Event
	json.NewDecoder(get(t, h, "/api/events?limit=100000", "secret").Body).Decode(&greedy)
	if len(greedy) > store.DefaultEventLimit {
		t.Fatalf("limit=100000 returned %d rows — the cap is not a cap", len(greedy))
	}
}

func TestHealthReportsLayerFailures(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "state.db"), filepath.Join(dir, "key.bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	enf := fakeEnforcement{
		status: map[string]string{"hosts": "active", "wfp": "error: needs elevation"},
		last:   time.Now(),
	}
	h := New(st, "secret", false, enf, nil).Handler()

	var got health
	json.NewDecoder(get(t, h, "/api/health", "secret").Body).Decode(&got)
	if got.Status != "degraded" {
		t.Fatalf("a failed layer must degrade health, got %q", got.Status)
	}
	if got.Layers["wfp"] != "error: needs elevation" {
		t.Fatalf("layer detail lost: %v", got.Layers)
	}
	if got.Reconcile == "never" {
		t.Fatal("last reconcile not reported")
	}
}

// The dev UI runs on localhost:3000 and the daemon on 127.0.0.1:8787 — a
// cross-origin pair. Without these headers the browser drops every response and
// the app sits on "Loading…" with an empty console.
func TestDevCORS(t *testing.T) {
	h, _, _ := newServer(t) // dev: true
	if got := get(t, h, "/api/health", "secret").Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("dev build must allow the origin, got %q", got)
	}

	// A preflight carries no Authorization header, so answering it through the
	// token check would 401 and the real request would never be sent.
	req := httptest.NewRequest("OPTIONS", "/api/state", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight got %d, want 204", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Fatalf("preflight must permit the bearer header, got %q",
			rec.Header().Get("Access-Control-Allow-Headers"))
	}
}

// Release builds are same-origin, so the header would only be widening the
// surface for nothing.
func TestReleaseHasNoCORS(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "state.db"), filepath.Join(dir, "key.bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	h := New(st, "secret", false, fakeEnforcement{}, nil).Handler()
	if got := get(t, h, "/api/health", "secret").Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("release build set CORS to %q", got)
	}
}

// Listen used to fall back to an ephemeral port when the requested one was
// taken. proxy.go and flowctl read the port file and were fine; the extension
// cannot read files, kept polling 8787, and silently stopped enforcing while the
// daemon went on reporting itself healthy. A bounded walk keeps it findable.
func TestListenWalksForwardWhenThePortIsTaken(t *testing.T) {
	dir := t.TempDir()
	portFile := filepath.Join(dir, "port")

	base, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	taken := base.Addr().(*net.TCPAddr).Port

	ln, err := Listen(taken, portFile)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	got := ln.Addr().(*net.TCPAddr).Port
	if got == taken {
		t.Fatal("bound a port that was already held")
	}
	if got < taken || got >= taken+PortSearch {
		t.Fatalf("landed on %d, outside %d-%d — the extension only probes that range",
			got, taken, taken+PortSearch-1)
	}

	// The port file still has to be right, because it is what every other
	// client reads.
	b, err := os.ReadFile(portFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(b)) != strconv.Itoa(got) {
		t.Fatalf("port file says %q, listening on %d", b, got)
	}
}

// A machine that cannot bind anything in range is a broken install. Erroring is
// better than an ephemeral port nothing can find.
func TestListenFailsRatherThanHidingOnAnEphemeralPort(t *testing.T) {
	dir := t.TempDir()

	start, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer start.Close()
	base := start.Addr().(*net.TCPAddr).Port

	// Hold the whole search range.
	for p := base + 1; p < base+PortSearch; p++ {
		held, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			t.Skipf("could not stage a full range: %v", err)
		}
		defer held.Close()
	}

	ln, err := Listen(base, filepath.Join(dir, "port"))
	if err == nil {
		ln.Close()
		t.Fatal("bound something outside the range the extension can find")
	}
	if !strings.Contains(err.Error(), "no free port") {
		t.Fatalf("unhelpful error: %v", err)
	}
}

func TestTokenFileRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "token")
	tok, err := LoadOrCreateToken(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 64 {
		t.Fatalf("want 32 random bytes hex, got %d chars", len(tok))
	}
	again, err := LoadOrCreateToken(p)
	if err != nil {
		t.Fatal(err)
	}
	if again != tok {
		t.Fatal("token must be stable across restarts or every UI reconnect breaks")
	}
}

