package runtimefs

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncTimezoneFileCopiesConfiguredHostLocaltime(t *testing.T) {
	source := filepath.Join(t.TempDir(), "host-localtime")
	want := []byte("timezone-bytes")
	if err := os.WriteFile(source, want, 0o644); err != nil {
		t.Fatal(err)
	}
	restore := SetHostLocaltimePathForTesting(source)
	t.Cleanup(restore)

	cooperDir := t.TempDir()
	hostPath, err := SyncTimezoneFile(cooperDir, "runtime-a")
	if err != nil {
		t.Fatal(err)
	}
	if hostPath != filepath.Join(SessionDir(cooperDir, "runtime-a"), TimezoneFilename) {
		t.Fatalf("host path = %q", hostPath)
	}
	got, err := os.ReadFile(hostPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("timezone data = %q, want %q", got, want)
	}
}

func TestPrepareSessionTimezoneFileUsesReadOnlySessionPath(t *testing.T) {
	source := filepath.Join(t.TempDir(), "host-localtime")
	want := []byte("tokyo-time")
	if err := os.WriteFile(source, want, 0o644); err != nil {
		t.Fatal(err)
	}
	restore := SetHostLocaltimePathForTesting(source)
	t.Cleanup(restore)

	cooperDir := t.TempDir()
	file, err := PrepareSessionTimezoneFile(cooperDir, "runtime-a")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(file.HostPath, SessionDir(cooperDir, "runtime-a")+string(filepath.Separator)) {
		t.Fatalf("host path = %q", file.HostPath)
	}
	if !strings.HasPrefix(file.ContainerPath, SessionContainerDir+"/"+sessionTimezonePrefix) || !strings.HasSuffix(file.ContainerPath, sessionTimezoneSuffix) {
		t.Fatalf("container path = %q", file.ContainerPath)
	}
	got, err := os.ReadFile(file.HostPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("timezone data = %q, want %q", got, want)
	}
}

func TestPrepareSessionTimezoneFileRandomizesName(t *testing.T) {
	source := filepath.Join(t.TempDir(), "host-localtime")
	if err := os.WriteFile(source, []byte("tokyo-time"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := SetHostLocaltimePathForTesting(source)
	t.Cleanup(restore)

	cooperDir := t.TempDir()
	first, err := PrepareSessionTimezoneFile(cooperDir, "runtime-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := PrepareSessionTimezoneFile(cooperDir, "runtime-a")
	if err != nil {
		t.Fatal(err)
	}
	if first.HostPath == second.HostPath || first.ContainerPath == second.ContainerPath {
		t.Fatal("session timezone paths are not random")
	}
}
