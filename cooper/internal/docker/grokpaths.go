package docker

import (
	"github.com/rickchristie/govner/cooper/internal/workload"
)

// GrokHostStateRoot returns the effective host Grok state root. Grok uses a
// nonempty GROK_HOME value as-is and otherwise uses ~/.grok. Cooper resolves a
// relative override before it gives the path to Docker, because Docker bind
// mount sources must not depend on the daemon's working directory.
func GrokHostStateRoot(homeDir string) string {
	return workload.GrokHostStateRoot(homeDir)
}

// ValidateGrokHostStateRoot rejects a Grok state root that overlaps the
// Cooper-owned directory. Cooper recursively resets children of cooperDir and
// can remove cooperDir during cleanup. The check compares direct paths and
// resolved paths, including a symlink in the nearest existing parent of a new
// GROK_HOME path.
func ValidateGrokHostStateRoot(homeDir, cooperDir string) error {
	return workload.ValidateHostOwnedPath(GrokHostStateRoot(homeDir), cooperDir)
}
