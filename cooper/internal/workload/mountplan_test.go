package workload

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestBuildMountPlanSelectedAgentState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		tool        string
		wantTargets []string
	}{
		{name: "Claude", tool: "claude", wantTargets: []string{HomeDir + "/.claude", HomeDir + "/.claude.json"}},
		{name: "Copilot", tool: "copilot", wantTargets: []string{HomeDir + "/.copilot"}},
		{name: "Codex", tool: "codex", wantTargets: []string{HomeDir + "/.codex"}},
		{name: "OpenCode", tool: "opencode", wantTargets: []string{HomeDir + "/.cache/opencode", HomeDir + "/.config/opencode", HomeDir + "/.local/share/opencode", HomeDir + "/.local/state/opencode", HomeDir + "/.opencode"}},
		{name: "Grok", tool: "grok", wantTargets: []string{GrokStateRoot}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			in := testMountInput(t, root, test.tool)
			if err := EnsureDirectories(in); err != nil {
				t.Fatalf("EnsureDirectories() error = %v", err)
			}
			if err := os.WriteFile(filepath.Join(in.HomeDir, ".claude.json"), []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}

			mounts, err := BuildMountPlan(in)
			if err != nil {
				t.Fatalf("BuildMountPlan() error = %v", err)
			}

			gotTargets := targetsWithOwnership(mounts, HostState)
			slices.Sort(gotTargets)
			wantTargets := append([]string(nil), test.wantTargets...)
			slices.Sort(wantTargets)
			if !slices.Equal(gotTargets, wantTargets) {
				t.Fatalf("host state targets = %v, want %v", gotTargets, wantTargets)
			}
			for _, mount := range mounts {
				if mount.Ownership == HostState && mount.Access != ReadWrite {
					t.Fatalf("host state mount %s access = %s, want rw", mount.ID, mount.Access)
				}
			}
		})
	}
}

func TestBuildMountPlanIncludesCommonPolicy(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	in := testMountInput(t, root, "codex")
	in.Config.ProgrammingTools = []config.ToolConfig{
		{Name: "go", Enabled: true},
		{Name: "node", Enabled: true},
		{Name: "python", Enabled: true},
	}
	if err := EnsureDirectories(in); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(in.WorkspaceDir, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(in.HomeDir, ".gitconfig"), []byte("[user]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(in.CooperDir, "ca", "cooper-ca.pem"),
		filepath.Join(in.CooperDir, "live", "socat-rules.json"),
		filepath.Join(in.CooperDir, "tokens", in.RuntimeID),
		filepath.Join(in.CooperDir, "session", in.RuntimeID, "cooper-localtime"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(in.CooperDir, "base", "shims"), 0o755); err != nil {
		t.Fatal(err)
	}

	mounts, err := BuildMountPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{
		"workspace", "git-hooks", "codex-state", "git-config",
		"go-mod-cache", "go-build-cache", "npm-cache", "pip-cache",
		"ca", "clipboard-token", "clipboard-shims", "live-config",
		"fonts", "playwright", "tmp", "session", "timezone",
	}
	for _, id := range wantIDs {
		if !hasMountID(mounts, id) {
			t.Errorf("mount plan does not contain %q", id)
		}
	}
	hooks := mountByID(t, mounts, "git-hooks")
	if hooks.Access != ReadOnly {
		t.Errorf("git hooks access = %s, want ro", hooks.Access)
	}
	if mountByID(t, mounts, "workspace").Target != in.WorkspaceDir {
		t.Error("workspace target does not keep its absolute host path")
	}
	if mountByID(t, mounts, "timezone").Target != TimezoneContainerPath {
		t.Errorf("timezone target = %q, want %q", mountByID(t, mounts, "timezone").Target, TimezoneContainerPath)
	}
}

func TestEnsureDirectoriesCreatesProtectedGitHooks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	in := testMountInput(t, root, "codex")
	gitDir := filepath.Join(in.WorkspaceDir, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := EnsureDirectories(in); err != nil {
		t.Fatal(err)
	}
	hooks := filepath.Join(gitDir, "hooks")
	info, err := os.Lstat(hooks)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("protected Git hooks path = %#v, %v", info, err)
	}
	mounts, err := BuildMountPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	hooksMount := mountByID(t, mounts, "git-hooks")
	if hooksMount.Source != hooks || hooksMount.Target != hooks || hooksMount.Access != ReadOnly {
		t.Fatalf("Git hooks mount = %#v", hooksMount)
	}
}

func TestGitHooksProtectionRejectsSymbolicLinks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		hooksLink bool
	}{
		{name: "Git directory"},
		{name: "hooks directory", hooksLink: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			in := testMountInput(t, root, "codex")
			target := filepath.Join(root, "outside-git")
			link := filepath.Join(in.WorkspaceDir, ".git")
			if test.hooksLink {
				if err := os.Mkdir(link, 0o755); err != nil {
					t.Fatal(err)
				}
				target = filepath.Join(root, "outside-hooks")
				link = filepath.Join(link, "hooks")
			}
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			if err := EnsureDirectories(in); err == nil || !strings.Contains(err.Error(), "symbolic link") {
				t.Fatalf("EnsureDirectories() error = %v, want symbolic-link error", err)
			}
			if _, err := BuildMountPlan(in); err == nil || !strings.Contains(err.Error(), "symbolic link") {
				t.Fatalf("BuildMountPlan() error = %v, want symbolic-link error", err)
			}
		})
	}
}

