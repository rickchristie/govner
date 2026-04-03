# Plan: Playwright Support for Cooper

## Status

- Date: 2026-04-04
- Scope: Linux-only Playwright runtime support inside Cooper barrels
- Applies to:
  - all officially supported AI CLI barrels
    - `claude`
    - `codex`
    - `copilot`
    - `opencode`
  - custom `cooper-cli-*` barrels built on top of `cooper-base`
- This document is a design and implementation handoff for another AI session.
- The goal is not "add a Playwright programming tool". The goal is to make
  Cooper barrels Playwright-ready by default.

## Executive Summary

Cooper should not manage Playwright itself.

Cooper should provide the Linux runtime environment that Playwright needs:

1. Chromium/Chrome OS dependencies in `cooper-base`, always installed
2. `Xvfb`, always installed
3. `fontconfig` plus a baseline font set, always installed
4. a Cooper-managed host font directory mounted into every barrel
5. a Cooper-managed Playwright browser cache mounted into every barrel
6. a default X11 display started for every barrel
7. a configurable per-barrel shared-memory size, defaulting to `1g`

What Cooper should not do:

1. Cooper should not install the Playwright npm package
2. Cooper should not install a system Chromium binary just for Playwright
3. Cooper should not manage repo Playwright versioning
4. Cooper should not auto-whitelist Playwright browser download hosts

The user or project remains responsible for:

1. installing Playwright in the repo or otherwise making it available
2. deciding whether to run headed, default headless, or headless with
   `channel: 'chromium'`
3. manually approving browser downloads when `playwright install` needs network

This design gives the user the flexibility they want:

- repo-local Playwright versioning remains the source of truth
- Cooper images do not need rebuilding just because a repo upgrades Playwright
- headed Playwright works because `Xvfb` and fonts are available
- headless and `channel: 'chromium'` also work because Playwright still uses
  its own managed browser binaries under the mounted cache

## Problem

Today, Cooper barrels are not generically Playwright-ready.

The current system has several gaps:

1. the base image does not provide a font runtime suitable for screenshot
   fidelity
2. there is no mounted persistent Playwright browser cache
3. there is no generic always-on X display for all barrels
4. current `Xvfb` behavior is partial and tool-specific
5. there is no configurable `--shm-size` for browser-heavy barrels

This leads to a bad user experience:

1. repo Playwright may be installed but browser launch still fails because the
   browser cache is not visible in the barrel
2. headed browser automation is not consistently available
3. screenshot rendering drifts because the container font set differs too much
   from the host
4. support is partial and accidental instead of being a deliberate Cooper
   capability

## Final Direction

Playwright support is a default barrel capability, not a configurable
programming tool.

This is the final design direction for v1:

1. always install Playwright Chromium OS dependencies in `cooper-base`
2. always install `Xvfb`, `fontconfig`, and a baseline font set in
   `cooper-base`
3. always mount `~/.cooper/fonts` into `/home/user/.local/share/fonts`
4. always expose `/home/user/.fonts` as a symlink to
   `/home/user/.local/share/fonts`
5. always mount `~/.cooper/cache/ms-playwright` into
   `/home/user/.cache/ms-playwright`
6. always set `PLAYWRIGHT_BROWSERS_PATH=/home/user/.cache/ms-playwright`
7. always start `Xvfb` for every barrel and export `DISPLAY`
8. add a new global config field for barrel shared memory, default `1g`
9. do not install Playwright itself
10. do not install a system Chromium binary
11. do not automatically whitelist Playwright download hosts

## Why This Is The Right Abstraction

The user originally explored several models:

1. "Headed Playwright" as a Cooper programming tool
2. `Chromium + Xvfb` as a Cooper programming tool
3. a much broader mounted-home and runtime-installed toolchain model

Those alternatives were rejected for these reasons:

1. Playwright package versions are repo-local and change frequently
2. tying Playwright version changes to Cooper image rebuilds is the wrong
   abstraction
3. Playwright already manages its own browser binaries
4. a broad runtime-installed toolchain model introduces much larger lifecycle
   and mutability problems than necessary for this feature

The stable abstraction is:

- Cooper provides the runtime environment
- the repo provides the Playwright package version
- Playwright itself provides the browser binary version

This keeps the layers clean.

## Verified Current State

These findings were verified against the current Cooper code and running images.

### Programming runtime install locations

Current `cooper-base` locations:

- Go: `/usr/local/go/bin/go`
- Node: `/usr/local/bin/node`
- npm: `/usr/local/bin/npm`
- npx: `/usr/local/bin/npx`
- Python: `/usr/bin/python` and `/usr/bin/python3`
- pip: `/usr/bin/pip` and `/usr/bin/pip3`

Relevant current file:

- `internal/templates/base.Dockerfile.tmpl`

Important implication:

- Node is already present even when the user does not enable Node as a
  programming tool, because npm-based AI CLIs need it
- that is good for repo-local JavaScript Playwright usage
- users who need a specific Node version still use the existing Node
  programming-tool flow

### AI CLI install locations

Current built-in AI CLIs are home-installed:

- Claude: `/home/user/.local/bin/claude`
- Codex: `/home/user/.npm-global/bin/codex`
- Copilot: `/home/user/.npm-global/bin/copilot`
- OpenCode: `/home/user/.opencode/bin/opencode`

Relevant current files:

- `internal/templates/base.Dockerfile.tmpl`
- `internal/templates/cli-tool.Dockerfile.tmpl`
- `internal/templates/templates.go`

Important implication:

- we should not mount all of `/home/user`
- doing so would shadow current AI CLI installs
- this plan intentionally avoids that larger architectural change

### Existing X11/Xvfb behavior

Current Cooper already has X11-related logic:

- OpenCode currently gets an OpenCode-specific `Xvfb` setup
- Codex and Copilot currently use X11 clipboard mode via the clipboard-bridge
  path
- Claude and OpenCode currently use shim clipboard mode

Relevant current files:

- `internal/templates/entrypoint.sh.tmpl`
- `internal/docker/barrel.go`

Important implication:

- Cooper already has proof that `Xvfb` can live inside barrels
- this plan should unify that logic instead of adding a second X11 path

### Current base image gaps

Verified current gaps in `cooper-base`:

1. no mounted Playwright browser cache
2. no generic always-on `DISPLAY`
3. no generic barrel `--shm-size`
4. no generic mounted host fonts
5. current base image does not expose a real font runtime suitable for this
   feature

Relevant current files:

- `internal/docker/barrel.go`
- `internal/templates/base.Dockerfile.tmpl`
- `internal/templates/entrypoint.sh.tmpl`

### Smoke verification for AI CLIs with X11 present

The following smoke tests were run:

1. `claude --help`
2. `codex --help`
3. `copilot --help`
4. `opencode --help`

Additional checks:

1. Claude was tested both with no `DISPLAY` and with forced `DISPLAY` plus
   `Xvfb`
2. Codex and Copilot were tested with the current X11 clipboard display setup
3. OpenCode was tested with its current `Xvfb`/`DISPLAY` setup

Observed result:

- all four CLIs started successfully for help/smoke usage
- no evidence was found that a default `Xvfb` plus exported `DISPLAY` breaks
  the current CLIs

This does not prove every authenticated interactive flow forever, but it is
strong enough to proceed with this design.

### Research grounding

The local research in `cooper/test-playwright/RESEARCH.md` is the main
behavioral foundation for this plan.

The most important conclusions from that research:

1. headed via `Xvfb` gives the closest visual fidelity to a real headed host
   browser
2. default headless uses a different browser path than headed
3. `channel: 'chromium'` uses the full browser path and is closer to headed
4. font handling is a major contributor to screenshot differences

