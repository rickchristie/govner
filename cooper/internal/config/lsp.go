package config

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultBaseNodeVersion is the Node runtime installed in cooper-base when
	// the node programming tool is disabled. Python's npm-based tooling still
	// relies on this runtime.
	DefaultBaseNodeVersion = "22.12.0"

	ImplicitToolKindLSP     = "lsp"
	ImplicitToolKindSupport = "support"
)

// ImplicitToolConfig tracks tooling that Cooper installs automatically as part
// of a programming language selection. These are built-state records, not user
// version preferences.
type ImplicitToolConfig struct {
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	ParentTool       string `json:"parent_tool"`
	Binary           string `json:"binary,omitempty"`
	ContainerVersion string `json:"container_version,omitempty"`
}

// DesiredVersionRefreshOptions controls how strictly Cooper refreshes the
// concrete desired versions used to render truthful templates.
type DesiredVersionRefreshOptions struct {
	AllowStaleFallback bool
}

// ImplicitToolResolveOptions controls whether implicit-tool resolution may fall
// back to last-built versions when configure save-only is offline.
type ImplicitToolResolveOptions struct {
	AllowStaleFallback bool
}

// Test hooks for implicit-tool resolution. Higher-level tests override these so
// save/build/update flows stay deterministic without live registry access.
var (
	GoplsLatestResolver                = ResolveGoplsLatest
	NPMPackageLatestResolver           = ResolveNPMPackageLatest
	NPMPackageMetadataResolver         = ResolveNPMPackageMetadata
	PyPIPackageLatestResolver          = ResolvePyPIPackageLatest
	PyPIPackageVersionMetadataResolver = ResolvePyPIPackageVersionMetadata
)

var (
	goplsLatestURL = "https://proxy.golang.org/golang.org/x/tools/gopls/@latest"
	pyPIBaseURL    = "https://pypi.org/pypi"
)

type goModuleLatestResponse struct {
	Version string `json:"Version"`
}

// PyPIPackageMetadata is the subset of PyPI metadata used by implicit-tool
// compatibility checks.
type PyPIPackageMetadata struct {
	Version        string
	RequiresPython string
}

type pyPIPackageResponse struct {
	Info struct {
		Version        string `json:"version"`
		RequiresPython string `json:"requires_python"`
	} `json:"info"`
}

type comparableVersion struct {
	major int
	minor int
	patch int
}

// ResolveGoplsLatest fetches the latest stable gopls release from the Go module proxy.
func ResolveGoplsLatest() (string, error) {
	body, err := httpGet(goplsLatestURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch gopls latest version: %w", err)
	}

	var resp goModuleLatestResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("failed to parse gopls latest version JSON: %w", err)
	}
	if strings.TrimSpace(resp.Version) == "" {
		return "", fmt.Errorf("gopls latest response did not include a version")
	}
	return resp.Version, nil
}

// ResolvePyPIPackageLatest fetches the latest published version of a PyPI package.
func ResolvePyPIPackageLatest(packageName string) (string, error) {
	meta, err := ResolvePyPIPackageVersionMetadata(packageName, "")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(meta.Version) == "" {
		return "", fmt.Errorf("pypi package %q did not return a latest version", packageName)
	}
	return meta.Version, nil
}

// ResolvePyPIPackageVersionMetadata fetches PyPI metadata for a specific
// package version. When version is empty, it returns the package's latest metadata.
func ResolvePyPIPackageVersionMetadata(packageName, version string) (PyPIPackageMetadata, error) {
	url := fmt.Sprintf("%s/%s/json", pyPIBaseURL, packageName)
	if strings.TrimSpace(version) != "" {
		url = fmt.Sprintf("%s/%s/%s/json", pyPIBaseURL, packageName, version)
	}

	body, err := httpGet(url)
	if err != nil {
		return PyPIPackageMetadata{}, fmt.Errorf("failed to fetch PyPI package %q metadata: %w", packageName, err)
	}

	var resp pyPIPackageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return PyPIPackageMetadata{}, fmt.Errorf("failed to parse PyPI metadata for %q: %w", packageName, err)
	}

	return PyPIPackageMetadata{
		Version:        resp.Info.Version,
		RequiresPython: resp.Info.RequiresPython,
	}, nil
}

// RefreshDesiredToolVersions refreshes the concrete desired top-level tool
// versions used for template rendering. It never mutates ContainerVersion.
func RefreshDesiredToolVersions(cfg *Config, opts DesiredVersionRefreshOptions) ([]string, error) {
	if cfg == nil {
		return nil, fmt.Errorf("refresh desired tool versions: nil config")
	}

	var warnings []string
	for i := range cfg.ProgrammingTools {
		warning, err := refreshDesiredToolVersion(&cfg.ProgrammingTools[i], opts)
		if err != nil {
			return warnings, err
		}
		if warning != "" {
			warnings = append(warnings, warning)
		}
	}
	for i := range cfg.AITools {
		warning, err := refreshDesiredToolVersion(&cfg.AITools[i], opts)
		if err != nil {
			return warnings, err
		}
		if warning != "" {
			warnings = append(warnings, warning)
		}
	}
	return warnings, nil
}

