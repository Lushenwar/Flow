// Package session owns the state machine, the time authority, and the generic
// delayed-release primitive that both surfaces use.
package session

import "time"

// Clock is the injectable time source. Everything in this package reads time
// through it, so the whole authority is testable without waiting or rebooting.
type Clock interface {
	// Wall is the system clock. Untrusted: a user can move it.
	Wall() time.Time
	// Monotonic is time since boot, INCLUDING sleep. A sleeping laptop is not
	// one you are doomscrolling on, so suspend counts as elapsed.
	Monotonic() time.Duration
	// Unbiased is time since boot EXCLUDING sleep. Recorded for the drift log;
	// never used to credit elapsed.
	Unbiased() time.Duration
	// BootID changes when the machine reboots. Monotonic resets at the same moment.
	BootID() string
}

// maxOfflineCredit caps what one shutdown gap can contribute. Uncapped, "shut
// down, set the BIOS clock to 2030, boot" ends any session.
const maxOfflineCredit = 4 * time.Hour

// driftThreshold is how far wall and monotonic may diverge within one boot
// before it is logged as a clock_drift event.
const driftThreshold = 5 * time.Minute

// Anchor is the signed time record for one deadline. It is the whole of the time
// authority's persistent state.
type Anchor struct {
	// BootID and MonoRef pin where CreditedBefore was last frozen.
	BootID         string        `json:"bootId"`
	MonoRef        time.Duration `json:"monoRef"`
	CreditedBefore time.Duration `json:"creditedBefore"`

	// Checkpointed every 5s so a shutdown gap can be measured.
	MonoAt time.Duration `json:"monoAt"`
	WallAt time.Time     `json:"wallAt"`

	// WallStart is display-only: it is what the UI shows as "started at".
	WallStart time.Time `json:"wallStart"`

	// ServerStart, when present, is a signed remote timestamp. server_now minus
	// this is a ceiling on credited elapsed.
	//
	// ponytail: nothing fetches this, deliberately, and the decision was
	// revisited rather than inherited. Monotonic already defeats the attack this
	// would defend against — threat model row 6, "move the system clock
	// forward", is closed and tested — so a time client would add a network
	// dependency, a reachability failure mode, and a fail-open path, for no
	// threat-model gain.
	//
	// The ceiling logic stays because it costs nothing and is tested
	// (TestServerTimestampIsACeilingNotAFloor), so wiring in a time service later
	// is a wiring change rather than a redesign. If it ever happens it must fail
	// OPEN: no network must never mean no session, or the app stops working on a
	// plane.
	ServerStart *time.Time `json:"serverStart,omitempty"`
}

// NewAnchor starts the clock for a deadline.
func NewAnchor(c Clock) Anchor {
	now := c.Monotonic()
	return Anchor{
		BootID:    c.BootID(),
		MonoRef:   now,
		MonoAt:    now,
		WallAt:    c.Wall(),
		WallStart: c.Wall(),
	}
}

// Elapsed credits time at the rate of the slowest credible source.
func (a Anchor) Elapsed(c Clock, serverNow *time.Time) time.Duration {
	elapsed := a.CreditedBefore
	if a.sameBoot(c) {
		if d := c.Monotonic() - a.MonoRef; d > 0 {
			elapsed += d
		}
	}
	// A different boot with no Recover() call yet means the monotonic reading is
	// from a clock that has since reset; credit nothing extra rather than guess.

	if a.ServerStart != nil && serverNow != nil {
		if ceiling := serverNow.Sub(*a.ServerStart); ceiling < elapsed {
			elapsed = ceiling
		}
	}
	if elapsed < 0 {
		return 0
	}
	return elapsed
}

// sameBoot reports whether the anchor's monotonic reference is still valid.
// A monotonic reading that went backwards is a reboot whatever the id says.
func (a Anchor) sameBoot(c Clock) bool {
	return a.BootID == c.BootID() && c.Monotonic() >= a.MonoRef
}

// Checkpoint updates the shutdown-gap markers. Called every 5s.
func (a Anchor) Checkpoint(c Clock) Anchor {
	if !a.sameBoot(c) {
		return a
	}
	a.MonoAt = c.Monotonic()
	a.WallAt = c.Wall()
	return a
}

// Recover folds a reboot into the anchor: freeze the previous boot's monotonic
// contribution, credit the shutdown gap from wall clock (capped), and re-reference
// against the new boot. Returns the anchor unchanged if no reboot happened.
func (a Anchor) Recover(c Clock) (Anchor, bool) {
	if a.sameBoot(c) {
		return a, false
	}
	// Everything the previous boot earned, measured while its clock was still running.
	if d := a.MonoAt - a.MonoRef; d > 0 {
		a.CreditedBefore += d
	}
	// The gap itself, on the only clock that survives a power cycle.
	gap := c.Wall().Sub(a.WallAt)
	if gap < 0 {
		gap = 0 // clock moved backwards across the reboot; credit nothing
	}
	if gap > maxOfflineCredit {
		gap = maxOfflineCredit
	}
	a.CreditedBefore += gap

	now := c.Monotonic()
	a.BootID, a.MonoRef, a.MonoAt = c.BootID(), now, now
	a.WallAt = c.Wall()
	return a, true
}

// Drift reports how far the wall clock has diverged from monotonic since the last
// checkpoint, and whether that exceeds the threshold. Monotonic keeps winning
// either way — the log is more useful than the punishment.
func (a Anchor) Drift(c Clock) (time.Duration, bool) {
	if !a.sameBoot(c) {
		return 0, false
	}
	wallDelta := c.Wall().Sub(a.WallAt)
	monoDelta := c.Monotonic() - a.MonoAt
	d := wallDelta - monoDelta
	if d < 0 {
		d = -d
	}
	return d, d > driftThreshold
}

// Deadline is an anchor plus a duration: the primitive behind session targets,
// the arming grace window, the escape hatch, and baseline disable delays.
// Built once, consumed everywhere.
type Deadline struct {
	Anchor   Anchor        `json:"anchor"`
	Duration time.Duration `json:"duration"`
}

func NewDeadline(c Clock, d time.Duration) Deadline {
	return Deadline{Anchor: NewAnchor(c), Duration: d}
}

// Remaining never goes negative and is never shortened on the word of one
// unverified clock.
func (d Deadline) Remaining(c Clock, serverNow *time.Time) time.Duration {
	rem := d.Duration - d.Anchor.Elapsed(c, serverNow)
	if rem < 0 {
		return 0
	}
	return rem
}

func (d Deadline) Expired(c Clock, serverNow *time.Time) bool {
	return d.Remaining(c, serverNow) == 0
}

// TargetAt is the projected completion instant, for display only. It is derived
// from remaining time on every read rather than stored, so a process restart
// cannot lose it and a wall-clock change cannot move it.
func (d Deadline) TargetAt(c Clock, serverNow *time.Time) time.Time {
	return c.Wall().Add(d.Remaining(c, serverNow))
}

// Extend adds to the deadline. Used for the capped, pre-disclosed tamper penalty.
func (d Deadline) Extend(by time.Duration) Deadline {
	d.Duration += by
	return d
}
