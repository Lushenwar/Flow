//go:build windows

package api

import "os/exec"

// restrictToken drops inherited ACEs and grants read to Administrators and the
// interactive user, full control to SYSTEM. Well-known SIDs, not names, so this
// works on a localised Windows.
//
// ponytail: icacls shell-out instead of hand-rolled SDDL through advapi32.
// Swap it for windows.SetNamedSecurityInfo if the exec ever shows up in a profile.
func restrictToken(path string) error {
	cmd := exec.Command("icacls", path,
		"/inheritance:r",
		"/grant:r", "*S-1-5-18:(F)",      // SYSTEM
		"/grant:r", "*S-1-5-32-544:(R)",  // Administrators
		"/grant:r", "*S-1-5-4:(R)",       // INTERACTIVE
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return &aclError{path: path, out: string(out), err: err}
	}
	return nil
}

type aclError struct {
	path string
	out  string
	err  error
}

func (e *aclError) Error() string { return "icacls " + e.path + ": " + e.err.Error() + ": " + e.out }
func (e *aclError) Unwrap() error { return e.err }
