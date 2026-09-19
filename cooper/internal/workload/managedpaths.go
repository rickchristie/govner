package workload

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/profilelink"
)

const profileAliasSuffix = "-canonical-"

// Native clients can save their host realpath in a session database. Expose
// that exact selected root at both paths, without mounting its store parent.
func profileAliases(mounts []MountSpec) ([]MountSpec, error) {
	var aliases []MountSpec
	for _, mount := range mounts {
		if (mount.Ownership != ProfileState && mount.Ownership != HostState) || mount.Kind != Directory || isProfileAlias(mount) {
			continue
		}
		paths := append([]string(nil), mount.CanonicalPaths...)
		seen := map[string]bool{mount.Target: true}
		for _, path := range paths {
			if !filepath.IsAbs(path) || filepath.Clean(path) != path {
				return nil, errors.New("canonical root path must be clean and absolute")
			}
			if seen[path] {
				continue
			}
			seen[path] = true
			alias := mount
			alias.ID += profileAliasSuffix + strconv.Itoa(len(aliases))
			alias.Target = path
			aliases = append(aliases, alias)
		}
	}
	return aliases, nil
}

func profilePrimaryID(id string) string {
	position := strings.LastIndex(id, profileAliasSuffix)
	if position < 0 {
		return ""
	}
	number := id[position+len(profileAliasSuffix):]
	index, err := strconv.Atoi(number)
	if err != nil || index < 0 || strconv.Itoa(index) != number {
		return ""
	}
	return id[:position]
}

func isProfileAlias(mount MountSpec) bool {
	return (mount.Ownership == ProfileState || mount.Ownership == HostState) && profilePrimaryID(mount.ID) != ""
}

// An alias must accompany the approved public root, with the same source and
// access. It cannot authorize a sibling profile or stand alone as a new root.
func validateProfileAlias(mount MountSpec, mounts []MountSpec) error {
	if !isProfileAlias(mount) {
		return nil
	}
	id := profilePrimaryID(mount.ID)
	for _, primary := range mounts {
		if primary.ID == id && primary.Ownership == mount.Ownership && primary.Source == mount.Source &&
			primary.Kind == mount.Kind && primary.Access == mount.Access {
			aliases, err := profileAliases([]MountSpec{primary})
			if err != nil {
				return err
			}
			for _, alias := range aliases {
				if alias.Target == mount.Target {
					return nil
				}
			}
		}
	}
	return errors.New("canonical profile alias requires its selected public root")
}

func isGitHooksMount(mount MountSpec) bool {
	return mount.ID == "git-hooks" || profilePrimaryID(mount.ID) == "git-hooks"
}

// Canonical targets can be below profile storage but must never cover home
// or a runtime system path. Account/path membership is checked by profiles.
func ValidateCanonicalTarget(path, home string) error {
	if err := validateHomeBoundary(path, home, "canonical profile path"); err != nil {
		return err
	}
	for _, protected := range protectedStatePaths {
		if pathsOverlap(path, protected) {
			return errors.New("canonical profile path overlaps a protected runtime path")
		}
	}
	return nil
}

// ResolveManagedPaths changes only exact registered host sources. Credential
// selection remains a launch policy; this function never makes a host session
// into a named session or mounts profile control metadata.
func ResolveManagedPaths(paths AgentPaths, cooperDir string) (AgentPaths, error) {
	result := AgentPaths{Environment: append([]EnvVar(nil), paths.Environment...)}
	for _, mount := range paths.Mounts {
		if mount.Ownership != HostState {
			result.Mounts = append(result.Mounts, mount)
			continue
		}
		binding, managed, err := profilelink.Resolve(mount.Source, cooperDir)
		if err != nil {
			return AgentPaths{}, err
		}
		if managed {
			mount.Source = filepath.Join(cooperDir, "profiles", binding.Source)
			mount.Ownership = ProfileState
			mount.CanonicalPaths = binding.Aliases
			if err := ValidateProfileSource(mount, cooperDir); err != nil {
				return AgentPaths{}, err
			}
		}
		result.Mounts = append(result.Mounts, mount)
	}
	return result, nil
}

// selectedProfileChild permits a workspace (or its read-only Git hooks) below
// an already approved root. It never grants a parent, sibling profile, or the
// control store. Use the fixed source so a later selector cannot redirect it.
func selectedProfileChild(source string, mounts []MountSpec, cooperDir string) (string, bool, error) {
	resolved, err := resolveExistingPath(source)
	if err != nil {
		return "", false, err
	}
	for _, mount := range mounts {
		if mount.Ownership != ProfileState || mount.Kind != Directory {
			continue
		}
		if err := ValidateProfileSource(mount, cooperDir); err != nil {
			return "", false, err
		}
		root, err := resolveExistingPath(mount.Source)
		if err != nil {
			return "", false, err
		}
		if resolved != root && pathContains(root, resolved) {
			return resolved, true, nil
		}
	}
	return source, false, nil
}
