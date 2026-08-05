package session

import (
	"errors"
	"time"
)

// CreditRate is recreation minutes earned per focus minute.
const CreditRate = 0.2

// MinSpend stops a spend from being used as a rapid unlock. Below a minute the
// round trip costs more than the recreation is worth.
const MinSpend = time.Minute

var (
	ErrInsufficient = errors.New("insufficient_balance")
	ErrSpendActive  = errors.New("spend_active")
	ErrNotIdle      = errors.New("not_idle")
)

// Bank is earned recreation time.
//
// A spend is the ONE path in this app that lifts enforcement without waiting out
// a delay, so it is deliberately hemmed in: it can only start from IDLE, the
// balance is deducted up front, and the window cannot be cancelled to bank the
// remainder. Otherwise "spend 30, use 2, refund 28" is an instant off switch
// with extra steps.
type Bank struct {
	BalanceSeconds int64 `json:"balanceSeconds"`
	// Window is the active recreation period. Nil when not spending.
	Window *Deadline `json:"window,omitempty"`
}

// Credit adds earned time. Called once per completed session, in the same
// transaction as the transition into COMPLETE — aborted and escaped sessions
// earn nothing, or "start, escape immediately" becomes a minute farm.
func (b *Bank) Credit(focused time.Duration) {
	if focused <= 0 {
		return
	}
	b.BalanceSeconds += int64(focused.Seconds() * CreditRate)
}

func (b *Bank) Balance() time.Duration {
	if b.BalanceSeconds < 0 {
		return 0 // never negative, whatever the row says
	}
	return time.Duration(b.BalanceSeconds) * time.Second
}

// Spending reports whether recreation time is currently open.
func (b *Bank) Spending(c Clock, serverNow *time.Time) bool {
	return b.Window != nil && !b.Window.Expired(c, serverNow)
}

// StartSpend opens the blocklist for the requested duration and deducts it
// immediately. Requires IDLE: spending during FOCUS would make the lock
// negotiable, which is the one thing it must never be.
func (b *Bank) StartSpend(c Clock, d time.Duration, state State) error {
	if state != Idle && state != Complete {
		return ErrNotIdle
	}
	if b.Spending(c, nil) {
		return ErrSpendActive
	}
	if d < MinSpend {
		return ErrInsufficient
	}
	if d > b.Balance() {
		return ErrInsufficient
	}
	b.BalanceSeconds -= int64(d.Seconds())
	if b.BalanceSeconds < 0 {
		b.BalanceSeconds = 0
	}
	w := NewDeadline(c, d)
	b.Window = &w
	return nil
}

// Remaining is how much of the current window is left.
func (b *Bank) Remaining(c Clock, serverNow *time.Time) time.Duration {
	if b.Window == nil {
		return 0
	}
	return b.Window.Remaining(c, serverNow)
}

// Tick clears an expired window. Returns true when a window just closed, which
// is the moment enforcement hard re-locks.
func (b *Bank) Tick(c Clock, serverNow *time.Time) bool {
	if b.Window == nil {
		return false
	}
	if b.Window.Expired(c, serverNow) {
		b.Window = nil
		return true
	}
	return false
}
