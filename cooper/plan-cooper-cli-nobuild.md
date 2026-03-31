# Plan: Move AI Tool Installation from Build-Time to Runtime

## Overview

Currently, AI CLI tools (Claude Code, Copilot, Codex, OpenCode) are installed
during `docker build` as layers in the barrel image. This means every version
bump requires a full image rebuild (~5-10 minutes). This plan moves AI tool
installation to a runtime step using a mounted volume, so updates take seconds
instead of minutes.

### Architecture Change

**Before:**
```
cooper configure → cooper build (includes AI tools) → cooper up → cooper cli
                    ↑ slow rebuild on version change
```

**After:**
```
cooper configure → cooper build (base only, FAST) → cooper up → install AI tools → cooper cli
                                                      ↑ seconds, no rebuild
```

### Key Design Decision: `/usr/local` Mount

AI tools are installed into `~/.cooper/ai-tools/` on the host, which is mounted
at `/usr/local/` inside barrel containers. This captures where npm global
installs and most CLI installers place their files.

To make this work, Node.js and bubblewrap (currently in `/usr/local/`) are
relocated to `/usr/` in the Dockerfile so they're not hidden by the mount.

A short-lived **installer container** runs on `cooper-external` (has internet
access, not proxied) to perform the actual installation, then exits. Barrel
containers mount the result read-write.

---

## Work Package 1: Volume and Mount Design

### 1.1 Host Directory Structure

```
~/.cooper/
  ai-tools/                          # NEW: mounted as /usr/local/ in barrels
    bin/                             # Tool binaries and symlinks
    lib/
      node_modules/                  # npm global packages (copilot, codex)
    share/
      ca-certificates/
        cooper-ca.crt                # CA cert (written by installer/entrypoint)
    manifest.json                    # Tracks installed tool versions
  config.json                        # (unchanged)
  ...
```

### 1.2 manifest.json Format

```json
{
  "installed_at": "2026-04-01T10:30:00Z",
  "node_version": "22.12.0",
  "tools": {
    "claude": {
      "version": "2.1.87",
      "mode": "mirror",
      "install_method": "curl-script",
      "bin_path": "bin/claude"
    },
    "copilot": {
      "version": "1.0.12",
      "mode": "pin",
      "install_method": "npm",
      "bin_path": "bin/copilot"
    },
    "codex": {
      "version": "0.117.0",
      "mode": "latest",
      "install_method": "npm",
      "bin_path": "bin/codex"
    },
    "opencode": {
      "version": "1.3.7",
      "mode": "mirror",
      "install_method": "curl-script",
      "bin_path": "bin/opencode"
    }
  }
}
```

### 1.3 Mount in Barrel Containers

**File: `internal/docker/barrel.go` — `appendVolumeMounts()`**

Add mount for AI tools volume:
```go
// AI tools volume (installed at runtime, not baked into image).
aiToolsDir := filepath.Join(cooperDir, "ai-tools")
if dirExists(aiToolsDir) {
    args = append(args, "-v", fmt.Sprintf("%s:/usr/local:rw", aiToolsDir))
}
```

This replaces the barrel's `/usr/local/` with the host-managed AI tools
directory. Since Node.js and bubblewrap are relocated to `/usr/` (WP3),
nothing critical is hidden.

The mount is `:rw` because some tools write caches/state into their install
directories at runtime.

### 1.4 Mount in Installer Container

The installer runs from `barrel-base` image on `cooper-external`:
```go
args := []string{
    "run", "--rm",
    "--name", "cooper-ai-installer",
    "--network", NetworkExternal,
    "-v", fmt.Sprintf("%s:/usr/local:rw", aiToolsDir),
    // CA cert for potential HTTPS verification during install
    "-v", fmt.Sprintf("%s:/etc/cooper/cooper-ca.pem:ro", caCertPath),
    // Pass install script as a bind-mounted file
    "-v", fmt.Sprintf("%s:/install.sh:ro", installScriptPath),
    docker.GetImageBarrelBase(),
    "bash", "/install.sh",
}
```

### 1.5 Ensure Host Directories

**File: `internal/docker/barrel.go` — `ensureBarrelHostDirs()`**

Add `~/.cooper/ai-tools/bin/` to the list of directories created before
Docker mount.

Also add `~/.cooper/ai-tools/lib/`, `~/.cooper/ai-tools/share/ca-certificates/`.

---

## Work Package 2: Installer Container Design

### 2.1 New File: `internal/docker/installer.go`

This file manages the AI tool installation lifecycle.

```go
package docker

// InstallAITools runs a short-lived container on cooper-external that
// installs the specified AI tools into the ai-tools volume.
//
// Flow:
// 1. Wipe ~/.cooper/ai-tools/ contents
// 2. Run installer container from barrel-base image
// 3. Container has internet access (cooper-external, no proxy)
// 4. Install each enabled AI tool at the configured version
// 5. Write manifest.json with installed versions
// 6. Container exits
//
// The progress callback receives line-by-line output from the installer.
func InstallAITools(cfg *config.Config, cooperDir string, progress func(line string)) error

// InstallAIToolsWithOutput is the channel-based version for TUI integration.
// Returns a lines channel and an error channel (same pattern as BuildImageWithOutput).
func InstallAIToolsWithOutput(cfg *config.Config, cooperDir string) (<-chan string, <-chan error)

// ReadAIToolsManifest reads the manifest.json from the ai-tools directory.
// Returns nil if no manifest exists (tools not installed yet).
func ReadAIToolsManifest(cooperDir string) (*AIToolsManifest, error)

// AIToolsManifest tracks what AI tools are installed in the volume.
type AIToolsManifest struct {
    InstalledAt string                       `json:"installed_at"`
    NodeVersion string                       `json:"node_version"`
    Tools       map[string]InstalledToolInfo `json:"tools"`
}

type InstalledToolInfo struct {
    Version       string `json:"version"`
    Mode          string `json:"mode"`
    InstallMethod string `json:"install_method"`
    BinPath       string `json:"bin_path"`
}
```

