//go:build windows

package enforce

import "path/filepath"

// Layers builds the full stack in application order. Ordering matters: the sink
// comes up and pins the resolver before WFP filters remote DNS.
func Layers(dataDir string, dev bool) []Layer {
	sink := NewDNSSink("127.0.0.1:53")
	if !dev {
		sink.Pin = &ResolverPin{BackupPath: filepath.Join(dataDir, "dns-backup.json")}
	}
	return []Layer{
		sink,
		NewWFP(sink.Ready),
		NewHosts(),
		NewProcWatch(),
	}
}
