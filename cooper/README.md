# Cooper

### Barrel-proof containers for undiluted AI

Run AI coding assistants in network-isolated Docker containers where every outbound request is visible, controllable, and reversible -- from a real-time TUI.

## Why Cooper?

AI coding assistants need broad system access to be useful -- but that access is a liability. They can be prompt-injected into exfiltrating code through package registries, downloading malicious dependencies, or making unexpected network requests. Cooper solves this by running each AI tool in its own Docker container on a network that **physically cannot reach the internet**, with a Squid SSL-bump proxy as the only exit and a TUI control panel where you approve every non-whitelisted request in real time.

**What you get:**

- **No internet escape** -- Containers run on a Docker `--internal` network with no gateway. Even raw sockets and `curl --noproxy '*'` can't get out. The [Security Model](#security-model) enforces this at the Linux networking layer -- there is simply no route.
- **See every HTTPS request** -- SSL bump decrypts TLS traffic so the [Proxy Monitor](#tui-control-panel) shows complete URLs, methods, and headers -- not just domain names.
- **Approve requests in real time** -- Non-whitelisted requests appear in the [TUI Control Panel](#tui-control-panel) with a countdown timer and a short host-side alert phrase. Approve or deny once, or explicitly allow one exact hostname until the current `cooper up` exits.
- **Access local host ports** -- Forward PostgreSQL, Redis, dev servers, or any host service into barrels through [Port Forwarding](#port-forwarding). Uses a two-hop socat relay so containers reach host services without any internet access.
- **Run scripts on host** -- Let AI tools trigger deploy, restart, or test scripts through the [Execution Bridge](#execution-bridge) -- a controlled HTTP API that returns stdout/stderr without giving shell access to your machine.
- **Copy-paste images** -- Paste screenshots and images into AI tools running inside containers with the [Clipboard Bridge](#clipboard-bridge). Press `c` in the TUI to stage your clipboard -- AI tools inside barrels see it as a normal paste. Time-limited, per-barrel authenticated.
- **Run headed browsers** -- Built-in Xvfb virtual display, Chromium dependencies, host font sync, and shared memory configuration for [Playwright](#playwright-support) testing inside barrels -- headed mode works out of the box.
- **Multi-tool, multi-workspace** -- Each AI tool gets its own [container image](#configuration). Open multiple barrels across different project directories, all monitored from one TUI.

## Supported AI Tools

| Tool | Command | Auto-approve flag |
|------|---------|-------------------|
| **Claude Code** | `cooper cli claude` | `--dangerously-skip-permissions` |
| **GitHub Copilot CLI** | `cooper cli copilot` | `--allow-all-tools` |
| **OpenAI Codex CLI** | `cooper cli codex` | `--dangerously-bypass-approvals-and-sandbox` |
| **OpenCode** | `cooper cli opencode` | None |
| **Grok Build** | `cooper cli grok` | `--always-approve` |

The listed auto-approve flags are safe because the container is already sandboxed -- Cooper's network isolation, seccomp profile, and capability restrictions replace each tool's built-in permission system.

Custom tools can be added by placing a Dockerfile in `~/.cooper/cli/{tool-name}/`. The name `grok` is reserved for built-in Grok Build. If you already have a custom `~/.cooper/cli/grok` directory, rename it and update any `cooper cli` invocation before configuring Grok.

### Grok Build

Grok is installed from xAI's native CLI release channel, not npm. Mirror, latest, and pin all resolve to an exact `grok-<version>-linux-<arch>` artifact that Cooper copies into `/home/user/.local/bin/grok`.

Sign in with Grok on the host before you start a Grok barrel:

```bash
grok login --oauth
```

Cooper mounts the complete host Grok state root read-write at `/home/user/.grok`. The source is `GROK_HOME` when that variable is not empty. Otherwise, the source is `~/.grok`. Cooper maps the source to a stable container path and sets `GROK_HOME=/home/user/.grok` in the image.

The Grok state root and the Cooper configuration directory must not contain each other. Cooper checks the direct paths and the paths after existing symlinks are resolved. It refuses an overlap before it resets runtime data, mounts Grok state, or removes Cooper configuration. This rule prevents Cooper cleanup from deleting host-owned Grok auth or sessions.

One root mount includes `auth.json`, `config.toml`, managed config, requirements, sessions, conversation search indexes, history, memory, rules, skills, plugins, logs, locks, downloads, and future Grok state. Cooper does not keep a separate OAuth store or per-barrel session store. A state file that Grok adds below this root is shared without a Cooper update.

The Grok leader socket is transient process transport, not durable state. Cooper sets `GROK_LEADER_SOCKET=/tmp/cooper-grok-leader.sock`. Grok uses this path for the socket and its paired lock. Each barrel has a separate `/tmp` mount. Thus, a Grok process in a barrel cannot attach to a Grok leader process on the host or in another barrel.

The workspace also has the same absolute path on the host and in the barrel. Thus, Grok uses the same workspace session key in both places. Exit a Grok conversation before you resume it in the other environment. Grok lock files protect shared files, but they do not make one conversation safe to use from two terminals at the same time.

Cooper does not install a Grok requirements file and does not set behavior-related `GROK_*` variables. The state-root and leader-socket variables only map paths. Host Grok settings, including memory settings, apply in the barrel. A host `XAI_API_KEY` is also forwarded when it is set. Cooper's network policy is separate from Grok settings. Default proxy hosts while Grok is enabled are exact `auth.x.ai` and `cli-chat-proxy.grok.com`. The inference host has a fixed path allowlist (`/v1/chat/completions`, `/v1/responses`, `/v1/messages`, `/v1/models`, `/v1/user`, `/v1/privacy/coding-data-retention`). Storage, traces, settings, and unknown paths are denied even after dynamic approval. A Grok feature that needs another host remains blocked until the user allows that host. All barrels share one proxy, so domains required by other agents, including GitHub for Copilot, remain reachable from a Grok barrel.

Clipboard paste uses Cooper's X11 bridge.

### OpenCode

OpenCode is installed from the official GitHub Release tarball
`https://github.com/anomalyco/opencode/releases/download/v<version>/opencode-linux-<x64|arm64>.tar.gz`,
not from `https://opencode.ai/install`. That convenience URL only redirects to a moving install script
which then fetches the same artifact, and it rate-limits (HTTP 429) during Docker builds.
The binary is placed in `/home/user/.local/bin/opencode` so the runtime `~/.opencode` state mount cannot hide it.

## Supported Platforms

- **Linux**: Any distro with Docker Engine 20.10+ and bash or zsh.
- **macOS (Apple Silicon)**: Docker Desktop 4.x+. Requires macOS 12+.
- **macOS (Intel)**: Docker Desktop 4.x+. Untested but expected to work.
- **Windows**: Not supported.

## How It Works

Cooper uses a **dual-network architecture** to enforce true network isolation at the Linux networking layer:

```mermaid
flowchart TB
    subgraph host["<b>HOST MACHINE</b>"]
        direction TB

        subgraph tui["<b>cooper up</b> &mdash; TUI Control Panel"]
            direction LR
            monitor["<b>Monitor</b><br/>Real-time request<br/>approve / deny"]
            bridge_api["<b>Execution Bridge</b><br/>localhost:4343"]
            clipboard["<b>Clipboard Manager</b><br/>TTL-based staging"]
        end

        host_services["<b>Host Services</b><br/>PostgreSQL, Redis,<br/>dev servers"]
    end

    subgraph external["<b>cooper-external</b> &mdash; bridge network <i>(has internet)</i>"]
        subgraph proxy_box["<b>cooper-proxy</b>"]
            direction TB
            squid["<b>Squid Proxy</b><br/>SSL bump on :3128"]
            socat_proxy["<b>socat relays</b><br/>port forwarding to host"]
        end
    end

    subgraph internal["<b>cooper-internal</b> &mdash; --internal network <i>(NO internet, NO gateway)</i>"]
        direction LR
        subgraph barrel1["<b>barrel-myproject-claude</b>"]
            claude["Claude Code<br/><i>auto-approve</i>"]
            tools1["Go + Node + Python<br/>Workspace mounted rw<br/>Cooper-managed caches rw"]
        end
        subgraph barrel2["<b>barrel-myproject-codex</b>"]
            codex["Codex CLI<br/><i>auto-approve</i>"]
            tools2["Same isolation,<br/>different AI tool"]
        end
    end

    allowed["<b>Allowed</b><br/>anthropic.com<br/>openai.com<br/>github.com"]
    denied["<b>Denied</b><br/>No route exists"]

    barrel1 -- "HTTPS_PROXY" --> squid
    barrel2 -- "HTTPS_PROXY" --> squid
    barrel1 -. "socat :4343" .-> socat_proxy
    barrel2 -. "socat :4343" .-> socat_proxy
    socat_proxy -- "host.docker.internal" --> host_services
    socat_proxy -- "host.docker.internal" --> bridge_api
    squid -- "Whitelisted" --> allowed
    squid -. "Non-whitelisted" .-> monitor
    monitor -. "approve / deny" .-> squid
    barrel1 --x denied
    barrel2 --x denied

```

**Key insight:** The `cooper-internal` network is created with Docker's `--internal` flag -- it has **no default gateway and no route to any external network**. Containers on this network can only reach each other via Docker DNS. Even if an AI tool ignores proxy environment variables, opens raw sockets, or runs `curl --noproxy '*'`, it cannot reach the internet. There is simply no route.

The proxy container sits on **both** networks -- it receives traffic from barrels on the internal network and forwards whitelisted requests to the internet via the external network. Non-whitelisted requests are held pending in the TUI for your real-time approval.

## Quick Start

### Prerequisites

- **Linux**: Docker Engine 20.10+
- **macOS**: Docker Desktop 4.x+ (Docker Engine runs inside a Linux VM)
- **Go 1.21+** (for installation via `go install`)
- bash or zsh

### Install

```bash
go install github.com/rickchristie/govner/cooper@latest
```

Make sure Go's bin directory is in your `PATH`. If `cooper` isn't found after install, add this to your `~/.bashrc` or `~/.zshrc`:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

### Setup

```bash
# 1. Interactive configuration wizard
#    Sets up programming tools, AI tools, proxy whitelist, port forwarding,
#    barrel environment variables
cooper configure

# 2. Build container images (proxy + base + per-tool CLI images)
cooper build

# 3. Start the control panel TUI (must be running before using barrels)
cooper up

# 4. Open a barrel (from your project directory)
cooper cli claude
```

### Day-to-day usage

```bash
# Start the control panel (once per session)
cooper up

# Open barrels from any project directory
cd ~/myproject && cooper cli claude
cd ~/other-project && cooper cli codex

# Update tool versions (mirrors host or fetches latest, based on your config)
cooper update

# Verify the full stack works end-to-end
cooper proof
```

## Commands

| Command | Description |
|---------|-------------|
| `cooper configure` | Interactive TUI wizard -- programming tools, AI tools, whitelist, ports, barrel env, bridge |
| `cooper build` | Build proxy and all CLI container images. `--clean` for no-cache rebuild |
| `cooper up` | Start proxy, bridge, and TUI control panel. Must be running for barrels to work |
| `cooper update` | Regenerate templates, reload a running proxy, and rebuild only images with desired-vs-built drift |
| `cooper cli <tool>` | Launch a barrel. `-c "cmd"` for one-shot execution. `list` to show available tools |
| `cooper proof` | Full lifecycle integration test -- preflight through AI smoke test, then teardown |
| `cooper cleanup` | Remove all containers, images, and networks. Optionally remove `~/.cooper` |

## TUI Control Panel

The control panel (`cooper up`) is the nerve center. It has these tabs:

| Tab | What it does |
|-----|-------------|
| **Containers** | Live CPU/memory stats for all barrels and proxy. Stop/restart containers |
| **Monitor** | Real-time pending requests with countdown. Approve/deny once, or allow an exact hostname for this `cooper up` session |
| **Blocked** | History of denied requests with full details |
| **Allowed** | History of approved requests with response status codes and headers |
| **Bridge Logs** | Execution bridge invocations -- route, script, status, duration, stdout/stderr |
| **Ports** | Port forwarding rules. Add/edit/delete live (applied via SIGHUP, no restart) |
| **Routes** | Execution bridge mappings (API path to host script). Add/edit/delete at runtime |
| **Runtime** | Monitor timeout, history limits, clipboard TTL/size, and proxy alert sound toggle. Changes take effect immediately |
| **About** | Version info, installed tool versions vs host versions, implicit language servers, startup warnings |

**Clipboard bar** is always visible at the top -- press `c` to copy an image from your host clipboard so AI tools can paste it, `x` to clear.

When a new request enters manual approval, Cooper can play one short host-side alert phrase. The Runtime tab includes a persisted checkbox for this, and it defaults to off so Cooper stays quiet unless you explicitly enable it. Linux uses PulseAudio or PipeWire's PulseAudio compatibility layer, macOS uses `afplay`, and if host audio is unavailable Cooper keeps running and disables the alert with a startup warning.

## Configuration

All configuration lives in `~/.cooper/`. Run `cooper configure` to change settings through the interactive wizard.

Every Cooper screen keeps its header and help footer fixed. The middle body owns
all remaining terminal rows and scrolls with the mouse wheel, arrow keys or
`j`/`k`, and Page Up/Page Down. On selectable lists, moving the selection also
keeps the selected row visible. Mouse reporting is enabled in both
`cooper configure` and `cooper up`; hold your terminal's selection modifier
(commonly Shift) when you want native text selection.

`Save & Build` has two explicit phases. Configuration validation, template
generation, ACL helper generation and CA staging finish on the preparation
screen at 100%. Cooper then switches to a dedicated Docker build screen that
streams combined Docker stdout/stderr. The log follows new output by default;
scrolling pauses follow mode and `End` resumes it. A failed build preserves its
logs and concrete error until dismissed.

### Programming Tools

Cooper detects Go, Node.js (npm/yarn/bun), and Python (pip/pipenv/poetry) on your host and offers three version modes:

| Mode | Behavior | When to use |
|------|----------|-------------|
| **Mirror** | Matches your host machine version | Keep container in sync with local dev |
| **Latest** | Fetches latest stable from upstream APIs | Always stay current |
| **Pin** | Exact version you specify | Reproducible builds |

Built-in programming tools also install Cooper-managed standard language-server tooling, versioned from the selected runtime:

- Go -> `gopls`
- Node.js -> `typescript-language-server` and `typescript`
- Python -> `pyright` and `python-lsp-server`

During the image build, Cooper installs `gopls` exclusively through the
official `proxy.golang.org` module service and `sum.golang.org` checksum
database. It does not trust a third-party module mirror and does not use
`GOPROXY=direct`, which would still depend on Google's vanity and source hosts.

Intermittent destination-specific routing failures can occur even while other
internet traffic remains healthy, so Cooper makes up to eight fresh install
attempts. Each attempt has a two-minute deadline and completed module-cache
downloads remain available to the next attempt. A five-second linear backoff
between attempts lets a failing edge or NAT path recover while keeping the
Docker layer's worst-case duration bounded.

The smaller official Go metadata requests use four bounded attempts with a
short linear backoff. Transport failures, HTTP 429, and server errors are
retried; HTTP 404 remains a definitive missing-version result. This covers both
latest-version resolution and the `go.dev` artifact check used to validate an
older pinned Go release before a build starts.

These are implicit defaults attached to the language tool, not separate top-level programming tools. TypeScript remains bundled under Node.js.

`~/.cooper/config.json` stores both desired configuration and built state. That built state includes top-level `container_version` values, resolved `implicit_tools`, and the built base Node runtime (`base_node_version`). `cooper update` and startup/About warnings compare those built values against the current desired state.

`cooper configure` save-only is allowed to reuse last-built implicit tool versions only when the relevant built runtime still matches the current desired runtime. If Cooper cannot prove that match, it fails instead of generating misleading Dockerfiles.

Run `cooper update` to apply Mirror/Latest changes after host upgrades.
When built language-server versions or the effective base Node runtime drift from the current desired versions, startup warnings and the About tab surface that mismatch before you open barrels.

Every `cooper update` also regenerates the volume-mounted proxy configuration. If Squid is running, Cooper reloads it even when no image needs a rebuild. This prevents save-only tool selection and whitelist changes from leaving Squid on an older authorization set.

### AI Tools

Same three version modes (Mirror/Latest/Pin) for each AI tool. Each enabled tool gets its own Docker image (`cooper-cli-{tool}`) built on top of `cooper-base`.

### Custom Tools

Place a Dockerfile in `~/.cooper/cli/{name}/` using `FROM cooper-base`. Cooper builds it as `cooper-cli-{name}` and never overwrites user-created directories. Launch with `cooper cli {name}`.

### Domain Whitelist

All traffic is blocked by default except:
- AI provider API domains for enabled tools (anthropic.com, openai.com, etc.)
- `raw.githubusercontent.com` (read-only, safe)

Package registries (npm, PyPI, Go proxy, crates.io) are **blocked by default** to prevent supply-chain attacks where an AI could be tricked into downloading malicious packages or exfiltrating data through registry requests. You can whitelist specific registries if needed, approve an individual request, or press `w` on a pending request to allow only that exact hostname until the current `cooper up` exits.

Persistent trusted domains still belong in `cooper configure` (company APIs, staging servers, metrics dashboards). Session access is held only in memory, applies to every barrel attached to that Cooper proxy, remains visible and revocable under `s` in the Monitor tab, and is cleared without editing proxy settings when Cooper exits.

### Port Forwarding

Forward host service ports into barrels (e.g., PostgreSQL, Redis, dev servers). Uses a two-hop socat relay: barrel -> proxy -> host.

**Note (Linux):** Host services must bind to `0.0.0.0` or the Docker gateway IP to be reachable from containers. Services bound to `127.0.0.1` are handled by Cooper's HostRelay, which transparently proxies connections from the gateway IP to localhost.

**Note (macOS):** Docker Desktop handles host access natively. Services on any bind address, including `127.0.0.1`, are reachable from containers via `host.docker.internal`. No HostRelay is needed.

### Barrel Environment

Use the `Barrel Environment` screen in `cooper configure` to define global env vars that are loaded into every later `cooper cli` session.

Example values:

```text
API_BASE_URL=https://internal.example.com
FEATURE_FLAG=1
EMPTY=
```

- Scope is global: the values live in `~/.cooper/config.json` and apply to all barrels, tools, and workspaces.
- Runtime-only: changes apply on the next `cooper cli` session. No `cooper build` is needed.
- Precedence is safe: Cooper loads user env first, then restores protected runtime env such as `HTTP_PROXY`, `PATH`, `TZ`, `DISPLAY`, token env, terminal color/hyperlink policy and metadata env, IDE env, and `COOPER_*` names.
- Protected names cannot be configured, including `HTTP_PROXY`, `PATH`, `TZ`, `TERM`, `COLORTERM`, `NO_COLOR`, `FORCE_COLOR`, `OPENAI_API_KEY`, and any `COOPER_*` variable.
- Values are stored in plain text in `~/.cooper/config.json`. This is not a secret store.

### Execution Bridge

Map API routes to host scripts so AI tools can trigger actions without shell access:

```
/deploy-staging  ->  ~/scripts/deploy-staging.sh
/restart-dev     ->  ~/scripts/restart-dev.sh
/go-mod-tidy     ->  ~/scripts/go-mod-tidy.sh
```

Scripts should take no input and handle concurrency. Stdout/stderr is returned in the HTTP response.

### Clipboard Bridge

Press `c` in the TUI to capture an image from your host clipboard. AI tools inside barrels see it as a normal paste -- no special commands needed.

- **User-initiated** -- your clipboard is never passively exposed. You choose when to share.
- **Time-limited** -- staged images expire after a configurable TTL (default 5 minutes).
- **Per-barrel authenticated** -- each barrel gets a unique cryptographic token. No cross-barrel access.
- **Format support** -- PNG, JPEG, GIF, BMP, TIFF, WebP, SVG (via ImageMagick). All converted to PNG.

Works transparently with every supported AI tool. Claude Code and OpenCode use shim scripts that intercept clipboard helper calls. Codex, Copilot, and Grok Build use an X11 bridge that owns the virtual display clipboard. Custom tools get both strategies.

Configure TTL and max image size in the TUI Runtime Settings tab.

### Playwright Support

Every barrel comes with the runtime environment Playwright needs for headless browser testing:

- Chromium shared-library dependencies pre-installed
- Xvfb virtual display (1920x1080) with authenticated X11
- Baseline font set (DejaVu, Roboto, Noto, Noto CJK, Liberation, Noto Color Emoji)
- Host fonts synced to `~/.cooper/fonts` (mounted read-only into barrels)
- Shared Playwright browser cache (`~/.cooper/cache/ms-playwright`, mounted read-write)
- Configurable shared memory (`barrel_shm_size`, default `1g`) -- Docker's default 64m is too small for browsers

Cooper does **not** install Playwright itself or download browsers. Your project provides `npm install playwright` and `playwright install`. When Playwright downloads browsers, the requests appear in the TUI monitor for approval.

## Volume Mounts

| Host Path | Container Path | Mode | Purpose |
|-----------|---------------|------|---------|
| Current directory | Same path | read-write | Workspace |
| `.git/hooks` | Same path | read-only | Prevent hook injection |
| `~/.claude`, `~/.claude.json` | `/home/user/...` | read-write | Claude Code state |
| `~/.copilot` | `/home/user/.copilot` | read-write | Copilot state |
| `~/.codex` | `/home/user/.codex` | read-write | Codex state |
| `~/.config/opencode`, `~/.local/share/opencode`, `~/.local/state/opencode`, `~/.opencode` | `/home/user/...` | read-write | OpenCode config and state (binary lives in `~/.local/bin`) |
| `$GROK_HOME`, or `~/.grok` when unset | `/home/user/.grok` | read-write | Complete Grok auth, config, sessions, history, memory, and other state |
| `~/.gitconfig` | `/home/user/.gitconfig` | read-only | Git identity |
| `~/.cooper/cache/go-mod` | `/home/user/go/pkg/mod` | read-write | Go module cache |
| `~/.cooper/cache/go-build` | `/home/user/.cache/go-build` | read-write | Go build cache |
| `~/.cooper/cache/npm` | `/home/user/.npm` | read-write | npm cache |
| `~/.cooper/cache/pip` | `/home/user/.cache/pip` | read-write | pip cache |
| `~/.cooper/tmp/{container}` | `/tmp` | read-write | Per-barrel temp directory |

Language caches are Cooper-managed under `~/.cooper/cache/`, auto-configured based on which programming tools are enabled. They start empty and fill naturally during normal package-manager usage. Each barrel gets its own host-backed `/tmp` directory, isolated per container to avoid collisions between barrels sharing a workspace. Cooper clears the entire `~/.cooper/tmp/` tree whenever `cooper up` starts and whenever it shuts down, so every control-plane session begins and ends with a pristine temp area.

## Security Model

| Layer | Mechanism |
|-------|-----------|
| **Network** | `--internal` Docker network -- no gateway, no route to internet |
| **Proxy** | Squid SSL bump with domain whitelist and real-time approval |
| **Capabilities** | `--cap-drop=ALL` -- all Linux capabilities dropped |
| **Privileges** | `--security-opt=no-new-privileges` |
| **Seccomp** | Custom profile allowing bubblewrap (for Codex) while restricting everything else |
| **Process** | `--init` for proper PID 1 signal handling |
| **CA** | Per-installation local CA for TLS interception, never shared |
| **Git hooks** | `.git/hooks` mounted read-only to prevent injection |
| **Dependencies** | Package registries blocked by default; caches Cooper-managed under `~/.cooper/cache/` |
| **Clipboard** | User-initiated, time-limited, per-barrel authenticated, fail-closed |

## Adding Dependencies

Package registries are blocked by default. To install dependencies inside a barrel, either whitelist the needed registries in `cooper configure` or approve individual requests through the TUI monitor.

```bash
# Inside a barrel (after whitelisting registries or approving via monitor):
go mod download          # cached in ~/.cooper/cache/go-mod
npm install              # cached in ~/.cooper/cache/npm
pip install -r req.txt   # cached in ~/.cooper/cache/pip
```

Caches persist across barrel runs under `~/.cooper/cache/`, so subsequent installs are fast.

## Troubleshooting

### Run the full diagnostic suite

```bash
cooper proof
```

This stands up the entire stack, tests SSL, proxy, tools, AI CLI connectivity, port forwarding, and bridge -- then tears everything down. Output is designed to be copy-pasted into a GitHub issue.

### Grok login, versions, or custom-directory collisions

- Missing or expired login: stop active Grok sessions and run `grok login --oauth` on the host. The next barrel uses the updated shared `auth.json`.
- Unexpected state root: start Cooper from the same host environment as Grok. If you use `GROK_HOME`, make sure that it is set before `cooper cli grok` starts.
- Unsafe state root: move `GROK_HOME` outside the Cooper configuration directory. Neither path can contain the other, including through a symlink.
- A conversation is not available: use the same workspace path, and exit the first Grok process before you resume the conversation.
- A new Grok API path is blocked: Squid denies unknown `cli-chat-proxy.grok.com` paths with 403. That is fail-closed until Cooper reviews the path.
- Unsupported architecture: Grok images support Linux `amd64`/`x86_64` and `arm64`/`aarch64` only.
- `~/.cooper/cli/grok` already exists as a custom image: rename it. Cooper will not overwrite or ignore that directory.

### Check logs

```bash
# Command logs
ls ~/.cooper/logs/

# Proxy logs (from running container)
docker logs cooper-proxy
```

### Rebuild everything

```bash
# Clean rebuild (no Docker cache)
cooper build --clean
```

### Remove everything

```bash
cooper cleanup
```

## License

MIT
