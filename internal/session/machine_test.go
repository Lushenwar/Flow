package session

import (
	"errors"
	"testing"
	"time"
)

func commit(t *testing.T, c Clock) Session {
	t.Helper()
	s, err := New().Commit(c, ModeCommitment, 25*time.Minute, []string{"preset.video"}, DefaultGrace, false)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The third state, and the reason the app is usable.
func TestArmingAppliesBlocksImmediately(t *testing.T) {
	c := newFake()
	s := commit(t, c)

	if s.State != Arming {
		t.Fatalf("state %s, want ARMING", s.State)
	}
	if !s.Active() {
		t.Fatal("session rules must be enforced during ARMING — the grace window releases the lock, not the blocks")
	}
	if !s.CanRelease() {
		t.Fatal("the second tap must work during ARMING")
	}
}

func TestGraceExpiryLocksTheSession(t *testing.T) {
	c := newFake()
	s := commit(t, c)

	c.tick(14 * time.Second)
	s, moved := s.Tick(c, nil)
	if moved || s.State != Arming {
		t.Fatal("grace has not expired yet")
	}

	c.tick(2 * time.Second)
	s, moved = s.Tick(c, nil)
	if !moved || s.State != Focus {
		t.Fatalf("state %s, want FOCUS", s.State)
	}
	if s.CanRelease() {
		t.Fatal("there is no release verb in FOCUS")
	}
}

func TestAbortIsFreeInArmingAndImpossibleAfter(t *testing.T) {
	c := newFake()
	s := commit(t, c)

	back, err := s.Abort()
	if err != nil || back.State != Idle {
		t.Fatalf("abort in ARMING must be free: %s %v", back.State, err)
	}

	// Now let it lock.
	s = commit(t, c)
	c.tick(DefaultGrace + time.Second)
	s, _ = s.Tick(c, nil)

	if _, err := s.Abort(); !errors.Is(err, ErrLocked) {
		t.Fatalf("abort in FOCUS returned %v, want locked", err)
	}
}

func TestNoSecondSessionWhileOneIsActive(t *testing.T) {
	c := newFake()
	s := commit(t, c)
	if _, err := s.Commit(c, ModeCommitment, 25*time.Minute, nil, DefaultGrace, false); !errors.Is(err, ErrActive) {
		t.Fatalf("got %v, want active", err)
	}
}

func TestDurationBounds(t *testing.T) {
	c := newFake()
	for _, d := range []time.Duration{time.Minute, 4 * time.Minute, 481 * time.Minute, 0, -time.Hour} {
		if _, err := New().Commit(c, ModeCommitment, d, nil, DefaultGrace, false); !errors.Is(err, ErrDuration) {
			t.Errorf("duration %v accepted", d)
		}
	}
	for _, d := range []time.Duration{5 * time.Minute, 25 * time.Minute, 480 * time.Minute} {
		if _, err := New().Commit(c, ModeCommitment, d, nil, DefaultGrace, false); err != nil {
			t.Errorf("duration %v rejected: %v", d, err)
		}
	}
}

func TestSessionCompletesOnTime(t *testing.T) {
	c := newFake()
	s := commit(t, c)
	c.tick(DefaultGrace)
	s, _ = s.Tick(c, nil)

	c.tick(24 * time.Minute)
	s, _ = s.Tick(c, nil)
	if s.State != Focus {
		t.Fatalf("state %s — not done yet", s.State)
	}

	c.tick(2 * time.Minute)
	s, moved := s.Tick(c, nil)
	if !moved || s.State != Complete {
		t.Fatalf("state %s, want COMPLETE", s.State)
	}
	if s.Active() {
		t.Fatal("session rules lift at 0:00")
	}
}

// The escape hatch always exists, and cannot be rushed.
func TestEscapeCannotBeShortenedBySpammingIt(t *testing.T) {
	c := newFake()
	s := commit(t, c)
	c.tick(DefaultGrace)
	s, _ = s.Tick(c, nil)

	s, err := s.RequestEscape(c, MinDelay)
	if err != nil {
		t.Fatal(err)
	}
	if s.State != Releasing {
		t.Fatalf("state %s, want RELEASING", s.State)
	}
	if !s.Active() {
		t.Fatal("blocks stay fully enforced during the escape countdown")
	}

	c.tick(10 * time.Minute)
	first := s.Escape.Remaining(c, nil)
	s, _ = s.RequestEscape(c, MinDelay) // second call
	if got := s.Escape.Remaining(c, nil); got != first {
		t.Fatalf("a second request shortened the delay: %v then %v", first, got)
	}

	if _, err := s.Release(c, nil); !errors.Is(err, ErrLocked) {
		t.Fatal("release before the delay expires must fail")
	}
	c.tick(5 * time.Minute)
	out, err := s.Release(c, nil)
	if err != nil || out.State != Idle {
		t.Fatalf("release after the delay: %s %v", out.State, err)
	}
}

func TestEscapeDelayIsClampedNotDisableable(t *testing.T) {
	c := newFake()
	s := commit(t, c)
	c.tick(DefaultGrace)
	s, _ = s.Tick(c, nil)

	// Every attempt to configure it away.
	for _, requested := range []time.Duration{0, -time.Hour, time.Second, time.Minute} {
		got, err := s.RequestEscape(c, requested)
		if err != nil {
			t.Fatal(err)
		}
		if got.Escape.Deadline.Duration != MinDelay {
			t.Fatalf("requested %v, got %v — the floor is %v", requested, got.Escape.Deadline.Duration, MinDelay)
		}
	}
	got, _ := s.RequestEscape(c, 24*time.Hour)
	if got.Escape.Deadline.Duration != MaxDelay {
		t.Fatalf("got %v, want the %v ceiling — this is the line between a tool and a trap", got.Escape.Deadline.Duration, MaxDelay)
	}
}

func TestEscapeRejectedOutsideFocus(t *testing.T) {
	c := newFake()
	if _, err := New().RequestEscape(c, MinDelay); !errors.Is(err, ErrLocked) {
		t.Fatal("nothing to escape from in IDLE")
	}
	if _, err := commit(t, c).RequestEscape(c, MinDelay); !errors.Is(err, ErrLocked) {
		t.Fatal("ARMING has abort, not escape")
	}
}

// A session that survives a reboot is the whole of threat model row 5.
func TestSessionSurvivesReboot(t *testing.T) {
	c := newFake()
	s := commit(t, c)
	c.tick(DefaultGrace)
	s, _ = s.Tick(c, nil)

	c.tick(4 * time.Minute)
	s.Target.Anchor = s.Target.Anchor.Checkpoint(c)

	c.reboot(time.Minute)
	s.Target.Anchor, _ = s.Target.Anchor.Recover(c)
	s, _ = s.Tick(c, nil)

	if s.State != Focus {
		t.Fatalf("state %s after reboot, want FOCUS", s.State)
	}
	// The target clock starts at commit, so it has run for the 15s grace window
	// plus 4 minutes of focus, plus the 1 minute the machine was off.
	if got := s.Target.Remaining(c, nil); got != 25*time.Minute-DefaultGrace-5*time.Minute {
		t.Fatalf("remaining %v, want 19m45s", got)
	}
}

func TestPenaltyIsCappedAndOnlyOnceAndOnlyIfAccepted(t *testing.T) {
	c := newFake()

	// Not accepted at commit: no penalty, ever.
	s, _ := New().Commit(c, ModeCommitment, 25*time.Minute, nil, DefaultGrace, false)
	if _, applied := s.ApplyPenalty(); applied {
		t.Fatal("penalty applied without consent at commit")
	}

	s, _ = New().Commit(c, ModeCommitment, 25*time.Minute, nil, DefaultGrace, true)
	before := s.Target.Remaining(c, nil)
	s, applied := s.ApplyPenalty()
	if !applied {
		t.Fatal("accepted penalty not applied")
	}
	if got := s.Target.Remaining(c, nil) - before; got != TamperPenalty {
		t.Fatalf("added %v, want %v", got, TamperPenalty)
	}
	if _, again := s.ApplyPenalty(); again {
		t.Fatal("penalty applied twice in one session")
	}
}

func TestAckClearsCompleteBackToIdle(t *testing.T) {
	c := newFake()
	s := commit(t, c)
	c.tick(DefaultGrace + 26*time.Minute)
	s, _ = s.Tick(c, nil)
	s, _ = s.Tick(c, nil)

	if s.State != Complete {
		t.Fatalf("state %s", s.State)
	}
	if got := s.Ack(); got.State != Idle {
		t.Fatalf("after ack: %s", got.State)
	}
}

// A daemon that was down for the whole session must land in COMPLETE, not sit
// in FOCUS waiting for another tick.
func TestTickCascadesThroughSkippedStates(t *testing.T) {
	c := newFake()
	s := commit(t, c)

	c.tick(2 * time.Hour) // grace and the entire 25 minutes elapsed while down

	s, moved := s.Tick(c, nil)
	if !moved || s.State != Complete {
		t.Fatalf("state %s, want COMPLETE in one tick", s.State)
	}
}

func TestTickIsIdempotentAtRest(t *testing.T) {
	c := newFake()
	s := commit(t, c)
	c.tick(DefaultGrace)
	s, _ = s.Tick(c, nil)

	for i := 0; i < 5; i++ {
		var moved bool
		s, moved = s.Tick(c, nil)
		if moved {
			t.Fatal("Tick reported a transition with no clock movement — every one is a signed write")
		}
	}
}
