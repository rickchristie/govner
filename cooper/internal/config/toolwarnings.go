package config

import (
	"fmt"
	"time"
)

// PrepareToolVersionSnapshot returns a deep-copied config whose latest/mirror
// desired state has been refreshed best-effort for the About tab, plus the
// startup warning strings derived from that refreshed copy. The original config
// is never mutated.
func PrepareToolVersionSnapshot(cfg *Config, timeout time.Duration) (*Config, []string) {
	copyCfg := CloneConfig(cfg)
	if copyCfg == nil {
		return nil, nil
	}

	refreshErrs := RefreshDesiredToolVersionsBestEffort(copyCfg, timeout)
	warnings := topLevelToolWarnings(copyCfg, refreshErrs)
	if warning := baseNodeRuntimeWarning(copyCfg, refreshErrs); warning != "" {
		warnings = append(warnings, warning)
	}
	for _, parent := range []string{"go", "node", "python"} {
		builtImplicit := filterImplicitToolsByParent(cfg.ImplicitTools, map[string]bool{parent: true})
		if !programmingToolEnabled(copyCfg, parent) {
			warnings = append(warnings, CompareImplicitTools(builtImplicit, nil)...)
			continue
		}
		if blocker := implicitVerificationBlocker(copyCfg, parent, refreshErrs); blocker != nil {
			warnings = append(warnings, fmt.Sprintf("could not verify implicit tools for %s: %v", parent, blocker))
			continue
		}
		targetImplicit, _, err := ResolveImplicitToolsForParent(copyCfg, parent, ImplicitToolResolveOptions{})
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("could not verify implicit tools for %s: %v", parent, err))
			continue
		}
		warnings = append(warnings, CompareImplicitTools(builtImplicit, targetImplicit)...)
	}
	return copyCfg, warnings
}

func topLevelToolWarnings(cfg *Config, refreshErrs map[string]error) []string {
	var warnings []string
	appendWarnings := func(tools []ToolConfig) {
		for _, tool := range tools {
			if !tool.Enabled || tool.Mode == ModeOff {
				continue
			}
			if err, failed := refreshErrs[tool.Name]; failed {
				warnings = append(warnings, fmt.Sprintf("could not verify %s: %v", tool.Name, err))
				continue
			}
			expected := concreteDesiredVersion(tool)
			status := CompareVersions(tool.ContainerVersion, expected, tool.Mode)
			if status == VersionMismatch {
				warnings = append(warnings, fmt.Sprintf("%s: container=%s, expected=%s (%s mode)", tool.Name, tool.ContainerVersion, expected, tool.Mode))
			}
		}
	}
	appendWarnings(cfg.ProgrammingTools)
	appendWarnings(cfg.AITools)
	return warnings
}

func baseNodeRuntimeWarning(cfg *Config, refreshErrs map[string]error) string {
	if programmingToolEnabled(cfg, "node") {
		if _, failed := refreshErrs["node"]; failed {
			return ""
		}
	}
	built, expected, mismatch, err := BaseNodeVersionDrift(cfg)
	if err != nil {
		return fmt.Sprintf("could not verify base node runtime: %v", err)
	}
	if !mismatch {
		return ""
	}
	if built == "" {
		built = "(unknown)"
	}
	return fmt.Sprintf("base node runtime: built=%s, expected=%s", built, expected)
}

func filterImplicitToolsByParent(tools []ImplicitToolConfig, parents map[string]bool) []ImplicitToolConfig {
	if len(parents) == 0 {
		return nil
	}
	filtered := make([]ImplicitToolConfig, 0, len(tools))
	for _, tool := range tools {
		if !parents[tool.ParentTool] {
			continue
		}
		filtered = append(filtered, tool)
	}
	sortImplicitTools(filtered)
	return filtered
}

func programmingToolEnabled(cfg *Config, name string) bool {
	if cfg == nil {
		return false
	}
	tool, ok := findToolConfig(cfg.ProgrammingTools, name)
	return ok && tool.Enabled && tool.Mode != ModeOff
}

func implicitVerificationBlocker(cfg *Config, parent string, refreshErrs map[string]error) error {
	if err, failed := refreshErrs[parent]; failed {
		return err
	}
	if parent == "python" && programmingToolEnabled(cfg, "node") {
		if err, failed := refreshErrs["node"]; failed {
			return fmt.Errorf("base node version unavailable: %w", err)
		}
	}
	return nil
}
