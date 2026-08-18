package enforce

import (
	"testing"

	"github.com/Lushenwar/Flow/internal/blocklist"
	"golang.org/x/net/dns/dnsmessage"
)

// Bedtime and Offline claimed "everything except the allowlist" and delivered a
// union of eleven presets. The gap was invisible from inside: the user turns on
// Offline, browses successfully, and concludes the app is broken.
func TestDefaultDenyRefusesWhatItWasNeverTold(t *testing.T) {
	eff := Union(cat, []Rule{{"preset.offline", Session}})
	if !eff.DefaultDeny {
		t.Fatal("preset.offline must invert the blocklist")
	}
	sink := startSink(t, eff)

	// Nothing named by any preset, and refused anyway. That is the whole point.
	for _, name := range []string{"example.com", "some-random-blog.net", "news.bbc.co.uk"} {
		if rc := ask(t, sink, name); rc != dnsmessage.RCodeNameError {
			t.Errorf("%s got %v, want NXDOMAIN under default-deny", name, rc)
		}
	}
}

// The line between a commitment device and a bricked laptop.
func TestDefaultDenyNeverTouchesThePermanentAllowlist(t *testing.T) {
	sink := startSink(t, Union(cat, []Rule{{"preset.offline", Baseline}}))

	for _, name := range []string{
		"cdc.gov", "988lifeline.org", "crisistextline.org", "nhs.uk",
		"windowsupdate.com", "update.microsoft.com", "localhost",
	} {
		if rc := ask(t, sink, name); rc != dnsmessage.RCodeSuccess {
			t.Errorf("%s got %v — a default-deny window must never reach the permanent allowlist", name, rc)
		}
	}
}

// Mandatory equipment: without it, default-deny is a machine with no internet
// and a fifteen-minute wait to fix it.
func TestTheUserAllowlistCarvesHolesInDefaultDeny(t *testing.T) {
	eff := Union(cat, []Rule{{"preset.bedtime", Session}})
	eff.Allow = []string{"work-vpn.example", "github.com"}
	sink := startSink(t, eff)

	for _, name := range []string{"work-vpn.example", "vpn.work-vpn.example", "github.com", "api.github.com"} {
		if rc := ask(t, sink, name); rc != dnsmessage.RCodeSuccess {
			t.Errorf("%s got %v, want the allowlist to reach it", name, rc)
		}
	}
	// And it does not become a blanket hole.
	if rc := ask(t, sink, "notgithub.com"); rc != dnsmessage.RCodeNameError {
		t.Errorf("notgithub.com got %v — the allowlist must respect label boundaries", rc)
	}
}

// An explicitly blocked domain stays blocked even if the user allowlists it,
// because the allowlist is checked before the inversion but the DoH names are
// checked before both — a browser that can resolve its DoH endpoint routes
// around the entire layer.
func TestTheUserAllowlistCannotReopenDoH(t *testing.T) {
	eff := Union(cat, []Rule{{"preset.offline", Session}})
	eff.Allow = []string{"cloudflare-dns.com", "use-application-dns.net"}
	sink := startSink(t, eff)

	for _, name := range []string{"cloudflare-dns.com", "use-application-dns.net"} {
		if rc := ask(t, sink, name); rc != dnsmessage.RCodeNameError {
			t.Errorf("%s got %v — allowlisting a DoH endpoint would make the layer decorative", name, rc)
		}
	}
}

// Without this a default-deny window looks like "nothing enforced" to the
// enforcer, and the sink shuts down mid-window.
func TestDefaultDenyIsNotAnEmptyEffectiveSet(t *testing.T) {
	eff := Effective{
		Lists:       map[string]Source{},
		Domains:     map[string]Source{},
		Processes:   map[string]Source{},
		DefaultDeny: true,
	}
	if eff.Empty() {
		t.Fatal("a default-deny window enforces a great deal while naming few domains")
	}
}

func TestDefaultDenyPropagatesThroughComposition(t *testing.T) {
	// offline composes bedtime, which is where the flag lives.
	if !cat.DefaultDenies("preset.offline") {
		t.Fatal("offline must inherit bedtime's inversion")
	}
	if cat.DefaultDenies("preset.video") {
		t.Fatal("an ordinary preset must not invert anything")
	}
	if cat.DefaultDenies("preset.nope") {
		t.Fatal("an unknown id must not invert anything")
	}
	// A cycle must terminate rather than hang.
	cyclic := blocklist.Catalog{
		"a": {ID: "a", Composes: []string{"b"}},
		"b": {ID: "b", Composes: []string{"a"}},
	}
	if cyclic.DefaultDenies("a") {
		t.Fatal("no default-deny in the cycle")
	}
}
