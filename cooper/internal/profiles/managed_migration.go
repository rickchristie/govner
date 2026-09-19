package profiles

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"syscall"

	"github.com/rickchristie/govner/cooper/internal/profilelink"
	"github.com/rickchristie/govner/cooper/internal/statelock"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

type MigrationRoot struct {
	Harness, Profile, Path, Method string
}

type MigrationPreview struct {
	Managed  bool
	Roots    []MigrationRoot
	Warnings []string
	Bytes    int64
}

type migrationInput struct {
	State       index
	HostSources map[string]bool
	Preview     MigrationPreview
}

// PreviewMigration reads state only. The actual migration repeats these checks
// under the mutation lock; a preview never grants permission to use stale data.
func (s *Service) PreviewMigration(ctx context.Context, choice ConflictChoice) (MigrationPreview, error) {
	if runtime.GOOS != "linux" {
		return MigrationPreview{}, errors.New("live profile storage requires Linux")
	}
	lock, err := statelock.Acquire(ctx, false)
	if err != nil {
		return MigrationPreview{}, err
	}
	defer lock.Close()
	store, err := s.open(false)
	if errors.Is(err, os.ErrNotExist) {
		return MigrationPreview{Warnings: []string{"No saved profiles. Build will initialize live profile storage."}}, nil
	}
	if err != nil {
		return MigrationPreview{}, err
	}
	defer store.Close()
	if err := profilelink.Ready(store); err != nil {
		return MigrationPreview{}, err
	}
	managed, err := profilelink.Managed(store)
	if err != nil {
		return MigrationPreview{}, err
	}
	if managed {
		return MigrationPreview{Managed: true}, nil
	}
	input, err := s.planMigration(ctx, store, choice)
	return input.Preview, err
}