// RefreshDesiredToolVersionsBestEffort refreshes latest/mirror concrete state
// on the provided config copy without persisting it. It never blocks past the
// supplied timeout budget and returns per-tool refresh errors.
func RefreshDesiredToolVersionsBestEffort(cfg *Config, timeout time.Duration) map[string]error {
	if cfg == nil {
		return map[string]error{"config": fmt.Errorf("nil config")}
	}

	deadline := time.Now().Add(timeout)
	errs := map[string]error{}
	for i := range cfg.ProgrammingTools {
		bestEffortRefreshTool(&cfg.ProgrammingTools[i], deadline, errs)
	}
	for i := range cfg.AITools {
		bestEffortRefreshTool(&cfg.AITools[i], deadline, errs)
	}
	return errs
}

// EffectiveProgrammingToolVersion returns the configured runtime version that
// implicit tooling should follow. It uses desired state, not built state.
func EffectiveProgrammingToolVersion(cfg *Config, toolName string) (string, bool, error) {
	if cfg == nil {
		return "", false, fmt.Errorf("effective programming tool version: nil config")
	}

	tool, ok := findToolConfig(cfg.ProgrammingTools, toolName)
	if !ok || !tool.Enabled {
		return "", false, nil
	}
	if tool.Mode == ModeOff && concreteDesiredVersion(*tool) == "" {
		return "", false, nil
	}

	source := concreteDesiredVersion(*tool)
	if strings.TrimSpace(source) == "" {
		return "", true, fmt.Errorf("%s is enabled but has no concrete desired version for %s mode", toolName, tool.Mode)
	}
	return source, true, nil
}

// EffectiveBaseNodeVersion returns the Node runtime available in cooper-base.
func EffectiveBaseNodeVersion(cfg *Config) (string, error) {
	nodeVersion, enabled, err := EffectiveProgrammingToolVersion(cfg, "node")
	if err != nil {
		return "", err
	}
	if enabled {
		return nodeVersion, nil
	}
	return DefaultBaseNodeVersion, nil
}

// ResolveGoplsVersion chooses the correct gopls release for a configured Go version.
func ResolveGoplsVersion(goVersion string) (string, error) {
	if ok, err := versionAtLeast(goVersion, "1.21"); err != nil {
		return "", fmt.Errorf("resolve gopls version for go %s: %w", goVersion, err)
	} else if ok {
		return GoplsLatestResolver()
	}
	if ok, _ := versionAtLeast(goVersion, "1.20"); ok {
		return "v0.15.3", nil
	}
	if ok, _ := versionAtLeast(goVersion, "1.18"); ok {
		return "v0.14.2", nil
	}
	if ok, _ := versionAtLeast(goVersion, "1.17"); ok {
		return "v0.11.0", nil
	}
	if ok, _ := versionAtLeast(goVersion, "1.15"); ok {
		return "v0.9.5", nil
	}
	if ok, _ := versionAtLeast(goVersion, "1.12"); ok {
		return "v0.7.5", nil
	}
	return "", fmt.Errorf("go %s is not supported by gopls; minimum supported Go version is 1.12", goVersion)
}

// ResolveTypeScriptLanguageServerVersion chooses the correct typescript-language-server release.
func ResolveTypeScriptLanguageServerVersion(nodeVersion string) (string, error) {
	if ok, err := versionAtLeast(nodeVersion, "20"); err != nil {
		return "", fmt.Errorf("resolve typescript-language-server for node %s: %w", nodeVersion, err)
	} else if ok {
		return NPMPackageLatestResolver("typescript-language-server")
	}
	if ok, _ := versionAtLeast(nodeVersion, "18"); ok {
		return "4.4.1", nil
	}
	if ok, _ := versionAtLeast(nodeVersion, "14.17"); ok {
		return "3.3.2", nil
	}
	return "", fmt.Errorf("node %s is too old for typescript-language-server; minimum supported Node version is 14.17", nodeVersion)
}

// ResolveTypeScriptPackageVersion chooses the bundled TypeScript release.
func ResolveTypeScriptPackageVersion(nodeVersion string) (string, error) {
	const fallback = "5.8.3"
	latest, err := NPMPackageLatestResolver("typescript")
	if err != nil {
		return "", fmt.Errorf("resolve latest typescript version: %w", err)
	}
	compatible, err := npmPackageCompatibleWithNode("typescript", latest, nodeVersion)
	if err != nil {
		return "", err
	}
	if compatible {
		return latest, nil
	}
	compatible, err = npmPackageCompatibleWithNode("typescript", fallback, nodeVersion)
	if err != nil {
		return "", err
	}
	if compatible {
		return fallback, nil
	}
	return "", fmt.Errorf("node %s is too old for TypeScript; even fallback version %s is incompatible", nodeVersion, fallback)
}

