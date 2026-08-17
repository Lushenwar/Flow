package blocklist

import (
	"errors"
	"strings"
)

// CustomListID is the one user-owned list. It behaves like any other list from
// the union's point of view — which is the point: it is enforced by the same
// layers, attributed the same way, and turned off through the same 15-minute
// delay as a preset. Nothing about it is a side door.
const CustomListID = "custom.blocked"

// MaxCustomDomains caps the list. Every entry becomes three lines in the hosts
// file and a linear scan in the DNS sink's matcher, so an unbounded list is a
// slow resolver rather than a stronger blocker.
const MaxCustomDomains = 500

var (
	ErrNotADomain    = errors.New("not_a_domain")
	ErrAllowlisted   = errors.New("allowlisted")
	ErrTooManyCustom = errors.New("too_many_domains")
)

// NormalizeDomain turns whatever the user pasted into a domain the enforcer can
// actually use, or explains why it cannot.
//
// People paste whole URLs, because that is what is in the address bar when they
// decide to block something. Everything after the host is discarded: HTTPS means
// the network layer sees `reddit.com` and never `/r/all`, so accepting a path
// here and silently blocking the whole domain would be the worst outcome — the
// user believes they scoped it, and the enforcement does not match the belief.
// The caller is expected to show what this returned.
func NormalizeDomain(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	// Credentials, then path, query, fragment.
	if i := strings.Index(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, ":"); i >= 0 {
		s = s[:i] // port
	}
	s = strings.Trim(s, ".")

	// A leading www. is noise: matching is by label-boundary suffix, so
	// `youtube.com` already covers `www.youtube.com` and every other subdomain.
	s = strings.TrimPrefix(s, "www.")

	if !validDomain(s) {
		return "", ErrNotADomain
	}
	// The permanent allowlist is not editable by any list, and that has to be
	// enforced where the user finds out — Resolve strips these anyway, so without
	// this check the entry would sit in the list looking blocked and do nothing.
	if Allowed(s) {
		return "", ErrAllowlisted
	}
	return s, nil
}

func validDomain(s string) bool {
	if len(s) == 0 || len(s) > 253 || !strings.Contains(s, ".") {
		return false
	}
	for _, label := range strings.Split(s, ".") {
		if len(label) == 0 || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			// ASCII only: an IDN has to arrive already punycoded. Accepting raw
			// unicode would let two different strings render identically, which
			// is a blocklist you cannot audit by reading it.
			if !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') && c != '-' {
				return false
			}
		}
	}
	return true
}

// CustomList builds the list value the catalog exposes for user domains.
func CustomList(domains []string) List {
	return List{
		ID:      CustomListID,
		Name:    "Custom sites",
		Domains: append([]string(nil), domains...),
	}
}
