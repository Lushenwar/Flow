package enforce

import (
	"slices"
	"testing"

	"github.com/Lushenwar/Flow/internal/blocklist"
)

var cat = blocklist.Presets()

func TestUnionNeverUnblocks(t *testing.T) {
	eff := Union(cat, []Rule{
		{"preset.adult", Baseline},
		{"preset.video", Session},
	})
	for _, d := range []string{"pornhub.com", "youtube.com"} {
		if _, ok := eff.Domains[d]; !ok {
			t.Fatalf("%q missing from the union", d)
		}
	}
}

// The rule the whole attribution model exists for.
func TestOverlapAttributesToTheLongerLivedSource(t *testing.T) {
	eff := Union(cat, []Rule{
		{"preset.video", Session},
		{"preset.video", Baseline},
	})
	if got := eff.Domains["youtube.com"]; got != Baseline {
		t.Fatalf("youtube.com attributed to %q, want baseline — it must survive 0:00", got)
	}
	if got := eff.Lists["preset.video"]; got != Baseline {
		t.Fatalf("list attributed to %q, want baseline", got)
	}
}

func TestSessionEndLiftsOnlySessionRules(t *testing.T) {
	rules := []Rule{
		{"preset.adult", Baseline},
		{"preset.video", Baseline},
		{"preset.video", Session},
		{"preset.doomscroll", Session},
	}
	after := Union(cat, Lift(cat, rules, Session))

	if _, ok := after.Domains["youtube.com"]; !ok {
		t.Fatal("preset.video was on in baseline too — it must stay blocked at 0:00")
	}
	if _, ok := after.Domains["pornhub.com"]; !ok {
		t.Fatal("baseline rules are not touched by session end")
	}
	if _, ok := after.Domains["reddit.com"]; ok {
		t.Fatal("session-only rules must lift at 0:00")
	}
}

func TestUnionCannotReachTheAllowlist(t *testing.T) {
	// Every preset, all at once, from every source.
	var rules []Rule
	for _, id := range cat.IDs() {
		rules = append(rules, Rule{id, Baseline}, Rule{id, Session}, Rule{id, Schedule})
	}
	eff := Union(cat, rules)
	for d := range eff.Domains {
		if blocklist.Allowed(d) {
			t.Fatalf("%q reached the effective set — the allowlist is not permanent", d)
		}
	}
	for _, a := range blocklist.Permanent() {
		if _, ok := eff.Domains[a]; ok {
			t.Fatalf("%q is blocked", a)
		}
	}
}

func TestProcessRulesCarryAttribution(t *testing.T) {
	eff := Union(cat, []Rule{{"preset.gaming", Session}})
	if got := eff.Processes["steam.exe"]; got != Session {
		t.Fatalf("steam.exe attributed to %q", got)
	}
	if !slices.Contains(eff.SortedProcesses(), "steam.exe") {
		t.Fatal("SortedProcesses lost an entry")
	}
}

func TestEmptyRulesIsEmptyNotNilMap(t *testing.T) {
	eff := Union(cat, nil)
	if !eff.Empty() {
		t.Fatal("no rules should mean nothing enforced")
	}
	// Must not panic — the daemon reads these maps every reconcile tick.
	_ = eff.Domains["anything"]
	_ = eff.SortedDomains()
}
