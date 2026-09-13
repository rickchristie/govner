package profiles

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

const transactionFile = "transaction.json"

type replacement struct {
	Root         Root   `json:"root"`
	ParentDevice uint64 `json:"parent_device"`
	ParentInode  uint64 `json:"parent_inode"`
	Before       string `json:"before"`
	After        string `json:"after"`
}

// One journal covers all roots. The index transaction ID is the commit point;
// a journal without that ID always rolls back, including after process exit.
type transaction struct {
	Schema      int `json:"schema"`
	ID, Harness string
	Roots       []replacement `json:"roots"`
}

func (t transaction) sibling(root Root, suffix string) string {
	return ".cooper-profile-" + t.ID + "-" + root.ID + "-" + suffix
}

func (s *Service) replaceHost(ctx context.Context, store *os.Root, state *index, profile Manifest, roots []Root, incoming string) (resultPath string, resultErr error) {
	id, err := newID()
	if err != nil {
		return "", err
	}
	txn := transaction{Schema: Schema, ID: id, Harness: profile.Harness}
	parents := make([]*os.Root, 0, len(roots))
	var stages []string
	journaled := false
	defer func() {
		// Before publication, only these new copies belong to this operation.
		// After publication, recovery must own them, including after a crash.
		if !journaled {
			for position, stage := range stages {
				resultErr = errors.Join(resultErr, removeTree(parents[position], stage))
			}
		}
		for _, parent := range parents {
			parent.Close()
		}
	}()
	for _, root := range roots {
		// A missing XDG parent can be created without replacing existing data.
		parentPath := filepath.Dir(root.HostPath)
		if err := os.MkdirAll(parentPath, 0o700); err != nil {
			return "", err
		}
		parent, err := os.OpenRoot(parentPath)
		if err != nil {
			return "", err
		}
		parents = append(parents, parent)
		stage := txn.sibling(root, "next")
		if _, err := parent.Lstat(stage); !errors.Is(err, os.ErrNotExist) {
			return "", errors.New("profile staging path already exists or cannot be checked")
		}
		stages = append(stages, stage)
		info, err := parent.Stat(".")
		if err != nil {
			return "", err
		}
		stat := info.Sys().(*syscall.Stat_t)
		before, err := optionalDigest(ctx, parent, filepath.Base(root.HostPath))
		if err != nil {
			return "", err
		}
		record := replacement{Root: root, ParentDevice: uint64(stat.Dev), ParentInode: stat.Ino, Before: before}
		source := snapshotSource(filepath.Join(s.storePath(), dataPath(profile)))(root)
		_, err = os.Lstat(source)
		if err == nil {
			if err := s.copy(ctx, source, filepath.Join(parentPath, stage)); err != nil {
				return "", err
			}
			record.After, err = optionalDigest(ctx, parent, stage)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		txn.Roots = append(txn.Roots, record)
	}
	// Check the complete incoming copy before the journal can authorize any
	// replacement. Runtime use checks and digests cover a concurrent writer.
	stagedSource := func(root Root) string { return filepath.Join(filepath.Dir(root.HostPath), txn.sibling(root, "next")) }
	credentials, err := s.readCredentials(store, profile)
	if err != nil {
		return "", err
	}
	stagedDigest, err := digestRoots(ctx, roots, stagedSource, credentials)
	if err != nil || stagedDigest != incoming {
		return "", errors.Join(err, errors.New("saved profile changed during load; host roots are unchanged"))
	}
	if err := s.validateTransaction(txn); err != nil {
		return "", err
	}
	if err := writeJSON(store, transactionFile, txn); err != nil {
		// A sync error can occur after the journal rename has succeeded.
		_, statErr := store.Lstat(transactionFile)
		journaled = !errors.Is(statErr, os.ErrNotExist)
		return "", err
	}
	journaled = true
	for position, record := range txn.Roots {
		parent := parents[position]
		if err := s.installRoot(ctx, txn, record, parent); err != nil {
			rollbackErr := s.rollback(context.WithoutCancel(ctx), txn, parents)
			if rollbackErr == nil {
				rollbackErr = removeJournal(store)
			}
			return "", errors.Join(err, rollbackErr)
		}
	}
	for position, record := range txn.Roots {
		if err := checkInstalled(ctx, parents[position], record); err != nil {
			rollbackErr := s.rollback(context.WithoutCancel(ctx), txn, parents)
			if rollbackErr == nil {
				rollbackErr = removeJournal(store)
			}
			return "", errors.Join(err, rollbackErr)
		}
	}
	state.Hosts[profile.Harness] = HostSelection{ProfileID: profile.ID, BaseDigest: incoming, Pending: profile.Identity.Key == "", RecoveryID: txn.ID}
	state.LastTransaction = txn.ID
	if err := s.publish(store, *state); err != nil {
		// Rename may have committed before a directory-sync error. Re-read the
		// authority instead of rolling back a possibly committed selection.
		onDisk, readErr := readIndex(store)
		if readErr != nil {
			return "", errors.Join(err, readErr)
		}
		recoveryErr := s.recoverTransaction(context.WithoutCancel(ctx), store, &onDisk)
		if onDisk.LastTransaction == txn.ID {
			return "", errors.Join(fmt.Errorf("profile loaded; metadata sync reported an error: %w", err), recoveryErr)
		}
		return "", errors.Join(err, recoveryErr)
	}
	path, err := s.finishTransaction(store, txn)
	if err != nil {
		return path, fmt.Errorf("profile loaded; recovery bookkeeping needs a retry; keep the journal and sibling copies: %w", err)
	}
	return path, nil
}

func checkInstalled(ctx context.Context, parent *os.Root, record replacement) error {
	if err := checkParent(parent, record); err != nil {
		return err
	}
	digest, err := optionalDigest(ctx, parent, filepath.Base(record.Root.HostPath))
	if err != nil {
		return err
	}
	if digest != record.After {
		return errors.New("installed state changed before commit; recovery data was retained")
	}
	return nil
}

func removeJournal(store *os.Root) error {
	if err := store.Remove(transactionFile); err != nil {
		return err
	}
	return syncRoot(store, ".")
}

func (s *Service) installRoot(ctx context.Context, txn transaction, record replacement, parent *os.Root) error {
	if err := checkParent(parent, record); err != nil {
		return err
	}
	name := filepath.Base(record.Root.HostPath)
	current, err := optionalDigest(ctx, parent, name)
	if err != nil {
		return err
	}
	if current != record.Before {
		return errors.New("host state changed during load; rolling back")
	}
	if record.Before != "" {
		if err := s.rename(parent, name, txn.sibling(record.Root, "before")); err != nil {
			return err
		}
	}
	if record.After != "" {
		if err := s.rename(parent, txn.sibling(record.Root, "next"), name); err != nil {
			return err
		}
	}
	return syncDirectory(parent.Name())
}

func checkParent(parent *os.Root, record replacement) error {
	info, err := parent.Stat(".")
	if err != nil {
		return err
	}
	stat := info.Sys().(*syscall.Stat_t)
	current, err := os.Stat(filepath.Dir(record.Root.HostPath))
	if err != nil {
		return err
	}
	if uint64(stat.Dev) != record.ParentDevice || stat.Ino != record.ParentInode || !os.SameFile(info, current) {
		return errors.New("state parent directory changed; recovery data was retained")
	}
	return nil
}

func optionalDigest(ctx context.Context, parent *os.Root, name string) (string, error) {
	info, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return "", errors.New("replacement path must be a regular file or directory, without a root symlink")
	}
	return treeDigestAt(ctx, parent, name)
}

