package docker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

func TestAppendVolumeMountsGrokSharesCompleteHostState(t *testing.T) {
	t.Setenv("GROK_HOME", "")
	homeDir := t.TempDir()
	cooperDir := t.TempDir()
	absWorkspace := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(absWorkspace, 0o755); err != nil {
		t.Fatalf("create workspace fixture: %v", err)
	}
	containerName := "barrel-ws-grok"

	got := renderedMountsForTest(t, barrelMountInput(absWorkspace, homeDir, &config.Config{}, cooperDir, "grok", containerName))
	joined := strings.Join(got, " ")

	hostState := filepath.Join(homeDir, ".grok")
	if !containsDockerMount(got, hostState, hostState, false) {
		t.Fatalf("missing complete Grok state mount from %q\n%s", hostState, joined)
	}
	if strings.Count(joined, "dst="+hostState) != 1 {
		t.Fatalf("Grok state must use one mount: %s", joined)
	}
	for _, legacy := range []string{".cooper-grok-auth", filepath.Join(cooperDir, "secrets", "grok"), filepath.Join(cooperDir, "state", "grok")} {
		if strings.Contains(joined, legacy) {
			t.Fatalf("legacy split Grok state is still mounted: %s", joined)
		}
	}
}

func TestSharedMountPolicyListsCompleteGrokRoot(t *testing.T) {
	t.Setenv("GROK_HOME", "")
	homeDir := t.TempDir()
	cooperDir := t.TempDir()
	input := barrelMountInput(filepath.Join(t.TempDir(), "workspace"), homeDir, &config.Config{}, cooperDir, "grok", "barrel-ws-grok")
	directorySpecs, err := workload.RequiredDirectories(input)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(homeDir, ".grok")
	count := 0
	for _, directory := range directorySpecs {
		if directory.Path == want {
			count++
		}
		if strings.Contains(directory.Path, filepath.Join(cooperDir, "state", "grok")) || strings.Contains(directory.Path, filepath.Join(cooperDir, "secrets", "grok")) {
			t.Fatalf("found legacy Cooper-owned Grok path: %s", directory.Path)
		}
	}
	if count != 1 {
		t.Fatalf("complete host Grok root count = %d, directories=%v", count, directorySpecs)
	}
}

func TestSharedMountPolicyCreatesPrivateGrokRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "shared-grok")
	t.Setenv("GROK_HOME", root)
	homeDir := t.TempDir()
	cooperDir := t.TempDir()
	input := barrelMountInput(filepath.Join(t.TempDir(), "workspace"), homeDir, &config.Config{}, cooperDir, "grok", "barrel-ws-grok")
	if err := workload.EnsureDirectories(input); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("new Grok state root mode = %o, want 0700", info.Mode().Perm())
	}
}
