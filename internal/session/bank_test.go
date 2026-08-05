package session

import (
	"errors"
	"testing"
	"time"
)

// The exit criterion: 50 locked minutes credits exactly 10.0 recreation minutes.
func TestFiftyMinutesCreditsExactlyTen(t *testing.T) {
	var b Bank
	b.Credit(50 * time.Minute)

	if got := b.Balance(); got != 10*time.Minute {
		t.Fatalf("balance %v, want 10m", got)
	}
}

func TestCreditIgnoresNonPositiveDurations(t *testing.T) {
	var b Bank
	b.Credit(0)
	b.Credit(-time.Hour)
	if b.Balance() != 0 {
		t.Fatalf("balance %v", b.Balance())
	}
}

func TestBalanceIsNeverNegative(t *testing.T) {
	// A hand-edited row claiming a negative balance must read as zero, not as a
	// debt that silently swallows the next credit.
	b := Bank{BalanceSeconds: -9999}
	if got := b.Balance(); got != 0 {
		t.Fatalf("balance %v", got)
	}
}

func TestSpendRequiresIdle(t *testing.T) {
	c := newFake()
	b := Bank{BalanceSeconds: 600}

	for _, s := range []State{Arming, Focus, Releasing} {
		if err := b.StartSpend(c, 5*time.Minute, s); !errors.Is(err, ErrNotIdle) {
			t.Errorf("state %s returned %v, want not_idle", s, err)
		}
	}
	if err := b.StartSpend(c, 5*time.Minute, Idle); err != nil {
		t.Fatalf("IDLE should be allowed: %v", err)
	}
}

func TestSpendDeductsUpFrontAndCannotBeRefunded(t *testing.T) {
	c := newFake()
	b := Bank{BalanceSeconds: 600} // 10 minutes

	if err := b.StartSpend(c, 10*time.Minute, Idle); err != nil {
		t.Fatal(err)
	}
	if got := b.Balance(); got != 0 {
		t.Fatalf("balance %v — the whole spend is deducted at the start", got)
	}

	// Use two minutes of it, then let the window run out. There is no API to
	// stop early, and nothing is returned: "spend 30, use 2, refund 28" would be
	// an instant off switch with extra steps.
	c.tick(2 * time.Minute)
	if !b.Spending(c, nil) {
		t.Fatal("window should still be open")
	}
	c.tick(8 * time.Minute)
	if !b.Tick(c, nil) {
		t.Fatal("window should have closed")
	}
	if got := b.Balance(); got != 0 {
		t.Fatalf("balance %v — an unused remainder is not refunded", got)
	}
}

func TestCannotSpendMoreThanEarned(t *testing.T) {
	c := newFake()
	b := Bank{BalanceSeconds: 300} // 5 minutes

	if err := b.StartSpend(c, 10*time.Minute, Idle); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("got %v, want insufficient", err)
	}
	if got := b.Balance(); got != 5*time.Minute {
		t.Fatalf("a rejected spend must not deduct: %v", got)
	}
}

func TestCannotStackSpends(t *testing.T) {
	c := newFake()
	b := Bank{BalanceSeconds: 3600}

	if err := b.StartSpend(c, 5*time.Minute, Idle); err != nil {
		t.Fatal(err)
	}
	if err := b.StartSpend(c, 5*time.Minute, Idle); !errors.Is(err, ErrSpendActive) {
		t.Fatalf("got %v, want spend_active", err)
	}
}

func TestTinySpendsRejected(t *testing.T) {
	c := newFake()
	b := Bank{BalanceSeconds: 3600}
	if err := b.StartSpend(c, time.Second, Idle); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("got %v", err)
	}
}

// The window is a Deadline, so it inherits the whole time authority.
func TestSpendWindowResistsAClockJump(t *testing.T) {
	c := newFake()
	b := Bank{BalanceSeconds: 1200}
	b.StartSpend(c, 20*time.Minute, Idle)

	c.tick(time.Minute)
	c.wall = c.wall.Add(3 * time.Hour) // drag the clock to extend recreation

	if got := b.Remaining(c, nil); got != 19*time.Minute {
		t.Fatalf("remaining %v, want 19m — the wall clock must not extend a spend", got)
	}
	if b.Tick(c, nil) {
		t.Fatal("window closed early")
	}
}

func TestSpendSurvivesARebootAndStillExpires(t *testing.T) {
	c := newFake()
	b := Bank{BalanceSeconds: 1200}
	b.StartSpend(c, 20*time.Minute, Idle)

	c.tick(5 * time.Minute)
	b.Window.Anchor = b.Window.Anchor.Checkpoint(c)
	c.reboot(2 * time.Minute)
	b.Window.Anchor, _ = b.Window.Anchor.Recover(c)

	if got := b.Remaining(c, nil); got != 13*time.Minute {
		t.Fatalf("remaining %v, want 13m", got)
	}
	c.tick(13 * time.Minute)
	if !b.Tick(c, nil) {
		t.Fatal("window must hard re-lock at expiry, reboot or not")
	}
}

func TestTickOnAnIdleBankIsANoOp(t *testing.T) {
	c := newFake()
	var b Bank
	if b.Tick(c, nil) {
		t.Fatal("nothing to close")
	}
	if b.Spending(c, nil) {
		t.Fatal("not spending")
	}
}
