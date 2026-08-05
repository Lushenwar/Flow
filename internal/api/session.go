package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Lushenwar/Flow/internal/enforce"
	"github.com/Lushenwar/Flow/internal/schedule"
	"github.com/Lushenwar/Flow/internal/session"
)

// Sessions is the slice of the manager the API needs.
type Sessions interface {
	Snapshot() session.Session
	Clock() session.Clock
	Effective() enforce.Effective
	Baseline() []session.BaselineRule
	EnableBaseline(id string) error
	DisableBaseline(id string) (*time.Time, error)
	CancelBaselineDisable(id string) error
	Bank() (balance, remaining time.Duration, spending bool)
	SpendBank(d time.Duration) error
	Schedules() ([]schedule.Schedule, []string)
	PutSchedule(s schedule.Schedule) error
	Commit(mode session.Mode, dur time.Duration, ids []string, grace time.Duration, penalty bool) (session.Session, error)
	Abort() (session.Session, error)
	RequestEscape(after time.Duration) (session.Session, error)
	Challenge() (session.Challenge, error)
	VerifyEscape(id, typed string) (session.Session, error)
	Ack() (session.Session, error)
}

// stateResponse is everything both screens need in one poll.
type stateResponse struct {
	Session   sessionView   `json:"session"`
	Baseline  []baselineRow `json:"baseline"`
	Effective effectiveView `json:"effective"`
}

type sessionView struct {
	State            session.State `json:"state"`
	Mode             session.Mode  `json:"mode"`
	TargetAt         *time.Time    `json:"targetAt"`
	RemainingSeconds int           `json:"remainingSeconds"`
	CanRelease       bool          `json:"canRelease"`
	BlocklistIDs     []string      `json:"blocklistIds"`
	Escape           escapeView    `json:"escape"`

	// The totals are the denominators for the dial's arc. Without them the UI
	// would have to remember what it asked for, and a restarted window would
	// draw the wrong ring.
	DurationSeconds       int `json:"durationSeconds"`
	GraceRemainingSeconds int `json:"graceRemainingSeconds"`
	GraceSeconds          int `json:"graceSeconds"`
}

type escapeView struct {
	Requested   bool       `json:"requested"`
	AvailableAt *time.Time `json:"availableAt"`
}

type baselineRow struct {
	ID               string     `json:"id"`
	Enabled          bool       `json:"enabled"`
	PendingDisableAt *time.Time `json:"pendingDisableAt"`
}

type effectiveView struct {
	BlockedIDs  []string                 `json:"blockedIds"`
	Attribution map[string]enforce.Source `json:"attribution"`
}

func (s *Server) state(w http.ResponseWriter, r *http.Request) {
	sess := s.sess.Snapshot()
	c := s.sess.Clock()
	eff := s.sess.Effective()

	view := sessionView{
		State:        sess.State,
		Mode:         sess.Mode,
		CanRelease:   sess.CanRelease(),
		BlocklistIDs: sess.BlocklistIDs,
		Escape: escapeView{
			Requested:   sess.Escape.Requested,
			AvailableAt: sess.Escape.AvailableAt(c, nil),
		},
	}
	if view.BlocklistIDs == nil {
		view.BlocklistIDs = []string{}
	}
	if sess.Active() {
		remaining := sess.Target.Remaining(c, nil)
		at := sess.Target.TargetAt(c, nil)
		view.TargetAt = &at
		view.RemainingSeconds = int(remaining.Seconds())
		view.DurationSeconds = int(sess.Target.Duration.Seconds())
		view.GraceSeconds = int(sess.Grace.Duration.Seconds())
	}
	if sess.State == session.Arming {
		view.GraceRemainingSeconds = int(sess.Grace.Remaining(c, nil).Seconds())
	}

	writeJSON(w, http.StatusOK, stateResponse{
		Session:  view,
		Baseline: s.baselineRows(),
		Effective: effectiveView{
			BlockedIDs:  eff.SortedLists(),
			Attribution: eff.Lists,
		},
	})
}

type commitRequest struct {
	Mode                string   `json:"mode"`
	DurationMinutes     int      `json:"durationMinutes"`
	BlocklistIDs        []string `json:"blocklistIds"`
	GraceSeconds        int      `json:"graceSeconds"`
	AcceptTamperPenalty bool     `json:"acceptTamperPenalty"`
}

func (s *Server) commit(w http.ResponseWriter, r *http.Request) {
	var req commitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("bad_request"))
		return
	}
	mode := session.Mode(req.Mode)
	if mode == "" {
		mode = session.ModeCommitment
	}
	grace := time.Duration(req.GraceSeconds) * time.Second

	sess, err := s.sess.Commit(mode, time.Duration(req.DurationMinutes)*time.Minute,
		req.BlocklistIDs, grace, req.AcceptTamperPenalty)
	switch {
	case errors.Is(err, session.ErrActive):
		writeJSON(w, http.StatusConflict, errBody("active"))
	case errors.Is(err, session.ErrDuration):
		writeJSON(w, http.StatusBadRequest, errBody("duration_out_of_range"))
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
	default:
		writeJSON(w, http.StatusOK, map[string]string{"state": string(sess.State)})
	}
}

// abort is valid only in ARMING. This exists so the grace window works; it is not
// a stop button and must never become one.
func (s *Server) abort(w http.ResponseWriter, r *http.Request) {
	sess, err := s.sess.Abort()
	if errors.Is(err, session.ErrLocked) {
		writeJSON(w, http.StatusConflict, errBody("locked"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": string(sess.State)})
}

func (s *Server) escape(w http.ResponseWriter, r *http.Request) {
	sess, err := s.sess.RequestEscape(session.MinDelay)
	if errors.Is(err, session.ErrLocked) {
		writeJSON(w, http.StatusConflict, errBody("locked"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"state":       sess.State,
		"availableAt": sess.Escape.AvailableAt(s.sess.Clock(), nil),
	})
}

// challenge hands out the text to type. Refused until the delay has elapsed, so
// it cannot be fetched early and pre-typed.
func (s *Server) challenge(w http.ResponseWriter, r *http.Request) {
	c, err := s.sess.Challenge()
	if errors.Is(err, session.ErrLocked) {
		writeJSON(w, http.StatusConflict, errBody("locked"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, c)
}

type verifyRequest struct {
	ChallengeID string `json:"challengeId"`
	Typed       string `json:"typed"`
}

// verify completes an escape.
func (s *Server) verify(w http.ResponseWriter, r *http.Request) {
	var req verifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("bad_request"))
		return
	}
	sess, err := s.sess.VerifyEscape(req.ChallengeID, req.Typed)
	switch {
	case errors.Is(err, session.ErrLocked):
		writeJSON(w, http.StatusConflict, errBody("locked"))
	case errors.Is(err, session.ErrChallenge):
		writeJSON(w, http.StatusBadRequest, errBody("challenge_mismatch"))
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
	default:
		writeJSON(w, http.StatusOK, map[string]string{"state": string(sess.State)})
	}
}

func (s *Server) ack(w http.ResponseWriter, r *http.Request) {
	sess, err := s.sess.Ack()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": string(sess.State)})
}

func errBody(msg string) map[string]string { return map[string]string{"error": msg} }
