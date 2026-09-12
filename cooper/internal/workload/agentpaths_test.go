package workload

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentStatePathsKeepTheirHostNames(t *testing.T) {
	for tool := range agentStatePaths {
		t.Run(tool, func(t *testing.T) {
			home := t.TempDir()
			paths, err := ResolveAgentPaths(tool, home, t.TempDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(paths.Mounts) == 0 {
				t.Fatal("no state mounts")
			}
			for _, mount := range paths.Mounts {
				if mount.Source != mount.Target || mount.Access != ReadWrite || mount.Ownership != HostState {
					t.Fatalf("state mount changes host path or ownership: %#v", mount)
				}
				if mount.Source == home {
					t.Fatal("complete host home is mounted")
				}
			}
		})
	}
}

func TestCopilotCustomRootsKeepTheirHostPaths(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	state := filepath.Join(t.TempDir(), "work account")
	paths, err := ResolveAgentPaths("copilot", home, workspace, map[string]string{
		"COPILOT_HOME": state, "COPILOT_CACHE_HOME": "cache/copilot",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{state, filepath.Join(workspace, "cache/copilot")}
	if len(paths.Mounts) != len(want) {
		t.Fatalf("Copilot mounts = %#v", paths.Mounts)
	}
	for index, path := range want {
		if paths.Mounts[index].Source != path || paths.Mounts[index].Target != path {
			t.Errorf("mount %d = %#v, want %s on both sides", index, paths.Mounts[index], path)
		}
	}
	if !strings.Contains(strings.Join(RenderEnvironment(paths.Environment), "\n"), "COPILOT_CACHE_HOME=cache/copilot") {
		t.Fatal("relative Copilot cache environment changed")
	}
}

func TestCopilotCacheFollowsTheHostPlatform(t *testing.T) {
	for _, test := range []struct{ platform, home, xdg, want string }{
		{"linux", "/home/ricky", "", "/home/ricky/.cache/copilot"},
		{"linux", "/home/ricky", "/srv/cache", "/srv/cache/copilot"},
		{"linux", "/home/ricky", "relative-cache", "relative-cache/copilot"},
		{"darwin", "/Users/ricky", "", "/Users/ricky/Library/Caches/copilot"},
		{"darwin", "/Users/ricky", "/srv/cache", "/Users/ricky/Library/Caches/copilot"},
	} {
		if got := copilotDefaultCache(test.home, test.xdg, test.platform); got != test.want {
			t.Errorf("Copilot cache on %s = %q, want %q", test.platform, got, test.want)
		}
	}
}

func TestCopilotKeepsExistingXDGMigrationRoots(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	config := filepath.Join(workspace, "old-config", ".copilot")
	if err := os.MkdirAll(config, 0o700); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{"XDG_CONFIG_HOME": "old-config", "XDG_STATE_HOME": "missing-state"}
	paths, err := ResolveAgentPaths("copilot", home, workspace, values)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths.Mounts) != 3 || paths.Mounts[2].Source != config || paths.Mounts[2].Target != config {
		t.Fatalf("existing Copilot migration root is missing: %#v", paths.Mounts)
	}
	values["COPILOT_HOME"] = filepath.Join(home, "custom-copilot")
	paths, err = ResolveAgentPaths("copilot", home, workspace, values)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths.Mounts) != 2 {
		t.Fatalf("custom Copilot home must not mount old XDG roots: %#v", paths.Mounts)
	}
}

func TestStateSymlinkCannotExportTheCompleteHome(t *testing.T) {
	home := t.TempDir()
	if err := os.Symlink(home, filepath.Join(home, ".codex")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveAgentPaths("codex", home, t.TempDir(), nil); err == nil || !strings.Contains(err.Error(), "complete host home") {
		t.Fatalf("complete home symlink was accepted: %v", err)
	}
}

func TestWorkspaceSymlinkCannotExportTheCompleteHome(t *testing.T) {
	in := testMountInput(t, t.TempDir(), "codex")
	if err := os.MkdirAll(in.HomeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	in.WorkspaceDir = filepath.Join(t.TempDir(), "workspace")
	if err := os.Symlink(in.HomeDir, in.WorkspaceDir); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDirectories(in); err == nil || !strings.Contains(err.Error(), "complete host home") {
		t.Fatalf("complete home workspace symlink was accepted: %v", err)
	}
}

func TestClaudeKeepsUnsetAndEmptyConfigDirectoriesDistinct(t *testing.T) {
	home, workspace := t.TempDir(), t.TempDir()
	unset, err := ResolveAgentPaths("claude", home, workspace, nil)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := ResolveAgentPaths("claude", home, workspace, map[string]string{"CLAUDE_CONFIG_DIR": ""})
	if err != nil {
		t.Fatal(err)
	}
	if unset.Mounts[0].Target != filepath.Join(home, ".claude") || empty.Mounts[0].Target != workspace {
		t.Fatalf("unset = %#v; empty = %#v", unset, empty)
	}
	unsetValues := strings.Join(RenderEnvironment(unset.Environment), "\n")
	emptyValues := strings.Join(RenderEnvironment(empty.Environment), "\n")
	if !strings.Contains(unsetValues, "CLAUDE_CONFIG_DIR\n") || !strings.Contains(emptyValues, "CLAUDE_CONFIG_DIR=\n") {
		t.Fatalf("unset and empty environment values changed: %q, %q", unsetValues, emptyValues)
	}
}

func TestRuntimeReuseDetectsAChangedStateSymlink(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	link := filepath.Join(t.TempDir(), "selected")
	if err := os.Symlink(first, link); err != nil {
		t.Fatal(err)
	}
	mounts := []MountSpec{{ID: "state", Source: link, Target: link, Kind: Directory, Access: ReadWrite, Ownership: HostState}}
	before, err := RuntimeDigest(mounts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first, "new-session"), []byte("state"), 0o600); err != nil {
		t.Fatal(err)
	}
	afterWrite, err := RuntimeDigest(mounts, nil)
	if err != nil || before != afterWrite {
		t.Fatalf("normal state writes changed runtime identity: %v", err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, link); err != nil {
		t.Fatal(err)
	}
	afterSwitch, err := RuntimeDigest(mounts, nil)
	if err != nil || before == afterSwitch {
		t.Fatalf("changed state source did not change runtime identity: %v", err)
	}
}

func TestOpenCodeDatabaseKeepsJournalFilesTogether(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	data := filepath.Join(root, "data")
	custom := filepath.Join(data, "database")
	if err := os.MkdirAll(custom, 0o700); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(custom, "work.db")
	if err := os.WriteFile(database, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{database, "../database/work.db"} {
		paths, err := ResolveAgentPaths("opencode", home, root, map[string]string{"OPENCODE_DB": value, "XDG_DATA_HOME": data})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, mount := range paths.Mounts {
			if mount.ID == "opencode-database" {
				found = mount.Source == custom && mount.Target == custom && mount.Kind == Directory
			}
		}
		if !found || agentEnv(paths, "OPENCODE_DB") != value {
			t.Fatalf("database and journals are not shared at the host path: %#v", paths)
		}
	}
	for _, value := range []string{":memory:", "work.db", filepath.Join(home, ".local", "share", "opencode", "new.db")} {
		paths, err := ResolveAgentPaths("opencode", home, root, map[string]string{"OPENCODE_DB": value})
		if err != nil || len(paths.Mounts) != 5 {
			t.Fatalf("database %q needs no extra mount: %#v, %v", value, paths, err)
		}
	}
}

func TestDockerMountPathsUseCSVQuoting(t *testing.T) {
	source := `/srv/Work, personal/"agent"`
	value := DockerBindMount(source, source, true)
	fields, err := csv.NewReader(strings.NewReader(value)).Read()
	if err != nil || len(fields) != 4 || fields[1] != "src="+source || fields[2] != "dst="+source || fields[3] != "readonly" {
		t.Fatalf("mount fields = %#v, error = %v", fields, err)
	}
}

func TestAgentWorkspaceInsideStatePreservesTheHostPath(t *testing.T) {
	in := testMountInput(t, t.TempDir(), "codex")
	in.WorkspaceDir = filepath.Join(in.HomeDir, ".codex", "worktrees", "project")
	if err := os.MkdirAll(in.WorkspaceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDirectories(in); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildMountPlan(in); err != nil {
		t.Fatal(err)
	}
}

func TestCodexExplicitHomeUsesTheCanonicalDirectory(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	if err := os.Mkdir(state, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "state-link")
	if err := os.Symlink(state, link); err != nil {
		t.Fatal(err)
	}
	paths, err := ResolveAgentPaths("codex", filepath.Join(root, "home"), root, map[string]string{"CODEX_HOME": "state-link"})
	if err != nil {
		t.Fatal(err)
	}
	if paths.Mounts[0].Source != state || paths.Mounts[0].Target != state || agentEnv(paths, "CODEX_HOME") != state {
		t.Fatalf("explicit Codex home = %#v", paths)
	}
	if _, err := ResolveAgentPaths("codex", filepath.Join(root, "home"), root, map[string]string{"CODEX_HOME": "missing"}); err == nil {
		t.Fatal("accepted a missing explicit CODEX_HOME")
	}
}

func TestRelativeAgentRootsUseTheLaunchDirectory(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	launch := filepath.Join(root, "project")
	for _, value := range []string{"state", " state with spaces "} {
		paths, err := ResolveAgentPaths("grok", home, launch, map[string]string{"GROK_HOME": value})
		if err != nil {
			t.Fatal(err)
		}
		if paths.Mounts[0].Target != filepath.Join(launch, value) || agentEnv(paths, "GROK_HOME") != value {
			t.Fatalf("relative Grok path = %#v", paths)
		}
		if agentEnv(paths, "GROK_LEADER_SOCKET") != GrokLeaderSocket {
			t.Fatal("leader transport is not isolated")
		}
	}
	paths, err := ResolveAgentPaths("opencode", home, launch, map[string]string{"XDG_DATA_HOME": "data"})
	if err != nil {
		t.Fatal(err)
	}
	for _, mount := range paths.Mounts {
		if mount.ID == "opencode-share" && mount.Target != filepath.Join(launch, "data", "opencode") {
			t.Fatalf("relative XDG path = %#v", mount)
		}
	}
}

func TestOpenCodeConfigInsideItsStateDoesNotAddAnotherMount(t *testing.T) {
	home := t.TempDir()
	config := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(config, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(config, "opencode.json")
	if err := os.WriteFile(file, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := ResolveAgentPaths("opencode", home, home, map[string]string{"OPENCODE_CONFIG": file, "OPENCODE_CONFIG_DIR": config})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths.Mounts) != 5 {
		t.Fatalf("mounts = %#v", paths.Mounts)
	}
	if agentEnv(paths, "OPENCODE_CONFIG") != file {
		t.Fatal("custom config environment is missing")
	}
}

func agentEnv(paths AgentPaths, name string) string {
	for _, value := range paths.Environment {
		if value.Name == name {
			return value.Value
		}
	}
	return ""
}
