package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/testdriver"
)

func setupCLIBarrelEnvTest(t *testing.T, mutator func(*config.Config)) (*testdriver.Driver, string) {
	t.Helper()
	driver := setupCommandDriver(t, mutator)
	withCommandGlobals(t, driver.CooperDir())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	if err := driver.Start(ctx); err != nil {
		t.Fatalf("start cooper runtime: %v", err)
	}

	workspaceDir := t.TempDir()
	prevDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(workspaceDir); err != nil {
		t.Fatalf("chdir(%q): %v", workspaceDir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prevDir); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})
	return driver, workspaceDir
}

func stripTerminalTitleEscapes(s string) string {
	for {
		start := strings.Index(s, "\033]0;")
		if start == -1 {
			return s
		}
		end := strings.Index(s[start:], "\a")
		if end == -1 {
			return s
		}
		s = s[:start] + s[start+end+1:]
	}
}

func updatePersistedBarrelEnvVars(t *testing.T, cooperDir string, vars []config.BarrelEnvVar) *config.Config {
	t.Helper()
	path := filepath.Join(cooperDir, "config.json")
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig(%q) failed: %v", path, err)
	}
	cfg.BarrelEnvVars = vars
	if err := config.SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig(%q) failed: %v", path, err)
	}
	return cfg
}

func TestRunCLIOneShotSeesConfiguredBarrelEnvValue(t *testing.T) {
	_, _ = setupCLIBarrelEnvTest(t, func(cfg *config.Config) {
		cfg.BarrelEnvVars = []config.BarrelEnvVar{{Name: "BARREL_TEST_VAR", Value: "cli-value"}}
	})

	cliOneShot = `printf %s "$BARREL_TEST_VAR"`
	stdout, _, err := captureCommandIO(t, "", func() error { return runCLI(nil, []string{"claude"}) })
	if err != nil {
		t.Fatalf("runCLI() failed: %v", err)
	}
	if got := stripTerminalTitleEscapes(stdout); got != "cli-value" {
		t.Fatalf("stdout = %q, want %q", got, "cli-value")
	}
}

func TestRunCLIBarrelEnvChangeAppliesNextSessionWithoutRestart(t *testing.T) {
	driver, workspaceDir := setupCLIBarrelEnvTest(t, func(cfg *config.Config) {
		cfg.BarrelEnvVars = []config.BarrelEnvVar{{Name: "MY_VAR", Value: "old"}}
	})

	cliOneShot = `printf %s "$MY_VAR"`
	stdout, _, err := captureCommandIO(t, "", func() error { return runCLI(nil, []string{"claude"}) })
	if err != nil {
		t.Fatalf("first runCLI() failed: %v", err)
	}
	if got := stripTerminalTitleEscapes(stdout); got != "old" {
		t.Fatalf("first stdout = %q, want %q", got, "old")
	}

	updatePersistedBarrelEnvVars(t, driver.CooperDir(), []config.BarrelEnvVar{{Name: "MY_VAR", Value: "new"}})
	stdout, stderr, err := captureCommandIO(t, "", func() error { return runCLI(nil, []string{"claude"}) })
	if err != nil {
		t.Fatalf("second runCLI() failed: %v", err)
	}
	if got := stripTerminalTitleEscapes(stdout); got != "new" {
		t.Fatalf("second stdout = %q, want %q", got, "new")
	}
	barrelName := docker.BarrelContainerName(workspaceDir, "claude")
	if strings.Contains(stderr, "Starting barrel container "+barrelName) {
		t.Fatalf("expected existing barrel reuse, got stderr %q", stderr)
	}
}

func TestRunCLIEmptyConfiguredValueIsSet(t *testing.T) {
	_, _ = setupCLIBarrelEnvTest(t, func(cfg *config.Config) {
		cfg.BarrelEnvVars = []config.BarrelEnvVar{{Name: "EMPTY_TEST", Value: ""}}
	})

	cliOneShot = `if [[ -v EMPTY_TEST ]]; then printf 'set:%s' "$EMPTY_TEST"; else printf 'unset'; fi`
	stdout, _, err := captureCommandIO(t, "", func() error { return runCLI(nil, []string{"claude"}) })
	if err != nil {
		t.Fatalf("runCLI() failed: %v", err)
	}
	if got := stripTerminalTitleEscapes(stdout); got != "set:" {
		t.Fatalf("stdout = %q, want %q", got, "set:")
	}
}

func TestRunCLISpecialCharactersRoundTripExactly(t *testing.T) {
	value := `a b '$PATH' \ tail = done`
	_, _ = setupCLIBarrelEnvTest(t, func(cfg *config.Config) {
		cfg.BarrelEnvVars = []config.BarrelEnvVar{{Name: "SPECIAL_TEST", Value: value}}
	})

	cliOneShot = `printf %s "$SPECIAL_TEST"`
	stdout, _, err := captureCommandIO(t, "", func() error { return runCLI(nil, []string{"claude"}) })
	if err != nil {
		t.Fatalf("runCLI() failed: %v", err)
	}
	if got := stripTerminalTitleEscapes(stdout); got != value {
		t.Fatalf("stdout = %q, want %q", got, value)
	}
}

