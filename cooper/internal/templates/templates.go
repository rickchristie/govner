package templates

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/rickchristie/govner/cooper/internal/aclsrc"
	"github.com/rickchristie/govner/cooper/internal/aitool"
	"github.com/rickchristie/govner/cooper/internal/clipboard"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/x11src"
)

//go:embed *.tmpl
var templateFS embed.FS

//go:embed doctor.sh
var doctorScript []byte

//go:embed ERR_ACCESS_DENIED
var errAccessDenied []byte

// baseDockerfileData holds template data for the base image Dockerfile.
type baseDockerfileData struct {
	HasGo        bool
	GoVersion    string
	GoPath       string
	GoBinDir     string
	GoModCache   string
	GoBuildCache string
	HasNode      bool
	NodeVersion  string
	HasPython    bool
	// Runtime deps flags (needed even though tools install in child images)
	HasCodex    bool // Controls bubblewrap build
	HasOpenCode bool // Controls xvfb/xclip install
	ProxyPort   int

	GoLSPVersion          string
	NodeTSLSPVersion      string
	NodeTypeScriptVersion string
	PythonPyrightVersion  string
	PythonPylspVersion    string
}

// cliToolDockerfileData holds template data for per-tool Dockerfiles.
type cliToolDockerfileData struct {
	BaseImage       string   // "cooper-base" or "{prefix}cooper-base"
	ToolName        string   // "claude", "copilot", "codex", "opencode", or "grok"
	ToolDisplayName string   // "Claude Code", "Copilot CLI", etc.
	Version         string   // Resolved image version
	AutoApproveFlag string   // Tool-specific auto-approve CLI flag
	InstallCommands string   // Pre-rendered install RUN commands
	ToolDirs        []string // Directories to create (e.g. /home/user/.claude)
	ProxyPort       int      // Proxy port to restore after install
	ClipboardMode   string   // Clipboard bridge mode: "shim", "x11", or "auto"
	RuntimeEnvs     []runtimeEnv
}

// runtimeEnv is a Dockerfile ENV entry set after the tool is installed.
type runtimeEnv struct {
	Name  string
	Value string
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
	HasGo            bool
	GoBinDir         string
	BridgePort       int
	ClipboardEnabled bool
}

// proxyEntrypointData holds template data for the proxy entrypoint script.
// Port forwarding rules are read from /etc/cooper/socat-rules.json at runtime,
// not baked into the template. BridgePort is kept as a fallback default.
type proxyEntrypointData struct {
	BridgePort int
}