// ResolvePyrightVersion chooses the bundled pyright release.
func ResolvePyrightVersion(nodeVersion string) (string, error) {
	const fallback = "1.1.408"
	if ok, err := versionAtLeast(nodeVersion, "14.0.0"); err != nil {
		return "", fmt.Errorf("resolve pyright for node %s: %w", nodeVersion, err)
	} else if !ok {
		return "", fmt.Errorf("node %s is too old for pyright; minimum supported Node version is 14.0.0", nodeVersion)
	}

	latest, err := NPMPackageLatestResolver("pyright")
	if err != nil {
		return "", fmt.Errorf("resolve latest pyright version: %w", err)
	}
	compatible, err := npmPackageCompatibleWithNode("pyright", latest, nodeVersion)
	if err != nil {
		return "", err
	}
	if compatible {
		return latest, nil
	}
	compatible, err = npmPackageCompatibleWithNode("pyright", fallback, nodeVersion)
	if err != nil {
		return "", err
	}
	if compatible {
		return fallback, nil
	}
	return "", fmt.Errorf("node %s is too old for pyright fallback version %s", nodeVersion, fallback)
}

// ResolvePythonLSPServerVersion chooses the correct python-lsp-server release.
func ResolvePythonLSPServerVersion(pythonVersion string) (string, error) {
	if ok, err := versionAtLeast(pythonVersion, "3.9"); err != nil {
		return "", fmt.Errorf("resolve python-lsp-server for python %s: %w", pythonVersion, err)
	} else if ok {
		latest, err := PyPIPackageLatestResolver("python-lsp-server")
		if err != nil {
			return "", fmt.Errorf("resolve latest python-lsp-server version: %w", err)
		}
		meta, err := PyPIPackageVersionMetadataResolver("python-lsp-server", latest)
		if err != nil {
			return "", err
		}
		if compatible, err := pythonPackageCompatible(meta.RequiresPython, pythonVersion); err != nil {
			return "", err
		} else if !compatible {
			return "", fmt.Errorf("python-lsp-server %s requires Python %s, but configured Python is %s", latest, strings.TrimSpace(meta.RequiresPython), pythonVersion)
		}
		return latest, nil
	}
	if ok, _ := versionAtLeast(pythonVersion, "3.8"); ok {
		return "1.12.2", nil
	}
	if ok, _ := versionAtLeast(pythonVersion, "3.7"); ok {
		return "1.7.4", nil
	}
	if ok, _ := versionAtLeast(pythonVersion, "3.6"); ok {
		return "1.3.3", nil
	}
	return "", fmt.Errorf("python %s is too old for python-lsp-server; minimum supported Python version is 3.6", pythonVersion)
}

// ResolveImplicitTools resolves the set of implicit language-server and support tools.
func ResolveImplicitTools(cfg *Config) ([]ImplicitToolConfig, error) {
	tools, _, err := ResolveImplicitToolsWithOptions(cfg, ImplicitToolResolveOptions{})
	return tools, err
}

