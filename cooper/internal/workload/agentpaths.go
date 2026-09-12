package workload

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"golang.org/x/text/unicode/norm"
)

// StatePath is one complete state root. Base selects an agent or XDG path
// rule; Path is relative to that base. Add newly supported folders here,
// without adding agent branches to the Docker or VM back end.
type StatePath struct {
	ID       string
	Base     string
	Path     string
	Kind     PathKind
	Optional bool
}

var agentStatePaths = map[string][]StatePath{
	"claude": {
		{ID: "claude-state", Base: "claude", Kind: Directory},
		{ID: "claude-config", Base: "claude-settings", Path: ".claude.json", Kind: File, Optional: true},
	},
	"copilot": {
		{ID: "copilot-state", Base: "copilot", Kind: Directory},
		{ID: "copilot-cache", Base: "copilot-cache", Kind: Directory},
		{ID: "copilot-old-config", Base: "copilot-old-config", Path: ".copilot", Kind: Directory, Optional: true},
		{ID: "copilot-old-state", Base: "copilot-old-state", Path: ".copilot", Kind: Directory, Optional: true},
	},
	"codex": {
		{ID: "codex-state", Base: "codex", Kind: Directory},
		{ID: "shared-agents", Base: "home", Path: ".agents", Kind: Directory},
		{ID: "claude-marketplace", Base: "home", Path: ".claude-plugin", Kind: Directory},
		{ID: "cursor-marketplace", Base: "home", Path: ".cursor-plugin", Kind: Directory},
	},
	"opencode": {
		{ID: "opencode-cache", Base: "XDG_CACHE_HOME", Path: "opencode", Kind: Directory},
		{ID: "opencode-config", Base: "XDG_CONFIG_HOME", Path: "opencode", Kind: Directory},
		{ID: "opencode-share", Base: "XDG_DATA_HOME", Path: "opencode", Kind: Directory},
		{ID: "opencode-local-state", Base: "XDG_STATE_HOME", Path: "opencode", Kind: Directory},
		{ID: "opencode-compat", Base: "home", Path: ".opencode", Kind: Directory},
		{ID: "opencode-extra-config", Base: "OPENCODE_CONFIG_DIR", Kind: Directory, Optional: true},
		{ID: "opencode-config-file", Base: "OPENCODE_CONFIG", Kind: File, Optional: true},
		{ID: "opencode-database", Base: "OPENCODE_DB", Kind: File, Optional: true},
	},
	"grok": {
		{ID: "grok-state", Base: "grok", Kind: Directory},
		{ID: "shared-agents", Base: "home", Path: ".agents", Kind: Directory},
	},
}

var pathEnvironmentNames = []string{
	"CODEX_HOME", "GROK_HOME", "CLAUDE_CONFIG_DIR",
	"COPILOT_HOME", "COPILOT_CACHE_HOME",
	"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME",
	"OPENCODE_CONFIG", "OPENCODE_CONFIG_DIR", "OPENCODE_DB",
}

// HostPathEnvironment keeps unset and empty values distinct. Claude treats
// an explicitly empty config directory as the current working directory.
func HostPathEnvironment() map[string]string {
	values := make(map[string]string, len(pathEnvironmentNames))
	for _, name := range pathEnvironmentNames {
		if value, present := os.LookupEnv(name); present {
			values[name] = value
		}
	}
	return values
}

// AgentPaths is the resolved selected-agent policy. Source and Target remain
// separate fields for future profiles, but today's host-state policy requires
// identical paths. The same value is passed to both execution back ends.
type AgentPaths struct {
	Mounts      []MountSpec
	Environment []EnvVar
}

