package templates

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/rickchristie/govner/cooper/internal/aclsrc"
	"github.com/rickchristie/govner/cooper/internal/config"
)

//go:embed *.tmpl
var templateFS embed.FS

// cliDockerfileData holds template data for the CLI Dockerfile.
type cliDockerfileData struct {
	HasGo       bool
	GoVersion   string
	HasNode     bool
	NodeVersion string
	HasPython   bool
	PythonVersion string
	HasRust     bool
	RustVersion string

	HasClaudeCode    bool
	ClaudeVersion    string // empty = install latest
	HasCopilot       bool
	CopilotVersion   string
	HasCodex         bool
	CodexVersion     string
	HasOpenCode      bool
	OpenCodeVersion  string

	ProxyPort int
}

// proxyDockerfileData holds template data for the proxy Dockerfile.
type proxyDockerfileData struct {
	ProxyPort int
}

// squidConfData holds template data for the Squid configuration.
type squidConfData struct {
	ProxyPort          int
	WhitelistedDomains []config.DomainEntry
}

// entrypointData holds template data for the CLI entrypoint script.
// Port forwarding rules are read from /etc/cooper/socat-rules.json at runtime,
// not baked into the template. BridgePort is kept as a fallback default.
type entrypointData struct {
	HasClaudeCode bool
	HasCopilot    bool
	HasCodex      bool
	HasOpenCode   bool
	BridgePort    int
}

// proxyEntrypointData holds template data for the proxy entrypoint script.
// Port forwarding rules are read from /etc/cooper/socat-rules.json at runtime,
// not baked into the template. BridgePort is kept as a fallback default.
type proxyEntrypointData struct {
	BridgePort int
}

// isToolEnabled checks if a tool with the given name is enabled in a slice of ToolConfig.
func isToolEnabled(tools []config.ToolConfig, name string) bool {
	for _, t := range tools {
		if strings.EqualFold(t.Name, name) && t.Enabled {
			return true
		}
	}
	return false
}

// getToolVersion returns the pinned or host version for a tool, or empty string if not found.
func getToolVersion(tools []config.ToolConfig, name string) string {
	for _, t := range tools {
		if strings.EqualFold(t.Name, name) && t.Enabled {
			if t.PinnedVersion != "" {
				return t.PinnedVersion
			}
			if t.HostVersion != "" {
				return t.HostVersion
			}
			// No concrete version — return empty so template uses default install method.
			return ""
		}
	}
	return ""
}

// buildCLIDockerfileData constructs template data from a Config.
func buildCLIDockerfileData(cfg *config.Config) cliDockerfileData {
	return cliDockerfileData{
		HasGo:         isToolEnabled(cfg.ProgrammingTools, "go"),
		GoVersion:     getToolVersion(cfg.ProgrammingTools, "go"),
		HasNode:       isToolEnabled(cfg.ProgrammingTools, "node"),
		NodeVersion:   getToolVersion(cfg.ProgrammingTools, "node"),
		HasPython:     isToolEnabled(cfg.ProgrammingTools, "python"),
		PythonVersion: getToolVersion(cfg.ProgrammingTools, "python"),
		HasRust:       isToolEnabled(cfg.ProgrammingTools, "rust"),
		RustVersion:   getToolVersion(cfg.ProgrammingTools, "rust"),
		HasClaudeCode:   isToolEnabled(cfg.AITools, "claude"),
		ClaudeVersion:   getToolVersion(cfg.AITools, "claude"),
		HasCopilot:      isToolEnabled(cfg.AITools, "copilot"),
		CopilotVersion:  getToolVersion(cfg.AITools, "copilot"),
		HasCodex:        isToolEnabled(cfg.AITools, "codex"),
		CodexVersion:    getToolVersion(cfg.AITools, "codex"),
		HasOpenCode:     isToolEnabled(cfg.AITools, "opencode"),
		OpenCodeVersion: getToolVersion(cfg.AITools, "opencode"),
		ProxyPort:       cfg.ProxyPort,
	}
}

// RenderCLIDockerfile renders the CLI container Dockerfile from config.
func RenderCLIDockerfile(cfg *config.Config) (string, error) {
	tmpl, err := template.ParseFS(templateFS, "cli.Dockerfile.tmpl")
	if err != nil {
		return "", fmt.Errorf("failed to parse CLI Dockerfile template: %w", err)
	}

	data := buildCLIDockerfileData(cfg)

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute CLI Dockerfile template: %w", err)
	}

	return buf.String(), nil
}

// RenderProxyDockerfile renders the proxy container Dockerfile from config.
func RenderProxyDockerfile(cfg *config.Config) (string, error) {
	tmpl, err := template.ParseFS(templateFS, "proxy.Dockerfile.tmpl")
	if err != nil {
		return "", fmt.Errorf("failed to parse proxy Dockerfile template: %w", err)
	}

	data := proxyDockerfileData{
		ProxyPort: cfg.ProxyPort,
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute proxy Dockerfile template: %w", err)
	}

	return buf.String(), nil
}

