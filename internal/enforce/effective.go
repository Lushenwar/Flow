// Package enforce turns rule sets into applied OS state and keeps them applied.
package enforce

import (
	"sort"
	"strings"

	"github.com/Lushenwar/Flow/internal/blocklist"
)

// Source is which surface owns a rule. Attribution is what stops the Blocking
// screen from appearing to lift a permanent rule when a session ends.
type Source string

const (
	Baseline Source = "baseline"
	Schedule Source = "schedule"
	Session  Source = "session"
)

// rank orders attribution when the same list is active from more than one source.
// The longest-lived source wins: if preset.video is on in baseline AND in the
// session, it is attributed to baseline and survives 0:00.
func rank(s Source) int {
	switch s {
	case Baseline:
		return 3
	case Schedule:
		return 2
	default:
		return 1
	}
}

type Rule struct {
	ListID string
	Source Source
}

// Effective is the union of every active rule set, with attribution.
// A union is the only composition that cannot accidentally unblock something.
type Effective struct {
	Lists     map[string]Source
	Domains   map[string]Source
	Processes map[string]Source

	// DefaultDeny inverts the DNS layer: everything is refused except the
	// permanent allowlist and Allow below. Set by any active list that declares
	// it — "Bedtime" and "Offline" mean everything, and a domain list cannot say
	// that.
	DefaultDeny bool
	// Allow is the user's own escape list, and it is MANDATORY under
	// DefaultDeny. A default-deny resolver with a wrong allowlist is a machine
	// with no internet and a fifteen-minute wait to fix it.
	Allow []string
}

// Permits reports whether a name survives a default-deny window. The permanent
// allowlist is checked by the caller and always wins; this is the user's own.
func (e Effective) Permits(name string) bool {
	for _, a := range e.Allow {
		if name == a || strings.HasSuffix(name, "."+a) {
			return true
		}
	}
	return false
}

func (e Effective) SortedDomains() []string   { return sortedKeys(e.Domains) }
func (e Effective) SortedProcesses() []string { return sortedKeys(e.Processes) }
func (e Effective) SortedLists() []string     { return sortedKeys(e.Lists) }

// Empty means nothing is being enforced. A default-deny window enforces a great
// deal while naming few domains, so it is never empty.
func (e Effective) Empty() bool {
	return !e.DefaultDeny && len(e.Domains) == 0 && len(e.Processes) == 0
}

func sortedKeys(m map[string]Source) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Union folds every rule into one effective set. Never an override, never a
// precedence chain — precedence applies only to the attribution label, never to
// whether something is blocked.
func Union(cat blocklist.Catalog, rules []Rule) Effective {
	eff := Effective{
		Lists:     map[string]Source{},
		Domains:   map[string]Source{},
		Processes: map[string]Source{},
	}
	for _, r := range rules {
		claim(eff.Lists, r.ListID, r.Source)
		if cat.DefaultDenies(r.ListID) {
			eff.DefaultDeny = true
		}
		domains, procs := cat.Resolve(r.ListID)
		for _, d := range domains {
			claim(eff.Domains, d, r.Source)
		}
		for _, p := range procs {
			claim(eff.Processes, p, r.Source)
		}
	}
	return eff
}

func claim(m map[string]Source, key string, s Source) {
	if cur, ok := m[key]; !ok || rank(s) > rank(cur) {
		m[key] = s
	}
}

// Lift removes only the rules a given source contributed, and only where no
// other source also claims them. This is what runs at 0:00.
func Lift(cat blocklist.Catalog, rules []Rule, gone Source) []Rule {
	out := rules[:0:0]
	for _, r := range rules {
		if r.Source != gone {
			out = append(out, r)
		}
	}
	return out
}
