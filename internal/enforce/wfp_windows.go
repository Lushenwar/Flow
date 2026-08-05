//go:build windows

package enforce

import (
	"fmt"
	"net/netip"
	"os"

	"github.com/tailscale/wf"
	"golang.org/x/sys/windows"
)

// WFP is the primary network layer: user-mode Windows Filtering Platform via
// fwpuclnt.dll, no kernel driver.
//
// Its job is DNS containment, not per-domain IP blocking. Resolving a blocked
// domain to IPs and filtering those is a trap — CDN addresses are shared, so
// blocking one takes out half the web, and they rotate faster than a 3s reconcile.
// Instead: every DNS query is forced through the local sink, and the public
// DoH/DoT escape routes fail closed.
//
// Applying a filter also triggers ALE reauthorization, which terminates
// already-open connections. That is why committing to a session kills a
// mid-stream YouTube tab instead of letting it play out.
type WFP struct {
	// SinkReady must report true before the remote-DNS block is installed.
	// Filtering remote resolvers while nothing is listening locally would leave
	// the machine with no DNS at all — that is a broken computer, not a blocker.
	SinkReady func() bool

	sess     *wf.Session
	provider wf.ProviderID
	sublayer wf.SublayerID
	rules    []wf.RuleID
}

func NewWFP(sinkReady func() bool) *WFP { return &WFP{SinkReady: sinkReady} }

func (w *WFP) sinkReady() bool { return w.SinkReady != nil && w.SinkReady() }

func (w *WFP) Name() string { return "wfp" }

// dohEndpoints are the public resolvers that would otherwise tunnel DNS over 443
// and route around the sink entirely. Not exhaustive and cannot be — this closes
// the defaults shipped by Firefox and Chrome, which is threat model row 9's
// "partly", stated honestly.
var dohEndpoints = []netip.Addr{
	netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("1.0.0.1"), // Cloudflare
	netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("8.8.4.4"), // Google
	netip.MustParseAddr("9.9.9.9"), netip.MustParseAddr("149.112.112.112"), // Quad9
	netip.MustParseAddr("208.67.222.222"), netip.MustParseAddr("208.67.220.220"), // OpenDNS
	netip.MustParseAddr("94.140.14.14"), netip.MustParseAddr("94.140.15.15"), // AdGuard
	netip.MustParseAddr("45.90.28.0"), netip.MustParseAddr("45.90.30.0"), // NextDNS
}

var (
	providerGUID = wf.ProviderID{Data1: 0x6d2a1f19, Data2: 0x9c3e, Data3: 0x4d55, Data4: [8]byte{0xb1, 0x0e, 0x4a, 0x77, 0x2c, 0x51, 0x00, 0x01}}
	sublayerGUID = wf.SublayerID{Data1: 0x6d2a1f19, Data2: 0x9c3e, Data3: 0x4d55, Data4: [8]byte{0xb1, 0x0e, 0x4a, 0x77, 0x2c, 0x51, 0x00, 0x02}}
)

func (w *WFP) Apply(eff Effective) error {
	if eff.Empty() {
		return w.Clear()
	}
	if !w.sinkReady() {
		return fmt.Errorf("DNS sink not ready; refusing to filter remote resolvers")
	}
	if w.sess != nil {
		return nil // filters are static while anything is blocked
	}
	return w.install()
}

func (w *WFP) install() error {
	// Dynamic: filters die with the process, so a crashed daemon cannot leave the
	// machine permanently firewalled. The service restart re-applies them.
	sess, err := wf.New(&wf.Options{
		Name:        "Flow",
		Description: "Flow DNS containment filters",
		Dynamic:     true,
	})
	if err != nil {
		return fmt.Errorf("open WFP session (needs elevation): %w", err)
	}
	w.sess = sess

	if err := sess.AddProvider(&wf.Provider{
		ID:   providerGUID,
		Name: "Flow",
		Data: []byte("flow"),
	}); err != nil {
		w.closeSession()
		return fmt.Errorf("add provider: %w", err)
	}
	w.provider = providerGUID

	if err := sess.AddSublayer(&wf.Sublayer{
		ID:       sublayerGUID,
		Name:     "Flow",
		Provider: providerGUID,
		Weight:   0xffff,
	}); err != nil {
		w.closeSession()
		return fmt.Errorf("add sublayer: %w", err)
	}
	w.sublayer = sublayerGUID

	if err := w.addRules(); err != nil {
		w.closeSession()
		return err
	}
	return nil
}

