// Package aitool is the single ordered catalog of Cooper's built-in AI CLIs.
//
// It holds only static identity and UX metadata: names, host version commands,
// auto-approve flags, clipboard mode, and in-image home directories. Install
// recipes, OAuth, network policy, mounts, and proof prompts stay in their
// owning packages so this does not become a god abstraction.
package aitool

// Clipboard modes select the bridge that matches how a CLI reads images.
// Shim tools call xclip, xsel, or wl-paste, so Cooper can put wrapper scripts
// first in PATH. X11 tools read the selection in-process, so Cooper uses Xvfb
// and the X11 bridge. Claude and OpenCode call the helper tools. Codex uses
// arboard, and Copilot uses @teddyzhu/clipboard. Runtime inspection confirmed
// these two in-process paths. Custom tools use auto, which enables both paths.
const (
	ClipboardShim = "shim"
	ClipboardX11  = "x11"
	ClipboardAuto = "auto"
)

// Definition is the static identity of one built-in AI CLI.
type Definition struct {
	Name               string
	DisplayName        string
	HostVersionCommand []string
	AutoApproveArgs    string
	ClipboardMode      string
	// HomeDirs are paths relative to /home/user that the image must create
	// before runtime mounts attach (for example ".grok").
	HomeDirs []string
}

// definitions is the ordered built-in list. Append-only at the end so existing
// configure/test migrations stay deterministic.
var definitions = []Definition{
	{
		Name:               "claude",
		DisplayName:        "Claude Code",
		HostVersionCommand: []string{"claude", "--version"},
		AutoApproveArgs:    "--dangerously-skip-permissions",
		ClipboardMode:      ClipboardShim,
		HomeDirs:           []string{".claude"},
	},
	{
		Name:               "copilot",
		DisplayName:        "Copilot CLI",
		HostVersionCommand: []string{"copilot", "--version"},
		AutoApproveArgs:    "--allow-all-tools",
		ClipboardMode:      ClipboardX11,
		HomeDirs:           []string{".copilot"},
	},
	{
		Name:               "codex",
		DisplayName:        "Codex CLI",
		HostVersionCommand: []string{"codex", "--version"},
		AutoApproveArgs:    "--dangerously-bypass-approvals-and-sandbox",
		ClipboardMode:      ClipboardX11,
		HomeDirs:           []string{".codex"},
	},
	{
		Name:               "opencode",
		DisplayName:        "OpenCode",
		HostVersionCommand: []string{"opencode", "--version"},
		AutoApproveArgs:    "",
		ClipboardMode:      ClipboardShim,
		HomeDirs: []string{
			".config/opencode",
			".local/share/opencode",
			".local/state/opencode",
			".opencode",
		},
	},
	{
		Name:               "grok",
		DisplayName:        "Grok Build",
		HostVersionCommand: []string{"grok", "--version"},
		AutoApproveArgs:    "--always-approve",
		ClipboardMode:      ClipboardX11,
		// Grok keeps auth, config, sessions, history, memory, skills, logs,
		// locks, and update state below one root. Create and mount the root,
		// not selected children, so new Grok state is shared automatically.
		HomeDirs: []string{".grok"},
	},
}

// Definitions returns a defensive copy of the ordered built-in catalog.
func Definitions() []Definition {
	out := make([]Definition, len(definitions))
	for i, def := range definitions {
		out[i] = cloneDefinition(def)
	}
	return out
}

// Lookup returns the catalog entry for a built-in tool name.
func Lookup(name string) (Definition, bool) {
	for _, def := range definitions {
		if def.Name == name {
			return cloneDefinition(def), true
		}
	}
	return Definition{}, false
}

// IsBuiltin reports whether name is a reserved built-in AI tool.
func IsBuiltin(name string) bool {
	_, ok := Lookup(name)
	return ok
}

// Names returns the ordered lowercase built-in tool IDs.
func Names() []string {
	out := make([]string, len(definitions))
	for i, def := range definitions {
		out[i] = def.Name
	}
	return out
}

// ClipboardMode returns the clipboard bridge mode for a built-in tool.
// Unknown or custom tools default to auto so both shim and X11 plumbing run.
func ClipboardMode(name string) string {
	if def, ok := Lookup(name); ok && def.ClipboardMode != "" {
		return def.ClipboardMode
	}
	return ClipboardAuto
}

func cloneDefinition(def Definition) Definition {
	out := def
	if def.HostVersionCommand != nil {
		out.HostVersionCommand = append([]string(nil), def.HostVersionCommand...)
	}
	if def.HomeDirs != nil {
		out.HomeDirs = append([]string(nil), def.HomeDirs...)
	}
	return out
}
