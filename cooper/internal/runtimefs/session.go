package runtimefs

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	shellMarkerPrefix = "cooper-session-shell-"
	shellMarkerSuffix = ".session"
)

// CreateSessionFile writes an unpredictable file into the host-controlled
// session directory. The container sees the same file through a read-only
// mount, so it cannot replace or pre-create the host file.
func CreateSessionFile(cooperDir, runtimeID, pattern string, data []byte, mode os.FileMode) (hostPath, containerPath string, err error) {
	dir := SessionDir(cooperDir, runtimeID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("create runtime session directory %s: %w", dir, err)
	}

	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", "", fmt.Errorf("create runtime session file in %s: %w", dir, err)
	}
	hostPath = file.Name()
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close runtime session file %s: %w", hostPath, closeErr)
		}
	}()

	if _, err := file.Write(data); err != nil {
		return "", "", fmt.Errorf("write runtime session file %s: %w", hostPath, err)
	}
	if err := file.Chmod(mode); err != nil {
		return "", "", fmt.Errorf("set runtime session file mode %s: %w", hostPath, err)
	}

	return hostPath, filepath.Join(SessionContainerDir, filepath.Base(hostPath)), nil
}

// ShellMarker tracks one live interactive Cooper process. It stores the host
// PID, so stale markers do not inflate the TUI shell count.
type ShellMarker struct {
	HostPath string
}

// CreateShellMarker writes one marker for an interactive CLI or VM session.
func CreateShellMarker(cooperDir, runtimeID string) (ShellMarker, error) {
	hostPath, _, err := CreateSessionFile(
		cooperDir,
		runtimeID,
		shellMarkerPrefix+"*"+shellMarkerSuffix,
		[]byte(strconv.Itoa(os.Getpid())),
		0o600,
	)
	if err != nil {
		return ShellMarker{}, fmt.Errorf("create shell session marker: %w", err)
	}
	return ShellMarker{HostPath: hostPath}, nil
}

// RemoveShellMarker deletes a marker that Cooper created.
func RemoveShellMarker(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove shell session marker %s: %w", path, err)
	}
	return nil
}

// CountActiveShells returns the live interactive shell count for one runtime.
// It removes stale markers after their host process exits.
func CountActiveShells(cooperDir, runtimeID string) (int, error) {
	dir := SessionDir(cooperDir, runtimeID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read shell session directory %s: %w", dir, err)
	}

	count := 0
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, shellMarkerPrefix) || !strings.HasSuffix(name, shellMarkerSuffix) {
			continue
		}
		hostPath := filepath.Join(dir, name)
		alive, err := markerPIDAlive(hostPath)
		if err != nil {
			return 0, err
		}
		if alive {
			count++
			continue
		}
		_ = os.Remove(hostPath)
	}
	return count, nil
}

func markerPIDAlive(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read shell session marker %s: %w", path, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false, nil
	}
	err = syscall.Kill(pid, 0)
	switch err {
	case nil, syscall.EPERM:
		return true, nil
	case syscall.ESRCH:
		return false, nil
	default:
		return false, fmt.Errorf("check shell session PID %d for %s: %w", pid, path, err)
	}
}
