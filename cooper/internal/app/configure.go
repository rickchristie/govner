package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/templates"
)

// ConfigureApp wraps the configure flow for programmatic use and testing.
// It provides methods to:
// - Detect host tools and versions
// - Get/set programming tools and AI CLI tools configuration
// - Get/set whitelist domains and port forwarding rules
// - Get/set proxy/bridge ports
// - Validate configuration
// - Save configuration and generate templates
// - Optionally trigger a build
type ConfigureApp struct {
	cooperDir string
	cfg       *config.Config
	existing  bool // true if loaded from existing config
}

var configureSaveStepNames = []string{
	"Validating configuration...",
	"Preparing cooper directories...",
	"Resolving tool versions...",
	"Writing configuration...",
	"Generating templates...",
	"Ensuring CA certificate...",
}

// SaveStepNames returns the ordered progress labels for ConfigureApp.Save.
func SaveStepNames() []string {
	return append([]string(nil), configureSaveStepNames...)
}

// programmingToolDefs defines the known programming tools and their display names.
var programmingToolDefs = []struct {
	name        string
	displayName string
}{
	{name: "go", displayName: "Go"},
	{name: "node", displayName: "Node.js"},
	{name: "python", displayName: "Python"},
}

// aiToolDefs defines the known AI CLI tools and their display names.
var aiToolDefs = []struct {
	name        string
	displayName string
}{
	{name: "claude", displayName: "Claude Code"},
	{name: "copilot", displayName: "Copilot CLI"},
	{name: "codex", displayName: "Codex CLI"},
	{name: "opencode", displayName: "OpenCode"},
}

// NewConfigureApp creates a new ConfigureApp. It loads an existing config
// from cooperDir/config.json if present, otherwise creates default config.
// The cooperDir is created if it does not exist.
func NewConfigureApp(cooperDir string) (*ConfigureApp, error) {
	if err := os.MkdirAll(cooperDir, 0755); err != nil {
		return nil, fmt.Errorf("create cooper directory: %w", err)
	}

	configPath := filepath.Join(cooperDir, "config.json")
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		// No existing config — use defaults.
		return &ConfigureApp{
			cooperDir: cooperDir,
			cfg:       config.DefaultConfig(),
			existing:  false,
		}, nil
	}

	return &ConfigureApp{
		cooperDir: cooperDir,
		cfg:       cfg,
		existing:  true,
	}, nil
}

// DetectHostTools detects installed programming tools and their versions
// on the host machine. Returns a ToolConfig slice with Name, HostVersion,
// Enabled, and Mode populated. Tools that are detected get Enabled=true
// and Mode=ModeMirror; tools that are not found get Enabled=false.
func (a *ConfigureApp) DetectHostTools() []config.ToolConfig {
	return detectTools(programmingToolDefs)
}

// DetectHostAITools detects installed AI CLI tools and their versions
// on the host machine. Returns a ToolConfig slice with Name, HostVersion,
// Enabled, and Mode populated.
func (a *ConfigureApp) DetectHostAITools() []config.ToolConfig {
	return detectTools(aiToolDefs)
}

// detectTools runs host version detection for each tool definition and
// returns a ToolConfig slice.
func detectTools(defs []struct {
	name        string
	displayName string
}) []config.ToolConfig {
	result := make([]config.ToolConfig, len(defs))
	for i, def := range defs {
		tc := config.ToolConfig{
			Name: def.name,
		}
		v, err := config.DetectHostVersion(def.name)
		if err == nil && v != "" {
			tc.HostVersion = v
			tc.Enabled = true
			tc.Mode = config.ModeMirror
		}
		result[i] = tc
	}
	return result
}

// Config returns a copy of the current configuration.
func (a *ConfigureApp) Config() *config.Config {
	return config.CloneConfig(a.cfg)
}

// SetProgrammingTools updates the programming tools configuration.
func (a *ConfigureApp) SetProgrammingTools(tools []config.ToolConfig) {
	a.cfg.ProgrammingTools = append([]config.ToolConfig(nil), tools...)
}

// SetAITools updates the AI CLI tools configuration.
func (a *ConfigureApp) SetAITools(tools []config.ToolConfig) {
	a.cfg.AITools = append([]config.ToolConfig(nil), tools...)
}

// SetWhitelistedDomains updates the whitelisted domains.
func (a *ConfigureApp) SetWhitelistedDomains(domains []config.DomainEntry) {
	a.cfg.WhitelistedDomains = append([]config.DomainEntry(nil), domains...)
}

// SetPortForwardRules updates the port forwarding rules.
func (a *ConfigureApp) SetPortForwardRules(rules []config.PortForwardRule) {
	a.cfg.PortForwardRules = append([]config.PortForwardRule(nil), rules...)
}

// SetBarrelEnvVars updates the barrel environment variable configuration.
func (a *ConfigureApp) SetBarrelEnvVars(vars []config.BarrelEnvVar) {
	a.cfg.BarrelEnvVars = append([]config.BarrelEnvVar(nil), vars...)
}