func (s *Service) validateTransaction(txn transaction) error {
	if txn.Schema != Schema || !storedID.MatchString(txn.ID) {
		return errors.New("invalid profile transaction")
	}
	_, expected, err := s.hostScope(txn.Harness)
	if err != nil {
		return err
	}
	if len(expected) != len(txn.Roots) {
		return errors.New("profile transaction paths differ from the current host settings")
	}
	for position, record := range txn.Roots {
		root := expected[position]
		if root.ID != record.Root.ID || root.HostPath != record.Root.HostPath || root.Target != record.Root.Target || root.Kind != record.Root.Kind {
			return errors.New("profile transaction paths differ from the current host settings; restore those settings before recovery")
		}
		if (record.Before != "" && !contentDigest.MatchString(record.Before)) || (record.After != "" && !contentDigest.MatchString(record.After)) {
			return errors.New("profile transaction has an invalid content digest")
		}
	}
	return nil
}

func (s *Service) recoverTransaction(ctx context.Context, store *os.Root, state *index) error {
	var txn transaction
	err := readJSON(store, transactionFile, &txn)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.validateTransaction(txn); err != nil {
		return err
	}
	if state.LastTransaction == txn.ID {
		_, err := s.finishTransaction(store, txn)
		return err
	}
	paths := make([]string, 0, len(txn.Roots))
	for _, record := range txn.Roots {
		paths = append(paths, record.Root.HostPath)
	}
	if err := s.options.Guard.Check(ctx, paths); err != nil {
		return err
	}
	parents, err := transactionParents(txn)
	if err != nil {
		return err
	}
	defer closeParents(parents)
	if err := s.rollback(ctx, txn, parents); err != nil {
		return fmt.Errorf("profile load needs recovery; keep the journal and sibling copies: %w", err)
	}
	return removeJournal(store)
}

