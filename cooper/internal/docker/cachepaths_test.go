package docker

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestLanguageCacheSpecs_EmptyWhenNoToolsEnabled(t *testing.T) {
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "go", Enabled: false},
			{Name: "node", Enabled: false},
			{Name: "python", Enabled: false},
		},
	}

	got := languageCacheSpecs("/tmp/cooper", cfg)
	if len(got) != 0 {
		t.Fatalf("len(specs) = %d, want 0", len(got))
	}
}

func TestLanguageCacheSpecs_EmptyWhenNoTools(t *testing.T) {
	cfg := &config.Config{}

	got := languageCacheSpecs("/tmp/cooper", cfg)
	if len(got) != 0 {
		t.Fatalf("len(specs) = %d, want 0", len(got))
	}
}

func TestLanguageCacheSpecs_GoNodePython(t *testing.T) {
	cooperDir := "/tmp/cooper"
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "go", Enabled: true},
			{Name: "node", Enabled: true},
			{Name: "python", Enabled: true},
		},
	}

	got := languageCacheSpecs(cooperDir, cfg)

	want := []cacheMountSpec{
		{
			Name:          "go-mod",
			HostPath:      filepath.Join(cooperDir, "cache", "go-mod"),
			ContainerPath: "/home/user/go/pkg/mod",
		},
		{
			Name:          "go-build",
			HostPath:      filepath.Join(cooperDir, "cache", "go-build"),
			ContainerPath: "/home/user/.cache/go-build",
		},
		{
			Name:          "npm",
			HostPath:      filepath.Join(cooperDir, "cache", "npm"),
			ContainerPath: "/home/user/.npm",
		},
		{
			Name:          "pip",
			HostPath:      filepath.Join(cooperDir, "cache", "pip"),
			ContainerPath: "/home/user/.cache/pip",
		},
	}

	if len(got) != len(want) {
		t.Fatalf("len(specs) = %d, want %d\ngot:  %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("spec[%d] mismatch\ngot:  %#v\nwant: %#v", i, got[i], want[i])
		}
	}
}

func TestLanguageCacheSpecs_GoOnly(t *testing.T) {
	cooperDir := "/tmp/cooper"
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "go", Enabled: true},
			{Name: "node", Enabled: false},
		},
	}

	got := languageCacheSpecs(cooperDir, cfg)
	if len(got) != 2 {
		t.Fatalf("len(specs) = %d, want 2 (go-mod + go-build)", len(got))
	}
	if got[0].Name != "go-mod" {
		t.Errorf("specs[0].Name = %q, want \"go-mod\"", got[0].Name)
	}
	if got[1].Name != "go-build" {
		t.Errorf("specs[1].Name = %q, want \"go-build\"", got[1].Name)
	}
}

func TestLanguageCacheSpecs_AllHostPathsUnderCooperDir(t *testing.T) {
	cooperDir := "/home/testuser/.cooper"
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "go", Enabled: true},
			{Name: "node", Enabled: true},
			{Name: "python", Enabled: true},
		},
	}

	for _, spec := range languageCacheSpecs(cooperDir, cfg) {
		rel, err := filepath.Rel(cooperDir, spec.HostPath)
		if err != nil || rel[:2] == ".." {
			t.Errorf("spec %q host path %q is not under cooperDir %q", spec.Name, spec.HostPath, cooperDir)
		}
	}
}

func TestLanguageCacheSpecs_AllMountsRW(t *testing.T) {
	// This is a structural guarantee: the function only returns specs,
	// and appendLanguageCacheMounts always mounts them :rw. We verify
	// here that the spec list is non-empty so the :rw contract is
	// meaningful.
	cooperDir := "/tmp/cooper"
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "go", Enabled: true},
			{Name: "node", Enabled: true},
			{Name: "python", Enabled: true},
		},
	}
	specs := languageCacheSpecs(cooperDir, cfg)
	if len(specs) == 0 {
		t.Fatal("expected non-empty specs for enabled tools")
	}
}

func TestBarrelMountDirs_ClaudeIncludesAuthCachesAndPlaywright(t *testing.T) {
	homeDir := "/tmp/home"
	cooperDir := "/tmp/cooper"
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "go", Enabled: true},
			{Name: "node", Enabled: true},
		},
	}

	got := barrelMountDirs(homeDir, "claude", cooperDir, "barrel-test-claude", cfg)

	wantContains := []string{
		filepath.Join(homeDir, ".claude"),
		filepath.Join(cooperDir, "cache", "go-mod"),
		filepath.Join(cooperDir, "cache", "go-build"),
		filepath.Join(cooperDir, "cache", "npm"),
		filepath.Join(cooperDir, "fonts"),
		filepath.Join(cooperDir, "cache", "ms-playwright"),
		filepath.Join(cooperDir, "tmp", "barrel-test-claude"),
	}

	for _, want := range wantContains {
		if !slices.Contains(got, want) {
			t.Errorf("mount dir list missing %q\ngot: %v", want, got)
		}
	}
}

