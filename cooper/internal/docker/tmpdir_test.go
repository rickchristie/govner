package docker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBarrelTmpRoot(t *testing.T) {
	got := BarrelTmpRoot("/tmp/cooper")
	want := filepath.Join("/tmp/cooper", "tmp")
	if got != want {
		t.Fatalf("BarrelTmpRoot() = %q, want %q", got, want)
	}
}

func TestBarrelTmpDir(t *testing.T) {
	got := BarrelTmpDir("/tmp/cooper", "barrel-demo-claude")
	want := filepath.Join("/tmp/cooper", "tmp", "barrel-demo-claude")
	if got != want {
		t.Fatalf("BarrelTmpDir() = %q, want %q", got, want)
	}
}

func TestBarrelSessionRoot(t *testing.T) {
	got := BarrelSessionRoot("/tmp/cooper")
	want := filepath.Join("/tmp/cooper", "session")
	if got != want {
		t.Fatalf("BarrelSessionRoot() = %q, want %q", got, want)
	}
}

func TestBarrelSessionDir(t *testing.T) {
	got := BarrelSessionDir("/tmp/cooper", "barrel-demo-claude")
	want := filepath.Join("/tmp/cooper", "session", "barrel-demo-claude")
	if got != want {
		t.Fatalf("BarrelSessionDir() = %q, want %q", got, want)
	}
}

func TestResetBarrelTmpRoot_CreatesMissingDir(t *testing.T) {
	cooperDir := t.TempDir()

	if err := ResetBarrelTmpRoot(cooperDir); err != nil {
		t.Fatalf("ResetBarrelTmpRoot() failed: %v", err)
	}

	assertDirExistsAndEmpty(t, BarrelTmpRoot(cooperDir))
}

func TestResetBarrelTmpRoot_RemovesExistingContents(t *testing.T) {
	cooperDir := t.TempDir()
	tmpRoot := BarrelTmpRoot(cooperDir)
	staleFile := filepath.Join(tmpRoot, "barrel-demo", "session", "stale.txt")
	if err := os.MkdirAll(filepath.Dir(staleFile), 0o755); err != nil {
		t.Fatalf("mkdir stale dir: %v", err)
	}
	if err := os.WriteFile(staleFile, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale file: %v", err)
	}

	if err := ResetBarrelTmpRoot(cooperDir); err != nil {
		t.Fatalf("ResetBarrelTmpRoot() failed: %v", err)
	}

	assertDirExistsAndEmpty(t, tmpRoot)
}

func TestResetBarrelTmpRoot_ReplacesFileWithDir(t *testing.T) {
	cooperDir := t.TempDir()
	tmpRoot := BarrelTmpRoot(cooperDir)
	if err := os.WriteFile(tmpRoot, []byte("not-a-dir"), 0o644); err != nil {
		t.Fatalf("write tmp root placeholder: %v", err)
	}

	if err := ResetBarrelTmpRoot(cooperDir); err != nil {
		t.Fatalf("ResetBarrelTmpRoot() failed: %v", err)
	}

	assertDirExistsAndEmpty(t, tmpRoot)
}

func TestResetBarrelTmpRoot_EmptyCooperDirIsNoop(t *testing.T) {
	if err := ResetBarrelTmpRoot(" "); err != nil {
		t.Fatalf("ResetBarrelTmpRoot(empty) = %v, want nil", err)
	}
}

func TestResetBarrelSessionRoot_CreatesMissingDir(t *testing.T) {
	cooperDir := t.TempDir()

	if err := ResetBarrelSessionRoot(cooperDir); err != nil {
		t.Fatalf("ResetBarrelSessionRoot() failed: %v", err)
	}

	assertDirExistsAndEmpty(t, BarrelSessionRoot(cooperDir))
}

func TestResetBarrelSessionRoot_RemovesExistingContents(t *testing.T) {
	cooperDir := t.TempDir()
	sessionRoot := BarrelSessionRoot(cooperDir)
	staleFile := filepath.Join(sessionRoot, "barrel-demo", "stale.txt")
	if err := os.MkdirAll(filepath.Dir(staleFile), 0o755); err != nil {
		t.Fatalf("mkdir stale dir: %v", err)
	}
	if err := os.WriteFile(staleFile, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale file: %v", err)
	}

	if err := ResetBarrelSessionRoot(cooperDir); err != nil {
		t.Fatalf("ResetBarrelSessionRoot() failed: %v", err)
	}

	assertDirExistsAndEmpty(t, sessionRoot)
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
