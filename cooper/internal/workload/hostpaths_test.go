package workload

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrokHostStateRootUsesTheProcessWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	t.Setenv("GROK_HOME", "relative-grok")

	want := filepath.Join(root, "relative-grok")
	if got := GrokHostStateRoot(t.TempDir()); got != want {
		t.Fatalf("GrokHostStateRoot() = %q, want %q", got, want)
	}
}

func TestValidateAllHostAgentStateRootsRejectsEachBuiltInRoot(t *testing.T) {
	t.Setenv("GROK_HOME", "")
	home := t.TempDir()
	for _, stateRoot := range HostAgentStateRoots(home) {
		stateRoot := stateRoot
		t.Run(filepath.Base(stateRoot), func(t *testing.T) {
			cooperDir := filepath.Join(stateRoot, "cooper-owned")
			if err := ValidateAllHostAgentStateRoots(home, cooperDir); err == nil || !strings.Contains(err.Error(), "host-owned agent state") {
				t.Fatalf("ValidateAllHostAgentStateRoots() error = %v, want overlap error", err)
			}
		})
	}
}

func TestValidateAllHostAgentStateRootsRejectsResolvedOverlap(t *testing.T) {
	t.Setenv("GROK_HOME", "")
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(home, ".codex")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "state-link")
	if err := os.Symlink(state, link); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAllHostAgentStateRoots(home, filepath.Join(link, "cooper")); err == nil || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("ValidateAllHostAgentStateRoots() error = %v, want resolved overlap error", err)
	}
}

func TestValidateAllHostAgentStateRootsAllowsSiblingDirectory(t *testing.T) {
	t.Setenv("GROK_HOME", "")
	root := t.TempDir()
	home := filepath.Join(root, "home")
	cooperDir := filepath.Join(root, "cooper")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAllHostAgentStateRoots(home, cooperDir); err != nil {
		t.Fatalf("ValidateAllHostAgentStateRoots() error = %v", err)
	}
}
