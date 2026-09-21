package workload

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
)

var profileStorageID = regexp.MustCompile(`^[a-f0-9]{24}$`)

type profileFileID struct{ Device, Inode uint64 }
type profileHost struct {
	ProfileID string `json:"profile_id"`
}
type profileRoot struct {
	ID, Target    string
	HostPath      string `json:"host_path"`
	Kind          PathKind
	Parent, Entry profileFileID
}
type profileCatalog struct {
	Schema   int
	Hosts    map[string]profileHost
	Profiles []struct {
		ID, Harness string
		Roots       []profileRoot
	}
}

// This bounded view contains only mount authority. The profile service checks
// the full schema and account identity. Keep it here to avoid a dependency
// cycle between profile operations and the common root catalog.
func readProfileCatalog(cooperDir string) (profileCatalog, error) {
	var catalog profileCatalog
	parent, err := os.OpenRoot(cooperDir)
	if err != nil {
		return catalog, err
	}
	defer parent.Close()
	info, err := parent.Lstat("profiles")
	if err != nil {
		return catalog, err
	}
	if runtime.GOOS != "linux" {
		return catalog, errors.New("account profiles require Linux; macOS profiles are not supported")
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return catalog, errors.New("profile metadata directory must be private and owned by this user")
	}
	store, err := parent.OpenRoot("profiles")
	if err != nil {
		return catalog, err
	}
	defer store.Close()
	if _, err := store.Lstat("transaction.json"); err == nil {
		return catalog, errors.New("profile state needs recovery before mounting")
	} else if !errors.Is(err, os.ErrNotExist) {
		return catalog, err
	}
	file, err := store.OpenFile("index.json", os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		entries, listErr := fs.ReadDir(store.FS(), ".")
		if listErr == nil && len(entries) == 0 {
			return profileCatalog{Schema: 3, Hosts: map[string]profileHost{}}, nil
		}
	}
	if err != nil {
		return catalog, fmt.Errorf("profile index cannot be opened: %v", err)
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return catalog, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 4<<20 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return catalog, errors.New("profile index must be a private regular file below 4 MiB")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 4<<20))
	if err := decoder.Decode(&catalog); err != nil {
		return catalog, errors.New("profile index cannot be read")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return catalog, errors.New("profile index has trailing data")
	}
	if catalog.Schema != 3 || catalog.Hosts == nil {
		return catalog, errors.New("unsupported profile storage format")
	}
	seen := map[string]bool{}
	for _, profile := range catalog.Profiles {
		if !profileStorageID.MatchString(profile.ID) || seen[profile.ID] {
			return catalog, errors.New("profile index has an invalid or duplicate ID")
		}
		seen[profile.ID] = true
	}
	return catalog, nil
}

func ValidateProfileSource(mount MountSpec, cooperDir string) error {
	if runtime.GOOS != "linux" {
		return errors.New("account profiles require Linux; macOS profiles are not supported")
	}
	if mount.Access != ReadWrite || mount.ID == "shared-agents" {
		return errors.New("profile source must be a writable account root")
	}
	catalog, err := readProfileCatalog(cooperDir)
	if err != nil {
		return err
	}
	for _, profile := range catalog.Profiles {
		for _, root := range profile.Roots {
			if root.ID != mount.ID || root.Target != mount.Target || root.Kind != mount.Kind {
				continue
			}
			expected := root.HostPath + ".cooper-" + profile.ID
			if catalog.Hosts[profile.Harness].ProfileID == profile.ID {
				expected = root.HostPath
			}
			if mount.Source != expected {
				continue
			}
			supported := false
			for _, spec := range agentStatePaths[profile.Harness] {
				if spec.ID == root.ID && spec.ID != "shared-agents" {
					supported = true
				}
			}
			if !supported {
				return errors.New("profile root is outside the harness catalog")
			}
			return validateProfileEntry(mount, root, cooperDir)
		}
	}
	return fmt.Errorf("profile source %s is not an exact registered root", mount.Source)
}

func validateProfileEntry(mount MountSpec, root profileRoot, cooperDir string) error {
	resolved, err := ResolvedPath(root.Target)
	if err != nil || resolved != root.HostPath {
		return errors.New("profile public path changed")
	}
	if err := ValidateHostOwnedPath(mount.Source, cooperDir); err != nil {
		return err
	}
	directory, err := os.Stat(filepath.Dir(mount.Source))
	if err != nil {
		return err
	}
	stat := directory.Sys().(*syscall.Stat_t)
	if root.Parent.Inode != stat.Ino || root.Parent.Device != uint64(stat.Dev) {
		return errors.New("profile parent identity changed")
	}
	entry, err := os.Lstat(mount.Source)
	if err != nil {
		return err
	}
	if (mount.Kind == Directory && !entry.IsDir()) || (mount.Kind == File && !entry.Mode().IsRegular()) {
		return errors.New("profile source type changed")
	}
	stat = entry.Sys().(*syscall.Stat_t)
	if stat.Uid != uint32(os.Getuid()) {
		return errors.New("profile source has another owner")
	}
	if mount.Kind == Directory && (root.Entry.Inode != stat.Ino || root.Entry.Device != uint64(stat.Dev)) {
		return errors.New("profile directory was replaced")
	}
	return nil
}

func isGitHooksMount(mount MountSpec) bool {
	return mount.ID == "git-hooks" || strings.HasPrefix(mount.ID, "git-hooks-state-")
}

// A named session maps a workspace below the public root into its selected
// account. The mount target stays unchanged. Other account roots and their
// parents must not enter a runtime through the workspace mount.
func selectedProfileChild(source string, mounts []MountSpec, cooperDir string) (string, bool, error) {
	resolved, err := resolveExistingPath(source)
	if err != nil {
		return "", false, err
	}
	for _, mount := range mounts {
		if mount.Kind != Directory {
			continue
		}
		if mount.Ownership == HostState {
			path, err := resolveExistingPath(mount.Source)
			if err != nil {
				return "", false, err
			}
			if pathContains(path, resolved) {
				return source, false, nil
			}
			continue
		}
		if mount.Ownership != ProfileState {
			continue
		}
		if err := ValidateProfileSource(mount, cooperDir); err != nil {
			return "", false, err
		}
		public, err := resolveExistingPath(mount.Target)
		if err != nil {
			return "", false, err
		}
		if pathContains(public, resolved) {
			relative, _ := filepath.Rel(public, resolved)
			return filepath.Join(mount.Source, relative), true, nil
		}
		if pathContains(mount.Source, resolved) {
			return resolved, true, nil
		}
	}
	catalog, err := readProfileCatalog(cooperDir)
	if errors.Is(err, os.ErrNotExist) {
		return source, false, nil
	}
	if err != nil {
		return "", false, err
	}
	for _, profile := range catalog.Profiles {
		for _, root := range profile.Roots {
			path := root.HostPath + ".cooper-" + profile.ID
			if pathsOverlap(path, resolved) || pathsOverlap(root.HostPath, resolved) || strings.HasPrefix(resolved, root.HostPath+".cooper-") {
				return "", false, errors.New("workspace overlaps an unselected profile or recovery root; use a workspace outside that state")
			}
		}
	}
	return source, false, nil
}
