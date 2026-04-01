package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
)

// testConfig returns a fully populated test config with all tools enabled.
func testConfig() *config.Config {
	return &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "go", Enabled: true, PinnedVersion: "1.24.10"},
			{Name: "node", Enabled: true, PinnedVersion: "22.12.0"},
			{Name: "python", Enabled: true, PinnedVersion: "3.12"},
		},
		AITools: []config.ToolConfig{
			{Name: "claude", Enabled: true},
			{Name: "copilot", Enabled: true},
			{Name: "codex", Enabled: true},
			{Name: "opencode", Enabled: true},
		},
		WhitelistedDomains: []config.DomainEntry{
			{Domain: ".anthropic.com", IncludeSubdomains: true, Source: "default"},
			{Domain: "platform.claude.com", IncludeSubdomains: false, Source: "default"},
			{Domain: ".openai.com", IncludeSubdomains: true, Source: "default"},
			{Domain: "github.com", IncludeSubdomains: false, Source: "default"},
			{Domain: "api.github.com", IncludeSubdomains: false, Source: "default"},
			{Domain: ".githubcopilot.com", IncludeSubdomains: true, Source: "default"},
			{Domain: "custom.example.com", IncludeSubdomains: false, Source: "user"},
		},
		PortForwardRules: []config.PortForwardRule{
			{ContainerPort: 5432, HostPort: 5432, Description: "postgres"},
			{ContainerPort: 6379, HostPort: 6379, Description: "redis"},
		},
		ProxyPort:  3128,
		BridgePort: 4343,
	}
}

// minimalConfig returns a config with Go and Claude Code only.
func minimalConfig() *config.Config {
	return &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "go", Enabled: true, PinnedVersion: "1.24.10"},
		},
		AITools: []config.ToolConfig{
			{Name: "claude", Enabled: true},
		},
		WhitelistedDomains: []config.DomainEntry{
			{Domain: ".anthropic.com", IncludeSubdomains: true, Source: "default"},
		},
		PortForwardRules: []config.PortForwardRule{},
		ProxyPort:        3128,
		BridgePort:       4343,
	}
}

// noToolsConfig returns a config with no tools enabled.
func noToolsConfig() *config.Config {
	return &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "go", Enabled: false},
			{Name: "node", Enabled: false},
			{Name: "python", Enabled: false},
		},
		AITools: []config.ToolConfig{
			{Name: "claude", Enabled: false},
			{Name: "copilot", Enabled: false},
			{Name: "codex", Enabled: false},
			{Name: "opencode", Enabled: false},
		},
		WhitelistedDomains: []config.DomainEntry{},
		PortForwardRules:   []config.PortForwardRule{},
		ProxyPort:          3128,
		BridgePort:         4343,
	}
}

// assertContains checks that haystack contains needle.
func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected output to contain %q, but it did not.\nOutput (first 500 chars):\n%s", needle, truncate(haystack, 500))
	}
}

// assertNotContains checks that haystack does NOT contain needle.
func assertNotContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("expected output NOT to contain %q, but it did.\nOutput (first 500 chars):\n%s", needle, truncate(haystack, 500))
	}
}

// truncate returns s truncated to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ---------------------------------------------------------------------------
// RenderBaseDockerfile tests
// ---------------------------------------------------------------------------

func TestRenderBaseDockerfile_DefaultConfig(t *testing.T) {
	cfg := testConfig()
	result, err := RenderBaseDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderBaseDockerfile failed: %v", err)
	}

	// Should use Go base image when Go is enabled
	assertContains(t, result, "golang:1.24.10-bookworm")

	// Should install Node.js via tarball with pinned version
	assertContains(t, result, "NODE_VERSION=22.12.0")
	assertContains(t, result, "nodejs.org/dist/v${NODE_VERSION}")

	// Should have Python installation
	assertContains(t, result, "python3")

	// Should have bubblewrap build (because Codex is enabled)
	assertContains(t, result, "bubblewrap")
	assertContains(t, result, "meson")

	// Should have OpenCode-specific packages (xvfb, xclip, inotify-tools)
	assertContains(t, result, "xvfb")
	assertContains(t, result, "xclip")
	assertContains(t, result, "inotify-tools")

	// Should have CA cert injection
	assertContains(t, result, "cooper-ca.pem")
	assertContains(t, result, "update-ca-certificates")
	assertContains(t, result, "NODE_EXTRA_CA_CERTS")

	// Should have proxy env vars pointing to cooper-proxy
	assertContains(t, result, "HTTP_PROXY=http://cooper-proxy:3128")
	assertContains(t, result, "HTTPS_PROXY=http://cooper-proxy:3128")

	// Should have user setup
	assertContains(t, result, "USER_UID")
	assertContains(t, result, "USER_GID")
	assertContains(t, result, "useradd")

	// Should have entrypoint
	assertContains(t, result, "COPY entrypoint.sh")
	assertContains(t, result, "ENTRYPOINT")

	// Should have doctor script
	assertContains(t, result, "COPY doctor.sh")

	// Base image should NOT contain AI tool install commands
	assertNotContains(t, result, "claude.ai/install.sh")
	assertNotContains(t, result, "@github/copilot")
	assertNotContains(t, result, "@openai/codex")
	assertNotContains(t, result, "opencode.ai/install")
	assertNotContains(t, result, "CACHE_BUST_AI")
	assertNotContains(t, result, "CACHE_BUST_LANG")
}