// ResolveImplicitToolsWithOptions resolves implicit tools and optionally allows
// stale fallback to previously built implicit versions when the current desired
// runtime still matches the built base state.
func ResolveImplicitToolsWithOptions(cfg *Config, opts ImplicitToolResolveOptions) ([]ImplicitToolConfig, []string, error) {
	if cfg == nil {
		return nil, nil, fmt.Errorf("resolve implicit tools: nil config")
	}

	var tools []ImplicitToolConfig
	var warnings []string

	if goVersion, enabled, err := EffectiveProgrammingToolVersion(cfg, "go"); err != nil {
		return nil, warnings, fmt.Errorf("resolve go implicit tools: %w", err)
	} else if enabled {
		version, warning, err := resolveImplicitVersionWithFallback(
			cfg,
			"gopls",
			opts.AllowStaleFallback && goplsUsesLatestLookup(goVersion),
			func() (string, error) { return ResolveGoplsVersion(goVersion) },
		)
		if err != nil {
			return nil, warnings, err
		}
		if warning != "" {
			warnings = append(warnings, warning)
		}
		tools = append(tools, ImplicitToolConfig{
			Name:             "gopls",
			Kind:             ImplicitToolKindLSP,
			ParentTool:       "go",
			Binary:           "gopls",
			ContainerVersion: version,
		})
	}

	if nodeVersion, enabled, err := EffectiveProgrammingToolVersion(cfg, "node"); err != nil {
		return nil, warnings, fmt.Errorf("resolve node implicit tools: %w", err)
	} else if enabled {
		tsLSVersion, warning, err := resolveImplicitVersionWithFallback(
			cfg,
			"typescript-language-server",
			opts.AllowStaleFallback && tsLanguageServerUsesLatestLookup(nodeVersion),
			func() (string, error) { return ResolveTypeScriptLanguageServerVersion(nodeVersion) },
		)
		if err != nil {
			return nil, warnings, err
		}
		if warning != "" {
			warnings = append(warnings, warning)
		}
		typeScriptVersion, warning, err := resolveImplicitVersionWithFallback(
			cfg,
			"typescript",
			opts.AllowStaleFallback && typeScriptUsesLatestLookup(nodeVersion),
			func() (string, error) { return ResolveTypeScriptPackageVersion(nodeVersion) },
		)
		if err != nil {
			return nil, warnings, err
		}
		if warning != "" {
			warnings = append(warnings, warning)
		}
		tools = append(tools,
			ImplicitToolConfig{
				Name:             "typescript-language-server",
				Kind:             ImplicitToolKindLSP,
				ParentTool:       "node",
				Binary:           "typescript-language-server",
				ContainerVersion: tsLSVersion,
			},
			ImplicitToolConfig{
				Name:             "typescript",
				Kind:             ImplicitToolKindSupport,
				ParentTool:       "node",
				Binary:           "tsc",
				ContainerVersion: typeScriptVersion,
			},
		)
	}

	if pythonVersion, enabled, err := EffectiveProgrammingToolVersion(cfg, "python"); err != nil {
		return nil, warnings, fmt.Errorf("resolve python implicit tools: %w", err)
	} else if enabled {
		baseNodeVersion, err := EffectiveBaseNodeVersion(cfg)
		if err != nil {
			return nil, warnings, fmt.Errorf("resolve python implicit tools: %w", err)
		}
		pyrightVersion, warning, err := resolveImplicitVersionWithFallback(
			cfg,
			"pyright",
			opts.AllowStaleFallback && pyrightUsesLatestLookup(baseNodeVersion),
			func() (string, error) { return ResolvePyrightVersion(baseNodeVersion) },
		)
		if err != nil {
			return nil, warnings, err
		}
		if warning != "" {
			warnings = append(warnings, warning)
		}
		pylspVersion, warning, err := resolveImplicitVersionWithFallback(
			cfg,
			"python-lsp-server",
			opts.AllowStaleFallback && pythonLSPUsesLatestLookup(pythonVersion),
			func() (string, error) { return ResolvePythonLSPServerVersion(pythonVersion) },
		)
		if err != nil {
			return nil, warnings, err
		}
		if warning != "" {
			warnings = append(warnings, warning)
		}
		tools = append(tools,
			ImplicitToolConfig{
				Name:             "pyright",
				Kind:             ImplicitToolKindLSP,
				ParentTool:       "python",
				Binary:           "pyright-langserver",
				ContainerVersion: pyrightVersion,
			},
			ImplicitToolConfig{
				Name:             "python-lsp-server",
				Kind:             ImplicitToolKindLSP,
				ParentTool:       "python",
				Binary:           "pylsp",
				ContainerVersion: pylspVersion,
			},
		)
	}

	sortImplicitTools(tools)
	return tools, warnings, nil
}

