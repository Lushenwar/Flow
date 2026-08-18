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
	Custom() []string
	AddCustom(raws []string) ([]string, error)
	RemoveCustom(raw string) error
	Schedules() ([]schedule.Schedule, []string)
	PutSchedule(s schedule.Schedule) error
	DeleteSchedule(id string) error
	SessionLists() []string
	SetSessionLists(ids []string) error
	Commit(p session.Plan) (session.Session, error)
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

	// Cycle is nil for a commitment session. Present for Hard Pomodoro, which is
	// what the spec's /api/state example promised all along.
	Cycle *cycleView `json:"cycle"`
}

type cycleView struct {
	Index int `json:"index"` // 1-based, for display
	Of    int `json:"of"`
	// Phase is "focus" or "break". The dial needs to know which countdown it is
	// drawing, and BREAK is not a lock.
	Phase string `json:"phase"`
	// BreakSeconds is the denominator for the break arc; BreakRemainingSeconds
	// is what it counts down.
	BreakSeconds          int `json:"breakSeconds"`
	BreakRemainingSeconds int `json:"breakRemainingSeconds"`
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
	if sess.Mode == session.ModePomodoro && sess.CycleOf > 0 {
		phase := "focus"
		if sess.State == session.Break {
			phase = "break"
		}
		view.Cycle = &cycleView{
			Index:                 sess.CycleIndex + 1,
			Of:                    sess.CycleOf,
			Phase:                 phase,
			BreakSeconds:          int(sess.BreakDuration.Seconds()),
			BreakRemainingSeconds: int(sess.Break.Remaining(c, nil).Seconds()),
		}
		// A break enforces nothing, so Active() is false and the block above
		// leaves the target fields at zero. The dial still needs a countdown.
		if sess.State == session.Break {
			view.RemainingSeconds = view.Cycle.BreakRemainingSeconds
			view.DurationSeconds = view.Cycle.BreakSeconds
			at := sess.Break.TargetAt(c, nil)
			view.TargetAt = &at
		}
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

	// Pomodoro only. DurationMinutes is one focus interval, not the total.
	BreakMinutes int `json:"breakMinutes"`
	Cycles       int `json:"cycles"`
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
	// Refuse a mode we do not implement rather than running a different one. An
	// API that reports success for work it did not do is worse than one that
	// refuses.
	if mode != session.ModeCommitment && mode != session.ModePomodoro {
		writeJSON(w, http.StatusBadRequest, detailBody("unsupported_mode", string(mode)))
		return
	}

	sess, err := s.sess.Commit(session.Plan{
		Mode:     mode,
		Duration: time.Duration(req.DurationMinutes) * time.Minute,
		ListIDs:  req.BlocklistIDs,
		Grace:    time.Duration(req.GraceSeconds) * time.Second,
		Penalty:  req.AcceptTamperPenalty,
		Break:    time.Duration(req.BreakMinutes) * time.Minute,
		Cycles:   req.Cycles,
	})
	switch {
	case errors.Is(err, session.ErrActive):
		writeJSON(w, http.StatusConflict, errBody("active"))
	case errors.Is(err, session.ErrDuration):
		writeJSON(w, http.StatusBadRequest, errBody("duration_out_of_range"))
	case errors.Is(err, session.ErrCycles):
		writeJSON(w, http.StatusBadRequest, errBody("cycles_out_of_range"))
	case errors.Is(err, session.ErrBreak):
		writeJSON(w, http.StatusBadRequest, errBody("break_out_of_range"))
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

// sessionLists is what a session covers by default.
//
// Its own endpoint rather than a field on the commit body, because the whole
// point is that it is chosen when calm: claude.md is explicit that picking a
// session's blocklist at the moment of commitment is a bypass, since a user
// mid-craving would deselect YouTube and commit to a session that blocks
// nothing.
func (s *Server) sessionLists(w http.ResponseWriter, r *http.Request) {
	ids := s.sess.SessionLists()
	if ids == nil {
		ids = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"listIds": ids})
}

type sessionListsPut struct {
	ListIDs []string `json:"listIds"`
}

func (s *Server) putSessionLists(w http.ResponseWriter, r *http.Request) {
	var req sessionListsPut
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody("bad_request"))
		return
	}
	// An empty list is a session that blocks nothing, which is the exact
	// outcome the mid-craving edit was going to produce.
	if len(req.ListIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, errBody("no_lists"))
		return
	}
	if err := s.sess.SetSessionLists(req.ListIDs); err != nil {
		if errors.Is(err, session.ErrWouldWeaken) {
			writeJSON(w, http.StatusConflict, errBody("would_weaken"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errBody(err.Error()))
		return
	}
	s.sessionLists(w, r)
}
