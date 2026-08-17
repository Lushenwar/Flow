package main

import (
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"
	"strings"

	"github.com/Lushenwar/Flow/internal/paths"
)

// daemonProxy forwards /api/* from the webview to flowd, attaching the bearer
// token on the way through.
//
// Two problems, one answer. Wails serves the page from wails.localhost, so a
// direct fetch to 127.0.0.1 is cross-origin — and the daemon only sends CORS
// headers in dev builds, by design, on the assumption that a release UI is
// same-origin. Proxying makes that assumption true again instead of widening the
// daemon to make it false.
//
// The token never reaches JavaScript as a result, which is strictly better than
// the browser build managed: there, it has to be compiled into the bundle.
func daemonProxy() http.Handler {
	return &httputil.ReverseProxy{
		Director: func(r *http.Request) {
			host := "127.0.0.1:" + daemonPort()
			r.URL.Scheme = "http"
			r.URL.Host = host
			r.Host = host
			if tok := readTrimmed(paths.Token()); tok != "" {
				r.Header.Set("Authorization", "Bearer "+tok)
			}
		},
		// The daemon being down is a normal state, not a crash: it is a service
		// that can be stopped by an elevated user at any time. The UI already
		// knows how to render "can't reach the daemon", so hand it something it
		// can parse rather than a proxy error page.
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error":"daemon_unreachable"}`))
		},
	}
}

// daemonPort prefers the port file the daemon writes, because api.Listen falls
// back to an ephemeral port when 8787 is taken. Assuming 8787 is how the browser
// extension silently stops blocking; the file is the only always-correct source.
func daemonPort() string {
	if p := readTrimmed(paths.Port()); p != "" {
		if _, err := strconv.Atoi(p); err == nil {
			return p
		}
	}
	return "8787"
}

// readTrimmed is quiet on failure: it runs on every proxied request, and a
// missing file just means the daemon has not started yet.
func readTrimmed(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