func TestEnsureDirectoriesDoesNotCreateGitMetadata(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	in := testMountInput(t, root, "codex")
	if err := EnsureDirectories(in); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(in.WorkspaceDir, ".git")); !os.IsNotExist(err) {
		t.Fatalf("non-Git workspace gained .git metadata: %v", err)
	}
}

func TestBuildMountPlanRejectsDirectAndResolvedStateOverlap(t *testing.T) {
	t.Parallel()

	t.Run("direct", func(t *testing.T) {
		root := t.TempDir()
		in := testMountInput(t, root, "grok")
		in.GrokStateRoot = filepath.Join(in.CooperDir, "grok")
		_, err := BuildMountPlan(in)
		if err == nil || !strings.Contains(err.Error(), "overlaps") {
			t.Fatalf("BuildMountPlan() error = %v, want overlap error", err)
		}
	})

	t.Run("state parent", func(t *testing.T) {
		root := t.TempDir()
		in := testMountInput(t, root, "grok")
		in.GrokStateRoot = root
		_, err := BuildMountPlan(in)
		if err == nil || !strings.Contains(err.Error(), "overlaps") {
			t.Fatalf("BuildMountPlan() error = %v, want overlap error", err)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		root := t.TempDir()
		in := testMountInput(t, root, "grok")
		outside := filepath.Join(root, "outside")
		if err := os.MkdirAll(outside, 0o755); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(outside, "state-link")
		if err := os.Symlink(in.CooperDir, link); err != nil {
			t.Fatal(err)
		}
		in.GrokStateRoot = filepath.Join(link, "future")
		_, err := BuildMountPlan(in)
		if err == nil || !strings.Contains(err.Error(), "overlaps") {
			t.Fatalf("BuildMountPlan() error = %v, want resolved overlap error", err)
		}
	})
}

func TestValidateMountPlanRejectsDuplicateTarget(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sourceOne := filepath.Join(root, "one")
	sourceTwo := filepath.Join(root, "two")
	if err := os.MkdirAll(sourceOne, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sourceTwo, 0o755); err != nil {
		t.Fatal(err)
	}
	mounts := []MountSpec{
		{ID: "one", Source: sourceOne, Target: "/target", Access: ReadWrite, Kind: Directory, Ownership: CooperCache},
		{ID: "two", Source: sourceTwo, Target: "/target", Access: ReadWrite, Kind: Directory, Ownership: CooperCache},
	}
	err := ValidateMountPlan(mounts, filepath.Join(root, "cooper"))
	if err == nil || !strings.Contains(err.Error(), "use target") {
		t.Fatalf("ValidateMountPlan() error = %v, want duplicate target error", err)
	}
}

func TestValidateMountPlanRejectsUnapprovedTargetOverlap(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cooperDir := filepath.Join(root, "cooper")
	parent := filepath.Join(root, "parent")
	child := filepath.Join(root, "child")
	for _, directory := range []string{cooperDir, parent, child} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mounts := []MountSpec{
		{ID: "parent", Source: parent, Target: "/target", Access: ReadWrite, Kind: Directory, Ownership: CooperCache},
		{ID: "child", Source: child, Target: "/target/child", Access: ReadOnly, Kind: Directory, Ownership: CooperCache},
	}
	if err := ValidateMountPlan(mounts, cooperDir); err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("ValidateMountPlan() error = %v, want target overlap error", err)
	}
}