func (s *Service) planMigration(ctx context.Context, store *os.Root, choice ConflictChoice) (migrationInput, error) {
	if runtime.GOOS != "linux" {
		return migrationInput{}, errors.New("managed profiles require Linux; this store remains in copy mode")
	}
	if err := validateConflictChoice(choice); err != nil {
		return migrationInput{}, err
	}
	state, err := readIndex(store)
	if err != nil {
		return migrationInput{}, err
	}
	if err := validateMigrationRoots(state); err != nil {
		return migrationInput{}, err
	}
	input := migrationInput{State: state, HostSources: map[string]bool{}}
	for _, profile := range state.Profiles {
		if err := s.checkProfile(profile); err != nil {
			return input, err
		}
		sources := []string{filepath.Join(s.storePath(), dataPath(profile))}
		for _, root := range profile.Roots {
			sources = append(sources, root.HostPath)
			sources = append(sources, root.Aliases...)
			canonical, err := workload.ResolvedPath(filepath.Join(s.storePath(), dataPath(profile), "roots", root.ID))
			if err != nil {
				return input, err
			}
			if err := workload.ValidateCanonicalTarget(canonical, profile.Account.Home); err != nil {
				return input, err
			}
		}
		if err := s.options.Guard.Check(ctx, sources); err != nil {
			return input, err
		}
		credentials, err := s.readCredentials(store, profile)
		if err != nil {
			return input, err
		}
		if profile.Identity.Key != "" {
			if err := s.checkSavedIdentity(ctx, profile, credentials); err != nil {
				return input, err
			}
		}
		if state.Hosts[profile.Harness].ProfileID == profile.ID {
			paths, roots, err := s.hostScope(profile.Harness)
			if err != nil {
				return input, err
			}
			if err := s.checkLoadTarget(ctx, store, profile, paths, roots); err != nil {
				return input, err
			}
			for _, root := range roots {
				if err := checkManagedTree(ctx, root.HostPath); err != nil {
					return input, err
				}
			}
			host, err := digestRoots(ctx, roots, hostSource, s.credentials(profile.Harness))
			if err != nil {
				return input, err
			}
			saved, err := s.profileDigest(ctx, store, profile)
			if err != nil {
				return input, err
			}
			base := state.Hosts[profile.Harness].BaseDigest
			switch {
			case host == saved:
				input.HostSources[profile.ID] = true
			case saved == base:
				input.HostSources[profile.ID] = true
			case host == base:
				input.HostSources[profile.ID] = false
			case choice == KeepHost:
				input.HostSources[profile.ID] = true
			case choice == KeepSaved:
				input.HostSources[profile.ID] = false
			default:
				return input, &Issue{Kind: StateConflict, Message: "host and saved profile changed; choose --conflict host or --conflict saved before migration"}
			}
			if input.HostSources[profile.ID] && profile.Identity.Key != "" {
				identity, err := s.options.Reader.Read(ctx, profile.Harness, paths.Mounts, s.options.Environment)
				if err != nil || identity.Key != profile.Identity.Key {
					return input, &Issue{Kind: AccountConflict, Message: "host login changed; save or resolve the account in copy mode before migration"}
				}
			}
		}
		for _, root := range profile.Roots {
			source := filepath.Join(s.storePath(), dataPath(profile), "roots", root.ID)
			if input.HostSources[profile.ID] {
				source = root.HostPath
			}
			if root.Kind == workload.File {
				if _, err := managedFileDigest(ctx, source); err != nil {
					return input, err
				}
			}
			if err := checkManagedTree(ctx, source); err != nil {
				return input, fmt.Errorf("cannot migrate %s/%s root %s: %w", profile.Harness, profile.Name, root.ID, err)
			}
			size, err := managedTreeSize(ctx, source)
			if err != nil {
				return input, err
			}
			input.Preview.Bytes += size
			method := "verified copy once; keep original generation"
			if state.Hosts[profile.Harness].ProfileID == profile.ID {
				if root.Kind == workload.Directory {
					method += "; replace host directory with managed link"
				} else {
					method += "; reconcile standalone file"
				}
			}
			input.Preview.Roots = append(input.Preview.Roots, MigrationRoot{Harness: profile.Harness, Profile: profile.Name, Path: root.Target, Method: method})
		}
	}
	input.Preview.Warnings = []string{"Host writes will change live profiles immediately. Load a new profile before a new login.", "Migration copies each complete profile once so old generations and host roots remain independent recovery data.", "Standalone files use checked copies; session directories use live links. Shared host roots follow the last loaded harness.", "At migration, shared roots use the last harness in name order. Load the desired profile afterward."}
	if err := checkManagedSpace(s.storePath(), input.Preview.Bytes+16<<20); err != nil {
		return input, err
	}
	return input, nil
}

// Copy-mode snapshots already define attribute handling. A verified first
// copy keeps an independent recovery generation across filesystems and avoids
// moving a linked root before the new format can recover it.
func (s *Service) cloneManaged(ctx context.Context, store *os.Root, profile Manifest, fromHost bool) (Manifest, error) {
	credentials, err := s.readCredentials(store, profile)
	if err != nil {
		return Manifest{}, err
	}
	source := snapshotSource(filepath.Join(s.storePath(), dataPath(profile)))
	if fromHost {
		source = hostSource
		credentials = s.credentials(profile.Harness)
	}
	before, err := digestRoots(ctx, profile.Roots, source, credentials)
	if err != nil {
		return Manifest{}, err
	}
	next := profile
	next.Roots = append([]Root(nil), profile.Roots...)
	next.PreviousGeneration = profile.Generation
	next.Generation, err = newID()
	if err != nil {
		return Manifest{}, err
	}
	next.CredentialRevision = ""
	path := dataPath(next)
	if err := privatePath(store, filepath.Join(path, "roots"), true); err != nil {
		return Manifest{}, err
	}
	for position, root := range profile.Roots {
		info, err := os.Lstat(source(root))
		if errors.Is(err, os.ErrNotExist) {
			next.Roots[position].Present = false
			continue
		}
		if err != nil {
			return Manifest{}, err
		}
		next.Roots[position].Present = true
		if root.Kind == workload.Directory && !info.IsDir() {
			return Manifest{}, errors.New("profile root changed its type")
		}
		if err := s.copyManaged(ctx, source(root), filepath.Join(s.storePath(), path, "roots", root.ID)); err != nil {
			return Manifest{}, err
		}
	}
	after, err := digestRoots(ctx, profile.Roots, source, credentials)
	if err != nil {
		return Manifest{}, err
	}
	copied, err := digestRoots(ctx, next.Roots, snapshotSource(filepath.Join(s.storePath(), path)), credentials)
	if err != nil {
		return Manifest{}, err
	}
	if before != after || before != copied {
		return Manifest{}, errors.New("profile changed during migration; original data was retained")
	}
	// Required directory mounts need stable empty roots even when the host has
	// never used that optional auxiliary directory. No account files are seeded.
	for position, root := range next.Roots {
		if root.Kind == workload.Directory && !root.Present {
			if err := store.Mkdir(filepath.Join(path, "roots", root.ID), 0700); err != nil {
				return Manifest{}, err
			}
			next.Roots[position].Present = true
		}
	}
	if err := writeJSON(store, filepath.Join(path, "credentials.json"), credentials); err != nil {
		return Manifest{}, err
	}
	next.Digest = copied
	if err := s.addCanonicalPaths(&next); err != nil {
		return Manifest{}, err
	}
	if err := writeJSON(store, filepath.Join(path, "snapshot.json"), next); err != nil {
		return Manifest{}, err
	}
	return next, nil
}

