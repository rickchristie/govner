package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/profiles"
)

func TestCommandHookRetiresOldCopiesBeforeLaunch(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("profiles require Linux")
	}
	previousNamespace, previousPrefix := docker.RuntimeNamespace(), docker.ImagePrefix()
	t.Cleanup(func() {
		docker.SetRuntimeNamespace(previousNamespace)
		docker.SetImagePrefix(previousPrefix)
	})
	for _, name := range []string{"cli", "vm", "up", "save", "load", "profiles", "vm restart", "profiles backup", "build", "configure", "update", "cleanup", "cli list", "vm list", "vm prepare", "vm doctor", "vm stop"} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			if err := os.Mkdir(filepath.Join(home, ".codex"), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
			t.Setenv("COOPER_CLI_TOOL", "")
			t.Setenv("COOPER_VM_CONTEXT", filepath.Join(home, "no-vm-context"))
			previous := configDir
			configDir = filepath.Join(home, ".cooper")
			t.Cleanup(func() { configDir = previous })
			store := filepath.Join(configDir, "profiles")
			if err := os.MkdirAll(store, 0700); err != nil {
				t.Fatal(err)
			}
			fixture, err := os.ReadFile("internal/profiles/testdata/old-copy-index.json")
			if err != nil {
				t.Fatal(err)
			}
			data := strings.ReplaceAll(string(fixture), "/old-home", home)
			if err := os.WriteFile(filepath.Join(store, "index.json"), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			parts := strings.Fields(name)
			command := &cobra.Command{Use: parts[0]}
			args := []string{"codex"}
			if len(parts) == 2 {
				args = parts[1:]
				if parts[0] == "profiles" {
					child := &cobra.Command{Use: parts[1]}
					command.AddCommand(child)
					command = child
				}
			}
			command.SetContext(t.Context())
			var notice bytes.Buffer
			command.SetErr(&notice)
			if err := rootCmd.PersistentPreRunE(command, args); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "build", "configure", "update", "cleanup", "cli list", "vm list", "vm prepare", "vm doctor", "vm stop":
				current, err := os.ReadFile(filepath.Join(store, "index.json"))
				if err != nil || string(current) != data || notice.Len() != 0 {
					t.Fatal("command changed old profiles outside launch or profile use")
				}
				return
			}
			if !strings.Contains(notice.String(), "profiles-copy-backup-") {
				t.Fatal("reset did not report its retained store")
			}
			if err := profiles.CheckReady(configDir); err != nil {
				t.Fatal(err)
			}
			// CLI and VM use this same selection before creating their mounts.
			selected, err := selectLaunchProfile(t.Context(), configDir, home, home, "codex", nil)
			if err != nil || selected.ID != "" || selected.Paths.Mounts[0].Source != filepath.Join(home, ".codex") {
				t.Fatalf("launch selection after reset: %+v %v", selected, err)
			}
			if err := removeCooperConfigDir(configDir); err == nil {
				t.Fatal("configuration cleanup removed the backup")
			}
			notice.Reset()
			if err := rootCmd.PersistentPreRunE(command, args); err != nil || notice.Len() != 0 {
				t.Fatalf("second launch repeated the reset: %v", err)
			}
		})
	}
}

func TestCommandHookCannotResetCopiesInsideAWorkload(t *testing.T) {
	t.Setenv("COOPER_CLI_TOOL", "codex")
	command := testCommand(t)
	command.Use = "cli"
	previous := configDir
	configDir = filepath.Join(t.TempDir(), ".cooper")
	t.Cleanup(func() { configDir = previous })
	store := filepath.Join(configDir, "profiles")
	if err := os.MkdirAll(store, 0700); err != nil {
		t.Fatal(err)
	}
	data := `{"schema":1,"profiles":[],"hosts":{}}`
	path := filepath.Join(store, "index.json")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := prepareProfileStore(command, []string{"codex"}); err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(path)
	if err != nil || string(current) != data {
		t.Fatal("workload changed the host profile store")
	}
}