// ResolveImplicitToolsForParent resolves only the implicit tools attached to a
// single programming tool. This is used by startup warnings so one resolver
// outage does not suppress warnings for unrelated parent tools.
func ResolveImplicitToolsForParent(cfg *Config, parent string, opts ImplicitToolResolveOptions) ([]ImplicitToolConfig, []string, error) {
	if cfg == nil {
		return nil, nil, fmt.Errorf("resolve implicit tools for %s: nil config", parent)
	}
	var tools []ImplicitToolConfig
	var warnings []string

	switch parent {
	case "go":
		goVersion, enabled, err := EffectiveProgrammingToolVersion(cfg, "go")
		if err != nil {
			return nil, warnings, fmt.Errorf("resolve go implicit tools: %w", err)
		}
		if !enabled {
			return nil, warnings, nil
		}
		version, warning, err := resolveImplicitVersionWithFallback(
			cfg,
			"gopls",
			opts.AllowStaleFallback && goplsUsesLatestLookup(goVersion),
			func() (string, error) { return ResolveGoplsVersion(goVersion) },
		)
		if err != nil {
			return nil, warnings, err
		}
		if warning != "" {
			warnings = append(warnings, warning)
		}
		tools = append(tools, ImplicitToolConfig{Name: "gopls", Kind: ImplicitToolKindLSP, ParentTool: "go", Binary: "gopls", ContainerVersion: version})
	case "node":
		nodeVersion, enabled, err := EffectiveProgrammingToolVersion(cfg, "node")
		if err != nil {
			return nil, warnings, fmt.Errorf("resolve node implicit tools: %w", err)
		}
		if !enabled {
			return nil, warnings, nil
		}
		tsLSVersion, warning, err := resolveImplicitVersionWithFallback(
			cfg,
			"typescript-language-server",
			opts.AllowStaleFallback && tsLanguageServerUsesLatestLookup(nodeVersion),
			func() (string, error) { return ResolveTypeScriptLanguageServerVersion(nodeVersion) },
		)
		if err != nil {
			return nil, warnings, err
		}
		if warning != "" {
			warnings = append(warnings, warning)
		}
		typeScriptVersion, warning, err := resolveImplicitVersionWithFallback(
			cfg,
			"typescript",
			opts.AllowStaleFallback && typeScriptUsesLatestLookup(nodeVersion),
			func() (string, error) { return ResolveTypeScriptPackageVersion(nodeVersion) },
		)
		if err != nil {
			return nil, warnings, err
		}
		if warning != "" {
			warnings = append(warnings, warning)
		}
		tools = append(tools,
			ImplicitToolConfig{Name: "typescript-language-server", Kind: ImplicitToolKindLSP, ParentTool: "node", Binary: "typescript-language-server", ContainerVersion: tsLSVersion},
			ImplicitToolConfig{Name: "typescript", Kind: ImplicitToolKindSupport, ParentTool: "node", Binary: "tsc", ContainerVersion: typeScriptVersion},
		)
	case "python":
		pythonVersion, enabled, err := EffectiveProgrammingToolVersion(cfg, "python")
		if err != nil {
			return nil, warnings, fmt.Errorf("resolve python implicit tools: %w", err)
		}
		if !enabled {
			return nil, warnings, nil
		}
		baseNodeVersion, err := EffectiveBaseNodeVersion(cfg)
		if err != nil {
			return nil, warnings, fmt.Errorf("resolve python implicit tools: %w", err)
		}
		pyrightVersion, warning, err := resolveImplicitVersionWithFallback(
			cfg,
			"pyright",
			opts.AllowStaleFallback && pyrightUsesLatestLookup(baseNodeVersion),
			func() (string, error) { return ResolvePyrightVersion(baseNodeVersion) },
		)
		if err != nil {
			return nil, warnings, err
		}
		if warning != "" {
			warnings = append(warnings, warning)
		}
		pylspVersion, warning, err := resolveImplicitVersionWithFallback(
			cfg,
			"python-lsp-server",
			opts.AllowStaleFallback && pythonLSPUsesLatestLookup(pythonVersion),
			func() (string, error) { return ResolvePythonLSPServerVersion(pythonVersion) },
		)
		if err != nil {
			return nil, warnings, err
		}
		if warning != "" {
			warnings = append(warnings, warning)
		}
		tools = append(tools,
			ImplicitToolConfig{Name: "pyright", Kind: ImplicitToolKindLSP, ParentTool: "python", Binary: "pyright-langserver", ContainerVersion: pyrightVersion},
			ImplicitToolConfig{Name: "python-lsp-server", Kind: ImplicitToolKindLSP, ParentTool: "python", Binary: "pylsp", ContainerVersion: pylspVersion},
		)
	default:
		return nil, warnings, fmt.Errorf("unknown implicit parent tool %q", parent)
	}

	sortImplicitTools(tools)
	return tools, warnings, nil
}

// CompareImplicitTools returns human-readable mismatch strings between built and target implicit tooling.
func CompareImplicitTools(built, target []ImplicitToolConfig) []string {
	builtByKey := map[string]ImplicitToolConfig{}
	targetByKey := map[string]ImplicitToolConfig{}
	for _, tool := range built {
		builtByKey[implicitToolKey(tool)] = tool
	}
	for _, tool := range target {
		targetByKey[implicitToolKey(tool)] = tool
	}

	var warnings []string
	targetKeys := sortedImplicitKeys(targetByKey)
	for _, key := range targetKeys {
		expected := targetByKey[key]
		actual, ok := builtByKey[key]
		if !ok || strings.TrimSpace(actual.ContainerVersion) == "" {
			warnings = append(warnings, fmt.Sprintf("%s (for %s): not built, expected=%s", expected.Name, expected.ParentTool, expected.ContainerVersion))
			continue
		}
		if actual.Kind != expected.Kind || actual.ParentTool != expected.ParentTool || actual.Binary != expected.Binary || actual.ContainerVersion != expected.ContainerVersion {
			warnings = append(warnings, fmt.Sprintf("%s (for %s): container=%s, expected=%s", expected.Name, expected.ParentTool, actual.ContainerVersion, expected.ContainerVersion))
		}
	}

	builtKeys := sortedImplicitKeys(builtByKey)
	for _, key := range builtKeys {
		if _, ok := targetByKey[key]; ok {
			continue
		}
		tool := builtByKey[key]
		warnings = append(warnings, fmt.Sprintf("%s (for %s): built but no longer expected", tool.Name, tool.ParentTool))
	}

	return warnings
}

// VisibleImplicitLSPs filters out support tooling so the About tab only shows language servers.
func VisibleImplicitLSPs(tools []ImplicitToolConfig) []ImplicitToolConfig {
	visible := make([]ImplicitToolConfig, 0, len(tools))
	for _, tool := range tools {
		if tool.Kind != ImplicitToolKindLSP {
			continue
		}
		visible = append(visible, tool)
	}
	sortImplicitTools(visible)
	return visible
}

