package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
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

func TestRenderCLIDockerfile_DefaultConfig(t *testing.T) {
	cfg := testConfig()
	result, err := RenderCLIDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderCLIDockerfile failed: %v", err)
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

	// Should have all AI tools
	assertContains(t, result, "claude.ai/install.sh")
	assertContains(t, result, "@github/copilot")
	assertContains(t, result, "@openai/codex")
	assertContains(t, result, "opencode.ai/install")

	// Should have CA cert injection
	assertContains(t, result, "cooper-ca.pem")
	assertContains(t, result, "update-ca-certificates")
	assertContains(t, result, "NODE_EXTRA_CA_CERTS")

	// Should have proxy env vars pointing to cooper-proxy
	assertContains(t, result, "HTTP_PROXY=http://cooper-proxy:3128")
	assertContains(t, result, "HTTPS_PROXY=http://cooper-proxy:3128")

	// Should have cache bust args
	assertContains(t, result, "CACHE_BUST_LANG")
	assertContains(t, result, "CACHE_BUST_AI")

	// Should have user setup
	assertContains(t, result, "USER_UID")
	assertContains(t, result, "USER_GID")

	// Should have OpenCode-specific packages (xvfb, xclip, inotify-tools)
	assertContains(t, result, "xvfb")
	assertContains(t, result, "xclip")
	assertContains(t, result, "inotify-tools")
}

func TestRenderCLIDockerfile_GoDisabled(t *testing.T) {
	cfg := testConfig()
	// Disable Go
	for i := range cfg.ProgrammingTools {
		if cfg.ProgrammingTools[i].Name == "go" {
			cfg.ProgrammingTools[i].Enabled = false
		}
	}

	result, err := RenderCLIDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderCLIDockerfile failed: %v", err)
	}

	// Should use Debian base when Go is disabled
	assertContains(t, result, "debian:bookworm-slim")
	assertNotContains(t, result, "golang:")
}

func TestRenderCLIDockerfile_NoToolsEnabled(t *testing.T) {
	cfg := noToolsConfig()
	result, err := RenderCLIDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderCLIDockerfile failed: %v", err)
	}

	// Should use Debian base
	assertContains(t, result, "debian:bookworm-slim")

	// Should NOT have programming tools
	assertNotContains(t, result, "golang:")
	assertNotContains(t, result, "python3=")
	// Should NOT have AI tools
	assertNotContains(t, result, "claude.ai/install.sh")
	assertNotContains(t, result, "@github/copilot")
	assertNotContains(t, result, "@openai/codex")
	assertNotContains(t, result, "opencode.ai/install")

	// Should NOT have bubblewrap (no Codex)
	assertNotContains(t, result, "bubblewrap")

	// Should NOT have xvfb (no OpenCode)
	assertNotContains(t, result, "xvfb")

	// Should still have base infrastructure
	assertContains(t, result, "cooper-ca.pem")
	assertContains(t, result, "HTTP_PROXY")
}

func TestRenderCLIDockerfile_MinimalConfig(t *testing.T) {
	cfg := minimalConfig()
	result, err := RenderCLIDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderCLIDockerfile failed: %v", err)
	}

	// Should use Go base image
	assertContains(t, result, "golang:1.24.10-bookworm")

	// Only Claude Code should be present
	assertContains(t, result, "claude.ai/install.sh")
	assertNotContains(t, result, "@github/copilot")
	assertNotContains(t, result, "@openai/codex")
	assertNotContains(t, result, "opencode.ai/install")

	// No bubblewrap (no Codex)
	assertNotContains(t, result, "bubblewrap")

	// No xvfb (no OpenCode)
	assertNotContains(t, result, "xvfb")
}

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

	// dns_v4_first removed in Squid 6 — verify the directive is NOT active.
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

