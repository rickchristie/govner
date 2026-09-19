package profiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/rickchristie/govner/cooper/internal/profilelink"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

// exportProfile makes an independent schema-1 snapshot. An active standalone
// file comes from the host; complete directories come from the fixed profile.
// It verifies both sides before publishing any restore point.
func (s *Service) exportProfile(ctx context.Context, source, output *os.Root, view profilelink.View, profile Manifest, outputPath string) (Manifest, error) {
	mounts, err := s.managedMounts(view, profile)
	if err != nil {
		return Manifest{}, err
	}
	physical := append([]Root(nil), profile.Roots...)
	for i, root := range physical {
		physical[i].HostPath = filepath.Join(s.storePath(), dataPath(profile), "roots", root.ID)
		for _, mount := range mounts {
			if mount.ID == root.ID {
				physical[i].HostPath = mount.Source
				break
			}
		}
		if err := checkManagedTree(ctx, physical[i].HostPath); err != nil {
			return Manifest{}, err
		}
	}
	credentials, err := s.readStoredCredentials(source, profile)
	if err != nil {
		return Manifest{}, err
	}
	before, err := digestRoots(ctx, physical, hostSource, credentials)
	if err != nil {
		return Manifest{}, err
	}
	next := profile
	next.Roots = append([]Root(nil), profile.Roots...)
	next.Generation, err = newID()
	if err != nil {
		return Manifest{}, err
	}
	next.PreviousGeneration = ""
	next.CredentialRevision = ""
	path := dataPath(next)
	if err := privatePath(output, filepath.Join(path, "roots"), true); err != nil {
		return Manifest{}, err
	}
	for i, root := range physical {
		_, err := os.Lstat(root.HostPath)
		next.Roots[i].Present = err == nil
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return Manifest{}, err
		}
		if err := s.copyManaged(ctx, root.HostPath, filepath.Join(outputPath, path, "roots", root.ID)); err != nil {
			return Manifest{}, err
		}
	}
	after, err := digestRoots(ctx, physical, hostSource, credentials)
	if err != nil {
		return Manifest{}, err
	}
	copied, err := digestRoots(ctx, next.Roots, snapshotSource(filepath.Join(outputPath, path)), credentials)
	if err != nil {
		return Manifest{}, err
	}
	if before != after || before != copied {
		return Manifest{}, errors.New("profile changed during export; the original state was retained")
	}
	next.Digest = copied
	if err := writeJSON(output, filepath.Join(path, "credentials.json"), credentials); err != nil {
		return Manifest{}, err
	}
	if err := writeJSON(output, filepath.Join(path, "snapshot.json"), next); err != nil {
		return Manifest{}, err
	}
	return next, nil
}

func (s *Service) exportCatalog(ctx context.Context, source, output *os.Root, view profilelink.View, state index, outputPath string) (index, error) {
	next := index{Schema: Schema, Hosts: map[string]HostSelection{}}
	for _, profile := range state.Profiles {
		copied, err := s.exportProfile(ctx, source, output, view, profile, outputPath)
		if err != nil {
			return index{}, err
		}
		next.Profiles = append(next.Profiles, copied)
	}
	for harness, host := range state.Hosts {
		profile := next.byID(host.ProfileID)
		host.BaseDigest = profile.Digest
		host.RecoveryID = ""
		next.Hosts[harness] = host
	}
	return next, nil
}

// Backup writes a portable copy-mode store to a new directory. The backup has
// no live aliases. It can be restored as profiles/ after detach and retaining
// the old store. Existing destinations are never replaced.
func (s *Service) Backup(ctx context.Context, destination string) error {
	store, lock, view, state, err := s.openManaged(ctx, true)
	if err != nil {
		return err
	}
	defer store.Close()
	defer lock.Close()
	if !filepath.IsAbs(destination) || filepath.Clean(destination) != destination {
		return errors.New("backup destination must be a clean absolute path")
	}
	resolved, err := workload.ResolvedPath(destination)
	if err != nil {
		return err
	}
	storePath, err := workload.ResolvedPath(s.storePath())
	if err != nil {
		return err
	}
	if containsPath(resolved, storePath) || containsPath(storePath, resolved) {
		return errors.New("backup must be outside the profile store")
	}
	var paths []string
	for _, profile := range state.Profiles {
		paths = append(paths, filepath.Join(s.storePath(), dataPath(profile)))
		for _, root := range profile.Roots {
			paths = append(paths, root.HostPath)
			if containsPath(root.HostPath, resolved) || containsPath(resolved, root.HostPath) {
				return errors.New("backup cannot overlap host state")
			}
		}
	}
	if err := s.options.Guard.Check(ctx, paths); err != nil {
		return err
	}
	parent, err := os.OpenRoot(filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer parent.Close()
	name := filepath.Base(destination)
	if _, err := parent.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		return errors.New("backup destination already exists or cannot be checked")
	}
	id, err := newID()
	if err != nil {
		return err
	}
	stage := ".cooper-backup-" + id
	if err := parent.Mkdir(stage, 0700); err != nil {
		return err
	}
	output, err := parent.OpenRoot(stage)
	if err != nil {
		return err
	}
	defer output.Close()
	next, err := s.exportCatalog(ctx, store, output, view, state, filepath.Join(filepath.Dir(destination), stage))
	if err != nil {
		return err
	}
	if err := writeJSON(output, "index.json", next); err != nil {
		return err
	}
	if err := syncRoot(output, "."); err != nil {
		return err
	}
	if err := publishBackup(parent, stage, name); err != nil {
		return err
	}
	return syncRoot(parent, ".")
}

// Detach restores ordinary host directories and changes the store back to
// copy mode. Copying is deliberate: both the latest live data and the restored
// host data remain available if publication is interrupted.
func (s *Service) Detach(ctx context.Context) (Result, error) {
	store, lock, view, state, err := s.openManaged(ctx, true)
	if err != nil {
		return Result{}, err
	}
	defer store.Close()
	defer lock.Close()
	if err := s.managedUse(ctx, view, state.Profiles...); err != nil {
		return Result{}, err
	}
	next, err := s.exportCatalog(ctx, store, store, view, state, s.storePath())
	if err != nil {
		return Result{}, err
	}
	txn, err := newManagedTransaction("detach", view.ID)
	if err != nil {
		return Result{}, err
	}
	txn.Legacy = &next
	for _, binding := range view.Bindings {
		if binding.Kind != "directory" {
			continue
		}
		profile := next.byID(binding.ProfileID)
		source := filepath.Join(s.storePath(), dataPath(*profile), "roots", binding.RootID)
		if err := s.stageManagedChange(ctx, &txn, binding.Path, source, ""); err != nil {
			return Result{}, err
		}
	}
	for _, profile := range state.Profiles {
		if err := s.stageCanonicalPaths(ctx, profile, &txn, true); err != nil {
			return Result{}, err
		}
	}
	// The commit view keeps the old fixed sources for recovery validation. The
	// schema-1 catalog becomes authoritative only after the detach commit.
	recovery, err := s.commitManaged(ctx, store, view, state, view.Bindings, txn)
	return Result{Recovery: recovery, Warning: "Detached host roots. Profiles now use independent copies. Keep the old store: native session paths still use its compatibility links. Replaced roots and migration recovery were retained."}, err
}
