package session

import (
	"fmt"
	"testing"
	"time"
)

// fakeClock is the whole point of the Clock interface: a 2-hour session, a
// reboot, and a BIOS clock set to 2030 all take microseconds to test.
type fakeClock struct {
	wall time.Time
	mono time.Duration
	boot int
}

func newFake() *fakeClock {
	return &fakeClock{wall: time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC), mono: time.Hour}
}

func (f *fakeClock) Wall() time.Time          { return f.wall }
func (f *fakeClock) Monotonic() time.Duration { return f.mono }
func (f *fakeClock) Unbiased() time.Duration  { return f.mono }
func (f *fakeClock) BootID() string           { return fmt.Sprintf("boot-%d", f.boot) }

// tick advances both clocks together, the normal case.
func (f *fakeClock) tick(d time.Duration) {
	f.wall = f.wall.Add(d)
	f.mono += d
}

// reboot resets monotonic and advances the wall clock by the downtime.
func (f *fakeClock) reboot(downtime time.Duration) {
	f.wall = f.wall.Add(downtime)
	f.mono = 0
	f.boot++
}

func TestElapsedTracksMonotonic(t *testing.T) {
	c := newFake()
	d := NewDeadline(c, 10*time.Minute)

	c.tick(4 * time.Minute)
	if got := d.Remaining(c, nil); got != 6*time.Minute {
		t.Fatalf("remaining %v, want 6m", got)
	}
	c.tick(6 * time.Minute)
	if !d.Expired(c, nil) {
		t.Fatal("should have expired at the target")
	}
	if got := d.Remaining(c, nil); got != 0 {
		t.Fatalf("remaining %v, want a floor of 0", got)
	}
}

// Threat model row 6.
func TestClockMovedForwardDoesNotEndTheSession(t *testing.T) {
	c := newFake()
	d := NewDeadline(c, 10*time.Minute)
	c.tick(1 * time.Minute)

	c.wall = c.wall.Add(2 * time.Hour) // user drags the system clock forward

	if got := d.Remaining(c, nil); got != 9*time.Minute {
		t.Fatalf("remaining %v — the wall clock must not shorten a lock", got)
	}
}

func TestClockMovedBackwardDoesNotExtendForever(t *testing.T) {
	c := newFake()
	d := NewDeadline(c, 10*time.Minute)
	c.tick(9 * time.Minute)

	c.wall = c.wall.Add(-5 * time.Hour)

	if got := d.Remaining(c, nil); got != 1*time.Minute {
		t.Fatalf("remaining %v — monotonic still governs", got)
	}
}

// Threat model row 5, and the reason monotonic alone is not enough.
func TestRebootCreditsDowntimeFromWallClock(t *testing.T) {
	c := newFake()
	d := NewDeadline(c, 60*time.Minute)

	c.tick(10 * time.Minute)
	d.Anchor = d.Anchor.Checkpoint(c) // the 5s checkpoint

	c.reboot(3 * time.Minute)
	a, rebooted := d.Anchor.Recover(c)
	if !rebooted {
		t.Fatal("reboot not detected")
	}
	d.Anchor = a

	// 10 elapsed before + 3 offline = 13 credited.
	if got := d.Remaining(c, nil); got != 47*time.Minute {
		t.Fatalf("remaining %v, want 47m", got)
	}
	c.tick(2 * time.Minute)
	if got := d.Remaining(c, nil); got != 45*time.Minute {
		t.Fatalf("remaining %v after the reboot, want 45m", got)
	}
}

// "Shut down, set the BIOS clock to 2030, boot" must not end a session.
func TestOfflineCreditIsCapped(t *testing.T) {
	c := newFake()
	d := NewDeadline(c, 10*time.Hour)
	c.tick(1 * time.Minute)
	d.Anchor = d.Anchor.Checkpoint(c)

	c.reboot(4 * 365 * 24 * time.Hour) // BIOS clock set to 2030
	d.Anchor, _ = d.Anchor.Recover(c)

	want := 10*time.Hour - 1*time.Minute - maxOfflineCredit
	if got := d.Remaining(c, nil); got != want {
		t.Fatalf("remaining %v, want %v — one gap credits at most %v", got, want, maxOfflineCredit)
	}
}

func TestRepeatedRebootsEachCreditAtMostTheCap(t *testing.T) {
	c := newFake()
	d := NewDeadline(c, 20*time.Hour)

	for i := 0; i < 3; i++ {
		c.tick(time.Minute)
		d.Anchor = d.Anchor.Checkpoint(c)
		c.reboot(100 * time.Hour)
		d.Anchor, _ = d.Anchor.Recover(c)
	}
	// 3 minutes of uptime plus 3 capped gaps.
	want := 20*time.Hour - 3*time.Minute - 3*maxOfflineCredit
	if got := d.Remaining(c, nil); got != want {
		t.Fatalf("remaining %v, want %v", got, want)
	}
}

func TestClockMovedBackwardsAcrossRebootCreditsNothing(t *testing.T) {
	c := newFake()
	d := NewDeadline(c, time.Hour)
	c.tick(5 * time.Minute)
	d.Anchor = d.Anchor.Checkpoint(c)

	c.reboot(-2 * time.Hour) // clock now reads earlier than at shutdown
	d.Anchor, _ = d.Anchor.Recover(c)

	if got := d.Remaining(c, nil); got != 55*time.Minute {
		t.Fatalf("remaining %v, want 55m — a backwards gap credits 0, never negative", got)
	}
}