func checkManagedTree(ctx context.Context, path string) error {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := checkManagedMounts(path); err != nil {
		return err
	}
	parent, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer parent.Close()
	return visitTree(ctx, parent, filepath.Base(path), ".", func(root *os.Root, name, relative string, info fs.FileInfo) error {
		if err := checkManagedAttributes(filepath.Join(path, relative), info); err != nil {
			return err
		}
		if info.Mode().IsRegular() && info.Sys().(*syscall.Stat_t).Nlink > 1 {
			return fmt.Errorf("hard-linked state at %s needs an independent copy before migration", relative)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
		link, err := root.Readlink(name)
		if err != nil {
			return err
		}
		if filepath.IsAbs(link) {
			return fmt.Errorf("absolute child link %s can select another account; resolve it before migration", relative)
		}
		destination, err := workload.ResolvedPath(filepath.Join(filepath.Dir(filepath.Join(path, relative)), link))
		if err != nil {
			return err
		}
		if !containsPath(path, destination) {
			return fmt.Errorf("child link %s leaves its state root; remove the external alias before conversion", relative)
		}
		return nil
	})
}

func (s *Service) Migrate(ctx context.Context, choice ConflictChoice) (Result, error) {
	if runtime.GOOS != "linux" {
		return Result{}, errors.New("live profile storage requires Linux")
	}
	lock, err := statelock.Acquire(ctx, true)
	if err != nil {
		return Result{}, err
	}
	defer lock.Close()
	// Build also prepares an empty store. The first save can then adopt host
	// roots directly into live storage without a separate migration command.
	store, err := s.open(true)
	if err != nil {
		return Result{}, err
	}
	defer store.Close()
	if err := profilelink.Ready(store); err != nil {
		return Result{}, err
	}
	managed, err := profilelink.Managed(store)
	if err != nil {
		return Result{}, err
	}
	if managed {
		if _, _, err := s.readManagedView(store); err != nil {
			return Result{}, err
		}
		return Result{Managed: true, Unchanged: true}, nil
	}
	state, err := readIndex(store)
	if err != nil {
		return Result{}, err
	}
	if err := s.recoverTransaction(ctx, store, &state); err != nil {
		return Result{}, err
	}
	input, err := s.planMigration(ctx, store, choice)
	if err != nil {
		return Result{}, err
	}
	original := input.State
	// Keep a durable copy-mode catalog before creating any live data or
	// views. A stop before the journal exists can then be retried safely.
	if err := writeJSON(store, "index.json", original); err != nil {
		return Result{}, err
	}
	if err := s.managedCheckpoint("migration-index"); err != nil {
		return Result{}, err
	}
	next := index{Schema: Schema, Hosts: map[string]HostSelection{}}
	for harness, host := range original.Hosts {
		next.Hosts[harness] = host
	}
	for _, profile := range original.Profiles {
		copied, err := s.cloneManaged(ctx, store, profile, input.HostSources[profile.ID])
		if err != nil {
			return Result{}, err
		}
		next.Profiles = append(next.Profiles, copied)
	}
	txn, err := newManagedTransaction("migrate", "")
	if err != nil {
		return Result{}, err
	}
	txn.Legacy = &original
	var bindings []profilelink.Binding
	harnesses := make([]string, 0, len(next.Hosts))
	for harness := range next.Hosts {
		harnesses = append(harnesses, harness)
	}
	sort.Strings(harnesses)
	for _, harness := range harnesses {
		profile := next.byID(next.Hosts[harness].ProfileID)
		bindings, err = s.selectManagedBindings(ctx, store, bindings, *profile, &txn)
		if err != nil {
			return Result{}, err
		}
	}
	for _, profile := range next.Profiles {
		if err := s.stageCanonicalPaths(ctx, profile, &txn, false); err != nil {
			return Result{}, err
		}
	}
	recovery, err := s.commitManaged(ctx, store, profilelink.View{}, next, bindings, txn)
	if len(original.Profiles) == 0 {
		return Result{Managed: true}, err
	}
	return Result{Managed: true, Recovery: recovery, Warning: "Managed profiles share live state. Use load with a new name before changing accounts. Migration originals were retained."}, err
}

func (s *Service) adoptManaged(ctx context.Context, store *os.Root, view profilelink.View, state index, request SaveRequest) (Result, error) {
	paths, err := workload.ResolveAgentScope(request.Harness, s.options.Account.Home, s.options.Workspace, s.options.Environment)
	if err != nil {
		return Result{}, err
	}
	var logical, physical []Root
	for _, mount := range paths.Mounts {
		if err := workload.ValidateAgentStatePath(mount.Source, s.options.Account.Home, s.options.CooperDir); err != nil {
			return Result{}, err
		}
		public, _, _, err := profilelink.Locate(mount.Source)
		if err != nil {
			return Result{}, err
		}
		resolved, err := workload.ResolvedPath(mount.Source)
		if err != nil {
			return Result{}, err
		}
		if err := s.checkWorkspaceOutside(resolved); err != nil {
			return Result{}, err
		}
		root := Root{ID: mount.ID, Target: mount.Target, HostPath: public, Kind: mount.Kind}
		logical = append(logical, root)
		root.HostPath = resolved
		physical = append(physical, root)
		if err := checkManagedTree(ctx, resolved); err != nil {
			return Result{}, err
		}
	}
	if err := compatibleManagedRoots(view, logical); err != nil {
		return Result{}, err
	}
	if err := s.options.Guard.Check(ctx, rootPaths(physical)); err != nil {
		return Result{}, err
	}
	identity, err := s.options.Reader.Read(ctx, request.Harness, paths.Mounts, s.options.Environment)
	if err != nil || identity.Key == "" {
		return Result{}, &Issue{Kind: IdentityUnknown, Message: "log in on the host before registering a live profile"}
	}
	profile, created, err := s.saveDestination(&state, request, identity)
	if err != nil {
		return Result{}, err
	}
	if !created {
		return Result{}, errors.New("load the existing managed profile before saving it")
	}
	profile.Generation, err = newID()
	if err != nil {
		return Result{}, err
	}
	// Adoption has the same timestamp contract as initial migration. Keep the
	// legacy capture checks, but use the managed copy for this new live root.
	captureService := *s
	captureService.copy = s.copyManaged
	captured, err := captureService.capture(ctx, store, dataPath(profile), request.Harness, paths, physical)
	if err != nil {
		return Result{}, err
	}
	copiedIdentity, err := s.options.Reader.Read(ctx, request.Harness, snapshotMounts(captured, filepath.Join(s.storePath(), dataPath(profile))), s.options.Environment)
	if err != nil || copiedIdentity.Key != identity.Key {
		return Result{}, errors.New("account changed while registering profile; original state was retained")
	}
	captured.ID, captured.Name, captured.Generation = profile.ID, profile.Name, profile.Generation
	captured.Created, captured.Saved, captured.Identity = profile.Created, s.now().UTC(), identity
	captured.Roots = logical
	for position, root := range logical {
		source := filepath.Join(s.storePath(), dataPath(captured), "roots", root.ID)
		_, err := os.Lstat(source)
		if errors.Is(err, os.ErrNotExist) && root.Kind == workload.Directory {
			err = os.Mkdir(source, 0700)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return Result{}, err
		}
		captured.Roots[position].Present = err == nil
	}
	if err := s.addCanonicalPaths(&captured); err != nil {
		return Result{}, err
	}
	if err := writeJSON(store, filepath.Join(dataPath(captured), "snapshot.json"), captured); err != nil {
		return Result{}, err
	}
	state.put(captured)
	state.Hosts[request.Harness] = HostSelection{ProfileID: captured.ID, BaseDigest: captured.Digest}
	txn, err := newManagedTransaction("switch", view.ID)
	if err != nil {
		return Result{}, err
	}
	bindings, err := s.selectManagedBindings(ctx, store, view.Bindings, captured, &txn)
	if err != nil {
		return Result{}, err
	}
	recovery, err := s.commitManaged(ctx, store, view, state, bindings, txn)
	return Result{Saved: captured.Name, Created: true, Managed: true, Recovery: recovery}, err
}

// Recover is explicit so a failed migration can be recovered even before the
// format switch. It shares the same exclusive lock as save/load and startup.
func (s *Service) Recover(ctx context.Context) error {
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
	var header map[string]any
	err = readJSON(store, transactionFile, &header)
	if errors.Is(err, os.ErrNotExist) {
		// Missing recovery metadata must not hide a damaged descriptor.
		// Never infer an empty store from unregistered managed views.
		_, err := readIndex(store)
		return err
	}
	if err != nil {
		return err
	}
	if header["schema"] == float64(profilelink.Schema) {
		return s.recoverManaged(ctx, store)
	}
	state, err := readIndex(store)
	if err != nil {
		return err
	}
	return s.recoverTransaction(ctx, store, &state)
}

func managedTreeSize(ctx context.Context, path string) (int64, error) {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}
	parent, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return 0, err
	}
	defer parent.Close()
	var size int64
	err = visitTree(ctx, parent, filepath.Base(path), ".", func(_ *os.Root, _ string, _ string, info fs.FileInfo) error {
		if info.Mode().IsRegular() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// Host settings must keep one complete path contract across account changes.
// Different paths cannot be switched by changing only a directory selector.
func validateMigrationRoots(state index) error {
	first := map[string]Manifest{}
	var known []Root
	for _, profile := range state.Profiles {
		if previous, exists := first[profile.Harness]; exists {
			if !reflect.DeepEqual(previous.Environment, profile.Environment) || len(previous.Roots) != len(profile.Roots) {
				return errors.New("profiles use different path settings; align them in copy mode before migration")
			}
			for i, root := range profile.Roots {
				old := previous.Roots[i]
				if old.ID != root.ID || old.Target != root.Target || old.HostPath != root.HostPath || old.Kind != root.Kind {
					return errors.New("profiles use different root paths; align them in copy mode before migration")
				}
			}
		} else {
			first[profile.Harness] = profile
		}
		for _, root := range profile.Roots {
			for _, other := range known {
				if root.HostPath == other.HostPath && root.ID == other.ID && root.Kind == other.Kind {
					continue
				}
				if containsPath(root.HostPath, other.HostPath) || containsPath(other.HostPath, root.HostPath) {
					return errors.New("profile catalogs contain overlapping or different roots at one host path; resolve these paths before conversion")
				}
			}
			known = append(known, root)
		}
	}
	return nil
}

func compatibleManagedRoots(view profilelink.View, roots []Root) error {
	for _, root := range roots {
		for _, binding := range view.Bindings {
			if root.HostPath == binding.Path && root.ID == binding.RootID && string(root.Kind) == binding.Kind {
				continue
			}
			if containsPath(root.HostPath, binding.Path) || containsPath(binding.Path, root.HostPath) {
				return errors.New("new harness roots overlap a different managed root; state was retained")
			}
		}
	}
	return nil
}
