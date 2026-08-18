// Package api is the loopback HTTP surface. Bind is 127.0.0.1 only and every
// route requires the bearer token.
package api

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Lushenwar/Flow/internal/store"
)

type Server struct {
	st      *store.Store
	token   string
	dev     bool
	enf     Enforcement
	sess    Sessions
	started time.Time

	// Verify walks and re-HMACs every signed row, which is exactly what
	// `flowctl verify` is for and exactly wrong on a route named "health".
	// Cached so a polling caller cannot make the cost of answering scale with
	// the size of the history.
	sigMu  sync.Mutex
	sigAt  time.Time
	sigBad []string
	sigErr error
	// sigTTL is how stale a cached verdict may be. A field rather than a
	// constant so tests can demand a fresh answer; nothing else changes it.
	sigTTL time.Duration
}

// signatureTTL is how stale a cached verdict may be. Tampering is not a
// millisecond-scale event, and flowctl verify is always available for an
// authoritative answer.
const signatureTTL = 2 * time.Minute

// Enforcement is the slice of the enforcer health needs. An interface so the API
// tests do not have to stand up real WFP filters.
type Enforcement interface {
	Status() map[string]string
	LastReconcile() time.Time
}

func New(st *store.Store, token string, dev bool, enf Enforcement, sess Sessions) *Server {
	return &Server{
		st: st, token: token, dev: dev, enf: enf, sess: sess,
		started: time.Now(), sigTTL: signatureTTL,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/events", s.events)
	if s.sess != nil {
		mux.HandleFunc("GET /api/state", s.state)
		mux.HandleFunc("POST /api/session", s.commit)
		// No stop verb exists. DELETE is valid only in ARMING.
		mux.HandleFunc("DELETE /api/session", s.abort)
		mux.HandleFunc("POST /api/session/escape", s.escape)
		mux.HandleFunc("GET /api/session/escape/challenge", s.challenge)
		mux.HandleFunc("POST /api/session/escape/verify", s.verify)
		mux.HandleFunc("POST /api/session/ack", s.ack)

		mux.HandleFunc("GET /api/baseline", s.baselineList)
		mux.HandleFunc("POST /api/baseline/{id}/enable", s.baselineEnable)
		mux.HandleFunc("POST /api/baseline/{id}/disable", s.baselineDisable)
		mux.HandleFunc("DELETE /api/baseline/{id}/disable", s.baselineCancelDisable)

		mux.HandleFunc("GET /api/blocklists", s.customList)
		mux.HandleFunc("POST /api/blocklists", s.customAdd)
		mux.HandleFunc("DELETE /api/blocklists/{domain}", s.customRemove)

		mux.HandleFunc("GET /api/allowlist", s.allowList)
		mux.HandleFunc("POST /api/allowlist", s.allowAdd)
		mux.HandleFunc("DELETE /api/allowlist/{domain}", s.allowRemove)

		mux.HandleFunc("GET /api/session/lists", s.sessionLists)
		mux.HandleFunc("PUT /api/session/lists", s.putSessionLists)

		mux.HandleFunc("GET /api/bank", s.bank)
		mux.HandleFunc("POST /api/bank/spend", s.spend)
		mux.HandleFunc("GET /api/schedules", s.schedules)
		mux.HandleFunc("POST /api/schedules", s.putSchedule)
		mux.HandleFunc("PUT /api/schedules/{id}", s.putSchedule)
		mux.HandleFunc("DELETE /api/schedules/{id}", s.deleteSchedule)
	}

	// /api/rules sits outside the token check. See the comment on rules() — the
	// extension cannot read the token file, and the endpoint grants no authority.
	outer := http.NewServeMux()
	outer.Handle("/", s.devCORS(s.auth(mux)))
	if s.sess != nil {
		outer.HandleFunc("GET /api/rules", func(w http.ResponseWriter, r *http.Request) {
			// Echo the origin only for extension origins.
			//
			// This was "*", which meant any web page you visited could fetch
			// http://127.0.0.1:8787/api/rules and READ the response — not merely
			// probe for it. That tells an arbitrary site you run Flow and whether
			// you block adult content, gambling, or your own named list of
			// domains. Not an authority leak; a privacy one, and the categories
			// involved are about as sensitive as categories get.
			//
			// The endpoint stays unauthenticated, because the reasoning for that
			// has not changed: an extension lives in the browser sandbox and
			// cannot read %ProgramData%\Flow	oken. It just stops being
			// universally readable. A page can still detect that something
			// answers on this port; it can no longer read what.
			if o := r.Header.Get("Origin"); isExtensionOrigin(o) {
				w.Header().Set("Access-Control-Allow-Origin", o)
				w.Header().Set("Vary", "Origin")
			}
			s.rules(w, r)
		})
	}
	return outer
}

// isExtensionOrigin reports whether an Origin belongs to a browser extension.
//
// Chrome and Edge use chrome-extension://, Firefox moz-extension://. A page
// cannot forge an Origin header — the browser sets it — so this is the whole
// check rather than the start of one.
func isExtensionOrigin(o string) bool {
	return strings.HasPrefix(o, "chrome-extension://") ||
		strings.HasPrefix(o, "moz-extension://")
}

// devCORS lets `npm run dev` on localhost:3000 talk to the daemon. Without it
// the browser blocks every call and the UI never leaves "Loading…", which is
// what the documented dev loop in CLAUDE.md actually did.
//
// Dev builds only, and it widens nothing: the token still gates every route, a
// page cannot read %ProgramData%\Flow\token, and no verb here ends a locked
// session. In release the UI is same-origin and this is off.
func (s *Server) devCORS(next http.Handler) http.Handler {
	if !s.dev {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, PUT")
		// Preflights carry no Authorization header, so they must answer before
		// the token check rather than through it.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// auth rejects anything without the exact bearer token, in constant time.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

type health struct {
	Status    string            `json:"status"`
	App       string            `json:"app"`
	Dev       bool              `json:"dev"`
	UptimeSec int64             `json:"uptimeSeconds"`
	Signature string            `json:"signature"`
	BadRows   []string          `json:"badRows,omitempty"`
	Layers    map[string]string `json:"layers"`
	Reconcile string            `json:"lastReconcile"`
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	h := health{
		Status:    "ok",
		App:       "flow",
		Dev:       s.dev,
		UptimeSec: int64(time.Since(s.started).Seconds()),
		Signature: "ok",
		Layers:    map[string]string{},
		Reconcile: "never",
	}
	if s.enf != nil {
		h.Layers = s.enf.Status()
		if t := s.enf.LastReconcile(); !t.IsZero() {
			h.Reconcile = t.UTC().Format(time.RFC3339)
		}
		for _, st := range h.Layers {
			if strings.HasPrefix(st, "error:") {
				h.Status = "degraded"
			}
		}
	}
	bad, err := s.signature()
	switch {
	case err != nil:
		h.Status, h.Signature = "degraded", "unreadable: "+err.Error()
	case len(bad) > 0:
		h.Status, h.Signature, h.BadRows = "degraded", "tampered", bad
	}
	writeJSON(w, http.StatusOK, h)
}

// events returns the newest first, bounded. The limit is capped rather than
// honoured blindly: the response costs an HMAC per row, and no caller has a
// reason to ask for the whole history over HTTP.
// signature returns the cached verify verdict, refreshing it when stale.
func (s *Server) signature() ([]string, error) {
	s.sigMu.Lock()
	defer s.sigMu.Unlock()

	if !s.sigAt.IsZero() && time.Since(s.sigAt) < s.sigTTL {
		return s.sigBad, s.sigErr
	}
	s.sigBad, s.sigErr = s.st.Verify()
	s.sigAt = time.Now()
	return s.sigBad, s.sigErr
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	var since int64
	fmt.Sscanf(r.URL.Query().Get("since"), "%d", &since)

	limit := store.DefaultEventLimit
	if q := r.URL.Query().Get("limit"); q != "" {
		var want int
		if _, err := fmt.Sscanf(q, "%d", &want); err == nil && want > 0 && want < limit {
			limit = want
		}
	}

	evs, err := s.st.Events(since, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if evs == nil {
		evs = []store.Event{}
	}
	writeJSON(w, http.StatusOK, evs)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// PortSearch is how many consecutive ports Listen will try before giving up.
//
// It exists for the browser extension, which is the one client that cannot be
// told where the daemon went. proxy.go and flowctl read %ProgramData%\Flow\port;
// an extension lives in the browser sandbox and cannot read files at all, so it
// probes a fixed range. Keep this in step with PORT_SEARCH in extension/background.js.
const PortSearch = 3

// Listen binds loopback on the requested port, walking forward a few ports if it
// is taken, and records the result so every client can find the daemon.
//
// It used to fall back to an ephemeral port, which was a silent failure with no
// symptom on the daemon side: the daemon worked, proxy.go and flowctl followed
// the port file, and the extension went on polling 8787 forever. URL-path
// granularity and warm-tab closing both stopped and nothing anywhere said so.
// That is the same shape as the MV3 deadlock in claude.md — the obvious health
// signal was the one thing still working.
//
// A bounded walk keeps the extension's search to a few probes. Exhausting it is
// an error rather than an ephemeral port, because a machine that cannot bind any
// of them is a broken install, not a state to degrade quietly into.
func Listen(port int, portFile string) (net.Listener, error) {
	var ln net.Listener
	var err error
	for try := port; try < port+PortSearch; try++ {
		ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", try))
		if err == nil {
			if try != port {
				// Degraded, not broken: say so. The extension can still find us
				// inside the search range, but only just.
				log.Printf("port %d was taken; listening on %d instead (the extension probes %d-%d)",
					port, try, port, port+PortSearch-1)
			}
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("no free port in %d-%d: %w", port, port+PortSearch-1, err)
	}

	actual := ln.Addr().(*net.TCPAddr).Port
	if err := os.WriteFile(portFile, []byte(fmt.Sprint(actual)), 0o644); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}
