package profiles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/rickchristie/govner/cooper/internal/profilelink"
	"github.com/rickchristie/govner/cooper/internal/statelock"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

func viewIndex(view profilelink.View) (index, error) {
	var state index
	decoder := json.NewDecoder(bytes.NewReader(view.Catalog))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return index{}, errors.New("managed catalog does not match its schema")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return index{}, errors.New("managed catalog has trailing data")
	}
	return validateIndex(state)
}

func (s *Service) usesManaged() (bool, error) {
	store, err := s.open(false)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer store.Close()
	var header map[string]json.RawMessage
	if err := readJSON(store, transactionFile, &header); err == nil {
		var schema int
		if err := json.Unmarshal(header["schema"], &schema); err != nil {
			return false, err
		}
		if schema == profilelink.Schema {
			return false, errors.New("profile state needs recovery; run 'cooper profiles recover' on the host")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}

	return profilelink.Managed(store)
}

func (s *Service) openManaged(ctx context.Context, exclusive bool) (*os.Root, *statelock.Lock, profilelink.View, index, error) {
	lock, err := statelock.Acquire(ctx, exclusive)
	if err != nil {
		return nil, nil, profilelink.View{}, index{}, err
	}
	store, err := s.open(false)
	if err != nil {
		lock.Close()
		return nil, nil, profilelink.View{}, index{}, err
	}
	fail := func(err error) (*os.Root, *statelock.Lock, profilelink.View, index, error) {
		store.Close()
		lock.Close()
		return nil, nil, profilelink.View{}, index{}, err
	}
	if err := profilelink.Ready(store); err != nil {
		return fail(err)
	}
	managed, err := profilelink.Managed(store)
	if err != nil {
		return fail(err)
	}
	if !managed {
		return fail(errors.New("profile storage mode changed; retry the command"))
	}
	view, state, err := s.readManagedView(store)
	if err != nil {
		return fail(err)
	}
	if exclusive {
		if err := s.checkManagedWorkspace(state); err != nil {
			return fail(err)
		}
	}
	return store, lock, view, state, nil
}

// A repeat build checks bounded metadata and root links, without scanning
// history or requiring active agents to stop when no conversion is needed.
func (s *Service) readManagedView(store *os.Root) (profilelink.View, index, error) {
	view, err := profilelink.Read(store)
	if err != nil {
		return profilelink.View{}, index{}, err
	}
	state, err := viewIndex(view)
	if err != nil {
		return profilelink.View{}, index{}, err
	}
	if err := s.validateManagedView(store, view, state); err != nil {
		return profilelink.View{}, index{}, err
	}
	if err := profilelink.CheckAliases(s.options.CooperDir, view); err != nil {
		return profilelink.View{}, index{}, err
	}
	if err := s.checkCanonicalPaths(state); err != nil {
		return profilelink.View{}, index{}, err
	}
	return view, state, nil
}

func (s *Service) checkManagedWorkspace(state index) error {
	workspace, err := workload.ResolvedPath(s.options.Workspace)
	if err != nil {
		return err
	}
	store, err := workload.ResolvedPath(s.storePath())
	if err != nil {
		return err
	}
	if containsPath(store, workspace) {
		return errors.New("run profile changes from outside profile storage")
	}
	for _, profile := range state.Profiles {
		for _, root := range profile.Roots {
			if containsPath(root.HostPath, workspace) || containsPath(root.HostPath, s.options.Workspace) {
				return errors.New("run profile changes from outside all managed state roots")
			}
		}
	}
	return nil
}

func (s *Service) validateManagedView(store *os.Root, view profilelink.View, state index) error {
	for _, profile := range state.Profiles {
		if err := s.checkManagedProfile(profile); err != nil {
			return err
		}
		if profile.Account != s.options.Account {
			return errors.New("profile belongs to a different host account or home")
		}
		if err := privatePath(store, filepath.Join(dataPath(profile), "roots"), false); err != nil {
			return err
		}
	}
	for _, binding := range view.Bindings {
		profile := state.byID(binding.ProfileID)
		if profile == nil {
			return errors.New("host binding refers to an unknown profile")
		}
		found := false
		for _, root := range profile.Roots {
			if root.ID == binding.RootID && root.HostPath == binding.Path && string(root.Kind) == binding.Kind && binding.Source == filepath.Join(dataPath(*profile), "roots", root.ID) && reflect.DeepEqual(root.Aliases, binding.Aliases) {
				found = true
				break
			}
		}
		if !found {
			return errors.New("host binding does not match the account root catalog")
		}
	}
	return nil
}

func (s *Service) prepareView(store *os.Root, previous string, state index, bindings []profilelink.Binding) (profilelink.View, error) {
	id, err := newID()
	if err != nil {
		return profilelink.View{}, err
	}
	catalog, err := json.Marshal(state)
	if err != nil {
		return profilelink.View{}, err
	}
	bindings = append([]profilelink.Binding(nil), bindings...)
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].Path < bindings[j].Path })
	view := profilelink.View{Schema: profilelink.Schema, ID: id, Previous: previous, Catalog: catalog, Bindings: bindings}
	if err := profilelink.Validate(view); err != nil {
		return profilelink.View{}, err
	}
	path := filepath.Join("views", id)
	if err := privatePath(store, filepath.Join(path, "roots"), true); err != nil {
		return profilelink.View{}, err
	}
	if err := s.managedCheckpoint("view-directories"); err != nil {
		return profilelink.View{}, err
	}
	for _, binding := range bindings {
		if binding.Kind != "directory" {
			continue
		}
		if err := store.Symlink(filepath.Join("..", "..", "..", binding.Source), filepath.Join(path, "roots", binding.ID)); err != nil {
			return profilelink.View{}, err
		}
	}
	if err := writeJSON(store, filepath.Join(path, "state.json"), view); err != nil {
		return profilelink.View{}, err
	}
	if err := syncRoot(store, filepath.Join(path, "roots")); err != nil {
		return profilelink.View{}, err
	}
	if err := syncRoot(store, "views"); err != nil {
		return profilelink.View{}, err
	}
	return view, nil
}

