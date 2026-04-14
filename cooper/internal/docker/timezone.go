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
// barrel's host-backed /tmp mount so docker exec can point TZ at it.
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
// persistent tmp directory. New barrels bind-mount this file onto /etc/localtime
// so long-lived container processes track the host timezone too.
func SyncBarrelTimezoneFile(cooperDir, containerName string) (string, error) {
	hostPath := filepath.Join(BarrelTmpDir(cooperDir, containerName), barrelTimezoneFilename)
	if err := writeTimezoneFile(hostPath); err != nil {
		return "", err
	}
	return hostPath, nil
}

// PrepareSessionTimezoneFile writes a per-session copy of the host localtime
// file into the barrel's mounted /tmp directory. docker exec can then set
// TZ=:/tmp/... so reused barrels pick up the current host timezone without a
// container restart.
func PrepareSessionTimezoneFile(cooperDir, containerName, sessionName string) (SessionTimezoneFile, error) {
	trimmed := strings.TrimSpace(sessionName)
	if trimmed == "" {
		return SessionTimezoneFile{}, fmt.Errorf("session name is required")
	}

	filename := sessionTimezoneFilePrefix + trimmed + sessionTimezoneFileSuffix
	hostPath := filepath.Join(BarrelTmpDir(cooperDir, containerName), filename)
	if err := writeTimezoneFile(hostPath); err != nil {
		return SessionTimezoneFile{}, err
	}

	return SessionTimezoneFile{
		HostPath:      hostPath,
		ContainerPath: "/tmp/" + filename,
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
	data, err := os.ReadFile(hostLocaltimePath)
	if err != nil {
		return fmt.Errorf("read host localtime %s: %w", hostLocaltimePath, err)
	}
	if err := os.MkdirAll(filepath.Dir(hostPath), 0o755); err != nil {
		return fmt.Errorf("create timezone dir %s: %w", filepath.Dir(hostPath), err)
	}
	if err := os.WriteFile(hostPath, data, 0o644); err != nil {
		return fmt.Errorf("write timezone file %s: %w", hostPath, err)
	}
	return nil
}