func transactionParents(txn transaction) ([]*os.Root, error) {
	var parents []*os.Root
	for _, record := range txn.Roots {
		parent, err := os.OpenRoot(filepath.Dir(record.Root.HostPath))
		if err != nil {
			closeParents(parents)
			return nil, err
		}
		parents = append(parents, parent)
		if err := checkParent(parent, record); err != nil {
			closeParents(parents)
			return nil, err
		}
	}
	return parents, nil
}

func closeParents(parents []*os.Root) {
	for _, parent := range parents {
		parent.Close()
	}
}

func (s *Service) rollback(ctx context.Context, txn transaction, parents []*os.Root) error {
	for position := len(txn.Roots) - 1; position >= 0; position-- {
		if err := rollbackRoot(ctx, txn, txn.Roots[position], parents[position]); err != nil {
			return err
		}
	}
	return nil
}

func rollbackRoot(ctx context.Context, txn transaction, record replacement, parent *os.Root) error {
	if err := checkParent(parent, record); err != nil {
		return err
	}
	name, backup, stage := filepath.Base(record.Root.HostPath), txn.sibling(record.Root, "before"), txn.sibling(record.Root, "next")
	current, err := optionalDigest(ctx, parent, name)
	if err != nil {
		return err
	}
	before, err := optionalDigest(ctx, parent, backup)
	if err != nil {
		return err
	}
	if before != "" {
		if before != record.Before || (current != "" && current != record.After) {
			return errors.New("replacement data changed; recovery cannot overwrite it")
		}
		if current != "" {
			if err := removeTree(parent, name); err != nil {
				return err
			}
		}
		if err := parent.Rename(backup, name); err != nil {
			return err
		}
	} else if current != record.Before {
		if record.Before != "" || current != record.After {
			return errors.New("original state is missing or changed; recovery data was retained")
		}
		if err := removeTree(parent, name); err != nil {
			return err
		}
	}
	staged, err := optionalDigest(ctx, parent, stage)
	if err != nil {
		return err
	}
	if staged != "" && staged != record.After {
		return errors.New("staged state changed; recovery data was retained")
	}
	if err := removeTree(parent, stage); err != nil {
		return err
	}
	return syncDirectory(parent.Name())
}

func (s *Service) finishTransaction(store *os.Root, txn transaction) (string, error) {
	path := filepath.Join("recovery", txn.ID)
	if err := privatePath(store, path, true); err != nil {
		return "", err
	}
	if err := writeJSON(store, filepath.Join(path, "host.json"), txn); err != nil {
		return "", err
	}
	if err := removeJournal(store); err != nil {
		return "", err
	}
	return filepath.Join(s.storePath(), path), nil
}

func (s *Service) removeRecovery(ctx context.Context, store *os.Root, id string) error {
	if !storedID.MatchString(id) {
		return errors.New("invalid recovery ID")
	}
	path := filepath.Join("recovery", id)
	if err := privatePath(store, path, false); err != nil {
		return err
	}
	var txn transaction
	if err := readJSON(store, filepath.Join(path, "host.json"), &txn); err != nil {
		return err
	}
	if txn.ID != id {
		return errors.New("recovery ID differs from its record")
	}
	if err := s.validateTransaction(txn); err != nil {
		return err
	}
	parents, err := transactionParents(txn)
	if err != nil {
		return err
	}
	defer closeParents(parents)
	for position, record := range txn.Roots {
		name := txn.sibling(record.Root, "before")
		parent := parents[position]
		if err := s.options.Guard.Check(ctx, []string{filepath.Join(parent.Name(), name)}); err != nil {
			return err
		}
		digest, err := optionalDigest(ctx, parent, name)
		if err != nil {
			return err
		}
		if digest != "" && digest != record.Before {
			return errors.New("previous recovery contents changed; they were retained")
		}
		if err := removeTree(parent, name); err != nil {
			return err
		}
		if err := syncRoot(parent, "."); err != nil {
			return err
		}
	}
	return store.RemoveAll(path)
}

// Copies can contain read-only directories. Make only those directories
// writable, through anchored handles, before removing a validated copy.
func removeTree(parent *os.Root, name string) error {
	info, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return parent.Remove(name)
	}
	directory, err := parent.OpenRoot(name)
	if err != nil {
		return err
	}
	defer directory.Close()
	actual, err := directory.Stat(".")
	if err != nil || !os.SameFile(info, actual) {
		return errors.New("copy directory changed during cleanup")
	}
	if err := directory.Chmod(".", info.Mode().Perm()|0o700); err != nil {
		return err
	}
	entries, err := fs.ReadDir(directory.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := removeTree(directory, entry.Name()); err != nil {
			return err
		}
	}
	return parent.Remove(name)
}
