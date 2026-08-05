package enforce

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakePin records whether the resolver was handed back.
type fakePin struct {
	backup    string
	pinned    bool
	unpinned  bool
	unpinErr  error
}

func (f *fakePin) Pin(port int) error {
	f.pinned = true
	return os.WriteFile(f.backup, []byte(`[{"InterfaceIndex":9,"ServerAddresses":["10.0.0.1"]}]`), 0o600)
}

func (f *fakePin) Unpin() error {
	f.unpinned = true
	if f.unpinErr != nil {
		return f.unpinErr
	}
	return os.Remove(f.backup)
}

func (f *fakePin) Pinned() (bool, error) { return f.pinned && !f.unpinned, nil }

// Uninstall must hand the resolver back before anything deletes the record of
// what it used to be. Getting this backwards leaves the machine pointing at a
// 127.0.0.1 resolver that is no longer listening.
func TestClearUnpinsTheResolverAndRestoresHosts(t *testing.T) {
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")
	const userEntry = "127.0.0.1 localhost\n"
	if err := os.WriteFile(hostsPath, []byte(userEntry), 0o644); err != nil {
		t.Fatal(err)
	}

	pin := &fakePin{backup: filepath.Join(dir, "dns-backup.json")}
	sink := NewDNSSink("127.0.0.1:0")
	sink.Pin = pin
	hosts := &Hosts{Path: hostsPath}

	e := New(false, nil, sink, hosts)
	e.Set(Union(cat, []Rule{{"preset.video", Baseline}}))

	if !pin.pinned {
		t.Fatal("setup: resolver was never pinned")
	}
	if _, err := os.Stat(pin.backup); err != nil {
		t.Fatal("setup: no backup written")
	}

	e.Clear()

	if !pin.unpinned {
		t.Fatal("Clear left the resolver pointing at a sink that is going away")
	}
	got, err := os.ReadFile(hostsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "youtube.com") {
		t.Fatalf("hosts block survived uninstall:\n%s", got)
	}
	if !strings.Contains(string(got), "localhost") {
		t.Fatal("Clear ate the user's own hosts entries")
	}
}

// A daemon that never pinned must still be safe to tear down.
func TestClearIsSafeWhenNothingWasApplied(t *testing.T) {
	dir := t.TempDir()
	pin := &fakePin{backup: filepath.Join(dir, "dns-backup.json")}
	sink := NewDNSSink("127.0.0.1:0")
	sink.Pin = pin

	e := New(false, nil, sink, &Hosts{Path: filepath.Join(dir, "hosts")})
	e.Clear() // no Set beforehand

	if !pin.unpinned {
		t.Fatal("teardown must attempt an unpin regardless — a half-finished pin leaves no in-memory trace")
	}
}