func TestRenderBaseDockerfile_GoEnabled(t *testing.T) {
	cfg := testConfig()
	result, err := RenderBaseDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderBaseDockerfile failed: %v", err)
	}

	assertContains(t, result, "FROM golang:1.24.10-bookworm")
}

func TestRenderBaseDockerfile_NoGo(t *testing.T) {
	cfg := testConfig()
	for i := range cfg.ProgrammingTools {
		if cfg.ProgrammingTools[i].Name == "go" {
			cfg.ProgrammingTools[i].Enabled = false
		}
	}

	result, err := RenderBaseDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderBaseDockerfile failed: %v", err)
	}

	assertContains(t, result, "FROM debian:bookworm-slim")
	assertNotContains(t, result, "golang:")
}

func TestRenderBaseDockerfile_NodeVersion(t *testing.T) {
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "node", Enabled: true, PinnedVersion: "22.12.0"},
		},
		AITools:    []config.ToolConfig{},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderBaseDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderBaseDockerfile failed: %v", err)
	}

	assertContains(t, result, "NODE_VERSION=22.12.0")
}

func TestRenderBaseDockerfile_PythonEnabled(t *testing.T) {
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "python", Enabled: true},
		},
		AITools:    []config.ToolConfig{},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderBaseDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderBaseDockerfile failed: %v", err)
	}

	assertContains(t, result, "python3")
	assertContains(t, result, "python3-pip")
	assertContains(t, result, "python3-venv")
}

func TestRenderBaseDockerfile_BubblewrapForCodex(t *testing.T) {
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{},
		AITools: []config.ToolConfig{
			{Name: "codex", Enabled: true},
		},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderBaseDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderBaseDockerfile failed: %v", err)
	}

	assertContains(t, result, "meson")
	assertContains(t, result, "bubblewrap")
	assertContains(t, result, "ninja-build")
}

func TestRenderBaseDockerfile_XvfbForOpenCode(t *testing.T) {
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{},
		AITools: []config.ToolConfig{
			{Name: "opencode", Enabled: true},
		},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderBaseDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderBaseDockerfile failed: %v", err)
	}

	assertContains(t, result, "xvfb")
	assertContains(t, result, "xclip")
	assertContains(t, result, "inotify-tools")
}

func TestRenderBaseDockerfile_ProxyEnvVars(t *testing.T) {
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{},
		AITools:          []config.ToolConfig{},
		ProxyPort:        8080,
		BridgePort:       4343,
	}

	result, err := RenderBaseDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderBaseDockerfile failed: %v", err)
	}

	assertContains(t, result, "HTTP_PROXY=http://cooper-proxy:8080")
	assertContains(t, result, "HTTPS_PROXY=http://cooper-proxy:8080")
}

func TestRenderBaseDockerfile_CACertInjection(t *testing.T) {
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{},
		AITools:          []config.ToolConfig{},
		ProxyPort:        3128,
		BridgePort:       4343,
	}

	result, err := RenderBaseDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderBaseDockerfile failed: %v", err)
	}

	assertContains(t, result, "COPY cooper-ca.pem")
	assertContains(t, result, "update-ca-certificates")
}

func TestRenderBaseDockerfile_UserSetup(t *testing.T) {
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{},
		AITools:          []config.ToolConfig{},
		ProxyPort:        3128,
		BridgePort:       4343,
	}

	result, err := RenderBaseDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderBaseDockerfile failed: %v", err)
	}

	assertContains(t, result, "USER_UID")
	assertContains(t, result, "USER_GID")
	assertContains(t, result, "useradd")
}

func TestRenderBaseDockerfile_NoAITools(t *testing.T) {
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "go", Enabled: true, PinnedVersion: "1.24.10"},
		},
		AITools:    []config.ToolConfig{},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderBaseDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderBaseDockerfile failed: %v", err)
	}

	// Should NOT contain any AI tool install commands
	assertNotContains(t, result, "claude.ai/install.sh")
	assertNotContains(t, result, "@github/copilot")
	assertNotContains(t, result, "@openai/codex")
	assertNotContains(t, result, "opencode.ai/install")
	assertNotContains(t, result, "CACHE_BUST_AI")
	assertNotContains(t, result, "CACHE_BUST_LANG")
}

func TestRenderBaseDockerfile_HasEntrypoint(t *testing.T) {
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{},
		AITools:          []config.ToolConfig{},
		ProxyPort:        3128,
		BridgePort:       4343,
	}

	result, err := RenderBaseDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderBaseDockerfile failed: %v", err)
	}

	assertContains(t, result, "COPY entrypoint.sh")
	assertContains(t, result, "ENTRYPOINT")
}

func TestRenderBaseDockerfile_HasDoctorScript(t *testing.T) {
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{},
		AITools:          []config.ToolConfig{},
		ProxyPort:        3128,
		BridgePort:       4343,
	}

	result, err := RenderBaseDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderBaseDockerfile failed: %v", err)
	}

	assertContains(t, result, "COPY doctor.sh")
}