func refreshDesiredToolVersion(tool *ToolConfig, opts DesiredVersionRefreshOptions) (string, error) {
	if tool == nil || !tool.Enabled || tool.Mode == ModeOff {
		return "", nil
	}

	switch tool.Mode {
	case ModeLatest:
		latest, err := LatestVersionResolver(tool.Name)
		if err != nil {
			if opts.AllowStaleFallback && strings.TrimSpace(tool.PinnedVersion) != "" {
				return fmt.Sprintf("Warning: could not refresh latest %s (%v). Using last-known resolved version %s for template generation. Run 'cooper build' or 'cooper update' with network access to refresh.", tool.Name, err, tool.PinnedVersion), nil
			}
			return "", fmt.Errorf("%s is enabled in latest mode but latest version could not be resolved: %w", tool.Name, err)
		}
		tool.PinnedVersion = latest
		return "", nil
	case ModeMirror:
		hostVersion, err := HostVersionDetector(tool.Name)
		if err != nil {
			if opts.AllowStaleFallback && strings.TrimSpace(tool.HostVersion) != "" {
				return fmt.Sprintf("Warning: could not detect host %s version (%v). Using last-known host version %s for template generation. Run 'cooper build' or 'cooper update' on the intended host to refresh.", tool.Name, err, tool.HostVersion), nil
			}
			return "", fmt.Errorf("%s is enabled in mirror mode but its host version could not be detected: %w", tool.Name, err)
		}
		tool.HostVersion = hostVersion
		return "", nil
	case ModePin:
		if strings.TrimSpace(tool.PinnedVersion) == "" {
			return "", fmt.Errorf("%s is enabled in pin mode but no pinned version is set", tool.Name)
		}
		ok, err := VersionValidator(tool.Name, tool.PinnedVersion)
		if err != nil {
			return "", fmt.Errorf("validate pinned %s version %s: %w", tool.Name, tool.PinnedVersion, err)
		}
		if !ok {
			return "", fmt.Errorf("%s pinned version %s is not available", tool.Name, tool.PinnedVersion)
		}
		return "", nil
	default:
		return "", nil
	}
}

func bestEffortRefreshTool(tool *ToolConfig, deadline time.Time, errs map[string]error) {
	if tool == nil || !tool.Enabled || tool.Mode == ModeOff {
		return
	}
	if tool.Mode != ModeLatest && tool.Mode != ModeMirror {
		return
	}

	remaining := time.Until(deadline)
	if remaining <= 0 {
		errs[tool.Name] = fmt.Errorf("timed out before %s %s version could be checked", tool.Name, tool.Mode)
		return
	}

	type result struct {
		value string
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		if tool.Mode == ModeLatest {
			value, err := LatestVersionResolver(tool.Name)
			ch <- result{value: value, err: err}
			return
		}
		value, err := HostVersionDetector(tool.Name)
		ch <- result{value: value, err: err}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			errs[tool.Name] = res.err
			return
		}
		if tool.Mode == ModeLatest {
			tool.PinnedVersion = res.value
		} else {
			tool.HostVersion = res.value
		}
	case <-time.After(remaining):
		errs[tool.Name] = fmt.Errorf("timed out checking %s %s version", tool.Name, tool.Mode)
	}
}

func findToolConfig(tools []ToolConfig, name string) (*ToolConfig, bool) {
	for i := range tools {
		if strings.EqualFold(tools[i].Name, name) {
			return &tools[i], true
		}
	}
	return nil, false
}

func concreteDesiredVersion(tool ToolConfig) string {
	switch tool.Mode {
	case ModeMirror:
		return strings.TrimSpace(tool.HostVersion)
	case ModePin, ModeLatest:
		return strings.TrimSpace(tool.PinnedVersion)
	default:
		if pinned := strings.TrimSpace(tool.PinnedVersion); pinned != "" {
			return pinned
		}
		return strings.TrimSpace(tool.HostVersion)
	}
}

func npmPackageCompatibleWithNode(packageName, version, nodeVersion string) (bool, error) {
	meta, err := NPMPackageMetadataResolver(packageName, version)
	if err != nil {
		return false, fmt.Errorf("resolve npm metadata for %s@%s: %w", packageName, version, err)
	}
	if strings.TrimSpace(meta.Engines.Node) == "" {
		return true, nil
	}
	ok, err := versionSatisfies(nodeVersion, meta.Engines.Node)
	if err != nil {
		return false, fmt.Errorf("evaluate node engine %q for %s@%s against node %s: %w", meta.Engines.Node, packageName, version, nodeVersion, err)
	}
	return ok, nil
}

func pythonPackageCompatible(requiresPython, pythonVersion string) (bool, error) {
	if strings.TrimSpace(requiresPython) == "" {
		return true, nil
	}
	ok, err := versionSatisfies(pythonVersion, requiresPython)
	if err != nil {
		return false, fmt.Errorf("evaluate Python requirement %q against Python %s: %w", requiresPython, pythonVersion, err)
	}
	return ok, nil
}

