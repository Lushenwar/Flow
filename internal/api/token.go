package api

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
)

// LoadOrCreateToken returns the bearer token, generating one on first run.
//
// The token being readable by the interactive user is deliberate and safe: no verb
// in this API ends a locked session or instantly disables a baseline rule. Authority
// lives in the state machine, not the transport.
func LoadOrCreateToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		if tok := strings.TrimSpace(string(b)); tok != "" {
			return tok, nil
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(raw)
	if err := os.WriteFile(path, []byte(tok), 0o600); err != nil {
		return "", err
	}
	return tok, restrictToken(path)
}
