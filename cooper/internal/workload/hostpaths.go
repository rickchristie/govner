package workload

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ValidateAgentStatePath checks a complete state root before profile copying
// or replacement. It applies the same protected path rules as runtime mounts.
func ValidateAgentStatePath(path, home, cooperDir string) error {
	if err := validateHomeBoundary(path, home, "agent state"); err != nil {
		return err
	}
	if err := ValidateHostOwnedPath(path, cooperDir); err != nil {
		return err
	}
	resolved, err := resolveExistingPath(path)
	if err != nil {
		return err
	}
	for _, protected := range protectedStatePaths {
		if pathsOverlap(path, protected) || pathsOverlap(resolved, protected) {
			return fmt.Errorf("agent state %s overlaps protected runtime path %s", path, protected)
		}
	}
	return nil
}

var protectedStatePaths = []string{"/opt/cooper", "/var/lib/cooper", "/go", "/etc", "/usr", "/bin", "/sbin", "/dev", "/proc", "/sys", "/run", "/var/run", "/var/lib/docker", "/var/lib/containerd"}

// ResolvedPath resolves existing parents even when an optional root is absent.
func ResolvedPath(path string) (string, error) { return resolveExistingPath(path) }

// HostAgentStateRoots lists every host-owned built-in agent state root. A
// cleanup check uses all roots, not only the agent selected for one workload.
func HostAgentStateRoots(homeDir string) ([]string, error) {
	launchDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	values := HostPathEnvironment()
	// Cleanup must protect ADC state even after the user leaves ADC mode.
	// Protect the default root as well as an explicit credential parent.
	values["AGY_ADC_AUTH"] = "true"
	roots := []string{filepath.Join(homeDir, ".config", "gcloud")}
	for tool := range agentStatePaths {
		paths, err := ResolveAgentPaths(tool, homeDir, launchDir, values)
		if err != nil {
			return nil, err
		}
		for _, mount := range paths.Mounts {
			roots = append(roots, mount.Source)
		}
	}
	return roots, nil
}

// ValidateAllHostAgentStateRoots rejects a Cooper-owned directory that can
// contain a host agent state root, or that an agent state root can contain.
// It checks direct paths and paths after existing symlinks are resolved.
func ValidateAllHostAgentStateRoots(homeDir, cooperDir string) error {
	roots, err := HostAgentStateRoots(homeDir)
	if err != nil {
		return err
	}
	for _, stateRoot := range roots {
		if err := ValidateHostOwnedPath(stateRoot, cooperDir); err != nil {
			return err
		}
	}
	return nil
}

// ValidateHostOwnedPath rejects overlap with a Cooper-owned deletion root.
func ValidateHostOwnedPath(hostPath, cooperDir string) error {
	if strings.TrimSpace(hostPath) == "" {
		return errors.New("host-owned path is required")
	}
	if strings.TrimSpace(cooperDir) == "" {
		return errors.New("cooper-owned directory is required")
	}
	hostAbs, err := filepath.Abs(hostPath)
	if err != nil {
		return fmt.Errorf("make host-owned path absolute: %w", err)
	}
	cooperAbs, err := filepath.Abs(cooperDir)
	if err != nil {
		return fmt.Errorf("make Cooper-owned directory absolute: %w", err)
	}
	hostAbs = filepath.Clean(hostAbs)
	cooperAbs = filepath.Clean(cooperAbs)
	if pathsOverlap(hostAbs, cooperAbs) {
		return hostStateOverlapError(hostAbs, cooperAbs)
	}

	hostResolved, err := resolveExistingPath(hostAbs)
	if err != nil {
		return fmt.Errorf("resolve host-owned path %s: %w", hostAbs, err)
	}
	cooperResolved, err := resolveExistingPath(cooperAbs)
	if err != nil {
		return fmt.Errorf("resolve Cooper-owned directory %s: %w", cooperAbs, err)
	}
	if pathsOverlap(hostResolved, cooperResolved) {
		return hostStateOverlapError(hostResolved, cooperResolved)
	}
	return nil
}

func hostStateOverlapError(hostPath, cooperDir string) error {
	return fmt.Errorf("host-owned agent state %q overlaps Cooper-owned directory %q; move the Cooper directory or agent state", hostPath, cooperDir)
}

// resolveExistingPath resolves every existing symlink component. It resolves
// the nearest existing parent first when the final path does not yet exist.
func resolveExistingPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := filepath.Clean(abs)
	var missing []string
	for {
		resolved, resolveErr := filepath.EvalSymlinks(current)
		if resolveErr == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
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
