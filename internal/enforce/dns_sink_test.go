package enforce

import (
	"fmt"
	"net"
	"testing"
	"time"

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
