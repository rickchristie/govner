package docker

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
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
	if hostPath != filepath.Join(cooperDir, "tmp", "barrel-demo-claude", barrelTimezoneFilename) {
		t.Fatalf("HostPath = %q, want path under barrel tmp dir", hostPath)
	}
	got, err := os.ReadFile(hostPath)
	if err != nil {
		t.Fatalf("ReadFile(hostPath) failed: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("timezone file bytes = %q, want %q", got, want)
	}
}

func TestPrepareSessionTimezoneFileWritesIntoMountedTmpDir(t *testing.T) {
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
	if file.HostPath != filepath.Join(cooperDir, "tmp", "barrel-demo-claude", sessionTimezoneFilePrefix+"oak-room"+sessionTimezoneFileSuffix) {
		t.Fatalf("HostPath = %q, want session file under barrel tmp dir", file.HostPath)
	}
	if file.ContainerPath != "/tmp/"+sessionTimezoneFilePrefix+"oak-room"+sessionTimezoneFileSuffix {
		t.Fatalf("ContainerPath = %q, want /tmp session path", file.ContainerPath)
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
	timezonePath := filepath.Join(BarrelTmpDir(cooperDir, containerName), barrelTimezoneFilename)
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
