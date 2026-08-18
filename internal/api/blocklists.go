package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Lushenwar/Flow/internal/blocklist"
	"github.com/Lushenwar/Flow/internal/session"
)

// The user's own blocklist: the sites the presets do not cover.
//
// Mutations are evaluated for DIRECTION, per the API spec — adding strengthens
// and is always allowed, removing weakens and is refused while the list is
// enforced. There is no verb here that unblocks anything immediately.
//
// Deviation from the spec's `DELETE /api/blocklists/{id}`: the id in the path is
// the domain, not a list id. There is exactly one user list, and addressing the
// list would leave no way to name the entry.

type customView struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Domains []string `json:"domains"`
	// Enabled mirrors the baseline rule that carries the list, so the UI can say
	// why a removal was refused without deriving it.
	Enabled bool `json:"enabled"`
	Max     int  `json:"max"`
}

func (s *Server) customList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.customView())
}

func (s *Server) customView() customView {
	domains := s.sess.Custom()
	if domains == nil {
		domains = []string{}
	}
	enabled := false
	for _, row := range s.sess.Baseline() {
		if row.ID == blocklist.CustomListID {
			enabled = row.Effective()
		}
	}
	return customView{
		ID:      blocklist.CustomListID,
		Name:    "Custom sites",
		Domains: domains,
		Enabled: enabled,
		Max:     blocklist.MaxCustomDomains,
	}
}

type addRequest struct {
	// Domains is what the UI splits out of the input box. Whole URLs are fine —
	// the daemon normalises and tells you what it stored.
	Domains []string `json:"domains"`
}

func (s *Server) customAdd(w http.ResponseWriter, r *http.Request) {
	var req addRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("bad_request"))
		return
	}
	if len(req.Domains) == 0 {
		writeJSON(w, http.StatusBadRequest, errBody("no_domains"))
		return
	}

	added, err := s.sess.AddCustom(req.Domains)
	switch {
	case errors.Is(err, blocklist.ErrAllowlisted):
		// Named separately from a malformed entry: "you cannot block this" and
		// "that is not a domain" are different problems with different fixes.
		writeJSON(w, http.StatusBadRequest, detailBody("allowlisted", err.Error()))
	case errors.Is(err, blocklist.ErrNotADomain):
		writeJSON(w, http.StatusBadRequest, detailBody("not_a_domain", err.Error()))
	case errors.Is(err, blocklist.ErrTooManyCustom):
		writeJSON(w, http.StatusBadRequest, errBody("too_many_domains"))
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
	default:
		if added == nil {
			added = []string{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"added": added,
			"list":  s.customView(),
		})
	}
}

func (s *Server) customRemove(w http.ResponseWriter, r *http.Request) {
	domain := strings.TrimSpace(r.PathValue("domain"))
	err := s.sess.RemoveCustom(domain)
	switch {
	case errors.Is(err, session.ErrWouldWeaken):
		writeJSON(w, http.StatusConflict, errBody("would_weaken"))
	case errors.Is(err, blocklist.ErrNotADomain):
		writeJSON(w, http.StatusBadRequest, errBody("not_a_domain"))
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
	default:
		writeJSON(w, http.StatusOK, s.customView())
	}
}

// detailBody carries the offending entry alongside the code. The generic message
// is useless when you pasted twenty lines and one of them is wrong.
func detailBody(code, detail string) map[string]string {
	return map[string]string{"error": code, "detail": detail}
}

// The user's allowlist, which only bites during a default-deny window.
//
// Direction is REVERSED from the blocklist and for the same reason: under
// default-deny, adding to the allowlist is what weakens enforcement. So adding
// is refused while a window is live and removing is always free — the exact
// mirror of custom.blocked, where adding is free and removing is refused.

type allowView struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Domains []string `json:"domains"`
	// Locked mirrors whether a default-deny rule is enforced right now, so the
	// UI can say why an addition was refused without deriving it.
	Locked bool `json:"locked"`
	Max    int  `json:"max"`
}

func (s *Server) allowList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.allowView())
}

func (s *Server) allowView() allowView {
	domains := s.sess.Allow()
	if domains == nil {
		domains = []string{}
	}
	return allowView{
		ID:      blocklist.AllowListID,
		Name:    "Always reachable",
		Domains: domains,
		Locked:  s.sess.Effective().DefaultDeny,
		Max:     blocklist.MaxCustomDomains,
	}
}

func (s *Server) allowAdd(w http.ResponseWriter, r *http.Request) {
	var req addRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("bad_request"))
		return
	}
	if len(req.Domains) == 0 {
		writeJSON(w, http.StatusBadRequest, errBody("no_domains"))
		return
	}

	added, err := s.sess.AddAllow(req.Domains)
	switch {
	case errors.Is(err, session.ErrWouldWeaken):
		writeJSON(w, http.StatusConflict, errBody("would_weaken"))
	case errors.Is(err, blocklist.ErrNotADomain):
		writeJSON(w, http.StatusBadRequest, detailBody("not_a_domain", err.Error()))
	case errors.Is(err, blocklist.ErrTooManyCustom):
		writeJSON(w, http.StatusBadRequest, errBody("too_many_domains"))
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
	default:
		if added == nil {
			added = []string{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"added": added, "list": s.allowView()})
	}
}

func (s *Server) allowRemove(w http.ResponseWriter, r *http.Request) {
	err := s.sess.RemoveAllow(strings.TrimSpace(r.PathValue("domain")))
	switch {
	case errors.Is(err, blocklist.ErrNotADomain):
		writeJSON(w, http.StatusBadRequest, errBody("not_a_domain"))
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
	default:
		writeJSON(w, http.StatusOK, s.allowView())
	}
}
