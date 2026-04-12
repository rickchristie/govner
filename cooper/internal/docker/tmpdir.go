package docker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// BarrelTmpRoot returns the Cooper-managed host directory that backs barrel
// /tmp mounts for the current Cooper installation.
func BarrelTmpRoot(cooperDir string) string {
	return filepath.Join(cooperDir, "tmp")
}

// ResetBarrelTmpRoot removes all persisted barrel /tmp contents and recreates
// the root directory. Cooper calls this at control-plane startup and shutdown
// so each session begins and ends with a pristine temp tree.
func ResetBarrelTmpRoot(cooperDir string) error {
	cooperDir = strings.TrimSpace(cooperDir)
	if cooperDir == "" {
		return nil
	}

	tmpRoot := BarrelTmpRoot(cooperDir)
	if err := removeAllWithPermissionRepair(tmpRoot); err != nil {
		return fmt.Errorf("remove barrel tmp root %s: %w", tmpRoot, err)
	}
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		return fmt.Errorf("create barrel tmp root %s: %w", tmpRoot, err)
	}
	return nil
}

func removeAllWithPermissionRepair(path string) error {
	err := os.RemoveAll(path)
	if err == nil || os.IsNotExist(err) {
		return nil
	}

	if repairErr := repairPathPermissions(path); repairErr != nil {
		return fmt.Errorf("initial remove failed: %w (repair failed: %v)", err, repairErr)
	}

	if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func repairPathPermissions(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("abs path %s: %w", path, err)
	}

	var errs []string
	for _, imageName := range permissionRepairImages() {
		if imageName == "" {
			continue
		}
		exists, err := ImageExists(imageName)
		if err != nil {
			errs = append(errs, fmt.Sprintf("check image %s: %v", imageName, err))
			continue
		}
		if !exists {
			continue
		}

		cmd := exec.Command(
			"docker", "run", "--rm",
			"--user", "root",
			"-v", absPath+":/target",
			"--entrypoint", "sh",
			imageName,
			"-c",
			fmt.Sprintf("chown -R %d:%d /target >/dev/null 2>&1 || true; chmod -R u+rwX /target >/dev/null 2>&1 || true", os.Getuid(), os.Getgid()),
		)
		if out, err := cmd.CombinedOutput(); err == nil {
			return nil
		} else {
			detail := strings.TrimSpace(string(out))
			if detail == "" {
				detail = err.Error()
			}
			errs = append(errs, fmt.Sprintf("repair with %s: %s", imageName, detail))
		}
	}

	if err := exec.Command("chmod", "-R", "u+rwX", absPath).Run(); err == nil {
		return nil
	} else {
		errs = append(errs, fmt.Sprintf("chmod -R u+rwX %s: %v", absPath, err))
	}

	return fmt.Errorf("%s", strings.Join(errs, "; "))
}

func permissionRepairImages() []string {
	images := []string{GetImageBase(), GetImageProxy()}
	return slices.Compact(images)
}
