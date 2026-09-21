package profiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rickchristie/govner/cooper/internal/workload"
)

// Restore copies to sibling stages, then uses the same checked rename journal
// as a switch. Old state remains in recovery siblings until explicit cleanup.
func (s *Service) Restore(ctx context.Context, request RestoreRequest) (Result, error) {
	store, lock, state, err := s.locked(ctx, false, true)
	if err != nil {
		return Result{}, err
	}
	defer store.Close()
	defer lock.Close()
	profile := state.byName(request.Harness, request.Name)
	if profile == nil {
		return Result{}, errors.New("restore requires an existing profile")
	}
	if err := s.checkProfile(*profile); err != nil {
		return Result{}, err
	}
	current, err := inspectProfile(state, *profile)
	if err != nil {
		return Result{}, err
	}
	if err := s.checkUse(ctx, state, current); err != nil {
		return Result{}, err
	}
	if !request.Confirmed || (request.ExpectedProfileID != "" && request.ExpectedProfileID != current.ID) {
		return Result{}, &Issue{Kind: ConfirmationRequired, ProfileID: current.ID, Message: fmt.Sprintf("Close all %s CLI instances and apps, and stop affected Cooper runtimes. Keep them closed until restore completes. Replace %s from its backup?", current.Harness, current.Name)}
	}
	backup, entry, err := s.openBackup(request.Backup, current)
	if err != nil {
		return Result{}, err
	}
	defer backup.Close()
	credentials, err := s.readCredentials(backup, entry.Manifest)
	if err != nil {
		return Result{}, err
	}
	if state.Hosts[current.Harness].ProfileID == current.ID {
		if err := compatibleCredentials(s.credentials(current.Harness), credentials); err != nil {
			return Result{}, err
		}
	}
	state.put(current)
	if err := s.publish(store, state); err != nil {
		return Result{}, err
	}
	txn, err := newTransaction("restore", state)
	if err != nil {
		return Result{}, err
	}
	restored := current
	restored.Roots = append([]Root(nil), current.Roots...)
	for position, root := range current.Roots {
		saved := entry.Manifest.Roots[position]
		stage := root.HostPath + ".cooper-restore-" + txn.ID
		if saved.Present {
			source := filepath.Join(request.Backup, backupData(entry.Manifest, saved))
			if err := privatePath(backup, filepath.Join("data", current.ID), false); err != nil {
				return Result{}, err
			}
			info, err := backup.Lstat(backupData(entry.Manifest, saved))
			if err != nil {
				return Result{}, err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return Result{}, errors.New("backup root must not be a link")
			}
			digest, err := s.checkedCopy(ctx, source, stage)
			if err != nil {
				return Result{}, fmt.Errorf("restore stage retained at %s: %w", stage, err)
			}
			if digest != entry.Digests[root.ID] {
				return Result{}, fmt.Errorf("backup content does not match its record; restore stage retained at %s", stage)
			}
		}
		next := root
		next.Present, next.Entry = false, FileID{}
		next, err = inspectRoot(next, stage)
		if err != nil {
			return Result{}, err
		}
		restored.Roots[position] = next
		txn.planRestoreRoot(current, root, next)
	}
	if entry.Manifest.Identity.Key != "" {
		var mounts []workload.MountSpec
		for _, root := range restored.Roots {
			mounts = append(mounts, workload.MountSpec{ID: root.ID, Source: root.HostPath + ".cooper-restore-" + txn.ID, Target: root.Target, Kind: root.Kind})
		}
		identity, err := s.options.Reader.Read(ctx, current.Harness, mounts, identityEnvironment(entry.Manifest, credentials))
		if err != nil || identity.Key != entry.Manifest.Identity.Key {
			return Result{}, errors.New("backup login does not match its saved account; restore stages were retained")
		}
	}
	restored.Identity = entry.Manifest.Identity
	restored.Saved = s.now().UTC()
	if err := recordCredentials(store, &restored, credentials); err != nil {
		return Result{}, err
	}
	txn.After.put(restored)
	if state.Hosts[current.Harness].ProfileID == current.ID {
		txn.After.Hosts[current.Harness] = HostSelection{ProfileID: current.ID, Pending: restored.Identity.Key == ""}
	}
	if err := s.commit(ctx, store, txn); err != nil {
		return Result{}, err
	}
	return Result{Saved: current.Name, Recovery: filepath.Join(s.storePath(), "recovery", txn.ID+".json")}, nil
}

func (s *Service) openBackup(path string, current Manifest) (*os.Root, backupProfile, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, backupProfile{}, errors.New("backup requires an absolute directory")
	}
	parent, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, backupProfile{}, err
	}
	defer parent.Close()
	if err := privatePath(parent, filepath.Base(path), false); err != nil {
		return nil, backupProfile{}, err
	}
	root, err := parent.OpenRoot(filepath.Base(path))
	if err != nil {
		return nil, backupProfile{}, err
	}
	var backup backupIndex
	if err := readJSON(root, "backup.json", &backup); err != nil {
		root.Close()
		return nil, backupProfile{}, err
	}
	if backup.Schema != Schema {
		root.Close()
		return nil, backupProfile{}, errors.New("unsupported backup schema")
	}
	for _, entry := range backup.Profiles {
		if entry.Manifest.ID != current.ID {
			continue
		}
		if err := s.checkProfile(entry.Manifest); err != nil {
			root.Close()
			return nil, backupProfile{}, err
		}
		if entry.Manifest.Harness != current.Harness || entry.Manifest.Identity.Key != current.Identity.Key {
			root.Close()
			return nil, backupProfile{}, errors.New("backup belongs to another account")
		}
		for position, saved := range entry.Manifest.Roots {
			now := current.Roots[position]
			if saved.ID != now.ID || saved.HostPath != now.HostPath || saved.Kind != now.Kind || (saved.Present && !contentDigest.MatchString(entry.Digests[saved.ID])) {
				root.Close()
				return nil, backupProfile{}, errors.New("backup root catalog does not match this profile")
			}
		}
		return root, entry, nil
	}
	root.Close()
	return nil, backupProfile{}, errors.New("backup does not contain this profile")
}
