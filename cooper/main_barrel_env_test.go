package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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

func requireZoneinfoFile(t *testing.T, rel string) string {
	t.Helper()
	for _, candidate := range []string{
		filepath.Join("/usr/share/zoneinfo", rel),
		filepath.Join("/var/db/timezone/zoneinfo", rel),
	} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	t.Skipf("zoneinfo file %q not found on host", rel)
	return ""
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

func TestRunCLIForwardsTerminalSessionEnv(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("COLORTERM", "truecolor")
	t.Setenv("TERM_PROGRAM", "WezTerm")
	t.Setenv("TERM_PROGRAM_VERSION", "20240203")
	t.Setenv("COLORFGBG", "15;0")
	t.Setenv("LC_TERMINAL", "iTerm2")
	t.Setenv("LC_TERMINAL_VERSION", "3.5")
	t.Setenv("TERM_SESSION_ID", "w0t0p0")
	t.Setenv("WEZTERM_PANE", "8")
	t.Setenv("KITTY_WINDOW_ID", "3")
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "3")
	t.Setenv("FORCE_HYPERLINK", "1")
	t.Setenv("CLICOLOR", "1")
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("NODE_DISABLE_COLORS", "1")

	_, _ = setupCLIBarrelEnvTest(t, nil)

	cliOneShot = `printf '%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|' "${TERM-}" "${COLORTERM-}" "${TERM_PROGRAM-}" "${TERM_PROGRAM_VERSION-}" "${COLORFGBG-}" "${LC_TERMINAL-}" "${LC_TERMINAL_VERSION-}" "${TERM_SESSION_ID-}" "${WEZTERM_PANE-}" "${KITTY_WINDOW_ID-}"; if [[ -v NO_COLOR ]]; then printf 'set:%s' "$NO_COLOR"; else printf unset; fi; printf '|%s|%s|%s|%s|%s' "${FORCE_COLOR-}" "${FORCE_HYPERLINK-}" "${CLICOLOR-}" "${CLICOLOR_FORCE-}" "${NODE_DISABLE_COLORS-}"`
	stdout, _, err := captureCommandIO(t, "", func() error { return runCLI(nil, []string{"claude"}) })
	if err != nil {
		t.Fatalf("runCLI() failed: %v", err)
	}
	const expected = "xterm-256color|truecolor|WezTerm|20240203|15;0|iTerm2|3.5|w0t0p0|8|3|set:|3|1|1|1|1"
	if got := stripTerminalTitleEscapes(stdout); got != expected {
		t.Fatalf("stdout = %q, want %q", got, expected)
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

func TestRunCLISessionTimezoneFollowsSyncedHostTimezoneOnReuse(t *testing.T) {
	_, workspaceDir := setupCLIBarrelEnvTest(t, nil)
	tokyoPath := requireZoneinfoFile(t, filepath.Join("Asia", "Tokyo"))
	utcPath := requireZoneinfoFile(t, filepath.Join("Etc", "UTC"))
	barrelName := docker.BarrelContainerName(workspaceDir, "claude")

	restoreTokyo := docker.SetHostLocaltimePathForTesting(tokyoPath)
	t.Cleanup(restoreTokyo)
	cliOneShot = `printf '%s|%s' "${TZ-}" "$(date +%z)"`
	stdout, stderr, err := captureCommandIO(t, "", func() error { return runCLI(nil, []string{"claude"}) })
	if err != nil {
		t.Fatalf("first runCLI() failed: %v", err)
	}
	first := strings.TrimSpace(stripTerminalTitleEscapes(stdout))
	firstParts := strings.Split(first, "|")
	if len(firstParts) != 2 {
		t.Fatalf("first stdout = %q, want TZ|offset", first)
	}
	if !strings.HasPrefix(firstParts[0], ":"+docker.BarrelSessionContainerDir+"/cooper-cli-tz-") || !strings.HasSuffix(firstParts[0], ".tz") {
		t.Fatalf("first TZ = %q, want cooper session timezone file", firstParts[0])
	}
	if firstParts[1] != "+0900" {
		t.Fatalf("first offset = %q, want %q", firstParts[1], "+0900")
	}
	if !strings.Contains(stderr, "Starting barrel container "+barrelName) {
		t.Fatalf("expected initial barrel start message, got %q", stderr)
	}

	restoreTokyo()
	restoreUTC := docker.SetHostLocaltimePathForTesting(utcPath)
	t.Cleanup(restoreUTC)
	stdout, stderr, err = captureCommandIO(t, "", func() error { return runCLI(nil, []string{"claude"}) })
	if err != nil {
		t.Fatalf("second runCLI() failed: %v", err)
	}
	second := strings.TrimSpace(stripTerminalTitleEscapes(stdout))
	secondParts := strings.Split(second, "|")
	if len(secondParts) != 2 {
		t.Fatalf("second stdout = %q, want TZ|offset", second)
	}
	if !strings.HasPrefix(secondParts[0], ":"+docker.BarrelSessionContainerDir+"/cooper-cli-tz-") || !strings.HasSuffix(secondParts[0], ".tz") {
		t.Fatalf("second TZ = %q, want cooper session timezone file", secondParts[0])
	}
	if secondParts[0] == firstParts[0] {
		t.Fatalf("expected a fresh per-session timezone file, got same path %q", secondParts[0])
	}
	if secondParts[1] != "+0000" {
		t.Fatalf("second offset = %q, want %q", secondParts[1], "+0000")
	}
	if strings.Contains(stderr, "Starting barrel container "+barrelName) {
		t.Fatalf("expected barrel reuse, got stderr %q", stderr)
	}
}

func TestRunCLITimezoneCannotBeOverriddenByBadConfig(t *testing.T) {
	_, _ = setupCLIBarrelEnvTest(t, func(cfg *config.Config) {
		cfg.BarrelEnvVars = []config.BarrelEnvVar{{Name: "TZ", Value: "UTC"}}
	})
	tokyoPath := requireZoneinfoFile(t, filepath.Join("Asia", "Tokyo"))
	restore := docker.SetHostLocaltimePathForTesting(tokyoPath)
	t.Cleanup(restore)

	cliOneShot = `printf '%s|%s' "${TZ-}" "$(date +%z)"`
	stdout, stderr, err := captureCommandIO(t, "", func() error { return runCLI(nil, []string{"claude"}) })
	if err != nil {
		t.Fatalf("runCLI() failed: %v", err)
	}
	parts := strings.Split(strings.TrimSpace(stripTerminalTitleEscapes(stdout)), "|")
	if len(parts) != 2 {
		t.Fatalf("stdout = %q, want TZ|offset", stdout)
	}
	if !strings.HasPrefix(parts[0], ":"+docker.BarrelSessionContainerDir+"/cooper-cli-tz-") || !strings.HasSuffix(parts[0], ".tz") {
		t.Fatalf("TZ = %q, want cooper session timezone file", parts[0])
	}
	if parts[1] != "+0900" {
		t.Fatalf("offset = %q, want %q", parts[1], "+0900")
	}
	if !strings.Contains(stderr, `TZ`) {
		t.Fatalf("stderr missing TZ warning: %q", stderr)
	}
}

func TestStartBarrelUsesSyncedHostTimezoneAtContainerStart(t *testing.T) {
	driver := setupCommandDriver(t, nil)
	tokyoPath := requireZoneinfoFile(t, filepath.Join("Asia", "Tokyo"))
	restore := docker.SetHostLocaltimePathForTesting(tokyoPath)
	t.Cleanup(restore)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	if err := driver.Start(ctx); err != nil {
		t.Fatalf("start cooper runtime: %v", err)
	}

	barrel, err := driver.StartBarrel("claude")
	if err != nil {
		t.Fatalf("StartBarrel() failed: %v", err)
	}
	out, err := driver.ExecBarrel(barrel.Name, `printf '%s|%s' "${TZ-}" "$(date +%z)"`)
	if err != nil {
		t.Fatalf("ExecBarrel() failed: %v", err)
	}
	parts := strings.Split(strings.TrimSpace(out), "|")
	if len(parts) != 2 {
		t.Fatalf("output = %q, want TZ|offset", out)
	}
	if parts[0] != ":/etc/localtime" {
		t.Fatalf("TZ = %q, want %q", parts[0], ":/etc/localtime")
	}
	if parts[1] != "+0900" {
		t.Fatalf("offset = %q, want %q", parts[1], "+0900")
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
	entries, err := os.ReadDir(docker.BarrelSessionDir(driver.CooperDir(), barrelName))
	if err != nil {
		t.Fatalf("ReadDir() failed: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "cooper-cli-env-") && strings.HasSuffix(entry.Name(), ".sh") {
			t.Fatalf("unexpected leftover session env file: %s", entry.Name())
		}
		if strings.HasPrefix(entry.Name(), "cooper-cli-tz-") && strings.HasSuffix(entry.Name(), ".tz") {
			t.Fatalf("unexpected leftover session timezone file: %s", entry.Name())
		}
	}
}

func TestRunCLISessionMountIsReadOnlyAndTmpRemainsWritable(t *testing.T) {
	driver, workspaceDir := setupCLIBarrelEnvTest(t, nil)
	barrelName := docker.BarrelContainerName(workspaceDir, "claude")

	cliOneShot = `if touch '` + docker.BarrelSessionContainerDir + `/should-not-write' 2>/dev/null; then printf 'session-rw'; elif touch /tmp/cooper-tmp-write-check 2>/dev/null; then printf 'session-ro|tmp-rw'; else printf 'session-ro|tmp-blocked'; fi`
	stdout, stderr, err := captureCommandIO(t, "", func() error { return runCLI(nil, []string{"claude"}) })
	if err != nil {
		t.Fatalf("runCLI() failed: %v", err)
	}
	if got := strings.TrimSpace(stripTerminalTitleEscapes(stdout)); got != "session-ro|tmp-rw" {
		t.Fatalf("stdout = %q, want %q", got, "session-ro|tmp-rw")
	}
	if !strings.Contains(stderr, "Starting barrel container "+barrelName) {
		t.Fatalf("expected barrel startup message, got %q", stderr)
	}
	if _, err := os.Stat(filepath.Join(docker.BarrelSessionDir(driver.CooperDir(), barrelName), "should-not-write")); !os.IsNotExist(err) {
		t.Fatalf("expected no host session write artifact, stat err=%v", err)
	}
}

func TestRunCLIRecreatesLegacyBarrelWithoutSessionMount(t *testing.T) {
	driver, workspaceDir := setupCLIBarrelEnvTest(t, func(cfg *config.Config) {
		cfg.BarrelEnvVars = []config.BarrelEnvVar{{Name: "LEGACY_FIX", Value: "restored"}}
	})
	barrelName := docker.BarrelContainerName(workspaceDir, "claude")
	legacyTmpDir := docker.BarrelTmpDir(driver.CooperDir(), barrelName)
	if err := os.MkdirAll(legacyTmpDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(legacyTmpDir) failed: %v", err)
	}

	args := []string{
		"run", "-d",
		"--name", barrelName,
		"--network", docker.InternalNetworkName(),
		"-w", workspaceDir,
		"-v", fmt.Sprintf("%s:%s:rw", workspaceDir, workspaceDir),
		"-v", legacyTmpDir + ":/tmp:rw",
		docker.GetImageCLI("claude"),
		"sleep", "infinity",
	}
	if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
		t.Fatalf("start legacy barrel failed: %v\n%s", err, string(out))
	}
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", barrelName).Run()
	})

	hasSessionMount, err := docker.BarrelHasSessionMount(barrelName)
	if err != nil {
		t.Fatalf("BarrelHasSessionMount(before) failed: %v", err)
	}
	if hasSessionMount {
		t.Fatal("expected legacy barrel to be missing the session mount")
	}

	cliOneShot = `printf %s "$LEGACY_FIX"`
	stdout, stderr, err := captureCommandIO(t, "", func() error { return runCLI(nil, []string{"claude"}) })
	if err != nil {
		t.Fatalf("runCLI() failed: %v", err)
	}
	if got := strings.TrimSpace(stripTerminalTitleEscapes(stdout)); got != "restored" {
		t.Fatalf("stdout = %q, want %q", got, "restored")
	}
	if !strings.Contains(stderr, "Recreating legacy barrel container "+barrelName) {
		t.Fatalf("stderr missing legacy recreation message: %q", stderr)
	}
	hasSessionMount, err = docker.BarrelHasSessionMount(barrelName)
	if err != nil {
		t.Fatalf("BarrelHasSessionMount(after) failed: %v", err)
	}
	if !hasSessionMount {
		t.Fatal("expected recreated barrel to include the session mount")
	}
}