// anyAIToolEnabled returns true if at least one AI tool is enabled.
func anyAIToolEnabled(tools []config.ToolConfig) bool {
	for _, t := range tools {
		if t.Enabled {
			return true
		}
	}
	return false
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

// opencodeReleaseTag maps a resolved npm/config version onto the GitHub
// release tag. Official artifacts live at
// https://github.com/anomalyco/opencode/releases/download/v<ver>/...
// (the unprefixed /<ver>/ path 404s).
func opencodeReleaseTag(version string) string {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	if version == "" {
		return ""
	}
	return "v" + version
}

// getToolVersion returns the pinned or host version for a tool, or empty string if not found.
func getToolVersion(tools []config.ToolConfig, name string) string {
	for _, t := range tools {
		if strings.EqualFold(t.Name, name) && t.Enabled {
			switch t.Mode {
			case config.ModeMirror:
				return t.HostVersion
			case config.ModePin, config.ModeLatest:
				return t.PinnedVersion
			default:
				if t.PinnedVersion != "" {
					return t.PinnedVersion
				}
				if t.HostVersion != "" {
					return t.HostVersion
				}
				return ""
			}
		}
	}
	return ""
}

// buildBaseDockerfileData constructs template data for the base image from a Config.
func buildBaseDockerfileData(cfg *config.Config, implicit []config.ImplicitToolConfig) (baseDockerfileData, error) {
	goVersion, _, err := config.EffectiveProgrammingToolVersion(cfg, "go")
	if err != nil {
		return baseDockerfileData{}, err
	}
	nodeVersion, err := config.EffectiveBaseNodeVersion(cfg)
	if err != nil {
		return baseDockerfileData{}, err
	}

	data := baseDockerfileData{
		HasGo:        isToolEnabled(cfg.ProgrammingTools, "go"),
		GoVersion:    goVersion,
		GoPath:       docker.BarrelGoPath,
		GoBinDir:     docker.BarrelGoBinDir,
		GoModCache:   docker.BarrelGoModCacheDir,
		GoBuildCache: docker.BarrelGoBuildCacheDir,
		HasNode:      isToolEnabled(cfg.ProgrammingTools, "node"),
		NodeVersion:  nodeVersion,
		HasPython:    isToolEnabled(cfg.ProgrammingTools, "python"),
		HasCodex:     isToolEnabled(cfg.AITools, "codex"),
		HasOpenCode:  isToolEnabled(cfg.AITools, "opencode"),
		ProxyPort:    cfg.ProxyPort,
	}
	for _, tool := range implicit {
		switch tool.Name {
		case "gopls":
			data.GoLSPVersion = tool.ContainerVersion
		case "typescript-language-server":
			data.NodeTSLSPVersion = tool.ContainerVersion
		case "typescript":
			data.NodeTypeScriptVersion = tool.ContainerVersion
		case "pyright":
			data.PythonPyrightVersion = tool.ContainerVersion
		case "python-lsp-server":
			data.PythonPylspVersion = tool.ContainerVersion
		}
	}
	return data, nil
}

// RenderBaseDockerfile renders the base image Dockerfile from config.
func RenderBaseDockerfile(cfg *config.Config, implicit []config.ImplicitToolConfig) (string, error) {
	tmpl, err := template.ParseFS(templateFS, "base.Dockerfile.tmpl")
	if err != nil {
		return "", fmt.Errorf("failed to parse base Dockerfile template: %w", err)
	}

	data, err := buildBaseDockerfileData(cfg, implicit)
	if err != nil {
		return "", fmt.Errorf("failed to build base Dockerfile data: %w", err)
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute base Dockerfile template: %w", err)
	}

	return buf.String(), nil
}

func toolHomeDirs(def aitool.Definition) []string {
	dirs := make([]string, 0, len(def.HomeDirs))
	for _, rel := range def.HomeDirs {
		dirs = append(dirs, filepath.Join(docker.BarrelHomeDir, filepath.FromSlash(rel)))
	}
	return dirs
}

// renderInstallCommands returns the Dockerfile RUN commands for installing a tool.
func renderInstallCommands(toolName, version string) (string, error) {
	switch toolName {
	case "claude":
		if version != "" {
			// Do not run `claude install` for a pinned version. It upgrades to latest.
			// Download before execution so a failed curl cannot become a successful
			// empty shell pipeline.
			return fmt.Sprintf("RUN curl -fsSL --http1.1 --retry 5 --retry-all-errors https://claude.ai/install.sh --output /tmp/claude-install.sh && \\\n    bash /tmp/claude-install.sh %s && \\\n    rm -f /tmp/claude-install.sh", version), nil
		}
		return "RUN curl -fsSL --http1.1 --retry 5 --retry-all-errors https://claude.ai/install.sh --output /tmp/claude-install.sh && \\\n    bash /tmp/claude-install.sh && \\\n    rm -f /tmp/claude-install.sh && \\\n    /home/user/.local/bin/claude install", nil
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
		if strings.TrimSpace(version) == "" {
			return "", fmt.Errorf("OpenCode image requires a resolved version; mirror, latest, and pin must resolve before rendering")
		}
		// Official versioned artifact from GitHub Releases. The convenience
		// URL https://opencode.ai/install is only a 307 to a moving raw
		// script that then fetches this same tarball; that wrapper 429s in
		// Docker and `curl | bash` is not fail-closed.
		// Install into ~/.local/bin so the runtime ~/.opencode state mount
		// cannot hide or replace the pinned binary.
		return fmt.Sprintf(`ARG TARGETARCH
RUN set -eu; \
    case "${TARGETARCH}" in \
      amd64) oc_arch=x64 ;; \
      arm64) oc_arch=arm64 ;; \
      "") case "$(uname -m)" in \
            x86_64) oc_arch=x64 ;; \
            aarch64|arm64) oc_arch=arm64 ;; \
            *) echo "unsupported OpenCode architecture: $(uname -m)" >&2; exit 1 ;; \
          esac ;; \
      *) echo "unsupported OpenCode architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    mkdir -p /home/user/.config/opencode /home/user/.local/bin /tmp/opencode-extract; \
    curl --fail --show-error --silent --location --http1.1 --retry 5 --retry-all-errors \
      "https://github.com/anomalyco/opencode/releases/download/%s/opencode-linux-${oc_arch}.tar.gz" \
      --output /tmp/opencode.tar.gz; \
    tar -xzf /tmp/opencode.tar.gz -C /tmp/opencode-extract; \
    if [ -f /tmp/opencode-extract/opencode ]; then \
      src=/tmp/opencode-extract/opencode; \
    elif [ -f /tmp/opencode-extract/bin/opencode ]; then \
      src=/tmp/opencode-extract/bin/opencode; \
    else \
      echo "opencode binary missing from release tarball" >&2; \
      find /tmp/opencode-extract -ls >&2; \
      exit 1; \
    fi; \
    cp "$src" /home/user/.local/bin/opencode; \
    chmod 0755 /home/user/.local/bin/opencode; \
    rm -rf /tmp/opencode.tar.gz /tmp/opencode-extract; \
    /home/user/.local/bin/opencode --version`, opencodeReleaseTag(version)), nil
	case "grok":
		if strings.TrimSpace(version) == "" {
			return "", fmt.Errorf("Grok image requires a resolved version; mirror, latest, and pin must resolve before rendering")
		}
		// Download the immutable official artifact directly into ~/.local/bin.
		// Do not run the moving installer and do not install under ~/.grok,
		// because the runtime state-root mount would hide that path.
		return fmt.Sprintf(`ARG TARGETARCH
RUN set -eu; \
    case "${TARGETARCH}" in \
      amd64) grok_arch=x86_64 ;; \
      arm64) grok_arch=aarch64 ;; \
      "") case "$(uname -m)" in \
            x86_64) grok_arch=x86_64 ;; \
            aarch64|arm64) grok_arch=aarch64 ;; \
            *) echo "unsupported Grok architecture: $(uname -m)" >&2; exit 1 ;; \
          esac ;; \
      *) echo "unsupported Grok architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl --fail --show-error --silent --location --retry 3 \
      "https://x.ai/cli/grok-%s-linux-${grok_arch}" \
      --output /home/user/.local/bin/grok; \
    chmod 0755 /home/user/.local/bin/grok; \
    /home/user/.local/bin/grok --version`, version), nil
	default:
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}
}

// RenderCLIToolDockerfile renders a per-tool Dockerfile from config and tool name.
func RenderCLIToolDockerfile(cfg *config.Config, toolName string) (string, error) {
	def, ok := aitool.Lookup(toolName)
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
		AutoApproveFlag: def.AutoApproveArgs,
		InstallCommands: installCmds,
		ToolDirs:        toolHomeDirs(def),
		ProxyPort:       cfg.ProxyPort,
		ClipboardMode:   aitool.ClipboardMode(toolName),
		RuntimeEnvs:     toolRuntimeEnvs(toolName),
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
		HasGo:            isToolEnabled(cfg.ProgrammingTools, "go"),
		GoBinDir:         docker.BarrelGoBinDir,
		BridgePort:       cfg.BridgePort,
		ClipboardEnabled: anyAIToolEnabled(cfg.AITools),
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute entrypoint.sh template: %w", err)
	}

	return buf.String(), nil
}

