package barrelenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
)

func TestPrepareSessionEnvFileWritesFileWithExpectedMode(t *testing.T) {
	cooperDir := t.TempDir()
	containerName := "barrel-demo-claude"
	sessionName := "session-a"

	file, warnings, err := PrepareSessionEnvFile(cooperDir, containerName, sessionName, []config.BarrelEnvVar{{Name: "FOO", Value: "1"}})
	if err != nil {
		t.Fatalf("PrepareSessionEnvFile() failed: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if file.HostPath == "" {
		t.Fatal("expected HostPath to be populated")
	}
	if !strings.HasPrefix(file.ContainerPath, docker.BarrelSessionContainerDir+"/cooper-cli-env-") || !strings.HasSuffix(file.ContainerPath, ".sh") {
		t.Fatalf("ContainerPath = %q, want path under %q", file.ContainerPath, docker.BarrelSessionContainerDir)
	}
	if !strings.HasPrefix(file.HostPath, docker.BarrelSessionDir(cooperDir, containerName)+string(filepath.Separator)) {
		t.Fatalf("HostPath = %q, want path under %q", file.HostPath, docker.BarrelSessionDir(cooperDir, containerName))
	}
	info, err := os.Stat(file.HostPath)
	if err != nil {
		t.Fatalf("Stat(%q) failed: %v", file.HostPath, err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 0600", got)
	}
}

func TestPrepareSessionEnvFileReturnsNoFileWhenAllEntriesFiltered(t *testing.T) {
	file, warnings, err := PrepareSessionEnvFile(t.TempDir(), "barrel-demo-claude", "session-a", []config.BarrelEnvVar{
		{Name: "HTTP_PROXY", Value: "http://bad"},
		{Name: "BAD-NAME", Value: "x"},
	})
	if err != nil {
		t.Fatalf("PrepareSessionEnvFile() failed: %v", err)
	}
	if file.HostPath != "" || file.ContainerPath != "" {
		t.Fatalf("expected no env file, got %+v", file)
	}
	if len(warnings) == 0 {
		t.Fatal("expected warnings when all entries are filtered out")
	}
}

func TestRemoveSessionEnvFileRemovesExistingAndMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.sh")
	if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}
	if err := RemoveSessionEnvFile(path); err != nil {
		t.Fatalf("RemoveSessionEnvFile(existing) failed: %v", err)
	}
	if err := RemoveSessionEnvFile(path); err != nil {
		t.Fatalf("RemoveSessionEnvFile(missing) failed: %v", err)
	}
}

func TestPrepareSessionEnvFileDoesNotClobberOtherSessions(t *testing.T) {
	cooperDir := t.TempDir()
	containerName := "barrel-demo-claude"

	fileA, _, err := PrepareSessionEnvFile(cooperDir, containerName, "a", []config.BarrelEnvVar{{Name: "FOO", Value: "one"}})
	if err != nil {
		t.Fatalf("PrepareSessionEnvFile(a) failed: %v", err)
	}
	fileB, _, err := PrepareSessionEnvFile(cooperDir, containerName, "b", []config.BarrelEnvVar{{Name: "FOO", Value: "two"}})
	if err != nil {
		t.Fatalf("PrepareSessionEnvFile(b) failed: %v", err)
	}
	if fileA.HostPath == fileB.HostPath {
		t.Fatal("expected distinct host paths for different session names")
	}
	if fileA.ContainerPath == fileB.ContainerPath {
		t.Fatal("expected distinct container paths for different session names")
	}
	dataA, err := os.ReadFile(fileA.HostPath)
	if err != nil {
		t.Fatalf("ReadFile(a) failed: %v", err)
	}
	dataB, err := os.ReadFile(fileB.HostPath)
	if err != nil {
		t.Fatalf("ReadFile(b) failed: %v", err)
	}
	if string(dataA) == string(dataB) {
		t.Fatalf("expected different file contents, got %q", string(dataA))
	}
}

func TestPrepareSessionEnvFileRandomizesNameEvenForSameSession(t *testing.T) {
	cooperDir := t.TempDir()
	containerName := "barrel-demo-claude"

	fileA, _, err := PrepareSessionEnvFile(cooperDir, containerName, "same-session", []config.BarrelEnvVar{{Name: "FOO", Value: "one"}})
	if err != nil {
		t.Fatalf("PrepareSessionEnvFile(first) failed: %v", err)
	}
	fileB, _, err := PrepareSessionEnvFile(cooperDir, containerName, "same-session", []config.BarrelEnvVar{{Name: "FOO", Value: "one"}})
	if err != nil {
		t.Fatalf("PrepareSessionEnvFile(second) failed: %v", err)
	}
	if fileA.HostPath == fileB.HostPath || fileA.ContainerPath == fileB.ContainerPath {
		t.Fatalf("expected randomized session file paths, got host=%q/%q container=%q/%q", fileA.HostPath, fileB.HostPath, fileA.ContainerPath, fileB.ContainerPath)
	}
}
