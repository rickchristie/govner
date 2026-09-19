package profiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rickchristie/govner/cooper/internal/profilelink"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

// Restore replaces one live profile from an independent backup. It keeps the
// replaced roots in recovery, including an unexpected changed login.
// Profile IDs must match so a name cannot overwrite another account mapping.
func (s *Service) Restore(ctx context.Context, harness, name, backup string) (Result, error) {
	if err := ValidateName(name); err != nil {
		return Result{}, err
	}
	store, lock, view, state, err := s.openManaged(ctx, true)
	if err != nil {
		return Result{}, err
	}
	defer store.Close()
	defer lock.Close()
	profile := state.byName(harness, name)
	if profile == nil {
		return Result{}, errors.New("load or retain this managed profile before restoring its backup")
	}
	if err := s.managedUse(ctx, view, *profile); err != nil {
		return Result{}, err
	}
	if !filepath.IsAbs(backup) || filepath.Clean(backup) != backup {
		return Result{}, errors.New("backup path must be clean and absolute")
	}
	if err := s.options.Guard.Check(ctx, []string{backup}); err != nil {
		return Result{}, err
	}
	parent, err := os.OpenRoot(filepath.Dir(backup))
	if err != nil {
		return Result{}, err
	}
	defer parent.Close()
	if err := privatePath(parent, filepath.Base(backup), false); err != nil {
		return Result{}, err
	}
	source, err := parent.OpenRoot(filepath.Base(backup))
	if err != nil {
		return Result{}, err
	}
	defer source.Close()
	managed, err := profilelink.Managed(source)
	if err != nil {
		return Result{}, err
	}
	if managed {
		return Result{}, errors.New("restore requires an independent copy-mode backup")
	}
	saved, err := readIndex(source)
	if err != nil {
		return Result{}, err
	}
	incoming := saved.byName(harness, name)
	if incoming == nil || incoming.ID != profile.ID || incoming.Identity.Key != profile.Identity.Key {
		return Result{}, errors.New("backup does not match this profile's account mapping")
	}
	if incoming.CredentialRevision != "" {
		return Result{}, errors.New("restore requires an independent copy-mode backup")
	}
	if err := s.checkManagedProfile(*incoming); err != nil {
		return Result{}, err
	}
	if err := privatePath(source, filepath.Join(dataPath(*incoming), "roots"), false); err != nil {
		return Result{}, err
	}
	credentials, err := s.readCredentials(source, *incoming)
	if err != nil {
		return Result{}, err
	}
	if fullySelected(view, *profile) {
		if err := compatibleCredentials(s.credentials(harness), credentials); err != nil {
			return Result{}, err
		}
	}
	copied, err := s.restoreCopy(ctx, store, backup, *incoming, credentials)
	if err != nil {
		return Result{}, err
	}
	// Keep exposed data paths fixed. Otherwise every restore adds a canonical
	// alias and another VM mount for each root. Stage replacement entries at
	// the existing paths; the journal retains the replaced directories.
	staged := copied
	copied.Generation = profile.Generation
	copied.PreviousGeneration = profile.PreviousGeneration
	if err := recordCredentialValues(store, &copied, credentials); err != nil {
		return Result{}, err
	}
	preserveCanonicalPaths(&copied, *profile)
	if err := s.addCanonicalPaths(&copied); err != nil {
		return Result{}, err
	}
	txn, err := newManagedTransaction("restore", view.ID)
	if err != nil {
		return Result{}, err
	}
	for _, root := range copied.Roots {
		source := filepath.Join(s.storePath(), dataPath(staged), "roots", root.ID)
		target := filepath.Join(s.storePath(), dataPath(copied), "roots", root.ID)
		if root.Kind == workload.Directory {
			target, err = filepath.EvalSymlinks(target)
			if err != nil {
				return Result{}, err
			}
		}
		if err := s.stageManagedChange(ctx, &txn, target, source, ""); err != nil {
			return Result{}, err
		}
	}
	bindings := append([]profilelink.Binding(nil), view.Bindings...)
	for position, binding := range bindings {
		if binding.ProfileID != profile.ID {
			continue
		}
		bindings[position].Source = filepath.Join(dataPath(copied), "roots", binding.RootID)
		for _, root := range copied.Roots {
			if root.ID == binding.RootID {
				bindings[position].Aliases = root.Aliases
			}
		}
		if binding.Kind == "file" {
			path := filepath.Join(s.storePath(), dataPath(staged), "roots", binding.RootID)
			bindings[position].Base, err = managedFileDigest(ctx, path)
			if err != nil {
				return Result{}, err
			}
			if err := s.stageManagedChange(ctx, &txn, binding.Path, path, ""); err != nil {
				return Result{}, err
			}
		}
	}
	if err := s.stageCanonicalPaths(ctx, copied, &txn, false); err != nil {
		return Result{}, err
	}
	state.put(copied)
	recovery, err := s.commitManaged(ctx, store, view, state, bindings, txn)
	return Result{Managed: true, Recovery: recovery, Warning: fmt.Sprintf("Restored %s/%s from backup. Replaced roots were retained for recovery.", harness, name)}, err
}

func (s *Service) restoreCopy(ctx context.Context, store *os.Root, backup string, profile Manifest, credentials []workload.EnvVar) (Manifest, error) {
	source := snapshotSource(filepath.Join(backup, dataPath(profile)))
	before, err := digestRoots(ctx, profile.Roots, source, credentials)
	if err != nil {
		return Manifest{}, err
	}
	if before != profile.Digest {
		return Manifest{}, errors.New("backup content changed after publication")
	}
	next := profile
	next.Generation, err = newID()
	if err != nil {
		return Manifest{}, err
	}
	path := dataPath(next)
	if err := privatePath(store, filepath.Join(path, "roots"), true); err != nil {
		return Manifest{}, err
	}
	for _, root := range profile.Roots {
		if err := checkManagedTree(ctx, source(root)); err != nil {
			return Manifest{}, err
		}
		if _, err := os.Lstat(source(root)); errors.Is(err, os.ErrNotExist) && root.Kind == workload.File {
			continue
		} else if err != nil {
			return Manifest{}, err
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
		return Manifest{}, errors.New("backup changed during restore")
	}
	env := map[string]string{}
	for name, value := range profile.PathEnvironment {
		env[name] = value
	}
	for _, value := range credentials {
		if !value.Unset {
			env[value.Name] = value.Value
		}
	}
	identity, err := s.options.Reader.Read(ctx, profile.Harness, snapshotMounts(next, filepath.Join(s.storePath(), path)), env)
	if profile.Identity.Key != "" && (err != nil || identity.Key != profile.Identity.Key) {
		return Manifest{}, errors.New("backup login does not match its account")
	}
	if err := writeJSON(store, filepath.Join(path, "credentials.json"), credentials); err != nil {
		return Manifest{}, err
	}
	if err := writeJSON(store, filepath.Join(path, "snapshot.json"), next); err != nil {
		return Manifest{}, err
	}
	return next, nil
}
