package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
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
	// customKey holds the signed list of user-added domains.
	customKey = "custom"
	// sessionListsKey holds what a session covers by default.
	sessionListsKey = "sessionLists"
	// allowKey holds the user's allowlist for default-deny windows.
	allowKey = "allow"
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
	mu        sync.Mutex
	st        *store.Store
	clock     Clock
	enf       Enforcement
	cat       blocklist.Catalog
	sess      Session
	baseline  *Baseline
	challenge Challenge
	bank      Bank
	schedules *schedule.Set
	// custom is the user's own domains. It is content, not a fourth rule source:
	// it reaches the union as one list carried by a baseline rule, so it inherits
	// the same attribution and the same 15-minute disable delay as a preset.
	custom []string
	// sessionLists is what a session covers by default, edited when calm.
	sessionLists []string
	// allow is the user's escape list for default-deny windows.
	allow []string
	// written is the last value persisted per key, so an unchanged row is not
	// re-signed and rewritten on every tick.
	written map[string]string
}

// ponytail: no commit rate limit. claudev2.md proposed one to bound ARMING/abort
// churn, and writing it made the case against it — aborting and immediately
// reconsidering is the grace window working, not abuse, and a cooldown turns
// that into an error. The row-level dirty check above removes most of the cost
// it was meant to address: churn now writes one row instead of seven. Revisit
// if a profile ever shows the writes mattering.

// DefaultSessionLists is what a session covers before anyone changes it.
var DefaultSessionLists = []string{"preset.video", "preset.doomscroll", "preset.gaming"}

func NewManager(st *store.Store, c Clock, enf Enforcement, cat blocklist.Catalog, baseline []string) *Manager {
	return &Manager{
		st: st, clock: c, enf: enf, cat: cat,
		sess:         New(),
		baseline:     NewBaseline(baseline),
		schedules:    schedule.Defaults(time.Local),
		sessionLists: append([]string(nil), DefaultSessionLists...),
	}
}

// SessionLists is what a session covers by default.
func (m *Manager) SessionLists() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.sessionLists...)
}

// SetSessionLists changes what future sessions cover.
//
// Refused while a session is running, and that refusal is the entire reason
// this lives behind its own verb instead of on the dial:
//
//	Choosing a session's blocklist on the dial would be a bypass: a user
//	mid-craving would deselect YouTube and commit to a session that blocks
//	nothing.
//
// A settings screen you can open mid-craving is the dial with extra clicks, so
// the guard is on the daemon rather than on whether the UI drew the control.
func (m *Manager) SetSessionLists(ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tickLocked()

	if m.sess.Active() || m.sess.State == Break {
		return ErrWouldWeaken
	}
	m.sessionLists = append([]string(nil), ids...)
	sort.Strings(m.sessionLists)
	m.event("session_lists_changed", fmt.Sprintf(`{"count":%d}`, len(ids)))
	return m.persistLocked()
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

	if m.sess.State == Break {
		m.sess.Break.Anchor, _ = m.sess.Break.Anchor.Recover(m.clock)
	}
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

// Snapshot returns the current session, ticked so a read is never stale.
//
// A tick that moves is a real transition — it changes enforcement and it can pay
// the time bank — so it is applied and persisted here rather than left in memory
// for the Run loop to notice a second later. Reads outnumber ticks by a wide
// margin and almost none of them move, so the write happens about as often as
// the state actually changes.
//
// Without this the credit was NOT in the same signed write as the transition,
// which is the one property the crediting rule is built around: a read drove the
// transition into COMPLETE, paid the bank in memory, and an unclean kill before
// the next Run tick lost both.
func (m *Manager) Snapshot() Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tickLocked() {
		m.applyLocked()
		if err := m.persistLocked(); err != nil {
			log.Printf("persist after read-driven transition: %v", err)
		}
	}
	return m.sess
}

func (m *Manager) Clock() Clock { return m.clock }

