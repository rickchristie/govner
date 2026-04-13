package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
)

func TestConfigureAppSetBarrelEnvVarsCopiesInput(t *testing.T) {
	ca, err := NewConfigureApp(t.TempDir())
	if err != nil {
		t.Fatalf("NewConfigureApp() failed: %v", err)
	}
	vars := []config.BarrelEnvVar{{Name: "FOO", Value: "1"}}
	ca.SetBarrelEnvVars(vars)
	vars[0].Value = "mutated"

	if got := ca.Config().BarrelEnvVars[0].Value; got != "1" {
		t.Fatalf("stored BarrelEnvVars[0].Value = %q, want %q", got, "1")
	}
}

func TestConfigureAppSavePersistsBarrelEnvVars(t *testing.T) {
	stubConfigureTestResolvers(t)

	cooperDir := t.TempDir()
	ca, err := NewConfigureApp(cooperDir)
	if err != nil {
		t.Fatalf("NewConfigureApp() failed: %v", err)
	}
	ca.SetBarrelEnvVars([]config.BarrelEnvVar{{Name: "FOO", Value: "1"}, {Name: "BAR", Value: "two words"}})

	if _, err := ca.Save(); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	loaded, err := config.LoadConfig(filepath.Join(cooperDir, "config.json"))
	if err != nil {
		t.Fatalf("LoadConfig() failed: %v", err)
	}
	if len(loaded.BarrelEnvVars) != 2 {
		t.Fatalf("len(loaded.BarrelEnvVars) = %d, want 2", len(loaded.BarrelEnvVars))
	}
	if loaded.BarrelEnvVars[0].Name != "FOO" || loaded.BarrelEnvVars[1].Name != "BAR" {
		t.Fatalf("loaded BarrelEnvVars = %+v", loaded.BarrelEnvVars)
	}
}

func TestConfigureAppNewExistingReloadsBarrelEnvVars(t *testing.T) {
	cooperDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.BarrelEnvVars = []config.BarrelEnvVar{{Name: "FOO", Value: "1"}, {Name: "BAR", Value: "2"}}
	if err := config.SaveConfig(filepath.Join(cooperDir, "config.json"), cfg); err != nil {
		t.Fatalf("SaveConfig() failed: %v", err)
	}

	ca, err := NewConfigureApp(cooperDir)
	if err != nil {
		t.Fatalf("NewConfigureApp() failed: %v", err)
	}
	got := ca.Config().BarrelEnvVars
	if len(got) != 2 || got[0] != cfg.BarrelEnvVars[0] || got[1] != cfg.BarrelEnvVars[1] {
		t.Fatalf("BarrelEnvVars = %+v, want %+v", got, cfg.BarrelEnvVars)
	}
}

func TestConfigureAppSaveFailsWhenBarrelEnvVarsInvalid(t *testing.T) {
	stubConfigureTestResolvers(t)

	ca, err := NewConfigureApp(t.TempDir())
	if err != nil {
		t.Fatalf("NewConfigureApp() failed: %v", err)
	}
	ca.SetBarrelEnvVars([]config.BarrelEnvVar{{Name: "BAD-NAME", Value: "x"}})

	if _, err := ca.Save(); err == nil {
		t.Fatal("expected Save() to fail for invalid barrel env vars")
	}
}

func TestConfigureAppSaveCanonicalizesBarrelEnvVars(t *testing.T) {
	stubConfigureTestResolvers(t)

	cooperDir := t.TempDir()
	ca, err := NewConfigureApp(cooperDir)
	if err != nil {
		t.Fatalf("NewConfigureApp() failed: %v", err)
	}
	ca.SetBarrelEnvVars([]config.BarrelEnvVar{{Name: "  FOO  ", Value: "x"}})

	if _, err := ca.Save(); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}
	if got := ca.Config().BarrelEnvVars[0].Name; got != "FOO" {
		t.Fatalf("in-memory BarrelEnvVars[0].Name = %q, want %q", got, "FOO")
	}
	loaded, err := config.LoadConfig(filepath.Join(cooperDir, "config.json"))
	if err != nil {
		t.Fatalf("LoadConfig() failed: %v", err)
	}
	if got := loaded.BarrelEnvVars[0].Name; got != "FOO" {
		t.Fatalf("persisted BarrelEnvVars[0].Name = %q, want %q", got, "FOO")
	}
}

func TestCooperAppUpdateSettingsIgnoresInvalidHandEditedBarrelEnv(t *testing.T) {
	cooperDir, cfg := setupCooperDir(t)
	cfg.BarrelEnvVars = []config.BarrelEnvVar{{Name: "HTTP_PROXY", Value: "http://bad"}}

	app := NewCooperApp(cfg, cooperDir)
	if err := app.UpdateSettings(15, 200, 300, 100, 42, 2048); err != nil {
		t.Fatalf("UpdateSettings() failed: %v", err)
	}

	loaded, err := config.LoadConfig(filepath.Join(cooperDir, "config.json"))
	if err != nil {
		t.Fatalf("LoadConfig() failed: %v", err)
	}
	if loaded.MonitorTimeoutSecs != 15 || loaded.ClipboardTTLSecs != 42 {
		t.Fatalf("persisted settings not updated: %+v", loaded)
	}
}

func TestCooperAppUpdatePortForwardsIgnoresInvalidHandEditedBarrelEnv(t *testing.T) {
	skipIfNoDocker(t)
	skipIfNoProxyImage(t)
	docker.SetImagePrefix(testImagePrefix)

	cooperDir, cfg := setupCooperDir(t)
	cfg.BarrelEnvVars = []config.BarrelEnvVar{{Name: "BAD-NAME", Value: "x"}}
	t.Cleanup(func() { cleanupDocker(t) })

	app := NewCooperApp(cfg, cooperDir)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := app.Start(ctx, nil); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer app.Stop()

	rules := []config.PortForwardRule{{ContainerPort: 18080, HostPort: 18080, Description: "test HTTP"}}
	if err := app.UpdatePortForwards(rules); err != nil {
		t.Fatalf("UpdatePortForwards() failed: %v", err)
	}
	loaded, err := config.LoadConfig(filepath.Join(cooperDir, "config.json"))
	if err != nil {
		t.Fatalf("LoadConfig() failed: %v", err)
	}
	if len(loaded.PortForwardRules) != 1 || loaded.PortForwardRules[0].ContainerPort != 18080 {
		t.Fatalf("persisted PortForwardRules = %+v", loaded.PortForwardRules)
	}
}
