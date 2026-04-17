package barrelenv

import (
	"fmt"
	"os"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
)

// SessionEnvFile describes the host and in-container path for one prepared
// barrel-session env file.
type SessionEnvFile struct {
	HostPath      string
	ContainerPath string
}

// PrepareSessionEnvFile writes a per-session env file into the barrel's
// read-only session directory after tolerant runtime sanitization.
func PrepareSessionEnvFile(cooperDir, containerName, sessionName string, vars []config.BarrelEnvVar) (SessionEnvFile, []string, error) {
	usable, warnings := config.NormalizeBarrelEnvVarsForRuntime(vars)
	if len(usable) == 0 {
		return SessionEnvFile{}, warnings, nil
	}

	data, err := RenderUserEnvFile(usable)
	if err != nil {
		return SessionEnvFile{}, warnings, fmt.Errorf("render user env file: %w", err)
	}

	hostPath, containerPath, err := docker.CreateBarrelSessionFile(
		cooperDir,
		containerName,
		sessionEnvPattern(sessionName),
		data,
		0o600,
	)
	if err != nil {
		return SessionEnvFile{}, warnings, fmt.Errorf("write session env file: %w", err)
	}

	return SessionEnvFile{
		HostPath:      hostPath,
		ContainerPath: containerPath,
	}, warnings, nil
}

// RemoveSessionEnvFile deletes a generated per-session env file.
func RemoveSessionEnvFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove session env file %s: %w", path, err)
	}
	return nil
}

func sessionEnvPattern(sessionName string) string {
	_ = strings.TrimSpace(sessionName)
	return "cooper-cli-env-*.sh"
}