### 2.2 New File: `internal/templates/install-ai.sh.tmpl`

This is the install script that runs INSIDE the installer container. It is
generated from a template (like entrypoint.sh.tmpl) so it can be
parameterized with tool versions.

```bash
#!/bin/bash
# Cooper AI Tools Installer - Generated by cooper
# Runs inside a temporary container on cooper-external (has internet).
set -euo pipefail

echo "=== Cooper AI Tools Installer ==="

# The volume is mounted at /usr/local. Wipe existing content.
echo "Cleaning /usr/local/..."
rm -rf /usr/local/*

# Ensure directory structure.
mkdir -p /usr/local/bin /usr/local/lib /usr/local/share

# ============================================================================
# CA Certificate (needed for HTTPS during installation)
# ============================================================================
if [ -f /etc/cooper/cooper-ca.pem ]; then
    mkdir -p /usr/local/share/ca-certificates
    cp /etc/cooper/cooper-ca.pem /usr/local/share/ca-certificates/cooper-ca.crt
fi

{{if .HasCopilot}}
# ============================================================================
# GitHub Copilot CLI (npm)
# ============================================================================
echo "Installing Copilot CLI{{if .CopilotVersion}} {{.CopilotVersion}}{{end}}..."
npm install -g --prefix /usr/local @github/copilot{{if .CopilotVersion}}@{{.CopilotVersion}}{{end}}
echo "  Copilot: $(copilot --version 2>&1 || echo installed)"
{{end}}

{{if .HasCodex}}
# ============================================================================
# OpenAI Codex CLI (npm)
# ============================================================================
echo "Installing Codex CLI{{if .CodexVersion}} {{.CodexVersion}}{{end}}..."
npm install -g --prefix /usr/local @openai/codex{{if .CodexVersion}}@{{.CodexVersion}}{{end}}
echo "  Codex: $(codex --version 2>&1 || echo installed)"
{{end}}

{{if .HasClaudeCode}}
# ============================================================================
# Claude Code (curl installer → redirected to /usr/local)
# ============================================================================
echo "Installing Claude Code{{if .ClaudeVersion}} {{.ClaudeVersion}}{{end}}..."
# Claude installer puts binary at ~/.local/bin/claude. We install to a temp
# HOME and then move the binary to /usr/local/bin/.
export CLAUDE_HOME=/tmp/claude-install
mkdir -p "$CLAUDE_HOME"
HOME="$CLAUDE_HOME" curl -fsSL https://claude.ai/install.sh | HOME="$CLAUDE_HOME" bash{{if .ClaudeVersion}} -s -- {{.ClaudeVersion}}{{end}}
# Move the installed binary/directory to /usr/local.
if [ -f "$CLAUDE_HOME/.local/bin/claude" ]; then
    cp -r "$CLAUDE_HOME/.local/bin/claude" /usr/local/bin/claude
    chmod +x /usr/local/bin/claude
fi
# If Claude installs additional files, copy them too.
if [ -d "$CLAUDE_HOME/.local/lib/claude" ]; then
    cp -r "$CLAUDE_HOME/.local/lib/claude" /usr/local/lib/claude
fi
HOME="$CLAUDE_HOME" /usr/local/bin/claude install 2>/dev/null || true
echo "  Claude: $(/usr/local/bin/claude --version 2>&1 || echo installed)"
{{end}}

{{if .HasOpenCode}}
# ============================================================================
# OpenCode (curl installer → redirected to /usr/local)
# ============================================================================
echo "Installing OpenCode{{if .OpenCodeVersion}} {{.OpenCodeVersion}}{{end}}..."
export OPENCODE_HOME=/tmp/opencode-install
mkdir -p "$OPENCODE_HOME"
HOME="$OPENCODE_HOME" curl -fsSL https://opencode.ai/install | HOME="$OPENCODE_HOME" bash{{if .OpenCodeVersion}} -s -- --version {{.OpenCodeVersion}}{{end}}
# OpenCode installs to ~/.opencode/bin/opencode.
if [ -d "$OPENCODE_HOME/.opencode/bin" ]; then
    cp -r "$OPENCODE_HOME/.opencode" /usr/local/lib/opencode
    ln -sf /usr/local/lib/opencode/bin/opencode /usr/local/bin/opencode
fi
echo "  OpenCode: $(/usr/local/bin/opencode --version 2>&1 || echo installed)"
{{end}}

# ============================================================================
# Write manifest.json
# ============================================================================
cat > /usr/local/manifest.json << 'MANIFEST'
{{.ManifestJSON}}
MANIFEST

echo ""
echo "=== Installation complete ==="
ls -la /usr/local/bin/
```

### 2.3 Template Data

**File: `internal/templates/templates.go`**

Add new data struct and render function:

```go
type installAIData struct {
    HasClaudeCode    bool
    ClaudeVersion    string
    HasCopilot       bool
    CopilotVersion   string
    HasCodex         bool
    CodexVersion     string
    HasOpenCode      bool
    OpenCodeVersion  string
    ManifestJSON     string // Pre-rendered JSON for manifest.json
}

func RenderInstallAIScript(cfg *config.Config) (string, error)
```

