package enforce

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeLayer records what it was told and can be made to drift or fail.
type fakeLayer struct {
	mu       sync.Mutex
	name     string
	applied  int
	cleared  int
	drift    bool
	applyErr error
	driftErr error
	last     Effective
}

func (f *fakeLayer) Name() string { return f.name }

func (f *fakeLayer) Apply(eff Effective) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.applyErr != nil {
		return f.applyErr
	}
	f.applied++
	f.last = eff
	f.drift = false
	return nil
}

func (f *fakeLayer) Drifted(eff Effective) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.drift, f.driftErr
}

func (f *fakeLayer) Clear() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleared++
	return nil
}

func (f *fakeLayer) set(fn func(*fakeLayer)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

type eventLog struct {
	mu   sync.Mutex
	kind []string
}

func (e *eventLog) add(kind, data string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.kind = append(e.kind, kind)
}

func (e *eventLog) joined() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return strings.Join(e.kind, ",")
}

func TestSetAppliesToEveryLayer(t *testing.T) {
	a, b := &fakeLayer{name: "a"}, &fakeLayer{name: "b"}
	e := New(false, nil, a, b)
	e.Set(Union(cat, []Rule{{"preset.video", Session}}))

	for _, l := range []*fakeLayer{a, b} {
		if l.applied != 1 {
			t.Fatalf("%s applied %d times", l.name, l.applied)
		}
		if _, ok := l.last.Domains["youtube.com"]; !ok {
			t.Fatalf("%s got the wrong rule set", l.name)
		}
	}
}

// The exit criterion: deleted rules self-repair, and every repair is an event.
func TestReconcileRepairsDriftAndLogsIt(t *testing.T) {
	l := &fakeLayer{name: "hosts"}
	log := &eventLog{}
	e := New(false, log.add, l)
	e.Set(Union(cat, []Rule{{"preset.video", Session}}))

	if n := e.Reconcile(); n != 0 {
		t.Fatalf("no drift should mean no repairs, got %d", n)
	}
	l.set(func(f *fakeLayer) { f.drift = true })

	if n := e.Reconcile(); n != 1 {
		t.Fatalf("drift not repaired, got %d repairs", n)
	}
	if !strings.Contains(log.joined(), "reconcile_repaired") {
		t.Fatalf("repair not logged: %s", log.joined())
	}
	if l.applied != 2 {
		t.Fatalf("repair should re-apply, applied %d times", l.applied)
	}
}

// Partial enforcement beats none: one broken layer must not stop the others.
func TestOneFailingLayerDoesNotStopTheRest(t *testing.T) {
	bad := &fakeLayer{name: "wfp", applyErr: errors.New("needs elevation")}
	good := &fakeLayer{name: "hosts"}
	e := New(false, nil, bad, good)
	e.Set(Union(cat, []Rule{{"preset.video", Session}}))

	if good.applied != 1 {
		t.Fatal("a failure in an earlier layer stopped a later one")
	}
	st := e.Status()
	if !strings.Contains(st["wfp"], "needs elevation") {
		t.Fatalf("health must surface the failure, got %q", st["wfp"])
	}
	if st["hosts"] != "active" {
		t.Fatalf("working layer reported as %q", st["hosts"])
	}
}

// PowerShell and netsh failures arrive as twenty lines of banner. Health renders
// this in a one-line status row.
func TestStatusErrorsAreCollapsedToOneLine(t *testing.T) {
	l := &fakeLayer{name: "dns", applyErr: errors.New("pin failed\nAt line:1 char:1\n+ Set-DnsClientServerAddress ...")}
	e := New(false, nil, l)
	e.Set(Union(cat, []Rule{{"preset.video", Session}}))

	got := e.Status()["dns"]
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("status is multi-line: %q", got)
	}
	if !strings.Contains(got, "pin failed") {
		t.Fatalf("collapsed away the useful part: %q", got)
	}
}

func TestDriftErrorIsReportedNotSwallowed(t *testing.T) {
	l := &fakeLayer{name: "dns", driftErr: errors.New("socket gone")}
	e := New(false, nil, l)
	e.Set(Union(cat, []Rule{{"preset.video", Session}}))
	e.Reconcile()

	if !strings.Contains(e.Status()["dns"], "socket gone") {
		t.Fatalf("drift error hidden, status %q", e.Status()["dns"])
	}
}

func TestDevModeAppliesNothing(t *testing.T) {
	l := &fakeLayer{name: "wfp", drift: true}
	e := New(true, nil, l)
	e.Set(Union(cat, []Rule{{"preset.video", Session}}))
	e.Reconcile()

	if l.applied != 0 {
		t.Fatal("dev mode must log, not apply — the first person this traps is the author")
	}
	if e.Status()["wfp"] != "dry-run" {
		t.Fatalf("status %q", e.Status()["wfp"])
	}
}

func TestClearTearsDownEveryLayer(t *testing.T) {
	a, b := &fakeLayer{name: "a"}, &fakeLayer{name: "b"}
	e := New(false, nil, a, b)
	e.Set(Union(cat, []Rule{{"preset.video", Session}}))
	e.Clear()

	if a.cleared != 1 || b.cleared != 1 {
		t.Fatalf("uninstall left a layer installed: %d %d", a.cleared, b.cleared)
	}
}

func TestLastReconcileAdvances(t *testing.T) {
	e := New(false, nil, &fakeLayer{name: "a"})
	if !e.LastReconcile().IsZero() {
		t.Fatal("nothing has run yet")
	}
	e.Reconcile()
	if e.LastReconcile().IsZero() {
		t.Fatal("health would report 'never' forever")
	}
}