func parseComparableVersion(raw string) (comparableVersion, error) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "v")
	if idx := strings.IndexAny(trimmed, "+-"); idx >= 0 {
		trimmed = trimmed[:idx]
	}
	if trimmed == "" {
		return comparableVersion{}, fmt.Errorf("empty version")
	}

	parts := strings.Split(trimmed, ".")
	if len(parts) > 3 {
		parts = parts[:3]
	}
	values := []int{0, 0, 0}
	for i, part := range parts {
		if strings.TrimSpace(part) == "" {
			return comparableVersion{}, fmt.Errorf("invalid version %q", raw)
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			return comparableVersion{}, fmt.Errorf("invalid version %q", raw)
		}
		values[i] = value
	}
	return comparableVersion{major: values[0], minor: values[1], patch: values[2]}, nil
}

func compareVersions(a, b string) (int, error) {
	parsedA, err := parseComparableVersion(a)
	if err != nil {
		return 0, err
	}
	parsedB, err := parseComparableVersion(b)
	if err != nil {
		return 0, err
	}
	return compareComparableVersions(parsedA, parsedB), nil
}

func compareComparableVersions(a, b comparableVersion) int {
	if a.major != b.major {
		if a.major < b.major {
			return -1
		}
		return 1
	}
	if a.minor != b.minor {
		if a.minor < b.minor {
			return -1
		}
		return 1
	}
	if a.patch != b.patch {
		if a.patch < b.patch {
			return -1
		}
		return 1
	}
	return 0
}

func versionAtLeast(version, minimum string) (bool, error) {
	cmp, err := compareVersions(version, minimum)
	if err != nil {
		return false, err
	}
	return cmp >= 0, nil
}

func versionSatisfies(version, constraint string) (bool, error) {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" || constraint == "*" || strings.EqualFold(constraint, "x") {
		return true, nil
	}

	var parseErr error
	for _, clause := range strings.Split(constraint, "||") {
		ok, err := versionSatisfiesAll(version, clause)
		if err != nil {
			parseErr = err
			continue
		}
		if ok {
			return true, nil
		}
	}
	if parseErr != nil {
		return false, parseErr
	}
	return false, nil
}