### 2.4 Installation Flow (in `internal/docker/installer.go`)

```go
func InstallAITools(cfg *config.Config, cooperDir string, progress func(line string)) error {
    aiToolsDir := filepath.Join(cooperDir, "ai-tools")

    // 1. Ensure directories exist.
    os.MkdirAll(filepath.Join(aiToolsDir, "bin"), 0755)
    os.MkdirAll(filepath.Join(aiToolsDir, "lib"), 0755)

    // 2. Render the install script.
    script, err := templates.RenderInstallAIScript(cfg)
    // Write to cooperDir/install-ai.sh

    // 3. Run the installer container.
    // Uses barrel-base image, cooper-external network, mounts ai-tools as /usr/local.
    // Streams output via progress callback.

    // 4. Read and validate manifest.json was written.

    // 5. Update config with installed versions (ContainerVersion).
}
```

### 2.5 Important: The Installer Does NOT Use the Proxy

The installer container runs on `cooper-external` without `HTTP_PROXY`/
`HTTPS_PROXY` env vars. It has direct internet access. This is critical because:
- npm needs to download packages from registry.npmjs.org
- curl needs to download from claude.ai, opencode.ai
- These downloads don't need to go through the SSL bump proxy

No whitelist bypass is needed — the installer simply isn't on the proxied
network.

---

## Work Package 3: Dockerfile Changes

### 3.1 Relocate Node.js from `/usr/local` to `/usr`

**File: `internal/templates/cli.Dockerfile.tmpl`**

Change Node.js tarball extraction:

```dockerfile
# BEFORE:
RUN curl -fsSL "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.xz" \
    | tar -xJ -C /usr/local --strip-components=1

# AFTER:
RUN curl -fsSL "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.xz" \
    | tar -xJ -C /usr --strip-components=1 \
    && node --version && npm --version
```

This puts `node` at `/usr/bin/node` and `npm` at `/usr/bin/npm`, which won't
be hidden when `/usr/local` is mounted.

### 3.2 Relocate Bubblewrap to `/usr`

```dockerfile
# BEFORE:
meson setup _builddir --prefix=/usr/local

# AFTER:
meson setup _builddir --prefix=/usr
```

The existing `ln -sf /usr/local/bin/bwrap /usr/bin/bwrap` line can be removed
since bwrap will already be at `/usr/bin/bwrap`.

### 3.3 Remove ALL AI Tool Installation Steps

Remove these entire sections from `cli.Dockerfile.tmpl`:
- Lines 120-123: `CACHE_BUST_AI` ARG
- Lines 124-135: Claude Code installation
- Lines 136-141: Copilot CLI installation
- Lines 142-147: Codex CLI installation
- Lines 148-158: OpenCode installation

The Dockerfile should go directly from the user setup / language tools to CA
certificate injection.

### 3.4 Remove AI-Only apt Dependencies

The `apt-get install` block at the top currently includes AI-tool-specific
dependencies:
- `xvfb`, `xclip`, `inotify-tools` — needed by OpenCode
- `build-essential`, `meson`, `ninja-build`, `pkg-config`, `libcap-dev` — needed by Codex's bubblewrap build

These need to stay in the image since they're runtime dependencies, not just
install-time. But the bubblewrap build dependencies (`build-essential`, `meson`,
`ninja-build`, `pkg-config`, `libcap-dev`) can be removed after building
bubblewrap (multi-stage or apt-get remove).

Actually, revisiting: bubblewrap is needed at RUNTIME by Codex (it uses bwrap
for sandboxing). But the BUILD dependencies (meson, ninja, etc.) are only
needed at build time. We should:
1. Keep bubblewrap binary in the image (build it, then remove build deps)
2. Keep xvfb, xclip, inotify-tools if OpenCode is enabled
3. Remove meson, ninja-build, etc. after bubblewrap is compiled

```dockerfile
# Build bubblewrap, then remove build deps.
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential meson ninja-build pkg-config libcap-dev && \
    BWRAP_VERSION=0.11.0 && ... (build bubblewrap) ... && \
    apt-get purge -y build-essential meson ninja-build pkg-config libcap-dev && \
    apt-get autoremove -y && rm -rf /var/lib/apt/lists/*
```

### 3.5 Update CA Certificate Handling

Since `/usr/local` will be a mount, the CA cert injected during build at
`/usr/local/share/ca-certificates/cooper-ca.crt` would be hidden. Change to
inject at a path outside `/usr/local`:

```dockerfile
# BEFORE:
COPY cooper-ca.pem /usr/local/share/ca-certificates/cooper-ca.crt
RUN update-ca-certificates

# AFTER:
COPY cooper-ca.pem /etc/cooper/cooper-ca.crt
RUN cp /etc/cooper/cooper-ca.crt /usr/local/share/ca-certificates/cooper-ca.crt && \
    update-ca-certificates && \
    chmod 666 /etc/ssl/certs/ca-certificates.crt
```

And update `NODE_EXTRA_CA_CERTS`:
```dockerfile
ENV NODE_EXTRA_CA_CERTS=/etc/cooper/cooper-ca.crt
```

The entrypoint runtime CA update also needs adjustment (WP4).

### 3.6 Update NPM_CONFIG_PREFIX

Since npm is now at `/usr/bin/npm` and we don't want npm global installs going
to the (hidden) `/usr/local`, change:

