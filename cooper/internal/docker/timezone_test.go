package docker

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestSyncBarrelTimezoneFileCopiesConfiguredHostLocaltime(t *testing.T) {
	source := filepath.Join(t.TempDir(), "host-localtime")
	want := []byte("timezone-bytes")
	if err := os.WriteFile(source, want, 0o644); err != nil {
		t.Fatalf("WriteFile(source) failed: %v", err)
	}
	restore := SetHostLocaltimePathForTesting(source)
	t.Cleanup(restore)

	cooperDir := t.TempDir()
	hostPath, err := SyncBarrelTimezoneFile(cooperDir, "barrel-demo-claude")
	if err != nil {
		t.Fatalf("SyncBarrelTimezoneFile() failed: %v", err)
	}
	if hostPath != filepath.Join(cooperDir, "session", "barrel-demo-claude", barrelTimezoneFilename) {
		t.Fatalf("HostPath = %q, want path under barrel session dir", hostPath)
	}
	got, err := os.ReadFile(hostPath)
	if err != nil {
		t.Fatalf("ReadFile(hostPath) failed: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("timezone file bytes = %q, want %q", got, want)
	}
}

func TestPrepareSessionTimezoneFileWritesIntoReadOnlySessionDir(t *testing.T) {
	source := filepath.Join(t.TempDir(), "host-localtime")
	want := []byte("tokyo-time")
	if err := os.WriteFile(source, want, 0o644); err != nil {
		t.Fatalf("WriteFile(source) failed: %v", err)
	}
	restore := SetHostLocaltimePathForTesting(source)
	t.Cleanup(restore)

	cooperDir := t.TempDir()
	file, err := PrepareSessionTimezoneFile(cooperDir, "barrel-demo-claude", "oak-room")
	if err != nil {
		t.Fatalf("PrepareSessionTimezoneFile() failed: %v", err)
	}
	if !strings.HasPrefix(file.HostPath, filepath.Join(cooperDir, "session", "barrel-demo-claude")+string(filepath.Separator)) {
		t.Fatalf("HostPath = %q, want session file under barrel session dir", file.HostPath)
	}
	if !strings.HasPrefix(file.ContainerPath, BarrelSessionContainerDir+"/"+sessionTimezoneFilePrefix) || !strings.HasSuffix(file.ContainerPath, sessionTimezoneFileSuffix) {
		t.Fatalf("ContainerPath = %q, want read-only session path", file.ContainerPath)
	}
	got, err := os.ReadFile(file.HostPath)
	if err != nil {
		t.Fatalf("ReadFile(hostPath) failed: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("timezone file bytes = %q, want %q", got, want)
	}
}

func TestAppendVolumeMounts_IncludesBarrelTimezoneMount(t *testing.T) {
	homeDir := t.TempDir()
	absWorkspace := filepath.Join(t.TempDir(), "workspace")
	cooperDir := filepath.Join(t.TempDir(), "cooper")
	containerName := "barrel-test-claude"
	timezonePath := filepath.Join(BarrelSessionDir(cooperDir, containerName), barrelTimezoneFilename)
	if err := os.MkdirAll(filepath.Dir(timezonePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(timezone dir) failed: %v", err)
	}
	if err := os.WriteFile(timezonePath, []byte("tz"), 0o644); err != nil {
		t.Fatalf("WriteFile(timezone) failed: %v", err)
	}

	got := appendVolumeMounts(nil, absWorkspace, homeDir, &config.Config{}, cooperDir, "claude", containerName)
	want := timezonePath + ":" + barrelTimezoneContainerPath + ":ro"
	if !slices.Contains(got, want) {
		t.Fatalf("appendVolumeMounts() missing timezone mount %q\ngot: %v", want, got)
	}
}

func TestPrepareSessionTimezoneFileRandomizesNameEvenForSameSession(t *testing.T) {
	source := filepath.Join(t.TempDir(), "host-localtime")
	if err := os.WriteFile(source, []byte("tokyo-time"), 0o644); err != nil {
		t.Fatalf("WriteFile(source) failed: %v", err)
	}
	restore := SetHostLocaltimePathForTesting(source)
	t.Cleanup(restore)

	cooperDir := t.TempDir()
	first, err := PrepareSessionTimezoneFile(cooperDir, "barrel-demo-claude", "same-session")
	if err != nil {
		t.Fatalf("PrepareSessionTimezoneFile(first) failed: %v", err)
	}
	second, err := PrepareSessionTimezoneFile(cooperDir, "barrel-demo-claude", "same-session")
	if err != nil {
		t.Fatalf("PrepareSessionTimezoneFile(second) failed: %v", err)
	}
	if first.HostPath == second.HostPath || first.ContainerPath == second.ContainerPath {
		t.Fatalf("expected randomized session timezone paths, got host=%q/%q container=%q/%q", first.HostPath, second.HostPath, first.ContainerPath, second.ContainerPath)
	}
}
