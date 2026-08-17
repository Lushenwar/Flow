package enforce

import (
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

// Mozilla's documented opt-out. NXDOMAIN here makes Firefox disable DoH by
// itself, which beats chasing endpoint IPs forever.
func TestFirefoxCanaryIsRefusedWhileEnforcing(t *testing.T) {
	sink := startSink(t, Union(cat, []Rule{{"preset.video", Session}}))

	if rc := ask(t, sink, "use-application-dns.net"); rc != dnsmessage.RCodeNameError {
		t.Fatalf("canary got %v, want NXDOMAIN — Firefox would keep DoH on", rc)
	}
}

// The gap that prompted this: Firefox's endpoint is a CDN address, so the WFP
// IP blocklist never touched it.
func TestDoHEndpointsAreRefusedByName(t *testing.T) {
	sink := startSink(t, Union(cat, []Rule{{"preset.video", Session}}))

	for _, name := range []string{
		"mozilla.cloudflare-dns.com",
		"chrome.cloudflare-dns.com",
		"cloudflare-dns.com",
		"dns.google",
		"dns.quad9.net",
		"dns.nextdns.io",
	} {
		if rc := ask(t, sink, name); rc != dnsmessage.RCodeNameError {
			t.Errorf("%s got %v, want NXDOMAIN", name, rc)
		}
	}
}

// With nothing blocked there is no reason to interfere with a resolver choice.
//
// Asserted on the method rather than over UDP because an idle sink is not
// listening at all — Apply does not bind when the rule set is empty, so the
// property already holds by construction at the process level.
func TestDoHIsUntouchedWhenNothingIsEnforced(t *testing.T) {
	idle := NewDNSSink("127.0.0.1:0")

	for _, name := range []string{"use-application-dns.net", "dns.google", "youtube.com"} {
		if idle.Blocked(name) {
			t.Errorf("%s blocked while nothing is enforced", name)
		}
	}
}

func TestDoHMatchingRespectsLabelBoundaries(t *testing.T) {
	// The same trap as everywhere else: a substring must not be enough.
	if blockedForDoH("notdns.google") {
		t.Error("notdns.google is not dns.google")
	}
	if blockedForDoH("dns.google.evil.net") {
		t.Error("dns.google.evil.net is not under dns.google")
	}
	if !blockedForDoH("mozilla.cloudflare-dns.com") {
		t.Error("subdomains of a listed endpoint must match")
	}
	if !blockedForDoH("USE-APPLICATION-DNS.NET.") {
		t.Error("matching must be case- and trailing-dot-insensitive")
	}
}

// A crisis line served behind a listed CDN must still resolve.
func TestAllowlistStillBeatsTheDoHList(t *testing.T) {
	sink := startSink(t, Union(cat, []Rule{{"preset.video", Session}}))

	for _, name := range []string{"988lifeline.org", "cdc.gov"} {
		if rc := ask(t, sink, name); rc != dnsmessage.RCodeSuccess {
			t.Errorf("%s got %v — the permanent allowlist outranks everything", name, rc)
		}
	}
}