func (s *Service) recordCredentials(store *os.Root, profile *Manifest) error {
	return recordCredentialValues(store, profile, s.credentials(profile.Harness))
}

func recordCredentialValues(store *os.Root, profile *Manifest, credentials []workload.EnvVar) error {
	if err := privatePath(store, "credentials", true); err != nil {
		return err
	}
	id, err := newID()
	if err != nil {
		return err
	}
	if err := writeJSON(store, filepath.Join("credentials", id+".json"), credentials); err != nil {
		return err
	}
	profile.CredentialRevision = id
	return nil
}

func bindingFor(view profilelink.View, path string) (profilelink.Binding, bool) {
	for _, binding := range view.Bindings {
		if binding.Path == path {
			return binding, true
		}
	}
	return profilelink.Binding{}, false
}

func fullySelected(view profilelink.View, profile Manifest) bool {
	for _, root := range profile.Roots {
		binding, exists := bindingFor(view, root.HostPath)
		if !exists || binding.ProfileID != profile.ID || binding.RootID != root.ID {
			return false
		}
	}
	return true
}

func (s *Service) checkManagedProfile(profile Manifest) error {
	if err := validateManifest(profile); err != nil {
		return err
	}
	policy, err := workload.AgentStatePolicy(profile.Harness)
	if err != nil {
		return err
	}
	if policy != profile.Policy {
		return errors.New("profile root policy changed; use the previous compatible Cooper version to detach before updating the root catalog")
	}
	paths, err := workload.ResolveAgentScope(profile.Harness, profile.Account.Home, s.options.Workspace, profile.PathEnvironment)
	if err != nil {
		return err
	}
	if len(paths.Mounts) != len(profile.Roots) || !reflect.DeepEqual(paths.Environment, profile.Environment) {
		return errors.New("profile path settings changed; use the original path settings")
	}
	for position, root := range profile.Roots {
		mount := paths.Mounts[position]
		if err := workload.ValidateManagedHostPath(root.HostPath, profile.Account.Home, s.options.CooperDir); err != nil {
			return err
		}
		logical, _, _, err := profilelink.Locate(mount.Source)
		if err != nil {
			return err
		}
		if logical != root.HostPath {
			return errors.New("managed host path does not match the root catalog")
		}
		if root.ID != mount.ID || root.Target != mount.Target || root.Kind != mount.Kind {
			return errors.New("profile paths do not match the root catalog")
		}
	}
	return nil
}

