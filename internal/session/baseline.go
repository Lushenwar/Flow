package session

import (
	"errors"
	"sort"
	"time"
)

// Baseline rules are indefinite: no timer, on until you wait out the delay.
//
// ponytail: this lives in package session rather than its own internal/baseline
// because it consumes Delay, and Delay consumes Clock. A separate package would
// need a third one holding the time primitives just to break the import cycle —
// more structure than one struct earns.

// BaselineDisableDelay is how long turning a rule OFF takes. Turning one ON is
// instant. That asymmetry is the single most important rule on the Blocking
// screen: a plain toggle is not a blocker, it is decoration inside a week.
//
// ponytail: one global constant. Per-category delays ("adult takes 24 hours,
// shopping takes 15 minutes") are the obvious next step and are not built.
const BaselineDisableDelay = 15 * time.Minute

var ErrWouldWeaken = errors.New("would_weaken")

type BaselineRule struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	// Pending is the delayed-disable request. The rule stays FULLY ENFORCED for
	// its entire countdown.
	Pending Delay `json:"pending"`
}

// Effective reports whether the rule is being enforced right now. A pending
// disable does not weaken anything until it fires.
func (r BaselineRule) Effective() bool { return r.Enabled }

// Baseline is the set of indefinite rules.
type Baseline struct {
	Rules map[string]*BaselineRule `json:"rules"`
}

func NewBaseline(enabled []string) *Baseline {
	b := &Baseline{Rules: map[string]*BaselineRule{}}
	for _, id := range enabled {
		b.Rules[id] = &BaselineRule{ID: id, Enabled: true}
	}
	return b
}

// IDs returns every known rule id, sorted, so the UI order is stable.
func (b *Baseline) IDs() []string {
	out := make([]string, 0, len(b.Rules))
	for id := range b.Rules {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// EnabledIDs is what the union consumes.
func (b *Baseline) EnabledIDs() []string {
	var out []string
	for _, id := range b.IDs() {
		if b.Rules[id].Effective() {
			out = append(out, id)
		}
	}
	return out
}

// Enable is immediate and always allowed. It also cancels any pending disable —
// re-enabling during the countdown is one of the two ways to call it off.
func (b *Baseline) Enable(id string) {
	r, ok := b.Rules[id]
	if !ok {
		r = &BaselineRule{ID: id}
		b.Rules[id] = r
	}
	r.Enabled = true
	r.Pending = r.Pending.Cancel()
}

// Disable starts the countdown. It does not disable anything yet.
//
// sessionActive rejects the request outright rather than starting a delay that
// would land mid-session: neither surface can weaken the other.
func (b *Baseline) Disable(c Clock, id string, sessionActive bool) error {
	r, ok := b.Rules[id]
	if !ok || !r.Enabled {
		return nil // already off; nothing to wait for
	}
	if sessionActive {
		return ErrWouldWeaken
	}
	r.Pending = r.Pending.Request(c, BaselineDisableDelay)
	return nil
}

// CancelDisable is instant and free.
func (b *Baseline) CancelDisable(id string) {
	if r, ok := b.Rules[id]; ok {
		r.Pending = r.Pending.Cancel()
	}
}

// Tick fires any disable whose countdown has run out. Returns the ids that
// changed so the caller can log them.
func (b *Baseline) Tick(c Clock, serverNow *time.Time) []string {
	var fired []string
	for _, id := range b.IDs() {
		r := b.Rules[id]
		if r.Pending.Available(c, serverNow) {
			r.Enabled = false
			r.Pending = r.Pending.Cancel()
			fired = append(fired, id)
		}
	}
	return fired
}
