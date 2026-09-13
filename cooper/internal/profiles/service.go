package profiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
	// The replacement boundary is injectable for crash and rollback tests.
	rename  func(*os.Root, string, string) error
	publish func(*os.Root, index) error
	copy    func(context.Context, string, string) error
}

func New(options Options) *Service {
	return &Service{options: options, now: time.Now, copy: copyTree, rename: func(root *os.Root, from, to string) error { return root.Rename(from, to) }, publish: func(root *os.Root, state index) error { return writeJSON(root, "index.json", state) }}
}

func (s *Service) storePath() string { return filepath.Join(s.options.CooperDir, "profiles") }

func (s *Service) open(create bool) (*os.Root, error) {
	if err := s.options.Account.Validate(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(s.options.CooperDir) || filepath.Clean(s.options.CooperDir) != s.options.CooperDir || s.options.CooperDir == "/" {
		return nil, errors.New("profile store requires a clean absolute Cooper directory")
	}
	if create {
		if err := os.MkdirAll(s.options.CooperDir, 0o700); err != nil {
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

func (s *Service) hostScope(harness string) (workload.AgentPaths, []Root, error) {
	paths, err := workload.ResolveAgentScope(harness, s.options.Account.Home, s.options.Workspace, s.options.Environment)
	if err != nil {
		return workload.AgentPaths{}, nil, err
	}
	var roots []Root
	for _, mount := range paths.Mounts {
		if err := workload.ValidateAgentStatePath(mount.Source, s.options.Account.Home, s.options.CooperDir); err != nil {
			return workload.AgentPaths{}, nil, err
		}
		resolved, err := workload.ResolvedPath(mount.Source)
		if err != nil {
			return workload.AgentPaths{}, nil, err
		}
		workspace, err := workload.ResolvedPath(s.options.Workspace)
		if err != nil {
			return workload.AgentPaths{}, nil, err
		}
		if containsPath(resolved, workspace) {
			return workload.AgentPaths{}, nil, fmt.Errorf("agent state %s contains the working directory; run the profile command from outside that root", mount.Source)
		}
		_, err = os.Stat(resolved)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return workload.AgentPaths{}, nil, err
		}
		roots = append(roots, Root{ID: mount.ID, Target: mount.Target, HostPath: resolved, Kind: mount.Kind, Present: err == nil})
	}
	for position, root := range roots {
		for _, other := range roots[position+1:] {
			if containsPath(root.HostPath, other.HostPath) || containsPath(other.HostPath, root.HostPath) {
				return workload.AgentPaths{}, nil, errors.New("agent root aliases overlap; use separate complete state roots")
			}
		}
	}
	return paths, roots, nil
}

func containsPath(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && (relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))))
}

func rootPaths(roots []Root) []string {
	paths := make([]string, 0, len(roots))
	for _, root := range roots {
		paths = append(paths, root.HostPath)
	}
	return paths
}

func (s *Service) checkProfile(profile Manifest) error {
	if err := s.checkProfilePaths(profile); err != nil {
		return err
	}
	policy, err := workload.AgentStatePolicy(profile.Harness)
	if err != nil {
		return err
	}
	if profile.Policy != policy {
		return errors.New("profile state-root rules changed; refresh this account from its host state with 'cooper save'")
	}
	expected, err := workload.ResolveAgentScope(profile.Harness, profile.Account.Home, s.options.Workspace, profile.PathEnvironment)
	if err != nil {
		return err
	}
	if len(expected.Mounts) != len(profile.Roots) || !reflect.DeepEqual(expected.Environment, profile.Environment) {
		return errors.New("profile path settings do not match the state-root catalog")
	}
	for position, mount := range expected.Mounts {
		root := profile.Roots[position]
		if root.ID != mount.ID || root.Target != mount.Target || root.Kind != mount.Kind {
			return errors.New("profile paths do not match this working directory; use the original path settings or absolute path overrides")
		}
	}
	return nil
}

