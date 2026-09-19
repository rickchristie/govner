package profiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"syscall"

	"github.com/rickchristie/govner/cooper/internal/profilelink"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

// Entry identifies directory entries without scanning live history. File
// roots also carry a digest because they use the explicit small-file fallback.
type managedEntry struct {
	Present bool   `json:"present"`
	Device  uint64 `json:"device,omitempty"`
	Inode   uint64 `json:"inode,omitempty"`
	Mode    uint32 `json:"mode,omitempty"`
	Link    string `json:"link,omitempty"`
	Digest  string `json:"digest,omitempty"`
}

type managedChange struct {
	Path         string       `json:"path"`
	ParentDevice uint64       `json:"parent_device"`
	ParentInode  uint64       `json:"parent_inode"`
	Before       managedEntry `json:"before"`
	After        managedEntry `json:"after"`
}

type managedTransaction struct {
	Schema    int             `json:"schema"`
	ID        string          `json:"id"`
	Operation string          `json:"operation"`
	OldView   string          `json:"old_view,omitempty"`
	NewView   string          `json:"new_view"`
	Changes   []managedChange `json:"changes"`
	Legacy    *index          `json:"legacy,omitempty"`
}

func (t managedTransaction) sibling(position int, suffix string) string {
	return ".cooper-managed-" + t.ID + "-" + strconv.Itoa(position) + "-" + suffix
}

func managedEntryAt(ctx context.Context, parent *os.Root, name string) (managedEntry, error) {
	info, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return managedEntry{}, nil
	}
	if err != nil {
		return managedEntry{}, err
	}
	stat := info.Sys().(*syscall.Stat_t)
	entry := managedEntry{Present: true, Device: uint64(stat.Dev), Inode: stat.Ino, Mode: uint32(info.Mode())}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		entry.Link, err = parent.Readlink(name)
	case info.Mode().IsRegular():
		if info.Size() > 16<<20 {
			return managedEntry{}, errors.New("standalone profile file exceeds 16 MiB; use a supported directory layout before conversion")
		}
		entry.Digest, err = treeDigestAt(ctx, parent, name)
	case info.IsDir():
	default:
		return managedEntry{}, errors.New("unsupported profile root type")
	}
	return entry, err
}

func (s *Service) stageManagedChange(ctx context.Context, txn *managedTransaction, path, source, link string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	parent, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer parent.Close()
	info, err := parent.Stat(".")
	if err != nil {
		return err
	}
	stat := info.Sys().(*syscall.Stat_t)
	before, err := managedEntryAt(ctx, parent, filepath.Base(path))
	if err != nil {
		return err
	}
	stage := txn.sibling(len(txn.Changes), "next")
	backup := txn.sibling(len(txn.Changes), "before")
	for _, name := range []string{stage, backup} {
		if _, err := parent.Lstat(name); !errors.Is(err, os.ErrNotExist) {
			return errors.New("managed staging entry already exists or cannot be checked")
		}
	}
	if link != "" {
		err = parent.Symlink(link, stage)
	} else if source != "" {
		// Source absence represents an absent incoming standalone file.
		if _, check := os.Lstat(source); check == nil {
			err = s.copyManaged(ctx, source, filepath.Join(filepath.Dir(path), stage))
		} else if !errors.Is(check, os.ErrNotExist) {
			err = check
		}
	}
	if err != nil {
		return err
	}
	after, err := managedEntryAt(ctx, parent, stage)
	if err != nil {
		return err
	}
	if err := syncRoot(parent, "."); err != nil {
		return err
	}
	txn.Changes = append(txn.Changes, managedChange{Path: path, ParentDevice: uint64(stat.Dev), ParentInode: stat.Ino, Before: before, After: after})
	return nil
}

func managedParent(change managedChange) (*os.Root, error) {
	parent, err := os.OpenRoot(filepath.Dir(change.Path))
	if err != nil {
		return nil, err
	}
	info, err := parent.Stat(".")
	if err != nil {
		parent.Close()
		return nil, err
	}
	stat := info.Sys().(*syscall.Stat_t)
	if uint64(stat.Dev) != change.ParentDevice || stat.Ino != change.ParentInode {
		parent.Close()
		return nil, errors.New("managed host parent changed; recovery data was retained")
	}
	return parent, nil
}

func readViewID(store *os.Root, id string) (profilelink.View, index, error) {
	view, err := profilelink.ReadControlID(store, id)
	if err != nil {
		return profilelink.View{}, index{}, err
	}
	state, err := viewIndex(view)
	return view, state, err
}

