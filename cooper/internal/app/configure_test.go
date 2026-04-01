package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
)

// TestConfigureApp_NewFresh verifies that creating a ConfigureApp with no
// existing config.json returns defaults and IsExisting=false.
func TestConfigureApp_NewFresh(t *testing.T) {
	cooperDir := t.TempDir()

	ca, err := NewConfigureApp(cooperDir)
	if err != nil {
		t.Fatalf("NewConfigureApp: %v", err)
	}

	if ca.IsExisting() {
		t.Error("expected IsExisting=false for fresh directory")
	}

	cfg := ca.Config()
	if cfg.ProxyPort != 3128 {
		t.Errorf("expected default ProxyPort=3128, got %d", cfg.ProxyPort)
	}
	if cfg.BridgePort != 4343 {
		t.Errorf("expected default BridgePort=4343, got %d", cfg.BridgePort)
	}
	if len(cfg.WhitelistedDomains) == 0 {
		t.Error("expected default whitelisted domains, got empty")
	}
	if len(cfg.ProgrammingTools) != 0 {
		t.Errorf("expected no programming tools in default config, got %d", len(cfg.ProgrammingTools))
	}
}

// TestConfigureApp_NewExisting verifies that creating a ConfigureApp with
// an existing config.json loads it and sets IsExisting=true.
func TestConfigureApp_NewExisting(t *testing.T) {
	cooperDir := t.TempDir()

	// Write a config.json with a non-default proxy port.
	cfg := config.DefaultConfig()
	cfg.ProxyPort = 9999
	configPath := filepath.Join(cooperDir, "config.json")
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	ca, err := NewConfigureApp(cooperDir)
	if err != nil {
		t.Fatalf("NewConfigureApp: %v", err)
	}

	if !ca.IsExisting() {
		t.Error("expected IsExisting=true for directory with config.json")
	}

	got := ca.Config()
	if got.ProxyPort != 9999 {
		t.Errorf("expected ProxyPort=9999 from existing config, got %d", got.ProxyPort)
	}
}

// TestConfigureApp_DetectHostTools verifies that DetectHostTools returns a
// non-empty list and populates host versions for tools that are installed.
func TestConfigureApp_DetectHostTools(t *testing.T) {
	cooperDir := t.TempDir()
	ca, err := NewConfigureApp(cooperDir)
	if err != nil {
		t.Fatalf("NewConfigureApp: %v", err)
	}

	tools := ca.DetectHostTools()
	if len(tools) == 0 {
		t.Fatal("expected non-empty tool list from DetectHostTools")
	}

	// We expect at least the known programming tool names.
	names := make(map[string]bool)
	for _, tc := range tools {
		names[tc.Name] = true
	}
	for _, expected := range []string{"go", "node", "python"} {
		if !names[expected] {
			t.Errorf("expected tool %q in DetectHostTools result", expected)
		}
	}

	// At least one tool should have a host version detected on a typical
	// dev machine. If running in CI with Go installed, "go" should be detected.
	anyDetected := false
	for _, tc := range tools {
		if tc.HostVersion != "" {
			anyDetected = true
			if !tc.Enabled {
				t.Errorf("tool %q has HostVersion=%q but Enabled=false", tc.Name, tc.HostVersion)
			}
			if tc.Mode != config.ModeMirror {
				t.Errorf("tool %q with detected version should have Mode=ModeMirror, got %v", tc.Name, tc.Mode)
			}
		}
	}
	if !anyDetected {
		t.Log("warning: no programming tools detected on host (expected at least 'go')")
	}
}

// TestConfigureApp_SetAndValidate verifies that setting tools and validating
// produces no error for a well-formed configuration.
func TestConfigureApp_SetAndValidate(t *testing.T) {
	cooperDir := t.TempDir()
	ca, err := NewConfigureApp(cooperDir)
	if err != nil {
		t.Fatalf("NewConfigureApp: %v", err)
	}

	ca.SetProgrammingTools([]config.ToolConfig{
		{Name: "go", Enabled: true, Mode: config.ModePin, PinnedVersion: "1.22.5"},
	})
	ca.SetAITools([]config.ToolConfig{
		{Name: "claude", Enabled: true, Mode: config.ModeLatest},
	})
	ca.SetProxyPort(3128)
	ca.SetBridgePort(4343)

	if err := ca.Validate(); err != nil {
		t.Errorf("expected no validation error, got: %v", err)
	}

	// Verify the config reflects what we set.
	cfg := ca.Config()
	if len(cfg.ProgrammingTools) != 1 || cfg.ProgrammingTools[0].Name != "go" {
		t.Errorf("unexpected ProgrammingTools: %+v", cfg.ProgrammingTools)
	}
	if len(cfg.AITools) != 1 || cfg.AITools[0].Name != "claude" {
		t.Errorf("unexpected AITools: %+v", cfg.AITools)
	}
}

