package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// stubDaemon stands up a fake flowd and points the data directory at it.
func stubDaemon(t *testing.T, token string, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FLOW_DATA_DIR", dir)

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	port := srv.Listener.Addr().(*net.TCPAddr).Port
	write(t, filepath.Join(dir, "port"), strconv.Itoa(port))
	if token != "" {
		// Trailing newline on purpose: the real file has one, and a bearer token
		// with a newline in it is a 401 that is very hard to see.
		write(t, filepath.Join(dir, "token"), token+"\n")
	}
	return srv
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The token never reaches JavaScript; it is attached here or the request is
// anonymous and the daemon answers 401.
func TestProxyAttachesTheTokenAndForwardsThePath(t *testing.T) {
	var gotAuth, gotPath string
	stubDaemon(t, "s3cret", func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		w.Write([]byte(`{"state":"IDLE"}`))
	})

	rec := httptest.NewRecorder()
	daemonProxy().ServeHTTP(rec, httptest.NewRequest("GET", "/api/state", nil))

	if gotAuth != "Bearer s3cret" {
		t.Fatalf("Authorization was %q — a trailing newline here is a 401 nobody can see", gotAuth)
	}
	if gotPath != "/api/state" {
		t.Fatalf("proxied to %q", gotPath)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "IDLE") {
		t.Fatalf("body not passed through: %q", rec.Body.String())
	}
}

// The daemon is a service an elevated user can stop at any time. That is a
// normal state, and the UI already knows how to render it — but only if it gets
// JSON back rather than a proxy error page.
func TestDeadDaemonAnswersJSONNotAnErrorPage(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FLOW_DATA_DIR", dir)

	// A port nobody is listening on.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	write(t, filepath.Join(dir, "port"), strconv.Itoa(closed))

	rec := httptest.NewRecorder()
	daemonProxy().ServeHTTP(rec, httptest.NewRequest("GET", "/api/state", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type %q — the client parses this", ct)
	}
	if !strings.Contains(rec.Body.String(), "daemon_unreachable") {
		t.Fatalf("body %q", rec.Body.String())
	}
}

// api.Listen falls back to an ephemeral port when 8787 is taken, so the file is
// the only always-correct source. Assuming 8787 is the bug the extension has.
func TestPortComesFromTheFileWithASaneFallback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FLOW_DATA_DIR", dir)

	if got := daemonPort(); got != "8787" {
		t.Fatalf("no port file should fall back to 8787, got %q", got)
	}

	write(t, filepath.Join(dir, "port"), "49152\n")
	if got := daemonPort(); got != "49152" {
		t.Fatalf("port file ignored, got %q", got)
	}

	// A truncated or corrupt file must not produce a nonsense host.
	write(t, filepath.Join(dir, "port"), "not-a-port")
	if got := daemonPort(); got != "8787" {
		t.Fatalf("garbage port accepted: %q", got)
	}
}
