package docker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/runtimefs"
)

func TestResetRuntimeTempRootCreatesMissingDirectory(t *testing.T) {
	t.Setenv("GROK_HOME", "")
	cooperDir := t.TempDir()

	if err := ResetRuntimeTempRoot(cooperDir); err != nil {
		t.Fatalf("ResetRuntimeTempRoot() failed: %v", err)
	}

	assertDirExistsAndEmpty(t, runtimefs.TempRoot(cooperDir))
}

func TestResetRuntimeTempRootRemovesExistingContents(t *testing.T) {
	t.Setenv("GROK_HOME", "")
	cooperDir := t.TempDir()
	tmpRoot := runtimefs.TempRoot(cooperDir)
	staleFile := filepath.Join(tmpRoot, "barrel-demo", "session", "stale.txt")
	if err := os.MkdirAll(filepath.Dir(staleFile), 0o755); err != nil {
		t.Fatalf("mkdir stale dir: %v", err)
	}
	if err := os.WriteFile(staleFile, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale file: %v", err)
	}

	if err := ResetRuntimeTempRoot(cooperDir); err != nil {
		t.Fatalf("ResetRuntimeTempRoot() failed: %v", err)
	}

	assertDirExistsAndEmpty(t, tmpRoot)
}

func TestResetRuntimeTempRootReplacesFileWithDirectory(t *testing.T) {
	t.Setenv("GROK_HOME", "")
	cooperDir := t.TempDir()
	tmpRoot := runtimefs.TempRoot(cooperDir)
	if err := os.WriteFile(tmpRoot, []byte("not-a-dir"), 0o644); err != nil {
		t.Fatalf("write tmp root placeholder: %v", err)
	}

	if err := ResetRuntimeTempRoot(cooperDir); err != nil {
		t.Fatalf("ResetRuntimeTempRoot() failed: %v", err)
	}

	assertDirExistsAndEmpty(t, tmpRoot)
}

func TestResetRuntimeTempRootWithEmptyCooperDirectoryDoesNothing(t *testing.T) {
	if err := ResetRuntimeTempRoot(" "); err != nil {
		t.Fatalf("ResetRuntimeTempRoot(empty) = %v, want nil", err)
	}
}

func TestResetRuntimeSessionRootCreatesMissingDirectory(t *testing.T) {
	t.Setenv("GROK_HOME", "")
	cooperDir := t.TempDir()

	if err := ResetRuntimeSessionRoot(cooperDir); err != nil {
		t.Fatalf("ResetRuntimeSessionRoot() failed: %v", err)
	}

	assertDirExistsAndEmpty(t, runtimefs.SessionRoot(cooperDir))
}

func TestResetRuntimeSessionRootRemovesExistingContents(t *testing.T) {
	t.Setenv("GROK_HOME", "")
	cooperDir := t.TempDir()
	sessionRoot := runtimefs.SessionRoot(cooperDir)
	staleFile := filepath.Join(sessionRoot, "barrel-demo", "stale.txt")
	if err := os.MkdirAll(filepath.Dir(staleFile), 0o755); err != nil {
		t.Fatalf("mkdir stale dir: %v", err)
	}
	if err := os.WriteFile(staleFile, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale file: %v", err)
	}

	if err := ResetRuntimeSessionRoot(cooperDir); err != nil {
		t.Fatalf("ResetRuntimeSessionRoot() failed: %v", err)
	}

	assertDirExistsAndEmpty(t, sessionRoot)
}

func TestResetRuntimeTempRootPreservesOverlappingGrokState(t *testing.T) {
	cooperDir := t.TempDir()
	stateRoot := filepath.Join(runtimefs.TempRoot(cooperDir), "grok")
	marker := filepath.Join(stateRoot, "auth.json")
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("host-owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GROK_HOME", stateRoot)

	if err := ResetRuntimeTempRoot(cooperDir); err == nil {
		t.Fatal("ResetRuntimeTempRoot() succeeded for overlapping Grok state")
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "host-owned" {
		t.Fatalf("host-owned Grok state changed: data=%q err=%v", data, err)
	}
}

func assertDirExistsAndEmpty(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatalf("read dir %s: %v", path, err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected %s to be empty, got %d entries", path, len(entries))
	}
}