func (s *Service) validateManagedTransaction(store *os.Root, txn managedTransaction) error {
	if txn.Schema != profilelink.Schema || !storedID.MatchString(txn.ID) || !storedID.MatchString(txn.NewView) {
		return errors.New("invalid managed transaction")
	}
	if txn.Operation != "switch" && txn.Operation != "migrate" && txn.Operation != "detach" && txn.Operation != "restore" {
		return errors.New("invalid managed transaction operation")
	}
	allowed := map[string]bool{}
	for _, id := range []string{txn.OldView, txn.NewView} {
		if id == "" {
			continue
		}
		view, state, err := readViewID(store, id)
		if err != nil {
			return err
		}
		if err := s.validateManagedView(store, view, state); err != nil {
			return err
		}
		for _, profile := range state.Profiles {
			for _, root := range profile.Roots {
				// Restore can replace a standalone file in an inactive profile.
				// It has no host binding, but still needs an exact catalog path.
				if root.Kind == workload.File {
					allowed[filepath.Join(s.storePath(), dataPath(profile), "roots", root.ID)] = true
				}
				for _, alias := range root.Aliases {
					parent, err := canonicalParent(profile, root, alias, false)
					if err != nil {
						return err
					}
					parent.Close()
					allowed[alias] = true
				}
			}
		}
		for _, binding := range view.Bindings {
			allowed[binding.Path] = true
			if binding.Kind == "file" {
				allowed[filepath.Join(s.storePath(), binding.Source)] = true
			}
		}
	}
	if txn.Operation == "migrate" && txn.OldView != "" {
		return errors.New("migration already has a selected view")
	}
	if txn.Operation != "migrate" && txn.OldView == "" {
		return errors.New("managed transaction has no old view")
	}
	if (txn.Operation == "migrate" || txn.Operation == "detach") && txn.Legacy == nil {
		return errors.New("format transaction has no legacy catalog")
	}
	if txn.Legacy != nil {
		if _, err := validateIndex(*txn.Legacy); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, change := range txn.Changes {
		if !allowed[change.Path] || seen[change.Path] {
			return errors.New("transaction path is not an exact registered root")
		}
		seen[change.Path] = true
	}
	return nil
}

func (s *Service) applyManaged(ctx context.Context, store *os.Root, txn managedTransaction) error {
	if err := s.validateManagedTransaction(store, txn); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := writeJSON(store, transactionFile, txn); err != nil {
		return err
	}
	if err := s.managedCheckpoint("journal"); err != nil {
		return err
	}
	for position, change := range txn.Changes {
		if err := ctx.Err(); err != nil {
			return errors.Join(err, s.recoverManaged(context.Background(), store))
		}
		if err := s.installManagedChange(ctx, txn, position, change); err != nil {
			return errors.Join(err, s.recoverManaged(context.Background(), store))
		}
		if err := s.managedCheckpoint("root-" + strconv.Itoa(position)); err != nil {
			return err
		}
	}
	temporary := ".current-" + txn.ID
	if err := store.Symlink(filepath.Join("views", txn.NewView), temporary); err != nil {
		return err
	}
	if err := s.rename(store, temporary, "current"); err != nil {
		return errors.Join(err, s.recoverManaged(context.Background(), store))
	}
	if err := s.managedCheckpoint("selector"); err != nil {
		return err
	}
	if err := syncRoot(store, "."); err != nil {
		return err
	}
	if err := s.managedCheckpoint("selector-sync"); err != nil {
		return err
	}
	return s.finishManaged(store, txn)
}

func (s *Service) installManagedChange(ctx context.Context, txn managedTransaction, position int, change managedChange) error {
	parent, err := managedParent(change)
	if err != nil {
		return err
	}
	defer parent.Close()
	name := filepath.Base(change.Path)
	before, err := managedEntryAt(ctx, parent, name)
	if err != nil {
		return err
	}
	after, err := managedEntryAt(ctx, parent, txn.sibling(position, "next"))
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(before, change.Before) || !reflect.DeepEqual(after, change.After) {
		return errors.New("profile root changed before replacement; recovery was retained")
	}
	if before.Present {
		if err := s.rename(parent, name, txn.sibling(position, "before")); err != nil {
			return err
		}
	}
	if err := s.managedCheckpoint("old-root-" + strconv.Itoa(position)); err != nil {
		return err
	}
	if after.Present {
		if err := s.rename(parent, txn.sibling(position, "next"), name); err != nil {
			return err
		}
	}
	if err := s.managedCheckpoint("new-root-" + strconv.Itoa(position)); err != nil {
		return err
	}
	return syncRoot(parent, ".")
}

func (s *Service) recoverManaged(ctx context.Context, store *os.Root) error {
	var txn managedTransaction
	if err := readJSON(store, transactionFile, &txn); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := s.validateManagedTransaction(store, txn); err != nil {
		return err
	}
	var paths []string
	for _, id := range []string{txn.OldView, txn.NewView} {
		if id == "" {
			continue
		}
		_, state, err := readViewID(store, id)
		if err != nil {
			return err
		}
		for _, profile := range state.Profiles {
			paths = append(paths, filepath.Join(s.storePath(), dataPath(profile)))
		}
	}
	for position, change := range txn.Changes {
		paths = append(paths, change.Path, filepath.Join(filepath.Dir(change.Path), txn.sibling(position, "before")), filepath.Join(filepath.Dir(change.Path), txn.sibling(position, "next")))
	}
	if err := s.options.Guard.Check(ctx, paths); err != nil {
		return err
	}
	selected, err := store.Readlink("current")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	committed := selected == filepath.Join("views", txn.NewView)
	if txn.Operation == "detach" && !committed {
		var state index
		if err := readJSON(store, "index.json", &state); err == nil && state.LastTransaction == txn.ID {
			committed = true
		}
	}
	if committed {
		return s.finishManaged(store, txn)
	}
	if (txn.OldView == "" && selected != "") || (txn.OldView != "" && selected != filepath.Join("views", txn.OldView)) {
		return errors.New("profile selector changed outside its transaction; recovery was retained")
	}
	for position := len(txn.Changes) - 1; position >= 0; position-- {
		if err := rollbackManagedChange(ctx, txn, position, txn.Changes[position]); err != nil {
			return err
		}
	}
	if txn.Operation == "migrate" {
		if err := writeJSON(store, "index.json", *txn.Legacy); err != nil {
			return err
		}
	}
	if err := retainManagedRecovery(store, txn); err != nil {
		return err
	}
	return removeJournal(store)
}

func rollbackManagedChange(ctx context.Context, txn managedTransaction, position int, change managedChange) error {
	parent, err := managedParent(change)
	if err != nil {
		return err
	}
	defer parent.Close()
	name, backup, stage := filepath.Base(change.Path), txn.sibling(position, "before"), txn.sibling(position, "next")
	current, err := managedEntryAt(ctx, parent, name)
	if err != nil {
		return err
	}
	old, err := managedEntryAt(ctx, parent, backup)
	if err != nil {
		return err
	}
	if reflect.DeepEqual(current, change.Before) && !old.Present {
		return nil
	}
	if !reflect.DeepEqual(old, change.Before) {
		return errors.New("previous profile entry changed; rollback was stopped")
	}
	if current.Present {
		if !reflect.DeepEqual(current, change.After) {
			return errors.New("installed profile entry changed; rollback was stopped")
		}
		if _, err := parent.Lstat(stage); !errors.Is(err, os.ErrNotExist) {
			return errors.New("rollback staging entry is occupied")
		}
		// Keep the installed entry, including any new data, instead of deleting it.
		if err := parent.Rename(name, stage); err != nil {
			return err
		}
	}
	if old.Present {
		if err := parent.Rename(backup, name); err != nil {
			return err
		}
	}
	return syncRoot(parent, ".")
}

func (s *Service) finishManaged(store *os.Root, txn managedTransaction) error {
	if txn.Operation == "detach" {
		state := *txn.Legacy
		state.LastTransaction = txn.ID
		if err := writeJSON(store, "index.json", state); err != nil {
			return err
		}
		if err := store.Remove("current"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	} else {
		if err := writeJSON(store, "index.json", struct {
			Schema int `json:"schema"`
		}{profilelink.Schema}); err != nil {
			return err
		}
	}
	if len(txn.Changes) > 0 || txn.Operation != "switch" {
		if err := retainManagedRecovery(store, txn); err != nil {
			return err
		}
	}
	if err := s.managedCheckpoint("finish"); err != nil {
		return err
	}
	return removeJournal(store)
}

func newManagedTransaction(operation, oldView string) (managedTransaction, error) {
	id, err := newID()
	return managedTransaction{Schema: profilelink.Schema, ID: id, Operation: operation, OldView: oldView}, err
}

func (s *Service) commitManaged(ctx context.Context, store *os.Root, old profilelink.View, state index, bindings []profilelink.Binding, txn managedTransaction) (string, error) {
	view, err := s.prepareView(store, old.ID, state, bindings)
	if err != nil {
		return "", err
	}
	if err := s.managedCheckpoint("view"); err != nil {
		return "", err
	}
	txn.NewView = view.ID
	if err := s.applyManaged(ctx, store, txn); err != nil {
		return "", err
	}
	if len(txn.Changes) == 0 && txn.Operation == "switch" {
		return "", nil
	}
	return filepath.Join(s.storePath(), "recovery", txn.ID), nil
}

func retainManagedRecovery(store *os.Root, txn managedTransaction) error {
	if err := privatePath(store, filepath.Join("recovery", txn.ID), true); err != nil {
		return err
	}
	return writeJSON(store, filepath.Join("recovery", txn.ID, "managed.json"), txn)
}
