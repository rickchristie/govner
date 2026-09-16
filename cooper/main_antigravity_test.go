package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/antigravity"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/testdocker"
	"github.com/rickchristie/govner/cooper/internal/vm"
)

func TestAntigravityCommandsReportSetupWithoutStartingRuntime(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("managed file authentication requires Linux")
	}
	driver := setupCommandDriver(t, nil)
	withCommandGlobals(t, driver.CooperDir())
	t.Setenv("SHELL", "/bin/false")
	for _, name := range []string{"GEMINI_API_KEY", "GOOGLE_GEMINI_BASE_URL", "AGY_ADC_AUTH", "GOOGLE_APPLICATION_CREDENTIALS", "CLOUDSDK_CONFIG", "GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION", "CLOUD_CODE_URL", "BAICODE_ENDPOINT_URL", "JETSKI_OAUTH_TOKEN"} {
		t.Setenv(name, "")
	}
	// A bus address selects the desktop setup check without opening a keyring.
	// Only this temporary home can supply native state or wrapper records.
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/cooper-test-absent-bus")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "agy"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if err := driver.Start(ctx); err != nil {
		t.Fatal(err)
	}

	const setupMessage = "Please run cooper build and then run agy to relogin\n"
	checkCommands := func(t *testing.T, wantStatus int, wantOutput string) {
		t.Helper()
		for _, mode := range []string{"cli", "vm"} {
			t.Run(mode, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHelperAntigravityLaunchCommand$", "--",
					"--config", driver.CooperDir(), "--prefix", testdocker.ImagePrefix,
					"--runtime-namespace", testdocker.RuntimeNamespace, mode, "antigravity")
				command.Dir = t.TempDir()
				command.Env = append(os.Environ(), "COOPER_TEST_ANTIGRAVITY_COMMAND=1")
				var stdout, stderr bytes.Buffer
				command.Stdout, command.Stderr = &stdout, &stderr
				err := command.Run()
				if strings.Contains(stdout.String()+stderr.String(), "never-print-this-secret") {
					t.Fatal("command output contains private state")
				}
				if command.ProcessState == nil {
					t.Fatalf("start command: %v", err)
				}
				if got := command.ProcessState.ExitCode(); got != wantStatus {
					t.Fatalf("exit status = %d, want %d: %v\nstdout: %s\nstderr: %s", got, wantStatus, err, &stdout, &stderr)
				}
				if wantStatus == 0 {
					if stdout.String() != wantOutput || stderr.Len() != 0 {
						t.Fatalf("setup output = %q, stderr = %q", &stdout, &stderr)
					}
					return
				}
				if stdout.Len() != 0 || !strings.Contains(stderr.String(), wantOutput) {
					t.Fatalf("error output = %q, stderr = %q", &stdout, &stderr)
				}
			})
		}
	}

	t.Run("keyring login needs wrapper", func(t *testing.T) {
		checkCommands(t, 0, setupMessage)
	})
	setup, err := antigravity.InstallHostFileAuth(t.Context(), driver.HomeDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(setup.Wrapper)+":"+os.Getenv("PATH"))
	t.Run("wrapper needs file login", func(t *testing.T) {
		checkCommands(t, 0, setupMessage)
	})

	stateDir := filepath.Join(driver.HomeDir(), ".gemini", "antigravity-cli")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	const invalidState = "{invalid-state-never-print-this-secret"
	for _, file := range []struct {
		name    string
		message string
	}{
		{"antigravity-oauth-token", "Error: Antigravity needs file authentication"},
		{"settings.json", "Error: Antigravity settings cannot be read"},
	} {
		path := filepath.Join(stateDir, file.name)
		if err := os.WriteFile(path, []byte(invalidState), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Run("invalid "+file.name, func(t *testing.T) {
			checkCommands(t, 1, file.message)
		})
		data, err := os.ReadFile(path)
		if err != nil || string(data) != invalidState {
			t.Fatalf("launch changed native state: %v", err)
		}
	}

	barrels, err := docker.ListBarrels()
	if err != nil || len(barrels) != 0 {
		t.Fatalf("setup started a barrel: %v, %v", barrels, err)
	}
	runtimes, err := vm.List(t.Context(), docker.RuntimeNamespace(), nil)
	if err != nil || len(runtimes) != 0 {
		t.Fatalf("setup started a VM: %v, %v", runtimes, err)
	}
}

// Exercise the real entry point and Cobra error output in a child process.
// The parent provides only test images, a test proxy, and fabricated state.
func TestHelperAntigravityLaunchCommand(t *testing.T) {
	if os.Getenv("COOPER_TEST_ANTIGRAVITY_COMMAND") != "1" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		t.Fatal("missing command arguments")
	}
	rootCmd.SetArgs(os.Args[separator+1:])
	main()
	os.Exit(0)
}