// ResolveAgentPaths preserves the agent's own root rules. In particular, only
// an explicit CODEX_HOME is canonicalized; relative Grok and XDG values are
// relative to the launch directory, never to the host home.
func ResolveAgentPaths(tool, home, launchDir string, values map[string]string) (AgentPaths, error) {
	result := AgentPaths{}
	roots := map[string]string{"home": home}
	environment := map[string]string{}
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		environment[name] = values[name]
	}
	for _, spec := range agentStatePaths[tool] {
		base, ok := roots[spec.Base]
		if !ok {
			var err error
			base, err = resolveStateBase(spec.Base, home, launchDir, values, environment)
			if err != nil {
				return AgentPaths{}, err
			}
			roots[spec.Base] = base
		}
		if base == "" {
			continue
		}
		path := filepath.Join(base, spec.Path)
		if err := validateHomeBoundary(path, home, "agent state"); err != nil {
			return AgentPaths{}, err
		}
		info, err := os.Stat(path)
		if spec.Optional && os.IsNotExist(err) && !(spec.Kind == Directory && values[spec.Base] != "") {
			// A SQLite file needs its containing directory for WAL and shared
			// memory files. It must exist before launch so Cooper does not
			// create or export an unrelated parent directory.
			if spec.Base == "OPENCODE_DB" {
				return AgentPaths{}, fmt.Errorf("custom OpenCode database %s must exist before launch", path)
			}
			continue
		}
		if err != nil && !os.IsNotExist(err) {
			return AgentPaths{}, fmt.Errorf("inspect agent state %s: %w", path, err)
		}
		kind := spec.Kind
		if spec.Base == "OPENCODE_DB" {
			path, kind = filepath.Dir(path), Directory
			if err := validateHomeBoundary(path, home, "agent state"); err != nil {
				return AgentPaths{}, err
			}
		}
		if err == nil && spec.Kind == Directory && !info.IsDir() {
			return AgentPaths{}, fmt.Errorf("agent state %s must be a directory", path)
		}
		if err == nil && spec.Kind == File && !info.Mode().IsRegular() {
			return AgentPaths{}, fmt.Errorf("agent state %s must be a regular file", path)
		}
		result.Mounts = append(result.Mounts, MountSpec{ID: spec.ID, Source: path, Target: path, Access: ReadWrite, Kind: kind, Ownership: HostState})
	}
	if tool == "grok" {
		environment["GROK_LEADER_SOCKET"] = GrokLeaderSocket
	}
	// A custom file or directory can be inside another selected state root.
	// Keep only the outer mount; no extra authority or file bind is needed.
	result.Mounts = removeCoveredStateMounts(result.Mounts)
	names := make([]string, 0, len(environment))
	for name := range environment {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		_, present := values[name]
		result.Environment = append(result.Environment, EnvVar{Name: name, Value: environment[name], Unset: !present && environment[name] == ""})
	}
	return result, nil
}

