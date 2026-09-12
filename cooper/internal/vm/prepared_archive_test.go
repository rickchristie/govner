package vm

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadImageArchiveCannotPrepareOrRepair(t *testing.T) {
	root := t.TempDir()
	id := "sha256:" + strings.Repeat("a", 64)
	missing := filepath.Join(root, "missing")
	if _, err := ReadImageArchive(missing, id); err == nil {
		t.Fatal("missing cache accepted")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("read created a cache")
	}
	runner := &imageArchiveRunner{imageID: id, payload: "small test archive"}
	archive, err := EnsureImageArchive(context.Background(), root, "test", runner, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ReadImageArchive(root, id); err != nil || got != archive {
		t.Fatalf("warm read: %#v %v", got, err)
	}
	manager := Manager{SkipPrepare: true, PreparedBase: "test-base", PreparedArchive: &archive}
	if err := manager.checkPreparedArchive(id); err != nil {
		t.Fatal(err)
	}
	if err := manager.checkPreparedArchive("sha256:" + strings.Repeat("b", 64)); err == nil {
		t.Fatal("changed image accepted")
	}
	if err := os.Remove(archive.Path); err != nil {
		t.Fatal(err)
	}
	if err := manager.checkPreparedArchive(id); err == nil {
		t.Fatal("missing prepared archive accepted")
	}
	if runner.saveCalls != 1 {
		t.Fatal("read exported an image")
	}
	if _, err := os.Stat(archive.Path); !os.IsNotExist(err) {
		t.Fatal("read repaired the archive")
	}
}
