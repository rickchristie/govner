//go:build linux

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/profileauth"
	"github.com/rickchristie/govner/cooper/internal/profiles"
	"github.com/rickchristie/govner/cooper/internal/usercontext"
)

func TestManagedProfileWorktreeHooksAreReadOnly(t *testing.T) {
	driver, workspace := setupCLIBarrelEnvTest(t, func(cfg *config.Config) {
		for index := range cfg.AITools {
			cfg.AITools[index].Enabled = cfg.AITools[index].Name == "codex"
		}
	})
	for _, name := range profileauth.CredentialNames("codex") {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(driver.HomeDir(), ".codex")
	t.Setenv("CODEX_HOME", root)
	project := filepath.Join(root, "worktrees", "project")
	if err := os.MkdirAll(filepath.Join(project, ".git", "hooks"), 0700); err != nil {
		t.Fatal(err)
	}
	for path, text := range map[string]string{
		filepath.Join(root, "auth.json"):                `{"auth_mode":"apikey","OPENAI_API_KEY":"fake-profile-key"}`,
		filepath.Join(project, ".git", "hooks", "kept"): "hook-ok",
	} {
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	account, err := usercontext.Current()
	if err != nil {
		t.Fatal(err)
	}
	account.Home = driver.HomeDir()
	service := profiles.New(profiles.Options{
		CooperDir: driver.CooperDir(), Workspace: workspace, Account: account,
		Environment: map[string]string{"CODEX_HOME": root}, CredentialNames: profileauth.CredentialNames, Reader: profileauth.Reader{},
		// The enclosing VM exposes /tmp. Only these fabricated roots bypass
		// that parent mount check; no developer state enters this fixture.
		Guard: profiles.GuardFunc(func(context.Context, []string) error { return nil }),
	})
	if _, err := service.Save(t.Context(), profiles.SaveRequest{Harness: "codex"}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Migrate(t.Context(), ""); err != nil {
		t.Fatal(err)
	}
	selection, err := service.Select(t.Context(), "codex", "Default")
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{project}
	for _, mount := range selection.Paths.Mounts {
		if mount.ID == "codex-state" {
			for _, alias := range mount.CanonicalPaths {
				paths = append(paths, filepath.Join(alias, "worktrees", "project"))
			}
		}
	}
	// Each launch must protect the public path and all recorded physical
	// paths. Writable workspace files rule out missing paths or denied users.
	const script = `set -eu
for project do
    test "$(cat "$project/.git/hooks/kept")" = hook-ok
    printf workspace-ok > "$project/writable"
    if touch "$project/.git/hooks/new" 2>/dev/null; then echo "created hook: $project"; exit 1; fi
    if (printf changed > "$project/.git/hooks/kept") 2>/dev/null; then echo "changed hook: $project"; exit 1; fi
    if mv "$project/.git/hooks/kept" "$project/.git/hooks/moved" 2>/dev/null; then echo "moved hook: $project"; exit 1; fi
    if rm "$project/.git/hooks/kept" 2>/dev/null; then echo "removed hook: $project"; exit 1; fi
done
`
	for _, launch := range []struct{ name, path string }{{"public", project}, {"canonical", canonical}} {
		for _, profile := range []struct{ name, id string }{{"host", ""}, {"named", selection.ID}} {
			t.Run(launch.name+"/"+profile.name, func(t *testing.T) {
				name := docker.BarrelContainerNameForProfile(launch.path, "codex", profile.id)
				if err := docker.StartBarrelWithProfile(driver.Config(), launch.path, driver.CooperDir(), driver.HomeDir(), "codex", profile.id); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := docker.StopBarrel(name); err != nil {
						t.Error(err)
					}
				})
				args := append([]string{"exec", name, "sh", "-c", script, "sh"}, paths...)
				if output, err := exec.CommandContext(t.Context(), "docker", args...).CombinedOutput(); err != nil {
					t.Fatalf("hook access: %v\n%s", err, output)
				}
			})
		}
	}
	for path, want := range map[string]string{
		filepath.Join(project, ".git", "hooks", "kept"): "hook-ok",
		filepath.Join(project, "writable"):              "workspace-ok",
	} {
		if data, err := os.ReadFile(path); err != nil || string(data) != want {
			t.Errorf("host file %s = %q, %v; want %q", path, data, err, want)
		}
	}
}