func TestRenderBaseDockerfile_RuntimeDepsIncluded(t *testing.T) {
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{},
		AITools: []config.ToolConfig{
			{Name: "codex", Enabled: true},
			{Name: "opencode", Enabled: true},
		},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderBaseDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderBaseDockerfile failed: %v", err)
	}

	// Codex runtime deps
	assertContains(t, result, "meson")
	assertContains(t, result, "bubblewrap")

	// OpenCode runtime deps
	assertContains(t, result, "xvfb")
	assertContains(t, result, "xclip")
}

func TestRenderBaseDockerfile_RuntimeDepsExcluded(t *testing.T) {
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{},
		AITools: []config.ToolConfig{
			{Name: "claude", Enabled: true},
		},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderBaseDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderBaseDockerfile failed: %v", err)
	}

	// No codex -> no bubblewrap/meson
	assertNotContains(t, result, "meson")
	assertNotContains(t, result, "bubblewrap")

	// No opencode -> no xvfb/xclip
	assertNotContains(t, result, "xvfb")
	assertNotContains(t, result, "xclip")
}

// ---------------------------------------------------------------------------
// RenderCLIToolDockerfile tests
// ---------------------------------------------------------------------------

func TestRenderCLIToolDockerfile_Claude(t *testing.T) {
	cfg := &config.Config{
		AITools: []config.ToolConfig{
			{Name: "claude", Enabled: true},
		},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderCLIToolDockerfile(cfg, "claude")
	if err != nil {
		t.Fatalf("RenderCLIToolDockerfile failed: %v", err)
	}

	assertContains(t, result, "FROM "+docker.GetImageBase())
	assertContains(t, result, "claude.ai/install.sh")
	assertContains(t, result, "COOPER_CLI_TOOL=claude")
	assertContains(t, result, "--dangerously-skip-permissions")
	assertContains(t, result, "/home/user/.claude")
}

func TestRenderCLIToolDockerfile_ClaudeVersionPinned(t *testing.T) {
	cfg := &config.Config{
		AITools: []config.ToolConfig{
			{Name: "claude", Enabled: true, PinnedVersion: "2.1.87"},
		},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderCLIToolDockerfile(cfg, "claude")
	if err != nil {
		t.Fatalf("RenderCLIToolDockerfile failed: %v", err)
	}

	assertContains(t, result, "bash -s -- 2.1.87")
}

func TestRenderCLIToolDockerfile_Copilot(t *testing.T) {
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "node", Enabled: true, PinnedVersion: "22.12.0"},
		},
		AITools: []config.ToolConfig{
			{Name: "copilot", Enabled: true},
		},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderCLIToolDockerfile(cfg, "copilot")
	if err != nil {
		t.Fatalf("RenderCLIToolDockerfile failed: %v", err)
	}

	assertContains(t, result, "npm install -g @github/copilot")
	assertContains(t, result, "COOPER_CLI_TOOL=copilot")
}

