package profiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/rickchristie/govner/cooper/internal/profilelink"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

func (s *Service) managedSave(ctx context.Context, request SaveRequest) (Result, error) {
	if err := validateConflictChoice(request.ConflictChoice); err != nil {
		return Result{}, err
	}
	store, lock, view, state, err := s.openManaged(ctx, true)
	if err != nil {
		return Result{}, err
	}
	defer store.Close()
	defer lock.Close()
	profile := state.byID(state.Hosts[request.Harness].ProfileID)
	if profile == nil {
		return s.adoptManaged(ctx, store, view, state, request)
	}
	if request.NewName != "" {
		return Result{}, errors.New("the selected live profile already has a name; use load to create another account")
	}
	if err := s.checkManagedEnvironment(*profile); err != nil {
		return Result{}, err
	}
	if !fullySelected(view, *profile) {
		return Result{}, &Issue{Kind: MixedState, Message: "another harness selected a shared root; reload this profile before saving"}
	}
	if err := s.checkManagedProfile(*profile); err != nil {
		return Result{}, err
	}
	if err := s.managedUse(ctx, view, *profile); err != nil {
		return Result{}, err
	}
	identity, err := s.managedIdentity(ctx, store, view, *profile, true)
	if err != nil || identity.Key == "" {
		return Result{}, &Issue{Kind: IdentityUnknown, Message: "live profile identity is unavailable; sign in on the host; state was retained"}
	}
	if profile.Identity.Key != "" && profile.Identity.Key != identity.Key {
		return Result{}, &Issue{Kind: AccountConflict, Message: "the live profile account changed; restore its login or recover a backup; use load with a new name before changing accounts"}
	}
	if existing := state.byIdentity(request.Harness, identity.Key); existing != nil && existing.ID != profile.ID {
		return Result{}, &Issue{Kind: AccountConflict, Message: "this account already belongs to another profile; live state was retained"}
	}
	txn, err := newManagedTransaction("switch", view.ID)
	if err != nil {
		return Result{}, err
	}
	bindings, err := s.syncManagedFiles(ctx, store, view, *profile, request.ConflictChoice, &txn)
	if err != nil {
		return Result{}, err
	}
	profile.Identity = identity
	profile.Saved = s.now().UTC()
	if err := s.recordCredentials(store, profile); err != nil {
		return Result{}, err
	}
	host := state.Hosts[request.Harness]
	host.Pending = false
	state.Hosts[request.Harness] = host
	_, err = s.commitManaged(ctx, store, view, state, bindings, txn)
	return Result{Saved: profile.Name, Managed: true}, err
}

