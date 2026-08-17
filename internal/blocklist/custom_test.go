package blocklist

import (
	"errors"
	"slices"
	"testing"
)

func TestNormalizeAcceptsWhatPeopleActuallyPaste(t *testing.T) {
	// The address bar is where the decision to block something gets made, so
	// what arrives here is a URL far more often than a bare domain.
	cases := map[string]string{
		"reddit.com":                       "reddit.com",
		"  Reddit.COM  ":                   "reddit.com",
		"https://www.reddit.com/r/all":     "reddit.com",
		"http://reddit.com":                "reddit.com",
		"www.youtube.com/watch?v=abc#t=10": "youtube.com",
		"news.ycombinator.com":             "news.ycombinator.com",
		"example.co.uk:8443/path":          "example.co.uk",
		"reddit.com.":                      "reddit.com",
		"https://user:pw@old.reddit.com/x": "old.reddit.com",
	}
	for in, want := range cases {
		got, err := NormalizeDomain(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q normalised to %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeRejectsThingsThatAreNotDomains(t *testing.T) {
	for _, in := range []string{
		"", "   ", "localhost-ish", "no-dot", "http://", "/just/a/path",
		"spaces in.it", "-lead.com", "trail-.com", "a..b.com", "café.com",
	} {
		if got, err := NormalizeDomain(in); !errors.Is(err, ErrNotADomain) {
			t.Errorf("%q returned (%q, %v), want not_a_domain", in, got, err)
		}
	}
}

// A blocker standing between a person and a crisis line has failed
// catastrophically. Resolve strips these anyway, so without an error here the
// entry would sit in the user's list looking enforced while doing nothing.
func TestNormalizeRefusesTheAllowlistOutright(t *testing.T) {
	for _, in := range []string{
		"988lifeline.org", "cdc.gov", "https://www.nhs.uk/", "windowsupdate.com",
		"crisistextline.org", "some.department.gov",
	} {
		if _, err := NormalizeDomain(in); !errors.Is(err, ErrAllowlisted) {
			t.Errorf("%q returned %v, want allowlisted", in, err)
		}
	}
}

// The whole point of stripping www. — one entry has to cover the subdomains, or
// users will add three variants of the same site and still get through on a fourth.
func TestStrippedDomainStillCoversSubdomains(t *testing.T) {
	got, err := NormalizeDomain("https://www.reddit.com/")
	if err != nil {
		t.Fatal(err)
	}
	cat := Catalog{CustomListID: CustomList([]string{got})}
	domains, _ := cat.Resolve(CustomListID)
	if !slices.Contains(domains, "reddit.com") {
		t.Fatalf("custom list resolved to %v", domains)
	}
}

// The custom list goes through the same Resolve as every preset, so the
// allowlist subtraction that protects presets protects it too — belt and braces
// behind NormalizeDomain, for a list that was written before an allowlist entry
// existed.
func TestCustomListCannotSmuggleAnAllowlistedDomain(t *testing.T) {
	cat := Catalog{CustomListID: CustomList([]string{"reddit.com", "988lifeline.org"})}
	domains, _ := cat.Resolve(CustomListID)

	if slices.Contains(domains, "988lifeline.org") {
		t.Fatal("an allowlisted domain reached the effective set through the custom list")
	}
	if !slices.Contains(domains, "reddit.com") {
		t.Fatal("the legitimate entry was dropped too")
	}
}
