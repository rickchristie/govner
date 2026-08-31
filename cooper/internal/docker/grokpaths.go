package docker

import (
	"os"
	"path/filepath"
)

// GrokHostStateRoot returns the effective host Grok state root. Grok uses a
// nonempty GROK_HOME value as-is and otherwise uses ~/.grok. Cooper resolves a
// relative override before it gives the path to Docker, because Docker bind
// mount sources must not depend on the daemon's working directory.
func GrokHostStateRoot(homeDir string) string {
	override := os.Getenv("GROK_HOME")
	if override == "" {
		return filepath.Join(homeDir, ".grok")
	}
	if filepath.IsAbs(override) {
		return filepath.Clean(override)
	}
	abs, err := filepath.Abs(override)
	if err != nil {
		return filepath.Clean(override)
	}
	return abs
}