func TestValidateMountPlanAllowsReviewedTargetOverlays(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cooperDir := filepath.Join(root, "cooper")
	for _, directory := range []string{cooperDir, filepath.Join(root, "tmp"), filepath.Join(root, "workspace"), filepath.Join(root, "hooks")} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mounts := []MountSpec{
		{ID: "tmp", Source: filepath.Join(root, "tmp"), Target: "/tmp", Access: ReadWrite, Kind: Directory, Ownership: CooperRuntime},
		{ID: "workspace", Source: filepath.Join(root, "workspace"), Target: "/tmp/workspace", Access: ReadWrite, Kind: Directory, Ownership: HostWorkspace},
		{ID: "git-hooks", Source: filepath.Join(root, "hooks"), Target: "/tmp/workspace/.git/hooks", Access: ReadOnly, Kind: Directory, Ownership: HostWorkspace},
	}
	if err := ValidateMountPlan(mounts, cooperDir); err != nil {
		t.Fatalf("ValidateMountPlan() error = %v", err)
	}
}

func TestBuildMountPlanRejectsWorkspaceAndCooperOverlap(t *testing.T) {
	t.Parallel()

	t.Run("workspace contains Cooper", func(t *testing.T) {
		root := t.TempDir()
		in := testMountInput(t, root, "codex")
		in.WorkspaceDir = root
		if err := EnsureDirectories(in); err == nil || !strings.Contains(err.Error(), "overlaps") {
			t.Fatalf("EnsureDirectories() error = %v, want overlap error", err)
		}
	})

	t.Run("workspace below Cooper", func(t *testing.T) {
		root := t.TempDir()
		in := testMountInput(t, root, "codex")
		in.WorkspaceDir = filepath.Join(in.CooperDir, "workspace")
		if err := os.MkdirAll(in.WorkspaceDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := EnsureDirectories(in); err == nil || !strings.Contains(err.Error(), "overlaps") {
			t.Fatalf("EnsureDirectories() error = %v, want overlap error", err)
		}
	})

	t.Run("resolved workspace contains Cooper", func(t *testing.T) {
		root := t.TempDir()
		in := testMountInput(t, root, "codex")
		link := filepath.Join(root, "workspace-link")
		if err := os.Symlink(root, link); err != nil {
			t.Fatal(err)
		}
		in.WorkspaceDir = link
		if err := EnsureDirectories(in); err == nil || !strings.Contains(err.Error(), "overlaps") {
			t.Fatalf("EnsureDirectories() error = %v, want resolved overlap error", err)
		}
	})
}

func TestEnsureDirectoriesRejectsStateOverlapBeforeCreation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	in := testMountInput(t, root, "grok")
	unsafeState := filepath.Join(in.CooperDir, "future-grok-state")
	in.GrokStateRoot = unsafeState
	if err := EnsureDirectories(in); err == nil || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("EnsureDirectories() error = %v, want overlap error", err)
	}
	if _, err := os.Stat(unsafeState); !os.IsNotExist(err) {
		t.Fatalf("unsafe state path was created: %v", err)
	}
}