This plan deliberately enables all three Playwright runtime styles:

1. headed via `Xvfb`
2. default headless
3. headless with `channel: 'chromium'`

Cooper should not force one mode. It should supply the environment so the user
or repo can choose.

## Requirements

### Functional requirements

1. Every Cooper AI barrel must be Playwright-ready on Linux by default.
2. The feature must automatically apply to custom `cooper-cli-*` barrels that
   inherit from `cooper-base`.
3. The user must be able to use repo-local Playwright of any supported version
   without rebuilding Cooper images for every repo Playwright bump.
4. Headed Playwright must work inside the barrel.
5. Default headless Playwright must work inside the barrel.
6. Headless Playwright with `channel: 'chromium'` must work inside the barrel.
7. A persistent Playwright browser cache must be available inside barrels.
8. A best-effort Cooper-managed host font mirror must be available inside
   barrels.
9. The user must be able to add extra fonts manually by copying files into the
   Cooper-managed font directory on the host.
10. The display environment must not break Claude, Codex, Copilot, or OpenCode.
11. Shared memory size must have a safe default and be configurable from
   `cooper configure`.

### Non-functional requirements

1. The design must stay simpler than the broader mounted-home and runtime
   toolchain-install architecture.
2. The feature must not add a separate Playwright version-management system to
   Cooper.
3. The feature must not require default network whitelist changes for browser
   downloads.
4. The feature must integrate cleanly with the existing clipboard-bridge X11
   architecture rather than competing with it.
5. Font sync should be best-effort and non-fatal.
6. The design should not require full-home mounts.

## Non-Goals

These items are explicitly out of scope for v1:

1. Playwright as a Cooper programming tool
2. system Chromium as a Cooper programming tool
3. Cooper-managed Playwright package installation
4. Cooper-managed Playwright version pinning
5. default whitelisting of Playwright browser download domains
6. VNC/noVNC visual browser viewing
7. perfect host-to-container font parity
8. automatic cross-platform font sync outside Linux
9. full runtime-installed toolchain architecture

## Key Decisions And Reasoning

### Decision 1: This is not a programming tool

Decision:

- do not add `playwright` to the programming tools UI
- do not add `chromium` to the programming tools UI

Reasoning:

1. Playwright package version is repo-local
2. Playwright browser versions are managed by Playwright itself
3. Cooper should supply runtime capability, not own repo dependency versions
4. making this a programming tool would incorrectly tie repo dependency churn
   to Cooper image churn

### Decision 2: Do not install a system Chromium binary

Decision:

- Cooper installs Chromium OS dependencies, not a system Chromium executable

Reasoning:

1. Playwright uses its own managed browser binaries
2. `headless: false` uses the Playwright-managed full browser
3. `channel: 'chromium'` also uses the Playwright-managed full browser
4. installing a system Chromium binary would add size and confusion without
   solving the real compatibility problem
5. the user explicitly wants the freedom to choose any Playwright version in
   the repo

### Decision 3: Use one Cooper-managed font directory

Decision:

- host font directory: `~/.cooper/fonts`
- mount that directory into `/home/user/.local/share/fonts`
- expose `/home/user/.fonts` as a symlink to the mounted directory

Reasoning:

1. one real mount point is simpler than mounting both legacy font locations
2. some software still checks `~/.fonts`
3. the symlink keeps compatibility without duplicated mounts
4. using a Cooper-managed directory is future-proof for non-Linux hosts later

### Decision 4: Mount Playwright browser cache

Decision:

- host cache directory: `~/.cooper/cache/ms-playwright`
- mount into `/home/user/.cache/ms-playwright`
- set `PLAYWRIGHT_BROWSERS_PATH=/home/user/.cache/ms-playwright`

Reasoning:

1. repo-local Playwright package needs a stable place for browser binaries
2. a persistent mounted cache avoids redownloading browsers into disposable
   containers
3. this is the missing piece that makes repo-local Playwright usable in a
   Cooper barrel

### Decision 5: Start Xvfb for every barrel

Decision:

- every barrel starts one authenticated `Xvfb` instance and exports `DISPLAY`

Reasoning:

1. headed Playwright becomes available everywhere by default
2. the same X11 display can be reused by clipboard-bridge native modes
3. smoke testing showed no evidence that this breaks the supported AI CLIs
4. one unified X11 startup path is cleaner than current tool-specific behavior

### Decision 6: Default barrel shared memory is 1g

Decision:

- add a new Cooper config field for barrel shared memory
- default value is `1g`

Reasoning:

1. Docker's default `/dev/shm` size of `64m` is too small for reliable
   Chromium use
2. `256m` is better but still tighter than we want for headed browser
   debugging
3. `1g` is a strong default without jumping all the way to `2g`
4. `--ipc=host` is intentionally not used because it is less contained than
   `--shm-size`

### Decision 7: No default whitelist changes

Decision:

- do not add Playwright browser download domains to the default whitelist

Reasoning:

1. the user explicitly wants browser installs to go through manual approval
2. this is consistent with Cooper's existing security posture
3. the runtime support should not silently broaden egress

## Proposed Architecture

### High-level model

Cooper provides a browser-ready Linux runtime layer.

The repo and user provide:

1. the Playwright package
2. the Playwright code
3. the decision to run headed/headless/channel
4. the explicit approval when browser downloads need internet

### Directory and mount layout

Host paths:

- `~/.cooper/fonts`
- `~/.cooper/cache/ms-playwright`

Container paths:

- `/home/user/.local/share/fonts`
- `/home/user/.fonts` -> symlink to `/home/user/.local/share/fonts`
- `/home/user/.cache/ms-playwright`

Docker mount policy:

- fonts mount is read-only
- Playwright cache mount is read-write

Concrete target docker args:

```text
-v ~/.cooper/fonts:/home/user/.local/share/fonts:ro
-v ~/.cooper/cache/ms-playwright:/home/user/.cache/ms-playwright:rw
-e PLAYWRIGHT_BROWSERS_PATH=/home/user/.cache/ms-playwright
-e DISPLAY=127.0.0.1:99
-e XAUTHORITY=/home/user/.cooper-clipboard.xauth
-e COOPER_CLIPBOARD_DISPLAY=127.0.0.1:99
-e COOPER_CLIPBOARD_XAUTHORITY=/home/user/.cooper-clipboard.xauth
--shm-size=1g
```

### Environment contract inside barrels

Barrels should expose this environment:

```bash
export DISPLAY=127.0.0.1:99
export XAUTHORITY=/home/user/.cooper-clipboard.xauth
export COOPER_CLIPBOARD_DISPLAY=127.0.0.1:99
export COOPER_CLIPBOARD_XAUTHORITY=/home/user/.cooper-clipboard.xauth
export PLAYWRIGHT_BROWSERS_PATH=/home/user/.cache/ms-playwright
```

This environment should be visible to:

1. the entrypoint
2. interactive `cooper cli` shells
3. `docker exec` sessions
4. AI CLI subprocesses
5. repo-local Playwright commands

Implementation note:

- do not let generic Playwright/X11 env drift from the clipboard-bridge env
- `DISPLAY` must match `COOPER_CLIPBOARD_DISPLAY`
- `XAUTHORITY` must match `COOPER_CLIPBOARD_XAUTHORITY`

### X11 runtime contract

The X11 runtime should be shared between Playwright support and clipboard
support.

Implementation rules:

1. start exactly one `Xvfb` per barrel
2. use one barrel-local authenticated display
3. keep the clipboard-bridge native X11 path on the same display
4. do not create a separate Playwright-only display
5. do not keep the old OpenCode-only Xvfb branch after the unified path exists
6. wait for Xvfb TCP readiness before starting any native X11 clipboard bridge
7. export one consistent display/auth pair for both generic X11 use and
   clipboard-bridge use

