package workload

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func registeredProfileRoot(t *testing.T, in MountInput, active bool) MountSpec {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("profiles are Linux-only")
	}
	id := strings.Repeat("a", 24)
	target := filepath.Join(in.HomeDir, ".codex")
	source := target + ".cooper-" + id
	if active {
		source = target
	}
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	identity := func(path string) map[string]uint64 {
		t.Helper()
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		stat := info.Sys().(*syscall.Stat_t)
		return map[string]uint64{"device": uint64(stat.Dev), "inode": stat.Ino}
	}
	selected := strings.Repeat("b", 24)
	if active {
		selected = id
	}
	catalog := map[string]any{"schema": 3, "hosts": map[string]any{"codex": map[string]any{"profile_id": selected}}, "profiles": []any{map[string]any{"id": id, "harness": "codex", "roots": []any{map[string]any{"id": "codex-state", "target": target, "host_path": target, "kind": Directory, "parent": identity(filepath.Dir(source)), "entry": identity(source)}}}}}
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(in.CooperDir, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(in.CooperDir, "profiles", "index.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return MountSpec{ID: "codex-state", Source: source, Target: target, Kind: Directory, Access: ReadWrite, Ownership: ProfileState}
}

func TestProfileMountRequiresExactRegisteredRoot(t *testing.T) {
	in := testMountInput(t, t.TempDir(), "codex")
	mount := registeredProfileRoot(t, in, false)
	if err := ValidateMountPlan([]MountSpec{mount}, in.CooperDir); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*MountSpec){
		func(m *MountSpec) { m.Source = filepath.Dir(m.Source) },
		func(m *MountSpec) { m.Source = in.CooperDir },
		func(m *MountSpec) { m.Source = m.Target },
		func(m *MountSpec) { m.Source = m.Target + ".cooper-" + strings.Repeat("b", 24) },
		func(m *MountSpec) { m.Target = "/etc/cooper" },
		func(m *MountSpec) { m.Target = filepath.Join(in.HomeDir, ".agents") },
		func(m *MountSpec) { m.Access = ReadOnly },
		func(m *MountSpec) { m.ID = "shared-agents" },
	} {
		bad := mount
		change(&bad)
		if err := ValidateMountPlan([]MountSpec{bad}, in.CooperDir); err == nil {
			t.Fatalf("unsafe mount accepted: %+v", bad)
		}
	}
	if err := os.Rename(mount.Source, mount.Source+"-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(mount.Source+"-old", mount.Source); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProfileSource(mount, in.CooperDir); err == nil {
		t.Fatal("root link accepted")
	}
	if err := os.Remove(mount.Source); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(mount.Source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProfileSource(mount, in.CooperDir); err == nil {
		t.Fatal("new directory inode accepted")
	}
}

func TestProfileMountRejectsUnfinishedTransactionAndPublicIndex(t *testing.T) {
	in := testMountInput(t, t.TempDir(), "codex")
	mount := registeredProfileRoot(t, in, true)
	journal := filepath.Join(in.CooperDir, "profiles", "transaction.json")
	if err := os.WriteFile(journal, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProfileSource(mount, in.CooperDir); err == nil {
		t.Fatal("unfinished state mounted")
	}
	if err := os.Remove(journal); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(in.CooperDir, "profiles", "index.json"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateProfileSource(mount, in.CooperDir); err == nil {
		t.Fatal("public metadata accepted")
	}
}

func TestWorkspaceCannotExposeOtherProfileOrItsParent(t *testing.T) {
	in := testMountInput(t, t.TempDir(), "codex")
	mount := registeredProfileRoot(t, in, false)
	for _, path := range []string{mount.Source, filepath.Dir(mount.Source), mount.Target + ".cooper-recovery-" + strings.Repeat("c", 24)} {
		if _, _, err := selectedProfileChild(path, nil, in.CooperDir); err == nil {
			t.Fatal("unselected workspace accepted", path)
		}
	}
}

func TestProfileWorkspaceMapsOnlySelectedRootAndProtectsHooks(t *testing.T) {
	for _, public := range []bool{false, true} {
		t.Run(map[bool]string{false: "sibling-workspace", true: "public-workspace"}[public], func(t *testing.T) {
			in := testMountInput(t, t.TempDir(), "codex")
			mount := registeredProfileRoot(t, in, false)
			physical := filepath.Join(mount.Source, "worktrees", "project")
			for _, path := range []string{physical, filepath.Join(mount.Target, "worktrees", "project")} {
				if err := os.MkdirAll(filepath.Join(path, ".git", "hooks"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			in.WorkspaceDir = physical
			if public {
				in.WorkspaceDir = filepath.Join(mount.Target, "worktrees", "project")
			}
			in.Agent = &AgentPaths{Mounts: []MountSpec{mount}}
			if err := EnsureDirectories(in); err != nil {
				t.Fatal(err)
			}
			plan, err := BuildMountPlan(in)
			if err != nil {
				t.Fatal(err)
			}
			protected := map[string]bool{}
			for _, item := range plan {
				if item.ID == "workspace" && item.Source != physical {
					t.Fatal("workspace used another profile")
				}
				if item.Ownership == ProfileState && item.Source != mount.Source {
					t.Fatal("plan exposed another root")
				}
				if isGitHooksMount(item) {
					if item.Access != ReadOnly {
						t.Fatal("hooks are writable")
					}
					protected[item.Target] = true
				}
			}
			for _, path := range []string{in.WorkspaceDir, filepath.Join(mount.Target, "worktrees", "project")} {
				if !protected[filepath.Join(path, ".git", "hooks")] {
					t.Fatal("hooks path is unprotected", path)
				}
			}
		})
	}
}
