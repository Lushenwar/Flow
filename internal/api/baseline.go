package api

import (
	"errors"
	"net/http"

	"github.com/Lushenwar/Flow/internal/session"
)

func (s *Server) baselineList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.baselineRows())
}

func (s *Server) baselineRows() []baselineRow {
	c := s.sess.Clock()
	rows := []baselineRow{}
	for _, rule := range s.sess.Baseline() {
		rows = append(rows, baselineRow{
			ID:               rule.ID,
			Enabled:          rule.Effective(),
			PendingDisableAt: rule.Pending.AvailableAt(c, nil),
		})
	}
	return rows
}

// baselineEnable is immediate, always allowed, and cancels any pending disable.
func (s *Server) baselineEnable(w http.ResponseWriter, r *http.Request) {
	if err := s.sess.EnableBaseline(r.PathValue("id")); err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, s.baselineRows())
}

// baselineDisable starts the delay and returns pendingDisableAt. The rule stays
// fully enforced throughout.
func (s *Server) baselineDisable(w http.ResponseWriter, r *http.Request) {
	at, err := s.sess.DisableBaseline(r.PathValue("id"))
	if errors.Is(err, session.ErrWouldWeaken) {
		writeJSON(w, http.StatusConflict, errBody("would_weaken"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"pendingDisableAt": at,
		"baseline":         s.baselineRows(),
	})
}

// baselineCancelDisable is instant and free.
func (s *Server) baselineCancelDisable(w http.ResponseWriter, r *http.Request) {
	if err := s.sess.CancelBaselineDisable(r.PathValue("id")); err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, s.baselineRows())
}
