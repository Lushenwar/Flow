//go:build windows

package main

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Autostart for the WINDOW. The daemon already starts itself — it is a service
// registered Automatic, and that is the part that matters and the part a user
// must not be able to switch off from here.
//
// This is HKCU\...\Run, so it is per-user, visible in Task Manager's Startup
// tab, and removable by anyone without touching Flow at all. That is the right
// shape for a window: a blocker that quietly adds itself to startup and hides
// the switch is behaving like the software this app is trying not to be. It
// ships OFF and the user turns it on.
const (
	runKey       = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "Flow"
	// autostartFlag marks a launch that the Run key started, so the window can
	// come up out of the way instead of over whatever you opened the laptop to do.
	autostartFlag = "-autostart"
)

// AutostartEnabled reports whether the Run key points at this executable.
//
// Compares the path rather than just checking the value exists: a stale entry
// left by an install in a different directory is not this build being enabled,
// and reporting it as "on" would make the checkbox lie.
func (a *App) AutostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()

	got, _, err := k.GetStringValue(runValueName)
	if err != nil {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(got), strings.ToLower(exe))
}

// SetAutostart adds or removes the Run entry.
func (a *App) SetAutostart(on bool) error {
	if !on {
		k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer k.Close()
		// Already absent is success, not an error to surface in a checkbox.
		if err := k.DeleteValue(runValueName); err != nil && !os.IsNotExist(err) {
			if !strings.Contains(err.Error(), "cannot find") {
				return err
			}
		}
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	// Quoted: Program Files has a space in it, and an unquoted path there is the
	// oldest bug in Windows.
	return k.SetStringValue(runValueName, fmt.Sprintf("%q %s", exe, autostartFlag))
}

// launchedByAutostart reports whether the Run key started this process.
func launchedByAutostart() bool {
	for _, arg := range os.Args[1:] {
		if arg == autostartFlag {
			return true
		}
	}
	return false
}
