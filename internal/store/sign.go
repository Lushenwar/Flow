package store

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"os"
	"strconv"
)

// ErrTampered means a row's HMAC did not match its contents. Tamper-EVIDENT:
// the key is on the same disk, so this catches casual sqlite3 edits and corruption,
// not a determined attacker with an afternoon.
var ErrTampered = errors.New("row signature does not match contents")

// sep is a byte that cannot appear in the JSON/text fields being signed, so
// sign("a","bc") and sign("ab","c") produce different digests.
const sep = 0x1f

func sign(key []byte, parts ...string) []byte {
	m := hmac.New(sha256.New, key)
	for _, p := range parts {
		m.Write([]byte(p))
		m.Write([]byte{sep})
	}
	return m.Sum(nil)
}

func verify(key []byte, sig []byte, parts ...string) bool {
	return hmac.Equal(sig, sign(key, parts...))
}

func itoa(i int64) string { return strconv.FormatInt(i, 10) }

// loadOrCreateKey returns the 32-byte HMAC key, generating and wrapping one on first run.
func loadOrCreateKey(path string) ([]byte, error) {
	wrapped, err := os.ReadFile(path)
	if err == nil {
		return unwrapKey(wrapped)
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	wrapped, err = wrapKey(key)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, wrapped, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}