func (s *Service) managedLoad(ctx context.Context, request LoadRequest) (Result, error) {
	if err := ValidateName(request.Name); err != nil {
		return Result{}, err
	}
	if err := validateConflictChoice(request.ConflictChoice); err != nil {
		return Result{}, err
	}
	if request.NewName != "" {
		return Result{}, errors.New("managed accounts use their selected profile; create new accounts with load before login")
	}
	store, lock, view, state, err := s.openManaged(ctx, true)
	if err != nil {
		return Result{}, err
	}
	defer store.Close()
	defer lock.Close()
	outgoing := state.byID(state.Hosts[request.Harness].ProfileID)
	if outgoing == nil {
		return Result{}, errors.New("save this harness once before loading a managed profile")
	}
	if err := checkHostExecutable(request.Harness, outgoing.Roots); err != nil {
		return Result{}, err
	}
	if err := s.checkManagedEnvironment(*outgoing); err != nil {
		return Result{}, err
	}
	if err := s.checkManagedProfile(*outgoing); err != nil {
		return Result{}, err
	}
	if outgoing.Identity.Key != "" {
		if err := s.checkManagedIdentity(ctx, store, view, *outgoing); err != nil {
			return Result{}, err
		}
	}
	target := state.byName(request.Harness, request.Name)
	created := target == nil
	if created {
		if err := compatibleCredentials(s.credentials(request.Harness), emptyCredentials(s.credentials(request.Harness))); err != nil {
			return Result{}, err
		}
	} else {
		if err := s.checkManagedEnvironment(*target); err != nil {
			return Result{}, err
		}
		if err := s.checkManagedProfile(*target); err != nil {
			return Result{}, err
		}
		credentials, err := s.readCredentials(store, *target)
		if err != nil {
			return Result{}, err
		}
		if err := compatibleCredentials(s.credentials(request.Harness), credentials); err != nil {
			return Result{}, err
		}
		if target.Identity.Key != "" {
			if err := s.checkManagedIdentity(ctx, store, view, *target); err != nil {
				return Result{}, err
			}
		}
	}
	users := []Manifest{*outgoing}
	if target != nil {
		users = append(users, *target)
	}
	if err := s.managedUse(ctx, view, users...); err != nil {
		return Result{}, err
	}
	if target != nil && target.ID == outgoing.ID && fullySelected(view, *target) {
		// A same-profile load also reconciles only its standalone config files.
		// Its session directories are never read or copied.
		txn, err := newManagedTransaction("switch", view.ID)
		if err != nil {
			return Result{}, err
		}
		bindings, err := s.syncManagedFiles(ctx, store, view, *outgoing, request.ConflictChoice, &txn)
		if err != nil {
			return Result{}, err
		}
		if len(txn.Changes) == 0 && reflect.DeepEqual(bindings, view.Bindings) {
			return Result{Loaded: target.Name, Pending: target.Identity.Key == "", Unchanged: true, Managed: true}, nil
		}
		_, err = s.commitManaged(ctx, store, view, state, bindings, txn)
		return Result{Loaded: target.Name, Pending: target.Identity.Key == "", Managed: true}, err
	}
	if created {
		paths := workload.AgentPaths{Environment: outgoing.Environment}
		profile, err := s.createEmpty(ctx, store, request.Harness, request.Name, paths, outgoing.Roots)
		if err != nil {
			return Result{}, err
		}
		if err := s.addCanonicalPaths(&profile); err != nil {
			return Result{}, err
		}
		state.put(profile)
		target = state.byID(profile.ID)
		// Appending a profile can move the slice holding outgoing.
		outgoing = state.byID(state.Hosts[request.Harness].ProfileID)
	}
	txn, err := newManagedTransaction("switch", view.ID)
	if err != nil {
		return Result{}, err
	}
	bindings, err := s.syncManagedFiles(ctx, store, view, *outgoing, request.ConflictChoice, &txn)
	if err != nil {
		return Result{}, err
	}
	bindings, err = s.selectManagedBindings(ctx, store, bindings, *target, &txn)
	if err != nil {
		return Result{}, err
	}
	host := state.Hosts[request.Harness]
	host.ProfileID, host.Pending = target.ID, target.Identity.Key == ""
	host.BaseDigest = target.Digest
	state.Hosts[request.Harness] = host
	_, err = s.commitManaged(ctx, store, view, state, bindings, txn)
	return Result{Saved: outgoing.Name, Loaded: target.Name, Created: created, Pending: host.Pending, Managed: true}, err
}

func managedFileDigest(ctx context.Context, path string) (string, error) {
	parent, err := os.OpenRoot(filepath.Dir(path))
	if errors.Is(err, os.ErrNotExist) {
		return "absent", nil
	}
	if err != nil {
		return "", err
	}
	defer parent.Close()
	entry, err := managedEntryAt(ctx, parent, filepath.Base(path))
	if err != nil {
		return "", err
	}
	if !entry.Present {
		return "absent", nil
	}
	if entry.Digest == "" {
		return "", errors.New("standalone profile state must be a regular file")
	}
	return entry.Digest, nil
}

func (s *Service) syncManagedFiles(ctx context.Context, store *os.Root, view profilelink.View, profile Manifest, choice ConflictChoice, txn *managedTransaction) ([]profilelink.Binding, error) {
	bindings := append([]profilelink.Binding(nil), view.Bindings...)
	for position, binding := range bindings {
		if binding.Kind != "file" || binding.ProfileID != profile.ID {
			continue
		}
		savedPath := filepath.Join(s.storePath(), binding.Source)
		host, err := managedFileDigest(ctx, binding.Path)
		if err != nil {
			return nil, err
		}
		saved, err := managedFileDigest(ctx, savedPath)
		if err != nil {
			return nil, err
		}
		switch {
		case host == saved:
			bindings[position].Base = host
		case saved == binding.Base || choice == KeepHost:
			if err := s.stageManagedChange(ctx, txn, savedPath, binding.Path, ""); err != nil {
				return nil, err
			}
			bindings[position].Base = host
		case host == binding.Base || choice == KeepSaved:
			if err := s.stageManagedChange(ctx, txn, binding.Path, savedPath, ""); err != nil {
				return nil, err
			}
			bindings[position].Base = saved
		default:
			return nil, &Issue{Kind: StateConflict, Message: "host and saved standalone file both changed; use --conflict host or --conflict saved; both files were retained"}
		}
	}
	return bindings, nil
}

