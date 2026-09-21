package profiles

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupAllowsOnlyAnEmptyProfileStore(t *testing.T) {
	f := newFixture(t)
	if !CanRemoveUnusedStore(f.service.options.CooperDir) {
		t.Fatal("absent store prevents cleanup")
	}
	store, err := f.service.open(true)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if !CanRemoveUnusedStore(f.service.options.CooperDir) {
		t.Fatal("empty directory prevents cleanup")
	}
	if err := writeJSON(store, "index.json", newIndex()); err != nil {
		t.Fatal(err)
	}
	if !CanRemoveUnusedStore(f.service.options.CooperDir) {
		t.Fatal("empty index prevents cleanup")
	}
	f.write(".cooper/profiles/unknown", "retain this data")
	if CanRemoveUnusedStore(f.service.options.CooperDir) {
		t.Fatal("cleanup accepted unknown data")
	}
	if err := store.Remove("unknown"); err != nil {
		t.Fatal(err)
	}
	f.write(".codex/account", "personal")
	f.save("codex")
	if CanRemoveUnusedStore(f.service.options.CooperDir) {
		t.Fatal("cleanup accepted registered state")
	}
}

func TestRenameCannotOverwriteAnExistingDestination(t *testing.T) {
	f := newFixture(t)
	f.write("source/value", "source")
	f.write("destination/value", "destination")
	sourceID := inode(t, filepath.Join(f.home, "source"))
	parent, err := os.OpenRoot(f.home)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := renameEntry(parent, "source", "destination"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("rename did not reject the destination: %v", err)
	}
	if inode(t, filepath.Join(f.home, "source")) != sourceID || f.read("source/value") != "source" || f.read("destination/value") != "destination" {
		t.Fatal("failed rename changed an entry")
	}
}

func TestRecoveryRejectsAnExtraRegisteredPath(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.save("codex")
	f.service.checkpoint = func(point string) error {
		if point == "journal" {
			return errors.New("interrupted")
		}
		return nil
	}
	_, err := f.service.Load(t.Context(), LoadRequest{Harness: "codex", Name: "Work", Confirmed: true})
	if err == nil {
		t.Fatal("checkpoint did not interrupt")
	}
	store, err := f.service.open(false)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var txn transaction
	if err := readJSON(store, transactionFile, &txn); err != nil {
		t.Fatal(err)
	}
	root := txn.Entries[0].Root
	// This path fits the reserved name rules but is not part of a switch.
	// Recovery must verify the complete operation, not just each path.
	txn.Entries = append(txn.Entries, rootEntry{Root: root, Name: filepath.Base(root.HostPath) + ".cooper-recovery-" + txn.ID})
	if err := writeJSON(store, transactionFile, txn); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Recover(t.Context()); err == nil {
		t.Fatal("recovery accepted an extra instruction")
	}
	if f.read(".codex/account") != "personal" {
		t.Fatal("failed recovery changed state")
	}
	if _, err := store.Stat(transactionFile); err != nil {
		t.Fatal("failed recovery removed its journal")
	}
}