func versionSatisfiesAll(version, clause string) (bool, error) {
	replacer := strings.NewReplacer(",", " ", "(", " ", ")", " ")
	tokens := strings.Fields(replacer.Replace(strings.TrimSpace(clause)))
	if len(tokens) == 0 {
		return true, nil
	}
	for _, token := range tokens {
		ok, err := versionSatisfiesToken(version, token)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func versionSatisfiesToken(version, token string) (bool, error) {
	token = strings.TrimSpace(token)
	if token == "" || token == "*" || strings.EqualFold(token, "x") {
		return true, nil
	}
	if strings.HasPrefix(token, "^") {
		base := strings.TrimPrefix(token, "^")
		lower, err := parseComparableVersion(base)
		if err != nil {
			return false, err
		}
		upper := comparableVersion{major: lower.major + 1}
		current, err := parseComparableVersion(version)
		if err != nil {
			return false, err
		}
		return compareComparableVersions(current, lower) >= 0 && compareComparableVersions(current, upper) < 0, nil
	}
	if strings.HasPrefix(token, "~") {
		base := strings.TrimPrefix(token, "~")
		lower, err := parseComparableVersion(base)
		if err != nil {
			return false, err
		}
		upper := comparableVersion{major: lower.major, minor: lower.minor + 1}
		current, err := parseComparableVersion(version)
		if err != nil {
			return false, err
		}
		return compareComparableVersions(current, lower) >= 0 && compareComparableVersions(current, upper) < 0, nil
	}
	if strings.ContainsAny(token, "xX*") {
		return satisfiesWildcard(version, token)
	}

	operators := []string{">=", "<=", ">", "<", "="}
	for _, operator := range operators {
		if strings.HasPrefix(token, operator) {
			cmp, err := compareVersions(version, strings.TrimSpace(strings.TrimPrefix(token, operator)))
			if err != nil {
				return false, err
			}
			switch operator {
			case ">=":
				return cmp >= 0, nil
			case "<=":
				return cmp <= 0, nil
			case ">":
				return cmp > 0, nil
			case "<":
				return cmp < 0, nil
			case "=":
				return cmp == 0, nil
			}
		}
	}

	cmp, err := compareVersions(version, token)
	if err != nil {
		return false, err
	}
	return cmp == 0, nil
}

func satisfiesWildcard(version, token string) (bool, error) {
	token = strings.TrimPrefix(strings.TrimSpace(token), "v")
	parts := strings.Split(token, ".")
	current, err := parseComparableVersion(version)
	if err != nil {
		return false, err
	}
	for i, part := range parts {
		if part == "" || strings.EqualFold(part, "x") || part == "*" {
			return true, nil
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			return false, fmt.Errorf("invalid wildcard constraint %q", token)
		}
		switch i {
		case 0:
			if current.major != value {
				return false, nil
			}
		case 1:
			if current.minor != value {
				return false, nil
			}
		case 2:
			if current.patch != value {
				return false, nil
			}
		}
	}
	return true, nil
}

func implicitToolKey(tool ImplicitToolConfig) string {
	return strings.Join([]string{tool.ParentTool, tool.Kind, tool.Name}, "\x00")
}

func sortedImplicitKeys(values map[string]ImplicitToolConfig) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortImplicitTools(tools []ImplicitToolConfig) {
	sort.Slice(tools, func(i, j int) bool {
		if tools[i].ParentTool != tools[j].ParentTool {
			return tools[i].ParentTool < tools[j].ParentTool
		}
		if tools[i].Kind != tools[j].Kind {
			return tools[i].Kind < tools[j].Kind
		}
		return tools[i].Name < tools[j].Name
	})
}

func resolveImplicitVersionWithFallback(cfg *Config, toolName string, allowStaleFallback bool, strict func() (string, error)) (string, string, error) {
	version, err := strict()
	if err == nil {
		return version, "", nil
	}
	if !allowStaleFallback {
		return "", "", err
	}
	fallbackVersion, ok := staleImplicitFallbackVersion(cfg, toolName)
	if !ok {
		return "", "", err
	}
	warning := fmt.Sprintf("Warning: could not refresh implicit tool %s (%v). Using last-built version %s for template generation. Run 'cooper build' or 'cooper update' with network access to refresh.", toolName, err, fallbackVersion)
	return fallbackVersion, warning, nil
}

func staleImplicitFallbackVersion(cfg *Config, toolName string) (string, bool) {
	if cfg == nil || !canUseBuiltImplicitFallback(cfg, toolName) {
		return "", false
	}
	for _, tool := range cfg.ImplicitTools {
		if tool.Name != toolName || strings.TrimSpace(tool.ContainerVersion) == "" {
			continue
		}
		return tool.ContainerVersion, true
	}
	return "", false
}

func canUseBuiltImplicitFallback(cfg *Config, toolName string) bool {
	switch toolName {
	case "gopls":
		return desiredProgrammingToolMatchesBuilt(cfg, "go")
	case "typescript-language-server", "typescript":
		return desiredProgrammingToolMatchesBuilt(cfg, "node")
	case "pyright":
		return desiredBaseNodeMatchesBuilt(cfg)
	case "python-lsp-server":
		return desiredProgrammingToolMatchesBuilt(cfg, "python")
	default:
		return false
	}
}

func desiredProgrammingToolMatchesBuilt(cfg *Config, toolName string) bool {
	tool, ok := findToolConfig(cfg.ProgrammingTools, toolName)
	if !ok || !tool.Enabled {
		return false
	}
	desired := concreteDesiredVersion(*tool)
	return desired != "" && desired == tool.ContainerVersion
}

func desiredBaseNodeMatchesBuilt(cfg *Config) bool {
	desiredBaseNodeVersion, err := EffectiveBaseNodeVersion(cfg)
	if err != nil || strings.TrimSpace(desiredBaseNodeVersion) == "" {
		return false
	}
	builtBaseNodeVersion, ok := BuiltBaseNodeVersion(cfg)
	return ok && desiredBaseNodeVersion == builtBaseNodeVersion
}

// BuiltBaseNodeVersion returns the best known built-state base Node runtime.
// It prefers the explicit persisted field, then falls back to the node tool's
// built container version when that is sufficient to infer the base runtime.
func BuiltBaseNodeVersion(cfg *Config) (string, bool) {
	if cfg == nil {
		return "", false
	}
	if version := strings.TrimSpace(cfg.BaseNodeVersion); version != "" {
		return version, true
	}
	nodeTool, ok := findToolConfig(cfg.ProgrammingTools, "node")
	if !ok || !nodeTool.Enabled {
		return "", false
	}
	if version := strings.TrimSpace(nodeTool.ContainerVersion); version != "" {
		return version, true
	}
	return "", false
}

// BaseNodeVersionDrift compares the desired base Node runtime against the best
// known built-state base runtime.
func BaseNodeVersionDrift(cfg *Config) (built string, expected string, mismatch bool, err error) {
	expected, err = EffectiveBaseNodeVersion(cfg)
	if err != nil {
		return "", "", false, err
	}
	built, ok := BuiltBaseNodeVersion(cfg)
	if !ok {
		return "", expected, true, nil
	}
	return built, expected, built != expected, nil
}

func goplsUsesLatestLookup(goVersion string) bool {
	ok, err := versionAtLeast(goVersion, "1.21")
	return err == nil && ok
}

func tsLanguageServerUsesLatestLookup(nodeVersion string) bool {
	ok, err := versionAtLeast(nodeVersion, "20")
	return err == nil && ok
}

func typeScriptUsesLatestLookup(nodeVersion string) bool {
	ok, err := versionAtLeast(nodeVersion, "14.17")
	return err == nil && ok
}

func pyrightUsesLatestLookup(nodeVersion string) bool {
	ok, err := versionAtLeast(nodeVersion, "14.0.0")
	return err == nil && ok
}

func pythonLSPUsesLatestLookup(pythonVersion string) bool {
	ok, err := versionAtLeast(pythonVersion, "3.9")
	return err == nil && ok
}
