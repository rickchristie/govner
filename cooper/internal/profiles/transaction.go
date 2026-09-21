package profiles

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"

	"github.com/rickchristie/govner/cooper/internal/statelock"
)

const transactionFile = "transaction.json"

type rootEntry struct {
	Root  Root   `json:"root"`
	Name  string `json:"name"`
	Entry FileID `json:"entry"`
}

type rootMove struct {
	Root     Root `json:"root"`
	From, To string
	Entry    FileID `json:"entry"`
}

// The index transaction ID is the commit point. Recovery checks a complete
// prefix of these moves by inode, so a crash between rename and fsync needs
// no history reads and cannot authorize replacement of an unexpected entry.
type transaction struct {
	Schema        int `json:"schema"`
	ID, Operation string
	Before, After index
	Entries       []rootEntry
	Moves         []rootMove
}

func newTransaction(operation string, state index) (transaction, error) {
	id, err := newID()
	if err != nil {
		return transaction{}, err
	}
	next := state
	next.Hosts, next.Profiles = maps.Clone(state.Hosts), slices.Clone(state.Profiles)
	next.LastTransaction = id
	return transaction{Schema: Schema, ID: id, Operation: operation, Before: state, After: next}, nil
}

func (s *Service) validateTransaction(txn transaction, state index) error {
	if txn.Schema != Schema || !storedID.MatchString(txn.ID) || txn.Before.LastTransaction == txn.ID || txn.After.LastTransaction != txn.ID || (txn.Operation != "switch" && txn.Operation != "restore" && txn.Operation != "delete") {
		return errors.New("profile recovery record has an unsupported operation")
	}
	if !reflect.DeepEqual(state, txn.Before) && !reflect.DeepEqual(state, txn.After) {
		return errors.New("profile index changed outside its transaction; recovery was retained")
	}
	for _, catalog := range []index{txn.Before, txn.After} {
		if _, err := validateIndex(catalog); err != nil {
			return err
		}
		for _, profile := range catalog.Profiles {
			if err := s.checkProfile(profile); err != nil {
				return err
			}
		}
	}
	if err := validateChange(txn); err != nil {
		return err
	}
	allowed := map[string]Root{}
	for _, catalog := range []index{txn.Before, txn.After} {
		for _, profile := range catalog.Profiles {
			for _, root := range profile.Roots {
				for _, path := range []string{root.HostPath, sibling(root, profile.ID), root.HostPath + ".cooper-restore-" + txn.ID, root.HostPath + ".cooper-recovery-" + txn.ID} {
					allowed[path] = root
				}
			}
		}
	}
	if len(txn.Entries) > 64 || len(txn.Moves) > 64 {
		return errors.New("profile recovery record exceeds the root limit")
	}
	seen := map[string]bool{}
	for _, entry := range txn.Entries {
		path := filepath.Join(filepath.Dir(entry.Root.HostPath), entry.Name)
		root, ok := allowed[path]
		if !ok || filepath.Base(entry.Name) != entry.Name || entry.Name == "." || entry.Name == ".." || seen[path] || root.Parent != entry.Root.Parent || root.HostPath != entry.Root.HostPath || root.Kind != entry.Root.Kind {
			return errors.New("profile recovery contains an unregistered entry")
		}
		seen[path] = true
	}
	expected := map[string]FileID{}
	for _, entry := range txn.Entries {
		expected[filepath.Join(filepath.Dir(entry.Root.HostPath), entry.Name)] = entry.Entry
	}
	for _, move := range txn.Moves {
		if move.From == move.To || move.Entry.Inode == 0 {
			return errors.New("profile recovery contains an invalid move")
		}
		for _, name := range []string{move.From, move.To} {
			path := filepath.Join(filepath.Dir(move.Root.HostPath), name)
			if filepath.Base(name) != name || !seen[path] || allowed[path].Parent != move.Root.Parent {
				return errors.New("profile recovery move is outside its entries")
			}
		}
		from, to := filepath.Join(filepath.Dir(move.Root.HostPath), move.From), filepath.Join(filepath.Dir(move.Root.HostPath), move.To)
		if expected[from] != move.Entry || expected[to].Inode != 0 {
			return errors.New("profile recovery contains an invalid move order")
		}
		expected[from], expected[to] = FileID{}, move.Entry
	}
	return nil
}