```dockerfile
# BEFORE:
ENV NPM_CONFIG_PREFIX=/home/user/.npm-global

# AFTER: (not needed — /usr/local is mounted volume, npm defaults to /usr/local)
# Remove NPM_CONFIG_PREFIX entirely. When /usr/local is mounted, npm install -g
# will write to the mounted volume, which is exactly what we want.
```

Wait, but for the barrel container at runtime, `npm install -g` would write to
the mounted AI tools volume. That's actually fine for AI tools but could be
confusing if the user runs `npm install -g something` in their barrel. We should
keep `NPM_CONFIG_PREFIX` pointing somewhere neutral for user-initiated npm
installs, and only override it in the installer.

```dockerfile
# Keep for user-initiated npm global installs inside barrel:
ENV NPM_CONFIG_PREFIX=/home/user/.npm-global
```

The installer script explicitly uses `--prefix /usr/local` to install to the
mounted volume.

### 3.7 Update PATH

```dockerfile
# BEFORE:
ENV PATH=/home/user/.local/bin:/home/user/.npm-global/bin:/home/user/.opencode/bin:$PATH

# AFTER:
ENV PATH=/usr/local/bin:/home/user/.npm-global/bin:$PATH
```

`/usr/local/bin` is already in the default PATH on Debian, but being explicit
ensures it's first. The `.local/bin` and `.opencode/bin` are no longer needed
since tools are in `/usr/local/bin`.

### 3.8 Remove cliDockerfileData AI Fields

**File: `internal/templates/templates.go`**

Remove from `cliDockerfileData`:
- `HasClaudeCode`, `ClaudeVersion`
- `HasCopilot`, `CopilotVersion`
- `HasCodex`, `CodexVersion`
- `HasOpenCode`, `OpenCodeVersion`