// SetProxyPort sets the proxy port.
func (a *ConfigureApp) SetProxyPort(port int) {
	a.cfg.ProxyPort = port
}

// SetBridgePort sets the bridge port.
func (a *ConfigureApp) SetBridgePort(port int) {
	a.cfg.BridgePort = port
}

// SetBarrelSHMSize sets the barrel shared memory size.
func (a *ConfigureApp) SetBarrelSHMSize(size string) {
	a.cfg.BarrelSHMSize = size
}

// Validate validates the full configuration and returns any error.
func (a *ConfigureApp) Validate() error {
	if err := a.cfg.Validate(); err != nil {
		return err
	}
	canonical := config.CanonicalizeBarrelEnvVars(a.cfg.BarrelEnvVars)
	if err := config.ValidateBarrelEnvVars(canonical); err != nil {
		return fmt.Errorf("barrel env validation failed: %w", err)
	}
	return nil
}

// Save writes config.json, generates all templates (CLI Dockerfiles,
// proxy Dockerfile, squid.conf), and ensures the CA certificate exists.
func (a *ConfigureApp) Save() ([]string, error) {
	return a.SaveWithProgress(nil)
}

// SaveWithProgress performs Save while reporting step completion or failure.
func (a *ConfigureApp) SaveWithProgress(onProgress func(step int, total int, name string, err error)) ([]string, error) {
	report := func(step int, err error) {
		if onProgress == nil {
			return
		}
		onProgress(step, len(configureSaveStepNames), configureSaveStepNames[step], err)
	}

	if err := a.cfg.Validate(); err != nil {
		err = fmt.Errorf("validation failed: %w", err)
		report(0, err)
		return nil, err
	}
	canonicalBarrelEnvVars := config.CanonicalizeBarrelEnvVars(a.cfg.BarrelEnvVars)
	if err := config.ValidateBarrelEnvVars(canonicalBarrelEnvVars); err != nil {
		err = fmt.Errorf("barrel env validation failed: %w", err)
		report(0, err)
		return nil, err
	}
	a.cfg.BarrelEnvVars = canonicalBarrelEnvVars
	report(0, nil)

	// Ensure cooperDir and subdirectories exist.
	baseDir := filepath.Join(a.cooperDir, "base")
	cliDir := filepath.Join(a.cooperDir, "cli")
	proxyDir := filepath.Join(a.cooperDir, "proxy")
	for _, dir := range []string{a.cooperDir, baseDir, cliDir, proxyDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			err = fmt.Errorf("create directory %s: %w", dir, err)
			report(1, err)
			return nil, err
		}
	}
	report(1, nil)

	warnings, err := config.RefreshDesiredToolVersions(a.cfg, config.DesiredVersionRefreshOptions{AllowStaleFallback: true})
	if err != nil {
		report(2, err)
		return nil, err
	}
	implicit, implicitWarnings, err := config.ResolveImplicitToolsWithOptions(a.cfg, config.ImplicitToolResolveOptions{AllowStaleFallback: true})
	if err != nil {
		report(2, err)
		return warnings, err
	}
	warnings = append(warnings, implicitWarnings...)
	report(2, nil)

	// Save config.json.
	configPath := filepath.Join(a.cooperDir, "config.json")
	if err := config.SaveConfig(configPath, a.cfg); err != nil {
		err = fmt.Errorf("save config: %w", err)
		report(3, err)
		return warnings, err
	}
	report(3, nil)

	// Generate base + per-tool CLI templates.
	if err := templates.WriteAllTemplates(baseDir, cliDir, a.cfg, implicit); err != nil {
		err = fmt.Errorf("write CLI templates: %w", err)
		report(4, err)
		return warnings, err
	}

	// Generate proxy templates.
	if err := templates.WriteProxyTemplates(proxyDir, a.cfg); err != nil {
		err = fmt.Errorf("write proxy templates: %w", err)
		report(4, err)
		return warnings, err
	}
	report(4, nil)

	// Ensure CA certificate.
	if _, _, err := config.EnsureCA(a.cooperDir); err != nil {
		err = fmt.Errorf("ensure CA: %w", err)
		report(5, err)
		return warnings, err
	}
	report(5, nil)

	return warnings, nil
}

// SaveAndBuild performs Save() and then triggers a Docker build of both
// the proxy and CLI images. This is equivalent to running `cooper build`
// after saving. The build output is written to stderr.
func (a *ConfigureApp) SaveAndBuild() error {
	warnings, err := a.Save()
	if err != nil {
		return err
	}
	for _, warning := range warnings {
		fmt.Fprintln(os.Stderr, warning)
	}

	// The actual Docker build is handled by the caller (main.go runBuild).
	// We return nil here — SaveAndBuild signals intent; main.go chains
	// the build step after the configure wizard returns BuildRequested=true.
	return nil
}

// IsExisting returns true if the ConfigureApp was loaded from an existing
// config.json file, false if it was initialized with defaults.
func (a *ConfigureApp) IsExisting() bool {
	return a.existing
}

// CooperDir returns the path to the cooper configuration directory.
func (a *ConfigureApp) CooperDir() string {
	return a.cooperDir
}