Recommended startup shape:

```bash
XAUTH_FILE=/home/user/.cooper-clipboard.xauth
DISPLAY_NUM=99
DISPLAY_ADDR=127.0.0.1:${DISPLAY_NUM}

COOKIE="$(mcookie)"
xauth -f "$XAUTH_FILE" add ":${DISPLAY_NUM}" . "$COOKIE"
chmod 0600 "$XAUTH_FILE"

Xvfb ":${DISPLAY_NUM}" \
  -screen 0 1920x1080x24 \
  -auth "$XAUTH_FILE" \
  -listen tcp \
  -nolisten unix >/tmp/xvfb.log 2>&1 &

export DISPLAY="$DISPLAY_ADDR"
export XAUTHORITY="$XAUTH_FILE"
```

Notes:

1. keep authenticated X11 behavior because clipboard-bridge already depends on
   it
2. `1920x1080x24` is preferred over `1024x768x24` for general browser work
3. implementation may keep current file naming for the xauth file even though
   the name still says "clipboard"

### Font sync model

Cooper should maintain a best-effort host-side font mirror.

Source roots on Linux:

1. `~/.local/share/fonts`
2. `~/.fonts`
3. `/usr/local/share/fonts`
4. `/usr/share/fonts`

Destination root:

- `~/.cooper/fonts`

Rules:

1. sync is best-effort, not fatal
2. sync runs on `cooper up`
3. sync copies changed and new files
4. sync does not delete user-added files from `~/.cooper/fonts`
5. sync should preserve enough path structure to avoid filename collisions
6. sync should only consider font-like files
7. after the mount is present inside the barrel, run `fc-cache -f`
8. the mounted font directory stays read-only inside barrels
9. fontconfig cache files should live under `/home/user/.cache/fontconfig`,
   not in the mounted font directory

Implementation direction:

- implement sync in Go, not shell, so it is testable and portable enough to
  extend later
- create a small dedicated package, for example `internal/fontsync`

Recommended file types:

- `.ttf`
- `.otf`
- `.ttc`
- `.otc`

Optional later:

- `.woff`
- `.woff2`

### Playwright browser cache model

Playwright browser binaries should live in the Cooper-managed cache mount.

Rules:

1. Cooper does not preinstall browsers
2. Cooper only provides the mounted cache location
3. `PLAYWRIGHT_BROWSERS_PATH` must point at the mounted path
4. the first browser install may require user approval through the proxy
5. once installed, the cache survives container restarts because it lives under
   `~/.cooper`
6. Cooper must create the host cache directory before any `docker run` so
   Docker does not create it with the wrong ownership

Important result:

- this makes repo-local Playwright practical without making Cooper responsible
  for Playwright versioning

### Shared memory model

Add a new config value:

```json
{
  "barrel_shm_size": "1g"
}
```

Rules:

1. default is `1g`
2. configurable via `cooper configure`
3. passed directly to `docker run --shm-size=...`
4. validate at config time using a restricted Docker-compatible size format

Recommended accepted formats:

- `64m`
- `256m`
- `512m`
- `1g`
- `2g`

Recommended validation rule for v1:

- accept positive integers optionally followed by one unit character from
  `k`, `m`, or `g`, case-insensitive
- keep it stricter than the full Docker parser to reduce ambiguity

## Important Clarifications

### This design does not install Playwright

The user still installs Playwright in the project.

Typical user flow:

1. install or update Playwright in the repo on the host
2. approve network access manually when Playwright needs to download browsers
3. run Playwright inside a Cooper barrel

### This design does not install system Chromium

That is intentional.

If the repo uses Playwright, Playwright's own browser binaries remain the
source of truth.

### Supported versus unsupported browser selection

This plan is meant to support the Playwright-managed Chromium paths:

1. headed Chromium through Playwright
2. default headless Chromium through Playwright
3. headless `channel: 'chromium'` through Playwright

This plan does not promise Cooper-provided support for:

1. `channel: 'chrome'`
2. `channel: 'msedge'`
3. arbitrary system-browser `executablePath` flows

Reason:

- those paths depend on system browser installations that this design
  intentionally does not provide

### This design does not promise zero-approval first run

Because there is no default whitelist for Playwright browser downloads, the
first `playwright install` will still need manual approval.

That is expected and desired.

### This design is language-agnostic at the runtime layer

The runtime support helps any Playwright client that can use the mounted
browser cache and X11 runtime.

Practical implications:

1. JavaScript Playwright works well because Node is already present in the base
2. Python Playwright can also benefit, but Python itself still depends on the
   user's Cooper programming-tool configuration or other repo/runtime choices

### What `cooper cleanup` means for this feature

Because the mounted font mirror and browser cache live under `~/.cooper`,
removing the Cooper directory also removes:

1. the mirrored host-font store
2. the cached Playwright browser binaries

That is acceptable and consistent with Cooper's current cleanup model.

## Detailed Requirements For The Implementation Session

These requirements are non-negotiable for the implementation.

### Base image requirements

`cooper-base` must always include:

1. `Xvfb`
2. `xauth`
3. `fontconfig`
4. a baseline font set
5. the Linux shared-library/runtime packages required by Playwright Chromium

Implementation source of truth:

- use the official Playwright Debian/Ubuntu Chromium dependency set as the
  baseline, not a hand-curated "probably enough" subset

Additional font direction from the local research:

1. include `fonts-dejavu-core`
2. include `fonts-roboto`
3. include `fonts-noto-core`
4. include `fonts-noto-cjk`
5. include `fonts-freefont-ttf`
6. include `fonts-liberation`
7. include `fonts-noto-color-emoji`

### Entry point requirements

The barrel entrypoint must:

1. ensure the font mount target exists
2. ensure `~/.fonts` points at `~/.local/share/fonts`
3. ensure `~/.cache/ms-playwright` exists
4. start one authenticated `Xvfb`
5. export `DISPLAY` and `XAUTHORITY`
6. persist `DISPLAY` and `XAUTHORITY` for later exec sessions
7. run `fc-cache -f`
8. start the clipboard X11 bridge only when clipboard mode requires it
9. remove the current duplicate or conflicting Xvfb startup paths

### Barrel runtime requirements

`docker run` for every barrel must:

1. mount the Cooper-managed font dir
2. mount the Playwright browser cache dir
3. set `PLAYWRIGHT_BROWSERS_PATH`
4. set `DISPLAY`
5. set `XAUTHORITY`
6. pass `--shm-size=<configured-value>`

### `cooper up` requirements

`cooper up` must:

1. ensure `~/.cooper/fonts` exists
2. ensure `~/.cooper/cache/ms-playwright` exists
3. perform best-effort font sync before the system is declared ready
4. not fail startup if font sync is partial or unavailable
5. surface startup warnings if font sync fails in a meaningful way
6. create the support directories before any barrel start so Docker does not
   create them as root-owned directories

### `cooper configure` requirements

`cooper configure` must:

1. not add a new Playwright tool entry
2. add a configurable `barrel_shm_size`
3. default that value to `1g`
4. validate and save the new field
5. explain briefly that Playwright support is built-in and repo-managed

### `cooper proof` and `doctor.sh` requirements

The diagnostics should validate the runtime environment, not Playwright package
installation.

They should check:

1. `Xvfb` is installed
2. `DISPLAY` is set
3. `XAUTHORITY` is set and file exists
4. `COOPER_CLIPBOARD_DISPLAY` is set
5. `COOPER_CLIPBOARD_XAUTHORITY` is set and file exists
6. `/home/user/.local/share/fonts` exists
7. `/home/user/.fonts` is a symlink to the mounted font dir
8. `fc-cache` and `fc-list` are available
9. `PLAYWRIGHT_BROWSERS_PATH` is set
10. the browser cache directory exists
11. `/dev/shm` size matches the configured value
12. `fc-cache -f` succeeds even though the mounted font directory is read-only

