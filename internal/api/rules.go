package api

import (
	"hash/fnv"
	"net/http"
	"strings"

	"github.com/Lushenwar/Flow/internal/blocklist"
)

// rulesResponse is what the browser extension polls.
type rulesResponse struct {
	Enforcing bool                `json:"enforcing"`
	Domains   []string            `json:"domains"`
	AllowPaths map[string][]string `json:"allowPaths"`
	// Version changes whenever the effective set does, so the extension can skip
	// rebuilding its rules on an unchanged poll.
	Version uint64 `json:"version"`
}

// rules is deliberately unauthenticated.
//
// An extension cannot read %ProgramData%\Mast\token — it lives in the browser
// sandbox — so requiring the bearer token here would mean pasting a secret into
// an options page for no gain. The endpoint is safe to leave open because it is
// bound to loopback, is read-only, and reveals only which categories are
// blocked. It grants no authority: the same reasoning that makes the token file
// user-readable in the first place.
func (s *Server) rules(w http.ResponseWriter, r *http.Request) {
	eff := s.sess.Effective()
	domains := eff.SortedDomains()

	resp := rulesResponse{
		Enforcing:  len(domains) > 0,
		Domains:    domains,
		AllowPaths: blocklist.Presets().ResolvePaths(eff.SortedLists()),
		Version:    version(domains),
	}
	if resp.AllowPaths == nil {
		resp.AllowPaths = map[string][]string{}
	}
	writeJSON(w, http.StatusOK, resp)
}

func version(domains []string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(strings.Join(domains, "\x1f")))
	return h.Sum64()
}
