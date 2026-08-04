package enforce

import "testing"

func TestProcessRulesTerminateOnlyWhatIsBlocked(t *testing.T) {
	eff := Union(cat, []Rule{{"preset.gaming", Session}})

	if !shouldTerminate("steam.exe", eff) {
		t.Fatal("steam.exe is in preset.gaming")
	}
	if !shouldTerminate("STEAM.EXE", eff) {
		t.Fatal("process matching must be case-insensitive")
	}
	if shouldTerminate("code.exe", eff) {
		t.Fatal("terminated something no list names")
	}
	if shouldTerminate("", eff) {
		t.Fatal("empty name must never match")
	}
}

// Non-negotiable: no blocking of system-critical or accessibility software.
func TestProtectedProcessesSurviveEvenWhenListed(t *testing.T) {
	// A hostile or careless custom list naming the shell, a screen reader, AV,
	// and the daemon itself.
	eff := Effective{Processes: map[string]Source{
		"explorer.exe": Session,
		"narrator.exe": Session,
		"nvda.exe":     Baseline,
		"msmpeng.exe":  Baseline,
		"lsass.exe":    Session,
		"mastd.exe":    Session,
	}}
	for name := range eff.Processes {
		if shouldTerminate(name, eff) {
			t.Errorf("%s must never be terminated, even when a list names it", name)
		}
	}
}

func TestNoRulesTerminatesNothing(t *testing.T) {
	if shouldTerminate("steam.exe", Union(cat, nil)) {
		t.Fatal("idle with no process rules must not kill anything")
	}
}