They should not fail just because:

1. Playwright npm package is not present in the workspace
2. no browser has been downloaded yet

Optional diagnostic behavior:

- if a workspace Playwright package and a browser cache are both present, run a
  best-effort browser smoke test and report the result as extra information

## Implementation Order

The next implementation session should follow this order to reduce drift and
avoid rework:

1. add `barrel_shm_size` to config, defaults, validation, and configure UI
2. add Cooper-managed support-dir creation and host-side font sync in `cooper up`
3. add barrel mounts, env vars, and `--shm-size` wiring
4. update `cooper-base` packages and unify entrypoint Xvfb startup
5. update diagnostics so `doctor` and `proof` reflect the new runtime contract
6. update e2e and template tests so the old OpenCode-only Xvfb expectations are
   replaced with unified all-barrel expectations
7. update docs last, after behavior and tests are stable

Do not invert this sequence.

In particular:

1. do not start by editing docs only
2. do not add new Playwright behavior without also updating diagnostics and e2e
3. do not keep old OpenCode-only Xvfb branches around while adding the new
   universal path
4. do not let Docker auto-create the support directories as root-owned mounts

## File-Level Implementation Plan

This section is intentionally concrete so another AI session can implement it
faithfully.

### 1. Configuration model

Files to update:

- `internal/config/config.go`
- `internal/config/config_test.go`
- `internal/app/configure.go`
- `internal/configure/configure.go`
- `internal/configure/layout.go`
- `internal/configure/proxy.go`
- `internal/configure/save.go`
- `internal/configure/configure_test.go`

Required changes:

1. add `BarrelSHMSize string` to `config.Config`
2. set default to `1g`
3. validate the field
4. surface it in the configure UI
5. persist it in `config.json`

Example config change:

```go
type Config struct {
    ProgrammingTools   []ToolConfig      `json:"programming_tools"`
    AITools            []ToolConfig      `json:"ai_tools"`
    WhitelistedDomains []DomainEntry     `json:"whitelisted_domains"`
    PortForwardRules   []PortForwardRule `json:"port_forward_rules"`
    ProxyPort          int               `json:"proxy_port"`
    BridgePort         int               `json:"bridge_port"`
    MonitorTimeoutSecs int               `json:"monitor_timeout_secs"`
    BlockedHistoryLimit int              `json:"blocked_history_limit"`
    AllowedHistoryLimit int              `json:"allowed_history_limit"`
    BridgeLogLimit     int               `json:"bridge_log_limit"`
    BridgeRoutes       []BridgeRoute     `json:"bridge_routes"`
    ClipboardTTLSecs   int               `json:"clipboard_ttl_secs"`
    ClipboardMaxBytes  int               `json:"clipboard_max_bytes"`
    BarrelSHMSize      string            `json:"barrel_shm_size"`
}
```

Example defaulting and validation:

```go
var shmSizeRE = regexp.MustCompile(`(?i)^[1-9][0-9]*[kmg]?$`)

func (c *Config) applyMissingDefaults() {
    if c.ClipboardTTLSecs <= 0 {
        c.ClipboardTTLSecs = 300
    }
    if c.ClipboardMaxBytes <= 0 {
        c.ClipboardMaxBytes = 20971520
    }
    if strings.TrimSpace(c.BarrelSHMSize) == "" {
        c.BarrelSHMSize = "1g"
    }
}

func (c *Config) Validate() error {
    // existing validation...
    if !shmSizeRE.MatchString(c.BarrelSHMSize) {
        return fmt.Errorf("barrel shm size %q is invalid", c.BarrelSHMSize)
    }
    return nil
}
```

### 2. Host-side font sync package

New package direction:

- `internal/fontsync/`

Likely files:

- `internal/fontsync/sync.go`
- `internal/fontsync/sync_test.go`

Required behavior:

1. enumerate Linux font source roots
2. create `~/.cooper/fonts` if missing
3. copy changed font files into the Cooper-managed destination
4. preserve user-added files
5. return warnings instead of hard failures where possible

Example package shape:

```go
package fontsync

type Result struct {
    Copied   int
    Skipped  int
    Warnings []string
}

func SyncLinuxFonts(homeDir, cooperDir string) (Result, error) {
    dstRoot := filepath.Join(cooperDir, "fonts")
    if err := os.MkdirAll(dstRoot, 0o755); err != nil {
        return Result{}, err
    }

    sources := []string{
        filepath.Join(homeDir, ".local", "share", "fonts"),
        filepath.Join(homeDir, ".fonts"),
        "/usr/local/share/fonts",
        "/usr/share/fonts",
    }

    // Walk readable source trees. Copy only font files. Preserve relative
    // structure under a source-specific prefix to avoid filename collisions.
    // Never delete files from dstRoot.
    // Return warnings for unreadable trees rather than failing startup.
}
```

Recommended destination structure:

```text
~/.cooper/fonts/
  user-local-share-fonts/
  user-dot-fonts/
  usr-local-share-fonts/
  usr-share-fonts/
```

Recommended copy rule:

- copy only when destination missing or source size/modtime differs
- do not attempt content hashing in v1 unless needed for performance reasons
- keep sync conservative and simple

### 3. `cooper up` startup integration

Files to update:

- `main.go`
- `internal/tui/loading/model.go`
- possibly `internal/tui/about/model.go` if startup warnings should mention font
  sync state

Required changes:

1. add a startup step for ensuring or syncing fonts
2. ensure `~/.cooper/cache/ms-playwright` exists
3. collect non-fatal startup warnings for font sync issues
4. ensure the support directories exist before any barrel start so Docker does
   not create them as root-owned directories

Example startup sequence change:

```text
1. Create networks
2. Start proxy
3. Verify CA
4. Start execution bridge
5. Ensure Playwright support dirs
6. Sync fonts (best effort)
7. Version check / startup warnings
8. Start ACL listener
9. Ready
```

Recommended helper:

```go
func ensurePlaywrightSupportDirs(cooperDir string) error {
    dirs := []string{
        filepath.Join(cooperDir, "fonts"),
        filepath.Join(cooperDir, "cache", "ms-playwright"),
    }
    for _, dir := range dirs {
        if err := os.MkdirAll(dir, 0o755); err != nil {
            return err
        }
    }
    return nil
}
```

### 4. Barrel volume mounts and env

Files to update:

- `internal/docker/barrel.go`
- `internal/app/cooper_test.go`

Required changes:

1. mount `~/.cooper/fonts` to `/home/user/.local/share/fonts:ro`
2. mount `~/.cooper/cache/ms-playwright` to
   `/home/user/.cache/ms-playwright:rw`
3. set `PLAYWRIGHT_BROWSERS_PATH`
4. set `DISPLAY` and `XAUTHORITY` for every barrel, not only current X11 tools
5. preserve `COOPER_CLIPBOARD_DISPLAY` and `COOPER_CLIPBOARD_XAUTHORITY` on
   the same values
6. add `--shm-size` from config
7. ensure host directories are created before Docker mounts them

Example `docker run` changes:

```go
args := []string{
    "run", "-d",
    "--name", name,
    "--network", NetworkInternal,
    "--cap-drop=ALL",
    "--security-opt=no-new-privileges",
    "--security-opt", fmt.Sprintf("seccomp=%s", seccompPath),
    "--init",
    "--shm-size", cfg.BarrelSHMSize,
}
```

Example mount/env helper:

