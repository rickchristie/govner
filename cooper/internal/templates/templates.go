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
	"github.com/rickchristie/govner/cooper/internal/docker"
)

//go:embed *.tmpl
var templateFS embed.FS

//go:embed doctor.sh
var doctorScript []byte

// baseDockerfileData holds template data for the base image Dockerfile.
type baseDockerfileData struct {
	HasGo       bool
	GoVersion   string
	HasNode     bool
	NodeVersion string
	HasPython   bool
	// Runtime deps flags (needed even though tools install in child images)
	HasCodex    bool // Controls bubblewrap build
	HasOpenCode bool // Controls xvfb/xclip install
	ProxyPort   int
}

// cliToolDockerfileData holds template data for per-tool Dockerfiles.
type cliToolDockerfileData struct {
	BaseImage       string   // "cooper-base" or "{prefix}cooper-base"
	ToolName        string   // "claude", "copilot", "codex", "opencode"
	ToolDisplayName string   // "Claude Code", "Copilot CLI", etc.
	Version         string   // Resolved version (empty = latest)
	AutoApproveFlag string   // Tool-specific auto-approve CLI flag
	InstallCommands string   // Pre-rendered install RUN commands
	ToolDirs        []string // Directories to create (e.g. /home/user/.claude)
	ProxyPort       int      // Proxy port to restore after install
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
	HasGo      bool
	BridgePort int
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

// buildBaseDockerfileData constructs template data for the base image from a Config.
func buildBaseDockerfileData(cfg *config.Config) baseDockerfileData {
	return baseDockerfileData{
		HasGo:       isToolEnabled(cfg.ProgrammingTools, "go"),
		GoVersion:   getToolVersion(cfg.ProgrammingTools, "go"),
		HasNode:     isToolEnabled(cfg.ProgrammingTools, "node"),
		NodeVersion: getToolVersion(cfg.ProgrammingTools, "node"),
		HasPython:   isToolEnabled(cfg.ProgrammingTools, "python"),
		HasCodex:    isToolEnabled(cfg.AITools, "codex"),
		HasOpenCode: isToolEnabled(cfg.AITools, "opencode"),
		ProxyPort:   cfg.ProxyPort,
	}
}

// RenderBaseDockerfile renders the base image Dockerfile from config.
func RenderBaseDockerfile(cfg *config.Config) (string, error) {
	tmpl, err := template.ParseFS(templateFS, "base.Dockerfile.tmpl")
	if err != nil {
		return "", fmt.Errorf("failed to parse base Dockerfile template: %w", err)
	}

	data := buildBaseDockerfileData(cfg)

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute base Dockerfile template: %w", err)
	}

	return buf.String(), nil
}

// toolDefinition holds the static metadata for each built-in AI tool.
type toolDefinition struct {
	DisplayName     string
	AutoApproveFlag string
	ToolDirs        []string
}

// builtinTools maps tool name to its static definition.
var builtinTools = map[string]toolDefinition{
	"claude": {
		DisplayName:     "Claude Code",
		AutoApproveFlag: "--dangerously-skip-permissions",
		ToolDirs:        []string{"/home/user/.claude"},
	},
	"copilot": {
		DisplayName:     "Copilot CLI",
		AutoApproveFlag: "--allow-all-tools",
		ToolDirs:        []string{"/home/user/.copilot"},
	},
	"codex": {
		DisplayName:     "Codex CLI",
		AutoApproveFlag: "--dangerously-bypass-approvals-and-sandbox",
		ToolDirs:        []string{"/home/user/.codex"},
	},
	"opencode": {
		DisplayName:     "OpenCode",
		AutoApproveFlag: "--auto-approve",
		ToolDirs:        []string{"/home/user/.config/opencode", "/home/user/.cache/opencode"},
	},
}

// renderInstallCommands returns the Dockerfile RUN commands for installing a tool.
func renderInstallCommands(toolName, version string) (string, error) {
	switch toolName {
	case "claude":
		if version != "" {
			// When version is pinned, don't run `claude install` — it upgrades to latest.
			// The curl installer already handles shell integration setup.
			return fmt.Sprintf("RUN curl -fsSL https://claude.ai/install.sh | bash -s -- %s", version), nil
		}
		return "RUN curl -fsSL https://claude.ai/install.sh | bash && \\\n    /home/user/.local/bin/claude install", nil
	case "copilot":
		if version != "" {
			return fmt.Sprintf("RUN npm install -g @github/copilot@%s", version), nil
		}
		return "RUN npm install -g @github/copilot", nil
	case "codex":
		if version != "" {
			return fmt.Sprintf("RUN npm install -g @openai/codex@%s", version), nil
		}
		return "RUN npm install -g @openai/codex", nil
	case "opencode":
		mkdirCmd := "RUN mkdir -p /home/user/.config/opencode"
		if version != "" {
			return fmt.Sprintf("RUN curl -fsSL https://opencode.ai/install | bash -s -- --version %s\n%s", version, mkdirCmd), nil
		}
		return fmt.Sprintf("RUN curl -fsSL https://opencode.ai/install | bash\n%s", mkdirCmd), nil
	default:
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}
}

