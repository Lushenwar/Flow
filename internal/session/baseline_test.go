package session

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func TestEnableIsInstant(t *testing.T) {
	c := newFake()
	b := NewBaseline(nil)

	b.Enable("preset.gambling")
	if !b.Rules["preset.gambling"].Effective() {
		t.Fatal("turning a rule ON must be immediate")
	}
	_ = c
}

// The asymmetry, which is the whole Blocking screen.
func TestDisableTakesFifteenMinutesAndStaysEnforcedThroughout(t *testing.T) {
	c := newFake()
	b := NewBaseline([]string{"preset.doomscroll"})

	if err := b.Disable(c, "preset.doomscroll", false); err != nil {
		t.Fatal(err)
	}
	r := b.Rules["preset.doomscroll"]
	if !r.Pending.Requested {
		t.Fatal("no countdown started")
	}
	if !r.Effective() {
		t.Fatal("the rule must stay fully enforced during the countdown")
	}
	if got := r.Pending.Remaining(c, nil); got != BaselineDisableDelay {
		t.Fatalf("countdown %v, want %v", got, BaselineDisableDelay)
	}

	c.tick(14 * time.Minute)
	b.Tick(c, nil)
	if !b.Rules["preset.doomscroll"].Effective() {
		t.Fatal("still enforced at 14:00")
	}

	c.tick(time.Minute)
	fired := b.Tick(c, nil)
	if !slices.Contains(fired, "preset.doomscroll") {
		t.Fatalf("did not fire: %v", fired)
	}
	if b.Rules["preset.doomscroll"].Effective() {
		t.Fatal("should be off after the delay")
	}
}

func TestCancellingIsInstantAndFree(t *testing.T) {
	c := newFake()
	b := NewBaseline([]string{"preset.adult"})
	b.Disable(c, "preset.adult", false)

	c.tick(14 * time.Minute)
	b.CancelDisable("preset.adult")

	if b.Rules["preset.adult"].Pending.Requested {
		t.Fatal("cancel must clear the countdown immediately")
	}
	c.tick(time.Hour)
	b.Tick(c, nil)
	if !b.Rules["preset.adult"].Effective() {
		t.Fatal("a cancelled disable must never fire")
	}
}

// Re-enabling during the countdown cancels it too.
func TestEnableDuringCountdownCancelsIt(t *testing.T) {
	c := newFake()
	b := NewBaseline([]string{"preset.adult"})
	b.Disable(c, "preset.adult", false)

	c.tick(10 * time.Minute)
	b.Enable("preset.adult")

	if b.Rules["preset.adult"].Pending.Requested {
		t.Fatal("re-enabling must cancel a pending disable")
	}
	c.tick(time.Hour)
	b.Tick(c, nil)
	if !b.Rules["preset.adult"].Effective() {
		t.Fatal("rule turned off after being re-enabled")
	}
}

// Neither surface can weaken the other.
func TestDisableRejectedOutrightDuringASession(t *testing.T) {
	c := newFake()
	b := NewBaseline([]string{"preset.video"})

	err := b.Disable(c, "preset.video", true)
	if !errors.Is(err, ErrWouldWeaken) {
		t.Fatalf("got %v, want would_weaken", err)
	}
	if b.Rules["preset.video"].Pending.Requested {
		t.Fatal("the delayed-disable path must not even start during a session")
	}
}

func TestDisableCannotBeRushedByRepeating(t *testing.T) {
	c := newFake()
	b := NewBaseline([]string{"preset.adult"})
	b.Disable(c, "preset.adult", false)

	c.tick(10 * time.Minute)
	first := b.Rules["preset.adult"].Pending.Remaining(c, nil)
	b.Disable(c, "preset.adult", false)

	if got := b.Rules["preset.adult"].Pending.Remaining(c, nil); got != first {
		t.Fatalf("a second request shortened the delay: %v then %v", first, got)
	}
}

func TestEnabledIDsFeedsTheUnion(t *testing.T) {
	c := newFake()
	b := NewBaseline([]string{"preset.adult", "preset.gambling"})

	if got := b.EnabledIDs(); !slices.Equal(got, []string{"preset.adult", "preset.gambling"}) {
		t.Fatalf("got %v", got)
	}
	b.Disable(c, "preset.adult", false)
	if got := b.EnabledIDs(); len(got) != 2 {
		t.Fatal("a pending disable must not remove the rule from the union")
	}
	c.tick(BaselineDisableDelay)
	b.Tick(c, nil)
	if got := b.EnabledIDs(); !slices.Equal(got, []string{"preset.gambling"}) {
		t.Fatalf("got %v after the delay fired", got)
	}
}

// The countdown is anchored, so it survives restarts and resists clock changes.
func TestCountdownResistsAClockJump(t *testing.T) {
	c := newFake()
	b := NewBaseline([]string{"preset.adult"})
	b.Disable(c, "preset.adult", false)

	c.wall = c.wall.Add(2 * time.Hour) // drag the clock forward to rush it
	b.Tick(c, nil)

	if !b.Rules["preset.adult"].Effective() {
		t.Fatal("a wall-clock jump must not fire a pending disable early")
	}
}