// Active standalone files stay at their host path. This supports writers
// that replace a file rather than following a symlink. Directory roots are
// always live; only standalone files need bounded save/load reconciliation.
func (s *Service) managedMounts(view profilelink.View, profile Manifest) ([]workload.MountSpec, error) {
	var mounts []workload.MountSpec
	for _, root := range profile.Roots {
		source := filepath.Join(s.storePath(), dataPath(profile), "roots", root.ID)
		ownership := workload.ProfileState
		binding, exists := bindingFor(view, root.HostPath)
		if root.Kind == workload.File && exists && binding.ProfileID == profile.ID {
			source, ownership = root.HostPath, workload.HostState
		}
		info, err := os.Lstat(source)
		if errors.Is(err, os.ErrNotExist) && root.Kind == workload.File {
			continue
		}
		if err != nil {
			return nil, err
		}
		if (root.Kind == workload.File && !info.Mode().IsRegular()) || (root.Kind == workload.Directory && !info.IsDir()) {
			return nil, fmt.Errorf("profile root %s is missing or changed its type", root.ID)
		}
		mount := workload.MountSpec{ID: root.ID, Source: source, Target: root.Target, Kind: root.Kind, Access: workload.ReadWrite, Ownership: ownership, CanonicalPaths: root.Aliases}
		if ownership == workload.ProfileState {
			if err := workload.ValidateProfileSource(mount, s.options.CooperDir); err != nil {
				return nil, err
			}
		} else if err := workload.ValidateAgentStatePath(source, s.options.Account.Home, s.options.CooperDir); err != nil {
			return nil, err
		}
		mounts = append(mounts, mount)
	}
	return mounts, nil
}

func (s *Service) managedIdentity(ctx context.Context, store *os.Root, view profilelink.View, profile Manifest, hostEnvironment bool) (Identity, error) {
	mounts, err := s.managedMounts(view, profile)
	if err != nil {
		return Identity{}, err
	}
	if hostEnvironment {
		// Host identity checks must include local credential-source observations,
		// such as a session bus. These values are not saved or forwarded.
		return s.options.Reader.Read(ctx, profile.Harness, mounts, s.options.Environment)
	}
	env := map[string]string{}
	for name, value := range profile.PathEnvironment {
		env[name] = value
	}
	credentials, err := s.readCredentials(store, profile)
	if err != nil {
		return Identity{}, err
	}
	for _, value := range credentials {
		if !value.Unset {
			env[value.Name] = value.Value
		}
	}
	return s.options.Reader.Read(ctx, profile.Harness, mounts, env)
}

func (s *Service) checkManagedIdentity(ctx context.Context, store *os.Root, view profilelink.View, profile Manifest) error {
	identity, err := s.managedIdentity(ctx, store, view, profile, false)
	if err != nil || identity.Key != profile.Identity.Key || identity.Key == "" {
		return &Issue{Kind: AccountConflict, Message: "live profile login does not match its account; restore the login on the host or use a backup; state was retained"}
	}
	return nil
}

func (s *Service) managedUse(ctx context.Context, view profilelink.View, profiles ...Manifest) error {
	var paths []string
	for _, profile := range profiles {
		paths = append(paths, filepath.Join(s.storePath(), dataPath(profile)))
		for _, root := range profile.Roots {
			paths = append(paths, root.HostPath)
			paths = append(paths, root.Aliases...)
		}
	}
	return s.options.Guard.Check(ctx, paths)
}

func (s *Service) checkManagedEnvironment(profile Manifest) error {
	paths, err := workload.ResolveAgentScope(profile.Harness, s.options.Account.Home, s.options.Workspace, s.options.Environment)
	if err != nil {
		return err
	}
	if len(paths.Mounts) != len(profile.Roots) || !reflect.DeepEqual(paths.Environment, profile.Environment) {
		return errors.New("host state path settings changed; restore the profile path settings before saving or loading")
	}
	for i, mount := range paths.Mounts {
		path, _, _, err := profilelink.Locate(mount.Source)
		if err != nil {
			return err
		}
		root := profile.Roots[i]
		if path != root.HostPath || mount.Target != root.Target || mount.ID != root.ID || mount.Kind != root.Kind {
			return errors.New("host state paths changed; restore their original locations before saving or loading")
		}
	}
	return nil
}

func requireCopyMode(store *os.Root) error {
	managed, err := profilelink.Managed(store)
	if err != nil {
		return err
	}
	if managed {
		return errors.New("profile storage mode changed; retry the command")
	}
	return nil
}
