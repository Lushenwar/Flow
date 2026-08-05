package session

import "time"

// Delay is the generic delayed-release primitive.
//
// One implementation, three consumers: the session escape hatch, baseline
// disable requests, and (later) time-bank spends. Writing a second one for
// baseline is how the two paths drift apart and one of them ends up cancellable.
//
// The invariants, in one place:
//   - Requesting is free and reversible. Cancelling is instant.
//   - The thing being released stays FULLY ENFORCED for the entire countdown.
//   - Re-requesting does not shorten an existing countdown.
//   - The delay is clamped to [MinDelay, MaxDelay]. It cannot be disabled by any
//     setting, hardcore mode, or config flag. That is the line between a tool and
//     a trap.
type Delay struct {
	Requested bool     `json:"requested"`
	Deadline  Deadline `json:"deadline"`
}

const (
	// MinDelay is the floor. A shorter delay is not friction, it is a stop button.
	MinDelay = 15 * time.Minute
	// MaxDelay is the ceiling, so a bad config cannot trap someone.
	MaxDelay = 30 * time.Minute
)

// Clamp keeps any configured delay inside the permitted band.
func Clamp(d time.Duration) time.Duration {
	if d < MinDelay {
		return MinDelay
	}
	if d > MaxDelay {
		return MaxDelay
	}
	return d
}

// Request starts the countdown. Idempotent: a second call does not shorten it,
// which is what stops "spam the escape button" from working.
func (d Delay) Request(c Clock, after time.Duration) Delay {
	if d.Requested {
		return d
	}
	return Delay{Requested: true, Deadline: NewDeadline(c, Clamp(after))}
}

// Cancel is instant and free. Always available — that is the whole point.
func (d Delay) Cancel() Delay { return Delay{} }

// Available reports whether the countdown has run out.
func (d Delay) Available(c Clock, serverNow *time.Time) bool {
	return d.Requested && d.Deadline.Expired(c, serverNow)
}

// Remaining is 0 when nothing is pending.
func (d Delay) Remaining(c Clock, serverNow *time.Time) time.Duration {
	if !d.Requested {
		return 0
	}
	return d.Deadline.Remaining(c, serverNow)
}

// AvailableAt is the projected release instant, for display.
func (d Delay) AvailableAt(c Clock, serverNow *time.Time) *time.Time {
	if !d.Requested {
		return nil
	}
	t := d.Deadline.TargetAt(c, serverNow)
	return &t
}
