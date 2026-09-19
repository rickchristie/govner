package workload

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/rickchristie/govner/cooper/internal/profilelink"
)

var profileStorageID = regexp.MustCompile(`^[a-f0-9]{24}$`)

// ValidateProfileSource grants only one catalog root in a private generation.
// A profile is durable account data, so it has its own ownership kind rather
// than being accepted as a cache or an exception to host-state validation.
func ValidateProfileSource(mount MountSpec, cooperDir string) error {
	rootID := mount.ID
	if isProfileAlias(mount) {
		rootID = profilePrimaryID(mount.ID)
		if mount.Kind != Directory {
			return errors.New("canonical profile aliases require a directory root")
		}
	}
	relative, err := filepath.Rel(cooperDir, mount.Source)
	if err != nil {
		return err
	}
	parts := strings.Split(relative, string(filepath.Separator))
	if len(parts) != 7 || parts[0] != "profiles" || parts[1] != "harnesses" || parts[5] != "roots" ||
		!profileStorageID.MatchString(parts[3]) || !profileStorageID.MatchString(parts[4]) || parts[6] != rootID {
		return errors.New("profile source must be one root in a saved profile generation")
	}
	supported := false
	for _, spec := range agentStatePaths[parts[2]] {
		if spec.ID == rootID {
			supported = true
			break
		}
	}
	if !supported || mount.Access != ReadWrite {
		return errors.New("profile source is not a supported writable agent root")
	}
	for _, alias := range mount.CanonicalPaths {
		if _, err := profilelink.CanonicalStore(alias, parts[2], parts[3], rootID); err != nil {
			return err
		}
	}
	parent, err := os.OpenRoot(cooperDir)
	if err != nil {
		return err
	}
	defer parent.Close()
	current := ""
	for position, part := range parts {
		current = filepath.Join(current, part)
		info, err := parent.Lstat(current)
		if errors.Is(err, os.ErrNotExist) && position == len(parts)-1 && mount.Kind == Directory {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("profile storage must not contain root or parent symlinks")
		}
		if position < len(parts)-1 && (!info.IsDir() || info.Mode().Perm()&0o077 != 0 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid())) {
			return fmt.Errorf("profile storage %s must be a private directory", current)
		}
	}
	return nil
}
