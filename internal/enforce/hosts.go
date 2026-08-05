package enforce

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Hosts is the secondary layer. It is best-effort by design: DNS-over-HTTPS, raw
// IPs and VPNs all route around it, and it cannot express a wildcard. It exists
// because it costs nothing and catches the plainest case.
//
// No LockFileEx theatre — a write lock stops a text editor and nothing else.
// Write, then let reconcile repair.
type Hosts struct{ Path string }

const (
	hostsBegin = "# BEGIN MAST — managed block, edits are reverted automatically"
	hostsEnd   = "# END MAST"
)

func NewHosts() *Hosts { return &Hosts{Path: defaultHostsPath()} }

func defaultHostsPath() string {
	if runtime.GOOS != "windows" {
		return "/etc/hosts"
	}
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32", "drivers", "etc", "hosts")
}

func (h *Hosts) Name() string { return "hosts" }

// block renders the managed section for eff.
func (h *Hosts) block(eff Effective) string {
	if len(eff.Domains) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(hostsBegin + "\n")
	for _, d := range eff.SortedDomains() {
		// ponytail: hosts has no wildcard, so www. is hardcoded rather than
		// enumerated. Subdomain coverage is the DNS sink's job.
		b.WriteString("127.0.0.1 " + d + "\n")
		b.WriteString("127.0.0.1 www." + d + "\n")
		b.WriteString("::1 " + d + "\n")
	}
	b.WriteString(hostsEnd + "\n")
	return b.String()
}

func (h *Hosts) Apply(eff Effective) error {
	cur, err := os.ReadFile(h.Path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		cur = nil
	}
	next := replaceBlock(string(cur), h.block(eff))
	if next == string(cur) {
		return nil
	}
	return writeHosts(h.Path, []byte(next))
}

func (h *Hosts) Drifted(eff Effective) (bool, error) {
	cur, err := os.ReadFile(h.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return h.block(eff) != "", nil
		}
		return false, err
	}
	return extractBlock(string(cur)) != h.block(eff), nil
}

func (h *Hosts) Clear() error { return h.Apply(Effective{}) }

// extractBlock returns the managed section including its markers, or "".
func extractBlock(s string) string {
	i := strings.Index(s, hostsBegin)
	if i < 0 {
		return ""
	}
	j := strings.Index(s[i:], hostsEnd)
	if j < 0 {
		return ""
	}
	end := i + j + len(hostsEnd)
	if end < len(s) && s[end] == '\n' {
		end++
	}
	return s[i:end]
}

// replaceBlock swaps the managed section for want, leaving the user's own entries
// untouched. An empty want removes the section entirely.
func replaceBlock(cur, want string) string {
	old := extractBlock(cur)
	if old != "" {
		cur = strings.Replace(cur, old, "", 1)
	}
	cur = strings.TrimRight(cur, "\n")
	if want == "" {
		if cur == "" {
			return ""
		}
		return cur + "\n"
	}
	if cur == "" {
		return want
	}
	return cur + "\n" + want
}

// writeHosts replaces the file, retrying the rename before falling back to an
// in-place write.
//
// Observed on the first elevated run: the very first apply failed with
// "rename hosts.mast.tmp hosts: Access is denied", and the identical write
// succeeded on the next reconcile tick 3 seconds later. Windows guards the hosts
// file, and the denial is intermittent rather than absolute. Retrying inline
// makes the first apply land instead of leaving a hole until reconcile notices.
func writeHosts(path string, data []byte) error {
	replaceErr := replaceViaTemp(path, data)
	if replaceErr == nil {
		return nil
	}

	// Replacing the file was refused — at the temp write or at the rename, both
	// of which Windows guards. Try writing through the file instead. That opens a
	// torn-write window on a crash, which the 3s reconcile closes; a hosts layer
	// that never writes is a layer that does not exist.
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("replace failed (%v) and in-place write failed: %w", replaceErr, err)
	}
	return nil
}

func replaceViaTemp(path string, data []byte) error {
	tmp := path + ".mast.tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	var err error
	for i := 0; i < 3; i++ {
		if err = os.Rename(tmp, path); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Never leave a stray hosts.mast.tmp in %SystemRoot%\System32\drivers\etc.
	os.Remove(tmp)
	return err
}
