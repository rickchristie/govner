package docker

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

// ValidateGrokHostStateRoot rejects a Grok state root that overlaps the
// Cooper-owned directory. Cooper recursively resets children of cooperDir and
// can remove cooperDir during cleanup. The check compares direct paths and
// resolved paths, including a symlink in the nearest existing parent of a new
// GROK_HOME path.
func ValidateGrokHostStateRoot(homeDir, cooperDir string) error {
	stateRoot, err := filepath.Abs(GrokHostStateRoot(homeDir))
	if err != nil {
		return fmt.Errorf("make Grok state root absolute: %w", err)
	}
	ownedRoot, err := filepath.Abs(cooperDir)
	if err != nil {
		return fmt.Errorf("make Cooper-owned directory absolute: %w", err)
	}
	stateRoot = filepath.Clean(stateRoot)
	ownedRoot = filepath.Clean(ownedRoot)
	if hostPathsOverlap(stateRoot, ownedRoot) {
		return grokStateOverlapError(stateRoot, ownedRoot)
	}

	resolvedStateRoot, err := resolveHostPath(stateRoot)
	if err != nil {
		return fmt.Errorf("resolve Grok state root: %w", err)
	}
	resolvedOwnedRoot, err := resolveHostPath(ownedRoot)
	if err != nil {
		return fmt.Errorf("resolve Cooper-owned directory: %w", err)
	}
	if hostPathsOverlap(resolvedStateRoot, resolvedOwnedRoot) {
		return grokStateOverlapError(resolvedStateRoot, resolvedOwnedRoot)
	}
	return nil
}

func grokStateOverlapError(stateRoot, ownedRoot string) error {
	return fmt.Errorf("GROK_HOME %q overlaps Cooper-owned directory %q; move Grok state outside the Cooper directory", stateRoot, ownedRoot)
}

// resolveHostPath returns an absolute path with every existing symlink
// resolved. EvalSymlinks needs the full path to exist, so resolve the nearest
// existing parent first and then restore the missing child names.
func resolveHostPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	current := filepath.Clean(abs)
	var missing []string
	for {
		resolved, resolveErr := filepath.EvalSymlinks(current)
		if resolveErr == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(resolveErr, fs.ErrNotExist) {
			return "", resolveErr
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", resolveErr
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func hostPathsOverlap(left, right string) bool {
	return hostPathContains(left, right) || hostPathContains(right, left)
}

func hostPathContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
