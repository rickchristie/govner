package profiles

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/workload"
)

func (s *Service) Delete(ctx context.Context, harness, name string) error {
	store, lock, state, err := s.locked(ctx, false, true)
	if err != nil {
		return err
	}
	defer store.Close()
	defer lock.Close()
	profile := state.byName(harness, name)
	if profile == nil {
		return errors.New("profile does not exist")
	}
	if state.Hosts[harness].ProfileID == profile.ID {
		return errors.New("load another profile before deleting the selected profile")
	}
	if err := s.checkProfile(*profile); err != nil {
		return err
	}
	checked, err := inspectProfile(state, *profile)
	if err != nil {
		return err
	}
	if err := s.checkUse(ctx, state, checked); err != nil {
		return err
	}
	state.put(checked)
	if err := s.publish(store, state); err != nil {
		return err
	}
	txn, err := newTransaction("delete", state)
	if err != nil {
		return err
	}
	txn.After.Profiles = slices.DeleteFunc(txn.After.Profiles, func(item Manifest) bool { return item.ID == checked.ID })
	txn.planDelete(checked)
	// Publish the exact deletion record first. A failed removal can then be
	// retried with prune-recovery, without keeping a partly deleted profile.
	if err := retainTransaction(store, txn); err != nil {
		return err
	}
	if err := s.publish(store, txn.After); err != nil {
		return err
	}
	if err := s.removeRecovery(ctx, store, txn.After, txn); err != nil {
		return err
	}
	return pruneCredentials(store, txn.After)
}

func retainTransaction(store *os.Root, txn transaction) error {
	if err := privatePath(store, "recovery", true); err != nil {
		return err
	}
	return writeJSON(store, filepath.Join("recovery", txn.ID+".json"), txn)
}

func (s *Service) PruneRecovery(ctx context.Context) error {
	store, lock, state, err := s.locked(ctx, false, true)
	if err != nil {
		return err
	}
	defer store.Close()
	defer lock.Close()
	if err := privatePath(store, "recovery", false); errors.Is(err, os.ErrNotExist) {
		return pruneCredentials(store, state)
	} else if err != nil {
		return err
	}
	entries, err := fs.ReadDir(store.FS(), "recovery")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !storedID.MatchString(id) || entry.Name() != id+".json" {
			return errors.New("recovery contains an unknown entry; data was retained")
		}
		var txn transaction
		if err := readJSON(store, filepath.Join("recovery", entry.Name()), &txn); err != nil {
			return err
		}
		if txn.ID != id {
			return errors.New("recovery record ID does not match its name")
		}
		if err := s.removeRecovery(ctx, store, state, txn); err != nil {
			return err
		}
	}
	return pruneCredentials(store, state)
}

// Credential changes can leave an old revision after a failed index write.
// Only explicit deletion or prune scans this small metadata directory. Keep
// every revision named by a current profile or a retained recovery record.
func pruneCredentials(store *os.Root, state index) error {
	keep := map[string]bool{}
	note := func(catalog index) {
		for _, profile := range catalog.Profiles {
			keep[profile.CredentialRevision+".json"] = true
		}
	}
	note(state)
	records, err := fs.ReadDir(store.FS(), "recovery")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, record := range records {
		var txn transaction
		if err := readJSON(store, filepath.Join("recovery", record.Name()), &txn); err != nil {
			return err
		}
		note(txn.Before)
		note(txn.After)
	}
	if err := privatePath(store, "credentials", false); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	entries, err := fs.ReadDir(store.FS(), "credentials")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if keep[name] {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if !storedID.MatchString(id) || name != id+".json" {
			return errors.New("credential storage contains an unknown entry; data was retained")
		}
		var record []workload.EnvVar
		if err := readJSON(store, filepath.Join("credentials", name), &record); err != nil {
			return err
		}
		if err := store.Remove(filepath.Join("credentials", name)); err != nil {
			return err
		}
	}
	return syncRoot(store, "credentials")
}

func (s *Service) removeRecovery(ctx context.Context, store *os.Root, current index, txn transaction) error {
	if err := s.validateTransaction(txn, txn.Before); err != nil {
		return err
	}
	var entries []rootEntry
	for _, entry := range txn.Entries {
		path := filepath.Join(filepath.Dir(entry.Root.HostPath), entry.Name)
		allowed := path == entry.Root.HostPath+".cooper-restore-"+txn.ID || path == entry.Root.HostPath+".cooper-recovery-"+txn.ID
		if txn.Operation == "delete" {
			for _, profile := range txn.Before.Profiles {
				if txn.After.byID(profile.ID) != nil {
					continue
				}
				if current.byID(profile.ID) != nil {
					return errors.New("profile deletion was not committed; its data was retained")
				}
				for _, root := range profile.Roots {
					if root.HostPath == entry.Root.HostPath && path == sibling(root, profile.ID) {
						allowed = true
					}
				}
			}
		}
		if !allowed {
			continue
		}
		for _, profile := range current.Profiles {
			for _, root := range profile.Roots {
				if containsPath(path, sourcePath(current, profile, root)) || containsPath(sourcePath(current, profile, root), path) {
					return errors.New("recovery overlaps a current profile; data was retained")
				}
			}
		}
		parent, err := rootParent(entry.Root)
		if err != nil {
			return err
		}
		actual, err := entryID(parent, entry.Name)
		parent.Close()
		if err != nil {
			return err
		}
		if actual.Inode == 0 {
			continue
		}
		// A restore recovery path starts empty. Its source inode is recorded
		// in the move that fills it, including after a crash before fsync.
		expected := entry.Entry
		for _, move := range txn.Moves {
			if move.Root.HostPath == entry.Root.HostPath && move.To == entry.Name {
				expected = move.Entry
			}
		}
		if actual != expected {
			return fmt.Errorf("recovery entry %s changed; data was retained", path)
		}
		entry.Entry = actual
		if err := s.checkWorkspaceOutside(path); err != nil {
			return err
		}
		if err := checkRootMounts(path); err != nil {
			return err
		}
		entries = append(entries, entry)
	}
	var paths []string
	for _, entry := range entries {
		paths = append(paths, filepath.Join(filepath.Dir(entry.Root.HostPath), entry.Name))
	}
	if err := s.options.Guard.Check(ctx, paths); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		parent, err := rootParent(entry.Root)
		if err != nil {
			return err
		}
		actual, err := entryID(parent, entry.Name)
		if err == nil && actual != entry.Entry {
			err = errors.New("recovery entry changed before deletion")
		}
		if err == nil {
			err = removeTree(parent, entry.Name)
		}
		if err == nil {
			err = syncRoot(parent, ".")
		}
		parent.Close()
		if err != nil {
			return err
		}
	}
	if err := store.Remove(filepath.Join("recovery", txn.ID+".json")); err != nil {
		return err
	}
	return syncRoot(store, "recovery")
}
