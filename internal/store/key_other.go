//go:build !windows

package store

// ponytail: no DPAPI off Windows. The key file's 0600 mode is the only protection
// until the macOS/Linux enforcers land, and those bring Keychain / kernel keyring with them.

func wrapKey(plain []byte) ([]byte, error)     { return plain, nil }
func unwrapKey(wrapped []byte) ([]byte, error) { return wrapped, nil }