func (s *Service) selectManagedBindings(ctx context.Context, store *os.Root, bindings []profilelink.Binding, profile Manifest, txn *managedTransaction) ([]profilelink.Binding, error) {
	result := append([]profilelink.Binding(nil), bindings...)
	for _, root := range profile.Roots {
		position := -1
		for i, binding := range result {
			if binding.Path == root.HostPath {
				position = i
				break
			}
		}
		binding := profilelink.Binding{Path: root.HostPath, ProfileID: profile.ID, RootID: root.ID, Kind: string(root.Kind), Source: filepath.Join(dataPath(profile), "roots", root.ID), Aliases: root.Aliases}
		if position >= 0 {
			binding.ID = result[position].ID
		} else {
			id, err := newID()
			if err != nil {
				return nil, err
			}
			binding.ID = id
		}
		if root.Kind == workload.File {
			source := filepath.Join(s.storePath(), binding.Source)
			digest, err := managedFileDigest(ctx, source)
			if err != nil {
				return nil, err
			}
			binding.Base = digest
			// An outgoing file sync can already own the host replacement. The
			// incoming profile must be the final host file for this transaction.
			if err := s.replaceStagedFile(ctx, txn, root.HostPath, source); err != nil {
				return nil, err
			}
		} else if position < 0 {
			if err := s.stageManagedChange(ctx, txn, root.HostPath, "", profilelink.Alias(s.options.CooperDir, binding)); err != nil {
				return nil, err
			}
		}
		if position >= 0 {
			result[position] = binding
		} else {
			result = append(result, binding)
		}
	}
	return result, nil
}