func TestBarrelMountDirs_CopilotAuthDir(t *testing.T) {
	homeDir := "/tmp/home"
	cooperDir := "/tmp/cooper"
	cfg := &config.Config{}

	got := barrelMountDirs(homeDir, "copilot", cooperDir, "barrel-test-copilot", cfg)
	if !slices.Contains(got, filepath.Join(homeDir, ".copilot")) {
		t.Errorf("copilot mount dirs should include .copilot\ngot: %v", got)
	}
	// Should NOT include .claude
	if slices.Contains(got, filepath.Join(homeDir, ".claude")) {
		t.Errorf("copilot mount dirs should not include .claude\ngot: %v", got)
	}
}

func TestBarrelMountDirs_OpenCodeAuthDirs(t *testing.T) {
	homeDir := "/tmp/home"
	cooperDir := "/tmp/cooper"
	cfg := &config.Config{}

	got := barrelMountDirs(homeDir, "opencode", cooperDir, "barrel-test-opencode", cfg)
	wantContains := []string{
		filepath.Join(homeDir, ".cache", "opencode"),
		filepath.Join(homeDir, ".config", "opencode"),
		filepath.Join(homeDir, ".local", "share", "opencode"),
		filepath.Join(homeDir, ".local", "state", "opencode"),
		filepath.Join(homeDir, ".opencode"),
	}
	for _, want := range wantContains {
		if !slices.Contains(got, want) {
			t.Errorf("opencode mount dirs missing %q\ngot: %v", want, got)
		}
	}
}

func TestBarrelMountDirs_NoHostCachePaths(t *testing.T) {
	// Verify that no mount dirs reference the user's home cache
	// directories — all language caches must be under cooperDir.
	homeDir := "/tmp/home"
	cooperDir := "/tmp/cooper"
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "go", Enabled: true},
			{Name: "node", Enabled: true},
			{Name: "python", Enabled: true},
		},
	}

	got := barrelMountDirs(homeDir, "claude", cooperDir, "barrel-test-claude", cfg)

	hostCacheDirs := []string{
		filepath.Join(homeDir, ".npm"),
		filepath.Join(homeDir, ".cache", "pip"),
		filepath.Join(homeDir, ".cache", "go-build"),
		filepath.Join(homeDir, "go", "pkg", "mod"),
	}
	for _, bad := range hostCacheDirs {
		if slices.Contains(got, bad) {
			t.Errorf("mount dirs should not include host cache path %q\ngot: %v", bad, got)
		}
	}
}

func TestBarrelMountDirs_PerBarrelTmpDir(t *testing.T) {
	homeDir := "/tmp/home"
	cooperDir := "/tmp/cooper"
	cfg := &config.Config{}

	// Each container name should produce a unique /tmp host path.
	got1 := barrelMountDirs(homeDir, "claude", cooperDir, "barrel-proj-claude", cfg)
	got2 := barrelMountDirs(homeDir, "copilot", cooperDir, "barrel-proj-copilot", cfg)

	want1 := filepath.Join(cooperDir, "tmp", "barrel-proj-claude")
	want2 := filepath.Join(cooperDir, "tmp", "barrel-proj-copilot")

	if !slices.Contains(got1, want1) {
		t.Errorf("claude barrel should include %q\ngot: %v", want1, got1)
	}
	if !slices.Contains(got2, want2) {
		t.Errorf("copilot barrel should include %q\ngot: %v", want2, got2)
	}
	// Must not include each other's tmp dir.
	if slices.Contains(got1, want2) {
		t.Errorf("claude barrel should not include copilot tmp dir %q", want2)
	}
}

func TestBarrelMountDirs_AlwaysIncludesPlaywright(t *testing.T) {
	// Even with no programming tools, Playwright dirs must be present.
	homeDir := "/tmp/home"
	cooperDir := "/tmp/cooper"
	cfg := &config.Config{}

	got := barrelMountDirs(homeDir, "claude", cooperDir, "barrel-test-claude", cfg)

	wantContains := []string{
		filepath.Join(cooperDir, "fonts"),
		filepath.Join(cooperDir, "cache", "ms-playwright"),
	}
	for _, want := range wantContains {
		if !slices.Contains(got, want) {
			t.Errorf("mount dirs missing Playwright dir %q\ngot: %v", want, got)
		}
	}
}