```go
func appendPlaywrightSupport(args []string, cooperDir string) []string {
    fontsDir := filepath.Join(cooperDir, "fonts")
    pwCacheDir := filepath.Join(cooperDir, "cache", "ms-playwright")

    args = append(args,
        "-v", fontsDir+":/home/user/.local/share/fonts:ro",
        "-v", pwCacheDir+":/home/user/.cache/ms-playwright:rw",
        "-e", "PLAYWRIGHT_BROWSERS_PATH=/home/user/.cache/ms-playwright",
        "-e", "DISPLAY=127.0.0.1:99",
        "-e", "XAUTHORITY=/home/user/.cooper-clipboard.xauth",
        "-e", "COOPER_CLIPBOARD_DISPLAY=127.0.0.1:99",
        "-e", "COOPER_CLIPBOARD_XAUTHORITY=/home/user/.cooper-clipboard.xauth",
    )
    return args
}
```

Recommended host-dir creation additions in `ensureBarrelHostDirs`:

```go
dirs = append(dirs,
    filepath.Join(cooperDir, "fonts"),
    filepath.Join(cooperDir, "cache", "ms-playwright"),
)
```

### 5. Base image and entrypoint

Files to update:

- `internal/templates/base.Dockerfile.tmpl`
- `internal/templates/entrypoint.sh.tmpl`
- `internal/templates/templates_test.go`

Required changes:

1. add the always-on browser dependency packages
2. add `fontconfig` and baseline fonts
3. remove the old OpenCode-only Xvfb special case
4. add the unified all-barrels Xvfb startup
5. create or fix `.fonts` symlink
6. ensure `/home/user/.cache/fontconfig` exists before running `fc-cache`
7. ensure `fc-cache -f` runs
8. make the clipboard X11 bridge reuse the same X11 display

Example Dockerfile package direction:

```dockerfile
RUN apt-get update && apt-get install -y --no-install-recommends \
    socat \
    git \
    curl \
    ca-certificates \
    xz-utils \
    ripgrep \
    jq \
    xclip \
    xsel \
    xauth \
    xvfb \
    fontconfig \
    fonts-dejavu-core \
    fonts-roboto \
    fonts-noto-core \
    fonts-noto-cjk \
    fonts-freefont-ttf \
    fonts-liberation \
    fonts-noto-color-emoji \
    libasound2t64 \
    libatk-bridge2.0-0t64 \
    libatk1.0-0t64 \
    libatspi2.0-0t64 \
    libcairo2 \
    libcups2t64 \
    libdbus-1-3 \
    libdrm2 \
    libgbm1 \
    libglib2.0-0t64 \
    libnspr4 \
    libnss3 \
    libpango-1.0-0 \
    libx11-6 \
    libxcb1 \
    libxcomposite1 \
    libxdamage1 \
    libxext6 \
    libxfixes3 \
    libxkbcommon0 \
    libxrandr2 \
    libfontconfig1 \
    libfreetype6 \
    && rm -rf /var/lib/apt/lists/*
```

Important note:

- the exact dependency list should be taken from Playwright's own Linux
  dependency source at implementation time
- the snippet above is an example baseline, not permission to silently drift

Example entrypoint shape:

```bash
ensure_playwright_runtime() {
  mkdir -p /home/user/.local/share/fonts
  mkdir -p /home/user/.cache/ms-playwright
  mkdir -p /home/user/.cache/fontconfig
  ln -snf /home/user/.local/share/fonts /home/user/.fonts
  fc-cache -f >/tmp/fc-cache.log 2>&1 || true
}

start_shared_xvfb() {
  XAUTH_FILE="/home/user/.cooper-clipboard.xauth"
  DISPLAY_NUM=99
  DISPLAY_ADDR="127.0.0.1:${DISPLAY_NUM}"

  COOKIE="$(mcookie)"
  xauth -f "$XAUTH_FILE" add ":${DISPLAY_NUM}" . "$COOKIE" 2>/dev/null || true
  chmod 0600 "$XAUTH_FILE" 2>/dev/null || true

  Xvfb ":${DISPLAY_NUM}" \
    -screen 0 1920x1080x24 \
    -auth "$XAUTH_FILE" \
    -listen tcp \
    -nolisten unix >/tmp/xvfb.log 2>&1 &

  export DISPLAY="$DISPLAY_ADDR"
  export XAUTHORITY="$XAUTH_FILE"
  export COOPER_CLIPBOARD_DISPLAY="$DISPLAY_ADDR"
  export COOPER_CLIPBOARD_XAUTHORITY="$XAUTH_FILE"
  sed -i "1i export DISPLAY=${DISPLAY_ADDR}\nexport XAUTHORITY=${XAUTH_FILE}" /home/user/.bashrc 2>/dev/null || true
  sed -i "1i export DISPLAY=${DISPLAY_ADDR}\nexport XAUTHORITY=${XAUTH_FILE}" /home/user/.profile 2>/dev/null || true
}
```

Ordering rule:

1. run `ensure_playwright_runtime`
2. run `start_shared_xvfb`
3. start clipboard X11 bridge only after Xvfb is ready
4. do not keep the old OpenCode-only branch afterward

Implementation note:

- keep one helper that writes all four X11-related exports:
  - `DISPLAY`
  - `XAUTHORITY`
  - `COOPER_CLIPBOARD_DISPLAY`
  - `COOPER_CLIPBOARD_XAUTHORITY`
- do not maintain two partially divergent code paths for generic X11 and
  clipboard X11

### 6. Diagnostics

Files to update:

- `internal/templates/doctor.sh`
- `internal/proof/proof.go`

Required changes:

1. add checks for `DISPLAY`, `XAUTHORITY`, `PLAYWRIGHT_BROWSERS_PATH`,
   `fc-list`, `fc-cache`, mounted fonts, `.fonts` symlink, and `/dev/shm`
2. do not require Playwright itself to be installed
3. optionally add a best-effort browser smoke check when the environment makes
   it possible

Example `doctor.sh` additions:

```bash
if command -v fc-cache >/dev/null 2>&1; then
    pass "fc-cache available: $(which fc-cache)"
else
    fail "fc-cache not found"
fi

if command -v fc-list >/dev/null 2>&1; then
    pass "fc-list available: $(which fc-list)"
else
    fail "fc-list not found"
fi

if [ -n "${PLAYWRIGHT_BROWSERS_PATH:-}" ]; then
    pass "PLAYWRIGHT_BROWSERS_PATH set: ${PLAYWRIGHT_BROWSERS_PATH}"
else
    fail "PLAYWRIGHT_BROWSERS_PATH not set"
fi

if [ -L "/home/user/.fonts" ]; then
    pass "~/.fonts symlink present"
else
    warn "~/.fonts symlink missing"
fi

df -h /dev/shm | tail -n 1
```

### Testing strategy

The implementation should use three layers of automated testing:

1. unit tests for pure logic and deterministic file operations
2. render or integration tests for Docker args, templates, and app startup
   behavior
3. `test-e2e.sh` for full real-container behavior across all supported barrels

Do not rely on e2e alone.

The unit and render tests should catch regressions quickly without requiring
Docker startup for every small change. The e2e test should then prove the full
runtime contract with real containers.

#### Unit tests

These should be small, deterministic, and fast.

1. `internal/config/config_test.go`
   - default `barrel_shm_size` to `1g`
   - accept valid sizes like `64m`, `256m`, `1g`, `2g`
   - reject invalid sizes like `0`, `-1`, `1gb`, `abc`, empty after trimming

2. `internal/fontsync/sync_test.go`
   - copy supported font files from each Linux source root
   - ignore non-font files
   - preserve source-specific subdirectory structure in `~/.cooper/fonts`
   - preserve user-added destination files
   - skip unchanged files
   - update changed files when size or modtime differs
   - return warnings, not fatal errors, for unreadable optional roots
   - handle missing roots without failing