Keep `HasCodex` and `HasOpenCode` booleans ONLY if they control apt dependencies
(xvfb, bubblewrap). Otherwise, make the apt dependencies always-installed
(they're small) and remove the booleans entirely.

**Decision:** Keep `HasCodex` (controls bubblewrap build, ~30s) and
`HasOpenCode` (controls xvfb/xclip install). These are runtime dependencies
that must be in the image. Rename them to clarify they're about deps, not
tool installation:

```go
type cliDockerfileData struct {
    HasGo            bool
    GoVersion        string
    HasNode          bool
    NodeVersion      string
    HasPython        bool
    PythonVersion    string
    NeedsBubblewrap  bool   // Codex requires bubblewrap at runtime
    NeedsXvfb       bool   // OpenCode requires virtual display
    ProxyPort        int
}
```

Update `buildCLIDockerfileData()` accordingly.

---

## Work Package 4: Entrypoint Changes

### 4.1 Update CA Certificate Runtime Handling

**File: `internal/templates/entrypoint.sh.tmpl`**

The entrypoint currently copies the volume-mounted CA cert to
`/usr/local/share/ca-certificates/`. Since `/usr/local` is now the AI tools
mount, this path is available but we should also ensure the directory exists:

```bash
# ============================================================================
# Runtime CA certificate update
# ============================================================================
if [ -f /etc/cooper/cooper-ca.pem ]; then
    mkdir -p /usr/local/share/ca-certificates
    cp /etc/cooper/cooper-ca.pem /usr/local/share/ca-certificates/cooper-ca.crt 2>/dev/null || true
    cat /etc/cooper/cooper-ca.pem >> /etc/ssl/certs/ca-certificates.crt 2>/dev/null || true
    export NODE_EXTRA_CA_CERTS=/etc/cooper/cooper-ca.pem
fi
```

### 4.2 Update Auto-Approve Aliases

The aliases should be conditional on whether the tool binary actually exists
in the PATH (since tools might not be installed yet):

```bash
# Cooper: Auto-approve aliases (container is network-isolated)
{{- if .HasClaudeCode}}
command -v claude &>/dev/null && alias claude='claude --dangerously-skip-permissions'
{{- end}}
```

Wait — the entrypoint template uses `HasClaudeCode` etc. which are set at
template generation time (during `cooper build`). Since we still know which
tools are configured, these flags are still valid. The aliases will just be
no-ops if the tool isn't installed yet.

Actually, keep the aliases as-is. If the tool isn't installed, the alias
exists but does nothing (the command won't be found). When the tool IS
installed, the alias works. No change needed.

### 4.3 Update entrypointData

The `entrypointData` struct still needs `HasClaudeCode`, `HasCopilot`,
`HasCodex`, `HasOpenCode` for the alias generation. These don't change.

### 4.4 Add AI Tools Not Installed Warning

Add a check at the top of entrypoint that warns if AI tools volume is empty:

```bash
# Check if AI tools are installed.
if [ ! -f /usr/local/manifest.json ]; then
    echo ""
    echo "WARNING: AI tools not installed yet."
    echo "   Run AI tool installation from the Cooper control panel (cooper up)."
    echo ""
fi
```

### 4.5 PATH in .bashrc

Update the PATH export in .bashrc to include `/usr/local/bin`:

```bash
export PATH="/usr/local/bin:/home/user/.npm-global/bin:$PATH"
```

(This replaces the current `/home/user/.local/bin:/home/user/.npm-global/bin`.)

---

## Work Package 5: Config Changes

### 5.1 No Schema Changes Needed

The `config.Config` struct already has `AITools []ToolConfig` with all
necessary fields (Name, Enabled, Mode, PinnedVersion, HostVersion,
ContainerVersion). No structural changes needed.

### 5.2 ContainerVersion Semantics Change

Currently `ContainerVersion` means "version baked into the Docker image."
It will now mean "version installed in the AI tools volume." The field name
is still appropriate since it's what's available inside the container.

### 5.3 New: `AIToolsInstalled` Config Field (Optional)

Consider adding a boolean or timestamp to track whether AI tools have been
installed:

```go
type Config struct {
    // ... existing fields ...
    AIToolsInstalledAt string `json:"ai_tools_installed_at,omitempty"`
}
```

This helps `cooper up` detect first-run scenarios and prompt for installation.

Alternatively, just check for `~/.cooper/ai-tools/manifest.json` existence.

**Decision:** Check manifest.json directly rather than adding config fields.
Simpler, and the manifest is the source of truth.

---

## Work Package 6: Build Command Changes

### 6.1 `runBuild()` in `main.go`

Remove AI tool version resolution from the build flow:

```go
// BEFORE:
resolveLatestVersions(cfg)  // Resolves ALL tools including AI

// AFTER:
resolveLatestProgrammingVersions(cfg)  // Only resolves programming tools
```

Create `resolveLatestProgrammingVersions()` that only operates on
`cfg.ProgrammingTools`, not `cfg.AITools`.

### 6.2 Remove CACHE_BUST_AI

Since AI tools are no longer in the Dockerfile, the `CACHE_BUST_AI` build arg
is unnecessary. Remove it from:
- `cli.Dockerfile.tmpl`
- `runUpdate()` in main.go (the `aiChanged` path that busts AI cache)

### 6.3 Update `updateContainerVersions()`

Only update ContainerVersion for programming tools after build:

```go
func updateContainerVersions(cfg *config.Config) {
    for i := range cfg.ProgrammingTools {
        cfg.ProgrammingTools[i].RefreshContainerVersion()
    }
    // AI tool ContainerVersion is updated after install, not build.
}
```

### 6.4 Build Becomes Faster

Without AI tool installation, the barrel image build drops from ~5-10 minutes
to ~1-2 minutes (just base OS + programming tools + bubblewrap). The proxy
image build is unchanged.

---

## Work Package 7: Update Command Changes

### 7.1 Split `runUpdate()` Logic

**File: `main.go` — `runUpdate()`**

The update command currently handles both programming and AI tool version
mismatches, rebuilding the Docker image for both. Split this:

- **Programming tools mismatch:** Still triggers Docker image rebuild
  (same as today, using CACHE_BUST_LANG).
- **AI tools mismatch:** Triggers AI tools reinstall via `InstallAITools()`
  (no Docker rebuild needed).

```go
func runUpdate(cmd *cobra.Command, args []string) error {
    cfg, cooperDir, err := loadConfig()

    langChanged := false
    aiChanged := false

    // Check programming tools (unchanged logic)...

    // Check AI tools — compare against manifest.json instead of container image.
    manifest, _ := docker.ReadAIToolsManifest(cooperDir)
    for i, tool := range cfg.AITools {
        if !tool.Enabled { continue }
        // Compare manifest version against expected version...
        // Set aiChanged = true if mismatch
    }

    if langChanged {
        // Rebuild Docker image (same as today)
    }

    if aiChanged {
        // Reinstall AI tools (NEW: fast path, no Docker rebuild)
        fmt.Fprintln(os.Stderr, "Reinstalling AI tools...")
        if err := docker.InstallAITools(cfg, cooperDir, func(line string) {
            fmt.Fprintln(os.Stderr, line)
        }); err != nil {
            return fmt.Errorf("AI tool installation failed: %w", err)
        }
    }
}
```

---

## Work Package 8: TUI Integration — AI Tools Management

### 8.1 New Tab vs. Settings Section

**Decision:** Add a new **"AI Tools"** tab to the TUI. This is a major
operation (stop containers, install, restart) that deserves its own screen
rather than being crammed into Settings.

**File: `internal/tui/theme/tabs.go`** (or wherever tabs are defined)

Add `TabAITools` between `TabConfigure` (Settings) and `TabAbout`:

```go
const (
    TabContainers TabID = iota
    TabMonitor
    TabBlocked
    TabAllowed
    TabBridgeLogs
    TabBridgeRoutes
    TabConfigure     // Settings
    TabAITools       // NEW
    TabAbout
)
```

### 8.2 New File: `internal/tui/aitools/model.go`

The AI Tools tab model:

```go
package aitools

type Model struct {
    tools       []toolRow      // Current config + installed status
    cursor      int
    editMode    bool           // Editing a tool's version/mode
    installing  bool           // Installation in progress
    installLog  []string       // Lines from installer output
    scrollOffset int
    width, height int
}

type toolRow struct {
    Name             string
    Enabled          bool
    Mode             config.VersionMode
    ConfiguredVersion string  // What the user wants
    InstalledVersion  string  // What's actually in the volume
    HostVersion       string  // Detected on host
    NeedsUpdate       bool   // Mismatch between configured and installed
}
```

### 8.3 AI Tools Tab Layout

```
+----------------------------------------------------------------------+
| 🥃 Cooper — AI Tools                                                 |
+----------------------------------------------------------------------+
| Containers | Monitor | Blocked | ... | AI Tools | About              |
+----------------------------------------------------------------------+
|                                                                      |
|  ── Installed AI Tools ──────────────────────────────────────────     |
|                                                                      |
|  TOOL         MODE      CONFIGURED    INSTALLED     STATUS           |
|  ──────────── ──────── ──────────── ──────────── ─────────           |
|  Claude Code  mirror    2.1.87        2.1.87        ✓ Up to date     |
|  Copilot CLI  latest    1.0.15        1.0.12        ⚠ Update avail   |
|  Codex CLI    pin       0.117.0       0.117.0       ✓ Up to date     |
|  OpenCode     off       —             —             — Disabled        |
|                                                                      |
|  Last installed: 2026-03-30 14:23                                    |
|                                                                      |
|  ── Actions ──────────────────────────────────────────────────────    |
|                                                                      |
|  [i Install/Update]  [Space Toggle]  [Enter Edit Mode/Version]       |
|                                                                      |
+----------------------------------------------------------------------+
| [i Install]  [Space On/Off]  [Enter Edit]  [Esc Back]                |
+----------------------------------------------------------------------+
```

### 8.4 Key Bindings

- **Space**: Toggle tool enabled/disabled
- **Enter**: Open mode/version editor (radio: Mirror/Latest/Pin + version input)
- **i**: Trigger installation (with confirmation modal)
- **r**: Refresh — check for available updates
- **Up/Down**: Navigate tool list

### 8.5 Installation Flow (TUI)

When user presses `i` (Install/Update):

1. **Pre-check**: Are there any running barrels?
   - If yes: Show confirmation modal:
     ```
     ┌─────────────────────────────────────────────┐
     │  Install AI Tools                            │
     │                                              │
     │  This will stop all running CLI containers:  │
     │    • barrel-myproject                        │
     │    • barrel-other-workspace                  │
     │                                              │
     │  Containers will be restarted after install. │
     │                                              │
     │  [Enter Confirm]    [Esc Cancel]             │
     └─────────────────────────────────────────────┘
     ```
   - If no barrels: Skip confirmation, proceed directly.

2. **Stop all barrels** (if any were running).

3. **Show installation progress modal:**
   ```
   ┌─────────────────────────────────────────────┐
   │  Installing AI Tools...                      │
   │                                              │
   │  Cleaning /usr/local/...                     │
   │  Installing Copilot CLI 1.0.15...            │
   │  Installing Codex CLI 0.117.0...             │
   │  Installing Claude Code 2.1.87...            │
   │  ████████████████░░░░░░░░  67%               │
   │                                              │
   │  [Esc Cancel]                                │
   └─────────────────────────────────────────────┘
   ```

4. **On success:**
   ```
   ┌─────────────────────────────────────────────┐
   │  ✓ AI Tools Installed                        │
   │                                              │
   │  Claude Code  2.1.87  ✓                      │
   │  Copilot CLI  1.0.15  ✓                      │
   │  Codex CLI    0.117.0 ✓                      │
   │                                              │
   │  Stopped barrels have been restarted.        │
   │                                              │
   │  [Enter OK]                                  │
   └─────────────────────────────────────────────┘
   ```

5. **On failure:**
   - Show error in modal with full output
   - Do NOT restart barrels (let user fix issue first)

### 8.6 Integration with Root Model

**File: `internal/tui/app.go`**

Add message types for AI tools installation:

```go
// AI Tools installation messages.
type aiToolsInstallRequestMsg struct{}
type aiToolsInstallProgressMsg struct{ Line string }
type aiToolsInstallResultMsg struct{ Err error }
```

The root model handles the async installation flow:
1. Receives `aiToolsInstallRequestMsg` from AI Tools tab
2. Shows progress modal
3. Runs installation in background goroutine
4. Sends progress lines to TUI via `p.Send()`
5. On completion, sends result and updates config

### 8.7 CooperApp Methods

**File: `internal/app/cooper.go`** and **`internal/app/app.go`** (interface)

Add to the App interface:

```go
// AI tool management.
InstallAITools() (<-chan string, <-chan error)
AIToolsManifest() (*docker.AIToolsManifest, error)
StopAllBarrels() error
RestartBarrels(names []string) error
```

Implementation in CooperApp:

```go
func (a *CooperApp) InstallAITools() (<-chan string, <-chan error) {
    return docker.InstallAIToolsWithOutput(a.cfg, a.cooperDir)
}

func (a *CooperApp) AIToolsManifest() (*docker.AIToolsManifest, error) {
    return docker.ReadAIToolsManifest(a.cooperDir)
}

func (a *CooperApp) StopAllBarrels() error {
    barrels, err := docker.ListBarrels()
    if err != nil {
        return err
    }
    for _, b := range barrels {
        if err := docker.StopBarrel(b.Name); err != nil {
            return err
        }
    }
    return nil
}
```

---

## Work Package 9: Configure Wizard Changes

### 9.1 AI CLI Tab Behavior

**File: `internal/configure/aicli.go`**

The configure wizard's AI CLI tab remains mostly the same — users still select
which tools to enable and what version mode to use. The difference is:

- **Before**: These choices determined what was baked into the Docker image.
- **After**: These choices determine what will be installed at runtime.

The save screen messaging changes (WP9.2).

### 9.2 Save Screen Changes

**File: `internal/configure/save.go`**

Update the save screen to reflect the new flow:

```go
// "Files to Write" section no longer mentions AI tool installation.
// Instead, note that AI tools will be installed separately.
m.doneMsgs = append(m.doneMsgs, "Configuration saved.")
m.doneMsgs = append(m.doneMsgs, "Run 'cooper build' to rebuild base images.")
m.doneMsgs = append(m.doneMsgs, "AI tools will be installed when you run 'cooper up'.")
```

### 9.3 Save & Build Still Works

The "Save & Build" button should still trigger `cooper build`. The build is now
faster since it doesn't include AI tools.

After build, if AI tools are configured but not yet installed, show a message
suggesting the user run `cooper up` to install them.

---

## Work Package 10: Proof/Diagnostics Changes

### 10.1 Update `checkToolInstallations()`

**File: `internal/proof/proof.go`**

The AI tool version checks should still work — they run `{tool} --version`
inside the barrel container, and the tools are available via the `/usr/local`
mount. No changes needed to the check logic.

However, add a new check for the AI tools volume:

```go
func checkAIToolsVolume(container string) ProofResult {
    // Check if /usr/local/manifest.json exists.
    out, err := dockerExec(container, "test -f /usr/local/manifest.json && echo found || echo missing")
    if err != nil || strings.Contains(out, "missing") {
        return ProofResult{
            Name:   "AI Tools Volume",
            Status: StatusFAIL,
            Detail: "AI tools volume not mounted or empty (run install from cooper up)",
        }
    }
    return ProofResult{
        Name:   "AI Tools Volume",
        Status: StatusOK,
        Detail: "AI tools volume mounted and manifest present",
    }
}
```

Add this check to `RunAllChecks()` before the tool installation checks.

---

## Work Package 11: Test Changes

### 11.1 `test-docker-build.sh`

**Major changes:**

The barrel image no longer contains AI tools, so runtime assertions for AI
tool versions need to change. Instead of checking the image directly, we need
to:

1. Run the installer against the built barrel-base image
2. Then verify the tools in the ai-tools directory

Add a new phase to the test:

```bash
# Phase: AI Tools Installation (separate from Docker build)
info "${mode}: Running AI tools installation..."
AI_TOOLS_DIR="${test_dir}/ai-tools"
mkdir -p "$AI_TOOLS_DIR"

# Run the installer using the test barrel-base image.
./cooper install-ai --config "$test_dir" --prefix "$prefix" 2>&1

# Verify manifest.json.
if [ -f "${AI_TOOLS_DIR}/manifest.json" ]; then
    pass "${mode}: AI tools manifest created"
else
    fail "${mode}: AI tools manifest not found"
fi

# Verify tools are installed (mount the volume and check).
barrel_run_with_ai() {
    docker run --rm --entrypoint "" \
        -v "${AI_TOOLS_DIR}:/usr/local:rw" \
        "$barrel_image" "$@" 2>&1
}

# Check each AI tool version using the mounted volume.
```

### 11.2 `test-e2e.sh`

**Changes:**

1. After `cooper build`, add AI tools installation phase:
   ```bash
   section "Phase 1b: Install AI Tools"
   info "Running AI tools installation..."
   "$COOPER" install-ai --config "$CONFIG_DIR" --prefix "$PREFIX" 2>&1
   ```

2. Update barrel container start to mount AI tools:
   ```bash
   BARREL_ARGS+=(
       "-v" "${CONFIG_DIR}/ai-tools:/usr/local:rw"
   )
   ```

3. Phase 9b (AI Tool Installations) assertions remain the same — they run
   `tool --version` inside the barrel which now reads from the mount.

4. Phase 10b (Login Shell PATH) — verify `/usr/local/bin` is in PATH.

### 11.3 Unit Tests

**File: `internal/templates/templates_test.go`**

Update Dockerfile template tests to verify AI tool installation steps are
NOT present in the generated Dockerfile.

**File: `internal/docker/installer_test.go`** (NEW)

Test `ReadAIToolsManifest()` and manifest parsing.

### 11.4 Test Config Fixtures

**Files: `.testfiles/config-*.json`**

These remain unchanged — they still specify AI tools. The difference is that
the build step no longer installs them; the install-ai step does.

---

## Work Package 12: New CLI Command — `cooper install-ai`

### 12.1 Command Definition

Add a new Cobra command for non-TUI AI tool installation:

```go
var installAICmd = &cobra.Command{
    Use:   "install-ai",
    Short: "Install or update AI CLI tools",
    Long:  "Installs AI tools into the shared volume. Stops running barrels first.",
    RunE:  runInstallAI,
}
```

### 12.2 Implementation

```go
func runInstallAI(cmd *cobra.Command, args []string) error {
    cfg, cooperDir, err := loadConfig()

    // 1. Resolve versions (mirror → detect host, latest → resolve upstream).
    resolveLatestAIVersions(cfg)

    // 2. Stop all running barrels.
    barrels, _ := docker.ListBarrels()
    for _, b := range barrels {
        fmt.Fprintf(os.Stderr, "Stopping %s...\n", b.Name)
        docker.StopBarrel(b.Name)
    }

    // 3. Ensure barrel-base image exists.
    exists, _ := docker.ImageExists(docker.GetImageBarrelBase())
    if !exists {
        return fmt.Errorf("barrel-base image not found, run 'cooper build' first")
    }

    // 4. Run installation.
    fmt.Fprintln(os.Stderr, "Installing AI tools...")
    err = docker.InstallAITools(cfg, cooperDir, func(line string) {
        fmt.Fprintln(os.Stderr, "  "+line)
    })
    if err != nil {
        return fmt.Errorf("installation failed: %w", err)
    }

    // 5. Update config with installed versions.
    updateAIContainerVersions(cfg)
    configPath := filepath.Join(cooperDir, "config.json")
    config.SaveConfig(configPath, cfg)

    fmt.Fprintln(os.Stderr, "AI tools installed successfully.")
    return nil
}
```

### 12.3 `resolveLatestAIVersions()`

Similar to existing `resolveLatestVersions()` but only for AI tools:

```go
func resolveLatestAIVersions(cfg *config.Config) {
    for i := range cfg.AITools {
        t := &cfg.AITools[i]
        if !t.Enabled { continue }
        switch t.Mode {
        case config.ModeLatest:
            v, _ := config.ResolveLatestVersion(t.Name)
            if v != "" { t.PinnedVersion = v }
        case config.ModeMirror:
            v, _ := config.DetectHostVersion(t.Name)
            if v != "" { t.HostVersion = v }
        }
    }
}
```

---

## Work Package 13: Migration for Existing Users

### 13.1 First-Run Detection

When `cooper up` starts and finds:
- AI tools configured in config.json
- No `~/.cooper/ai-tools/manifest.json`

It should show a one-time prompt:

```
┌─────────────────────────────────────────────┐
│  AI Tools Setup Required                     │
│                                              │
│  Cooper now installs AI tools separately     │
│  from the Docker image for faster updates.   │
│                                              │
│  Install now?                                │
│                                              │
│  [Enter Install Now]    [Esc Skip]           │
└─────────────────────────────────────────────┘
```

### 13.2 Backward Compatibility

If the barrel image already has AI tools baked in (old build), they still
work. The `/usr/local` mount only happens when `~/.cooper/ai-tools/` exists.
So existing users can continue using their current setup until they rebuild.

**In `appendVolumeMounts()`:**
```go
// Only mount AI tools volume if it exists and has content.
aiToolsDir := filepath.Join(cooperDir, "ai-tools")
if dirExists(aiToolsDir) {
    manifestPath := filepath.Join(aiToolsDir, "manifest.json")
    if fileExists(manifestPath) {
        args = append(args, "-v", fmt.Sprintf("%s:/usr/local:rw", aiToolsDir))
    }
}
```

### 13.3 cooper build After Migration

When a user runs `cooper build` with the new code, the generated Dockerfile
no longer includes AI tool installation. This is fine — the image builds
faster. The user then needs to run `cooper install-ai` (or use the TUI) to
populate the AI tools volume.

---

## Implementation Order

The work packages should be implemented in this order:

### Phase 1: Foundation (WP1 + WP3 + WP4)
1. **WP3**: Modify Dockerfile template (relocate Node.js, remove AI tools)
2. **WP4**: Update entrypoint template (CA cert handling, PATH)
3. **WP1**: Volume mount design (barrel.go changes)
4. Verify `cooper build` still produces a working barrel image (sans AI tools)

### Phase 2: Installer (WP2 + WP12)
5. **WP2**: Create installer container logic + install script template
6. **WP12**: Add `cooper install-ai` CLI command
7. Verify full cycle: build → install-ai → barrel starts with AI tools

### Phase 3: Build/Update Changes (WP5 + WP6 + WP7)
8. **WP5**: Config changes (if any)
9. **WP6**: Update build command
10. **WP7**: Update update command

### Phase 4: TUI Integration (WP8 + WP9)
11. **WP8**: AI Tools tab in TUI
12. **WP9**: Configure wizard save screen updates

### Phase 5: Polish (WP10 + WP11 + WP13)
13. **WP10**: Proof/diagnostics updates
14. **WP13**: Migration path
15. **WP11**: Test updates (docker-build, e2e, unit tests)

---

## Risk Mitigation

### Risk: Claude/OpenCode installer scripts change behavior
**Mitigation:** The install script template uses `HOME` redirection and
explicit file copying. If an installer changes its output path, only the
template needs updating. The manifest.json verification step catches install
failures.

### Risk: npm --prefix doesn't capture all files
**Mitigation:** Test with each npm package. npm `--prefix` is well-supported
for global installs. If a package installs post-install hooks that write
outside the prefix, we'll discover this during testing.

### Risk: /usr/local mount hides system files
**Mitigation:** Node.js and bubblewrap are relocated to /usr/. CA cert
handling is updated. The only thing at /usr/local/ before the mount was
Node.js and bubblewrap, both of which are relocated.

### Risk: File permission issues on mounted volume
**Mitigation:** The installer runs as the same UID/GID as the barrel user
(using the barrel-base image which has the correct user). The volume is
mounted :rw. Host directories are pre-created with correct ownership
(ensureBarrelHostDirs).

### Risk: Installer container needs internet but networks may not exist
**Mitigation:** `cooper install-ai` ensures networks exist before running.
The TUI flow runs within `cooper up` where networks are already created.

---

## Files Modified (Summary)

### New Files
- `internal/docker/installer.go` — AI tool installation logic
- `internal/templates/install-ai.sh.tmpl` — Installer script template
- `internal/tui/aitools/model.go` — AI Tools TUI tab

### Modified Files
- `internal/templates/cli.Dockerfile.tmpl` — Remove AI tools, relocate Node.js
- `internal/templates/entrypoint.sh.tmpl` — PATH, CA cert handling
- `internal/templates/templates.go` — New render function, updated data structs
- `internal/docker/barrel.go` — AI tools volume mount
- `internal/docker/build.go` — (minor: image name for installer)
- `internal/app/cooper.go` — AI tool management methods
- `internal/app/app.go` — Interface additions
- `internal/proof/proof.go` — AI tools volume check
- `internal/configure/save.go` — Updated messaging
- `internal/tui/model.go` — New tab
- `internal/tui/app.go` — AI tools installation messages
- `internal/tui/theme/tabs.go` — New tab constant
- `main.go` — install-ai command, split version resolution, build changes
- `test-docker-build.sh` — AI tools install phase
- `test-e2e.sh` — AI tools install phase, volume mount
