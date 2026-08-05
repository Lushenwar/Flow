package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
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

	var rest []store.Event
	json.NewDecoder(get(t, h, "/api/events?since="+strconv.FormatInt(first, 10), "secret").Body).Decode(&rest)
	if len(rest) != 1 || rest[0].Kind != "service_stop" {
		t.Fatalf("since filter wrong: %+v", rest)
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

