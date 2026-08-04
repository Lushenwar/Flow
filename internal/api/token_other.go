//go:build !windows

package api

// ponytail: 0600 from WriteFile is the whole ACL story off Windows.
func restrictToken(path string) error { return nil }