func TestRenderCLIToolDockerfile_CopilotVersionPinned(t *testing.T) {
	cfg := &config.Config{
		AITools: []config.ToolConfig{
			{Name: "copilot", Enabled: true, PinnedVersion: "1.0.12"},
		},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderCLIToolDockerfile(cfg, "copilot")
	if err != nil {
		t.Fatalf("RenderCLIToolDockerfile failed: %v", err)
	}

	assertContains(t, result, "@github/copilot@1.0.12")
}

func TestRenderCLIToolDockerfile_Codex(t *testing.T) {
	cfg := &config.Config{
		AITools: []config.ToolConfig{
			{Name: "codex", Enabled: true},
		},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderCLIToolDockerfile(cfg, "codex")
	if err != nil {
		t.Fatalf("RenderCLIToolDockerfile failed: %v", err)
	}

	assertContains(t, result, "npm install -g @openai/codex")
	assertContains(t, result, "COOPER_CLI_TOOL=codex")
}

func TestRenderCLIToolDockerfile_CodexVersionPinned(t *testing.T) {
	cfg := &config.Config{
		AITools: []config.ToolConfig{
			{Name: "codex", Enabled: true, PinnedVersion: "0.117.0"},
		},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderCLIToolDockerfile(cfg, "codex")
	if err != nil {
		t.Fatalf("RenderCLIToolDockerfile failed: %v", err)
	}

	assertContains(t, result, "@openai/codex@0.117.0")
}

func TestRenderCLIToolDockerfile_OpenCode(t *testing.T) {
	cfg := &config.Config{
		AITools: []config.ToolConfig{
			{Name: "opencode", Enabled: true},
		},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderCLIToolDockerfile(cfg, "opencode")
	if err != nil {
		t.Fatalf("RenderCLIToolDockerfile failed: %v", err)
	}

	assertContains(t, result, "opencode.ai/install")
	assertContains(t, result, "COOPER_CLI_TOOL=opencode")
}

func TestRenderCLIToolDockerfile_OpenCodeVersionPinned(t *testing.T) {
	cfg := &config.Config{
		AITools: []config.ToolConfig{
			{Name: "opencode", Enabled: true, PinnedVersion: "1.3.7"},
		},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderCLIToolDockerfile(cfg, "opencode")
	if err != nil {
		t.Fatalf("RenderCLIToolDockerfile failed: %v", err)
	}

	assertContains(t, result, "--version 1.3.7")
}

func TestRenderCLIToolDockerfile_UnknownTool(t *testing.T) {
	cfg := &config.Config{
		AITools:    []config.ToolConfig{},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	_, err := RenderCLIToolDockerfile(cfg, "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}
	assertContains(t, err.Error(), "unknown")
}

func TestRenderCLIToolDockerfile_UsesCorrectBaseImage(t *testing.T) {
	cfg := &config.Config{
		AITools: []config.ToolConfig{
			{Name: "claude", Enabled: true},
		},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderCLIToolDockerfile(cfg, "claude")
	if err != nil {
		t.Fatalf("RenderCLIToolDockerfile failed: %v", err)
	}

	expectedBase := docker.GetImageBase()
	assertContains(t, result, "FROM "+expectedBase)
}

// ---------------------------------------------------------------------------
// RenderEntrypoint tests
// ---------------------------------------------------------------------------

func TestRenderEntrypoint(t *testing.T) {
	cfg := testConfig()
	result, err := RenderEntrypoint(cfg)
	if err != nil {
		t.Fatalf("RenderEntrypoint failed: %v", err)
	}

	// Should have dynamic auto-approve alias using env vars
	assertContains(t, result, "$COOPER_CLI_TOOL")
	assertContains(t, result, "$COOPER_CLI_AUTO_APPROVE")

	// Should have OpenCode DISPLAY setup conditional on tool type
	assertContains(t, result, "DISPLAY=:99.0")
	assertContains(t, result, "Xvfb :99")
	assertContains(t, result, "opencode.json")

	// Should read rules from socat-rules.json config file
	assertContains(t, result, "socat-rules.json")
	assertContains(t, result, "start_socat_from_config")
	assertContains(t, result, "jq")

	// Should have fallback bridge port from template
	assertContains(t, result, "run_socat 4343")

	// socat should target cooper-proxy (NOT host.docker.internal)
	assertContains(t, result, "cooper-proxy")
	assertNotContains(t, result, "host.docker.internal")

	// Should have bind to 127.0.0.1, fork, reuseaddr, backlog
	assertContains(t, result, "bind=127.0.0.1")
	assertContains(t, result, "fork")
	assertContains(t, result, "reuseaddr")
	assertContains(t, result, "backlog=5000")

	// Should track socat PIDs for clean reload
	assertContains(t, result, "SOCAT_SUPERVISORS")

	// Should handle SIGHUP for live reload
	assertContains(t, result, "reload_socat")
	assertContains(t, result, "trap")
	assertContains(t, result, "HUP")

	// Should run command in background and wait (not exec, to preserve signal traps)
	assertContains(t, result, `"$@" &`)
}

func TestRenderEntrypoint_DynamicAutoApprove(t *testing.T) {
	cfg := testConfig()
	result, err := RenderEntrypoint(cfg)
	if err != nil {
		t.Fatalf("RenderEntrypoint failed: %v", err)
	}

	// Should use dynamic env vars for auto-approve alias
	assertContains(t, result, "$COOPER_CLI_TOOL")
	assertContains(t, result, "$COOPER_CLI_AUTO_APPROVE")

	// Should NOT contain hardcoded tool-specific aliases
	assertNotContains(t, result, "alias claude=")
	assertNotContains(t, result, "alias copilot=")
	assertNotContains(t, result, "alias codex=")
	assertNotContains(t, result, "alias opencode=")
}

func TestRenderEntrypoint_XvfbConditionalOnTool(t *testing.T) {
	cfg := testConfig()
	result, err := RenderEntrypoint(cfg)
	if err != nil {
		t.Fatalf("RenderEntrypoint failed: %v", err)
	}

	// OpenCode Xvfb setup should be conditional on COOPER_CLI_TOOL env var
	assertContains(t, result, `if [ "$COOPER_CLI_TOOL" = "opencode" ]`)
}

func TestRenderEntrypoint_NoToolBooleans(t *testing.T) {
	// Verify that entrypointData only has HasGo and BridgePort fields.
	// This is a compile-time check: if someone adds HasClaudeCode back
	// to the struct, this test will fail to compile.
	d := entrypointData{
		HasGo:      true,
		BridgePort: 4343,
	}

	// Verify the struct fields are what we expect
	if !d.HasGo {
		t.Error("expected HasGo=true")
	}
	if d.BridgePort != 4343 {
		t.Errorf("expected BridgePort=4343, got %d", d.BridgePort)
	}
}

func TestRenderEntrypoint_ConfigDriven(t *testing.T) {
	// Port forwarding rules are now read from socat-rules.json at runtime,
	// not baked into the template. Verify the config-driven approach.
	cfg := &config.Config{
		AITools: []config.ToolConfig{
			{Name: "claude", Enabled: true},
		},
		PortForwardRules: []config.PortForwardRule{
			{ContainerPort: 8000, HostPort: 8000, Description: "dev-ports", IsRange: true, RangeEnd: 8010},
		},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderEntrypoint(cfg)
	if err != nil {
		t.Fatalf("RenderEntrypoint failed: %v", err)
	}

	// Should read from config file, not have hardcoded port rules
	assertContains(t, result, "socat-rules.json")
	assertContains(t, result, "start_socat_from_config")
	// Template should NOT contain the specific port numbers from rules
	assertNotContains(t, result, "run_socat 8000")
	// But should have the fallback bridge port
	assertContains(t, result, "run_socat 4343")
}

func TestRenderEntrypoint_NoAITools(t *testing.T) {
	cfg := noToolsConfig()
	result, err := RenderEntrypoint(cfg)
	if err != nil {
		t.Fatalf("RenderEntrypoint failed: %v", err)
	}

	// Should still have bridge socat
	assertContains(t, result, "run_socat 4343")

	// Should still run main command
	assertContains(t, result, `"$@" &`)
}

// ---------------------------------------------------------------------------
// WriteAllTemplates tests
// ---------------------------------------------------------------------------

func TestWriteAllTemplates(t *testing.T) {
	cfg := testConfig()
	baseDir := filepath.Join(t.TempDir(), "base")
	cliDir := filepath.Join(t.TempDir(), "cli")

	err := WriteAllTemplates(baseDir, cliDir, cfg)
	if err != nil {
		t.Fatalf("WriteAllTemplates failed: %v", err)
	}

	// Verify base directory files
	baseFiles := []string{
		"Dockerfile",
		"entrypoint.sh",
		"doctor.sh",
	}
	for _, name := range baseFiles {
		path := filepath.Join(baseDir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected file %s in baseDir not found: %v", name, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("file %s in baseDir is empty", name)
		}
	}

	// Verify per-tool Dockerfiles in cliDir
	expectedTools := []string{"claude", "copilot", "codex", "opencode"}
	for _, tool := range expectedTools {
		path := filepath.Join(cliDir, tool, "Dockerfile")
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected per-tool Dockerfile for %s not found: %v", tool, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("per-tool Dockerfile for %s is empty", tool)
		}
	}

	// Verify entrypoint.sh is executable
	info, err := os.Stat(filepath.Join(baseDir, "entrypoint.sh"))
	if err != nil {
		t.Fatalf("failed to stat entrypoint.sh: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Error("entrypoint.sh should be executable")
	}

	// Verify doctor.sh is executable
	info, err = os.Stat(filepath.Join(baseDir, "doctor.sh"))
	if err != nil {
		t.Fatalf("failed to stat doctor.sh: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Error("doctor.sh should be executable")
	}
}

func TestWriteAllTemplates_CreatesDirectory(t *testing.T) {
	cfg := minimalConfig()
	baseDir := filepath.Join(t.TempDir(), "nested", "base")
	cliDir := filepath.Join(t.TempDir(), "nested", "cli")

	err := WriteAllTemplates(baseDir, cliDir, cfg)
	if err != nil {
		t.Fatalf("WriteAllTemplates failed: %v", err)
	}

	// Base directory should have been created
	if _, err := os.Stat(baseDir); err != nil {
		t.Fatalf("base directory was not created: %v", err)
	}

	// CLI tool directory should have been created
	claudeDir := filepath.Join(cliDir, "claude")
	if _, err := os.Stat(claudeDir); err != nil {
		t.Fatalf("cli/claude directory was not created: %v", err)
	}
}

func TestWriteAllTemplates_FileContents(t *testing.T) {
	cfg := testConfig()
	baseDir := filepath.Join(t.TempDir(), "base")
	cliDir := filepath.Join(t.TempDir(), "cli")

	err := WriteAllTemplates(baseDir, cliDir, cfg)
	if err != nil {
		t.Fatalf("WriteAllTemplates failed: %v", err)
	}

	// Read back the base Dockerfile and verify content
	data, err := os.ReadFile(filepath.Join(baseDir, "Dockerfile"))
	if err != nil {
		t.Fatalf("failed to read base Dockerfile: %v", err)
	}
	assertContains(t, string(data), "golang:1.24.10-bookworm")
	// Base should NOT have AI tool installs
	assertNotContains(t, string(data), "claude.ai/install.sh")

	// Read back entrypoint.sh and verify content
	data, err = os.ReadFile(filepath.Join(baseDir, "entrypoint.sh"))
	if err != nil {
		t.Fatalf("failed to read entrypoint.sh: %v", err)
	}
	assertContains(t, string(data), "run_socat 4343")
	assertContains(t, string(data), "socat-rules.json")

	// Read back per-tool Dockerfile and verify content
	data, err = os.ReadFile(filepath.Join(cliDir, "claude", "Dockerfile"))
	if err != nil {
		t.Fatalf("failed to read claude Dockerfile: %v", err)
	}
	assertContains(t, string(data), "FROM "+docker.GetImageBase())
	assertContains(t, string(data), "claude.ai/install.sh")
	assertContains(t, string(data), "COOPER_CLI_TOOL=claude")

	// Verify codex per-tool Dockerfile
	data, err = os.ReadFile(filepath.Join(cliDir, "codex", "Dockerfile"))
	if err != nil {
		t.Fatalf("failed to read codex Dockerfile: %v", err)
	}
	assertContains(t, string(data), "@openai/codex")
	assertContains(t, string(data), "COOPER_CLI_TOOL=codex")
}

// ---------------------------------------------------------------------------
// Proxy Dockerfile tests (unchanged)
// ---------------------------------------------------------------------------

func TestRenderProxyDockerfile(t *testing.T) {
	cfg := testConfig()
	result, err := RenderProxyDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderProxyDockerfile failed: %v", err)
	}

	// Should build Squid from source with SSL bump support
	assertContains(t, result, "--enable-ssl-crtd")
	assertContains(t, result, "--with-openssl")

	// Should have security_file_certgen for dynamic cert generation
	assertContains(t, result, "security_file_certgen")

	// Should have Alpine base
	assertContains(t, result, "alpine:3.21")

	// Should have socat installed
	assertContains(t, result, "socat")

	// Should have logrotate
	assertContains(t, result, "logrotate")

	// Should have ACL helper
	assertContains(t, result, "cooper-acl-helper")

	// Should have CA cert copy
	assertContains(t, result, "cooper-ca.pem")
	assertContains(t, result, "cooper-ca-key.pem")

	// Should expose the proxy port
	assertContains(t, result, "EXPOSE 3128")
}

// ---------------------------------------------------------------------------
// Squid conf tests (unchanged)
// ---------------------------------------------------------------------------

func TestRenderSquidConf(t *testing.T) {
	cfg := testConfig()
	result, err := RenderSquidConf(cfg)
	if err != nil {
		t.Fatalf("RenderSquidConf failed: %v", err)
	}

	// Should have SSL bump configuration
	assertContains(t, result, "ssl-bump")
	assertContains(t, result, "ssl_bump peek")
	assertContains(t, result, "ssl_bump bump all")
	assertContains(t, result, "security_file_certgen")
	assertContains(t, result, "generate-host-certificates=on")

	// Should have all whitelisted domains from config
	assertContains(t, result, ".anthropic.com")
	assertContains(t, result, "platform.claude.com")
	assertContains(t, result, ".openai.com")
	assertContains(t, result, "github.com")
	assertContains(t, result, "api.github.com")
	assertContains(t, result, ".githubcopilot.com")
	assertContains(t, result, "custom.example.com")

	// Should have external ACL for non-whitelisted domain approval
	assertContains(t, result, "external_acl_type cooper_acl")
	assertContains(t, result, "cooper-acl-helper")
	assertContains(t, result, "acl.sock")

	// Should have streaming-friendly timeouts
	assertContains(t, result, "read_timeout 120 minutes")
	assertContains(t, result, "client_lifetime 1 day")

	// dns_v4_first removed in Squid 6 -- verify the directive is NOT active.
	assertNotContains(t, result, "dns_v4_first on")

	// Should disable caching
	assertContains(t, result, "cache deny all")

	// Should have logging
	assertContains(t, result, "access_log")

	// Port should match config
	assertContains(t, result, "http_port 3128")
}

func TestRenderSquidConf_EmptyWhitelist(t *testing.T) {
	cfg := &config.Config{
		WhitelistedDomains: []config.DomainEntry{},
		ProxyPort:          3128,
	}

	result, err := RenderSquidConf(cfg)
	if err != nil {
		t.Fatalf("RenderSquidConf failed: %v", err)
	}

	// Should still have the structural elements even with no domains
	assertContains(t, result, "http_port 3128")
	assertContains(t, result, "external_acl_type cooper_acl")
	assertContains(t, result, "http_access deny all")
}

func TestRenderSquidConf_CustomPort(t *testing.T) {
	cfg := &config.Config{
		WhitelistedDomains: []config.DomainEntry{
			{Domain: ".anthropic.com"},
		},
		ProxyPort: 8080,
	}

	result, err := RenderSquidConf(cfg)
	if err != nil {
		t.Fatalf("RenderSquidConf failed: %v", err)
	}

	assertContains(t, result, "http_port 8080")
}

func TestRenderSquidConf_CustomDomains(t *testing.T) {
	cfg := &config.Config{
		WhitelistedDomains: []config.DomainEntry{
			{Domain: ".example.com", IncludeSubdomains: true, Source: "user"},
			{Domain: "api.myservice.io", IncludeSubdomains: false, Source: "user"},
			{Domain: ".internal.corp", IncludeSubdomains: true, Source: "user"},
		},
		ProxyPort: 3128,
	}

	result, err := RenderSquidConf(cfg)
	if err != nil {
		t.Fatalf("RenderSquidConf failed: %v", err)
	}

	assertContains(t, result, ".example.com")
	assertContains(t, result, "api.myservice.io")
	assertContains(t, result, ".internal.corp")
	// All domains should appear in acl lines.
	assertContains(t, result, "acl allowed_domains dstdomain .example.com")
	assertContains(t, result, "acl allowed_domains dstdomain api.myservice.io")
	assertContains(t, result, "acl allowed_domains dstdomain .internal.corp")
}

func TestRenderSquidConf_SingleDomain(t *testing.T) {
	cfg := &config.Config{
		WhitelistedDomains: []config.DomainEntry{
			{Domain: "registry.npmjs.org", IncludeSubdomains: false, Source: "user"},
		},
		ProxyPort: 3128,
	}

	result, err := RenderSquidConf(cfg)
	if err != nil {
		t.Fatalf("RenderSquidConf failed: %v", err)
	}

	assertContains(t, result, "acl allowed_domains dstdomain registry.npmjs.org")
	// Should still have the deny all at the bottom.
	assertContains(t, result, "http_access deny all")
}

func TestRenderSquidConf_MixedDefaultAndUserDomains(t *testing.T) {
	cfg := &config.Config{
		WhitelistedDomains: []config.DomainEntry{
			{Domain: ".anthropic.com", IncludeSubdomains: true, Source: "default"},
			{Domain: "custom.example.com", IncludeSubdomains: false, Source: "user"},
		},
		ProxyPort: 8080,
	}

	result, err := RenderSquidConf(cfg)
	if err != nil {
		t.Fatalf("RenderSquidConf failed: %v", err)
	}

	assertContains(t, result, ".anthropic.com")
	assertContains(t, result, "custom.example.com")
	assertContains(t, result, "http_port 8080")
}

// ---------------------------------------------------------------------------
// Proxy entrypoint tests (unchanged)
// ---------------------------------------------------------------------------

func TestRenderProxyEntrypoint(t *testing.T) {
	cfg := testConfig()
	result, err := RenderProxyEntrypoint(cfg)
	if err != nil {
		t.Fatalf("RenderProxyEntrypoint failed: %v", err)
	}

	// Should start Squid in background (shell stays alive for SIGHUP)
	assertContains(t, result, "squid -N &")
	assertContains(t, result, "wait $SQUID_PID")

	// Should read rules from socat-rules.json config file
	assertContains(t, result, "socat-rules.json")
	assertContains(t, result, "start_socat_from_config")
	assertContains(t, result, "jq")

	// Should have fallback bridge port from template
	assertContains(t, result, "run_socat 4343")
	assertContains(t, result, "host.docker.internal")

	// Socat should bind on 0.0.0.0 (not 127.0.0.1 like the CLI)
	assertContains(t, result, "bind=0.0.0.0")

	// Should track socat PIDs for clean reload
	assertContains(t, result, "SOCAT_SUPERVISORS")

	// Should have logrotate cron
	assertContains(t, result, "logrotate")

	// Should handle SIGHUP for live reload
	assertContains(t, result, "reload_socat")
	assertContains(t, result, "trap")
	assertContains(t, result, "HUP")
}

func TestRenderProxyEntrypoint_NoPortForwards(t *testing.T) {
	cfg := &config.Config{
		PortForwardRules: []config.PortForwardRule{},
		BridgePort:       4343,
	}

	result, err := RenderProxyEntrypoint(cfg)
	if err != nil {
		t.Fatalf("RenderProxyEntrypoint failed: %v", err)
	}

	// Should still have bridge port as fallback
	assertContains(t, result, "run_socat 4343")

	// Should read from config file
	assertContains(t, result, "socat-rules.json")

	// Should still start squid
	assertContains(t, result, "squid -N")
}

func TestRenderProxyEntrypoint_ConfigDriven(t *testing.T) {
	// Port forwarding rules are now read from socat-rules.json at runtime.
	// The template should NOT contain specific port numbers from rules.
	cfg := &config.Config{
		PortForwardRules: []config.PortForwardRule{
			{ContainerPort: 3000, HostPort: 3000, Description: "web-app"},
		},
		BridgePort: 4343,
	}

	result, err := RenderProxyEntrypoint(cfg)
	if err != nil {
		t.Fatalf("RenderProxyEntrypoint failed: %v", err)
	}

	// Should read from config, not hardcode rules
	assertContains(t, result, "socat-rules.json")
	assertContains(t, result, "start_socat_from_config")
	assertContains(t, result, "host.docker.internal")
	// Bridge port should be present as fallback
	assertContains(t, result, "run_socat 4343")
	// Specific port from rules should NOT be in template
	assertNotContains(t, result, "run_socat 3000")
}

func TestRenderProxyEntrypoint_CustomBridgePort(t *testing.T) {
	cfg := &config.Config{
		PortForwardRules: []config.PortForwardRule{},
		BridgePort:       5555,
	}

	result, err := RenderProxyEntrypoint(cfg)
	if err != nil {
		t.Fatalf("RenderProxyEntrypoint failed: %v", err)
	}

	// Fallback bridge port should use the custom value
	assertContains(t, result, "run_socat 5555")
	assertContains(t, result, `bridge_port=5555`)
}

func TestRenderProxyEntrypoint_ReloadSupport(t *testing.T) {
	cfg := &config.Config{
		PortForwardRules: []config.PortForwardRule{},
		BridgePort:       4343,
	}

	result, err := RenderProxyEntrypoint(cfg)
	if err != nil {
		t.Fatalf("RenderProxyEntrypoint failed: %v", err)
	}

	// Should have SIGHUP reload support
	assertContains(t, result, "reload_socat")
	assertContains(t, result, "SOCAT_SUPERVISORS")
	assertContains(t, result, "trap")
	assertContains(t, result, "HUP")
}

// ---------------------------------------------------------------------------
// WriteProxyTemplates tests (unchanged)
// ---------------------------------------------------------------------------

func TestWriteProxyTemplates(t *testing.T) {
	cfg := testConfig()
	dir := t.TempDir()

	err := WriteProxyTemplates(dir, cfg)
	if err != nil {
		t.Fatalf("WriteProxyTemplates failed: %v", err)
	}

	expectedFiles := []string{
		"proxy.Dockerfile",
		"squid.conf",
		"proxy-entrypoint.sh",
	}

	for _, name := range expectedFiles {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected file %s not found: %v", name, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("file %s is empty", name)
		}
	}

	// Verify proxy-entrypoint.sh is executable
	info, err := os.Stat(filepath.Join(dir, "proxy-entrypoint.sh"))
	if err != nil {
		t.Fatalf("failed to stat proxy-entrypoint.sh: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Error("proxy-entrypoint.sh should be executable")
	}
}

// ---------------------------------------------------------------------------
// ACL helper source tests (unchanged)
// ---------------------------------------------------------------------------

func TestWriteACLHelperSource_DirectoryStructure(t *testing.T) {
	proxyDir := t.TempDir()

	err := WriteACLHelperSource(proxyDir)
	if err != nil {
		t.Fatalf("WriteACLHelperSource failed: %v", err)
	}

	helperDir := filepath.Join(proxyDir, "acl-helper")

	// Check go.mod exists.
	goModPath := filepath.Join(helperDir, "go.mod")
	info, err := os.Stat(goModPath)
	if err != nil {
		t.Fatalf("expected go.mod at %s: %v", goModPath, err)
	}
	if info.Size() == 0 {
		t.Error("go.mod should not be empty")
	}

	// Check cmd/acl-helper/main.go exists.
	mainGoPath := filepath.Join(helperDir, "cmd", "acl-helper", "main.go")
	info, err = os.Stat(mainGoPath)
	if err != nil {
		t.Fatalf("expected main.go at %s: %v", mainGoPath, err)
	}
	if info.Size() == 0 {
		t.Error("main.go should not be empty")
	}

	// Check internal/proxy/helper.go exists.
	helperGoPath := filepath.Join(helperDir, "internal", "proxy", "helper.go")
	info, err = os.Stat(helperGoPath)
	if err != nil {
		t.Fatalf("expected helper.go at %s: %v", helperGoPath, err)
	}
	if info.Size() == 0 {
		t.Error("helper.go should not be empty")
	}
}

func TestWriteACLHelperSource_GoModContent(t *testing.T) {
	proxyDir := t.TempDir()

	err := WriteACLHelperSource(proxyDir)
	if err != nil {
		t.Fatalf("WriteACLHelperSource failed: %v", err)
	}

	goModPath := filepath.Join(proxyDir, "acl-helper", "go.mod")
	data, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("failed to read go.mod: %v", err)
	}

	content := string(data)
	assertContains(t, content, "module github.com/rickchristie/govner/cooper")
	assertContains(t, content, "go 1.24")
}

func TestWriteACLHelperSource_Idempotent(t *testing.T) {
	proxyDir := t.TempDir()

	// Write twice -- should not fail.
	err := WriteACLHelperSource(proxyDir)
	if err != nil {
		t.Fatalf("first WriteACLHelperSource failed: %v", err)
	}
	err = WriteACLHelperSource(proxyDir)
	if err != nil {
		t.Fatalf("second WriteACLHelperSource failed: %v", err)
	}

	// Verify files still exist.
	mainGoPath := filepath.Join(proxyDir, "acl-helper", "cmd", "acl-helper", "main.go")
	if _, err := os.Stat(mainGoPath); err != nil {
		t.Errorf("main.go missing after idempotent write: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Helper function tests (unchanged)
// ---------------------------------------------------------------------------

func TestIsToolEnabled(t *testing.T) {
	tools := []config.ToolConfig{
		{Name: "go", Enabled: true},
		{Name: "python", Enabled: false},
		{Name: "node", Enabled: true},
	}

	if !isToolEnabled(tools, "go") {
		t.Error("expected go to be enabled")
	}
	if isToolEnabled(tools, "python") {
		t.Error("expected python to be disabled")
	}
	if !isToolEnabled(tools, "node") {
		t.Error("expected node to be enabled")
	}
	// Case insensitive
	if !isToolEnabled(tools, "Go") {
		t.Error("expected Go (case insensitive) to be enabled")
	}
}

func TestGetToolVersion(t *testing.T) {
	tools := []config.ToolConfig{
		{Name: "go", Enabled: true, PinnedVersion: "1.24.10"},
		{Name: "python", Enabled: true, HostVersion: "3.12.1"},
		{Name: "node", Enabled: true},
	}

	if v := getToolVersion(tools, "go"); v != "1.24.10" {
		t.Errorf("expected go version 1.24.10, got %s", v)
	}
	if v := getToolVersion(tools, "python"); v != "3.12.1" {
		t.Errorf("expected python version 3.12.1 (host), got %s", v)
	}
	if v := getToolVersion(tools, "node"); v != "" {
		t.Errorf("expected node version empty (no pinned/host), got %s", v)
	}
}
