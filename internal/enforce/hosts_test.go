package enforce

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempHosts(t *testing.T, initial string) *Hosts {
	t.Helper()
	p := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(p, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	return &Hosts{Path: p}
}

func read(t *testing.T, h *Hosts) string {
	t.Helper()
	b, err := os.ReadFile(h.Path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const userEntries = "127.0.0.1 localhost\n10.0.0.5 build-server\n"

func TestHostsPreservesUserEntries(t *testing.T) {
	h := tempHosts(t, userEntries)
	eff := Union(cat, []Rule{{"preset.video", Session}})
	if err := h.Apply(eff); err != nil {
		t.Fatal(err)
	}

	got := read(t, h)
	if !strings.Contains(got, "10.0.0.5 build-server") {
		t.Fatal("clobbered the user's own hosts entries")
	}
	if !strings.Contains(got, "127.0.0.1 youtube.com") {
		t.Fatalf("did not block youtube:\n%s", got)
	}
	if !strings.Contains(got, "127.0.0.1 www.youtube.com") {
		t.Fatal("www. variant missing — hosts has no wildcard")
	}
}

func TestHostsApplyIsIdempotent(t *testing.T) {
	h := tempHosts(t, userEntries)
	eff := Union(cat, []Rule{{"preset.video", Session}})
	for i := 0; i < 3; i++ {
		if err := h.Apply(eff); err != nil {
			t.Fatal(err)
		}
	}
	if n := strings.Count(read(t, h), hostsBegin); n != 1 {
		t.Fatalf("reconcile would stack %d managed blocks per pass", n)
	}
}

// The exit criterion: deleting entries by hand self-repairs.
func TestHostsDetectsAndRepairsManualDeletion(t *testing.T) {
	h := tempHosts(t, userEntries)
	eff := Union(cat, []Rule{{"preset.video", Session}})
	if err := h.Apply(eff); err != nil {
		t.Fatal(err)
	}
	if drifted, _ := h.Drifted(eff); drifted {
		t.Fatal("freshly applied state reported as drifted")
	}

	// A user opens hosts in Notepad and deletes the block.
	if err := os.WriteFile(h.Path, []byte(userEntries), 0o644); err != nil {
		t.Fatal(err)
	}
	drifted, err := h.Drifted(eff)
	if err != nil || !drifted {
		t.Fatalf("manual deletion not detected: %v %v", drifted, err)
	}
	if err := h.Apply(eff); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, h), "127.0.0.1 youtube.com") {
		t.Fatal("not repaired")
	}
}

// Editing a single line inside the block is the subtler tamper.
func TestHostsDetectsSingleLineEdit(t *testing.T) {
	h := tempHosts(t, userEntries)
	eff := Union(cat, []Rule{{"preset.video", Session}})
	h.Apply(eff)

	edited := strings.Replace(read(t, h), "127.0.0.1 youtube.com\n", "", 1)
	os.WriteFile(h.Path, []byte(edited), 0o644)

	if drifted, _ := h.Drifted(eff); !drifted {
		t.Fatal("one deleted line inside the block is still drift")
	}
}

func TestHostsClearLeavesNoResidue(t *testing.T) {
	h := tempHosts(t, userEntries)
	h.Apply(Union(cat, []Rule{{"preset.video", Session}}))
	if err := h.Clear(); err != nil {
		t.Fatal(err)
	}

	got := read(t, h)
	if strings.Contains(got, "FLOW") || strings.Contains(got, "youtube") {
		t.Fatalf("uninstall left residue:\n%s", got)
	}
	if !strings.Contains(got, "10.0.0.5 build-server") {
		t.Fatalf("clear ate the user's entries:\n%s", got)
	}
}

// The temp file lives in %SystemRoot%\System32\drivers\etc. Leaving one there is
// residue in a directory nobody expects us to touch.
func TestHostsLeavesNoTempFileBehind(t *testing.T) {
	h := tempHosts(t, userEntries)
	eff := Union(cat, []Rule{{"preset.video", Session}})

	for i := 0; i < 3; i++ {
		if err := h.Apply(eff); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(h.Path + ".flow.tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file left in the hosts directory")
	}
}

// A write path that gives up on the first refusal leaves the layer inert. The
// real hosts file intermittently denies the replace, so falling through to an
// in-place write matters more than atomicity here.
func TestHostsWriteFallsBackWhenReplaceIsRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	if err := os.WriteFile(path, []byte(userEntries), 0o644); err != nil {
		t.Fatal(err)
	}

	// A directory where the temp file wants to go makes the rename fail without
	// making the target unwritable — the same shape as the observed denial.
	if err := os.Mkdir(path+".flow.tmp", 0o755); err != nil {
		t.Skipf("cannot stage the failure on this platform: %v", err)
	}
	defer os.RemoveAll(path + ".flow.tmp")

	if err := writeHosts(path, []byte("written anyway\n")); err != nil {
		t.Fatalf("gave up instead of falling back: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "written anyway\n" {
		t.Fatalf("fallback did not write: %q", got)
	}
}

func TestHostsHandlesMissingFile(t *testing.T) {
	h := &Hosts{Path: filepath.Join(t.TempDir(), "nope", "..", "hosts")}
	eff := Union(cat, []Rule{{"preset.video", Session}})
	if drifted, err := h.Drifted(eff); err != nil || !drifted {
		t.Fatalf("absent hosts file with rules pending is drift: %v %v", drifted, err)
	}
	if err := h.Apply(eff); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(t, h), "youtube.com") {
		t.Fatal("did not create the file")
	}
}
