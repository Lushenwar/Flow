//go:build windows

package enforce

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ResolverPin points every active adapter at the local sink and restores the
// original servers on release.
//
// This is not optional decoration: WFP blocks outbound DNS to remote resolvers,
// so without the pin the machine has no working DNS at all. The two are applied
// together and released together — see DNSSink.Pin.
//
// ponytail: PowerShell's DnsClient cmdlets rather than netsh. netsh's output is
// localised and would need per-language parsing; the cmdlets return structured
// data on every Windows install.
type ResolverPin struct {
	// BackupPath persists the original servers, so a crash between pin and
	// restore does not leave the user with a dead resolver and no record of
	// what it used to be.
	BackupPath string
}

type dnsBackup struct {
	InterfaceIndex int      `json:"InterfaceIndex"`
	Servers        []string `json:"ServerAddresses"`
}

func (r *ResolverPin) Pin(port int) error {
	if port != 53 {
		// Windows offers no way to point the system resolver at a non-standard
		// port. Refuse rather than silently enforce nothing.
		return fmt.Errorf("resolver pin needs the sink on port 53, got %d", port)
	}
	current, err := r.read()
	if err != nil {
		return err
	}
	if _, err := os.Stat(r.BackupPath); os.IsNotExist(err) {
		b, err := json.Marshal(current)
		if err != nil {
			return err
		}
		if err := os.WriteFile(r.BackupPath, b, 0o600); err != nil {
			return err
		}
	}

	for _, iface := range current {
		if len(iface.Servers) == 1 && iface.Servers[0] == "127.0.0.1" {
			continue
		}
		if err := powershell(fmt.Sprintf(
			"Set-DnsClientServerAddress -InterfaceIndex %d -ServerAddresses 127.0.0.1",
			iface.InterfaceIndex)); err != nil {
			return err
		}
	}
	return nil
}

func (r *ResolverPin) Unpin() error {
	b, err := os.ReadFile(r.BackupPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var saved []dnsBackup
	if err := json.Unmarshal(b, &saved); err != nil {
		return err
	}

	// Skip adapters that already hold their saved servers. A pin that failed on
	// the first adapter changed nothing, and reissuing Set on every stop churns
	// the resolver — and fails outright when we are not elevated.
	current := map[int][]string{}
	if got, err := r.read(); err == nil {
		for _, iface := range got {
			current[iface.InterfaceIndex] = iface.Servers
		}
	}

	for _, iface := range saved {
		if same(current[iface.InterfaceIndex], iface.Servers) {
			continue
		}
		var cmd string
		if len(iface.Servers) == 0 {
			// No static servers originally: hand the adapter back to DHCP.
			cmd = fmt.Sprintf("Set-DnsClientServerAddress -InterfaceIndex %d -ResetServerAddresses", iface.InterfaceIndex)
		} else {
			cmd = fmt.Sprintf("Set-DnsClientServerAddress -InterfaceIndex %d -ServerAddresses %s",
				iface.InterfaceIndex, strings.Join(quoteAll(iface.Servers), ","))
		}
		if err := powershell(cmd); err != nil {
			return err
		}
	}
	return os.Remove(r.BackupPath)
}

// Pinned reports whether every active adapter still points at the sink.
func (r *ResolverPin) Pinned() (bool, error) {
	current, err := r.read()
	if err != nil {
		return false, err
	}
	for _, iface := range current {
		if len(iface.Servers) != 1 || iface.Servers[0] != "127.0.0.1" {
			return false, nil
		}
	}
	return true, nil
}

func (r *ResolverPin) read() ([]dnsBackup, error) {
	out, err := powershellOut(
		"Get-DnsClientServerAddress -AddressFamily IPv4 | " +
			"Where-Object { $_.ServerAddresses.Count -gt 0 } | " +
			"Select-Object InterfaceIndex,ServerAddresses | ConvertTo-Json -Compress -Depth 3")
	if err != nil {
		return nil, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	// ConvertTo-Json emits a bare object, not an array, for a single adapter.
	if !strings.HasPrefix(out, "[") {
		out = "[" + out + "]"
	}
	var got []dnsBackup
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		return nil, fmt.Errorf("parse adapter DNS: %w", err)
	}
	return got, nil
}

func same(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func quoteAll(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = `"` + s + `"`
	}
	return out
}

func powershell(cmd string) error {
	_, err := powershellOut(cmd)
	return err
}

func powershellOut(cmd string) (string, error) {
	c := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", cmd)
	out, err := c.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("powershell: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
