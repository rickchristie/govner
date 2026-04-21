package docker

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	shellSessionFilePrefix = "cooper-cli-shell-"
	shellSessionFileSuffix = ".session"
)

// ShellSessionMarker tracks one active interactive cooper cli shell attached to
// a barrel. The marker lives in the host-controlled per-barrel session
// directory so cooper up can count live shells without execing into containers.
type ShellSessionMarker struct {
	HostPath string
}

// CreateShellSessionMarker writes a marker file for one active interactive
// cooper cli shell. The file contains the host PID of the cooper cli process so
// stale markers can be ignored if that process has already exited.
func CreateShellSessionMarker(cooperDir, containerName, sessionName string) (ShellSessionMarker, error) {
	data := []byte(strconv.Itoa(os.Getpid()))
	hostPath, _, err := CreateBarrelSessionFile(
		cooperDir,
		containerName,
		shellSessionPattern(sessionName),
		data,
		0o600,
	)
	if err != nil {
		return ShellSessionMarker{}, fmt.Errorf("create shell session marker: %w", err)
	}
	return ShellSessionMarker{HostPath: hostPath}, nil
}

// RemoveShellSessionMarker deletes a previously-created interactive shell
// marker.
func RemoveShellSessionMarker(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove shell session marker %s: %w", path, err)
	}
	return nil
}

// CountActiveShellSessions returns the number of live interactive cooper cli
// shells currently attached to the given barrel. Stale markers are ignored and
// removed opportunistically.
func CountActiveShellSessions(cooperDir, containerName string) (int, error) {
	dir := BarrelSessionDir(cooperDir, containerName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read shell session dir %s: %w", dir, err)
	}

	count := 0
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, shellSessionFilePrefix) || !strings.HasSuffix(name, shellSessionFileSuffix) {
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

func shellSessionPattern(sessionName string) string {
	_ = strings.TrimSpace(sessionName)
	return shellSessionFilePrefix + "*" + shellSessionFileSuffix
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
	if err == nil {
		return true, nil
	}
	if err == syscall.EPERM {
		return true, nil
	}
	if err == syscall.ESRCH {
		return false, nil
	}
	return false, fmt.Errorf("check shell session pid %d for %s: %w", pid, path, err)
}