func (w *WFP) addRules() error {
	// flowd itself must reach upstream resolvers, or the sink has nothing to forward to.
	self, err := os.Executable()
	if err != nil {
		return err
	}
	appID, err := wf.AppID(self)
	if err != nil {
		return fmt.Errorf("app id for %s: %w", self, err)
	}

	v4, v6 := wf.LayerALEAuthConnectV4, wf.LayerALEAuthConnectV6
	n := 0
	add := func(name string, layer wf.LayerID, weight uint64, action wf.Action, conds ...*wf.Match) error {
		n++
		id := ruleGUID(uint16(n))
		if err := w.sess.AddRule(&wf.Rule{
			ID:         id,
			Name:       name,
			Layer:      layer,
			Sublayer:   w.sublayer,
			Provider:   w.provider,
			Weight:     weight,
			Conditions: conds,
			Action:     action,
		}); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		w.rules = append(w.rules, id)
		return nil
	}

	// Weight order within the sublayer: permits outrank blocks.
	for _, layer := range []wf.LayerID{v4, v6} {
		if err := add("Flow permit self", layer, 30, wf.ActionPermit,
			&wf.Match{Field: wf.FieldALEAppID, Op: wf.MatchTypeEqual, Value: appID},
		); err != nil {
			return err
		}
	}
	// Loopback: the sink lives here.
	if err := add("Flow permit loopback DNS", v4, 25, wf.ActionPermit,
		&wf.Match{Field: wf.FieldIPRemoteAddress, Op: wf.MatchTypeEqual, Value: netip.MustParsePrefix("127.0.0.0/8")},
	); err != nil {
		return err
	}
	if err := add("Flow permit loopback DNS v6", v6, 25, wf.ActionPermit,
		&wf.Match{Field: wf.FieldIPRemoteAddress, Op: wf.MatchTypeEqual, Value: netip.MustParsePrefix("::1/128")},
	); err != nil {
		return err
	}

	// Everything else talking to a remote resolver is blocked, which forces the
	// system through the sink whatever the adapter is configured with.
	for _, layer := range []wf.LayerID{v4, v6} {
		for _, proto := range []wf.IPProto{wf.IPProtoUDP, wf.IPProtoTCP} {
			if err := add("Flow block remote DNS", layer, 10, wf.ActionBlock,
				&wf.Match{Field: wf.FieldIPProtocol, Op: wf.MatchTypeEqual, Value: proto},
				&wf.Match{Field: wf.FieldIPRemotePort, Op: wf.MatchTypeEqual, Value: uint16(53)},
			); err != nil {
				return err
			}
		}
		// DNS-over-TLS.
		if err := add("Flow block DoT", layer, 10, wf.ActionBlock,
			&wf.Match{Field: wf.FieldIPProtocol, Op: wf.MatchTypeEqual, Value: wf.IPProtoTCP},
			&wf.Match{Field: wf.FieldIPRemotePort, Op: wf.MatchTypeEqual, Value: uint16(853)},
		); err != nil {
			return err
		}
	}

	// DNS-over-HTTPS, by endpoint. Port 443 in general obviously stays open.
	for _, ip := range dohEndpoints {
		if err := add("Flow block DoH "+ip.String(), v4, 10, wf.ActionBlock,
			&wf.Match{Field: wf.FieldIPProtocol, Op: wf.MatchTypeEqual, Value: wf.IPProtoTCP},
			&wf.Match{Field: wf.FieldIPRemotePort, Op: wf.MatchTypeEqual, Value: uint16(443)},
			&wf.Match{Field: wf.FieldIPRemoteAddress, Op: wf.MatchTypeEqual, Value: ip},
		); err != nil {
			return err
		}
	}
	return nil
}

// Drifted reports whether our filters are still installed. A dynamic session's
// rules cannot be removed by netsh, but the engine can be reset.
func (w *WFP) Drifted(eff Effective) (bool, error) {
	if eff.Empty() {
		return w.sess != nil, nil
	}
	if w.sess == nil {
		// Only drift if we could actually install; otherwise reconcile would
		// retry-and-fail every 3 seconds and fill the event log with noise.
		return w.sinkReady(), nil
	}
	rules, err := w.sess.Rules()
	if err != nil {
		return true, err
	}
	have := make(map[wf.RuleID]bool, len(rules))
	for _, r := range rules {
		have[r.ID] = true
	}
	for _, id := range w.rules {
		if !have[id] {
			return true, nil
		}
	}
	return false, nil
}

func (w *WFP) Clear() error {
	if w.sess == nil {
		return nil
	}
	w.closeSession()
	return nil
}

func (w *WFP) closeSession() {
	if w.sess != nil {
		w.sess.Close()
		w.sess = nil
	}
	w.rules = nil
}

// ruleGUID derives a stable per-rule GUID from the provider GUID and an index,
// so Drifted can look rules up by id across reconcile ticks.
func ruleGUID(n uint16) wf.RuleID {
	g := windows.GUID(providerGUID)
	g.Data4[6] = byte(n >> 8)
	g.Data4[7] = byte(n)
	return wf.RuleID(g)
}