func (s *Service) checkProfilePaths(profile Manifest) error {
	if err := validateManifest(profile); err != nil {
		return err
	}
	if profile.Account != s.options.Account {
		return errors.New("profile belongs to a different host account or home")
	}
	for _, root := range profile.Roots {
		for _, path := range []string{root.Target, root.HostPath} {
			if err := workload.ValidateAgentStatePath(path, profile.Account.Home, s.options.CooperDir); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) List(ctx context.Context) ([]Summary, error) {
	lock, err := statelock.Acquire(ctx, false)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	root, err := s.open(false)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	state, err := readIndex(root)
	if err != nil {
		return nil, err
	}
	var result []Summary
	for _, profile := range state.Profiles {
		host := state.Hosts[profile.Harness]
		if err := s.checkProfilePaths(profile); err != nil {
			return nil, err
		}
		usageErr := s.options.Guard.Check(ctx, []string{filepath.Join(s.storePath(), dataPath(profile))})
		var issue *Issue
		if usageErr != nil && (!errors.As(usageErr, &issue) || issue.Kind != StateInUse) {
			return nil, usageErr
		}
		result = append(result, Summary{ID: profile.ID, Harness: profile.Harness, Name: profile.Name, Account: profile.Identity.Label,
			Saved: profile.Saved, Loaded: host.ProfileID == profile.ID, Pending: profile.Identity.Key == "", InUse: usageErr != nil})
	}
	sort.Slice(result, func(a, b int) bool {
		if result[a].Harness != result[b].Harness {
			return result[a].Harness < result[b].Harness
		}
		return strings.ToLower(result[a].Name) < strings.ToLower(result[b].Name)
	})
	return result, nil
}

func (s *Service) Save(ctx context.Context, request SaveRequest) (Result, error) {
	if err := validateConflictChoice(request.ConflictChoice); err != nil {
		return Result{}, err
	}
	paths, roots, err := s.hostScope(request.Harness)
	if err != nil {
		return Result{}, err
	}
	lock, err := statelock.Acquire(ctx, true)
	if err != nil {
		return Result{}, err
	}
	defer lock.Close()
	store, err := s.open(true)
	if err != nil {
		return Result{}, err
	}
	defer store.Close()
	state, err := readIndex(store)
	if err != nil {
		return Result{}, err
	}
	if err := s.recoverTransaction(ctx, store, &state); err != nil {
		return Result{}, err
	}
	paths, roots, err = s.hostScope(request.Harness)
	if err != nil {
		return Result{}, err
	}
	return s.save(ctx, store, &state, request, paths, roots)
}

func (s *Service) Delete(ctx context.Context, harness, name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	lock, err := statelock.Acquire(ctx, true)
	if err != nil {
		return err
	}
	defer lock.Close()
	store, err := s.open(false)
	if err != nil {
		return err
	}
	defer store.Close()
	state, err := readIndex(store)
	if err != nil {
		return err
	}
	if err := s.recoverTransaction(ctx, store, &state); err != nil {
		return err
	}
	profile := state.byName(harness, name)
	if profile == nil {
		return fmt.Errorf("profile %s/%s does not exist", harness, name)
	}
	if state.Hosts[harness].ProfileID == profile.ID {
		return &Issue{Kind: StateInUse, Message: "load another profile before deleting the profile selected on the host"}
	}
	path := filepath.Join("harnesses", harness, profile.ID)
	if err := privatePath(store, path, false); err != nil {
		return err
	}
	if err := s.options.Guard.Check(ctx, []string{filepath.Join(s.storePath(), path)}); err != nil {
		return err
	}
	// Remove the index entry before data. A crash can leave unreferenced data,
	// but must not leave a profile that points to already deleted credentials.
	id := profile.ID
	kept := make([]Manifest, 0, len(state.Profiles)-1)
	for _, candidate := range state.Profiles {
		if candidate.ID != id {
			kept = append(kept, candidate)
		}
	}
	state.Profiles = kept
	if err := s.publish(store, state); err != nil {
		return err
	}
	return removeTree(store, path)
}
