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
	"github.com/Lushenwar/Flow/internal/schedule"
	"github.com/Lushenwar/Flow/internal/store"
)

const (
	// storeKey holds the signed session row.
	storeKey = "session"
	// baselineKey holds the signed baseline row.
	baselineKey = "baseline"
	// bankKey and schedulesKey hold the signed bank and schedule rows.
	bankKey      = "bank"
	schedulesKey = "schedules"
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
	bank      Bank
	schedules *schedule.Set
}

func NewManager(st *store.Store, c Clock, enf Enforcement, cat blocklist.Catalog, baseline []string) *Manager {
	return &Manager{
		st: st, clock: c, enf: enf, cat: cat,
		sess:      New(),
		baseline:  NewBaseline(baseline),
		schedules: schedule.Defaults(time.Local),
	}
}

// Load restores the session from disk and folds in any reboot that happened
// while the daemon was down. A tampered row is treated as no session rather than
// crashing the daemon — the event log records it either way.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.loadBaselineLocked()
	m.loadExtrasLocked()

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
		m.creditLocked()
	}
	if m.bank.Tick(m.clock, nil) {
		// The window closed. Enforcement hard re-locks on the same tick.
		m.event("bank_spend_ended", "{}")
		moved = true
	}
	return moved
}

// creditLocked pays the time bank on the transition into COMPLETE, exactly once.
// The Credited flag is written in the same signed row as the state, so a crash
// between the two cannot double-pay or skip.
//
// Aborted and escaped sessions never reach COMPLETE and so earn nothing —
// otherwise "start, escape immediately" becomes a minute farm.
func (m *Manager) creditLocked() {
	if m.sess.State != Complete || m.sess.Credited {
		return
	}
	earned := time.Duration(float64(m.sess.Target.Duration) * CreditRate)
	m.bank.Credit(m.sess.Target.Duration)
	m.sess.Credited = true
	m.event("bank_credited", fmt.Sprintf(`{"seconds":%d}`, int(earned.Seconds())))
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

// rulesLocked is the union input: baseline ∪ session ∪ schedules.
//
// A live bank spend is the single exception in the whole app — it returns
// nothing, opening the blocklist for the window that was paid for up front. It
// can only start from IDLE and cannot be cancelled, so it never weakens a lock.
func (m *Manager) rulesLocked() []enforce.Rule {
	if m.bank.Spending(m.clock, nil) {
		return nil
	}
	var rules []enforce.Rule
	for _, id := range m.baseline.EnabledIDs() {
		rules = append(rules, enforce.Rule{ListID: id, Source: enforce.Baseline})
	}
	for _, id := range m.schedules.ActiveListIDs(m.clock.Wall()) {
		rules = append(rules, enforce.Rule{ListID: id, Source: enforce.Schedule})
	}
	if m.sess.Active() {
		for _, id := range m.sess.BlocklistIDs {
			rules = append(rules, enforce.Rule{ListID: id, Source: enforce.Session})
		}
	}
	return rules
}

// applyLocked recomputes the union and hands it to the enforcer. Never an
// override, never a precedence chain.
func (m *Manager) applyLocked() {
	m.enf.Set(enforce.Union(m.cat, m.rulesLocked()))
}

// Effective is what /api/state reports, computed daemon-side. The UI must never
// derive attribution.
func (m *Manager) Effective() enforce.Effective {
	m.mu.Lock()
	defer m.mu.Unlock()
	return enforce.Union(m.cat, m.rulesLocked())
}

// Bank returns the balance and any open recreation window.
func (m *Manager) Bank() (balance, remaining time.Duration, spending bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bank.Tick(m.clock, nil)
	return m.bank.Balance(), m.bank.Remaining(m.clock, nil), m.bank.Spending(m.clock, nil)
}

// SpendBank opens recreation time. Requires IDLE; the balance is deducted up
// front and the window cannot be cancelled to bank the remainder.
func (m *Manager) SpendBank(d time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tickLocked()

	if err := m.bank.StartSpend(m.clock, d, m.sess.State); err != nil {
		return err
	}
	m.event("bank_spend_started", fmt.Sprintf(`{"seconds":%d}`, int(d.Seconds())))
	m.applyLocked()
	return m.persistLocked()
}

// Schedules returns the configured recurring locks and which are in force.
func (m *Manager) Schedules() ([]schedule.Schedule, []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	active := []string{}
	for _, s := range m.schedules.Active(m.clock.Wall()) {
		active = append(active, s.ID)
	}
	return append([]schedule.Schedule(nil), m.schedules.Schedules...), active
}

// PutSchedule adds or replaces a schedule.
func (m *Manager) PutSchedule(s schedule.Schedule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.schedules.Add(s)
	m.event("schedule_saved", fmt.Sprintf(`{"id":%q,"enabled":%t}`, s.ID, s.Enabled))
	m.applyLocked()
	return m.persistLocked()
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
	if err := m.st.Put(baselineKey, string(bl)); err != nil {
		return err
	}
	bk, err := json.Marshal(m.bank)
	if err != nil {
		return err
	}
	if err := m.st.Put(bankKey, string(bk)); err != nil {
		return err
	}
	sc, err := json.Marshal(m.schedules)
	if err != nil {
		return err
	}
	return m.st.Put(schedulesKey, string(sc))
}

// loadExtrasLocked restores the bank and schedules. A tampered row is refused
// rather than trusted: for the bank that means the balance goes to zero, which
// costs earned recreation time but cannot manufacture it.
func (m *Manager) loadExtrasLocked() {
	if raw, err := m.st.Get(bankKey); err == nil {
		var b Bank
		if json.Unmarshal([]byte(raw), &b) == nil {
			m.bank = b
			if m.bank.Window != nil {
				m.bank.Window.Anchor, _ = m.bank.Window.Anchor.Recover(m.clock)
			}
		}
	} else if err == store.ErrTampered {
		m.event("bank_signature_invalid", "{}")
		log.Printf("bank row failed its signature; balance reset to zero")
	}

	if raw, err := m.st.Get(schedulesKey); err == nil {
		var set schedule.Set
		if json.Unmarshal([]byte(raw), &set) == nil && len(set.Schedules) > 0 {
			m.schedules = &set
		}
	} else if err == store.ErrTampered {
		m.event("schedules_signature_invalid", "{}")
		log.Printf("schedules row failed its signature; keeping the seeded defaults")
	}
	m.checkTimezoneLocked()
}

// checkTimezoneLocked logs a tamper event when the system zone no longer matches
// the zone an active schedule was pinned to.
//
// No enforcement change is needed: ActiveAt always evaluates in the pinned zone,
// so the window already holds for its full length. The event exists because the
// log is more useful than the punishment.
func (m *Manager) checkTimezoneLocked() {
	now := m.clock.Wall()
	system := time.Local.String()
	for _, s := range m.schedules.Active(now) {
		if s.TZ != system {
			m.event("schedule_timezone_changed", fmt.Sprintf(
				`{"id":%q,"pinned":%q,"system":%q,"holdsUntil":%q}`,
				s.ID, s.TZ, system, s.EndsAt(now).Format(time.RFC3339)))
			log.Printf("schedule %s pinned to %s but system is %s; holding until %s",
				s.ID, s.TZ, system, s.EndsAt(now).Format(time.RFC3339))
		}
	}
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
