package workload

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileWorktreeIsBoundToSelectedRoot(t *testing.T) {
	base := t.TempDir()
	cooper := filepath.Join(base, "cooper")
	target := filepath.Join(base, "home", ".codex")
	source := filepath.Join(cooper, "profiles", "harnesses", "codex", strings.Repeat("a", 24), strings.Repeat("b", 24), "roots", "codex-state")
	project := filepath.Join(source, "worktrees", "project")
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, target); err != nil {
		t.Fatal(err)
	}
	root := MountSpec{ID: "codex-state", Source: source, Target: target, Access: ReadWrite, Kind: Directory, Ownership: ProfileState}
	workspace := MountSpec{ID: "workspace", Source: project, Target: filepath.Join(target, "worktrees", "project"), Access: ReadWrite, Kind: Directory, Ownership: HostWorkspace}
	if err := ValidateMountPlan([]MountSpec{root, workspace}, cooper); err != nil {
		t.Fatal("selected worktree refused", err)
	}
	resolved, allowed, err := selectedProfileChild(workspace.Target, []MountSpec{root}, cooper)
	if err != nil || !allowed || resolved != project {
		t.Fatal("source not fixed", resolved, allowed, err)
	}
	for _, path := range []string{source, filepath.Dir(source), cooper, filepath.Join(cooper, "profiles", "harnesses", "codex", strings.Repeat("c", 24))} {
		workspace.Source = path
		if err := ValidateMountPlan([]MountSpec{root, workspace}, cooper); err == nil {
			t.Fatal("unapproved workspace accepted", path)
		}
	}
}

func TestProfileStateCanCoverWorkspaceAlias(t *testing.T) {
	base := t.TempDir()
	cooper := filepath.Join(base, "cooper")
	workspace := filepath.Join(base, "project")
	source := filepath.Join(cooper, "profiles", "harnesses", "claude", strings.Repeat("a", 24), strings.Repeat("b", 24), "roots", "claude-state")
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(workspace, "state")
	if err := os.Symlink(source, target); err != nil {
		t.Fatal(err)
	}
	mounts := []MountSpec{{ID: "workspace", Source: workspace, Target: workspace, Access: ReadWrite, Kind: Directory, Ownership: HostWorkspace}, {ID: "claude-state", Source: source, Target: target, Access: ReadWrite, Kind: Directory, Ownership: ProfileState}}
	if err := ValidateMountPlan(mounts, cooper); err != nil {
		t.Fatal(err)
	}
}

func TestHostStateLinkKeepsReadOnlyHooksAtBothPaths(t *testing.T) {
	for _, workspace := range []string{"public", "canonical"} {
		t.Run(workspace, func(t *testing.T) {
			in := testMountInput(t, t.TempDir(), "codex")
			source := filepath.Join(in.HomeDir, "codex-state")
			target := filepath.Join(in.HomeDir, ".codex")
			if err := os.MkdirAll(filepath.Join(source, "worktrees/project/.git"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(source, target); err != nil {
				t.Fatal(err)
			}
			in.Agent = &AgentPaths{Mounts: []MountSpec{{ID: "codex-state", Source: target, Target: target, Access: ReadWrite, Kind: Directory, Ownership: HostState, CanonicalPaths: []string{source}}}}
			workspaceRoot := target
			if workspace == "canonical" {
				workspaceRoot = source
			}
			in.WorkspaceDir = filepath.Join(workspaceRoot, "worktrees/project")
			if err := EnsureDirectories(in); err != nil {
				t.Fatal(err)
			}
			mounts, err := BuildMountPlan(in)
			if err != nil {
				t.Fatal(err)
			}
			for _, root := range []string{target, source} {
				hooks := filepath.Join(root, "worktrees/project/.git/hooks")
				found := false
				for _, mount := range mounts {
					if mount.Target == hooks && mount.Access == ReadOnly {
						found = true
					}
				}
				if !found {
					t.Errorf("hooks remain writable at %s", hooks)
				}
			}
		})
	}
}

func TestProfileCanonicalPathsKeepSelectedStateAndReadOnlyHooks(t *testing.T) {
	for _, workspace := range []string{"public", "canonical"} {
		t.Run(workspace, func(t *testing.T) {
			in := testMountInput(t, t.TempDir(), "codex")
			source := filepath.Join(in.CooperDir, "profiles", "harnesses", "codex", strings.Repeat("a", 24), strings.Repeat("b", 24), "roots", "codex-state")
			target := filepath.Join(in.HomeDir, ".codex")
			if err := os.MkdirAll(filepath.Join(source, "worktrees/project/.git"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(source, target); err != nil {
				t.Fatal(err)
			}
			historical := strings.Replace(source, strings.Repeat("b", 24), strings.Repeat("d", 24), 1)
			in.Agent = &AgentPaths{Mounts: []MountSpec{{ID: "codex-state", Source: source, Target: target, Access: ReadWrite, Kind: Directory, Ownership: ProfileState, CanonicalPaths: []string{historical, source}}}}
			workspaceRoot := target
			if workspace == "canonical" {
				workspaceRoot = source
			}
			in.WorkspaceDir = filepath.Join(workspaceRoot, "worktrees/project")
			if err := EnsureDirectories(in); err != nil {
				t.Fatal(err)
			}
			mounts, err := BuildMountPlan(in)
			if err != nil {
				t.Fatal(err)
			}
			var alias MountSpec
			hooks := map[string]bool{}
			for _, mount := range mounts {
				if isProfileAlias(mount) {
					alias = mount
				}
				if isGitHooksMount(mount) && mount.Access == ReadOnly {
					hooks[mount.Target] = true
				}
			}
			if len(hooks) != 3 {
				t.Errorf("read-only hook targets = %v; want three distinct paths", hooks)
			}
			if alias.Source != source || alias.Target != source {
				t.Fatal("canonical root is unavailable", alias)
			}
			if !hooks[filepath.Join(target, "worktrees/project/.git/hooks")] || !hooks[filepath.Join(source, "worktrees/project/.git/hooks")] {
				t.Fatal("a worktree path has writable hooks", hooks)
			}
			if !hooks[filepath.Join(historical, "worktrees/project/.git/hooks")] {
				t.Fatal("a historical state path has writable hooks")
			}
			primary := in.Agent.Mounts[0]
			if err := ValidateMountPlan([]MountSpec{alias}, in.CooperDir); err == nil {
				t.Fatal("standalone alias accepted")
			}
			alias.Target = filepath.Dir(source)
			if err := ValidateMountPlan([]MountSpec{primary, alias}, in.CooperDir); err == nil {
				t.Fatal("parent alias accepted")
			}
			sibling := strings.Replace(source, strings.Repeat("a", 24), strings.Repeat("c", 24), 1)
			if err := os.MkdirAll(sibling, 0700); err != nil {
				t.Fatal(err)
			}
			alias.Source, alias.Target = sibling, sibling
			if err := ValidateMountPlan([]MountSpec{primary, alias}, in.CooperDir); err == nil {
				t.Fatal("sibling account alias accepted")
			}
		})
	}
}
