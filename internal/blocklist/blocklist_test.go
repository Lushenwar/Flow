package blocklist

import (
	"slices"
	"testing"
)

func TestAllowedMatchesOnLabelBoundaries(t *testing.T) {
	allowed := []string{
		"localhost", "cdc.gov", "www.cdc.gov", "GOV", "nhs.uk",
		"988lifeline.org", "windowsupdate.com", "5.0.0.127.in-addr.arpa",
	}
	for _, d := range allowed {
		if !Allowed(d) {
			t.Errorf("%q must never be blockable", d)
		}
	}

	// The dangerous near-misses: a domain that merely contains an allowlisted
	// string must not inherit its immunity.
	blocked := []string{
		"notgov.com", "gov.evil.com", "mygov.io", "nhs.uk.phishing.net",
		"youtube.com", "reddit.com", "windowsupdate.com.attacker.net",
	}
	for _, d := range blocked {
		if Allowed(d) {
			t.Errorf("%q must not be allowlisted by substring", d)
		}
	}
}

func TestResolveComposesWithoutCycling(t *testing.T) {
	c := Presets()
	work, _ := c.Resolve("preset.work")
	video, _ := c.Resolve("preset.video")

	for _, d := range video {
		if !slices.Contains(work, d) {
			t.Fatalf("preset.work must contain everything in preset.video, missing %q", d)
		}
	}
	if !slices.Contains(work, "reddit.com") {
		t.Fatal("preset.work must fold in doomscroll")
	}
	_, procs := c.Resolve("preset.work")
	if !slices.Contains(procs, "steam.exe") {
		t.Fatal("preset.work must fold in gaming's process rules")
	}
}

func TestResolveTerminatesOnCycle(t *testing.T) {
	c := Catalog{
		"a": {ID: "a", Domains: []string{"a.com"}, Composes: []string{"b"}},
		"b": {ID: "b", Domains: []string{"b.com"}, Composes: []string{"a"}},
	}
	got, _ := c.Resolve("a")
	if len(got) != 2 {
		t.Fatalf("want both domains once, got %v", got)
	}
}

func TestResolveStripsAllowlistedEntries(t *testing.T) {
	// A user forks a preset and adds a crisis line to it. Resolve must drop it.
	c := Catalog{"custom": {ID: "custom", Domains: []string{
		"youtube.com", "988lifeline.org", "cdc.gov", "localhost",
	}}}
	got, _ := c.Resolve("custom")
	if len(got) != 1 || got[0] != "youtube.com" {
		t.Fatalf("allowlisted entries survived a list edit: %v", got)
	}
}

func TestNoPresetShipsAnAllowlistedDomain(t *testing.T) {
	c := Presets()
	for _, id := range c.IDs() {
		for _, d := range c[id].Domains {
			if Allowed(d) {
				t.Errorf("%s ships %q, which the allowlist protects — one of the two is wrong", id, d)
			}
		}
	}
}

func TestResolveUnknownIDIsEmptyNotPanic(t *testing.T) {
	d, p := Presets().Resolve("preset.nope")
	if len(d) != 0 || len(p) != 0 {
		t.Fatalf("unknown id should resolve to nothing, got %v %v", d, p)
	}
}
