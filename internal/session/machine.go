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
	// Break is the gap between Hard Pomodoro intervals. It is its own state
	// rather than a reuse of IDLE because the two differ in exactly the way that
	// matters: session rules lift (so Active is false, like IDLE) but a new
	// session must NOT be committable over the top of a pomodoro that is still
	// running (unlike IDLE). It also has a countdown to render, and IDLE has
	// nothing to count.
	Break State = "BREAK"
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
	ErrCycles   = errors.New("cycles out of range")
	ErrBreak    = errors.New("break out of range")
)

// Duration bounds from the API spec.
const (
	MinDuration = 5 * time.Minute
	MaxDuration = 480 * time.Minute

	// Pomodoro bounds. Cycles are capped because N intervals of MaxDuration is
	// an arbitrarily long commitment assembled out of individually reasonable
	// pieces, and the escape hatch is the only way out of a very long one.
	MinCycles = 1
	MaxCycles = 12
	// Breaks are shorter than MinDuration on purpose: the classic interval is
	// 25/5, and a five-minute floor on the break would make the standard
	// configuration unexpressible.
	MinBreak = time.Minute
	MaxBreak = 30 * time.Minute

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

	// Break is the current between-intervals countdown. Only meaningful in BREAK.
	Break         Deadline      `json:"break"`
	BreakDuration time.Duration `json:"breakDuration"`

	BlocklistIDs []string `json:"blocklistIds"`

	AcceptTamperPenalty bool `json:"acceptTamperPenalty"`
	PenaltyApplied      bool `json:"penaltyApplied"`

	// Cycle tracks Hard Pomodoro. CycleOf is 1 for a commitment session, so the
	// two modes share one crediting rule instead of two.
	CycleIndex int `json:"cycleIndex"`
	CycleOf    int `json:"cycleOf"`

	// CreditedIntervals is how many focus intervals have been paid for. It is
	// written in the same signed row as the transition, so a crash between the
	// two cannot double-pay or skip — and being a count rather than a flag is
	// what lets a pomodoro credit each interval instead of only the last.
	CreditedIntervals int `json:"creditedIntervals"`
}

func New() Session { return Session{State: Idle} }

// Plan is what a commit asks for.
//
// A struct rather than seven positional parameters: mode, duration, break,
// cycles, lists, grace and penalty is past the point where a call site can be
// read without counting commas.
type Plan struct {
	Mode     Mode
	Duration time.Duration // one focus interval
	ListIDs  []string
	Grace    time.Duration
	Penalty  bool

	// Pomodoro only. Ignored for commitment sessions.
	Break  time.Duration
	Cycles int
}

// IntervalsCompleted is how many focus intervals this session has finished.
// CycleIndex advances when the NEXT interval begins, so the interval just ended
// is only counted once the session has actually left FOCUS.
func (s Session) IntervalsCompleted() int {
	if s.State == Break || s.State == Complete {
		return s.CycleIndex + 1
	}
	return s.CycleIndex
}

// Active reports whether any session rules are being enforced. ARMING counts:
// blocks apply at commit, not at grace-end.
func (s Session) Active() bool {
	return s.State == Arming || s.State == Focus || s.State == Releasing
}

// CanRelease is computed here and never in the UI. It is false in exactly the
// states where no verb ends the session.
//
// BREAK is releasable because nothing is enforced during a break beyond
// baseline — there is no lock to break out of, only the next interval to
// decline.
func (s Session) CanRelease() bool { return s.State == Arming || s.State == Break }

// Commit moves IDLE (or COMPLETE) to ARMING. Blocks are live from this moment.
func (s Session) Commit(c Clock, p Plan) (Session, error) {
	// BREAK counts as occupied even though it enforces nothing: a pomodoro is
	// still running and committing over the top of it would silently discard the
	// intervals still owed.
	if s.Active() || s.State == Break {
		return s, ErrActive
	}
	if p.Duration < MinDuration || p.Duration > MaxDuration {
		return s, ErrDuration
	}

	cycles, brk := 1, time.Duration(0)
	if p.Mode == ModePomodoro {
		cycles, brk = p.Cycles, p.Break
		if cycles < MinCycles || cycles > MaxCycles {
			return s, ErrCycles
		}
		if brk < MinBreak || brk > MaxBreak {
			return s, ErrBreak
		}
	}

	grace := p.Grace
	if grace <= 0 {
		grace = DefaultGrace
	}
	return Session{
		State:               Arming,
		Mode:                p.Mode,
		Target:              NewDeadline(c, p.Duration),
		Grace:               NewDeadline(c, grace),
		BreakDuration:       brk,
		CycleOf:             cycles,
		BlocklistIDs:        append([]string(nil), p.ListIDs...),
		AcceptTamperPenalty: p.Penalty,
	}, nil
}

// Abort is valid in ARMING, and between pomodoro intervals.
//
// ARMING is the grace window: that is what this exists for, and it is not a stop
// button. BREAK is allowed for a different reason — nothing is enforced during a
// break beyond baseline, so there is no lock to break out of. Refusing would
// only mean waiting for the next interval to start in order to be allowed to
// stop, which is friction with nothing behind it.
//
// The commitment is per-interval and always was. You cannot leave a focus
// interval early without the escape hatch; you can decline the next one.
func (s Session) Abort() (Session, error) {
	if s.State != Arming && s.State != Break {
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
//
// Returns every state passed through, not just where it landed. The caller logs
// one event per entry: a reboot recovery that crosses ARMING -> FOCUS ->
// COMPLETE used to write a single session_COMPLETE row, so the moment the lock
// actually became irreversible was missing from a log whose whole job is being a
// defensible record. Empty means nothing moved.
func (s Session) Tick(c Clock, serverNow *time.Time) (Session, []State) {
	var passed []State
	for i := 0; i < s.maxSteps(); i++ {
		next, stepped := s.step(c, serverNow)
		if !stepped {
			break
		}
		s = next
		passed = append(passed, s.State)
	}
	return s, passed
}

// allStates bounds the cascade. A pomodoro can cross many intervals in one tick
// after a long downtime, so the bound is per-interval rather than a fixed five.
var allStates = []State{Idle, Arming, Focus, Break, Releasing, Complete}

func (s Session) maxSteps() int {
	// Two transitions per interval (focus->break, break->focus), plus arming and
	// the final complete.
	return 2*max(s.CycleOf, 1) + 2
}

func (s Session) step(c Clock, serverNow *time.Time) (Session, bool) {
	switch s.State {
	case Arming:
		if s.Grace.Expired(c, serverNow) {
			s.State = Focus
			return s, true
		}
	case Focus:
		if !s.Target.Expired(c, serverNow) {
			break
		}
		// More intervals owed: take the break. Session rules lift for it, which
		// is the whole point of a break, and baseline keeps running.
		if s.CycleIndex+1 < s.CycleOf {
			s.State = Break
			s.Break = NewDeadline(c, s.BreakDuration)
			return s, true
		}
		s.State = Complete
		return s, true
	case Releasing:
		// An escape in flight ends the whole pomodoro at the interval boundary
		// rather than starting another break.
		if s.Target.Expired(c, serverNow) {
			s.State = Complete
			return s, true
		}
	case Break:
		if s.Break.Expired(c, serverNow) {
			s.CycleIndex++
			s.State = Focus
			s.Target = NewDeadline(c, s.Target.Duration)
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