func TestRenderEntrypoint(t *testing.T) {
	cfg := testConfig()
	result, err := RenderEntrypoint(cfg)
	if err != nil {
		t.Fatalf("RenderEntrypoint failed: %v", err)
	}

	// Should have auto-approve aliases for all enabled AI tools
	assertContains(t, result, "claude --dangerously-skip-permissions")
	assertContains(t, result, "copilot --allow-all-tools")
	assertContains(t, result, "codex --dangerously-bypass-approvals-and-sandbox")
	assertContains(t, result, "opencode --auto-approve")

	// Should have OpenCode DISPLAY setup
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

func TestRenderEntrypoint_MinimalConfig(t *testing.T) {
	cfg := minimalConfig()
	result, err := RenderEntrypoint(cfg)
	if err != nil {
		t.Fatalf("RenderEntrypoint failed: %v", err)
	}

	// Only Claude Code alias should be present
	assertContains(t, result, "claude --dangerously-skip-permissions")
	assertNotContains(t, result, "copilot --allow-all-tools")
	assertNotContains(t, result, "codex --dangerously-bypass-approvals-and-sandbox")
	assertNotContains(t, result, "opencode --auto-approve")

	// Should NOT have OpenCode DISPLAY setup
	assertNotContains(t, result, "Xvfb")
	assertNotContains(t, result, "opencode.json")

	// Should still have bridge port socat
	assertContains(t, result, "run_socat 4343")

	// Should NOT have user-configured port forwards (none configured)
	assertNotContains(t, result, "run_socat 5432")
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

	// Should NOT have any aliases
	assertNotContains(t, result, "claude --dangerously-skip-permissions")
	assertNotContains(t, result, "copilot --allow-all-tools")
	assertNotContains(t, result, "codex --dangerously-bypass-approvals-and-sandbox")
	assertNotContains(t, result, "opencode --auto-approve")

	// Should still have bridge socat
	assertContains(t, result, "run_socat 4343")

	// Should still have exec
	assertContains(t, result, `exec "$@"`)
}

func TestWriteAllTemplates(t *testing.T) {
	cfg := testConfig()
	dir := t.TempDir()

	err := WriteAllTemplates(dir, cfg)
	if err != nil {
		t.Fatalf("WriteAllTemplates failed: %v", err)
	}

	// Verify all files were written
	expectedFiles := []string{
		"Dockerfile",
		"proxy.Dockerfile",
		"squid.conf",
		"entrypoint.sh",
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

	// Verify entrypoint.sh is executable
	info, err := os.Stat(filepath.Join(dir, "entrypoint.sh"))
	if err != nil {
		t.Fatalf("failed to stat entrypoint.sh: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Error("entrypoint.sh should be executable")
	}

	// Verify proxy-entrypoint.sh is executable
	info, err = os.Stat(filepath.Join(dir, "proxy-entrypoint.sh"))
	if err != nil {
		t.Fatalf("failed to stat proxy-entrypoint.sh: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Error("proxy-entrypoint.sh should be executable")
	}
}

func TestWriteAllTemplates_CreatesDirectory(t *testing.T) {
	cfg := minimalConfig()
	dir := filepath.Join(t.TempDir(), "nested", "output")

	err := WriteAllTemplates(dir, cfg)
	if err != nil {
		t.Fatalf("WriteAllTemplates failed: %v", err)
	}

	// Directory should have been created
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("output directory was not created: %v", err)
	}
}

func TestWriteAllTemplates_FileContents(t *testing.T) {
	cfg := testConfig()
	dir := t.TempDir()

	err := WriteAllTemplates(dir, cfg)
	if err != nil {
		t.Fatalf("WriteAllTemplates failed: %v", err)
	}

	// Read back the CLI Dockerfile and verify content
	data, err := os.ReadFile(filepath.Join(dir, "Dockerfile"))
	if err != nil {
		t.Fatalf("failed to read Dockerfile: %v", err)
	}
	assertContains(t, string(data), "golang:1.24.10-bookworm")

	// Read back squid.conf and verify content
	data, err = os.ReadFile(filepath.Join(dir, "squid.conf"))
	if err != nil {
		t.Fatalf("failed to read squid.conf: %v", err)
	}
	assertContains(t, string(data), ".anthropic.com")
	assertContains(t, string(data), "custom.example.com")

	// Read back entrypoint.sh and verify content
	data, err = os.ReadFile(filepath.Join(dir, "entrypoint.sh"))
	if err != nil {
		t.Fatalf("failed to read entrypoint.sh: %v", err)
	}
	assertContains(t, string(data), "run_socat 4343") // fallback bridge port
	assertContains(t, string(data), "socat-rules.json") // config-driven port forwarding

	// Read back proxy-entrypoint.sh and verify content
	data, err = os.ReadFile(filepath.Join(dir, "proxy-entrypoint.sh"))
	if err != nil {
		t.Fatalf("failed to read proxy-entrypoint.sh: %v", err)
	}
	assertContains(t, string(data), "squid -N")
	assertContains(t, string(data), "host.docker.internal")
}

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

func TestRenderCLIDockerfile_PinnedAIVersions(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AITools = []config.ToolConfig{
		{Name: "claude", Enabled: true, Mode: config.ModeLatest, PinnedVersion: "2.1.86"},
		{Name: "copilot", Enabled: true, Mode: config.ModePin, PinnedVersion: "0.7.2"},
		{Name: "codex", Enabled: true, Mode: config.ModeMirror, HostVersion: "0.117.0"},
		{Name: "opencode", Enabled: true, Mode: config.ModeLatest, PinnedVersion: "1.3.0"},
	}
	cfg.ProgrammingTools = []config.ToolConfig{
		{Name: "node", Enabled: true, Mode: config.ModeLatest, PinnedVersion: "22.12.0"},
	}

	output, err := RenderCLIDockerfile(cfg)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	// Claude should use npm with pinned version (not curl installer).
	// Claude uses curl installer with version arg (not npm).
	if !strings.Contains(output, "claude.ai/install.sh | bash -s -- 2.1.86") {
		t.Error("expected pinned claude curl install with version 2.1.86")
	}
	// Copilot pinned.
	if !strings.Contains(output, "@github/copilot@0.7.2") {
		t.Error("expected pinned copilot install @0.7.2")
	}
	// Codex with host version.
	if !strings.Contains(output, "@openai/codex@0.117.0") {
		t.Error("expected pinned codex install @0.117.0")
	}
	// OpenCode uses curl installer (no version pinning via npm).
	if !strings.Contains(output, "opencode.ai/install") {
		t.Error("expected opencode curl installer")
	}
}

func TestRenderCLIDockerfile_UnpinnedAIFallback(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AITools = []config.ToolConfig{
		{Name: "claude", Enabled: true, Mode: config.ModeLatest},
		{Name: "copilot", Enabled: true, Mode: config.ModeLatest},
	}
	cfg.ProgrammingTools = []config.ToolConfig{
		{Name: "node", Enabled: true, Mode: config.ModeLatest, PinnedVersion: "22.12.0"},
	}

	output, err := RenderCLIDockerfile(cfg)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	// Claude with no version should use curl installer.
	if !strings.Contains(output, "claude.ai/install.sh") {
		t.Error("expected curl installer for unpinned claude")
	}
	// Copilot with no version should install bare (no @version).
	if !strings.Contains(output, "npm install -g @github/copilot\n") {
		t.Error("expected bare copilot install without version pin")
	}
}

// ---------------------------------------------------------------------------
// RenderProxyEntrypoint with port forwarding rules
// ---------------------------------------------------------------------------

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
// RenderSquidConf with custom domain whitelist
// ---------------------------------------------------------------------------

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
// WriteACLHelperSource directory structure
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

func TestBuildCLIDockerfileData(t *testing.T) {
	cfg := testConfig()
	data := buildCLIDockerfileData(cfg)

	if !data.HasGo {
		t.Error("expected HasGo=true")
	}
	if data.GoVersion != "1.24.10" {
		t.Errorf("expected GoVersion=1.24.10, got %q", data.GoVersion)
	}
	if !data.HasNode {
		t.Error("expected HasNode=true")
	}
	if !data.HasPython {
		t.Error("expected HasPython=true")
	}
	if !data.HasClaudeCode {
		t.Error("expected HasClaudeCode=true")
	}
	if !data.HasCopilot {
		t.Error("expected HasCopilot=true")
	}
	if !data.HasCodex {
		t.Error("expected HasCodex=true")
	}
	if !data.HasOpenCode {
		t.Error("expected HasOpenCode=true")
	}
	if data.ProxyPort != 3128 {
		t.Errorf("expected ProxyPort=3128, got %d", data.ProxyPort)
	}
}

func TestBuildCLIDockerfileData_NoTools(t *testing.T) {
	cfg := noToolsConfig()
	data := buildCLIDockerfileData(cfg)

	if data.HasGo {
		t.Error("expected HasGo=false")
	}
	if data.HasNode {
		t.Error("expected HasNode=false")
	}
	if data.HasClaudeCode {
		t.Error("expected HasClaudeCode=false")
	}
}