func toolRuntimeEnvs(toolName string) []runtimeEnv {
	if toolName != "grok" {
		return nil
	}
	return []runtimeEnv{
		// Map any host GROK_HOME root to one stable container path.
		{Name: "GROK_HOME", Value: docker.BarrelGrokStateRoot},
		// Keep transient leader transport in the per-barrel /tmp mount. This
		// prevents a barrel from attaching to a host Grok process through the
		// leader socket in the shared state root.
		{Name: "GROK_LEADER_SOCKET", Value: docker.BarrelGrokLeaderSocket},
	}
}

// generatedCLIDockerfilePrefix is the first two lines Cooper writes into a
// built-in CLI Dockerfile. Display name is included so the marker matches the
// generated file exactly.
func generatedCLIDockerfilePrefix(displayName string) string {
	return "# Cooper CLI: " + displayName + " - Generated by cooper configure\n# DO NOT EDIT - This file is regenerated by cooper.\n"
}

// isGeneratedGrokOutputDir reports whether toolDir is Cooper-generated Grok
// output. Only the exact generated Dockerfile header makes it replaceable.
func isGeneratedGrokOutputDir(toolDir string) bool {
	def, ok := aitool.Lookup("grok")
	if !ok {
		return false
	}
	data, err := os.ReadFile(filepath.Join(toolDir, "Dockerfile"))
	if err != nil {
		return false
	}
	return strings.HasPrefix(string(data), generatedCLIDockerfilePrefix(def.DisplayName))
}

// ValidateGrokOutputDir fails before a write when cli/grok is user-managed.
// Grok is the only new reserved name. The four older built-in names already
// had reserved directory behavior before Cooper added this migration check.
func ValidateGrokOutputDir(cliDir string) error {
	const toolName = "grok"
	toolDir := filepath.Join(cliDir, toolName)
	info, err := os.Lstat(toolDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat reserved CLI directory %s: %w", toolDir, err)
	}
	if info.IsDir() && isGeneratedGrokOutputDir(toolDir) {
		return nil
	}
	return fmt.Errorf("custom image path %s uses the reserved built-in name %q and is not Cooper-generated. Rename it (for example, to %q) and update its `cooper cli` command. Cooper will not overwrite this path", toolDir, toolName, toolName+"-custom")
}