func TestValidateMountPlanRejectsWrongSourceKindAndUncleanPath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	cooperDir := filepath.Join(root, "cooper")
	if err := os.MkdirAll(cooperDir, 0o700); err != nil {
		t.Fatal(err)
	}
	wrongKind := []MountSpec{{ID: "wrong", Source: file, Target: "/target", Access: ReadOnly, Kind: Directory, Ownership: HostConfig}}
	if err := ValidateMountPlan(wrongKind, cooperDir); err == nil || !strings.Contains(err.Error(), "path kind") {
		t.Fatalf("ValidateMountPlan() wrong-kind error = %v", err)
	}
	unclean := []MountSpec{{ID: "unclean", Source: file, Target: "/target/../other", Access: ReadOnly, Kind: File, Ownership: HostConfig}}
	if err := ValidateMountPlan(unclean, cooperDir); err == nil || !strings.Contains(err.Error(), "clean") {
		t.Fatalf("ValidateMountPlan() unclean-path error = %v", err)
	}
}

func TestMountPlanSupportsSpecialPathCharacters(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "space:colon-世界")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	in := testMountInput(t, root, "codex")
	if err := EnsureDirectories(in); err != nil {
		t.Fatal(err)
	}
	mounts, err := BuildMountPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	if got := mountByID(t, mounts, "workspace").Source; got != in.WorkspaceDir {
		t.Fatalf("workspace source = %q, want %q", got, in.WorkspaceDir)
	}
}

func TestBuildMountPlanOrdersParentBeforeWorkspaceAndHooks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	in := testMountInput(t, root, "codex")
	if err := os.MkdirAll(filepath.Join(in.WorkspaceDir, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDirectories(in); err != nil {
		t.Fatal(err)
	}
	mounts, err := BuildMountPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	indices := make(map[string]int)
	for index, mount := range mounts {
		indices[mount.ID] = index
	}
	if indices["tmp"] >= indices["workspace"] || indices["workspace"] >= indices["git-hooks"] {
		t.Fatalf("nested mount order = tmp:%d workspace:%d hooks:%d", indices["tmp"], indices["workspace"], indices["git-hooks"])
	}
}

func TestMountPlanDigestChangesWithAuthorization(t *testing.T) {
	t.Parallel()
	first := []MountSpec{{ID: "workspace", Source: "/work/one", Target: "/work/one", Access: ReadWrite, Kind: Directory, Ownership: HostWorkspace}}
	second := append([]MountSpec(nil), first...)
	second[0].Source = "/work/two"
	firstDigest, err := MountPlanDigest(first)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := MountPlanDigest(first)
	secondDigest, _ := MountPlanDigest(second)
	if firstDigest != again || firstDigest == secondDigest || len(firstDigest) != 64 {
		t.Fatalf("mount plan digests = %q, %q, %q", firstDigest, again, secondDigest)
	}
}

func testMountInput(t *testing.T, root, tool string) MountInput {
	t.Helper()
	home := filepath.Join(root, "home")
	workspace := filepath.Join(root, "workspace")
	cooperDir := filepath.Join(root, "cooper")
	for _, dir := range []string{home, workspace, cooperDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return MountInput{
		WorkspaceDir:  workspace,
		HomeDir:       home,
		CooperDir:     cooperDir,
		RuntimeID:     "test-runtime",
		ToolName:      tool,
		GrokStateRoot: filepath.Join(root, "grok-state"),
		Config:        config.DefaultConfig(),
	}
}

func targetsWithOwnership(mounts []MountSpec, ownership Ownership) []string {
	var targets []string
	for _, mount := range mounts {
		if mount.Ownership == ownership {
			targets = append(targets, mount.Target)
		}
	}
	return targets
}

func hasMountID(mounts []MountSpec, id string) bool {
	for _, mount := range mounts {
		if mount.ID == id {
			return true
		}
	}
	return false
}

func mountByID(t *testing.T, mounts []MountSpec, id string) MountSpec {
	t.Helper()
	for _, mount := range mounts {
		if mount.ID == id {
			return mount
		}
	}
	t.Fatalf("mount %q not found", id)
	return MountSpec{}
}
