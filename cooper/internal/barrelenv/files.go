package barrelenv

import (
	"fmt"
	"os"
	"path/filepath"
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

// PrepareSessionEnvFile writes a per-session env file into the barrel's mounted
// tmp directory after tolerant runtime sanitization.
func PrepareSessionEnvFile(cooperDir, containerName, sessionName string, vars []config.BarrelEnvVar) (SessionEnvFile, []string, error) {
	usable, warnings := config.NormalizeBarrelEnvVarsForRuntime(vars)
	if len(usable) == 0 {
		return SessionEnvFile{}, warnings, nil
	}

	tmpDir := docker.BarrelTmpDir(cooperDir, containerName)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return SessionEnvFile{}, warnings, fmt.Errorf("create barrel tmp dir %s: %w", tmpDir, err)
	}

	data, err := RenderUserEnvFile(usable)
	if err != nil {
		return SessionEnvFile{}, warnings, fmt.Errorf("render user env file: %w", err)
	}

	filename := "cooper-cli-env-" + sessionName + ".sh"
	hostPath := filepath.Join(tmpDir, filename)
	if err := os.WriteFile(hostPath, data, 0o600); err != nil {
		return SessionEnvFile{}, warnings, fmt.Errorf("write session env file %s: %w", hostPath, err)
	}

	return SessionEnvFile{
		HostPath:      hostPath,
		ContainerPath: "/tmp/" + filename,
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
