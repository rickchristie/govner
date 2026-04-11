package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/clipboard"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/testdocker"
	"github.com/rickchristie/govner/cooper/internal/testdriver"
)

func setupCommandDriver(t *testing.T, mutator func(*config.Config)) *testdriver.Driver {
	t.Helper()

	lock, err := testdocker.SetupPackageNamed("main", true)
	if err != nil {
		t.Fatalf("setup docker-backed command test: %v", err)
	}
	t.Cleanup(func() {
		if err := lock.Release(); err != nil {
			t.Errorf("release docker test lock: %v", err)
		}
	})

	driver, err := testdriver.New(testdriver.Options{
		ImagePrefix:          testdocker.ImagePrefix,
		DisableHostClipboard: true,
		ConfigMutator:        mutator,
	})
	if err != nil {
		t.Fatalf("create command test driver: %v", err)
	}
	t.Cleanup(func() {
		if err := driver.Close(); err != nil {
			t.Errorf("close command test driver: %v", err)
		}
	})
	return driver
}

func withCommandGlobals(t *testing.T, cooperDir string) {
	t.Helper()

	prevConfigDir := configDir
	prevImagePrefix := imagePrefix
	prevCliOneShot := cliOneShot

	configDir = cooperDir
	imagePrefix = testdocker.ImagePrefix
	cliOneShot = ""
	docker.SetImagePrefix(imagePrefix)
	docker.SetRuntimeNamespace(testdocker.RuntimeNamespace)
	docker.SetStopTimeoutSeconds(testdocker.TestStopTimeoutSeconds)

	t.Cleanup(func() {
		configDir = prevConfigDir
		imagePrefix = prevImagePrefix
		cliOneShot = prevCliOneShot
		docker.SetImagePrefix(prevImagePrefix)
	})
}

func captureCommandIO(t *testing.T, stdin string, fn func() error) (string, string, error) {
	t.Helper()

	origStdout := os.Stdout
	origStderr := os.Stderr
	origStdin := os.Stdin

	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}

	var stdinR *os.File
	if stdin != "" {
		stdinPipeR, stdinPipeW, pipeErr := os.Pipe()
		if pipeErr != nil {
			t.Fatalf("create stdin pipe: %v", pipeErr)
		}
		if _, pipeErr := io.WriteString(stdinPipeW, stdin); pipeErr != nil {
			t.Fatalf("write stdin pipe: %v", pipeErr)
		}
		_ = stdinPipeW.Close()
		stdinR = stdinPipeR
		os.Stdin = stdinR
	}

	os.Stdout = stdoutW
	os.Stderr = stderrW

	runErr := fn()

	_ = stdoutW.Close()
	_ = stderrW.Close()
	if stdinR != nil {
		_ = stdinR.Close()
	}

	stdoutBytes, readStdoutErr := io.ReadAll(stdoutR)
	if readStdoutErr != nil {
		t.Fatalf("read captured stdout: %v", readStdoutErr)
	}
	stderrBytes, readStderrErr := io.ReadAll(stderrR)
	if readStderrErr != nil {
		t.Fatalf("read captured stderr: %v", readStderrErr)
	}

	_ = stdoutR.Close()
	_ = stderrR.Close()

	os.Stdout = origStdout
	os.Stderr = origStderr
	os.Stdin = origStdin

	return string(stdoutBytes), string(stderrBytes), runErr
}