func resolveStateBase(base, home, launchDir string, values, environment map[string]string) (string, error) {
	var name, fallback string
	switch base {
	case "codex":
		name, fallback = "CODEX_HOME", ".codex"
	case "grok":
		name, fallback = "GROK_HOME", ".grok"
	case "claude":
		name, fallback = "CLAUDE_CONFIG_DIR", ".claude"
	case "copilot":
		name, fallback = "COPILOT_HOME", ".copilot"
	case "copilot-cache":
		name = "COPILOT_CACHE_HOME"
		if values[name] == "" {
			path := copilotDefaultCache(home, values["XDG_CACHE_HOME"], runtime.GOOS)
			if !filepath.IsAbs(path) {
				path = filepath.Join(launchDir, path)
			}
			// Copilot uses Library/Caches on macOS. Give the Linux workload
			// the effective host path instead of its Linux default.
			environment[name] = path
			return path, nil
		}
	case "copilot-old-config", "copilot-old-state":
		name = "XDG_CONFIG_HOME"
		if base == "copilot-old-state" {
			name = "XDG_STATE_HOME"
		}
		// Current Copilot migrates these roots only when the XDG variable
		// is nonempty and COPILOT_HOME does not select a custom root.
		if values["COPILOT_HOME"] != "" || values[name] == "" {
			return "", nil
		}
	case "claude-settings":
		if values["CLAUDE_CONFIG_DIR"] != "" {
			return "", nil // The custom directory already includes this file.
		}
		return home, nil
	case "XDG_CONFIG_HOME":
		name, fallback = base, ".config"
	case "XDG_DATA_HOME":
		name, fallback = base, ".local/share"
	case "XDG_STATE_HOME":
		name, fallback = base, ".local/state"
	case "XDG_CACHE_HOME":
		name, fallback = base, ".cache"
	case "OPENCODE_CONFIG", "OPENCODE_CONFIG_DIR", "OPENCODE_DB":
		name = base
	default:
		return "", fmt.Errorf("unknown agent state base %q", base)
	}
	value, present := values[name]
	environment[name] = value
	if base == "claude" {
		value = norm.NFC.String(value)
	}
	if value == "" {
		if base == "claude" && present {
			return launchDir, nil
		}
		if fallback == "" {
			return "", nil
		}
		baseHome := home
		if base == "grok" {
			resolved, err := filepath.EvalSymlinks(home)
			if err == nil {
				baseHome = resolved
			}
			// The default Grok home is canonical. Preserve that result even
			// when the image home is a logical path through a host symlink.
			environment[name] = filepath.Join(baseHome, fallback)
		}
		path := filepath.Join(baseHome, fallback)
		if base == "claude" {
			path = norm.NFC.String(path)
		}
		return path, nil
	}
	if base == "OPENCODE_DB" {
		if value == ":memory:" {
			return "", nil
		}
		data, err := resolveStateBase("XDG_DATA_HOME", home, launchDir, values, environment)
		if err != nil {
			return "", err
		}
		dataRoot := filepath.Join(data, "opencode")
		path := value
		if !filepath.IsAbs(path) {
			path = filepath.Join(dataRoot, path)
		}
		if pathContains(dataRoot, path) {
			// The complete data root includes new databases and their WAL
			// files, whether the override is absolute or relative.
			return "", nil
		}
		return filepath.Clean(path), nil
	}
	path := value
	if !filepath.IsAbs(path) {
		path = filepath.Join(launchDir, path)
	}
	path = filepath.Clean(path)
	if base == "codex" {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return "", fmt.Errorf("resolve CODEX_HOME: %w", err)
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() {
			return "", fmt.Errorf("CODEX_HOME %s must be an existing directory", path)
		}
		path = resolved
		environment[name] = path
	}
	return path, nil
}

func copilotDefaultCache(home, xdgCache, platform string) string {
	if platform == "darwin" {
		return filepath.Join(home, "Library", "Caches", "copilot")
	}
	if xdgCache != "" {
		return filepath.Join(xdgCache, "copilot")
	}
	return filepath.Join(home, ".cache", "copilot")
}

// A symlink must not turn a workspace or selected state root into an export
// of the complete home. Check both spellings before creating directories.
func validateHomeBoundary(path, home, kind string) error {
	resolvedPath, err := resolveExistingPath(path)
	if err != nil {
		return err
	}
	resolvedHome, err := resolveExistingPath(home)
	if err != nil {
		return err
	}
	if pathContains(path, home) || pathContains(resolvedPath, resolvedHome) {
		return fmt.Errorf("%s %s overlaps the complete host home %s", kind, path, home)
	}
	return nil
}

func removeCoveredStateMounts(mounts []MountSpec) []MountSpec {
	var result []MountSpec
	for index, mount := range mounts {
		covered := false
		for otherIndex, other := range mounts {
			if index == otherIndex || other.Kind != Directory || !pathContains(other.Target, mount.Target) {
				continue
			}
			if other.Target != mount.Target || otherIndex < index {
				covered = true
				break
			}
		}
		if !covered {
			result = append(result, mount)
		}
	}
	return result
}

// ResolveMountInput fills the optional raw input once. Tests can supply a
// private environment; production callers supply HostPathEnvironment.
func ResolveMountInput(in MountInput) (MountInput, error) {
	if in.Agent != nil {
		return in, nil
	}
	values := make(map[string]string, len(in.Environment)+1)
	for name, value := range in.Environment {
		values[name] = value
	}
	agent, err := ResolveAgentPaths(in.ToolName, in.HomeDir, in.WorkspaceDir, values)
	if err != nil {
		return in, err
	}
	in.Agent = &agent
	return in, nil
}
