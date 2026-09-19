package profiles

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"

	"github.com/rickchristie/govner/cooper/internal/profilelink"
	"github.com/rickchristie/govner/cooper/internal/statelock"
)

func managedRecoveryRecords(store *os.Root) ([]managedTransaction, error) {
	if err := privatePath(store, "recovery", false); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(store.FS(), "recovery")
	if err != nil {
		return nil, err
	}
	var records []managedTransaction
	for _, entry := range entries {
		if !storedID.MatchString(entry.Name()) {
			return nil, errors.New("unrecognized recovery entry; retained without change")
		}
		path := filepath.Join("recovery", entry.Name())
		if err := privatePath(store, path, false); err != nil {
			return nil, err
		}
		var txn managedTransaction
		if err := readJSON(store, filepath.Join(path, "managed.json"), &txn); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		if txn.ID != entry.Name() {
			return nil, errors.New("recovery ID mismatch")
		}
		records = append(records, txn)
	}
	return records, nil
}

// PruneRecovery is separate from load/save because it can scan and delete
// complete trees. It never treats a retained view as a content backup.
func (s *Service) PruneRecovery(ctx context.Context) error {
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
	if err := profilelink.Ready(store); err != nil {
		return err
	}
	state, err := readIndex(store)
	if err != nil {
		return err
	}
	managed, err := profilelink.Managed(store)
	if err != nil {
		return err
	}
	if managed {
		view, err := profilelink.Read(store)
		if err != nil {
			return err
		}
		if err := s.validateManagedView(store, view, state); err != nil {
			return err
		}
		if err := profilelink.CheckAliases(s.options.CooperDir, view); err != nil {
			return err
		}
		if err := s.checkCanonicalPaths(state); err != nil {
			return err
		}
	}
	if err := s.checkManagedWorkspace(state); err != nil {
		return err
	}
	records, err := managedRecoveryRecords(store)
	if err != nil {
		return err
	}
	paths := []string{s.storePath()}
	for _, profile := range state.Profiles {
		paths = append(paths, rootPaths(profile.Roots)...)
	}
	for _, txn := range records {
		if err := s.validateManagedTransaction(store, txn); err != nil {
			return err
		}
		for position, change := range txn.Changes {
			for _, suffix := range []string{"before", "next"} {
				paths = append(paths, filepath.Join(filepath.Dir(change.Path), txn.sibling(position, suffix)))
			}
		}
	}
	if err := s.options.Guard.Check(ctx, paths); err != nil {
		return err
	}
	// Validate every existing sibling before removing the first. A changed
	// original must be inspected, rather than erased as an old recovery copy.
	for _, txn := range records {
		for position, change := range txn.Changes {
			if err := checkRecoverySiblings(ctx, txn, position, change); err != nil {
				return err
			}
		}
	}
	for _, txn := range records {
		for position, change := range txn.Changes {
			parent, err := managedParent(change)
			if err != nil {
				return err
			}
			for _, suffix := range []string{"before", "next"} {
				name := txn.sibling(position, suffix)
				if _, err := parent.Lstat(name); errors.Is(err, os.ErrNotExist) {
					continue
				} else if err != nil {
					parent.Close()
					return err
				}
				if err := removeTree(parent, name); err != nil {
					parent.Close()
					return err
				}
			}
			err = syncRoot(parent, ".")
			parent.Close()
			if err != nil {
				return err
			}
		}
		if err := removeTree(store, filepath.Join("recovery", txn.ID)); err != nil {
			return err
		}
	}
	return s.pruneManagedMetadata(ctx, store, state)
}

func checkRecoverySiblings(ctx context.Context, txn managedTransaction, position int, change managedChange) error {
	parent, err := managedParent(change)
	if err != nil {
		return err
	}
	defer parent.Close()
	for _, value := range []struct {
		suffix   string
		expected managedEntry
	}{{"before", change.Before}, {"next", change.After}} {
		entry, err := managedEntryAt(ctx, parent, txn.sibling(position, value.suffix))
		if err != nil {
			return err
		}
		if entry.Present && !reflect.DeepEqual(entry, value.expected) {
			return errors.New("recovery state changed; inspect and back it up before removal")
		}
	}
	return nil
}

func (s *Service) pruneManagedMetadata(ctx context.Context, store *os.Root, state index) error {
	keep := map[string]bool{}
	credentials := map[string]bool{}
	managed, err := profilelink.Managed(store)
	if err != nil {
		return err
	}
	for _, profile := range state.Profiles {
		keep[dataPath(profile)] = true
		for _, root := range profile.Roots {
			for _, alias := range root.Aliases {
				base, err := filepath.EvalSymlinks(s.storePath())
				if err != nil {
					return err
				}
				if containsPath(base, alias) {
					relative, _ := filepath.Rel(base, filepath.Dir(filepath.Dir(alias)))
					keep[relative] = true
				}
			}
		}
		if !managed && profile.PreviousGeneration != "" {
			keep[filepath.Join("harnesses", profile.Harness, profile.ID, profile.PreviousGeneration)] = true
		}
		if profile.CredentialRevision != "" {
			credentials[profile.CredentialRevision+".json"] = true
		}
	}
	// Only remove generations whose own manifest confirms the complete path.
	// Unrecognized entries are retained and reported, never guessed to be data.
	harnesses, err := fs.ReadDir(store.FS(), "harnesses")
	if err != nil {
		return err
	}
	for _, harness := range harnesses {
		base := filepath.Join("harnesses", harness.Name())
		if err := privatePath(store, base, false); err != nil {
			return err
		}
		profiles, err := fs.ReadDir(store.FS(), base)
		if err != nil {
			return err
		}
		for _, profile := range profiles {
			parent := filepath.Join(base, profile.Name())
			if !storedID.MatchString(profile.Name()) {
				return errors.New("unrecognized profile directory")
			}
			if err := privatePath(store, parent, false); err != nil {
				return err
			}
			generations, err := fs.ReadDir(store.FS(), parent)
			if err != nil {
				return err
			}
			for _, generation := range generations {
				path := filepath.Join(parent, generation.Name())
				if keep[path] {
					continue
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				if !storedID.MatchString(generation.Name()) {
					return errors.New("unrecognized generation directory")
				}
				if err := privatePath(store, path, false); err != nil {
					return err
				}
				var snapshot Manifest
				if err := readJSON(store, filepath.Join(path, "snapshot.json"), &snapshot); err != nil {
					return err
				}
				if err := validateManifest(snapshot); err != nil {
					return err
				}
				if dataPath(snapshot) != path {
					return errors.New("generation path mismatch")
				}
				if err := removeTree(store, path); err != nil {
					return err
				}
			}
		}
	}
	current, _ := store.Readlink("current")
	for _, directory := range []string{"views", "credentials"} {
		entries, err := fs.ReadDir(store.FS(), directory)
		if errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := privatePath(store, directory, false); err != nil {
			return err
		}
		for _, entry := range entries {
			path := filepath.Join(directory, entry.Name())
			if path == current || directory == "credentials" && credentials[entry.Name()] {
				continue
			}
			if directory == "views" {
				if !storedID.MatchString(entry.Name()) {
					return errors.New("invalid retained view")
				}
			} else if len(entry.Name()) != 29 || !storedID.MatchString(entry.Name()[:24]) || filepath.Ext(entry.Name()) != ".json" {
				return errors.New("invalid credential revision")
			}
			if err := removeTree(store, path); err != nil {
				return err
			}
		}
	}
	return syncRoot(store, ".")
}
