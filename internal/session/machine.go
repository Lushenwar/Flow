package session

import (
	"errors"
	"time"
)

type State string

const (
	Idle      State = "IDLE"
	Arming    State = "ARMING"
	Focus     State = "FOCUS"
	Releasing State = "RELEASING"
	Complete  State = "COMPLETE"
)

type Mode string

const (
	ModeCommitment Mode = "commitment"
	ModePomodoro   Mode = "pomodoro"
)

var (
	ErrActive   = errors.New("active")
	ErrLocked   = errors.New("locked")
	ErrDuration = errors.New("duration out of range")
)

// Duration bounds from the API spec.
const (
	MinDuration = 5 * time.Minute
	MaxDuration = 480 * time.Minute

	// DefaultGrace is the ARMING window. Session blocks are already applied
	// during it — the grace window releases the LOCK, not the BLOCKS, or the
	// window becomes the bypass.
	DefaultGrace = 15 * time.Second

	// TamperPenalty is capped and pre-disclosed, at most once per session.
	// Open-ended punishment turns a dead CMOS battery into an unescapable lock
	// on the user's own computer.
	TamperPenalty = 15 * time.Minute
)

// Session is the persisted state machine. It is a value: every transition
// returns a new one, which the caller writes as a single signed row.
type Session struct {
	State State `json:"state"`
	Mode  Mode  `json:"mode"`

	Target Deadline `json:"target"`
	Grace  Deadline `json:"grace"`
	Escape Delay    `json:"escape"`

	BlocklistIDs []string `json:"blocklistIds"`

	AcceptTamperPenalty bool `json:"acceptTamperPenalty"`
	PenaltyApplied      bool `json:"penaltyApplied"`

	// Cycle tracks Hard Pomodoro. Zero for commitment sessions.
	CycleIndex int `json:"cycleIndex"`
	CycleOf    int `json:"cycleOf"`

	// Credited is set exactly once, on the transition into COMPLETE.
	Credited bool `json:"credited"`
}

func New() Session { return Session{State: Idle} }

// Active reports whether any session rules are being enforced. ARMING counts:
// blocks apply at commit, not at grace-end.
func (s Session) Active() bool {
	return s.State == Arming || s.State == Focus || s.State == Releasing
}

// CanRelease is computed here and never in the UI. It is false in exactly the
// states where no verb ends the session.
func (s Session) CanRelease() bool { return s.State == Arming }

// Commit moves IDLE (or COMPLETE) to ARMING. Blocks are live from this moment.
func (s Session) Commit(c Clock, mode Mode, dur time.Duration, ids []string, grace time.Duration, penalty bool) (Session, error) {
	if s.Active() {
		return s, ErrActive
	}
	if dur < MinDuration || dur > MaxDuration {
		return s, ErrDuration
	}
	if grace <= 0 {
		grace = DefaultGrace
	}
	return Session{
		State:               Arming,
		Mode:                mode,
		Target:              NewDeadline(c, dur),
		Grace:               NewDeadline(c, grace),
		BlocklistIDs:        append([]string(nil), ids...),
		AcceptTamperPenalty: penalty,
	}, nil
}

// Abort is valid only in ARMING. This exists so the grace window works; it is
// not a stop button and must never become one.
func (s Session) Abort() (Session, error) {
	if s.State != Arming {
		return s, ErrLocked
	}
	return New(), nil
}

// RequestEscape starts the delayed cancel and moves to RELEASING. Blocks stay
// fully enforced throughout. Idempotent.
func (s Session) RequestEscape(c Clock, after time.Duration) (Session, error) {
	if s.State != Focus && s.State != Releasing {
		return s, ErrLocked
	}
	s.Escape = s.Escape.Request(c, after)
	s.State = Releasing
	return s, nil
}

// Release completes an escape. Only valid at or after the delay expires.
func (s Session) Release(c Clock, serverNow *time.Time) (Session, error) {
	if s.State != Releasing || !s.Escape.Available(c, serverNow) {
		return s, ErrLocked
	}
	// Escaped sessions earn nothing, or "start, escape immediately" becomes a
	// minute farm.
	return New(), nil
}

// Tick advances the machine on the clock alone. Called by the daemon loop and by
// every read, so state is never stale.
//
// It cascades: a daemon that was down long enough for both the grace window and
// the whole session to elapse must land in COMPLETE, not sit in FOCUS until the
// next tick. Bounded because the machine has finitely many states.
func (s Session) Tick(c Clock, serverNow *time.Time) (Session, bool) {
	moved := false
	for i := 0; i < len(allStates); i++ {
		next, stepped := s.step(c, serverNow)
		if !stepped {
			break
		}
		s, moved = next, true
	}
	return s, moved
}

var allStates = []State{Idle, Arming, Focus, Releasing, Complete}

func (s Session) step(c Clock, serverNow *time.Time) (Session, bool) {
	switch s.State {
	case Arming:
		if s.Grace.Expired(c, serverNow) {
			s.State = Focus
			return s, true
		}
	case Focus, Releasing:
		if s.Target.Expired(c, serverNow) {
			s.State = Complete
			s.Credited = false // the credit transaction happens once, at COMPLETE
			return s, true
		}
	}
	return s, false
}

// ApplyPenalty extends the target by the capped amount, at most once per session,
// and only when penalties were accepted at commit.
func (s Session) ApplyPenalty() (Session, bool) {
	if !s.AcceptTamperPenalty || s.PenaltyApplied || !s.Active() {
		return s, false
	}
	s.Target = s.Target.Extend(TamperPenalty)
	s.PenaltyApplied = true
	return s, true
}

// Ack moves COMPLETE back to IDLE once the UI has shown the result.
func (s Session) Ack() Session {
	if s.State != Complete {
		return s
	}
	return New()
}
