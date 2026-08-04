//go:build !windows

package enforce

// ponytail: pf/nftables enforcers are stubs. hosts and the sink are portable, so
// off Windows the stack is those two and nothing else.
func Layers(dataDir string, dev bool) []Layer {
	return []Layer{NewDNSSink("127.0.0.1:53"), NewHosts()}
}
