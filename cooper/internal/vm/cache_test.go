package vm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveCacheRemovesOnlyVMTree(t *testing.T) {
	t.Parallel()
	cooperDir := t.TempDir()
	vmFile := filepath.Join(cooperDir, "vm", "assets", "base")
	otherFile := filepath.Join(cooperDir, "config.json")
	controlFile := filepath.Join(controlRoot(cooperDir), "runtime", "control.sock")
	if err := os.MkdirAll(filepath.Dir(vmFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vmFile, []byte("vm"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherFile, []byte("config"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(controlFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(controlFile, []byte("control"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveCache(cooperDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cooperDir, "vm")); !os.IsNotExist(err) {
		t.Fatalf("VM tree still exists: %v", err)
	}
	if _, err := os.Stat(otherFile); err != nil {
		t.Fatalf("non-VM file changed: %v", err)
	}
	if _, err := os.Stat(controlRoot(cooperDir)); !os.IsNotExist(err) {
		t.Fatalf("VM control cache still exists: %v", err)
	}
}

func TestRemoveCacheRejectsRelativeCooperDirectory(t *testing.T) {
	t.Parallel()
	if err := RemoveCache("relative"); err == nil {
		t.Fatal("RemoveCache() accepted a relative Cooper directory")
	}
}
