package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Lushenwar/Flow/internal/schedule"
	"github.com/Lushenwar/Flow/internal/session"
)

type bankView struct {
	BalanceSeconds   int  `json:"balanceSeconds"`
	Spending         bool `json:"spending"`
	RemainingSeconds int  `json:"remainingSeconds"`
}

func (s *Server) bank(w http.ResponseWriter, r *http.Request) {
	balance, remaining, spending := s.sess.Bank()
	writeJSON(w, http.StatusOK, bankView{
		BalanceSeconds:   int(balance.Seconds()),
		Spending:         spending,
		RemainingSeconds: int(remaining.Seconds()),
	})
}

type spendRequest struct {
	Minutes int `json:"minutes"`
}

// spend opens recreation time. Requires IDLE, deducts up front, and the window
// cannot be cancelled — see Bank.StartSpend for why refunds would be an off switch.
func (s *Server) spend(w http.ResponseWriter, r *http.Request) {
	var req spendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("bad_request"))
		return
	}
	err := s.sess.SpendBank(time.Duration(req.Minutes) * time.Minute)
	switch {
	case errors.Is(err, session.ErrNotIdle):
		writeJSON(w, http.StatusConflict, errBody("not_idle"))
	case errors.Is(err, session.ErrSpendActive):
		writeJSON(w, http.StatusConflict, errBody("spend_active"))
	case errors.Is(err, session.ErrInsufficient):
		writeJSON(w, http.StatusBadRequest, errBody("insufficient_balance"))
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
	default:
		s.bank(w, r)
	}
}

type scheduleView struct {
	schedule.Schedule
	Active bool `json:"active"`
}

func (s *Server) schedules(w http.ResponseWriter, r *http.Request) {
	all, active := s.sess.Schedules()
	live := map[string]bool{}
	for _, id := range active {
		live[id] = true
	}

	out := []scheduleView{}
	for _, sc := range all {
		out = append(out, scheduleView{Schedule: sc, Active: live[sc.ID]})
	}
	writeJSON(w, http.StatusOK, out)
}

type schedulePut struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	ListIDs []string `json:"listIds"`
	Start   string   `json:"start"`
	End     string   `json:"end"`
	Enabled bool     `json:"enabled"`
	// Days the window may START on, 0=Sunday. Empty means every day. The field
	// existed on the model and not on the wire, so a schedule created over HTTP
	// was every-day whatever the caller sent.
	Days []int `json:"days,omitempty"`
}

// putSchedule creates or replaces a schedule, pinning the current zone.
//
// Refused while the schedule's own window is live: its rules are in the
// effective set right now, and rewriting its hours would weaken enforcement on
// the same terms a baseline toggle would.
func (s *Server) putSchedule(w http.ResponseWriter, r *http.Request) {
	var req schedulePut
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("bad_request"))
		return
	}
	if req.ID == "" {
		req.ID = r.PathValue("id")
	}
	if req.ID == "" {
		writeJSON(w, http.StatusBadRequest, errBody("missing_id"))
		return
	}

	sc, err := schedule.New(req.ID, req.Name, req.ListIDs, req.Start, req.End, time.Local)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err.Error()))
		return
	}
	sc.Enabled = req.Enabled
	for _, d := range req.Days {
		if d < 0 || d > 6 {
			writeJSON(w, http.StatusBadRequest, errBody("bad_day"))
			return
		}
		sc.Days = append(sc.Days, time.Weekday(d))
	}

	if err := s.sess.PutSchedule(sc); err != nil {
		if errors.Is(err, session.ErrWouldWeaken) {
			writeJSON(w, http.StatusConflict, errBody("would_weaken"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	s.schedules(w, r)
}

// deleteSchedule removes one, and refuses while its window is live.
func (s *Server) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	if err := s.sess.DeleteSchedule(r.PathValue("id")); err != nil {
		if errors.Is(err, session.ErrWouldWeaken) {
			writeJSON(w, http.StatusConflict, errBody("would_weaken"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	s.schedules(w, r)
}
