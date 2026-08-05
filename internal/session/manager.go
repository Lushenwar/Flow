package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Lushenwar/Flow/internal/blocklist"
	"github.com/Lushenwar/Flow/internal/enforce"
	"github.com/Lushenwar/Flow/internal/store"
)

const (
	// storeKey holds the signed session row.
	storeKey = "session"
	// baselineKey holds the signed baseline row.
	baselineKey = "baseline"
	// tickEvery drives state transitions. Sub-second precision is pointless for
	// a 25-minute lock.
	tickEvery = time.Second
	// checkpointEvery persists the shutdown-gap markers.
	checkpointEvery = 5 * time.Second
)

// Enforcement is the slice of the enforcer the manager drives.
type Enforcement interface{ Set(enforce.Effective) }

// Manager owns the session and is the only thing that writes it. The UI owns
// zero authority: delete the UI mid-session and enforcement is unaffected.
type Manager struct {
	mu       sync.Mutex
	st       *store.Store
	clock    Clock
	enf      Enforcement
	cat      blocklist.Catalog
	sess      Session
	baseline  *Baseline
	challenge Challenge
}

func NewManager(st *store.Store, c Clock, enf Enforcement, cat blocklist.Catalog, baseline []string) *Manager {
	return &Manager{st: st, clock: c, enf: enf, cat: cat, sess: New(), baseline: NewBaseline(baseline)}
}

// Load restores the session from disk and folds in any reboot that happened
// while the daemon was down. A tampered row is treated as no session rather than
// crashing the daemon — the event log records it either way.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.loadBaselineLocked()

	raw, err := m.st.Get(storeKey)
	if err != nil {
		if err == store.ErrTampered {
			m.event("session_signature_invalid", "{}")
			log.Printf("session row failed its signature; starting idle")
		}
		m.tickBaselineLocked()
		m.applyLocked()
		return nil // no row yet is the normal first-run case
	}
	var s Session
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return fmt.Errorf("decode session: %w", err)
	}
	m.sess = s

	if m.sess.Active() {
		if a, rebooted := m.sess.Target.Anchor.Recover(m.clock); rebooted {
			m.sess.Target.Anchor = a
			m.sess.Grace.Anchor, _ = m.sess.Grace.Anchor.Recover(m.clock)
			if m.sess.Escape.Requested {
				m.sess.Escape.Deadline.Anchor, _ = m.sess.Escape.Deadline.Anchor.Recover(m.clock)
			}
			m.event("boot_recovered", fmt.Sprintf(`{"remainingSeconds":%d}`,
				int(m.sess.Target.Remaining(m.clock, nil).Seconds())))
			log.Printf("recovered session across reboot: %v remaining", m.sess.Target.Remaining(m.clock, nil))
		}
	}
	m.tickLocked()
	m.tickBaselineLocked()
	m.applyLocked()
	return m.persistLocked()
}

// Run drives transitions and checkpoints until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) {
	tick := time.NewTicker(tickEvery)
	check := time.NewTicker(checkpointEvery)
	defer tick.Stop()
	defer check.Stop()

	for {
		select {
		case <-ctx.Done():
			m.mu.Lock()
			m.checkpointLocked()
			m.persistLocked()
			m.mu.Unlock()
			return
		case <-tick.C:
			m.mu.Lock()
			sessionMoved := m.tickLocked()
			baselineMoved := m.tickBaselineLocked()
			if sessionMoved || baselineMoved {
				m.applyLocked()
				m.persistLocked()
			}
			m.mu.Unlock()
		case <-check.C:
			m.mu.Lock()
			m.checkDriftLocked()
			m.checkpointLocked()
			m.persistLocked()
			m.mu.Unlock()
		}
	}
}

func (m *Manager) Snapshot() Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tickLocked()
	return m.sess
}

func (m *Manager) Clock() Clock { return m.clock }

// Commit starts a session. Blocks apply immediately, during ARMING.
func (m *Manager) Commit(mode Mode, dur time.Duration, ids []string, grace time.Duration, penalty bool) (Session, error) {
	return m.transition("session_commit", func(s Session) (Session, error) {
		return s.Commit(m.clock, mode, dur, ids, grace, penalty)
	})
}

func (m *Manager) Abort() (Session, error) {
	return m.transition("session_abort", func(s Session) (Session, error) { return s.Abort() })
}

