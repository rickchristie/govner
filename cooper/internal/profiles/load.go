package profiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/rickchristie/govner/cooper/internal/statelock"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

func (s *Service) Load(ctx context.Context, request LoadRequest) (Result, error) {
	if err := validateConflictChoice(request.ConflictChoice); err != nil {
		return Result{}, err
	}
	if err := ValidateName(request.Name); err != nil {
		return Result{}, err
	}
	lock, err := statelock.Acquire(ctx, true)
	if err != nil {
		return Result{}, err
	}
	defer lock.Close()
	paths, roots, err := s.hostScope(request.Harness)
	if err != nil {
		return Result{}, err
	}
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
	if err := s.options.Guard.Check(ctx, rootPaths(roots)); err != nil {
		return Result{}, err
	}
	if err := checkHostExecutable(request.Harness, roots); err != nil {
		return Result{}, err
	}
	if target := state.byName(request.Harness, request.Name); target != nil {
		if err := s.checkLoadTarget(ctx, store, *target, paths, roots); err != nil {
			return Result{}, err
		}
	} else if err := compatibleCredentials(s.credentials(request.Harness), emptyCredentials(s.credentials(request.Harness))); err != nil {
		return Result{}, err
	}
	saved, err := s.saveOutgoing(ctx, store, &state, request, paths, roots)
	if err != nil {
		return Result{}, err
	}
	target := state.byName(request.Harness, request.Name)
	created := target == nil
	if created {
		profile, err := s.createEmpty(ctx, store, request.Harness, request.Name, paths, roots)
		if err != nil {
			return Result{}, err
		}
		state.put(profile)
		if err := s.publish(store, state); err != nil {
			return Result{}, err
		}
		target = state.byID(profile.ID)
	}
	if err := s.options.Guard.Check(ctx, []string{filepath.Join(s.storePath(), dataPath(*target))}); err != nil {
		return Result{}, err
	}
	incoming, err := s.profileDigest(ctx, store, *target)
	if err != nil {
		return Result{}, err
	}
	current, err := digestRoots(ctx, roots, hostSource, s.credentials(request.Harness))
	if err != nil {
		return Result{}, err
	}
	result := Result{Saved: saved.Saved, Loaded: target.Name, Created: created, Pending: target.Identity.Key == "", Warning: saved.Warning, Recovery: saved.Recovery}
	if incoming == current {
		previous := state.Hosts[request.Harness]
		state.Hosts[request.Harness] = HostSelection{ProfileID: target.ID, BaseDigest: incoming, Pending: result.Pending, RecoveryID: previous.RecoveryID}
		result.Unchanged = true
		return result, s.publish(store, state)
	}
	previousRecovery := state.Hosts[request.Harness].RecoveryID
	recovery, err := s.replaceHost(ctx, store, &state, *target, roots, incoming)
	if saved.Recovery != "" {
		result.Warning += " Host conflict copy: " + saved.Recovery
	}
	result.Recovery = recovery
	if err == nil && previousRecovery != "" {
		if cleanupErr := s.removeRecovery(ctx, store, previousRecovery); cleanupErr != nil {
			result.Warning += " Previous host recovery was retained: " + cleanupErr.Error()
		}
	}
	return result, err
}

// OpenCode's installer can put the host executable inside its state root.
// A complete root replacement would remove it, including the command needed
// to log in to a fresh profile. Refuse before changing either saved or live state.
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
			return fmt.Errorf("check host OpenCode executable: %w", err)
		}
		if info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return fmt.Errorf("OpenCode executable %q is inside a profile state root; move the executable outside all OpenCode state roots and update PATH before loading a profile", path)
		}
	}
	return nil
}

func (s *Service) saveOutgoing(ctx context.Context, store *os.Root, state *index, request LoadRequest, paths workload.AgentPaths, roots []Root) (Result, error) {
	host := state.Hosts[request.Harness]
	// A new empty profile has no account yet. Selecting another profile before
	// login is safe only while the host still has that exact empty state.
	if host.Pending {
		digest, err := digestRoots(ctx, roots, hostSource, s.credentials(request.Harness))
		if err != nil {
			return Result{}, err
		}
		if digest == host.BaseDigest {
			return Result{}, nil
		}
	}
	return s.save(ctx, store, state, SaveRequest{Harness: request.Harness, NewName: request.NewName, ConflictChoice: request.ConflictChoice}, paths, roots)
}