// TestConfigureApp_ValidationFails verifies that Validate returns an error
// when proxy and bridge ports conflict.
func TestConfigureApp_ValidationFails(t *testing.T) {
	cooperDir := t.TempDir()
	ca, err := NewConfigureApp(cooperDir)
	if err != nil {
		t.Fatalf("NewConfigureApp: %v", err)
	}

	// Set conflicting ports.
	ca.SetProxyPort(3128)
	ca.SetBridgePort(3128)

	err = ca.Validate()
	if err == nil {
		t.Fatal("expected validation error for conflicting ports, got nil")
	}

	// Also test port out of range.
	ca.SetProxyPort(0)
	ca.SetBridgePort(4343)
	err = ca.Validate()
	if err == nil {
		t.Fatal("expected validation error for port 0, got nil")
	}

	// Port forward rule colliding with proxy port.
	ca.SetProxyPort(3128)
	ca.SetPortForwardRules([]config.PortForwardRule{
		{ContainerPort: 3128, HostPort: 8080, Description: "conflict"},
	})
	err = ca.Validate()
	if err == nil {
		t.Fatal("expected validation error for port forward colliding with proxy port, got nil")
	}
}

// TestConfigureApp_Save verifies that Save writes config.json and generates
// Dockerfiles and squid.conf in the cooperDir.
func TestConfigureApp_Save(t *testing.T) {
	cooperDir := t.TempDir()
	ca, err := NewConfigureApp(cooperDir)
	if err != nil {
		t.Fatalf("NewConfigureApp: %v", err)
	}

	// Set up a minimal valid config.
	ca.SetProgrammingTools([]config.ToolConfig{
		{Name: "go", Enabled: true, Mode: config.ModePin, PinnedVersion: "1.22.5"},
	})

	if err := ca.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Check that config.json was written.
	configPath := filepath.Join(cooperDir, "config.json")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config.json was not written")
	}

	// Verify config.json can be loaded back and has our tool.
	loaded, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig after Save: %v", err)
	}
	if len(loaded.ProgrammingTools) != 1 || loaded.ProgrammingTools[0].Name != "go" {
		t.Errorf("loaded config ProgrammingTools mismatch: %+v", loaded.ProgrammingTools)
	}

	// Check that base templates were written.
	baseDir := filepath.Join(cooperDir, "base")
	for _, name := range []string{"Dockerfile", "entrypoint.sh", "doctor.sh"} {
		path := filepath.Join(baseDir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected %s to exist after Save", path)
		}
	}

	// Check that per-tool Dockerfiles were written for enabled tools.
	cliDir := filepath.Join(cooperDir, "cli")
	for _, tool := range ca.Config().AITools {
		if !tool.Enabled {
			continue
		}
		path := filepath.Join(cliDir, tool.Name, "Dockerfile")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected %s to exist after Save", path)
		}
	}

	// Check that proxy templates were written.
	proxyDir := filepath.Join(cooperDir, "proxy")
	for _, name := range []string{"proxy.Dockerfile", "squid.conf", "proxy-entrypoint.sh"} {
		path := filepath.Join(proxyDir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected %s to exist after Save", path)
		}
	}

	// Check that CA certificate was created.
	caDir := filepath.Join(cooperDir, "ca")
	for _, name := range []string{"cooper-ca.pem", "cooper-ca-key.pem"} {
		path := filepath.Join(caDir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected %s to exist after Save", path)
		}
	}
}

// TestConfigureApp_ConfigIsCopy verifies that Config() returns a copy
// that does not mutate the internal state.
func TestConfigureApp_ConfigIsCopy(t *testing.T) {
	cooperDir := t.TempDir()
	ca, err := NewConfigureApp(cooperDir)
	if err != nil {
		t.Fatalf("NewConfigureApp: %v", err)
	}

	cfg := ca.Config()
	cfg.ProxyPort = 9999
	cfg.WhitelistedDomains = nil

	// Internal state should be unchanged.
	internal := ca.Config()
	if internal.ProxyPort == 9999 {
		t.Error("Config() returned a reference instead of a copy — ProxyPort was mutated")
	}
	if internal.WhitelistedDomains == nil {
		t.Error("Config() returned a reference instead of a copy — WhitelistedDomains was mutated")
	}
}
