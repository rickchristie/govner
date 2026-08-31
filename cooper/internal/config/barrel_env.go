package config

import (
	"fmt"
	"regexp"
	"strings"
)

// BarrelEnvVar is a user-defined environment variable applied to each
// `cooper cli` session at runtime.
type BarrelEnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

var barrelEnvNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var protectedBarrelEnvExactNames = []string{
	"HTTP_PROXY",
	"HTTPS_PROXY",
	"NO_PROXY",
	"http_proxy",
	"https_proxy",
	"no_proxy",
	"ALL_PROXY",
	"all_proxy",
	"DISPLAY",
	"XAUTHORITY",
	"PLAYWRIGHT_BROWSERS_PATH",
	"PATH",
	"HOME",
	"USER",
	"LOGNAME",
	"SHELL",
	"NODE_EXTRA_CA_CERTS",
	"NPM_CONFIG_PREFIX",
	"GOPATH",
	"GOMODCACHE",
	"GOCACHE",
	"OPENAI_API_KEY",
	"GH_TOKEN",
	"GITHUB_TOKEN",
	"TERM",
	"COLORTERM",
	"TERM_PROGRAM",
	"TERM_PROGRAM_VERSION",
	"COLORFGBG",
	"LC_TERMINAL",
	"LC_TERMINAL_VERSION",
	"TERM_SESSION_ID",
	"ITERM_PROFILE",
	"ITERM_SESSION_ID",
	"VTE_VERSION",
	"KONSOLE_VERSION",
	"KONSOLE_PROFILE_NAME",
	"WT_SESSION",
	"WT_PROFILE_ID",
	"KITTY_WINDOW_ID",
	"WEZTERM_PANE",
	"TERMINAL_EMULATOR",
	"DOMTERM",
	"TERMINOLOGY",
	"NO_COLOR",
	"FORCE_COLOR",
	"FORCE_HYPERLINK",
	"CLICOLOR",
	"CLICOLOR_FORCE",
	"NODE_DISABLE_COLORS",
	"TZ",
	"CLAUDE_CODE_SSE_PORT",
	"CLAUDE_CODE_ENTRYPOINT",
	"ENABLE_IDE_INTEGRATION",
	"CLAUDECODE",
}

var protectedBarrelEnvExactNameSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(protectedBarrelEnvExactNames))
	for _, name := range protectedBarrelEnvExactNames {
		set[name] = struct{}{}
	}
	return set
}()

// ProtectedBarrelEnvNames returns the reserved exact names that cannot be
// configured by the user.
func ProtectedBarrelEnvNames() []string {
	return append([]string(nil), protectedBarrelEnvExactNames...)
}

// IsProtectedBarrelEnvName reports whether name is reserved by Cooper.
func IsProtectedBarrelEnvName(name string) bool {
	trimmed := strings.TrimSpace(name)
	if strings.HasPrefix(trimmed, "COOPER_") {
		return true
	}
	_, ok := protectedBarrelEnvExactNameSet[trimmed]
	return ok
}

// IsProtectedBarrelEnvNameForTool reports whether name is reserved for a
// specific barrel. Built-in Grok reserves GROK_HOME because Cooper maps the
// effective host state root to /home/user/.grok. It reserves
// GROK_LEADER_SOCKET because process transport must stay in the barrel. Other
// Grok settings and API keys remain user-controlled.
func IsProtectedBarrelEnvNameForTool(name, toolName string) bool {
	if IsProtectedBarrelEnvName(name) {
		return true
	}
	if !strings.EqualFold(strings.TrimSpace(toolName), "grok") {
		return false
	}
	switch strings.TrimSpace(name) {
	case "GROK_HOME", "GROK_LEADER_SOCKET":
		return true
	default:
		return false
	}
}

