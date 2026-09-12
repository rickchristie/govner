package docker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/runtimefs"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

// ResetRuntimeTempRoot removes all workload /tmp contents and recreates the
// root. Cooper calls it at control-plane startup and shutdown.
func ResetRuntimeTempRoot(cooperDir string) error {
	cooperDir = strings.TrimSpace(cooperDir)
	if cooperDir == "" {
		return nil
	}
	if err := validateHostAgentStateOutsideCooperDir(cooperDir); err != nil {
		return err
	}

	return resetOwnedRoot(runtimefs.TempRoot(cooperDir), "runtime temporary")
}

// ResetRuntimeSessionRoot removes all host-controlled workload session files
// and recreates the root. No stale control file survives between runs.
func ResetRuntimeSessionRoot(cooperDir string) error {
	cooperDir = strings.TrimSpace(cooperDir)
	if cooperDir == "" {
		return nil
	}
	if err := validateHostAgentStateOutsideCooperDir(cooperDir); err != nil {
		return err
	}

	return resetOwnedRoot(runtimefs.SessionRoot(cooperDir), "runtime session")
}

func validateHostAgentStateOutsideCooperDir(cooperDir string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home directory before Cooper-owned cleanup: %w", err)
	}
	return workload.ValidateAllHostAgentStateRoots(homeDir, cooperDir)
}

func resetOwnedRoot(rootPath, label string) error {
	if err := removeAllWithPermissionRepair(rootPath); err != nil {
		return fmt.Errorf("remove %s root %s: %w", label, rootPath, err)
	}
	if err := os.MkdirAll(rootPath, 0o755); err != nil {
		return fmt.Errorf("create %s root %s: %w", label, rootPath, err)
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
