package docker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCountActiveShellSessions(t *testing.T) {
	cooperDir := t.TempDir()
	containerName := "barrel-test"
	marker, err := CreateShellSessionMarker(cooperDir, containerName, "demo")
	if err != nil {
		t.Fatalf("CreateShellSessionMarker() failed: %v", err)
	}
	t.Cleanup(func() { _ = RemoveShellSessionMarker(marker.HostPath) })

	count, err := CountActiveShellSessions(cooperDir, containerName)
	if err != nil {
		t.Fatalf("CountActiveShellSessions() failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("CountActiveShellSessions() = %d, want 1", count)
	}
}

func TestCountActiveShellSessionsRemovesStaleMarkers(t *testing.T) {
	cooperDir := t.TempDir()
	containerName := "barrel-test"
	dir := BarrelSessionDir(cooperDir, containerName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s) failed: %v", dir, err)
	}
	stale := filepath.Join(dir, shellSessionFilePrefix+"stale"+shellSessionFileSuffix)
	if err := os.WriteFile(stale, []byte("999999"), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) failed: %v", stale, err)
	}

	count, err := CountActiveShellSessions(cooperDir, containerName)
	if err != nil {
		t.Fatalf("CountActiveShellSessions() failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("CountActiveShellSessions() = %d, want 0", count)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("expected stale marker to be removed, stat err = %v", err)
	}
}