func (s *Service) replaceStagedFile(ctx context.Context, txn *managedTransaction, path, source string) error {
	for position, change := range txn.Changes {
		if change.Path != path {
			continue
		}
		parent, err := managedParent(change)
		if err != nil {
			return err
		}
		defer parent.Close()
		stage := txn.sibling(position, "next")
		if err := parent.Remove(stage); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if _, err := os.Lstat(source); err == nil {
			if err := s.copyManaged(ctx, source, filepath.Join(filepath.Dir(path), stage)); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		after, err := managedEntryAt(ctx, parent, stage)
		if err != nil {
			return err
		}
		txn.Changes[position].After = after
		return syncRoot(parent, ".")
	}
	return s.stageManagedChange(ctx, txn, path, source, "")
}

func (s *Service) managedSelect(ctx context.Context, harness, key string, byID bool) (Selection, error) {
	store, lock, view, state, err := s.openManaged(ctx, false)
	if err != nil {
		return Selection{}, err
	}
	defer store.Close()
	defer lock.Close()
	if key == "" {
		if selected := state.byID(state.Hosts[harness].ProfileID); selected != nil && selected.Identity.Key != "" {
			identity, err := s.managedIdentity(ctx, store, view, *selected, true)
			if err != nil || identity.Key != selected.Identity.Key {
				return Selection{}, &Issue{Kind: AccountConflict, Message: "host login or credential environment does not match the live profile; state was retained"}
			}
		}
		paths, err := workload.ResolveAgentPaths(harness, s.options.Account.Home, s.options.Workspace, s.options.Environment)
		if err != nil {
			return Selection{}, err
		}
		paths, err = workload.ResolveManagedPaths(paths, s.options.CooperDir)
		return Selection{Paths: paths}, err
	}
	if byID && !storedID.MatchString(key) {
		return Selection{}, errors.New("invalid profile ID")
	}
	if !byID {
		if err := ValidateName(key); err != nil {
			return Selection{}, err
		}
	}
	profile := state.byName(harness, key)
	if byID {
		profile = state.byID(key)
	}
	if profile == nil || profile.Harness != harness {
		return Selection{}, fmt.Errorf("profile %s/%s does not exist", harness, key)
	}
	if err := s.checkManagedProfile(*profile); err != nil {
		return Selection{}, err
	}
	if profile.Identity.Key == "" {
		return Selection{}, errors.New("profile needs login; log in on the host and run cooper save")
	}
	if err := s.checkManagedIdentity(ctx, store, view, *profile); err != nil {
		return Selection{}, err
	}
	credentials, err := s.readCredentials(store, *profile)
	if err != nil {
		return Selection{}, err
	}
	mounts, err := s.managedMounts(view, *profile)
	if err != nil {
		return Selection{}, err
	}
	return Selection{ID: profile.ID, Name: profile.Name, Credentials: credentials, Paths: workload.AgentPaths{Mounts: mounts, Environment: profile.Environment}}, nil
}

func (s *Service) managedList(ctx context.Context) ([]Summary, error) {
	store, lock, view, state, err := s.openManaged(ctx, false)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	defer lock.Close()
	var result []Summary
	for _, profile := range state.Profiles {
		usePaths := []string{filepath.Join(s.storePath(), dataPath(profile))}
		var hostRoots []HostRoot
		for _, root := range profile.Roots {
			binding, exists := bindingFor(view, root.HostPath)
			status := HostRoot{Path: root.Target}
			if exists {
				owner := state.byID(binding.ProfileID)
				status.Harness, status.Profile, status.Selected = owner.Harness, owner.Name, owner.ID == profile.ID
				if status.Selected && root.Kind == workload.File {
					usePaths = append(usePaths, root.HostPath)
				}
			}
			hostRoots = append(hostRoots, status)
		}
		useErr := s.options.Guard.Check(ctx, usePaths)
		var issue *Issue
		if useErr != nil && (!errors.As(useErr, &issue) || issue.Kind != StateInUse) {
			return nil, useErr
		}
		selected := fullySelected(view, profile)
		partial := false
		for _, binding := range view.Bindings {
			if binding.ProfileID == profile.ID {
				partial = true
			}
		}
		mismatch := profile.Identity.Key != "" && s.checkManagedIdentity(ctx, store, view, profile) != nil
		result = append(result, Summary{ID: profile.ID, Harness: profile.Harness, Name: profile.Name, Account: profile.Identity.Label, Saved: profile.Saved, Loaded: selected, Mixed: partial && !selected, Pending: profile.Identity.Key == "", InUse: useErr != nil, Managed: true, Mismatch: mismatch, HostRoots: hostRoots})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Harness != result[j].Harness {
			return result[i].Harness < result[j].Harness
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func (s *Service) managedDelete(ctx context.Context, harness, name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	store, lock, view, state, err := s.openManaged(ctx, true)
	if err != nil {
		return err
	}
	defer store.Close()
	defer lock.Close()
	profile := state.byName(harness, name)
	if profile == nil {
		return fmt.Errorf("profile %s/%s does not exist", harness, name)
	}
	for _, binding := range view.Bindings {
		if binding.ProfileID == profile.ID {
			return &Issue{Kind: StateInUse, Message: "this profile still supplies host state; load another profile first"}
		}
	}
	if err := s.options.Guard.Check(ctx, []string{filepath.Join(s.storePath(), "harnesses", harness, profile.ID)}); err != nil {
		return err
	}
	records, err := managedRecoveryRecords(store)
	if err != nil {
		return err
	}
	if len(records) > 0 {
		return errors.New("managed recovery still refers to profile data; make a backup, then run cooper profiles prune-recovery --yes before deletion")
	}
	id := profile.ID
	state.Profiles = append([]Manifest(nil), state.Profiles...)
	for position, entry := range state.Profiles {
		if entry.ID == id {
			state.Profiles = append(state.Profiles[:position], state.Profiles[position+1:]...)
			break
		}
	}
	for key, host := range state.Hosts {
		if host.ProfileID == id {
			delete(state.Hosts, key)
		}
	}
	txn, err := newManagedTransaction("switch", view.ID)
	if err != nil {
		return err
	}
	if _, err := s.commitManaged(ctx, store, view, state, view.Bindings, txn); err != nil {
		return err
	}
	return removeTree(store, filepath.Join("harnesses", harness, id))
}
