package docker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/runtimefs"
)

func TestSharedMountPlanIncludesRuntimeTimezone(t *testing.T) {
	homeDir := t.TempDir()
	absWorkspace := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(absWorkspace, 0o755); err != nil {
		t.Fatalf("create workspace fixture: %v", err)
	}
	cooperDir := filepath.Join(t.TempDir(), "cooper")
	containerName := "barrel-test-claude"
	timezonePath := filepath.Join(runtimefs.SessionDir(cooperDir, containerName), runtimefs.TimezoneFilename)
	if err := os.MkdirAll(filepath.Dir(timezonePath), 0o755); err != nil {
		t.Fatalf("MkdirAll(timezone dir) failed: %v", err)
	}
	if err := os.WriteFile(timezonePath, []byte("tz"), 0o644); err != nil {
		t.Fatalf("WriteFile(timezone) failed: %v", err)
	}

	got := renderedMountsForTest(t, barrelMountInput(absWorkspace, homeDir, &config.Config{}, cooperDir, "claude", containerName))
	if !containsDockerMount(got, timezonePath, runtimefs.TimezoneContainerPath, true) {
		t.Fatalf("shared mount policy is missing timezone mount from %q\ngot: %v", timezonePath, got)
	}
}