// A reboot inside the same boot-id minute is still a reboot.
func TestMonotonicGoingBackwardsCountsAsReboot(t *testing.T) {
	c := newFake()
	d := NewDeadline(c, time.Hour)
	c.tick(5 * time.Minute)
	d.Anchor = d.Anchor.Checkpoint(c)

	c.mono = 0 // same boot id, monotonic reset
	c.wall = c.wall.Add(time.Minute)

	if d.Anchor.sameBoot(c) {
		t.Fatal("a monotonic reading that went backwards must invalidate the reference")
	}
	d.Anchor, _ = d.Anchor.Recover(c)
	if got := d.Remaining(c, nil); got != 54*time.Minute {
		t.Fatalf("remaining %v, want 54m", got)
	}
}

// Uptime since the last checkpoint is not lost: the wall gap is measured from
// WallAt, so it absorbs the uncheckpointed seconds. The credit is therefore
// slightly generous rather than slightly short, which is the correct direction —
// a lock must never end early because the daemon died between checkpoints.
func TestUncheckpointedUptimeIsAbsorbedByTheGap(t *testing.T) {
	c := newFake()
	d := NewDeadline(c, 30*time.Minute)

	c.tick(10 * time.Minute)
	d.Anchor = d.Anchor.Checkpoint(c)
	c.tick(4 * time.Second) // less than one checkpoint interval

	c.reboot(3 * time.Minute)
	d.Anchor, _ = d.Anchor.Recover(c)

	// 10m monotonic + (4s + 3m) measured on the wall clock = 13m4s.
	if got := d.Remaining(c, nil); got != 30*time.Minute-13*time.Minute-4*time.Second {
		t.Fatalf("remaining %v, want 16m56s", got)
	}
}

// The credit direction is the safety property: however the machine goes down,
// the session must never come back with MORE time remaining than it should.
func TestNoRecoveryPathEverAddsTimeBack(t *testing.T) {
	c := newFake()
	d := NewDeadline(c, time.Hour)
	before := d.Remaining(c, nil)

	c.tick(10 * time.Minute)
	d.Anchor = d.Anchor.Checkpoint(c)
	c.reboot(time.Minute)
	d.Anchor, _ = d.Anchor.Recover(c)

	if got := d.Remaining(c, nil); got >= before {
		t.Fatalf("remaining %v grew back toward %v across a reboot", got, before)
	}
	// A second Recover with no reboot must be a no-op, not another credit.
	again := d.Remaining(c, nil)
	d.Anchor, _ = d.Anchor.Recover(c)
	if got := d.Remaining(c, nil); got != again {
		t.Fatalf("Recover is not idempotent: %v then %v", again, got)
	}
}

func TestServerTimestampIsACeilingNotAFloor(t *testing.T) {
	c := newFake()
	d := NewDeadline(c, 30*time.Minute)
	start := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	d.Anchor.ServerStart = &start

	c.tick(20 * time.Minute)

	// Server says only 5 minutes passed: credit the slower source.
	slow := start.Add(5 * time.Minute)
	if got := d.Remaining(c, &slow); got != 25*time.Minute {
		t.Fatalf("remaining %v, want 25m — the ceiling must apply", got)
	}
	// Server says an hour passed: monotonic is slower, so it still wins.
	fast := start.Add(time.Hour)
	if got := d.Remaining(c, &fast); got != 10*time.Minute {
		t.Fatalf("remaining %v, want 10m — a fast remote clock must not shorten the lock", got)
	}
}

func TestDriftDetected(t *testing.T) {
	c := newFake()
	a := NewAnchor(c)
	c.tick(time.Minute)
	a = a.Checkpoint(c)

	if _, drifted := a.Drift(c); drifted {
		t.Fatal("clocks in step should not report drift")
	}

	c.mono += 10 * time.Second
	c.wall = c.wall.Add(20 * time.Minute) // wall jumped, monotonic did not

	d, drifted := a.Drift(c)
	if !drifted {
		t.Fatalf("20-minute divergence not flagged, got %v", d)
	}
}

func TestExtendAppliesTheCappedPenalty(t *testing.T) {
	c := newFake()
	d := NewDeadline(c, 25*time.Minute)
	c.tick(5 * time.Minute)

	d = d.Extend(15 * time.Minute)
	if got := d.Remaining(c, nil); got != 35*time.Minute {
		t.Fatalf("remaining %v, want 35m", got)
	}
}

func TestTargetAtIsDerivedNotStored(t *testing.T) {
	c := newFake()
	d := NewDeadline(c, 10*time.Minute)

	first := d.TargetAt(c, nil)
	c.wall = c.wall.Add(3 * time.Hour) // only the wall clock moves

	// Remaining is unchanged, so the projected instant moves with the wall clock
	// rather than the lock ending early.
	if got := d.Remaining(c, nil); got != 10*time.Minute {
		t.Fatalf("remaining %v", got)
	}
	if second := d.TargetAt(c, nil); !second.After(first) {
		t.Fatal("targetAt should track the wall clock it is displayed against")
	}
}
