package docker

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

func renderedMountsForTest(t *testing.T, input workload.MountInput) []string {
	t.Helper()
	if err := workload.EnsureDirectories(input); err != nil {
		t.Fatal(err)
	}
	mounts, err := workload.BuildMountPlan(input)
	if err != nil {
		t.Fatal(err)
	}
	return appendDockerMounts(nil, mounts)
}

func containsDockerMount(args []string, source, target string, readOnly bool) bool {
	want := "type=bind,src=" + source + ",dst=" + target
	if readOnly {
		want += ",readonly"
	}
	for _, argument := range args {
		if argument == want {
			return true
		}
	}
	return false
}

func TestBarrelMountInputUsesSharedPolicyValues(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	home := filepath.Join(root, "home")
	cooperDir := filepath.Join(root, "cooper")
	cfg := config.DefaultConfig()

	got := barrelMountInput(workspace, home, cfg, cooperDir, "codex", "barrel-project-codex")
	want := workload.MountInput{
		WorkspaceDir:  workspace,
		HomeDir:       home,
		CooperDir:     cooperDir,
		RuntimeID:     "barrel-project-codex",
		ToolName:      "codex",
		GrokStateRoot: filepath.Join(home, ".grok"),
		Config:        cfg,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("barrelMountInput() = %#v; want %#v", got, want)
	}
}

func TestAppendDockerMountsPreservesSharedAccessPolicy(t *testing.T) {
	t.Parallel()
	mounts := []workload.MountSpec{
		{ID: "workspace", Source: "/host/work", Target: "/host/work", Access: workload.ReadWrite},
		{ID: "git-hooks", Source: "/host/work/.git/hooks", Target: "/host/work/.git/hooks", Access: workload.ReadOnly},
	}
	got := appendDockerMounts([]string{"run"}, mounts)
	want := []string{
		"run",
		"--mount", "type=bind,src=/host/work,dst=/host/work",
		"--mount", "type=bind,src=/host/work/.git/hooks,dst=/host/work/.git/hooks,readonly",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("appendDockerMounts() = %#v; want %#v", got, want)
	}
}