// RenderCLIToolDockerfile renders a per-tool Dockerfile from config and tool name.
func RenderCLIToolDockerfile(cfg *config.Config, toolName string) (string, error) {
	def, ok := builtinTools[toolName]
	if !ok {
		return "", fmt.Errorf("unknown AI tool: %s", toolName)
	}

	version := getToolVersion(cfg.AITools, toolName)
	installCmds, err := renderInstallCommands(toolName, version)
	if err != nil {
		return "", err
	}

	tmpl, err := template.ParseFS(templateFS, "cli-tool.Dockerfile.tmpl")
	if err != nil {
		return "", fmt.Errorf("failed to parse cli-tool Dockerfile template: %w", err)
	}

	data := cliToolDockerfileData{
		BaseImage:       docker.GetImageBase(),
		ToolName:        toolName,
		ToolDisplayName: def.DisplayName,
		Version:         version,
		AutoApproveFlag: def.AutoApproveFlag,
		InstallCommands: installCmds,
		ToolDirs:        def.ToolDirs,
		ProxyPort:       cfg.ProxyPort,
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute cli-tool Dockerfile template: %w", err)
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
		HasGo:      isToolEnabled(cfg.ProgrammingTools, "go"),
		BridgePort: cfg.BridgePort,
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute entrypoint.sh template: %w", err)
	}

	return buf.String(), nil
}

// WriteAllTemplates writes all generated files for the base image to the base directory,
// and per-tool Dockerfiles to cli/<tool>/ directories.
// baseDir is the path to ~/.cooper/base/.
// cliDir is the path to ~/.cooper/cli/.
func WriteAllTemplates(baseDir, cliDir string, cfg *config.Config) error {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create base directory: %w", err)
	}

	// Generate and write base Dockerfile.
	baseDockerfile, err := RenderBaseDockerfile(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(baseDir, "Dockerfile"), []byte(baseDockerfile), 0644); err != nil {
		return fmt.Errorf("failed to write base Dockerfile: %w", err)
	}

	// Write doctor.sh diagnostic script (embedded, not generated).
	if err := os.WriteFile(filepath.Join(baseDir, "doctor.sh"), doctorScript, 0755); err != nil {
		return fmt.Errorf("failed to write doctor.sh: %w", err)
	}

	// Generate and write entrypoint.sh.
	entrypoint, err := RenderEntrypoint(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(baseDir, "entrypoint.sh"), []byte(entrypoint), 0755); err != nil {
		return fmt.Errorf("failed to write entrypoint.sh: %w", err)
	}

	// Write per-tool Dockerfiles.
	for _, tool := range cfg.AITools {
		if !tool.Enabled {
			continue
		}
		// Skip custom tools (user-managed).
		if _, ok := builtinTools[tool.Name]; !ok {
			continue
		}
		toolDir := filepath.Join(cliDir, tool.Name)
		if err := os.MkdirAll(toolDir, 0755); err != nil {
			return fmt.Errorf("failed to create tool directory %s: %w", tool.Name, err)
		}
		dockerfile, err := RenderCLIToolDockerfile(cfg, tool.Name)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(toolDir, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
			return fmt.Errorf("failed to write %s Dockerfile: %w", tool.Name, err)
		}
	}

	return nil
}

// WriteProxyTemplates writes proxy-specific templates (proxy.Dockerfile,
// squid.conf, proxy-entrypoint.sh) into the given directory.
func WriteProxyTemplates(dir string, cfg *config.Config) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create proxy directory: %w", err)
	}

	proxyDockerfile, err := RenderProxyDockerfile(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "proxy.Dockerfile"), []byte(proxyDockerfile), 0644); err != nil {
		return fmt.Errorf("failed to write proxy.Dockerfile: %w", err)
	}

	squidConf, err := RenderSquidConf(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "squid.conf"), []byte(squidConf), 0644); err != nil {
		return fmt.Errorf("failed to write squid.conf: %w", err)
	}

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