// Commit starts a session. Blocks apply immediately, during ARMING.
func (m *Manager) Commit(p Plan) (Session, error) {
	return m.transition("session_commit", func(s Session) (Session, error) {
		return s.Commit(m.clock, p)
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
	next, passed := m.sess.Tick(m.clock, nil)
	moved := len(passed) > 0
	if moved {
		m.sess = next
		// One event per state crossed, not just the landing state. A cascade
		// through ARMING -> FOCUS -> COMPLETE has to leave FOCUS in the log:
		// that is the moment the lock became irreversible.
		for _, st := range passed {
			m.event("session_"+string(st), "{}")
		}
		m.creditLocked()
	}
	if m.bank.Tick(m.clock, nil) {
		// The window closed. Enforcement hard re-locks on the same tick.
		m.event("bank_spend_ended", "{}")
		moved = true
	}
	return moved
}

// creditLocked pays the time bank for every focus interval that has finished
// and not yet been paid for. CreditedIntervals is written in the same signed row
// as the state, so a crash between the two cannot double-pay or skip.
//
// A loop rather than a single credit because Tick cascades: a daemon that was
// down for two hours can cross several pomodoro intervals in one tick, and
// crediting only the final transition would quietly lose the rest. Comparing a
// count against IntervalsCompleted makes the arithmetic independent of how many
// steps it took to get here.
//
// Aborted and escaped sessions never reach the end of an interval and so earn
// nothing — otherwise "start, escape immediately" becomes a minute farm.
func (m *Manager) creditLocked() {
	for m.sess.CreditedIntervals < m.sess.IntervalsCompleted() {
		interval := m.sess.Target.Duration
		earned := time.Duration(float64(interval) * CreditRate)
		m.bank.Credit(interval)
		m.sess.CreditedIntervals++
		m.event("bank_credited", fmt.Sprintf(`{"seconds":%d,"interval":%d}`,
			int(earned.Seconds()), m.sess.CreditedIntervals))
	}
}

func (m *Manager) checkpointLocked() {
	// Pending baseline disables checkpoint whether or not a session is running:
	// their countdown must survive a reboot too.
	for _, id := range m.baseline.IDs() {
		if r := m.baseline.Rules[id]; r.Pending.Requested {
			r.Pending.Deadline.Anchor = r.Pending.Deadline.Anchor.Checkpoint(m.clock)
		}
	}
	// A break is not Active — nothing is enforced during one — but its countdown
	// still has to survive a reboot, or the machine comes back owing intervals
	// with no idea when the next one starts.
	if m.sess.State == Break {
		m.sess.Break.Anchor = m.sess.Break.Anchor.Checkpoint(m.clock)
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
// A live bank spend is the single exception in the whole app — it suppresses
// the rules you earned the right to suspend, for the window that was paid for
// up front. It can only start from IDLE and cannot be cancelled, so it never
// weakens a lock.
//
// It suppresses SESSION and SCHEDULE rules only. Baseline survives a spend,
// because baseline is not "off" and never was:
//
//	A user in IDLE with gambling and adult content on baseline is being
//	protected right now.
//
// Earning minutes by focusing buys back the things you are avoiding for
// productivity. It does not buy back the things you asked to be permanently
// protected from — collapsing the two would make the bank a supported path to
// the one outcome the app exists to prevent.
func (m *Manager) rulesLocked() []enforce.Rule {
	var rules []enforce.Rule
	for _, id := range m.baseline.EnabledIDs() {
		rules = append(rules, enforce.Rule{ListID: id, Source: enforce.Baseline})
	}
	if m.bank.Spending(m.clock, nil) {
		return rules
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

// catalogLocked is the preset catalog plus the user's own list.
//
// ponytail: copies the map on every call rather than caching an assembled
// catalog and invalidating it. Eleven entries, once a second at worst — a cache
// here would cost more in staleness bugs than it saves in allocations.
func (m *Manager) catalogLocked() blocklist.Catalog {
	if len(m.custom) == 0 {
		return m.cat
	}
	out := make(blocklist.Catalog, len(m.cat)+1)
	for id, l := range m.cat {
		out[id] = l
	}
	out[blocklist.CustomListID] = blocklist.CustomList(m.custom)
	return out
}

// applyLocked recomputes the union and hands it to the enforcer. Never an
// override, never a precedence chain.
func (m *Manager) applyLocked() {
	m.enf.Set(m.effectiveLocked())
}

// effectiveLocked is the union plus the user's allowlist, which is not a rule
// source — it only carves holes in a default-deny window.
func (m *Manager) effectiveLocked() enforce.Effective {
	eff := enforce.Union(m.catalogLocked(), m.rulesLocked())
	eff.Allow = append([]string(nil), m.allow...)
	return eff
}

// Effective is what /api/state reports, computed daemon-side. The UI must never
// derive attribution.
func (m *Manager) Effective() enforce.Effective {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.effectiveLocked()
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
//
// Editing one whose window is currently live is refused: the rules it
// contributes are in the effective set right now, and rewriting its hours would
// weaken enforcement on the same terms a baseline toggle would. There is no
// delay to invent here — the window ends on its own, which is friction the clock
// already provides.
func (m *Manager) PutSchedule(s schedule.Schedule) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.scheduleLiveLocked(s.ID) {
		return ErrWouldWeaken
	}
	m.schedules.Add(s)
	m.event("schedule_saved", fmt.Sprintf(`{"id":%q,"enabled":%t}`, s.ID, s.Enabled))
	m.applyLocked()
	return m.persistLocked()
}

// DeleteSchedule removes a schedule, and refuses while its window is live.
//
// Direction-aware like every other mutation: deleting an inactive schedule
// weakens nothing, because it is not enforcing anything yet.
func (m *Manager) DeleteSchedule(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.scheduleLiveLocked(id) {
		return ErrWouldWeaken
	}
	if !m.schedules.Remove(id) {
		return nil // not there; nothing to do
	}
	m.event("schedule_deleted", fmt.Sprintf(`{"id":%q}`, id))
	m.applyLocked()
	return m.persistLocked()
}

// scheduleLiveLocked reports whether a schedule's window is in force right now.
func (m *Manager) scheduleLiveLocked(id string) bool {
	for _, s := range m.schedules.Active(m.clock.Wall()) {
		if s.ID == id {
			return true
		}
	}
	return false
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

// Allow returns the user's escape list for default-deny windows.
func (m *Manager) Allow() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.allow...)
}

// AddAllow widens the escape list, and is refused while a default-deny window is
// live.
//
// This is the mirror of the custom blocklist's rule, and the direction is
// reversed for the same reason: under default-deny, ADDING to the allowlist is
// what weakens enforcement. Without the guard, "block everything" is one
// text-box entry away from "block everything except the site I want".
//
// Editing it while nothing is inverted is free — it enforces nothing then.
func (m *Manager) AddAllow(raws []string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tickLocked()

	if m.effectiveLocked().DefaultDeny {
		return nil, ErrWouldWeaken
	}

	have := map[string]bool{}
	for _, d := range m.allow {
		have[d] = true
	}
	var added []string
	for _, raw := range raws {
		d, err := blocklist.NormalizeAllowDomain(raw)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", strings.TrimSpace(raw), err)
		}
		if have[d] {
			continue
		}
		have[d] = true
		added = append(added, d)
	}
	if len(added) == 0 {
		return nil, nil
	}
	if len(m.allow)+len(added) > blocklist.MaxCustomDomains {
		return nil, blocklist.ErrTooManyCustom
	}

	m.allow = append(m.allow, added...)
	sort.Strings(m.allow)
	m.event("allow_added", fmt.Sprintf(`{"count":%d}`, len(added)))
	m.applyLocked()
	return added, m.persistLocked()
}

// RemoveAllow narrows the escape list. Strengthening, so it is always allowed —
// including mid-window, where it takes effect immediately.
func (m *Manager) RemoveAllow(raw string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	d, err := blocklist.NormalizeAllowDomain(raw)
	if err != nil {
		return err
	}
	kept := m.allow[:0:0]
	for _, existing := range m.allow {
		if existing != d {
			kept = append(kept, existing)
		}
	}
	if len(kept) == len(m.allow) {
		return nil
	}
	m.allow = kept
	m.event("allow_removed", fmt.Sprintf(`{"domain":%q}`, d))
	m.applyLocked()
	return m.persistLocked()
}

// Custom returns the user's own blocked domains.
func (m *Manager) Custom() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.custom...)
}

// AddCustom adds domains to the user's list. Strengthening, so it is instant and
// allowed in every state, mid-session included — the same rule that lets a
// baseline category be switched on during a session.
//
// Adding also enables the custom rule. A site you just typed in should be
// blocked when you press the button, not when you remember to flip a second
// switch; and enabling is itself a strengthening operation, so nothing is
// weakened by doing it for you.
//
// All-or-nothing on a bad entry. Partial success would mean reporting "3 of 5
// added" and leaving the user to work out which two, on a screen whose entire
// job is telling you what is enforced.
func (m *Manager) AddCustom(raws []string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	have := map[string]bool{}
	for _, d := range m.custom {
		have[d] = true
	}

	var added []string
	for _, raw := range raws {
		d, err := blocklist.NormalizeDomain(raw)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", strings.TrimSpace(raw), err)
		}
		if have[d] {
			continue // already blocked; not an error, just nothing to do
		}
		have[d] = true
		added = append(added, d)
	}
	if len(added) == 0 {
		return nil, nil
	}
	if len(m.custom)+len(added) > blocklist.MaxCustomDomains {
		return nil, blocklist.ErrTooManyCustom
	}

	m.custom = append(m.custom, added...)
	sort.Strings(m.custom)
	m.baseline.Enable(blocklist.CustomListID)

	m.event("custom_added", fmt.Sprintf(`{"count":%d}`, len(added)))
	m.applyLocked()
	return added, m.persistLocked()
}

// RemoveCustom takes a domain back off the list, and refuses while that list is
// being enforced.
//
// This is the asymmetry applied to content rather than categories, and it needs
// no new machinery: turn the Custom sites row off first, which takes the usual
// fifteen minutes, then edit. Without the guard the list would be an off switch
// with extra steps — add the site you actually struggle with, then delete it the
// moment you want it back.
func (m *Manager) RemoveCustom(raw string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tickLocked()
	m.tickBaselineLocked()

	// Checked on the rule rather than on the effective set, so that a bank spend —
	// which suppresses enforcement for its window — is not a hole you can edit through.
	if r, ok := m.baseline.Rules[blocklist.CustomListID]; ok && r.Enabled {
		return ErrWouldWeaken
	}
	// And separately for a session or schedule that names the list directly.
	if _, live := enforce.Union(m.catalogLocked(), m.rulesLocked()).Lists[blocklist.CustomListID]; live {
		return ErrWouldWeaken
	}

	d, err := blocklist.NormalizeDomain(raw)
	if err != nil {
		return err
	}
	kept := m.custom[:0:0]
	for _, existing := range m.custom {
		if existing != d {
			kept = append(kept, existing)
		}
	}
	if len(kept) == len(m.custom) {
		return nil // not there; nothing to do
	}
	m.custom = kept

	m.event("custom_removed", fmt.Sprintf(`{"domain":%q}`, d))
	m.applyLocked()
	return m.persistLocked()
}

func (m *Manager) tickBaselineLocked() bool {
	fired := m.baseline.Tick(m.clock, nil)
	for _, id := range fired {
		m.event("baseline_disabled", fmt.Sprintf(`{"id":%q}`, id))
	}
	return len(fired) > 0
}

// persistLocked writes the rows whose contents actually changed.
//
// It used to marshal and write all six every time, so a single dial tap cost
// six signed writes plus an event — and ARMING/abort churn is unbounded. The
// hash comparison is cheaper than the write it avoids, and the correctness
// argument is simple: a row nobody changed does not need re-signing, because
// the signature covers contents rather than freshness.
func (m *Manager) persistLocked() error {
	rows := []struct {
		key string
		val any
	}{
		{storeKey, m.sess},
		{baselineKey, m.baseline},
		{bankKey, m.bank},
		{schedulesKey, m.schedules},
		{customKey, m.custom},
		{sessionListsKey, m.sessionLists},
		{allowKey, m.allow},
	}
	if m.written == nil {
		m.written = make(map[string]string, len(rows))
	}
	for _, r := range rows {
		b, err := json.Marshal(r.val)
		if err != nil {
			return err
		}
		if m.written[r.key] == string(b) {
			continue
		}
		if err := m.st.Put(r.key, string(b)); err != nil {
			return err
		}
		m.written[r.key] = string(b)
	}
	return nil
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

	// A tampered custom row keeps the startup default of nothing, which is the
	// safe direction for the store but the wrong one for the user: it silently
	// unblocks their own list. The event is what makes that visible.
	if raw, err := m.st.Get(customKey); err == nil {
		var domains []string
		if json.Unmarshal([]byte(raw), &domains) == nil {
			m.custom = domains
		}
	} else if err == store.ErrTampered {
		m.event("custom_signature_invalid", "{}")
		log.Printf("custom domain row failed its signature; the list is empty until you re-add them")
	}

	// A tampered allow row empties the list, which is the SAFE direction for a
	// blocker and the harsh one for the user: a default-deny window with no
	// allowlist refuses everything but the permanent list. The event is what
	// makes that visible rather than baffling.
	if raw, err := m.st.Get(allowKey); err == nil {
		var domains []string
		if json.Unmarshal([]byte(raw), &domains) == nil {
			m.allow = domains
		}
	} else if err == store.ErrTampered {
		m.event("allow_signature_invalid", "{}")
		log.Printf("allowlist row failed its signature; it is empty until you re-add it")
	}

	// A tampered row keeps the defaults, which is the safe direction: a session
	// covering more than the user last chose is a stronger lock, not a weaker one.
	if raw, err := m.st.Get(sessionListsKey); err == nil {
		var ids []string
		if json.Unmarshal([]byte(raw), &ids) == nil && len(ids) > 0 {
			m.sessionLists = ids
		}
	} else if err == store.ErrTampered {
		m.event("session_lists_signature_invalid", "{}")
		log.Printf("session list row failed its signature; falling back to the defaults")
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
//
// Compared by OFFSET, not by name. Names cannot do this job on the platform this
// runs on: time.Local.String() returns the literal "Local" on Windows, and so
// does the TZ captured at creation, so `s.TZ != system` was "Local" != "Local"
// and the branch was dead — the detection for the exact attack it was written
// for could never fire. That is the same trap the pinning itself already fell
// into and fixed with OffsetSeconds; only the logging half was left behind.
func (m *Manager) checkTimezoneLocked() {
	now := m.clock.Wall()
	_, systemOffset := now.Zone()
	for _, s := range m.schedules.Active(now) {
		if s.OffsetSeconds == systemOffset {
			continue
		}
		m.event("schedule_timezone_changed", fmt.Sprintf(
			`{"id":%q,"pinnedOffset":%d,"systemOffset":%d,"holdsUntil":%q}`,
			s.ID, s.OffsetSeconds, systemOffset, s.EndsAt(now).Format(time.RFC3339)))
		log.Printf("schedule %s pinned to UTC%+d but system is UTC%+d; holding until %s",
			s.ID, s.OffsetSeconds/3600, systemOffset/3600, s.EndsAt(now).Format(time.RFC3339))
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
