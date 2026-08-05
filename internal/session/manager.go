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
	sess     Session
	baseline []string
}

func NewManager(st *store.Store, c Clock, enf Enforcement, cat blocklist.Catalog, baseline []string) *Manager {
	return &Manager{st: st, clock: c, enf: enf, cat: cat, sess: New(), baseline: baseline}
}

// Load restores the session from disk and folds in any reboot that happened
// while the daemon was down. A tampered row is treated as no session rather than
// crashing the daemon — the event log records it either way.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	raw, err := m.st.Get(storeKey)
	if err != nil {
		if err == store.ErrTampered {
			m.event("session_signature_invalid", "{}")
			log.Printf("session row failed its signature; starting idle")
		}
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
			if m.tickLocked() {
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

func (m *Manager) Release() (Session, error) {
	return m.transition("session_escaped", func(s Session) (Session, error) {
		return s.Release(m.clock, nil)
	})
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
	for _, id := range m.baseline {
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
	for _, id := range m.baseline {
		rules = append(rules, enforce.Rule{ListID: id, Source: enforce.Baseline})
	}
	if m.sess.Active() {
		for _, id := range m.sess.BlocklistIDs {
			rules = append(rules, enforce.Rule{ListID: id, Source: enforce.Session})
		}
	}
	return enforce.Union(m.cat, rules)
}

func (m *Manager) Baseline() []string { return m.baseline }

func (m *Manager) persistLocked() error {
	b, err := json.Marshal(m.sess)
	if err != nil {
		return err
	}
	return m.st.Put(storeKey, string(b))
}

func (m *Manager) event(kind, data string) {
	if _, err := m.st.Append(kind, data); err != nil {
		log.Printf("event log: %v", err)
	}
}