// WriteAllTemplates writes all generated files for the base image to the base directory,
// and per-tool Dockerfiles to cli/<tool>/ directories.
// baseDir is the path to ~/.cooper/base/.
// cliDir is the path to ~/.cooper/cli/.
func WriteAllTemplates(baseDir, cliDir string, cfg *config.Config, implicit []config.ImplicitToolConfig) error {
	if err := ValidateGrokOutputDir(cliDir); err != nil {
		return err
	}
	// Old Cooper versions put a requirements file in generated Grok output.
	// Remove this host-setting override even when Grok is now disabled.
	grokDir := filepath.Join(cliDir, "grok")
	if isGeneratedGrokOutputDir(grokDir) {
		reqPath := filepath.Join(grokDir, "requirements.toml")
		if err := os.Remove(reqPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove obsolete Grok requirements.toml %s: %w", reqPath, err)
		}
	}
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create base directory: %w", err)
	}

	// Generate and write base Dockerfile.
	baseDockerfile, err := RenderBaseDockerfile(cfg, implicit)
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

	// Write clipboard shim scripts alongside the base image templates.
	// The shims are mounted into barrel containers at /etc/cooper/shims/ and
	// copied to /home/user/.local/bin/ by the entrypoint at startup.
	if err := WriteClipboardShims(baseDir); err != nil {
		return fmt.Errorf("write clipboard shims: %w", err)
	}

	// Write cooper-x11-bridge Go source for multi-stage Docker build.
	if err := WriteX11BridgeSource(baseDir); err != nil {
		return fmt.Errorf("write x11 bridge source: %w", err)
	}

	// Write per-tool Dockerfiles.
	for _, tool := range cfg.AITools {
		if !tool.Enabled {
			continue
		}
		// Skip custom tools (user-managed).
		if !aitool.IsBuiltin(tool.Name) {
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

	// Custom error page for blocked requests.
	if err := os.WriteFile(filepath.Join(dir, "ERR_ACCESS_DENIED"), errAccessDenied, 0644); err != nil {
		return fmt.Errorf("failed to write ERR_ACCESS_DENIED: %w", err)
	}

	return nil
}

// WriteClipboardShims writes the xclip, xsel, and wl-paste shim scripts into
// {dir}/shims/. These are shell scripts generated by the clipboard package that
// intercept clipboard tool invocations and redirect image reads to the Cooper
// clipboard bridge. The shims are designed to be copied into /etc/cooper/shims/
// inside barrel containers and then installed to /home/user/.local/bin/ by the
// entrypoint script at startup.
//
// Real binary paths point to /usr/bin/ where the actual tools are installed in
// the base image. The shims shadow these binaries in PATH via ~/.local/bin/.
func WriteClipboardShims(dir string) error {
	shimsDir := filepath.Join(dir, "shims")
	if err := os.MkdirAll(shimsDir, 0755); err != nil {
		return fmt.Errorf("create shims directory: %w", err)
	}

	shims := map[string]string{
		"xclip":    clipboard.XclipShim("/usr/bin/xclip"),
		"xsel":     clipboard.XselShim("/usr/bin/xsel"),
		"wl-paste": clipboard.WlPasteShim("/usr/bin/wl-paste"),
	}

	for name, content := range shims {
		path := filepath.Join(shimsDir, name)
		if err := os.WriteFile(path, []byte(content), 0755); err != nil {
			return fmt.Errorf("write shim %s: %w", name, err)
		}
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

// WriteX11BridgeSource writes the cooper-x11-bridge Go source into the base
// build context as a self-contained Go module at {baseDir}/x11-bridge-src/.
// This allows the base Dockerfile to compile the bridge inside Docker
// (multi-stage build), making `cooper build` self-contained.
func WriteX11BridgeSource(baseDir string) error {
	srcDir := filepath.Join(baseDir, "x11-bridge-src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		return fmt.Errorf("create x11-bridge-src dir: %w", err)
	}

	// go.mod with xgb dependency.
	goMod := `module cooper-x11-bridge

go 1.25

require github.com/jezek/xgb v1.3.0
`
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"), []byte(goMod), 0644); err != nil {
		return fmt.Errorf("write go.mod: %w", err)
	}

	// go.sum — xgb has no transitive deps, so this is sufficient.
	goSum := `github.com/jezek/xgb v1.3.0 h1:Wa1pn4GVtcmNVAVB6/pnQVJ7xPFZVZ/W1Tc27msDhgI=
github.com/jezek/xgb v1.3.0/go.mod h1:nrhwO0FX/enq75I7Y7G8iN1ubpSGZEiA3v9e9GyRFlk=
`
	if err := os.WriteFile(filepath.Join(srcDir, "go.sum"), []byte(goSum), 0644); err != nil {
		return fmt.Errorf("write go.sum: %w", err)
	}

	// main.go — exact copy embedded at compile time.
	if err := os.WriteFile(filepath.Join(srcDir, "main.go"), x11src.MainGo, 0644); err != nil {
		return fmt.Errorf("write main.go: %w", err)
	}

	return nil
}
