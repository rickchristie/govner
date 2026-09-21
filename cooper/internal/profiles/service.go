package profiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/rickchristie/govner/cooper/internal/statelock"
	"github.com/rickchristie/govner/cooper/internal/usercontext"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

type Options struct {
	CooperDir, Workspace string
	Account              usercontext.Account
	Environment          map[string]string
	CredentialNames      func(string) []string
	Reader               IdentityReader
	Guard                UsageGuard
}

type Service struct {
	options Options
	now     func() time.Time
	// These boundaries let tests stop real filesystem operations at commit
	// points. The production path never substitutes a copy for a failed move.
	rename     func(*os.Root, string, string) error
	publish    func(*os.Root, index) error
	copy       func(context.Context, string, string) error
	checkpoint func(string) error
}

func New(options Options) *Service {
	return &Service{options: options, now: time.Now, rename: renameEntry,
		publish: func(root *os.Root, state index) error { return writeJSON(root, "index.json", state) },
		copy:    copyTree, checkpoint: func(string) error { return nil }}
}

func Supported() error {
	if runtime.GOOS != "linux" {
		return errors.New("account profiles require Linux; macOS profiles are not supported")
	}
	return nil
}

func (s *Service) storePath() string { return filepath.Join(s.options.CooperDir, "profiles") }

func (s *Service) open(create bool) (*os.Root, error) {
	if err := Supported(); err != nil {
		return nil, err
	}
	if err := s.options.Account.Validate(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(s.options.CooperDir) || filepath.Clean(s.options.CooperDir) != s.options.CooperDir || s.options.CooperDir == "/" {
		return nil, errors.New("profile store requires a clean absolute Cooper directory")
	}
	if create {
		if err := os.MkdirAll(s.options.CooperDir, 0700); err != nil {
			return nil, err
		}
	}
	parent, err := os.OpenRoot(s.options.CooperDir)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	if err := privatePath(parent, "profiles", create); err != nil {
		return nil, err
	}
	return parent.OpenRoot("profiles")
}

// All operations take the same per-user lock as runtime startup. Human
// confirmation is a typed result and is handled after this lock is released.
func (s *Service) locked(ctx context.Context, create, exclusive bool) (*os.Root, *statelock.Lock, index, error) {
	if err := Supported(); err != nil {
		return nil, nil, index{}, err
	}
	lock, err := statelock.Acquire(ctx, exclusive)
	if err != nil {
		return nil, nil, index{}, err
	}
	store, err := s.open(create)
	if err != nil {
		lock.Close()
		return nil, nil, index{}, err
	}
	state, err := readIndex(store)
	if err == nil {
		err = ready(store)
	}
	if err != nil {
		store.Close()
		lock.Close()
		return nil, nil, index{}, err
	}
	return store, lock, state, nil
}

func containsPath(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && (relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))))
}

// A rename leaves a shell's current directory in the outgoing profile.
// Reject both direct and symlink spellings before moving any state root.
func (s *Service) checkWorkspaceOutside(path string) error {
	workspace, err := workload.ResolvedPath(s.options.Workspace)
	if err != nil {
		return err
	}
	if containsPath(path, workspace) {
		return fmt.Errorf("agent state %s contains the working directory; run the profile command from outside that root", path)
	}
	return nil
}

func (s *Service) hostScope(harness string) (workload.AgentPaths, []Root, error) {
	paths, err := workload.ResolveProfileScope(harness, s.options.Account.Home, s.options.Workspace, s.options.Environment)
	if err != nil {
		return workload.AgentPaths{}, nil, err
	}
	var roots []Root
	for _, mount := range paths.Mounts {
		if err := workload.ValidateAgentStatePath(mount.Source, s.options.Account.Home, s.options.CooperDir); err != nil {
			return paths, nil, err
		}
		parent, err := workload.ResolvedPath(filepath.Dir(mount.Source))
		if err != nil {
			return paths, nil, err
		}
		path := filepath.Join(parent, filepath.Base(mount.Source))
		if err := s.checkWorkspaceOutside(path); err != nil {
			return paths, nil, err
		}
		roots = append(roots, Root{ID: mount.ID, Target: mount.Target, HostPath: path, Kind: mount.Kind})
	}
	for position, root := range roots {
		for _, other := range roots[position+1:] {
			if containsPath(root.HostPath, other.HostPath) || containsPath(other.HostPath, root.HostPath) {
				return paths, nil, errors.New("profile roots overlap; use separate complete state roots")
			}
		}
	}
	return paths, roots, checkHostExecutable(harness, roots)
}

