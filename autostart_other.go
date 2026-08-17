//go:build !windows

package main

import "errors"

// ponytail: autostart is a Run key, which is a Windows idea. macOS wants a
// LaunchAgent plist and Linux a .desktop file in ~/.config/autostart; both land
// with their enforcers rather than ahead of them.
//
// The frontend hides the control when AutostartEnabled is unavailable, so these
// only have to be honest, not useful.

func (a *App) AutostartEnabled() bool { return false }

func (a *App) SetAutostart(on bool) error {
	return errors.New("autostart is Windows-only for now")
}

func launchedByAutostart() bool { return false }