3. `internal/docker/barrel` tests
   - `ensureBarrelHostDirs` creates:
     - Cooper font dir
     - Cooper Playwright cache dir
   - barrel run args include:
     - fonts mount
     - Playwright cache mount
     - `PLAYWRIGHT_BROWSERS_PATH`
     - `DISPLAY`
     - `XAUTHORITY`
     - `COOPER_CLIPBOARD_DISPLAY`
     - `COOPER_CLIPBOARD_XAUTHORITY`
     - `--shm-size=<configured-value>`
   - the same display/auth values are used for both generic X11 and
     clipboard-bridge env vars

4. `internal/proof` tests
   - new environment checks behave correctly when vars are:
     - present and valid
     - missing
     - pointing to missing paths

#### Template and render tests

These should prove generated image and entrypoint content without needing a
full e2e run.

1. `internal/templates/templates_test.go`
   - `base.Dockerfile.tmpl` includes:
     - `Xvfb`
     - `xauth`
     - `fontconfig`
     - baseline fonts
     - the Playwright-derived Linux dependency set
   - `entrypoint.sh.tmpl`:
     - creates `~/.local/share/fonts`
     - creates `~/.cache/ms-playwright`
     - creates `~/.cache/fontconfig`
     - creates or refreshes `.fonts` symlink
     - starts one shared `Xvfb`
     - exports all four X11-related env vars
     - no longer contains the old OpenCode-only Xvfb conditional
     - waits for Xvfb readiness before native clipboard bridge startup

2. `internal/app/cooper_test.go`
   - generated runtime or app wiring includes the new mounts and env vars
   - startup support directories are created before barrels are started
   - startup warning collection can represent non-fatal font sync problems

#### E2E strategy in `test-e2e.sh`

The e2e test should use real containers and assert the runtime contract, not
just generated files.

Use the existing `barrel_exec()` pattern already present in `test-e2e.sh`.

Update the test config in `test-e2e.sh` to include:

```json
{
  "barrel_shm_size": "1g"
}
```

Add e2e fixture setup before starting barrels:

1. create a host font fixture directory under the test area, for example:
   - `${CONFIG_DIR}/fonts-fixture`
2. place at least one known font file there, for example a copied
   `DejaVuSans.ttf`
3. create a host Playwright cache dir under the test area, for example:
   - `${CONFIG_DIR}/ms-playwright`
4. ensure the started barrels mount those fixture directories the same way the
   real implementation mounts `~/.cooper/fonts` and
   `~/.cooper/cache/ms-playwright`

Required e2e assertions for every built-in tool barrel:

1. env contract:
   - `test -n "$DISPLAY"`
   - `test -n "$XAUTHORITY"`
   - `test -n "$COOPER_CLIPBOARD_DISPLAY"`
   - `test -n "$COOPER_CLIPBOARD_XAUTHORITY"`
   - `test -n "$PLAYWRIGHT_BROWSERS_PATH"`
   - `test "$DISPLAY" = "$COOPER_CLIPBOARD_DISPLAY"`
   - `test "$XAUTHORITY" = "$COOPER_CLIPBOARD_XAUTHORITY"`

2. filesystem and mounts:
   - `test -d /home/user/.cache/ms-playwright`
   - `test -d /home/user/.local/share/fonts`
   - `test -L /home/user/.fonts`
   - `test "$(readlink /home/user/.fonts)" = "/home/user/.local/share/fonts"`
   - `test -f "$XAUTHORITY"`

3. X11 runtime:
   - `pgrep -x Xvfb >/dev/null`
   - `bash -c "echo > /dev/tcp/127.0.0.1/6099"`

4. font runtime:
   - `fc-cache -f /home/user/.local/share/fonts`
   - `fc-list | grep -F "DejaVuSans"`

5. Playwright cache mount behavior:
   - `test "$PLAYWRIGHT_BROWSERS_PATH" = "/home/user/.cache/ms-playwright"`
   - `touch /home/user/.cache/ms-playwright/e2e-write-test`
   - `test -f /home/user/.cache/ms-playwright/e2e-write-test`

6. shared memory:
   - `df -h /dev/shm`
   - assert that `/dev/shm` reports the configured size rather than Docker's
     default `64M`

7. CLI regression:
   - existing help/smoke commands still succeed for:
     - `claude`
     - `codex`
     - `copilot`
     - `opencode`

8. clipboard-bridge compatibility:
   - native X11 tools still see the shared X11 env
   - shim tools still start successfully with the shared X11 env present

Example e2e assertions to add with the current helper style:

```bash
display_val=$(barrel_exec 'echo "$DISPLAY"')
clip_display_val=$(barrel_exec 'echo "$COOPER_CLIPBOARD_DISPLAY"')
if [ "$display_val" = "$clip_display_val" ] && [ -n "$display_val" ]; then
    pass "${tool}: DISPLAY matches clipboard display"
else
    fail "${tool}: DISPLAY mismatch: DISPLAY=${display_val} CLIP=${clip_display_val}"
fi

fonts_link=$(barrel_exec 'readlink /home/user/.fonts 2>/dev/null || true')
if [ "$fonts_link" = "/home/user/.local/share/fonts" ]; then
    pass "${tool}: ~/.fonts symlink correct"
else
    fail "${tool}: ~/.fonts symlink incorrect: ${fonts_link}"
fi

font_seen=$(barrel_exec 'fc-list | grep -F "DejaVuSans" | head -n 1')
if [ -n "$font_seen" ]; then
    pass "${tool}: mounted test font visible via fontconfig"
else
    fail "${tool}: mounted test font not visible via fontconfig"
fi

shm_line=$(barrel_exec 'df -h /dev/shm | tail -n 1')
if echo "$shm_line" | grep -q "1.0G"; then
    pass "${tool}: /dev/shm size reflects config"
else
    fail "${tool}: unexpected /dev/shm size: ${shm_line}"
fi
```

#### Optional gated browser smoke tests

These are valuable, but they should be gated because they require a prepared
browser cache or manual network approval.

Recommended gate:

- run only when an explicit env var is set, for example
  `COOPER_E2E_PLAYWRIGHT=1`

When the gate is enabled and a browser cache is already present, run three real
Playwright smokes inside at least one barrel:

1. default headless
2. headless with `channel: 'chromium'`
3. headed with `headless: false`

These tests should use a tiny inline Playwright script and assert:

1. browser launches successfully
2. a screenshot file is created
3. console logging is observable
4. the process exits zero

The implementation should not make these heavy tests mandatory for normal CI if
they require live downloads.

#### E2E assumptions to verify or eliminate

The e2e implementation should not silently depend on host-specific state.

These are the main assumptions that must either be verified explicitly or
eliminated from the test design:

1. Test font fixture availability
   - do not assume the host machine has a specific font like `DejaVuSans.ttf`
   - preferred solution: check in a small redistributable test font under a
     repo-owned fixture path and copy it into the e2e font fixture dir
   - fallback only if necessary: probe a host font path and skip the font
     visibility assertion when absent

2. Fontconfig output stability
   - do not rely only on a family-name grep if path-based matching is possible
   - preferred assertion: confirm `fc-list` reports the mounted fixture font
     path or basename, not just a family string that may vary slightly by
     distro

3. `/dev/shm` output formatting
   - do not rely on `df -h` human-readable strings like `1.0G`
   - preferred assertion: use `df -B1 /dev/shm | awk 'NR==2 {print $2}'` and
     compare exact byte counts
   - verified locally: `--shm-size=1g` yields `1073741824` bytes