// CanonicalizeBarrelEnvVars returns a copied slice with whitespace-trimmed
// names and exact original values.
func CanonicalizeBarrelEnvVars(vars []BarrelEnvVar) []BarrelEnvVar {
	if len(vars) == 0 {
		return []BarrelEnvVar{}
	}
	canonical := append([]BarrelEnvVar(nil), vars...)
	for i := range canonical {
		canonical[i].Name = strings.TrimSpace(canonical[i].Name)
	}
	return canonical
}

// ValidateBarrelEnvVars strictly validates persisted barrel env vars for the
// configure/save path. It never mutates its input, and it rejects duplicates
// so config writes stay deterministic and users get immediate feedback.
func ValidateBarrelEnvVars(vars []BarrelEnvVar) error {
	canonical := CanonicalizeBarrelEnvVars(vars)
	seen := make(map[string]string, len(canonical))

	for _, variable := range canonical {
		if variable.Name == "" {
			return fmt.Errorf("barrel env name is required")
		}
		if !barrelEnvNameRE.MatchString(variable.Name) {
			return fmt.Errorf("barrel env %q has invalid name", variable.Name)
		}
		if IsProtectedBarrelEnvName(variable.Name) {
			return fmt.Errorf("barrel env %q uses a protected name", variable.Name)
		}
		if err := validateBarrelEnvValue(variable.Value); err != nil {
			return fmt.Errorf("barrel env %q: %w", variable.Name, err)
		}

		lookup := strings.ToUpper(variable.Name)
		if existing, ok := seen[lookup]; ok {
			return fmt.Errorf("barrel env %q duplicates %q", variable.Name, existing)
		}
		seen[lookup] = variable.Name
	}

	return nil
}

// NormalizeBarrelEnvVarsForRuntime tolerantly filters malformed runtime env
// entries so hand-edited config cannot break `cooper cli` startup. Unlike the
// strict configure/save validator, it intentionally preserves duplicate order
// for usable entries and lets normal shell export order make the last value
// win during runtime recovery.
func NormalizeBarrelEnvVarsForRuntime(vars []BarrelEnvVar) ([]BarrelEnvVar, []string) {
	return NormalizeBarrelEnvVarsForRuntimeForTool(vars, "")
}

// NormalizeBarrelEnvVarsForRuntimeForTool is the tool-aware runtime filter.
// For built-in Grok it also drops the state-root and leader-socket path values
// with a visible warning. Cooper owns these path mappings, but it does not
// override other Grok settings.
func NormalizeBarrelEnvVarsForRuntimeForTool(vars []BarrelEnvVar, toolName string) ([]BarrelEnvVar, []string) {
	canonical := CanonicalizeBarrelEnvVars(vars)
	usable := make([]BarrelEnvVar, 0, len(canonical))
	warnings := make([]string, 0)

	for _, variable := range canonical {
		switch {
		case variable.Name == "":
			warnings = append(warnings, "ignoring barrel env with empty name")
		case !barrelEnvNameRE.MatchString(variable.Name):
			warnings = append(warnings, fmt.Sprintf("ignoring barrel env %q: invalid name", variable.Name))
		case IsProtectedBarrelEnvNameForTool(variable.Name, toolName):
			warnings = append(warnings, fmt.Sprintf("ignoring barrel env %q: protected name", variable.Name))
		default:
			if err := validateBarrelEnvValue(variable.Value); err != nil {
				warnings = append(warnings, fmt.Sprintf("ignoring barrel env %q: %s", variable.Name, err.Error()))
				continue
			}
			usable = append(usable, variable)
		}
	}

	if usable == nil {
		usable = []BarrelEnvVar{}
	}
	return usable, warnings
}

func validateBarrelEnvValue(value string) error {
	switch {
	case strings.ContainsRune(value, '\x00'):
		return fmt.Errorf("value contains NUL")
	case strings.ContainsRune(value, '\n'):
		return fmt.Errorf("value contains newline")
	case strings.ContainsRune(value, '\r'):
		return fmt.Errorf("value contains carriage return")
	default:
		return nil
	}
}
