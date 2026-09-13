package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/profileauth"
	"github.com/rickchristie/govner/cooper/internal/profilemanager"
	"github.com/rickchristie/govner/cooper/internal/profiles"
	"github.com/rickchristie/govner/cooper/internal/usercontext"
)

func TestRunCLINamedProfilePersistsStateAndSeparatesHost(t *testing.T) {
	driver, workspace := setupCLIBarrelEnvTest(t, func(cfg *config.Config) {
		for index := range cfg.AITools {
			cfg.AITools[index].Enabled = cfg.AITools[index].Name == "codex"
		}
	})
	home := driver.HomeDir()
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("OPENAI_API_KEY", "fake-live-host-key")
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex/auth.json"), []byte(`{"auth_mode":"apikey","OPENAI_API_KEY":"fake-profile-key"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex/session"), []byte("saved-session"), 0600); err != nil {
		t.Fatal(err)
	}
	account, err := usercontext.Current()
	if err != nil {
		t.Fatal(err)
	}
	account.Home = home
	// The fixture bypasses the physical-host command check, but uses the real
	// account reader and complete-root service. The enclosing development VM
	// mounts all of /tmp, so isolated fixture saves use a test use guard.
	service := profiles.New(profiles.Options{CooperDir: driver.CooperDir(), Workspace: workspace, Account: account,
		Environment: map[string]string{"CODEX_HOME": filepath.Join(home, ".codex")}, CredentialNames: profileauth.CredentialNames,
		Reader: profileauth.Reader{}, Guard: profiles.GuardFunc(func(context.Context, []string) error { return nil })})
	if _, err := service.Save(t.Context(), profiles.SaveRequest{Harness: "codex"}); err != nil {
		t.Fatal(err)
	}
	selection, err := service.Select(t.Context(), "codex", "Default")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex/session"), []byte("host-session"), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		stdout, _, err := captureCommandIO(t, "", func() error { return runCLI(testCommand(t), args) })
		if err != nil {
			t.Fatal(err)
		}
		return stripTerminalTitleEscapes(stdout)
	}
	cliOneShot = `printf '%s|%s' "$(cat "$CODEX_HOME/session")" "${OPENAI_API_KEY-unset}"; printf runtime-session > "$CODEX_HOME/session"`
	if output := run("codex", "Default"); output != "saved-session|unset" {
		t.Fatalf("named session: %q", output)
	}
	name := docker.BarrelContainerNameForProfile(workspace, "codex", selection.ID)
	before, err := exec.Command("docker", "inspect", "--format", "{{.Id}}", name).Output()
	if err != nil {
		t.Fatal(err)
	}
	cliOneShot = `cat "$CODEX_HOME/session"`
	if output := run("codex", "default"); output != "runtime-session" {
		t.Fatalf("profile state was not retained: %q", output)
	}
	after, err := exec.Command("docker", "inspect", "--format", "{{.Id}}", name).Output()
	if err != nil || string(after) != string(before) {
		t.Fatal("unchanged named profile did not reuse its container")
	}
	if output := run("codex"); output != "host-session" {
		t.Fatalf("ordinary CLI did not use live host: %q", output)
	}
	err = (profilemanager.HostUsage{}).Check(t.Context(), []string{selection.Paths.Mounts[0].Source})
	var issue *profiles.Issue
	if !errors.As(err, &issue) || issue.Kind != profiles.StateInUse {
		t.Fatalf("idle mounts did not block replacement: %v", err)
	}
	infos, err := docker.ListBarrels()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, info := range infos {
		if info.Name == name && info.ProfileID == selection.ID && info.ProfileName == "Default" {
			found = true
		}
	}
	if !found {
		t.Fatal("runtime list lost profile identity")
	}
	if err := docker.StopBarrel(docker.BarrelContainerName(workspace, "codex")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex/auth.json"), []byte(`{"auth_mode":"apikey","OPENAI_API_KEY":"fake-work-key"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex/session"), []byte("work-session"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Save(t.Context(), profiles.SaveRequest{Harness: "codex", NewName: "Work"}); err != nil {
		t.Fatal(err)
	}
	if output := run("codex", "Work"); output != "work-session" {
		t.Fatalf("second named profile: %q", output)
	}
	if output := run("codex", "Default"); output != "runtime-session" {
		t.Fatalf("two profiles shared a runtime: %q", output)
	}
	// Normal cleanup must leave account state intact, including its session.
	if err := docker.CleanupRuntime(); err != nil {
		t.Fatal(err)
	}
	selected, err := service.Select(t.Context(), "codex", "Default")
	if err != nil {
		t.Fatal(err)
	}
	for _, mount := range selected.Paths.Mounts {
		if mount.ID != "codex-state" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(mount.Source, "session"))
		if err != nil || string(data) != "runtime-session" {
			t.Fatal("runtime cleanup removed profile data")
		}
	}
}

func TestConfigurationRemovalPreservesProfileData(t *testing.T) {
	cooperDir := t.TempDir()
	path := filepath.Join(cooperDir, "profiles", "fixture-session")
	if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := removeCooperConfigDir(cooperDir); err == nil || !strings.Contains(err.Error(), "profile data") {
		t.Fatalf("cleanup: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "keep" {
		t.Fatal("configuration cleanup removed account data")
	}
}

func TestProfileCommandArgumentsDoNotAcceptNamedSaveDestination(t *testing.T) {
	command := newSaveCommand()
	if err := command.Args(command, []string{"codex", "Default"}); err == nil {
		t.Fatal("named save destination was accepted")
	}
	if err := command.Args(command, []string{"codex"}); err != nil {
		t.Fatal(err)
	}
	command = newLoadCommand()
	if err := command.Args(command, []string{"Work"}); err == nil {
		t.Fatal("load accepted an ambiguous harness")
	}
}
