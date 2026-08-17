package enforce

import (
	"fmt"
	"log"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Lushenwar/Flow/internal/blocklist"
	"golang.org/x/net/dns/dnsmessage"
)

// DNSSink is a local resolver that answers NXDOMAIN for blocked zones and
// forwards everything else upstream.
//
// This is the layer that handles subdomains: hosts entries are exact strings,
// but a suffix match here covers m.youtube.com, i.ytimg.com and every CDN edge
// without enumerating them.
type DNSSink struct {
	// Addr to listen on. Port 53 in production; tests use :0.
	Addr string
	// Upstream resolvers, tried in order.
	//
	// ponytail: hardcoded public resolvers rather than reading the adapter's
	// original DNS before we repoint it. Swap in the saved adapter config if a
	// captive portal or split-horizon corporate DNS ever needs to work.
	Upstream []string

	// Pin points the system resolver at this sink. It lives here rather than in
	// its own layer on purpose: WFP blocks outbound DNS to remote resolvers, so
	// a machine that is filtered but not pinned has no working DNS at all. Binding
	// them to one layer makes that pairing impossible to get wrong.
	Pin Pinner

	mu      sync.RWMutex
	blocked []string
	conn    *net.UDPConn
	pinned  bool
	wg      sync.WaitGroup
}

// Pinner redirects the OS resolver. Nil means "leave the system alone", which is
// what dev mode and the unit tests use.
type Pinner interface {
	Pin(port int) error
	Unpin() error
	Pinned() (bool, error)
}

// Ready reports whether the sink is listening and the resolver points at it.
// WFP asks before installing its remote-DNS block, so the block never lands
// without somewhere for queries to go.
func (d *DNSSink) Ready() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.conn != nil && (d.Pin == nil || d.pinned)
}

func NewDNSSink(addr string) *DNSSink {
	return &DNSSink{Addr: addr, Upstream: []string{"1.1.1.1:53", "8.8.8.8:53"}}
}

func (d *DNSSink) Name() string { return "dns" }

// Port is the port actually bound, useful when Addr asked for :0.
func (d *DNSSink) Port() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.conn == nil {
		return 0
	}
	return d.conn.LocalAddr().(*net.UDPAddr).Port
}

func (d *DNSSink) Apply(eff Effective) error {
	want := eff.SortedDomains()

	d.mu.Lock()
	d.blocked = want
	running, pinned := d.conn != nil, d.pinned
	d.mu.Unlock()

	// An empty set means stop, which is what Drifted already reports. Returning
	// nil here instead left the two disagreeing forever: Drifted said "running
	// with nothing to block is drift", Apply changed nothing, and reconcile wrote
	// a reconcile_repaired event every 3 seconds claiming a repair that never
	// happened. A bank spend — which empties the union by design — filled the
	// Blocking screen's history with repairs at the one moment nothing was
	// being enforced.
	//
	// Stop() unpins the resolver from its on-disk backup before closing the
	// socket, so this never strands an adapter on a sink that is gone.
	if len(want) == 0 {
		if running {
			return d.Stop()
		}
		return nil
	}
	if !running {
		return d.Start()
	}
	if d.Pin != nil && pinned {
		// Re-pin: someone may have pointed the adapter back at their own resolver.
		return d.Pin.Pin(d.Port())
	}
	return nil
}

func (d *DNSSink) Drifted(eff Effective) (bool, error) {
	d.mu.RLock()
	running, pinned := d.conn != nil, d.pinned
	sameSet := slices.Equal(d.blocked, eff.SortedDomains())
	d.mu.RUnlock()

	if eff.Empty() {
		return running, nil
	}
	if !running || !sameSet {
		return true, nil
	}
	if d.Pin != nil && pinned {
		// Checked outside the lock: this shells out and must not stall queries.
		ok, err := d.Pin.Pinned()
		if err != nil {
			return false, err
		}
		return !ok, nil
	}
	return false, nil
}

func (d *DNSSink) Clear() error { return d.Stop() }