func TestCollectUpdatePlan(t *testing.T) {
	t.Run("programming mirror mismatch rebuilds base", func(t *testing.T) {
		cfg := &config.Config{
			ProgrammingTools: []config.ToolConfig{
				{Name: "go", Enabled: true, Mode: config.ModeMirror, ContainerVersion: "0.0.0"},
			},
		}

		var out bytes.Buffer
		plan := collectUpdatePlan(
			cfg,
			func(name string) (string, error) { return "", fmt.Errorf("unexpected latest lookup for %s", name) },
			func(name string) (string, error) { return "1.24.10", nil },
			&out,
		)

		if !plan.baseChanged {
			t.Fatal("expected baseChanged for programming tool mirror mismatch")
		}
		if len(plan.toolsChanged) != 0 {
			t.Fatalf("expected no AI tool rebuilds, got %+v", plan.toolsChanged)
		}
		if got := cfg.ProgrammingTools[0].HostVersion; got != "1.24.10" {
			t.Fatalf("HostVersion = %q, want 1.24.10", got)
		}
		if !strings.Contains(out.String(), "container=0.0.0, host=1.24.10") {
			t.Fatalf("expected mismatch message in output, got %q", out.String())
		}
	})

	t.Run("ai latest mismatch rebuilds only changed tool", func(t *testing.T) {
		cfg := &config.Config{
			AITools: []config.ToolConfig{
				{Name: "codex", Enabled: true, Mode: config.ModeLatest, ContainerVersion: "0.1.0"},
				{Name: "claude", Enabled: true, Mode: config.ModeLatest, ContainerVersion: "2.1.87"},
			},
		}

		plan := collectUpdatePlan(
			cfg,
			func(name string) (string, error) {
				switch name {
				case "codex":
					return "0.2.0", nil
				case "claude":
					return "2.1.87", nil
				default:
					return "", fmt.Errorf("unexpected tool %s", name)
				}
			},
			func(name string) (string, error) { return "", fmt.Errorf("unexpected host lookup for %s", name) },
			nil,
		)

		if plan.baseChanged {
			t.Fatal("baseChanged should be false for AI-tool-only mismatch")
		}
		if !plan.toolsChanged["codex"] {
			t.Fatalf("expected codex rebuild, got %+v", plan.toolsChanged)
		}
		if plan.toolsChanged["claude"] {
			t.Fatalf("did not expect claude rebuild, got %+v", plan.toolsChanged)
		}
		if got := cfg.AITools[0].PinnedVersion; got != "0.2.0" {
			t.Fatalf("PinnedVersion = %q, want 0.2.0", got)
		}
	})

	t.Run("lookup errors warn and continue", func(t *testing.T) {
		cfg := &config.Config{
			ProgrammingTools: []config.ToolConfig{
				{Name: "go", Enabled: true, Mode: config.ModeLatest, ContainerVersion: "1.24.10"},
			},
		}

		var out bytes.Buffer
		plan := collectUpdatePlan(
			cfg,
			func(name string) (string, error) { return "", fmt.Errorf("boom") },
			func(name string) (string, error) { return "", fmt.Errorf("unexpected host lookup for %s", name) },
			&out,
		)

		if plan.baseChanged || len(plan.toolsChanged) != 0 {
			t.Fatalf("expected no changes on lookup failure, got %+v", plan)
		}
		if !strings.Contains(out.String(), "Warning: could not resolve latest go") {
			t.Fatalf("expected warning output, got %q", out.String())
		}
	})
}

func TestRunCLIList(t *testing.T) {
	driver := setupCommandDriver(t, nil)
	withCommandGlobals(t, driver.CooperDir())

	images, err := docker.ListCLIImages()
	if err != nil {
		t.Fatalf("ListCLIImages() failed: %v", err)
	}
	if len(images) == 0 {
		t.Fatal("expected at least one CLI image in test bootstrap")
	}

	stdout, stderr, err := captureCommandIO(t, "", func() error {
		return runCLI(nil, []string{"list"})
	})
	if err != nil {
		t.Fatalf("runCLI(list) failed: %v", err)
	}
	if stdout != "" {
		t.Fatalf("expected no stdout for cli list, got %q", stdout)
	}
	if !strings.Contains(stderr, "Available CLI tool images:") {
		t.Fatalf("expected cli list header, got %q", stderr)
	}
	for _, imageName := range images {
		tool := strings.TrimPrefix(imageName, docker.ImagePrefix()+"cooper-cli-")
		if !strings.Contains(stderr, tool) {
			t.Fatalf("expected tool %q in cli list output: %q", tool, stderr)
		}
	}
}

