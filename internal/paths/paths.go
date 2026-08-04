// Package paths resolves every on-disk location the daemon owns.
package paths

import (
	"os"
	"path/filepath"
)

// AppName is the codename decision CLAUDE.md required before Phase 0 ships.
// It is baked into the service name, the install path, and the binary names.
const AppName = "Mast"

// Dir is where all persistent state lives: %ProgramData%\Mast on Windows.
// MAST_DATA_DIR overrides it, which is how tests get an isolated store.
func Dir() string {
	if d := os.Getenv("MAST_DATA_DIR"); d != "" {
		return d
	}
	base := os.Getenv("ProgramData")
	if base == "" {
		// ponytail: dev fallback only; real macOS/Linux paths land with their enforcers.
		base = os.TempDir()
	}
	return filepath.Join(base, AppName)
}

func DB() string    { return filepath.Join(Dir(), "state.db") }
func Key() string   { return filepath.Join(Dir(), "key.bin") }
func Token() string { return filepath.Join(Dir(), "token") }
func Port() string  { return filepath.Join(Dir(), "port") }

// EnsureDir creates the data directory if it is missing.
func EnsureDir() error { return os.MkdirAll(Dir(), 0o755) }
