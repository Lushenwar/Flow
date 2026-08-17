package enforce

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/Lushenwar/Flow/internal/blocklist"
	"golang.org/x/net/dns/dnsmessage"
)

// fakeUpstream answers everything with a fixed A record, so a forwarded query is
// distinguishable from a sunk one.
func fakeUpstream(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			var p dnsmessage.Parser
			h, err := p.Start(buf[:n])
			if err != nil {
				continue
			}
			q, err := p.Question()
			if err != nil {
				continue
			}
			b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: h.ID, Response: true, RCode: dnsmessage.RCodeSuccess})
			b.StartQuestions()
			b.Question(q)
			b.StartAnswers()
			b.AResource(
				dnsmessage.ResourceHeader{Name: q.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60},
				dnsmessage.AResource{A: [4]byte{203, 0, 113, 1}},
			)
			out, err := b.Finish()
			if err == nil {
				conn.WriteToUDP(out, from)
			}
		}
	}()
	return conn.LocalAddr().String()
}

func startSink(t *testing.T, eff Effective) *DNSSink {
	t.Helper()
	d := NewDNSSink("127.0.0.1:0")
	d.Upstream = []string{fakeUpstream(t)}
	if err := d.Apply(eff); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Stop() })
	return d
}

// ask sends a real query and returns the response code.
func ask(t *testing.T, sink *DNSSink, name string) dnsmessage.RCode {
	t.Helper()
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: 0x1234, RecursionDesired: true})
	b.StartQuestions()
	if err := b.Question(dnsmessage.Question{
		Name:  dnsmessage.MustNewName(name + "."),
		Type:  dnsmessage.TypeA,
		Class: dnsmessage.ClassINET,
	}); err != nil {
		t.Fatal(err)
	}
	query, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}

	conn, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", sink.Port()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(query); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("no reply for %s: %v", name, err)
	}
	var p dnsmessage.Parser
	h, err := p.Start(buf[:n])
	if err != nil {
		t.Fatal(err)
	}
	if h.ID != 0x1234 {
		t.Fatalf("reply id %x does not match the query", h.ID)
	}
	return h.RCode
}

func TestSinkNXDOMAINsBlockedNamesAndSubdomains(t *testing.T) {
	sink := startSink(t, Union(cat, []Rule{{"preset.video", Session}}))

	for _, name := range []string{"youtube.com", "www.youtube.com", "m.youtube.com", "rr1---sn-x.googlevideo.com"} {
		if rc := ask(t, sink, name); rc != dnsmessage.RCodeNameError {
			t.Errorf("%s got %v, want NXDOMAIN", name, rc)
		}
	}
}

func TestSinkForwardsEverythingElse(t *testing.T) {
	sink := startSink(t, Union(cat, []Rule{{"preset.video", Session}}))

	for _, name := range []string{"go.dev", "wikipedia.org"} {
		if rc := ask(t, sink, name); rc != dnsmessage.RCodeSuccess {
			t.Errorf("%s got %v, want a forwarded success", name, rc)
		}
	}
}

func TestSinkNeverBlocksTheAllowlist(t *testing.T) {
	// Every preset active at once.
	var rules []Rule
	for _, id := range cat.IDs() {
		rules = append(rules, Rule{id, Baseline})
	}
	sink := startSink(t, Union(cat, rules))

	for _, name := range []string{"cdc.gov", "988lifeline.org", "windowsupdate.com", "nhs.uk"} {
		if rc := ask(t, sink, name); rc != dnsmessage.RCodeSuccess {
			t.Errorf("%s got %v — the allowlist must be unreachable by any list", name, rc)
		}
	}
}

// Label-boundary matching, the same trap as the allowlist.
func TestSinkDoesNotBlockBySubstring(t *testing.T) {
	sink := startSink(t, Union(cat, []Rule{{"preset.video", Session}}))

	if rc := ask(t, sink, "notyoutube.com"); rc != dnsmessage.RCodeSuccess {
		t.Errorf("notyoutube.com got %v — suffix match must respect label boundaries", rc)
	}
	if rc := ask(t, sink, "youtube.com.evil.net"); rc != dnsmessage.RCodeSuccess {
		t.Errorf("youtube.com.evil.net got %v — it is not under youtube.com", rc)
	}
}