4. X11 display number and TCP port
   - do not hardcode `6099` without deriving it from `DISPLAY`
   - preferred assertion: parse the display number from `$DISPLAY` and compute
     `6000 + display_num`
   - verified locally: `DISPLAY=127.0.0.1:99` maps to TCP port `6099`

5. Xvfb readiness timing
   - do not assert X11 connectivity immediately after container start
   - add a short wait loop for both `pgrep -x Xvfb` and TCP reachability before
     failing the e2e check

6. Browser cache prepopulation
   - do not assume Playwright browsers already exist in the cache for normal CI
   - keep real browser-launch tests behind an explicit gate like
     `COOPER_E2E_PLAYWRIGHT=1`

7. Host ownership of mounted directories
   - ensure the e2e fixture dirs are created by the test script before barrel
     startup so Docker does not create them with unexpected ownership

### 7. E2E and regression tests

Files to update:

- `test-e2e.sh`
- `internal/app/cooper_test.go`
- `internal/templates/templates_test.go`

Required automated coverage:

1. every built-in barrel has `DISPLAY` and `XAUTHORITY`
2. every built-in barrel starts successfully with the unified X11 runtime
3. `.fonts` symlink exists and points to `.local/share/fonts`
4. `PLAYWRIGHT_BROWSERS_PATH` is set in every barrel
5. mounted browser cache path exists
6. mounted font path exists
7. `/dev/shm` size reflects the configured value
8. current CLI smoke commands still succeed for Claude, Codex, Copilot, and
   OpenCode
9. `fc-cache -f` succeeds with the fonts directory mounted read-only
10. `fc-list` can see at least one mounted font when test fixtures provide one

Recommended optional E2E coverage:

1. if a Playwright package and browser cache are available, run:
   - default headless smoke
   - `channel: 'chromium'` headless smoke
   - headed smoke under `Xvfb`
2. gate these heavier tests so repo CI does not require live browser downloads

Example barrel assertions:

```bash
barrel_exec 'test -n "$DISPLAY"'
barrel_exec 'test -n "$XAUTHORITY"'
barrel_exec 'test -n "$PLAYWRIGHT_BROWSERS_PATH"'
barrel_exec 'test -d /home/user/.cache/ms-playwright'
barrel_exec 'test -L /home/user/.fonts'
barrel_exec 'fc-list | head -n 1'
barrel_exec "df -B1 /dev/shm | awk 'NR==2 {print \$2}'"
```

### 8. Documentation

Files to update:

- `REQUIREMENTS.md`
- possibly `README`-style user docs if Cooper has one later

Required doc changes:

1. explain that Playwright support is now a built-in runtime capability
2. explain that Playwright itself is still repo-managed
3. explain the mounted font directory and browser cache
4. explain the manual-approval model for browser downloads
5. explain `barrel_shm_size`

## Acceptance Criteria

The implementation is done only when all of the following are true:

1. `cooper build` produces images with the new browser-runtime packages and the
   unified Xvfb startup path
2. `cooper up` creates `~/.cooper/fonts` and
   `~/.cooper/cache/ms-playwright` before any barrel starts
3. `cooper up` performs best-effort Linux font sync and surfaces warnings
   without blocking startup
4. every built-in barrel mounts the Cooper-managed font dir read-only and the
   Playwright cache dir read-write
5. every built-in barrel exports:
   - `DISPLAY`
   - `XAUTHORITY`
   - `COOPER_CLIPBOARD_DISPLAY`
   - `COOPER_CLIPBOARD_XAUTHORITY`
   - `PLAYWRIGHT_BROWSERS_PATH`
6. every built-in barrel starts one authenticated shared X11 display and does
   not retain the old OpenCode-only Xvfb branch
7. `~/.fonts` exists as a symlink to `~/.local/share/fonts`
8. `fc-cache -f` succeeds with the font mount read-only and `fc-list` can
   discover mounted fonts
9. `doctor.sh` and `cooper proof` reflect the new runtime contract without
   requiring Playwright itself to be installed
10. `/dev/shm` size inside barrels matches the configured
    `barrel_shm_size`, default `1g`
11. Claude, Codex, Copilot, and OpenCode still pass CLI smoke checks under the
    unified display setup
12. clipboard-bridge native X11 behavior still uses the same display and auth
    material as the Playwright runtime
13. e2e coverage exists for the new mounts, env vars, X11 runtime, and shm
    behavior
14. documentation clearly states that Cooper provides runtime support only, not
    Playwright package management

## Integration With Clipboard-Bridge

This plan must not fork the X11 story away from the clipboard-bridge plan.

Rules:

1. there must be one barrel X11 display, not separate Playwright and
   clipboard displays
2. the clipboard X11 bridge continues to attach to the shared display
3. Claude and OpenCode remain shim-clipboard tools, but they still get the
   shared display runtime
4. Codex and Copilot continue to use the native X11 clipboard path

Practical consequence:

- this plan should simplify the current entrypoint because the old OpenCode-only
  Xvfb branch becomes unnecessary once Xvfb is universal

## Expected User Experience

After implementation, the intended user experience is:

1. user runs `cooper configure`
2. user sees no new Playwright tool toggle
3. user optionally adjusts barrel shared memory size
4. user runs `cooper build`
5. user runs `cooper up`
6. Cooper best-effort syncs fonts into `~/.cooper/fonts`
7. Cooper mounts fonts and Playwright browser cache into barrels
8. Cooper starts `Xvfb` for every barrel
9. user installs Playwright in the repo if needed
10. user runs `playwright install` when needed and approves network manually
11. AI CLI can now run Playwright headed, default headless, or `channel:
    'chromium'`

## Risks And Tradeoffs

### Tradeoffs we accept

1. larger base image
2. more packages to keep patched
3. first browser install still needs manual approval
4. best-effort font sync is not perfect host parity

### Risks we intentionally avoid

1. no broad mounted-home architecture
2. no repo-version-driven Cooper rebuild loop
3. no system Chromium ambiguity
4. no automatic whitelist broadening

### Remaining practical risks

1. some sites still render differently across browser modes
2. host font parity can never be perfect in every case
3. the Linux font sync step may be slow on machines with large font trees
4. heavier browser use may still require users to raise `barrel_shm_size`

## Assumption Verification

The goal of this section is to avoid hidden assumptions.

Every meaningful technical assumption used by the design is listed here with a
verification method and result.

### A1. Cooper barrels already have the basic Node runtime needed for repo-local JavaScript Playwright

Assumption:

- JavaScript Playwright can run inside barrels without adding a new Node
  installation path for this feature.

Verification:

- local container inspection of `cooper-base`

Observed result:

- `node`, `npm`, and `npx` are present under `/usr/local/bin`

Status:

- verified locally

### A2. Current built-in AI CLIs are installed under `/home/user`

Assumption:

- mounting all of `/home/user` would shadow current AI CLI installs and is
  therefore incompatible with the chosen simpler design.

Verification:

- local container inspection of current CLI images

Observed result:

- Claude lives under `/home/user/.local/...`
- Codex and Copilot live under `/home/user/.npm-global/...`
- OpenCode lives under `/home/user/.opencode/...`

Status:

- verified locally

### A3. Starting `Xvfb` and exporting `DISPLAY` by default does not obviously break the supported AI CLIs

Assumption:

- a shared default display is safe enough for Cooper's built-in AI CLIs.

Verification:

- local smoke tests:
  - `claude --help`
  - `codex --help`
  - `copilot --help`
  - `opencode --help`
- Claude also tested with forced `DISPLAY` and `Xvfb`

Observed result:

- all four commands completed successfully
- no startup breakage was observed from `DISPLAY`/`Xvfb`

Status:

- verified locally for smoke/startup behavior

Important limit:

