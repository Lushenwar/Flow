// Package api is the loopback HTTP surface. Bind is 127.0.0.1 only and every
// route requires the bearer token.
package api

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
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
}

// Enforcement is the slice of the enforcer health needs. An interface so the API
// tests do not have to stand up real WFP filters.
type Enforcement interface {
	Status() map[string]string
	LastReconcile() time.Time
}

func New(st *store.Store, token string, dev bool, enf Enforcement, sess Sessions) *Server {
	return &Server{st: st, token: token, dev: dev, enf: enf, sess: sess, started: time.Now()}
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

		mux.HandleFunc("GET /api/bank", s.bank)
		mux.HandleFunc("POST /api/bank/spend", s.spend)
		mux.HandleFunc("GET /api/schedules", s.schedules)
		mux.HandleFunc("POST /api/schedules", s.putSchedule)
		mux.HandleFunc("PUT /api/schedules/{id}", s.putSchedule)
	}

	// /api/rules sits outside the token check. See the comment on rules() — the
	// extension cannot read the token file, and the endpoint grants no authority.
	outer := http.NewServeMux()
	outer.Handle("/", s.devCORS(s.auth(mux)))
	if s.sess != nil {
		outer.HandleFunc("GET /api/rules", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			s.rules(w, r)
		})
	}
	return outer
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
	bad, err := s.st.Verify()
	switch {
	case err != nil:
		h.Status, h.Signature = "degraded", "unreadable: "+err.Error()
	case len(bad) > 0:
		h.Status, h.Signature, h.BadRows = "degraded", "tampered", bad
	}
	writeJSON(w, http.StatusOK, h)
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	var since int64
	fmt.Sscanf(r.URL.Query().Get("since"), "%d", &since)
	evs, err := s.st.Events(since)
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

// Listen binds loopback on the requested port, falling back to an ephemeral one
// if it is taken, and records the result so the UI can find the daemon.
func Listen(port int, portFile string) (net.Listener, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		if ln, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			return nil, err
		}
	}
	actual := ln.Addr().(*net.TCPAddr).Port
	if err := os.WriteFile(portFile, []byte(fmt.Sprint(actual)), 0o644); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}
