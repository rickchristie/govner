package runtimefs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimePaths(t *testing.T) {
	t.Parallel()
	cooperDir := "/tmp/cooper"
	if got, want := TempRoot(cooperDir), "/tmp/cooper/tmp"; got != want {
		t.Fatalf("TempRoot() = %q, want %q", got, want)
	}
	if got, want := SessionRoot(cooperDir), "/tmp/cooper/session"; got != want {
		t.Fatalf("SessionRoot() = %q, want %q", got, want)
	}
	if got, want := TempDir(cooperDir, "runtime-a"), "/tmp/cooper/tmp/runtime-a"; got != want {
		t.Fatalf("TempDir() = %q, want %q", got, want)
	}
	if got, want := SessionDir(cooperDir, "runtime-a"), "/tmp/cooper/session/runtime-a"; got != want {
		t.Fatalf("SessionDir() = %q, want %q", got, want)
	}
}

func TestCountActiveShells(t *testing.T) {
	cooperDir := t.TempDir()
	marker, err := CreateShellMarker(cooperDir, "runtime-a")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = RemoveShellMarker(marker.HostPath) })

	count, err := CountActiveShells(cooperDir, "runtime-a")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("CountActiveShells() = %d, want 1", count)
	}
}

func TestCountActiveShellsRemovesStaleMarkers(t *testing.T) {
	cooperDir := t.TempDir()
	dir := SessionDir(cooperDir, "runtime-a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(dir, shellMarkerPrefix+"stale"+shellMarkerSuffix)
	if err := os.WriteFile(stale, []byte("999999"), 0o600); err != nil {
		t.Fatal(err)
	}

	count, err := CountActiveShells(cooperDir, "runtime-a")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("CountActiveShells() = %d, want 0", count)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale marker still exists: %v", err)
	}
}