func TestRunCLIWarningsDoNotBlockSessionAndProtectedValuesWin(t *testing.T) {
	_, _ = setupCLIBarrelEnvTest(t, func(cfg *config.Config) {
		cfg.BarrelEnvVars = []config.BarrelEnvVar{
			{Name: "GOOD", Value: "1"},
			{Name: "HTTP_PROXY", Value: "http://bad:9999"},
			{Name: "http_proxy", Value: "http://bad:9999"},
			{Name: "DISPLAY", Value: ":55"},
			{Name: "BAD-NAME", Value: "x"},
		}
	})

	cliOneShot = `printf '%s|%s|%s|%s' "$GOOD" "${HTTP_PROXY-}" "${http_proxy-}" "${DISPLAY-}"`
	stdout, stderr, err := captureCommandIO(t, "", func() error { return runCLI(nil, []string{"claude"}) })
	if err != nil {
		t.Fatalf("runCLI() failed: %v", err)
	}
	proxyCfg, _, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() failed: %v", err)
	}
	expected := "1|" + "http://" + docker.ProxyHost() + ":" + strconv.Itoa(proxyCfg.ProxyPort) + "||127.0.0.1:99"
	if got := stripTerminalTitleEscapes(stdout); got != expected {
		t.Fatalf("stdout = %q, want %q", got, expected)
	}
	if !strings.Contains(stderr, `HTTP_PROXY`) || !strings.Contains(stderr, `http_proxy`) || !strings.Contains(stderr, `DISPLAY`) || !strings.Contains(stderr, `BAD-NAME`) {
		t.Fatalf("stderr missing expected warnings: %q", stderr)
	}
}

func TestRunCLIProtectedTokenEnvWinsOverBadConfig(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-real")
	_, _ = setupCLIBarrelEnvTest(t, func(cfg *config.Config) {
		cfg.BarrelEnvVars = []config.BarrelEnvVar{{Name: "OPENAI_API_KEY", Value: "sk-bad"}}
	})

	cliOneShot = `printf %s "$OPENAI_API_KEY"`
	stdout, stderr, err := captureCommandIO(t, "", func() error { return runCLI(nil, []string{"codex"}) })
	if err != nil {
		t.Fatalf("runCLI(codex) failed: %v", err)
	}
	if got := stripTerminalTitleEscapes(stdout); got != "sk-real" {
		t.Fatalf("stdout = %q, want %q", got, "sk-real")
	}
	if !strings.Contains(stderr, `OPENAI_API_KEY`) {
		t.Fatalf("stderr missing OPENAI_API_KEY warning: %q", stderr)
	}
}

func TestRunCLIPathCannotBeOverriddenByBadConfig(t *testing.T) {
	_, _ = setupCLIBarrelEnvTest(t, func(cfg *config.Config) {
		cfg.BarrelEnvVars = []config.BarrelEnvVar{{Name: "PATH", Value: "/broken"}}
	})

	cliOneShot = `command -v bash >/dev/null && printf ok`
	stdout, stderr, err := captureCommandIO(t, "", func() error { return runCLI(nil, []string{"claude"}) })
	if err != nil {
		t.Fatalf("runCLI() failed: %v", err)
	}
	if !strings.Contains(stripTerminalTitleEscapes(stdout), "ok") {
		t.Fatalf("stdout = %q, want it to contain %q", stdout, "ok")
	}
	if !strings.Contains(stderr, `PATH`) {
		t.Fatalf("stderr missing PATH warning: %q", stderr)
	}
}

func TestRunCLISessionEnvFileIsCleanedUp(t *testing.T) {
	driver, workspaceDir := setupCLIBarrelEnvTest(t, func(cfg *config.Config) {
		cfg.BarrelEnvVars = []config.BarrelEnvVar{{Name: "FOO", Value: "1"}}
	})

	cliOneShot = `printf %s "$FOO"`
	if _, _, err := captureCommandIO(t, "", func() error { return runCLI(nil, []string{"claude"}) }); err != nil {
		t.Fatalf("runCLI() failed: %v", err)
	}

	barrelName := docker.BarrelContainerName(workspaceDir, "claude")
	entries, err := os.ReadDir(docker.BarrelTmpDir(driver.CooperDir(), barrelName))
	if err != nil {
		t.Fatalf("ReadDir() failed: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "cooper-cli-env-") && strings.HasSuffix(entry.Name(), ".sh") {
			t.Fatalf("unexpected leftover session env file: %s", entry.Name())
		}
	}
}