// RenderSquidConf renders the Squid proxy configuration from config.
func RenderSquidConf(cfg *config.Config) (string, error) {
	tmpl, err := template.ParseFS(templateFS, "squid.conf.tmpl")
	if err != nil {
		return "", fmt.Errorf("failed to parse squid.conf template: %w", err)
	}

	data := squidConfData{
		ProxyPort:          cfg.ProxyPort,
		WhitelistedDomains: cfg.WhitelistedDomains,
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute squid.conf template: %w", err)
	}

	return buf.String(), nil
}

// RenderProxyEntrypoint renders the proxy container entrypoint script from config.
func RenderProxyEntrypoint(cfg *config.Config) (string, error) {
	tmpl, err := template.ParseFS(templateFS, "proxy-entrypoint.sh.tmpl")
	if err != nil {
		return "", fmt.Errorf("failed to parse proxy-entrypoint.sh template: %w", err)
	}

	data := proxyEntrypointData{
		BridgePort: cfg.BridgePort,
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute proxy-entrypoint.sh template: %w", err)
	}

	return buf.String(), nil
}

// RenderEntrypoint renders the CLI container entrypoint script from config.
func RenderEntrypoint(cfg *config.Config) (string, error) {
	tmpl, err := template.ParseFS(templateFS, "entrypoint.sh.tmpl")
	if err != nil {
		return "", fmt.Errorf("failed to parse entrypoint.sh template: %w", err)
	}

	data := entrypointData{
		HasClaudeCode: isToolEnabled(cfg.AITools, "claude"),
		HasCopilot:    isToolEnabled(cfg.AITools, "copilot"),
		HasCodex:      isToolEnabled(cfg.AITools, "codex"),
		HasOpenCode:   isToolEnabled(cfg.AITools, "opencode"),
		BridgePort:    cfg.BridgePort,
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute entrypoint.sh template: %w", err)
	}

	return buf.String(), nil
}

// WriteAllTemplates writes all generated files to the output directory.
// The directory is created if it does not exist.
func WriteAllTemplates(dir string, cfg *config.Config) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Generate and write CLI Dockerfile
	cliDockerfile, err := RenderCLIDockerfile(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(cliDockerfile), 0644); err != nil {
		return fmt.Errorf("failed to write CLI Dockerfile: %w", err)
	}

	// Generate and write proxy Dockerfile
	proxyDockerfile, err := RenderProxyDockerfile(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "proxy.Dockerfile"), []byte(proxyDockerfile), 0644); err != nil {
		return fmt.Errorf("failed to write proxy Dockerfile: %w", err)
	}

	// Generate and write squid.conf
	squidConf, err := RenderSquidConf(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "squid.conf"), []byte(squidConf), 0644); err != nil {
		return fmt.Errorf("failed to write squid.conf: %w", err)
	}

	// Generate and write entrypoint.sh
	entrypoint, err := RenderEntrypoint(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "entrypoint.sh"), []byte(entrypoint), 0755); err != nil {
		return fmt.Errorf("failed to write entrypoint.sh: %w", err)
	}

	// Generate and write proxy-entrypoint.sh
	proxyEntrypoint, err := RenderProxyEntrypoint(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "proxy-entrypoint.sh"), []byte(proxyEntrypoint), 0755); err != nil {
		return fmt.Errorf("failed to write proxy-entrypoint.sh: %w", err)
	}

	return nil
}

// WriteACLHelperSource writes the ACL helper Go source into the proxy build context
// as a self-contained Go module at {proxyDir}/acl-helper/. The source is the exact
// same code from cmd/acl-helper/main.go and internal/proxy/helper.go, embedded via
// the aclsrc package. A go.mod with a replace directive maps the import path locally.
//
// This allows the proxy Dockerfile to compile the helper inside Docker (multi-stage
// build), making `cooper build` self-contained — no host Go installation required.
// A test in aclsrc/ verifies the embedded copies match the originals.
func WriteACLHelperSource(proxyDir string) error {
	helperDir := filepath.Join(proxyDir, "acl-helper")
	cmdDir := filepath.Join(helperDir, "cmd", "acl-helper")
	proxyPkgDir := filepath.Join(helperDir, "internal", "proxy")

	for _, d := range []string{cmdDir, proxyPkgDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("create acl-helper dirs: %w", err)
		}
	}

	// go.mod — same module path as the real repo so import paths resolve locally.
	goMod := `module github.com/rickchristie/govner/cooper

go 1.24
`
	if err := os.WriteFile(filepath.Join(helperDir, "go.mod"), []byte(goMod), 0644); err != nil {
		return fmt.Errorf("write go.mod: %w", err)
	}

	// cmd/acl-helper/main.go — exact copy embedded at compile time.
	if err := os.WriteFile(filepath.Join(cmdDir, "main.go"), aclsrc.MainGo, 0644); err != nil {
		return fmt.Errorf("write main.go: %w", err)
	}

	// internal/proxy/helper.go — exact copy embedded at compile time.
	if err := os.WriteFile(filepath.Join(proxyPkgDir, "helper.go"), aclsrc.HelperGo, 0644); err != nil {
		return fmt.Errorf("write helper.go: %w", err)
	}

	return nil
}