func (m *Manager) RequestEscape(after time.Duration) (Session, error) {
	return m.transition("session_escape_requested", func(s Session) (Session, error) {
		return s.RequestEscape(m.clock, after)
	})
}

// Challenge returns the typed challenge, generating one on first ask. It is
// refused until the escape delay has run out — the delay is the friction, the
// typing is only proof you meant it.
//
// ponytail: held in memory, not the store. A daemon restart mints a new one,
// which costs a retype and never shortens the wait.
func (m *Manager) Challenge() (Challenge, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tickLocked()

	if m.sess.State != Releasing || !m.sess.Escape.Available(m.clock, nil) {
		return Challenge{}, ErrLocked
	}
	if m.challenge.ID == "" {
		c, err := NewChallenge()
		if err != nil {
			return Challenge{}, err
		}
		m.challenge = c
	}
	return m.challenge, nil
}

// VerifyEscape ends the session early. Accepted only at or after availableAt,
// and only for the exact challenge text.
func (m *Manager) VerifyEscape(id, typed string) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tickLocked()

	if m.sess.State != Releasing || !m.sess.Escape.Available(m.clock, nil) {
		return m.sess, ErrLocked
	}
	if m.challenge.ID == "" || !m.challenge.Matches(id, typed) {
		m.event("escape_challenge_failed", "{}")
		return m.sess, ErrChallenge
	}

	next, err := m.sess.Release(m.clock, nil)
	if err != nil {
		return m.sess, err
	}
	m.sess = next
	m.challenge = Challenge{}
	m.event("session_ended_early", "{}")
	m.applyLocked()
	return m.sess, m.persistLocked()
}

func (m *Manager) Ack() (Session, error) {
	return m.transition("session_acked", func(s Session) (Session, error) { return s.Ack(), nil })
}

// transition is the single write path: mutate, log, re-apply enforcement, persist.
// Every transition is a signed write.
func (m *Manager) transition(kind string, fn func(Session) (Session, error)) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.tickLocked()
	next, err := fn(m.sess)
	if err != nil {
		return m.sess, err
	}
	m.sess = next
	m.event(kind, fmt.Sprintf(`{"state":%q}`, next.State))
	m.applyLocked()
	return m.sess, m.persistLocked()
}

func (m *Manager) tickLocked() bool {
	next, moved := m.sess.Tick(m.clock, nil)
	if moved {
		m.sess = next
		m.event("session_"+string(next.State), "{}")
	}
	return moved
}

func (m *Manager) checkpointLocked() {
	// Pending baseline disables checkpoint whether or not a session is running:
	// their countdown must survive a reboot too.
	for _, id := range m.baseline.IDs() {
		if r := m.baseline.Rules[id]; r.Pending.Requested {
			r.Pending.Deadline.Anchor = r.Pending.Deadline.Anchor.Checkpoint(m.clock)
		}
	}
	if !m.sess.Active() {
		return
	}
	m.sess.Target.Anchor = m.sess.Target.Anchor.Checkpoint(m.clock)
	m.sess.Grace.Anchor = m.sess.Grace.Anchor.Checkpoint(m.clock)
	if m.sess.Escape.Requested {
		m.sess.Escape.Deadline.Anchor = m.sess.Escape.Deadline.Anchor.Checkpoint(m.clock)
	}
}

// checkDriftLocked logs clock tampering. The penalty is capped, pre-disclosed,
// and applied at most once; the event is recorded either way, because the log is
// more useful than the punishment.
func (m *Manager) checkDriftLocked() {
	if !m.sess.Active() {
		return
	}
	d, drifted := m.sess.Target.Anchor.Drift(m.clock)
	if !drifted {
		return
	}
	next, applied := m.sess.ApplyPenalty()
	m.sess = next
	m.event("clock_drift", fmt.Sprintf(`{"driftSeconds":%d,"penaltyApplied":%t}`,
		int(d.Seconds()), applied))
	log.Printf("clock drift of %v detected (penalty applied: %t)", d, applied)
}

// applyLocked recomputes the union and hands it to the enforcer. Baseline ∪
// session, never an override.
func (m *Manager) applyLocked() {
	var rules []enforce.Rule
	for _, id := range m.baseline.EnabledIDs() {
		rules = append(rules, enforce.Rule{ListID: id, Source: enforce.Baseline})
	}
	if m.sess.Active() {
		for _, id := range m.sess.BlocklistIDs {
			rules = append(rules, enforce.Rule{ListID: id, Source: enforce.Session})
		}
	}
	m.enf.Set(enforce.Union(m.cat, rules))
}

