package docker

import (
	"fmt"
	"os"
	"path/filepath"
)

// CreateBarrelSessionFile writes data into the host-controlled per-barrel
// session directory using CreateTemp so filenames are unpredictable and file
// creation is symlink-safe. The returned container path points at the matching
// file inside the read-only session bind mount.
func CreateBarrelSessionFile(cooperDir, containerName, pattern string, data []byte, perm os.FileMode) (hostPath, containerPath string, err error) {
	dir := BarrelSessionDir(cooperDir, containerName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("create barrel session dir %s: %w", dir, err)
	}

	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", "", fmt.Errorf("create barrel session file in %s: %w", dir, err)
	}
	hostPath = file.Name()
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close barrel session file %s: %w", hostPath, closeErr)
		}
	}()

	if _, err := file.Write(data); err != nil {
		return "", "", fmt.Errorf("write barrel session file %s: %w", hostPath, err)
	}
	if err := file.Chmod(perm); err != nil {
		return "", "", fmt.Errorf("chmod barrel session file %s: %w", hostPath, err)
	}

	return hostPath, filepath.Join(BarrelSessionContainerDir, filepath.Base(hostPath)), nil
}