- this is not a guarantee about every future interactive behavior
- the implementation must still keep e2e coverage for all four CLIs

### A4. Playwright browser binaries can and should be redirected to a shared path via `PLAYWRIGHT_BROWSERS_PATH`

Assumption:

- a mounted shared browser cache is a supported Playwright workflow.

Verification:

- official Playwright browsers docs describe
  `PLAYWRIGHT_BROWSERS_PATH=$HOME/pw-browsers`
- the docs explicitly describe both install-time and run-time use with that env
  var

Status:

- verified from primary docs

### A5. The official Playwright Docker image includes browser system dependencies but not the Playwright package itself

Assumption:

- Playwright package management should remain separate from the runtime image
  capability.

Verification:

- official Playwright Docker docs say:
  - the image includes browsers and browser system dependencies
  - the Playwright package should be installed separately
- local runtime check:
  - `node --version` and `npm --version` succeed
  - `require.resolve("playwright")` fails in the official image

Status:

- verified from primary docs and local experiment

### A6. We can derive a correct Chromium dependency baseline from Playwright itself

Assumption:

- Cooper should use Playwright's own dependency source of truth rather than an
  ad hoc package guess.

Verification:

- local command in official Playwright image:
  - `npx -y playwright@1.59.1 install-deps chromium --dry-run`

Observed result:

- Playwright prints a concrete Debian/Ubuntu apt package set including:
  `xvfb`, `libnss3`, `libx11-6`, `libgbm1`, `libfontconfig1`,
  `fonts-liberation`, `fonts-noto-color-emoji`, and related libraries/fonts

Status:

- verified locally

Implementation implication:

- the exact dependency set in Cooper should be refreshed from Playwright's own
  dependency source when implementing, not copied blindly from this document

### A7. Fontconfig searches both `~/.local/share/fonts` and `~/.fonts` for a non-root user

Assumption:

- mounting one directory and exposing the other as a symlink is a valid user
  font strategy.

Verification:

- official fontconfig docs show `prefix="xdg"` font directories and user cache
  behavior
- local command in the official Playwright image as the non-root user:
  - `fc-cache -v`

Observed result:

- fontconfig enumerates:
  - `/home/ubuntu/.local/share/fonts`
  - `/home/ubuntu/.fonts`

Status:

- verified from primary docs and local experiment

### A8. A mounted read-only user font directory plus `fc-cache -f` makes fonts discoverable

Assumption:

- Cooper can mount host-managed fonts read-only into the barrel and make them
  visible to applications via fontconfig without needing write access to the
  mount itself.

Verification:

- local experiment in the official Playwright image as the non-root user:
  1. mount a host directory onto `/home/ubuntu/.local/share/fonts`
  2. keep that mount read-only
  3. create `.fonts` symlink pointing at that directory
  4. run `fc-cache -f /home/ubuntu/.local/share/fonts`
  5. query `fc-list`
  6. inspect `~/.cache/fontconfig`

Observed result:

- `fc-list` reported a font from the mounted path:
  `/home/ubuntu/.local/share/fonts/DejaVuSans.ttf`
- `fc-cache` succeeded even with the mounted font directory read-only
- fontconfig wrote cache files under `/home/ubuntu/.cache/fontconfig`
  instead of into the mounted font directory

Status:

- verified locally

### A9. The `.fonts` symlink strategy works in Cooper's current runtime layout

Assumption:

- creating `/home/user/.fonts -> /home/user/.local/share/fonts` is mechanically
  sound in Cooper barrels.

Verification:

- local experiment in `cooper-base`

Observed result:

- the symlink can be created and resolves correctly while the mounted font dir
  remains accessible

Status:

- verified locally

### A10. A mounted Playwright browser cache is writable by the Cooper barrel user when host ownership matches

Assumption:

- mounting `~/.cooper/cache/ms-playwright` read-write is viable because Cooper
  already matches the barrel user UID/GID to the host user.

Verification:

- local experiment:
  1. create a host directory owned by the current host user
  2. mount it to `/home/user/.cache/ms-playwright` in `cooper-base`
  3. write `.links` and a test file from inside the container as `user`

Observed result:

- the barrel user successfully created and wrote files in the mounted cache

Status:

- verified locally

### A11. Docker `--shm-size` is practical and visible inside the container

Assumption:

- Cooper can safely expose a configurable shared-memory size by using Docker's
  `--shm-size` flag.

Verification:

- local `docker run` checks with:
  - `--shm-size=64m`
  - `--shm-size=256m`
  - `--shm-size=1g`
- inspected `df -h /dev/shm` inside the container

Observed result:

- `/dev/shm` reflected the configured size exactly in each case

Status:

- verified locally

Important limit:

- this verifies the mechanism, not the final sufficiency of `1g` for every
  future browser workload
- the implementation must still validate this with real Playwright e2e tests

### A12. The official Playwright guidance prefers image-provided deps and separate package installation

Assumption:

- Cooper should copy the same separation of concerns rather than merging the
  runtime and package-management layers.

Verification:

- official Playwright Docker docs

Observed result:

- the docs explicitly state that the image includes browsers and system deps
  while the Playwright package/dependency should be installed separately

Status:

- verified from primary docs

### A13. The current clipboard-bridge X11 split is real and should remain intact

Assumption:

- Codex and Copilot are native X11 clipboard consumers while Claude and
  OpenCode are shim consumers.

Verification:

- current Cooper code in `internal/docker/barrel.go`
- earlier clipboard-bridge design work and smoke tests

Observed result:

- `clipboardModeForTool` maps:
  - `claude`, `opencode` -> `shim`
  - `codex`, `copilot` -> `x11`

Status:

- verified locally

### A14. Manual approval for first browser download remains a product choice, not a technical blocker

Assumption:

- lack of default whitelist entries for browser downloads does not invalidate
  the runtime design.

Verification:

- product decision from this planning session

Observed result:

- the design still works because the cache mount is independent of how the
  initial browser download is approved

Status:

- verified as an explicit product decision, not a technical unknown

### Assumptions intentionally eliminated from the design

The following were considered and deliberately removed so the implementation
does not depend on them:

1. "Cooper must manage Playwright versions"
2. "Cooper must install a system Chromium binary"
3. "Mounting all of `/home/user` is acceptable"
4. "Playwright browser downloads should be auto-whitelisted"
5. "`--ipc=host` is required"

### Remaining validation targets, not hidden assumptions

These are not assumptions. They are explicit tests the implementation must add.

1. real Playwright headed smoke in a Cooper barrel using repo-local Playwright
2. real Playwright default-headless smoke in a Cooper barrel
3. real Playwright `channel: 'chromium'` smoke in a Cooper barrel
4. regression checks that the shared X11 display still cooperates with
   clipboard-bridge

## Faithful Implementation Rules

The next implementation session should follow these rules exactly.

1. Do not turn this into a new programming tool.
2. Do not install Playwright itself in Cooper images.
3. Do not install a system Chromium binary for this feature.
4. Do not skip the Playwright browser cache mount.
5. Do not skip the font mount.
6. Do not keep OpenCode as a special Xvfb snowflake after the unified path
   exists.
7. Do not add default Playwright download domains to the whitelist.
8. Do not use `--ipc=host` as the default browser-memory strategy.
9. Do not make Playwright support contingent on custom per-tool toggles.
10. Keep the shared X11 display compatible with clipboard-bridge.

## Final Recommendation

Implement this as a built-in barrel capability with:

1. always-on browser deps
2. always-on `Xvfb`
3. always-on font support
4. mounted Playwright browser cache
5. mounted Cooper-managed fonts
6. default `barrel_shm_size = 1g`

This is the simplest design that still solves the actual problem without
dragging Cooper into Playwright package management or system-browser
versioning.