func TestRunCLIStartsBarrelForOneShot(t *testing.T) {
	driver := setupCommandDriver(t, nil)
	withCommandGlobals(t, driver.CooperDir())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := driver.Start(ctx); err != nil {
		t.Fatalf("start cooper runtime: %v", err)
	}

	workspaceDir := t.TempDir()
	prevDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(workspaceDir); err != nil {
		t.Fatalf("chdir to workspace: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prevDir); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})

	cliOneShot = "printf cli-ok"
	barrelName := docker.BarrelContainerName(workspaceDir, "claude")
	tokenPath := clipboard.TokenFilePath(driver.CooperDir(), barrelName)

	stdout, stderr, err := captureCommandIO(t, "", func() error {
		return runCLI(nil, []string{"claude"})
	})
	if err != nil {
		t.Fatalf("runCLI(claude -c) failed: %v", err)
	}
	if !strings.Contains(stdout, "cli-ok") {
		t.Fatalf("expected one-shot command output on stdout, got %q", stdout)
	}
	if !strings.Contains(stderr, "Starting barrel container "+barrelName) {
		t.Fatalf("expected barrel start message, got %q", stderr)
	}

	running, err := docker.IsBarrelRunning(barrelName)
	if err != nil {
		t.Fatalf("check barrel running: %v", err)
	}
	if !running {
		t.Fatalf("expected barrel %s to be running after runCLI", barrelName)
	}
	if _, err := os.Stat(tokenPath); err != nil {
		t.Fatalf("expected clipboard token file %s: %v", tokenPath, err)
	}
}

func TestRunCleanupRemovesRuntimeArtifacts(t *testing.T) {
	driver := setupCommandDriver(t, nil)
	withCommandGlobals(t, driver.CooperDir())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := driver.Start(ctx); err != nil {
		t.Fatalf("start cooper runtime: %v", err)
	}

	barrel, err := driver.StartBarrel("claude")
	if err != nil {
		t.Fatalf("start barrel: %v", err)
	}
	tokenPath := clipboard.TokenFilePath(driver.CooperDir(), barrel.Name)
	if _, err := os.Stat(tokenPath); err != nil {
		t.Fatalf("expected token file before cleanup: %v", err)
	}

	_, stderr, err := captureCommandIO(t, "n\n", func() error {
		return runCleanup(nil, nil)
	})
	if err != nil {
		t.Fatalf("runCleanup() failed: %v", err)
	}
	if !strings.Contains(stderr, "Cleanup complete.") {
		t.Fatalf("expected cleanup completion message, got %q", stderr)
	}

	if _, err := os.Stat(tokenPath); !os.IsNotExist(err) {
		t.Fatalf("expected token file %s to be removed, err=%v", tokenPath, err)
	}

	barrelRunning, err := docker.IsBarrelRunning(barrel.Name)
	if err != nil {
		t.Fatalf("check barrel after cleanup: %v", err)
	}
	if barrelRunning {
		t.Fatalf("barrel %s still running after cleanup", barrel.Name)
	}

	proxyRunning, err := docker.IsProxyRunning()
	if err != nil {
		t.Fatalf("check proxy after cleanup: %v", err)
	}
	if proxyRunning {
		t.Fatal("proxy still running after cleanup")
	}

	for _, imageName := range []string{
		docker.GetImageProxy(),
		docker.GetImageBase(),
		docker.GetImageCLI("claude"),
	} {
		exists, err := docker.ImageExists(imageName)
		if err != nil {
			t.Fatalf("check image %s after cleanup: %v", imageName, err)
		}
		if exists {
			t.Fatalf("image %s still exists after cleanup", imageName)
		}
	}

	for _, networkName := range []string{docker.InternalNetworkName(), docker.ExternalNetworkName()} {
		exists, err := docker.NetworkExists(networkName)
		if err != nil {
			t.Fatalf("check network %s after cleanup: %v", networkName, err)
		}
		if exists {
			t.Fatalf("network %s still exists after cleanup", networkName)
		}
	}
}

func TestRunUpdateNoRebuildNeeded(t *testing.T) {
	driver := setupCommandDriver(t, func(cfg *config.Config) {
		cfg.ProgrammingTools = nil
		cfg.AITools = nil
	})
	withCommandGlobals(t, driver.CooperDir())

	stdout, stderr, err := captureCommandIO(t, "", func() error {
		return runUpdate(nil, nil)
	})
	if err != nil {
		t.Fatalf("runUpdate() failed: %v", err)
	}
	if stdout != "" {
		t.Fatalf("expected no stdout for update, got %q", stdout)
	}
	if !strings.Contains(stderr, "All tool versions match. No rebuild needed.") {
		t.Fatalf("expected no-op update message, got %q", stderr)
	}

	cfgPath := filepath.Join(driver.CooperDir(), "config.json")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected config.json to remain after update: %v", err)
	}
}
