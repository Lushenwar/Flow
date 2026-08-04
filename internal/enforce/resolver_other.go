//go:build !windows

package enforce

import "errors"

// ponytail: resolv.conf / systemd-resolved and scutil land with their enforcers.
type ResolverPin struct{ BackupPath string }

func (r *ResolverPin) Pin(port int) error   { return errors.New("resolver pin is Windows-only for now") }
func (r *ResolverPin) Unpin() error         { return nil }
func (r *ResolverPin) Pinned() (bool, error) { return false, nil }