func progress(txn transaction) (int, error) {
	expected, actual := map[string]FileID{}, map[string]FileID{}
	for _, entry := range txn.Entries {
		parent, err := rootParent(entry.Root)
		if err != nil {
			return 0, err
		}
		id, err := entryID(parent, entry.Name)
		parent.Close()
		if err != nil {
			return 0, err
		}
		path := filepath.Join(filepath.Dir(entry.Root.HostPath), entry.Name)
		expected[path], actual[path] = entry.Entry, id
	}
	if maps.Equal(expected, actual) {
		return 0, nil
	}
	for position, move := range txn.Moves {
		from, to := filepath.Join(filepath.Dir(move.Root.HostPath), move.From), filepath.Join(filepath.Dir(move.Root.HostPath), move.To)
		if expected[from] != move.Entry || expected[to].Inode != 0 {
			return 0, errors.New("profile journal has an invalid move order")
		}
		expected[from], expected[to] = FileID{}, move.Entry
		if maps.Equal(expected, actual) {
			return position + 1, nil
		}
	}
	return 0, errors.New("profile entries changed outside the transaction; all recovery data was retained")
}

func (s *Service) move(move rootMove) error {
	parent, err := rootParent(move.Root)
	if err != nil {
		return err
	}
	defer parent.Close()
	from, err := entryID(parent, move.From)
	if err != nil {
		return err
	}
	to, err := entryID(parent, move.To)
	if err != nil {
		return err
	}
	if from != move.Entry || to.Inode != 0 {
		return errors.New("profile move source or destination changed; state was retained")
	}
	return s.rename(parent, move.From, move.To)
}

func (s *Service) commit(ctx context.Context, store *os.Root, txn transaction) error {
	if err := s.validateTransaction(txn, txn.Before); err != nil {
		return err
	}
	if done, err := progress(txn); err != nil || done != 0 {
		return errors.Join(err, errors.New("profile entries changed before the switch"))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := writeJSON(store, transactionFile, txn); err != nil {
		return err
	}
	if err := s.checkpoint("journal"); err != nil {
		return err
	}
	for position, move := range txn.Moves {
		if err := ctx.Err(); err != nil {
			return errors.Join(err, s.recover(ctx, store))
		}
		if err := s.move(move); err != nil {
			return errors.Join(err, s.recover(context.WithoutCancel(ctx), store))
		}
		if err := s.checkpoint("move-" + strconv.Itoa(position)); err != nil {
			return err
		}
		if err := syncDirectory(filepath.Dir(move.Root.HostPath)); err != nil {
			return errors.Join(err, s.recover(context.WithoutCancel(ctx), store))
		}
		if err := s.checkpoint("sync-" + strconv.Itoa(position)); err != nil {
			return err
		}
	}
	if err := s.publish(store, txn.After); err != nil {
		return errors.Join(err, s.recover(context.WithoutCancel(ctx), store))
	}
	if err := s.checkpoint("index"); err != nil {
		return err
	}
	return finishTransaction(store, txn)
}

func finishTransaction(store *os.Root, txn transaction) error {
	if txn.Operation == "restore" {
		if err := retainTransaction(store, txn); err != nil {
			return err
		}
	}
	if err := store.Remove(transactionFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncRoot(store, ".")
}

func (s *Service) Recover(ctx context.Context) error {
	if err := Supported(); err != nil {
		return err
	}
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
	return s.recover(ctx, store)
}

func (s *Service) recover(ctx context.Context, store *os.Root) error {
	state, err := readIndex(store)
	if err != nil {
		return err
	}
	var txn transaction
	if err := readJSON(store, transactionFile, &txn); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := s.validateTransaction(txn, state); err != nil {
		return err
	}
	var paths []string
	for _, entry := range txn.Entries {
		path := filepath.Join(filepath.Dir(entry.Root.HostPath), entry.Name)
		if err := s.checkWorkspaceOutside(path); err != nil {
			return err
		}
		if err := checkRootMounts(path); err != nil {
			return err
		}
		paths = append(paths, path)
	}
	if err := s.options.Guard.Check(context.WithoutCancel(ctx), paths); err != nil {
		return err
	}
	done, err := progress(txn)
	if err != nil {
		return err
	}
	if state.LastTransaction == txn.ID {
		if done != len(txn.Moves) {
			return errors.New("committed profile entries changed; recovery was retained")
		}
		return finishTransaction(store, txn)
	}
	for position := done - 1; position >= 0; position-- {
		move := txn.Moves[position]
		move.From, move.To = move.To, move.From
		if err := s.move(move); err != nil {
			return err
		}
		if err := syncDirectory(filepath.Dir(move.Root.HostPath)); err != nil {
			return err
		}
	}
	// Restore stages can contain the only checked copy after interruption.
	// Retain their journal for explicit cleanup rather than deleting it here.
	if txn.Operation == "restore" {
		if err := retainTransaction(store, txn); err != nil {
			return err
		}
	}
	if err := store.Remove(transactionFile); err != nil {
		return fmt.Errorf("remove completed recovery record: %w", err)
	}
	return syncRoot(store, ".")
}