func (s *Service) checkProfile(profile Manifest) error {
	if err := validateManifest(profile); err != nil {
		return err
	}
	if profile.Account != s.options.Account {
		return errors.New("profile belongs to a different host account or home")
	}
	if err := s.checkRootOwnership(newIndex(), profile.Harness, profile.Roots); err != nil {
		return err
	}
	policy, err := workload.AgentStatePolicy(profile.Harness)
	if err != nil {
		return err
	}
	if profile.Policy != policy {
		return errors.New("profile root rules changed; the stored data was retained")
	}
	paths, err := workload.ResolveProfileScope(profile.Harness, profile.Account.Home, s.options.Workspace, profile.PathEnvironment)
	if err != nil {
		return err
	}
	if len(paths.Mounts) != len(profile.Roots) || !reflect.DeepEqual(paths.Environment, profile.Environment) {
		return errors.New("profile path settings do not match the root catalog")
	}
	for position, mount := range paths.Mounts {
		root := profile.Roots[position]
		parent, err := workload.ResolvedPath(filepath.Dir(mount.Target))
		if err != nil {
			return err
		}
		if root.ID != mount.ID || root.Target != mount.Target || root.Kind != mount.Kind || root.HostPath != filepath.Join(parent, filepath.Base(mount.Target)) {
			return errors.New("profile state paths changed; restore the original path settings")
		}
		if err := workload.ValidateAgentStatePath(root.HostPath, profile.Account.Home, s.options.CooperDir); err != nil {
			return err
		}
	}
	return nil
}

func sibling(root Root, id string) string { return root.HostPath + ".cooper-" + id }

func sourcePath(state index, profile Manifest, root Root) string {
	if state.Hosts[profile.Harness].ProfileID == profile.ID {
		return root.HostPath
	}
	return sibling(root, profile.ID)
}

func profilePaths(state index, profile Manifest) []string {
	paths := make([]string, 0, len(profile.Roots))
	for _, root := range profile.Roots {
		paths = append(paths, sourcePath(state, profile, root))
	}
	return paths
}

func (s *Service) checkUse(ctx context.Context, state index, profiles ...Manifest) error {
	var paths []string
	for _, profile := range profiles {
		for _, path := range profilePaths(state, profile) {
			if err := s.checkWorkspaceOutside(path); err != nil {
				return err
			}
			if err := checkRootMounts(path); err != nil {
				return err
			}
			paths = append(paths, path)
		}
	}
	return s.options.Guard.Check(ctx, paths)
}

func (s *Service) List(ctx context.Context) ([]Summary, error) {
	store, lock, state, err := s.locked(ctx, false, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer store.Close()
	defer lock.Close()
	var result []Summary
	for _, profile := range state.Profiles {
		if err := s.checkProfile(profile); err != nil {
			return nil, err
		}
		useErr := s.options.Guard.Check(ctx, profilePaths(state, profile))
		var issue *Issue
		if useErr != nil && (!errors.As(useErr, &issue) || issue.Kind != StateInUse) {
			return nil, useErr
		}
		credentials, err := s.readCredentials(store, profile)
		if err != nil {
			return nil, err
		}
		mismatch := profile.Identity.Key != "" && s.checkIdentity(ctx, state, profile, credentials) != nil
		result = append(result, Summary{ID: profile.ID, Harness: profile.Harness, Name: profile.Name, Account: profile.Identity.Label,
			Saved: profile.Saved, Loaded: state.Hosts[profile.Harness].ProfileID == profile.ID, Pending: profile.Identity.Key == "", InUse: useErr != nil, Mismatch: mismatch})
	}
	sort.Slice(result, func(a, b int) bool {
		if result[a].Harness != result[b].Harness {
			return result[a].Harness < result[b].Harness
		}
		return strings.ToLower(result[a].Name) < strings.ToLower(result[b].Name)
	})
	return result, nil
}

// OpenCode can install its executable below the root that a fresh profile
// replaces. Preserve the command needed to sign in to the new account.
func checkHostExecutable(harness string, roots []Root) error {
	if harness != "opencode" {
		return nil
	}
	for _, root := range roots {
		if root.ID != "opencode-compat" {
			continue
		}
		path := filepath.Join(root.HostPath, "bin", "opencode")
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return fmt.Errorf("OpenCode executable %s is inside a profile root; move it outside the state roots before saving", path)
		}
	}
	return nil
}