func (s *Service) checkLoadTarget(ctx context.Context, store *os.Root, profile Manifest, paths workload.AgentPaths, roots []Root) error {
	if err := s.checkProfile(profile); err != nil {
		return err
	}
	if len(roots) != len(profile.Roots) || !reflect.DeepEqual(paths.Environment, profile.Environment) {
		return errors.New("profile uses different state path settings; use the same path environment on the host before loading")
	}
	for index, root := range roots {
		stored := profile.Roots[index]
		if root.ID != stored.ID || root.Target != stored.Target || root.HostPath != stored.HostPath || root.Kind != stored.Kind {
			return errors.New("profile state paths changed; restore the original path settings and symlinks before loading")
		}
	}
	credentials, err := s.readCredentials(store, profile)
	if err != nil {
		return err
	}
	if profile.Identity.Key != "" {
		if err := s.checkSavedIdentity(ctx, profile, credentials); err != nil {
			return err
		}
	}
	return compatibleCredentials(s.credentials(profile.Harness), credentials)
}

func compatibleCredentials(current, incoming []workload.EnvVar) error {
	if len(current) != len(incoming) {
		return errors.New("profile credential rules changed; refresh the profile before loading")
	}
	for index, value := range current {
		next := incoming[index]
		if value.Name != next.Name {
			return errors.New("profile credential rules changed; refresh the profile before loading")
		}
		if value.Unset == next.Unset && value.Value == next.Value {
			continue
		}
		if next.Unset {
			return fmt.Errorf("unset %s in your shell before loading; this shell value would override the profile login", value.Name)
		}
		return fmt.Errorf("set %s to this profile's saved value before loading, or use 'cooper cli <harness> <profile>'; Cooper cannot change its parent shell", value.Name)
	}
	return nil
}

func emptyCredentials(values []workload.EnvVar) []workload.EnvVar {
	result := make([]workload.EnvVar, 0, len(values))
	for _, value := range values {
		result = append(result, workload.EnvVar{Name: value.Name, Secret: true, Unset: true})
	}
	return result
}

func (s *Service) readCredentials(store *os.Root, profile Manifest) ([]workload.EnvVar, error) {
	values, err := s.readStoredCredentials(store, profile)
	if err != nil {
		return nil, err
	}
	if len(values) != len(s.credentials(profile.Harness)) {
		return nil, errors.New("profile credential rules changed; refresh the profile before use")
	}
	return values, nil
}

// An older snapshot can omit a newly supported variable. Save compares the
// old credential scope before it captures the new scope. Launch and load
// require the complete current scope and never fill it from the host shell.
func (s *Service) readStoredCredentials(store *os.Root, profile Manifest) ([]workload.EnvVar, error) {
	path := dataPath(profile)
	if err := privatePath(store, path, false); err != nil {
		return nil, err
	}
	var values []workload.EnvVar
	if err := readJSON(store, filepath.Join(path, "credentials.json"), &values); err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, value := range s.credentials(profile.Harness) {
		allowed[value.Name] = true
	}
	previous := ""
	for index, value := range values {
		if !allowed[value.Name] || (index > 0 && value.Name <= previous) || !value.Secret || (value.Unset && value.Value != "") {
			return nil, errors.New("profile has invalid credential settings")
		}
		previous = value.Name
	}
	return values, nil
}

func (s *Service) createEmpty(ctx context.Context, store *os.Root, harness, name string, paths workload.AgentPaths, roots []Root) (Manifest, error) {
	id, err := newID()
	if err != nil {
		return Manifest{}, err
	}
	generation, err := newID()
	if err != nil {
		return Manifest{}, err
	}
	policy, err := workload.AgentStatePolicy(harness)
	if err != nil {
		return Manifest{}, err
	}
	profile := Manifest{Schema: Schema, ID: id, Generation: generation, Harness: harness, Name: name,
		Account: s.options.Account, Policy: policy, Created: s.now().UTC(), Saved: s.now().UTC(),
		Roots: append([]Root(nil), roots...), Environment: paths.Environment, PathEnvironment: pathValues(paths)}
	path := dataPath(profile)
	if err := privatePath(store, filepath.Join(path, "roots"), true); err != nil {
		return Manifest{}, err
	}
	for index := range profile.Roots {
		root := &profile.Roots[index]
		root.Present = root.Kind == workload.Directory
		if root.Present {
			if err := store.Mkdir(filepath.Join(path, "roots", root.ID), 0o700); err != nil {
				return Manifest{}, err
			}
		}
	}
	credentials := emptyCredentials(s.credentials(harness))
	if err := writeJSON(store, filepath.Join(path, "credentials.json"), credentials); err != nil {
		return Manifest{}, err
	}
	profile.Digest, err = digestRoots(ctx, profile.Roots, snapshotSource(filepath.Join(s.storePath(), path)), credentials)
	return profile, err
}