// Effective is what /api/state reports, computed daemon-side. The UI must never
// derive attribution.
func (m *Manager) Effective() enforce.Effective {
	m.mu.Lock()
	defer m.mu.Unlock()
	var rules []enforce.Rule
	for _, id := range m.baseline.EnabledIDs() {
		rules = append(rules, enforce.Rule{ListID: id, Source: enforce.Baseline})
	}
	if m.sess.Active() {
		for _, id := range m.sess.BlocklistIDs {
			rules = append(rules, enforce.Rule{ListID: id, Source: enforce.Session})
		}
	}
	return enforce.Union(m.cat, rules)
}

// Baseline returns a snapshot of the rules, ticked so a fired disable is never
// reported as still pending.
func (m *Manager) Baseline() []BaselineRule {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tickBaselineLocked()

	out := make([]BaselineRule, 0, len(m.baseline.Rules))
	for _, id := range m.baseline.IDs() {
		out = append(out, *m.baseline.Rules[id])
	}
	return out
}

// EnableBaseline is immediate and always allowed.
func (m *Manager) EnableBaseline(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.baseline.Enable(id)
	m.event("baseline_enabled", fmt.Sprintf(`{"id":%q}`, id))
	m.applyLocked()
	return m.persistLocked()
}

// DisableBaseline starts the 15-minute delay. The rule stays enforced throughout.
func (m *Manager) DisableBaseline(id string) (*time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tickLocked()

	if err := m.baseline.Disable(m.clock, id, m.sess.Active()); err != nil {
		return nil, err
	}
	m.event("baseline_disable_requested", fmt.Sprintf(`{"id":%q}`, id))
	if err := m.persistLocked(); err != nil {
		return nil, err
	}
	r, ok := m.baseline.Rules[id]
	if !ok {
		return nil, nil
	}
	return r.Pending.AvailableAt(m.clock, nil), nil
}

// CancelBaselineDisable is instant and free.
func (m *Manager) CancelBaselineDisable(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.baseline.CancelDisable(id)
	m.event("baseline_disable_cancelled", fmt.Sprintf(`{"id":%q}`, id))
	return m.persistLocked()
}

func (m *Manager) tickBaselineLocked() bool {
	fired := m.baseline.Tick(m.clock, nil)
	for _, id := range fired {
		m.event("baseline_disabled", fmt.Sprintf(`{"id":%q}`, id))
	}
	return len(fired) > 0
}

func (m *Manager) persistLocked() error {
	b, err := json.Marshal(m.sess)
	if err != nil {
		return err
	}
	if err := m.st.Put(storeKey, string(b)); err != nil {
		return err
	}
	bl, err := json.Marshal(m.baseline)
	if err != nil {
		return err
	}
	return m.st.Put(baselineKey, string(bl))
}

// loadBaselineLocked restores the rules, preserving the pending-disable
// countdowns across a restart. The countdown surviving is the point: a
// disable request that resets when you close the app is not friction.
func (m *Manager) loadBaselineLocked() {
	raw, err := m.st.Get(baselineKey)
	if err != nil {
		if err == store.ErrTampered {
			m.event("baseline_signature_invalid", "{}")
			log.Printf("baseline row failed its signature; keeping the startup defaults")
		}
		return // no row yet: the -block defaults stand
	}
	var b Baseline
	if err := json.Unmarshal([]byte(raw), &b); err != nil || b.Rules == nil {
		log.Printf("baseline row unreadable; keeping the startup defaults")
		return
	}
	// Anything named at startup but absent from the row is added, so a new
	// preset in -block shows up rather than silently doing nothing.
	for id, r := range m.baseline.Rules {
		if _, ok := b.Rules[id]; !ok {
			b.Rules[id] = r
		}
	}
	m.baseline = &b

	for _, id := range m.baseline.IDs() {
		r := m.baseline.Rules[id]
		if r.Pending.Requested {
			r.Pending.Deadline.Anchor, _ = r.Pending.Deadline.Anchor.Recover(m.clock)
		}
	}
}

func (m *Manager) event(kind, data string) {
	if _, err := m.st.Append(kind, data); err != nil {
		log.Printf("event log: %v", err)
	}
}