// The whole chain for a user-added site: typed into the box, through the
// catalog as an ordinary list, out of the sink as NXDOMAIN — subdomains
// included, which is why the leading www. is stripped on the way in.
func TestSinkBlocksUserAddedDomains(t *testing.T) {
	domain, err := blocklist.NormalizeDomain("https://www.example-forum.com/r/hot")
	if err != nil {
		t.Fatal(err)
	}
	withCustom := blocklist.Catalog{blocklist.CustomListID: blocklist.CustomList([]string{domain})}
	sink := startSink(t, Union(withCustom, []Rule{{blocklist.CustomListID, Baseline}}))

	for _, name := range []string{"example-forum.com", "www.example-forum.com", "old.example-forum.com"} {
		if rc := ask(t, sink, name); rc != dnsmessage.RCodeNameError {
			t.Errorf("%s got %v, want NXDOMAIN", name, rc)
		}
	}
	// A custom list is still a list: it cannot reach past a label boundary.
	if rc := ask(t, sink, "notexample-forum.com"); rc != dnsmessage.RCodeSuccess {
		t.Errorf("notexample-forum.com got %v — suffix match must respect labels", rc)
	}
}

func TestSinkDriftOnRuleSetChange(t *testing.T) {
	video := Union(cat, []Rule{{"preset.video", Session}})
	sink := startSink(t, video)

	if drifted, _ := sink.Drifted(video); drifted {
		t.Fatal("freshly applied set reported as drifted")
	}
	more := Union(cat, []Rule{{"preset.video", Session}, {"preset.doomscroll", Session}})
	if drifted, _ := sink.Drifted(more); !drifted {
		t.Fatal("a changed rule set is drift")
	}
	if err := sink.Apply(more); err != nil {
		t.Fatal(err)
	}
	if rc := ask(t, sink, "reddit.com"); rc != dnsmessage.RCodeNameError {
		t.Fatalf("newly added rule not live: %v", rc)
	}
}

// Apply and Drifted have to agree about the empty set, or reconcile repairs
// forever. Drifted said "running with nothing blocked is drift"; Apply returned
// without stopping; every 3s tick wrote a reconcile_repaired event for a repair
// that never happened. A bank spend empties the union by design, so this fired
// during the one window where nothing is supposed to be enforced.
func TestEmptyRuleSetStopsTheSinkInsteadOfRepairingForever(t *testing.T) {
	sink := startSink(t, Union(cat, []Rule{{"preset.video", Session}}))
	port := sink.Port()

	empty := Union(cat, nil)
	if drifted, _ := sink.Drifted(empty); !drifted {
		t.Fatal("a running sink with nothing to block is drift")
	}
	if err := sink.Apply(empty); err != nil {
		t.Fatal(err)
	}

	// The repair has to actually settle it. This is the whole bug.
	if drifted, _ := sink.Drifted(empty); drifted {
		t.Fatal("still drifted after Apply — reconcile would log a repair every 3s, forever")
	}
	if sink.Port() != 0 {
		t.Fatal("sink still bound")
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		t.Fatalf("port still held: %v", err)
	}
	conn.Close()

	// And it must come back when something is blocked again.
	if err := sink.Apply(Union(cat, []Rule{{"preset.video", Session}})); err != nil {
		t.Fatal(err)
	}
	if rc := ask(t, sink, "youtube.com"); rc != dnsmessage.RCodeNameError {
		t.Fatalf("sink did not restart: %v", rc)
	}
}

// Applying an empty set to a sink that was never started must not error.
func TestEmptyRuleSetOnAStoppedSinkIsANoOp(t *testing.T) {
	d := NewDNSSink("127.0.0.1:0")
	if err := d.Apply(Union(cat, nil)); err != nil {
		t.Fatalf("nothing to stop should not be an error: %v", err)
	}
	if drifted, _ := d.Drifted(Union(cat, nil)); drifted {
		t.Fatal("a stopped sink with nothing to block is not drift")
	}
}

func TestSinkStopReleasesThePort(t *testing.T) {
	sink := startSink(t, Union(cat, []Rule{{"preset.video", Session}}))
	port := sink.Port()
	if err := sink.Stop(); err != nil {
		t.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		t.Fatalf("port still held after Stop: %v", err)
	}
	conn.Close()
}
