package blocklist

import "strings"

// permanent is the non-removable allowlist. Nothing blocks these: not a preset,
// not a schedule, not a session, not a custom list, not "offline mode".
//
// A blocker standing between a person and a crisis line has failed catastrophically
// regardless of uptime. This list is code, not seed data, precisely so a user edit
// cannot reach it.
var permanent = []string{
	// Loopback and local resolution.
	"localhost",
	"local",
	"localdomain",
	"arpa", // in-addr.arpa / ip6.arpa reverse lookups

	// Government and emergency services. Suffix match covers every subdomain.
	"gov",
	"mil",
	"gov.uk",
	"gc.ca",
	"canada.ca",
	"gov.au",
	"govt.nz",
	"europa.eu",

	// Crisis and helpline services.
	"988lifeline.org",
	"suicidepreventionlifeline.org",
	"crisistextline.org",
	"samaritans.org",
	"befrienders.org",
	"iasp.info",
	"talksuicide.ca",
	"kidshelpphone.ca",
	"thetrevorproject.org",
	"findahelpline.com",

	// Health authorities.
	"who.int",
	"nhs.uk",
	"healthlinkbc.ca",

	// OS update and security endpoints. Blocking these turns a focus timer into
	// an unpatched machine.
	"windowsupdate.com",
	"update.microsoft.com",
	"delivery.mp.microsoft.com",
	"ctldl.windowsupdate.com",
	"msftconnecttest.com",
	"msftncsi.com",
	"apple.com",         // softwareupdate lives under this
	"security.ubuntu.com",
	"archive.ubuntu.com",
}

// Allowed reports whether a domain can never be blocked. Matching is on label
// boundaries, so "gov" allows "cdc.gov" but not "notgov.com" or "gov.evil.com".
func Allowed(domain string) bool {
	d := strings.ToLower(strings.Trim(strings.TrimSpace(domain), "."))
	for _, a := range permanent {
		if d == a || strings.HasSuffix(d, "."+a) {
			return true
		}
	}
	return false
}

// Permanent returns a copy of the allowlist, for display and for tests.
func Permanent() []string { return append([]string(nil), permanent...) }
