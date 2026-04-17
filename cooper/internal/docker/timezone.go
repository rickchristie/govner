package docker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var hostLocaltimePath = "/etc/localtime"

const (
	barrelTimezoneFilename      = "cooper-localtime"
	sessionTimezoneFilePrefix   = "cooper-cli-tz-"
	sessionTimezoneFileSuffix   = ".tz"
	barrelTimezoneContainerPath = "/etc/localtime"
)

// SessionTimezoneFile describes one per-session timezone file staged into the
// barrel's read-only session mount so docker exec can point TZ at it.
type SessionTimezoneFile struct {
	HostPath      string
	ContainerPath string
}

// SetHostLocaltimePathForTesting overrides the source localtime file used by
// timezone sync helpers. It returns a restore function for test cleanup.
func SetHostLocaltimePathForTesting(path string) func() {
	previous := hostLocaltimePath
	hostLocaltimePath = path
	return func() {
		hostLocaltimePath = previous
	}
}

// SyncBarrelTimezoneFile copies the host localtime file into the barrel's
// persistent session directory. New barrels bind-mount this file onto
// /etc/localtime so long-lived container processes track the host timezone too.
func SyncBarrelTimezoneFile(cooperDir, containerName string) (string, error) {
	hostPath := filepath.Join(BarrelSessionDir(cooperDir, containerName), barrelTimezoneFilename)
	if err := writeTimezoneFile(hostPath); err != nil {
		return "", err
	}
	return hostPath, nil
}

// PrepareSessionTimezoneFile writes a per-session copy of the host localtime
// file into the barrel's read-only session mount. docker exec can then set
// TZ=:/run/cooper/session/... so reused barrels pick up the current host
// timezone without a container restart.
func PrepareSessionTimezoneFile(cooperDir, containerName, sessionName string) (SessionTimezoneFile, error) {
	data, err := readHostTimezoneData()
	if err != nil {
		return SessionTimezoneFile{}, err
	}
	hostPath, containerPath, err := CreateBarrelSessionFile(
		cooperDir,
		containerName,
		sessionTimezonePattern(sessionName),
		data,
		0o644,
	)
	if err != nil {
		return SessionTimezoneFile{}, err
	}

	return SessionTimezoneFile{
		HostPath:      hostPath,
		ContainerPath: containerPath,
	}, nil
}

// RemoveSessionTimezoneFile deletes a generated per-session timezone file.
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
		return fmt.Errorf("create timezone dir %s: %w", filepath.Dir(hostPath), err)
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

func sessionTimezonePattern(sessionName string) string {
	_ = strings.TrimSpace(sessionName)
	return sessionTimezoneFilePrefix + "*" + sessionTimezoneFileSuffix
}