func (d *DNSSink) Start() error {
	addr, err := net.ResolveUDPAddr("udp", d.Addr)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("bind DNS sink on %s: %w", d.Addr, err)
	}
	d.mu.Lock()
	d.conn = conn
	d.mu.Unlock()

	d.wg.Add(1)
	go d.serve(conn)

	if d.Pin != nil {
		if err := d.Pin.Pin(d.Port()); err != nil {
			// Serving on a port nobody queries is useless but harmless; failing
			// loudly here lets health report it and keeps WFP from filtering.
			d.Stop()
			return fmt.Errorf("pin system resolver: %w", err)
		}
		d.mu.Lock()
		d.pinned = true
		d.mu.Unlock()
	}
	return nil
}

func (d *DNSSink) Stop() error {
	d.mu.Lock()
	conn := d.conn
	d.conn, d.pinned = nil, false
	d.mu.Unlock()

	// Unpin unconditionally, not just when we believe we pinned successfully. A
	// pin that failed halfway through the adapter list has already moved some of
	// them, and Unpin is driven by the on-disk backup rather than by what this
	// process remembers. A user left pointing at a dead 127.0.0.1 resolver has no
	// internet at all.
	var unpinErr error
	if d.Pin != nil {
		unpinErr = d.Pin.Unpin()
	}
	if conn == nil {
		return unpinErr
	}
	err := conn.Close()
	d.wg.Wait()
	if unpinErr != nil {
		return unpinErr
	}
	return err
}

func (d *DNSSink) serve(conn *net.UDPConn) {
	defer d.wg.Done()
	buf := make([]byte, 1500) // ponytail: UDP only; a truncated reply makes clients retry over TCP, which WFP blocks to remote resolvers anyway
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			return // closed
		}
		query := make([]byte, n)
		copy(query, buf[:n])
		go d.handle(conn, from, query)
	}
}

func (d *DNSSink) handle(conn *net.UDPConn, from *net.UDPAddr, query []byte) {
	name, ok := questionName(query)
	if ok && d.Blocked(name) {
		if reply, err := nxdomain(query); err == nil {
			conn.WriteToUDP(reply, from)
			return
		}
	}
	if reply, err := d.forward(query); err == nil {
		conn.WriteToUDP(reply, from)
	}
}

// Blocked reports whether a name falls under a blocked domain. Matching is on
// label boundaries, and the permanent allowlist always wins.
func (d *DNSSink) Blocked(name string) bool {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	if blocklist.Allowed(name) {
		return false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Nothing enforced means no reason to interfere with anyone's resolver.
	if len(d.blocked) == 0 {
		return false
	}
	// Refusing the DoH canary and the major endpoints keeps browsers on system
	// DNS, which is this sink. Without it a browser resolves around every rule
	// below and the whole layer is decorative.
	if blockedForDoH(name) {
		return true
	}
	for _, b := range d.blocked {
		if name == b || strings.HasSuffix(name, "."+b) {
			return true
		}
	}
	return false
}

func questionName(query []byte) (string, bool) {
	var p dnsmessage.Parser
	if _, err := p.Start(query); err != nil {
		return "", false
	}
	q, err := p.Question()
	if err != nil {
		return "", false
	}
	return q.Name.String(), true
}

func nxdomain(query []byte) ([]byte, error) {
	var p dnsmessage.Parser
	h, err := p.Start(query)
	if err != nil {
		return nil, err
	}
	q, err := p.Question()
	if err != nil {
		return nil, err
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{
		ID:                 h.ID,
		Response:           true,
		OpCode:             h.OpCode,
		RecursionDesired:   h.RecursionDesired,
		RecursionAvailable: true,
		RCode:              dnsmessage.RCodeNameError,
	})
	if err := b.StartQuestions(); err != nil {
		return nil, err
	}
	if err := b.Question(q); err != nil {
		return nil, err
	}
	return b.Finish()
}

func (d *DNSSink) forward(query []byte) ([]byte, error) {
	var lastErr error
	for _, up := range d.Upstream {
		conn, err := net.DialTimeout("udp", up, 2*time.Second)
		if err != nil {
			lastErr = err
			continue
		}
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		if _, err := conn.Write(query); err != nil {
			conn.Close()
			lastErr = err
			continue
		}
		buf := make([]byte, 1500)
		n, err := conn.Read(buf)
		conn.Close()
		if err != nil {
			lastErr = err
			continue
		}
		return buf[:n], nil
	}
	if lastErr != nil {
		log.Printf("dns forward: %v", lastErr)
	}
	return nil, lastErr
}
