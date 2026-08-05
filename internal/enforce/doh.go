package enforce

import "strings"

// DNS-over-HTTPS containment at the name layer.
//
// The WFP layer blocks DoH by IP, and that turned out to be much weaker than it
// reads: Firefox's default endpoint is `mozilla.cloudflare-dns.com`, which
// resolves to ordinary Cloudflare CDN addresses, not to 1.1.1.1. Blocking the
// well-known resolver IPs does nothing about it.
//
// Names are the right layer for this. The sink already sees every query, and a
// browser that cannot resolve its DoH endpoint falls back to system DNS — which
// is the sink.

// dohCanary is the mechanism Mozilla built for exactly this situation.
//
// Firefox resolves use-application-dns.net before enabling DoH; an NXDOMAIN is
// the documented signal for "this network manages DNS, do not bypass it", and
// Firefox disables DoH on its own. That is a supported opt-out rather than an
// arms race against endpoint lists, so it is the primary defence and the reason
// this file is small.
const dohCanary = "use-application-dns.net"

// dohEndpointHosts is the belt to the canary's braces: it covers browsers with
// DoH explicitly switched on, which ignore the canary by design.
//
// ponytail: majors only, and it cannot be exhaustive. Someone running a private
// DoH endpoint is threat model row 11 — not defended, and honestly so.
var dohEndpointHosts = []string{
	"cloudflare-dns.com", // covers mozilla./chrome./security./family. subdomains
	"dns.google",
	"dns.quad9.net",
	"doh.opendns.com",
	"dns.nextdns.io",
	"dns.adguard.com",
	"dns.adguard-dns.com",
	"doh.cleanbrowsing.org",
	"doh.dns.sb",
	"dns.alidns.com",
	"doh.pub",
	"dns.controld.com",
	"freedns.controld.com",
}

// blockedForDoH reports whether a name must be refused to keep DNS on the sink.
//
// Only consulted while something is actually enforced. With nothing blocked
// there is no reason to interfere with anyone's resolver choice.
func blockedForDoH(name string) bool {
	n := strings.ToLower(strings.TrimSuffix(name, "."))
	if n == dohCanary || strings.HasSuffix(n, "."+dohCanary) {
		return true
	}
	for _, h := range dohEndpointHosts {
		if n == h || strings.HasSuffix(n, "."+h) {
			return true
		}
	}
	return false
}
