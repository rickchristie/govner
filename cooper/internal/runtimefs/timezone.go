package runtimefs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	sessionTimezonePrefix = "cooper-session-tz-"
	sessionTimezoneSuffix = ".tz"
)

var hostLocaltimePath = "/etc/localtime"

// SessionTimezoneFile identifies one host file and its read-only workload
// path.
type SessionTimezoneFile struct {
	HostPath      string
	ContainerPath string
}

// SetHostLocaltimePathForTesting changes the timezone source for one test.
// The returned function restores the previous source.
func SetHostLocaltimePathForTesting(path string) func() {
	previous := hostLocaltimePath
	hostLocaltimePath = path
	return func() { hostLocaltimePath = previous }
}

// SyncTimezoneFile copies the host timezone into one workload session tree.
// A copied file avoids Docker following the host /etc/localtime symlink and
// hiding its zoneinfo target in the container.
func SyncTimezoneFile(cooperDir, runtimeID string) (string, error) {
	hostPath := filepath.Join(SessionDir(cooperDir, runtimeID), TimezoneFilename)
	if err := writeTimezoneFile(hostPath); err != nil {
		return "", err
	}
	return hostPath, nil
}

// PrepareSessionTimezoneFile creates a fresh per-shell timezone snapshot.
func PrepareSessionTimezoneFile(cooperDir, runtimeID string) (SessionTimezoneFile, error) {
	data, err := readHostTimezoneData()
	if err != nil {
		return SessionTimezoneFile{}, err
	}
	hostPath, containerPath, err := CreateSessionFile(
		cooperDir,
		runtimeID,
		sessionTimezonePrefix+"*"+sessionTimezoneSuffix,
		data,
		0o644,
	)
	if err != nil {
		return SessionTimezoneFile{}, err
	}
	return SessionTimezoneFile{HostPath: hostPath, ContainerPath: containerPath}, nil
}

// RemoveSessionTimezoneFile deletes a generated timezone snapshot.
func RemoveSessionTimezoneFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove session timezone file %s: %w", path, err)
	}
	return nil
}

func writeTimezoneFile(hostPath string) error {
	data, err := readHostTimezoneData()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(hostPath), 0o755); err != nil {
		return fmt.Errorf("create timezone directory %s: %w", filepath.Dir(hostPath), err)
	}
	if err := os.WriteFile(hostPath, data, 0o644); err != nil {
		return fmt.Errorf("write timezone file %s: %w", hostPath, err)
	}
	return nil
}

func readHostTimezoneData() ([]byte, error) {
	data, err := os.ReadFile(hostLocaltimePath)
	if err != nil {
		return nil, fmt.Errorf("read host localtime %s: %w", hostLocaltimePath, err)
	}
	return data, nil
}
