package enforce

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Layer is one enforcement mechanism. Layers are independent: a failure in one
// does not stop the others, because partial enforcement beats none.
type Layer interface {
	Name() string
	// Apply makes the OS match eff. Must be idempotent — reconcile calls it repeatedly.
	Apply(eff Effective) error
	// Drifted reports whether actual OS state has diverged from eff. This is the
	// actual anti-tamper mechanism; file locking was never going to be.
	Drifted(eff Effective) (bool, error)
	// Clear removes everything this layer installed. Uninstall depends on it.
	Clear() error
}

// Enforcer applies the union of every rule set across every layer.
type Enforcer struct {
	mu     sync.Mutex
	layers []Layer
	dev    bool
	want   Effective
	status map[string]string
	last   time.Time
	events func(kind, data string)
}

// New builds an enforcer. In dev mode every action is logged instead of applied —
// the first person this app traps will be the person writing it.
func New(dev bool, events func(kind, data string), layers ...Layer) *Enforcer {
	e := &Enforcer{
		layers: layers,
		dev:    dev,
		want:   Union(nil, nil),
		status: map[string]string{},
		events: events,
	}
	for _, l := range layers {
		e.status[l.Name()] = "idle"
	}
	return e
}

func (e *Enforcer) log(kind, data string) {
	if e.events != nil {
		e.events(kind, data)
	}
}

// Set changes the intended rule set and applies it immediately. Blocks apply at
// commit, not at grace-end.
func (e *Enforcer) Set(eff Effective) {
	e.mu.Lock()
	e.want = eff
	e.mu.Unlock()
	e.apply("set")
}

func (e *Enforcer) apply(reason string) {
	e.mu.Lock()
	want, dev, layers := e.want, e.dev, e.layers
	e.mu.Unlock()

	for _, l := range layers {
		if dev {
			log.Printf("[dev] %s: would apply %d domains, %d processes (%s)",
				l.Name(), len(want.Domains), len(want.Processes), reason)
			e.setStatus(l.Name(), "dry-run")
			continue
		}
		if err := l.Apply(want); err != nil {
			e.setStatus(l.Name(), "error: "+err.Error())
			log.Printf("%s apply: %v", l.Name(), err)
			continue
		}
		e.setStatus(l.Name(), "active")
	}
	e.mu.Lock()
	e.last = time.Now()
	e.mu.Unlock()
}

// Reconcile diffs intended against actual state and repairs any drift, returning
// the number of layers repaired. Every repair is an event.
func (e *Enforcer) Reconcile() int {
	e.mu.Lock()
	want, dev, layers := e.want, e.dev, e.layers
	e.mu.Unlock()

	e.mu.Lock()
	e.last = time.Now()
	e.mu.Unlock()

	if dev {
		return 0
	}

	repaired := 0
	for _, l := range layers {
		drifted, err := l.Drifted(want)
		if err != nil {
			e.setStatus(l.Name(), "error: "+err.Error())
			continue
		}
		if !drifted {
			continue
		}
		if err := l.Apply(want); err != nil {
			e.setStatus(l.Name(), "error: "+err.Error())
			e.log("reconcile_failed", fmt.Sprintf(`{"layer":%q,"error":%q}`, l.Name(), err.Error()))
			continue
		}
		repaired++
		e.setStatus(l.Name(), "active")
		e.log("reconcile_repaired", fmt.Sprintf(`{"layer":%q}`, l.Name()))
		log.Printf("%s: drift repaired", l.Name())
	}
	return repaired
}

// Run reconciles on a ticker until ctx is cancelled.
func (e *Enforcer) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.Reconcile()
		}
	}
}

// Clear tears down every layer. Used by uninstall and by service shutdown.
func (e *Enforcer) Clear() {
	for _, l := range e.layers {
		if err := l.Clear(); err != nil {
			log.Printf("%s clear: %v", l.Name(), err)
		}
		e.setStatus(l.Name(), "cleared")
	}
}

func (e *Enforcer) setStatus(name, s string) {
	e.mu.Lock()
	e.status[name] = firstLine(s)
	e.mu.Unlock()
}

// firstLine keeps health readable. Shelling out to PowerShell or netsh produces
// twenty lines of banner per failure, and the UI renders this in a status row.
func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i]) + " …"
	}
	if len(s) > 200 {
		s = s[:200] + " …"
	}
	return s
}

// Status is what /api/health reports, per layer.
func (e *Enforcer) Status() map[string]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[string]string, len(e.status))
	for k, v := range e.status {
		out[k] = v
	}
	return out
}

// LastReconcile is the timestamp of the most recent pass, zero if never.
func (e *Enforcer) LastReconcile() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.last
}
