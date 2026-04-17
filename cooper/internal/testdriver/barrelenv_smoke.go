package testdriver

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/barrelenv"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/names"
)

// RunBarrelEnvSmoke starts the real runtime and verifies global barrel env vars
// are applied per session while protected runtime env remains Cooper-controlled.
func RunBarrelEnvSmoke(ctx context.Context, d *Driver) error {
	if err := d.RequireProxyImage(); err != nil {
		return err
	}
	if err := d.RequireBaseImage(); err != nil {
		return err
	}
	if err := d.Start(ctx); err != nil {
		return fmt.Errorf("start cooper runtime: %w", err)
	}

	barrel, err := d.StartBarrel("claude")
	if err != nil {
		return err
	}

	if err := verifyConfiguredBarrelEnvVisible(d, barrel); err != nil {
		return err
	}
	if err := verifyProtectedBarrelEnvRestore(d, barrel); err != nil {
		return err
	}
	if err := verifyBarrelEnvNextSessionReload(d, barrel); err != nil {
		return err
	}
	if err := verifySessionMountIsolation(d, barrel); err != nil {
		return err
	}
	return nil
}

func verifyConfiguredBarrelEnvVisible(d *Driver, barrel *Barrel) error {
	out, warnings, err := runWrappedBarrelSession(d, barrel, d.Config().BarrelEnvVars,
		`if [[ -v SMOKE_EMPTY ]]; then printf '%s|set:%s' "$SMOKE_ALPHA" "$SMOKE_EMPTY"; else printf '%s|unset' "$SMOKE_ALPHA"; fi`)
	if err != nil {
		return fmt.Errorf("wrapped barrel env session: %w", err)
	}
	if len(warnings) != 0 {
		return fmt.Errorf("unexpected warnings for valid env: %v", warnings)
	}
	if out != "one|set:" {
		return fmt.Errorf("configured barrel env output=%q, want %q", out, "one|set:")
	}
	return nil
}

func verifyProtectedBarrelEnvRestore(d *Driver, barrel *Barrel) error {
	if err := persistDriverBarrelEnv(d, []config.BarrelEnvVar{{Name: "HTTP_PROXY", Value: "http://bad"}, {Name: "DISPLAY", Value: ":55"}}); err != nil {
		return err
	}
	reloaded, err := d.PersistedConfig()
	if err != nil {
		return fmt.Errorf("reload persisted config: %w", err)
	}
	out, _, err := runWrappedBarrelSession(d, barrel, reloaded.BarrelEnvVars, `printf '%s|%s|%s' "$HTTP_PROXY" "$DISPLAY" "${http_proxy-}"`)
	if err != nil {
		return fmt.Errorf("protected barrel env session: %w", err)
	}
	expected := fmt.Sprintf("http://%s:%d|127.0.0.1:99|", docker.ProxyHost(), d.Config().ProxyPort)
	if out != expected {
		return fmt.Errorf("protected env output=%q, want %q", out, expected)
	}
	return nil
}

func verifyBarrelEnvNextSessionReload(d *Driver, barrel *Barrel) error {
	if err := persistDriverBarrelEnv(d, []config.BarrelEnvVar{{Name: "SMOKE_CHANGE", Value: "before"}}); err != nil {
		return err
	}
	reloaded, err := d.PersistedConfig()
	if err != nil {
		return fmt.Errorf("reload persisted config: %w", err)
	}
	out, warnings, err := runWrappedBarrelSession(d, barrel, reloaded.BarrelEnvVars, `printf %s "$SMOKE_CHANGE"`)
	if err != nil {
		return fmt.Errorf("before-reload session: %w", err)
	}
	if len(warnings) != 0 || out != "before" {
		return fmt.Errorf("before-reload output=%q warnings=%v", out, warnings)
	}

	if err := persistDriverBarrelEnv(d, []config.BarrelEnvVar{{Name: "SMOKE_CHANGE", Value: "after"}}); err != nil {
		return err
	}
	reloaded, err = d.PersistedConfig()
	if err != nil {
		return fmt.Errorf("reload persisted config: %w", err)
	}
	out, warnings, err = runWrappedBarrelSession(d, barrel, reloaded.BarrelEnvVars, `printf %s "$SMOKE_CHANGE"`)
	if err != nil {
		return fmt.Errorf("after-reload session: %w", err)
	}
	if len(warnings) != 0 || out != "after" {
		return fmt.Errorf("after-reload output=%q warnings=%v", out, warnings)
	}
	running, err := docker.IsBarrelRunning(barrel.Name)
	if err != nil {
		return fmt.Errorf("check barrel running: %w", err)
	}
	if !running {
		return fmt.Errorf("barrel %s stopped unexpectedly between sessions", barrel.Name)
	}
	return nil
}

func verifySessionMountIsolation(d *Driver, barrel *Barrel) error {
	out, err := d.ExecBarrel(barrel.Name, `if touch '`+docker.BarrelSessionContainerDir+`/blocked' 2>/dev/null; then printf 'session-rw'; elif touch /tmp/cooper-session-check 2>/dev/null; then printf 'session-ro|tmp-rw'; else printf 'session-ro|tmp-blocked'; fi`)
	if err != nil {
		return fmt.Errorf("session mount isolation exec: %w", err)
	}
	if strings.TrimSpace(out) != "session-ro|tmp-rw" {
		return fmt.Errorf("session mount isolation output=%q, want %q", strings.TrimSpace(out), "session-ro|tmp-rw")
	}
	if _, err := exec.Command("docker", "exec", barrel.Name, "test", "-w", docker.BarrelSessionContainerDir).CombinedOutput(); err == nil {
		return fmt.Errorf("session mount %s unexpectedly writable", docker.BarrelSessionContainerDir)
	}
	return nil
}

func persistDriverBarrelEnv(d *Driver, vars []config.BarrelEnvVar) error {
	d.Config().BarrelEnvVars = append([]config.BarrelEnvVar(nil), vars...)
	if err := config.SaveConfig(filepath.Join(d.CooperDir(), "config.json"), d.Config()); err != nil {
		return fmt.Errorf("save config.json: %w", err)
	}
	return nil
}

func runWrappedBarrelSession(d *Driver, barrel *Barrel, vars []config.BarrelEnvVar, shellCmd string) (string, []string, error) {
	sessionName := names.Generate(barrel.WorkspaceDir)
	defer names.Release(sessionName)

	sessionEnvFile, warnings, err := barrelenv.PrepareSessionEnvFile(d.CooperDir(), barrel.Name, sessionName, vars)
	if err != nil {
		return "", warnings, fmt.Errorf("prepare session env file: %w", err)
	}
	if sessionEnvFile.HostPath != "" {
		defer barrelenv.RemoveSessionEnvFile(sessionEnvFile.HostPath)
	}

	wrapperCmd, err := barrelenv.BuildExecWrapperCommand(
		sessionEnvFile.ContainerPath,
		barrelenv.ProtectedRuntimeEnvNames(nil),
		[]string{"bash", "-c", shellCmd},
	)
	if err != nil {
		return "", warnings, fmt.Errorf("build wrapper command: %w", err)
	}

	args := []string{"exec", barrel.Name}
	args = append(args, wrapperCmd...)
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(out)), warnings, fmt.Errorf("docker exec %s failed: %w", barrel.Name, err)
	}
	return strings.TrimSpace(string(out)), warnings, nil
}
