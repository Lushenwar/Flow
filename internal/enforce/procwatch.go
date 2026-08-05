package enforce

import "strings"

// protected can never be terminated, whatever a list says. Screen readers, input
// devices, AV, Windows Update and the OS shell are all off limits — a focus timer
// that closes Narrator has locked someone out of their own computer.
var protected = map[string]bool{
	// OS shell and core.
	"explorer.exe": true, "csrss.exe": true, "winlogon.exe": true, "wininit.exe": true,
	"services.exe": true, "lsass.exe": true, "smss.exe": true, "svchost.exe": true,
	"dwm.exe": true, "fontdrvhost.exe": true, "sihost.exe": true, "ctfmon.exe": true,
	"logonui.exe": true, "userinit.exe": true, "taskhostw.exe": true, "runtimebroker.exe": true,
	// Accessibility.
	"narrator.exe": true, "magnify.exe": true, "osk.exe": true, "displayswitch.exe": true,
	"nvda.exe": true, "jfw.exe": true, "zt.exe": true, "dragonbar.exe": true,
	// Security and update.
	"msmpeng.exe": true, "mssense.exe": true, "securityhealthservice.exe": true,
	"securityhealthsystray.exe": true, "wuauclt.exe": true, "usoclient.exe": true,
	"trustedinstaller.exe": true, "tiworker.exe": true,
	// Ourselves. Killing the daemon from its own process rules would be a
	// creative way to build an off switch.
	"flowd.exe": true, "flowctl.exe": true,
}

// shouldTerminate decides whether a running process matches a process rule.
// Pure, so the decision is testable without spawning anything.
func shouldTerminate(exeName string, eff Effective) bool {
	name := strings.ToLower(strings.TrimSpace(exeName))
	if name == "" || protected[name] {
		return false
	}
	_, blocked := eff.Processes[name]
	return blocked
}
