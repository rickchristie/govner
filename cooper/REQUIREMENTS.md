Read through /sandb and thoroughly understand what it does - completely.
Currently sandb is tough to set-up, we need to copy paste the folder directly on each repository.
Then please read through /pgflock and notice the difference:
  - pgflock has command line to control dockers.
  - pgflock builds Dockerfile based on configuration.
  - pgflock builds once and then can be run over and over again.

# Cooper Requirements

## High level goals
- Build and start one proxy container, the proxy can be configured and rebuild easily.
  - Change whitelists, which domain is allowed or not.
  - Change socat rules, which ports in CLI containers are forwarded to which ports on the host machine.
- Livestream of the proxy logs in the TUI, shows requests that are coming in.
  - On-the-fly approve/deny for non-whitelisted domains.
- Easily change configuration
- Same ease of use as pgflock.

## Supported Platforms

- **Linux**: Any distro (Ubuntu/Debian, Fedora, Arch, Alpine, etc.) with Docker Engine 20.10+.
  The `cooper` binary is pure Go with no distro-specific dependencies. The CLI container Dockerfile
  uses Debian bookworm as base, but this only affects what runs *inside* the container, not the host.
  Host requirements: Docker Engine, bash or zsh (for login shell token resolution).
- **macOS (Apple Silicon)**: Supported with Docker Desktop 4.x+ on macOS 12+.
  Docker Desktop runs Docker Engine inside a Linux VM, so Cooper's container-side security model still applies.
  Host requirements: Docker Desktop, bash or zsh.
- **macOS (Intel)**: Expected to work with Docker Desktop 4.x+, but untested.
- **Windows**: Not supported in v1.

## New Features (Next Steps)
- Request/response body inspection in the proxy monitor via ICAP server integration. v1 has SSL bump which gives URL, method,
  headers, and status code. Full body inspection (seeing what the AI is sending/receiving) requires an ICAP server that Squid
  forwards decrypted traffic to for deep inspection. This is significantly more complex but enables the richest visibility.

## Playwright Support (Built-in Runtime Capability)

Playwright support is a built-in barrel capability, not a configurable programming tool. Cooper provides the Linux runtime
environment that Playwright needs; the repo provides the Playwright package; Playwright itself provides browser binaries.

What Cooper provides (always, in every barrel):
- Chromium/Chrome OS shared-library dependencies in `cooper-base`
- `Xvfb` virtual display (1920x1080x24) started for every barrel, with authenticated X11
- `fontconfig` plus a baseline font set (DejaVu, Roboto, Noto, Noto CJK, FreeFont, Liberation, Noto Color Emoji)
- Cooper-managed host font directory (`~/.cooper/fonts`) mounted read-only into `/home/user/.local/share/fonts`
- Cooper-managed Playwright browser cache (`~/.cooper/cache/ms-playwright`) mounted read-write into `/home/user/.cache/ms-playwright`
- `PLAYWRIGHT_BROWSERS_PATH` environment variable set in every barrel
- `DISPLAY` and `XAUTHORITY` set for every barrel (shared with clipboard-bridge X11)
- Configurable barrel shared memory (`barrel_shm_size`, default `1g`) via `--shm-size`
- Best-effort host font sync on `cooper up` (copies .ttf/.otf/.ttc/.otc from standard host font dirs for the current OS)

What Cooper does NOT do:
- Cooper does not install the Playwright npm package
- Cooper does not install a system Chromium binary
- Cooper does not manage Playwright versioning
- Cooper does not auto-whitelist Playwright browser download domains

The user or project is responsible for:
- Installing Playwright in the repo (`npm install playwright`)
- Running `playwright install` and manually approving browser downloads through the proxy monitor
- Choosing headed, default headless, or headless with `channel: 'chromium'`

This design keeps Cooper images stable across Playwright version bumps and avoids silently broadening egress.

## Detailed requirements

- `cooper configure` sets-up all required files in the machine:
  - If already configured, this command still runs and allows user to change the configuration.
  - Cooper detects that Docker Engine is installed and is the appropriate version.
  - Creates `~/.cooper` folder to contain all cooper files.
  - Generates a Cooper CA certificate (`~/.cooper/ca/cooper-ca.pem`) for TLS interception (SSL bump),
    only if one doesn't already exist. This CA is local-only, generated per-installation, never shared.
    It allows the proxy to decrypt HTTPS traffic so the monitor can show full URL, method, and headers.
    - If the CA already exists, `cooper configure` reuses it (no regeneration). This avoids invalidating
      existing barrel images that have the old CA baked in.
    - If regeneration is needed (CA deleted, corrupted, or expired), user can run `cooper configure --regenerate-ca`.
      This regenerates the CA and warns that `cooper build` must be run afterward to inject the new CA into images.
  - Creates Dockerfile for the proxy container. The proxy image must include Squid built with
    `--enable-ssl-crtd --with-openssl` for SSL bump support (may require building Squid from source
    instead of using the Alpine package).
  - Creates proxy configuration file that is loaded when cooper starts the proxy container.
  - Creates configuration that is used to generate Dockerfile for the CLI container:
    - We generate both configuration json at `~/.cooper/config.json` and Dockerfile(s).
      - The config file is important for the `cooper update` commands, as we can specify "latest" versions that doesn't version pin at all.
      - We can also specify "mirror", which means mirror version in the host machine, so whenever user runs `cooper update`, the Dockerfile will
        always be re-generated with the version that is currently in the host machine.
    - Programming Tool Setup Flow:
      - Sets up programming language environment in Dockerfile for the AI CLI to use.
      - This is important, because we want the AI to be able to run tests, run lints, run builds, etc.
      - Out-of-the box support for: Golang, Nodejs (npm, yarn, bun), Python (pip, pipenv, poetry).
        User is able to auto-generate Dockerfile for these.
      - User is instructed that they can add programming language/tools that they want manually if not in the list.
        - User customizations can be added as custom image directories in `~/.cooper/cli/{custom-name}/` containing a Dockerfile.
          These are built as `cooper-cli-{custom-name}` using `FROM cooper-base`. Cooper never touches user-created directories.
          This way `cooper update` can regenerate the base without clobbering user additions.
      - When run, this flow checks if host machine has any of these languages and tools installed, if so, it also checks
        the versions of these languages/tools in host machine.
      - Flow:
        - Programming Tool Setup screen shows list of all out-of-the-box programming language/tools and versions that will be added to the Dockerfile.
          If Dockerfile is already generated, it shows the programming language/tools that are already in the Dockerfile and their versions.
          If no Dockerfile is generated, programming tools that are detected in the host machine is on with versions detected at the host machine as starting point.
        - User can navigate and select the list of programming language/tool, it will enter the config for that language/tool, where user can:
          - Turn on/off button. Off means not included in the Dockerfile at all.
          - Mirror version button. Updates version of this tool in config is updated to mirror the version in host machine (no version pinning in config).
          - Latest version button. Updates version of this tool in config to the latest version available (no version pinning in config).
          - Pin version button. User can input any version they want, and it will be validated to ensure it is a valid version.
          - Version resolution sources (used for "latest" lookup and pin validation):
            - Go: `go.dev/dl/?mode=json` API
            - Node.js: `nodejs.org/dist/index.json`
            - Python: `endoflife.date/api/python.json` or similar stable API
            - AI CLI tools (npm-based): npm registry HTTP API (`https://registry.npmjs.org/<package>`) queried
              directly from Go. Does NOT require npm installed on the host.
            All version resolution is done via HTTP APIs directly from Go — the only host dependencies are
            Docker Engine and bash/zsh. Cooper resolves versions at `cooper configure` time and at `cooper update` time, not at runtime.
          - Back button. Go back to Programming Tool Setup screen.
          - (UI) When Mirror/Latest version is selected, it tells user that they can run `cooper update` to rebuild
            with "latest" or "mirror" version in the future.
          - (UI) Mirror button shows the version in the host machine.
        - Back to main screen, user can select "Save & Continue" button to save the configuration file.
    - AI CLI Tool Setup Flow:
      - Sets up which AI CLI tool to install in the CLI container, so when user runs `cooper cli` to enter the CLI container, the AI CLIs are ready to use.
      - Out-of-the-box support for: Claude Code, GitHub Copilot CLI, OpenAI Codex, OpenCode.
        User is able to auto-generate Dockerfile for these.
      - User is instructed that they can add AI CLI tools that they want manually if not in the list.
        User is also instructed to create GitHub issue in our repo to request adding CLI tool they want.
        - Same custom-directory approach as programming tools: user customizations go in `~/.cooper/cli/{custom-name}/Dockerfile`,
          cooper-generated Dockerfiles are never modified by the user.
      - When run, this flow checks if host machine has any of these AI CLI tools installed, if so, it also checks
        the versions of these AI CLI tools in host machine.
      - Flow:
        - AI CLI Tool Setup screen shows list of all out-of-the-box AI CLI tools and versions that will be added to the Dockerfile.
          If Dockerfile is already generated, it shows the AI CLI tools that are already in the Dockerfile and their versions.
          If no Dockerfile is generated, AI CLI tools that are detected in the host machine is on with versions detected at the host machine as starting point.
        - User can navigate and select list of AI CLI tools, it will enter the config for that AI CLI tool, where user can:
          - Turn on/off button. Off means not included in the Dockerfile at all.
          - Mirror version button. Updates version of this tool in config is updated to mirror the version in host machine (no version pinning in config).
          - Latest version button. Updates version of this tool in config to the latest version available (no version pinning in config).
          - Pin Version button. User can input any version they want, and it will be validated to ensure it is a valid version.
          - Back button. Go back to AI CLI Tool Setup screen.
          - (UI) When Mirror/Latest version is selected, it tells user that they can run `cooper update` to rebuild
            with "latest" or "mirror" version in the future.
          - (UI) Mirror button shows the version in the host machine.
        - Back to main screen, user can select "Save & Continue" button to save the configuration file.
    - Proxy Whitelist Setup Flow:
      - Sets up proxy whitelist config, that is used to define which domains are allowed through the proxy.
      - By default, all traffic is blocked, except the ones whitelisted.
      - By default, these traffic is allowed:
        - Requests to AI provider API domains that are enabled in the CLI container, so that AI CLI tools work out of the box.
          (e.g., `.anthropic.com` for Claude, `.openai.com` for Codex, `.githubcopilot.com` for Copilot, etc.)
        - Requests to `raw.githubusercontent.com`, because it's read-only and safe.
      - All package registries (npm, gopkg, pypi, crates.io, etc.) are blocked by default — including NPM.
        AI tool installation happens at image build time (`cooper build`/`cooper update`), not at runtime.
        This prevents supply-chain attacks where an AI could be tricked into downloading malicious packages or exfiltrating data.
      - Flow:
        - Proxy Whitelist Setup screen shows domain whitelist configuration.
          - Shows list of default whitelisted domains above, and list of user-added whitelisted domains.
          - For each whitelisted domain, user can select it to edit or delete it.
          - When adding/editing a whitelisted domain, user can input the domain, and also specify if subdomains are included or not
          - (UI) User is guided to add domain names that the user trusts completely, for example:
            - The user's company domains, API domains, staging domains, etc.
            - Personally owned domains.
            - Trusted metrics companies, such as your company's grafana or sentry domain.
          - (UI) User is told that requests to package manager registries, such as gopkg, npm registry, pypi, etc. are not allowed by default.
            This is to prevent supply-chain attacks, AIs can be tricked to download malicious dependencies, and could even exfiltrate data through these requests.
            User is told that Cooper mounts its own managed cache directories (`~/.cooper/cache/`) into the barrel as read-write volumes.
            Dependencies are installed inside the barrel through the proxy. Package manager registries must be explicitly whitelisted.
          - (UI) User is recommended to be as strict as possible, because control panel at `cooper up` allows the user to take a look at live network request
            and allow them on the fly. This is the recommended way, so any requests to the web are monitored.
        - Back to main screen, user can select "Save & Continue" button to save the configuration file.
    - Port Forwarding Setup Flow:
      - Port forwarding is configured as its own dedicated screen (separate from Proxy Whitelist).
      - Port forwarding uses a two-hop socat relay (see Network Architecture): socat inside the CLI container
        forwards `localhost:{port}` to `cooper-proxy:{port}` on the internal network, then socat inside the
        proxy container forwards to `host.docker.internal:{port}` on the external network to reach host services.
        Rules are configured centrally here and applied to both container entrypoints when `cooper cli` launches.
      - Shows list of port forwarding rules, each rule is like "localhost:X in CLI container is forwarded to Port Y on host machine".
      - User can add/edit/delete port forwarding rules.
      - User can self-forward in range, for example, forward port 8000-8100 to host 8000-8100, useful when development needs many ports.
      - (UI) Port-forwarding guidance is platform-aware:
        - Linux: host services should bind to `0.0.0.0` or the Docker gateway IP. Services bound strictly
          to `127.0.0.1` are reached via Cooper's HostRelay.
        - macOS: Docker Desktop tunnels `host.docker.internal` to the host machine, so services on any bind
          address, including `127.0.0.1`, are reachable from barrels.
      - (UI) User is guided to only forward ports that are necessary, for example,
        - The AI will need to access port of the local postgres database, so user adds a rule for that port.
        - User is using a self-hosted AI provider, so they add a rule for the port of that provider.
        - User is developing a web application, so they add a rule for the port of that application so AI can curl and test the application.
      - Back to main screen, user can select "Save & Continue" button to save the configuration file.
    - Proxy Setup:
      - Users can set-up:
        - Which port is used by the Squid proxy. Default: 3128 (Squid standard).
        - Which port is used for the execution bridge API. Default: 4343.
        - Barrel shared memory size (`barrel_shm_size`). Default: `1g`. Controls `--shm-size` for Docker containers.
          Needed for Chromium/Playwright browser workloads (Docker's default 64m is too small for reliable browser use).
          Accepts positive integers with optional k/m/g suffix (e.g. `64m`, `256m`, `1g`, `2g`).
        These ports must not collide, and are separate from the barrel shared memory setting.
      - The execution bridge HTTP API always binds to `127.0.0.1`.
        - Linux: it also binds to the Docker bridge gateway IP(s) discovered at runtime so containers can reach it
          directly without exposing it to the LAN.
        - macOS: Docker Desktop tunnels `host.docker.internal` to the host loopback, so no extra bind address is needed.
        No authentication is required — Cooper's threat model is a single-user local dev machine.
        Any process with local access already has the same privileges as the bridge scripts.
        See Network Architecture for the full relay path.
      - (UI) User is explained the use-case. We might need the AI CLI to do something from the host machine, for example: deploy to staging, restart-local-dev, go-mod-tidy.
        Bridges gives a HTTP API that the AI CLI can call, without us giving it direct access to the machine.
        Recommend that the script **takes no input**, and handles concurrency, i.e. what happens when requests from multiple AI agents happen at the same time.
        The stdout and stderr of the script is returned in the response of the API call, so AI can read and understand it.
  - Cooper then writes configuration file and generates the Dockerfile.
  - Cooper then asks the user, "Would you like to build?" - if so, cooper runs `cooper build`.
  - Once finished with the build, Cooper tells the user to start with `cooper up`.

- `cooper build` runs build for the proxy and CLI container images.
  - Builds proxy and all CLI images. With `--clean` flag, deletes existing images and builds with no-cache.
  - The proxy image name is `cooper-proxy`. The container name is also `cooper-proxy`.
  - CLI image build uses a multi-image architecture (one image per AI tool):
    1. Builds `cooper-base` from the cooper-generated base Dockerfile (`~/.cooper/base/Dockerfile`) — OS + programming languages.
    2. For each enabled AI tool, builds `cooper-cli-{toolname}` (e.g., `cooper-cli-claude`, `cooper-cli-codex`) from
       per-tool Dockerfiles (`~/.cooper/cli/{tool}/Dockerfile`). Each uses `FROM cooper-base` as its base layer.
    3. Also builds any user-custom images: directories in `~/.cooper/cli/` that don't match built-in tool names
       and contain a Dockerfile are built as `cooper-cli-{dirname}`.
  - This multi-image approach means each AI tool gets its own container image, enabling independent tool updates
    and smaller incremental rebuilds.
  - `cooper cli {tool-name}` launches the specific tool's image (e.g., `cooper cli claude` uses `cooper-cli-claude`).
  - Both base and proxy images are reused for all projects. Currently we don't support multiple cooper instances. Maybe later.
    - For example, user have both python and Go project in the same PC, then currently user would have to set up CLI supporting both python and Go.

- `cooper up` starts the proxy container and opens the control panel with live updates and configuration.
  - `cooper up` must be running before `cooper cli` can be used. `cooper cli` checks for a running proxy and refuses
    to start if `cooper up` is not active. The TUI is the control plane; CLI containers are the data plane.
  - The TUI must always be active, and just like `pgflock` when user exits the TUI, it also stops all cooper containers.
  - (UI) Exiting always pops up an exit confirmation dialog (like pgflock). This is intentional — keeping barrels running
    without proxy/bridge is unsafe, and an explicit visible TUI is preferred over a background daemon.
  - When starting, checks host clipboard prerequisites and warns if missing.
    - Linux: `xclip` or `wl-paste`
    - macOS: `osascript` (built-in)
  - When starting, it checks programming tool and AI CLI tool versions in the docker image against the expected version per mode:
    - **Mirror mode**: compares container version against host machine version. Warns if different.
    - **Latest mode**: compares container version against latest remote version (queried from registry APIs). Warns if outdated.
    - **Pinned mode**: no warning. The version is what the user explicitly chose.
    - If any mismatch is found, it prompts user to run `cooper update` to update the CLI image.
    - This is important because the CLI docker shares important folders with the host machine (AI CLI config folders, Cooper-managed caches, etc.).
  - **Clipboard header bar** — always visible at the top of TUI, shows clipboard state with TTL countdown.
    User presses `c` to capture host clipboard, `x` to clear. See "Clipboard Bridge" section for full details.
  - Control panel TUI tabs (Tab/Shift+Tab navigation, each tab is its own BubbleTea sub-model):
    - **Containers** tab:
      - List all live cooper containers (proxy and CLI containers), their CPU and memory usage, status.
      - Stop (s), restart (r) containers.
    - **Monitor** tab (Proxy Monitor):
      - Cooper uses Squid SSL bump (TLS interception) to decrypt HTTPS traffic. This allows the monitor to show
        full request details, not just the domain name. A Cooper CA certificate is generated during `cooper configure`
        and injected into CLI containers at build time (system CA store + `NODE_EXTRA_CA_CERTS` for Node.js tools).
      - Two-pane UI (40% left, 60% right): left pane shows a scrolling list of pending requests to non-whitelisted domains,
        right pane shows details of the currently selected request.
      - Each request to a non-whitelisted domain appears in the left pane with a countdown timer, sorted by time remaining (most urgent at top).
      - User navigates with up/down arrow keys; the right detail pane shows request-side data only (response doesn't
        exist yet while pending): full URL, HTTP method, request headers, destination domain, which container sent it, timestamp.
      - User can press 'a' or Enter to allow, 'd' to deny, 'A' to approve all pending requests.
        If timer runs out, the request is denied automatically.
      - Each approval applies to that single request. If the same domain is requested again, it appears as a new pending request.
        The HTTP client sees its connection hanging while waiting for approval; on deny/timeout, Squid returns a 403.
      - This allows user to make real-time decisions, when the AI needs to do research for example.
      - Cooper purposefully doesn't have "Always allow this request" option, it forces user to verify and think for every single request to be secure.
    - **Blocked** tab:
      - Shows history of blocked requests, including which container sent it.
      - User can navigate up and down the history, select request to view more details.
      - Detail view shows: full URL, method, request headers, domain, container, timestamp, reason (timeout/manual deny).
      - Blocked history viewer is capped at max N lines (See: Runtime Settings).
    - **Allowed** tab:
      - Shows history of allowed requests (both whitelist and manually allowed), including which container sent it.
      - User can navigate up and down the history, select request to view more details.
      - Detail view shows request data plus response data (status code, response headers) captured after the request completed.
      - Allowed history viewer is capped at max N lines (See: Runtime Settings).
    - **Bridge Logs** tab:
      - Shows live logs of the execution bridge, each log entry shows columns: TIME, ROUTE, SCRIPT, STATUS, DURATION.
      - User can select each log entry to view more details (stdout, stderr).
      - Logs viewer is capped at max N lines (See: Runtime Settings).
    - **Ports** tab (Port Forwarding):
      - Shows current port forwarding rules.
      - User can add (n), edit (e/Enter), delete (x) rules at runtime.
      - Supports both single ports and port ranges.
      - Changes are applied live via socat SIGHUP reload (no container restart needed).
    - **Routes** tab (Bridge Routes):
      - Shows list of all active execution bridges between CLI and host machine, each entry maps API path to script file path.
      - User can add (n), edit (e/Enter), delete (x) bridge entries, for example, map `/deploy-staging` to `~/scripts/deploy-staging.sh`.
      - Modal-based editing for add/edit/delete.
      - (UI) User is reminded that the best practice is to have these scripts take no input. If scripts take input, scripts must validate them religiously.
    - **Runtime** tab (Runtime Settings):
      - Can set how long to block the request in Monitor tab before it's automatically blocked. Defaults to 5 seconds.
      - Can set how many lines of blocked history requests in the log. Default 500.
      - Can set how many lines of allowed history requests in the log. Default 500.
      - Can set how many lines of execution bridge requests in the log. Default to 500.
      - Can set clipboard TTL (10–3600 seconds, default 300). How long staged clipboard images remain available.
      - Can set clipboard max size (1–100 MB, default 20 MiB). Maximum clipboard image payload size.
      - Configuration, when changed, takes effect immediately.
      - (UI) Tells the user that full logs are available at `~/.cooper/logs/` directory.
    - **About** tab:
      - Shows Cooper version, infrastructure info (proxy/bridge ports).
      - Shows list of active programming tools/CLIs, each with their installed version, compared to the version in the host machine (if any).
      - Displays startup warnings (version mismatches).
  - Even though log views are capped, they are actually written to `~/.cooper/logs` using logrotate at 10 files, each at 10MB max.

- `cooper update` regenerates Dockerfiles and rebuilds CLI images.
  - This is used when user wants to update CLI images with new versions of programming tools or AI CLI tools.
  - Detects version mismatches for programming tools (mirror/latest modes) and AI tools, then:
    - Rebuilds `cooper-base` if programming tool versions changed.
    - Rebuilds individual `cooper-cli-{toolname}` images if AI tool versions changed.
  - This is cheaper than `cooper build` — it only rebuilds images that have version mismatches.
  - If AI tool selection has changed (tools added/removed), `cooper update` also regenerates the proxy squid.conf
    (to add/remove the corresponding API domains) and hot-reloads it via `squid -k reconfigure`. No proxy image rebuild needed —
    squid.conf is volume-mounted, not baked into the image.
  - Updates `ContainerVersion` in config.json to reflect what was just built.

- `cooper cli {tool-name}` opens a CLI container for a specific AI tool:
  - Usage: `cooper cli claude`, `cooper cli codex`, `cooper cli copilot`, `cooper cli opencode`.
  - `cooper cli list` lists available tool images.
  - Each tool uses its own image (`cooper-cli-{toolname}`), but the same image is used across all workspaces.
  - It mounts the current folder where user runs this command to the CLI container.
  - This allows people to create multiple containers for different projects, each in different workspace/directory.
  - The name of the container is `barrel-{dirname}-{tool}`, for example, if user runs `cooper cli claude` in `~/myproject`,
    the container name is `barrel-myproject-claude`. If the name collides with an existing container from a different workspace
    path, a short hash of the absolute path is appended (e.g., `barrel-myproject-claude-a3f1`). This only happens on collision.
  - Supports one-shot commands with `-c` flag: `cooper cli -c "go test ./..."` runs the command and exits.
  - Sets the terminal title to `{workspace}-{random-name}` (via `\033]0;TITLE\007` escape sequence) so the user can
    distinguish multiple shells in their terminal tabs/windows.
  - Each time `cooper cli` is run, the shell is initialized with a random name suffix at the end.
    - It doesn't create new container, it reuses the same container, but with a different shell.
    - The random name are list of one or two words related to cooper profession, whiskey aging, wine aging. These are randomly selected and is unique globally.
      If "rickhouse" is already active in "myproject", it can't be used even in another workspace dir.
    - Proxy identification is at container (workspace+tool) granularity, not per-shell. All shells in the same container share the
      same IP, so the proxy monitor shows `barrel-{dir}-{tool}` for requests. The random name is for the user's own terminal
      management (distinguishing shells), not for proxy identification.
  - Volume Mounts:
    - The workspace dir is the only directory mounted as read-write.
    - `.git/hooks` inside the workspace is overlaid as read-only to prevent hook injection attacks.
    - AI tool auth/config directories (mounted read-write):
      - `~/.claude` and `~/.claude.json` — Claude Code auth and config
      - `~/.copilot` — GitHub Copilot CLI auth and chat history
      - `~/.codex` — OpenAI Codex CLI config
      - `~/.config/opencode`, `~/.local/share/opencode`, `~/.local/state/opencode`, and `~/.opencode` — OpenCode CLI config, state, and install data
    - `~/.gitconfig` — git identity (read-only)
    - Clipboard bridge (read-only):
      - `~/.cooper/tokens/{containerName}` → `/etc/cooper/clipboard-token` — per-barrel auth token
      - `~/.cooper/base/shims/` → `/etc/cooper/shims/` — pre-generated clipboard shim scripts
    - Playwright support (always mounted for every barrel):
      - `~/.cooper/fonts` → `/home/user/.local/share/fonts` (read-only) — Cooper-managed host font mirror
      - `~/.cooper/cache/ms-playwright` → `/home/user/.cache/ms-playwright` (read-write) — Playwright browser cache
      - `PLAYWRIGHT_BROWSERS_PATH=/home/user/.cache/ms-playwright` environment variable
      - `DISPLAY=127.0.0.1:99` and `XAUTHORITY=/home/user/.cooper-clipboard.xauth` for Xvfb display
      - `--shm-size` from `barrel_shm_size` config (default `1g`)
    - Per-barrel /tmp directory:
      - `~/.cooper/tmp/{containerName}` → `/tmp` (read-write) — each barrel gets its own host-backed /tmp
      - Isolated per container to avoid temp file collisions between barrels sharing a workspace.
      - Persists across container restarts (useful for AI tools that write temp files for cross-session context).
      - Host directory is pre-created before mount (`mkdir -p`), same as other mount dirs.
    - Language-specific caches (Cooper-managed, auto-configured based on enabled programming tools):
      - Go: `~/.cooper/cache/go-mod` → `/home/user/go/pkg/mod` (read-write), `~/.cooper/cache/go-build` → `/home/user/.cache/go-build` (read-write)
      - Node: `~/.cooper/cache/npm` → `/home/user/.npm` (read-write)
      - Python: `~/.cooper/cache/pip` → `/home/user/.cache/pip` (read-write)
    - All cache directories live under `~/.cooper/cache/` — no host tool caches are mounted.
    - Directories are created on host if they don't exist (`mkdir -p` before mount).
    - Caches start empty and fill naturally during normal package-manager usage inside the barrel.
    - No `GOFLAGS=-mod=readonly` — Go modules are fully writable. Dependencies are installed inside the barrel
      through the proxy (package manager registries must be whitelisted or approved via the monitor).
    - Dependency workflow per ecosystem:
      - Go: `go mod download`, `go get`, `go mod tidy` work normally inside the barrel. Module cache persists across barrel runs in `~/.cooper/cache/go-mod`.
      - Node: `npm install` works inside the barrel. The npm cache persists in `~/.cooper/cache/npm`. `node_modules/` lives in the workspace (rw).
      - Python: `pip install`, `pipenv install`, or `poetry install` work inside the barrel. The pip cache persists in `~/.cooper/cache/pip`.
        Cooper detects which Python tool is installed and generates the Dockerfile accordingly, but
        does not enforce a specific virtualenv layout — it just ensures `python` is available at the configured version.
  - Authentication / Token Management:
    - API keys and tokens are automatically resolved and forwarded into the container as environment variables.
    - Resolution order (first match wins): environment variable → `~/.cooper/secrets/{workspace-hash}` cache → login shell profile (`~/.bashrc`, `~/.zshrc`).
    - If resolved from login shell, the value is cached to `~/.cooper/secrets/{workspace-hash}` for faster subsequent launches.
      Secrets are stored in `~/.cooper/` (never in the workspace directory) to prevent accidental git commits.
    - Tokens forwarded per AI tool:
      - Claude Code: auth handled via mounted `~/.claude` and `~/.claude.json` (no env var needed)
      - GitHub Copilot CLI: `GH_TOKEN` or `GITHUB_TOKEN` env var, or `~/.copilot/.gh_token` file
      - OpenAI Codex CLI: `OPENAI_API_KEY` env var
    - VS Code integration env vars forwarded when available: `TERM_PROGRAM`, `TERM_PROGRAM_VERSION`, `CLAUDE_CODE_SSE_PORT`.
    - `CLAUDECODE` env var is NOT forwarded (prevents "nested session" error — container is an isolated sandbox, not a nested session).
  - Security Settings:
    - `--cap-drop=ALL` — drop all Linux capabilities
    - `--security-opt=no-new-privileges` — prevent privilege escalation
    - `--security-opt seccomp=<custom-profile>` — custom seccomp profile that allows bubblewrap (bwrap) syscalls for Codex CLI sandboxing, while keeping all other Docker default restrictions
    - `--init` — proper PID 1 process for signal handling
    - `--network cooper-internal` — internal Docker network with NO internet gateway. Even raw sockets and proxy-ignoring
      tools cannot reach the internet. This is the core isolation mechanism (see Network Architecture).
    - Cooper CA certificate injected into container at build time (system CA store + `NODE_EXTRA_CA_CERTS` env var) to
      enable SSL bump. This is transparent to AI tools — they see valid certificates signed by a trusted CA.
    - Auto-approve aliases configured in container's `.bashrc` via the entrypoint script (safe because container is already
      sandboxed by Cooper's network isolation, seccomp, and capability restrictions). These aliases are critical — without
      them, each AI tool would prompt for its own permission system, which is redundant inside the sandbox:
      - `claude` → `claude --dangerously-skip-permissions`
      - `copilot` → `copilot --allow-all-tools`
      - `codex` → `codex --dangerously-bypass-approvals-and-sandbox`
      - `opencode` → `opencode --auto-approve`
    - The entrypoint script must be generated from template (not hardcoded) so it adapts to which AI tools are enabled.
      Disabled tools should not have aliases or startup configuration.

- `cooper proof` is a fully self-contained integration test that stands up the entire Cooper stack, validates
  every layer, and tears it down. Output is plain text designed to be copy-pasted into a GitHub issue.
  - Refuses to run if `cooper up` is already running (to avoid conflicts).
  - Requires `cooper configure` + `cooper build` completed first.
  - **Phase 1 — Preflight**: Docker daemon, config file, CA certificate, images exist.
  - **Phase 2 — Startup**: Creates Docker networks, starts proxy, starts bridge, starts ACL listener, resolves auth tokens.
  - **Phase 3 — Container**: Starts barrel container per enabled AI tool, tests DNS resolution and proxy connectivity.
  - **Phase 4 — Network Security**:
    - **SSL bump verification**: HTTPS request through proxy without `--insecure`, validates entire CA chain.
    - Tests blocked domains are actually blocked (example.com, google.com).
    - Tests direct internet access is blocked (no route bypassing proxy).
  - **Phase 5 — Tools**: Verifies Go/Node/Python installations and versions (based on enabled tools).
    Verifies AI CLI tool installations (Claude Code, Copilot, Codex, OpenCode — based on enabled tools).
  - **Phase 6 — AI CLI Smoke Test**: Runs actual AI CLI commands to verify API connectivity (e.g., `claude -p "Reply with only the word: ok"`).
  - **Phase 7 — Port Forwarding & Bridge**: Tests bridge health endpoint and port forwarding connectivity.
  - **Teardown**: Always runs (even on failure) — stops ACL listener, bridge, barrels, proxy, cleans up networks.
  - Output shows real-time progress with ANSI colors and summary counts (PASS/FAIL/WARN/INFO).
  - Usage: `cooper proof` (from the workspace directory).

- `cooper cleanup` removes all resources created by cooper:
  - Stops and removes all running cooper containers (proxy and all CLI barrels).
  - Removes cooper Docker images (`cooper-proxy`, `cooper-base`, and all `cooper-cli-*` tool images).
  - Removes Docker networks (`cooper-external`, `cooper-internal`).
  - Optionally removes `~/.cooper` directory (config, logs, Dockerfiles). Prompts for confirmation before deleting config.
  - Does NOT remove auth directories (`~/.claude`, `~/.copilot`, etc.) — these belong to the AI tools, not cooper.

## Clipboard Bridge

The clipboard bridge solves the Docker/host clipboard gap for image paste support across all AI CLIs.
Docker containers have no access to the host clipboard — AI tools running inside barrels cannot paste images.
The clipboard bridge provides a controlled, user-initiated mechanism to stage host clipboard images and
make them available to AI tools inside containers.

### Design Principles
- **Explicit user consent**: User must press `c` in the TUI to stage a clipboard image. The host clipboard
  is never passively or automatically exposed to containers.
- **Time-limited access**: Staged images expire after a configurable TTL (default 5 minutes). Expired images
  are inaccessible — the system is fail-closed.
- **Per-barrel authentication**: Each running barrel receives a unique cryptographic token (32-byte random,
  hex-encoded to 64 chars). Tokens are mounted as read-only files, never passed as environment variables or CLI args.
- **Two delivery strategies**: Shim scripts (for tools that call xclip/xsel/wl-paste helper binaries) and
  X11 selection ownership (for tools with native clipboard integration like Rust's `arboard` crate).

### Architecture

```
Host clipboard (xclip/wl-paste)
         ↓
   [User presses 'c' in TUI]
         ↓
   LinuxReader.Read()          — reads image bytes from host clipboard
         ↓
   Normalize()                 — detects format, converts to PNG, enforces size limit
         ↓
   Manager.Stage()             — stores snapshot in memory with TTL and unique ID
         ↓
   Bridge HTTP server          — serves /clipboard/* endpoints with bearer token auth
         ↓
   [socat relay: barrel → cooper-proxy → host bridge]
         ↓
   Shim intercept              — xclip/xsel/wl-paste wrapper fetches from bridge
     OR
   X11 Bridge                  — owns CLIPBOARD selection on Xvfb, serves PNG via X11 protocol
         ↓
   AI CLI inside barrel        — sees standard clipboard image, pastes normally
```

### Delivery Strategies Per Tool

Each AI tool uses a different mechanism to access the clipboard. Cooper auto-selects the strategy:

| Tool | Mode | Mechanism | Why |
|------|------|-----------|-----|
| claude | `shim` | xclip/xsel/wl-paste wrapper scripts | Claude Code shells out to clipboard helper binaries |
| opencode | `shim` | xclip/xsel/wl-paste wrapper scripts | OpenCode uses multiple clipboard helper binaries at runtime |
| codex | `x11` | Xvfb + cooper-x11-bridge | Codex uses Rust `arboard` crate (native X11 clipboard via in-process code) |
| copilot | `x11` | Xvfb + cooper-x11-bridge | Copilot uses native clipboard module |
| custom | `auto` | Both shim + X11 | Custom tools get both strategies enabled |

#### Shim Strategy (`shim` mode)
Wrapper bash scripts replace `xclip`, `xsel`, and `wl-paste` in the container's `PATH`:
- **xclip shim**: Intercepts `xclip -selection clipboard -t TARGETS -o` (advertises `image/png` if image staged)
  and `xclip -selection clipboard -t image/* -o` (fetches PNG from bridge). All other invocations pass through
  to the real `xclip` binary.
- **xsel shim**: Intercepts `--clipboard --output` pattern. Falls back to real `xsel` otherwise.
- **wl-paste shim** (Wayland): Intercepts `wl-paste --list-types` and `wl-paste --type image/*`.
- All shims use a shared `_cooper_clip_fetch()` bash function that reads the bearer token from
  `$COOPER_CLIPBOARD_TOKEN_FILE` and calls `curl` to fetch from the bridge. Binary data is handled
  safely via tmpfile to preserve NUL bytes.

#### X11 Strategy (`x11` mode)
For tools with native clipboard integration (no shell-out to helper binaries):
- **Xvfb** (X virtual framebuffer) runs on display `:99` with `-listen tcp -nolisten unix` (TCP-only for Docker).
- **cooper-x11-bridge** binary runs as a background daemon inside the container. It:
  - Connects to Xvfb via TCP `127.0.0.1:6099` with `MIT-MAGIC-COOKIE-1` authentication.
  - Owns the X11 `CLIPBOARD` selection.
  - When a tool requests clipboard contents, the bridge fetches the staged image from the HTTP bridge
    endpoint and serves it as `image/png` via the X11 selection protocol.
  - Supports INCR (incremental transfer) for large payloads (>256KB, in 64KB chunks).
- X authority cookie is generated per-session via `mcookie`, written with mode 0600.

### HTTP Endpoints (on Execution Bridge)

Clipboard endpoints are served on the same bridge HTTP server (`localhost:{bridge_port}`), under the
reserved `/clipboard/*` namespace. User bridge routes cannot use this namespace.

- `GET /clipboard/type` — Returns JSON metadata: state (empty/staged/expired), MIME type, size, TTL remaining, variants.
- `GET /clipboard/image` — Returns raw PNG bytes with `X-Cooper-Clipboard-Id` header.
- Both endpoints require `Authorization: Bearer <token>` header. Invalid/missing tokens get 403 Forbidden.

### Token Management
- `clipboard.GenerateToken()` creates 32-byte random tokens (64-char hex strings).
- Tokens are written to `~/.cooper/tokens/{containerName}` with mode 0600 on barrel start.
- Token files are mounted into barrels at `/etc/cooper/clipboard-token` (read-only).
- Tokens are removed when barrels stop (`clipboard.RemoveTokenFile()`).
- In-memory token validation in the Manager, with disk-scan fallback for `cooper cli` barrels
  (started as separate processes).

### Container Integration

**Environment variables set in barrel containers:**
- `COOPER_CLIPBOARD_ENABLED=1`
- `COOPER_CLIPBOARD_BRIDGE_URL=http://127.0.0.1:{bridge_port}`
- `COOPER_CLIPBOARD_TOKEN_FILE=/etc/cooper/clipboard-token`
- `COOPER_CLIPBOARD_MODE={shim|x11|auto}` — auto-selected per tool
- `COOPER_CLIPBOARD_SHIMS=xclip,xsel` — which shim scripts to install
- `COOPER_CLIPBOARD_XAUTHORITY=/home/user/.cooper-clipboard.xauth` — X11 auth file path (x11/auto modes)
- `COOPER_CLIPBOARD_DISPLAY=127.0.0.1:99` — X11 display address (x11/auto modes)

**Volume mounts:**
- `~/.cooper/tokens/{containerName}` → `/etc/cooper/clipboard-token` (read-only) — per-barrel auth token
- `~/.cooper/base/shims/` → `/etc/cooper/shims/` (read-only) — pre-generated shim scripts

**Base image additions:**
- Packages: `xclip`, `xsel`, `xauth`, `xvfb` (installed unconditionally in base image)
- Multi-stage build compiles `cooper-x11-bridge` from embedded Go source, copies to `/usr/local/bin/`

**Entrypoint setup** (conditional on `ClipboardEnabled`):
- Shim mode: copies shim scripts from `/etc/cooper/shims/` to `/home/user/.local/bin/` (prepended to PATH).
- X11 mode: generates X authority cookie → starts Xvfb on TCP `:99` → starts `cooper-x11-bridge` daemon
  with auto-restart supervisor loop → exports `DISPLAY` and `XAUTHORITY`.

### TUI Integration

**Header bar** shows clipboard status at all times:
- **Empty**: `Clipboard Empty [c Copy]`
- **Staged**: `Clipboard Staged [████░░░░░░] 45s [c Replace] [x Delete]` — TTL countdown bar (color: green→yellow→red)
- **Failed**: `Clipboard Failed: <error> [c Retry]`
- **Expired**: `Clipboard Expired [c Copy]`

**Global hotkeys:**
- `c` — Capture clipboard from host (reads, normalizes, stages). Disabled during text input.
- `x` — Clear staged clipboard. Only available when staged. Disabled during text input.

**Runtime Settings tab** exposes two clipboard settings (editable, immediate effect):
- **Clipboard TTL** (10–3600 seconds, default 300) — how long staged images remain available.
- **Clipboard max size** (1–100 MB, default 20 MiB) — maximum clipboard image payload size.

### Configuration

Config fields in `~/.cooper/config.json`:
```json
{
  "monitor_timeout_secs": 30,
  "blocked_history_limit": 500,
  "allowed_history_limit": 500,
  "bridge_log_limit": 500,
  "clipboard_ttl_secs": 300,
  "clipboard_max_bytes": 20971520
}
```

Runtime settings (monitor timeout, history limits, clipboard TTL/max size) are NOT part of `cooper configure` —
they use sensible defaults and are editable at runtime via the TUI Runtime Settings tab.

### Image Processing Pipeline

1. **Read**: Cooper uses a platform-specific host reader.
   - Linux: `LinuxReader` detects display server (Wayland vs X11 via `WAYLAND_DISPLAY` env var),
     then calls `wl-paste` or `xclip` to read raw image bytes from host clipboard.
   - macOS: `DarwinReader` uses `osascript` to inspect clipboard types and export image data.
2. **Detect format**: Magic-byte detection for PNG, JPEG, GIF, BMP, TIFF, WebP, SVG. Falls back to
   `http.DetectContentType` for edge cases.
3. **Convert**: In-process conversion to PNG for common formats (JPEG, GIF, BMP, TIFF, WebP via
   `golang.org/x/image`). External conversion via ImageMagick `magick` CLI for uncommon formats
   (SVG, AVIF, HEIC, ICO, PDF, JPEG 2000, PSD, and other uncommon formats) — 30-second timeout.
4. **Size enforcement**: Input and output size limits enforced (configurable, default 20 MiB).
5. **Stage**: PNG bytes stored in memory as `StagedSnapshot` with unique ID, creation time, expiry,
   and access tracking (LastAccessAt, AccessCount).

### Host Prerequisites
- Linux: `xclip` or `wl-paste` must be installed on the host (for reading clipboard).
- macOS: `osascript` is used for clipboard access and is built into the OS.
- Optional: ImageMagick `magick` for uncommon image formats.
- `cooper up` checks prerequisites at startup and warns if missing.

## Scope Model

- **Global** (`~/.cooper/`): config.json, generated Dockerfiles, images (`cooper-proxy`, `cooper-base`, `cooper-cli-*`),
  proxy container (`cooper-proxy`), secrets cache (`~/.cooper/secrets/`), logs (`~/.cooper/logs/`),
  clipboard tokens (`~/.cooper/tokens/`), generated shim scripts (`~/.cooper/base/shims/`).
- **Per-workspace**: CLI containers (`barrel-{dirname}-{tool}`), volume mounts (workspace dir rw, Cooper-managed caches rw), socat port forwarding,
  token resolution (per-workspace secret cache keyed by path hash).
- **Per-workspace persisted** (`~/.cooper/config.json`): execution bridge route mappings (API path → script path), configured
  via the Bridges tab in the Execution Bridge screen.
- **Runtime-only** (not persisted): proxy monitor pending queue, approval decisions, TUI state, bridge API server process,
  staged clipboard snapshots (in-memory with TTL), per-barrel clipboard tokens.

## Config Change → Required Action Matrix

Different config types are editable in different places and require different actions to apply:

- **`cooper configure` only (v1)**: domain whitelist, AI tool selection, programming tool versions,
  proxy/bridge ports, CA regeneration. These require container restarts or rebuilds to apply.
- **TUI runtime (Runtime tab)**: monitor timeout, log line limits, clipboard TTL/max size. These take effect immediately.
- **TUI runtime (Routes tab)**: execution bridge route mappings. These take effect immediately.
- **TUI runtime (Ports tab)**: port forwarding rules. Applied via SIGHUP live reload.

| Config change | Required action | Reason |
|---|---|---|
| Domain whitelist add/remove | Proxy hot-reload (`squid -k reconfigure`) | squid.conf is volume-mounted, not baked in |
| AI tool enabled/disabled | `cooper update` (CLI image rebuild + proxy hot-reload) | Tool installed in image; proxy domains change |
| Programming tool version | `cooper update` (CLI image rebuild) | Tool version baked into image |
| Port forwarding rule add/remove | Live reload via SIGHUP | socat rules file is volume-mounted; SIGHUP triggers re-read in proxy + barrels |
| Execution bridge route add/remove | Immediate (runtime) | Bridge runs in `cooper up` host process, routes held in memory + persisted |
| Bridge/proxy port change | `cooper up` restart | Port is bound at process/container start |
| CA certificate regeneration | `cooper build` (full rebuild) | CA baked into CLI image at build time |
| Monitor timeout / log limits | Immediate (runtime) | TUI-side config, no container changes |
| Clipboard TTL / max size | Immediate (runtime) | Manager holds config in memory, no container changes |

`cooper configure` and the TUI Configure tab should tell the user which action is needed after each change.

# Network Architecture

The network model is the foundation that enables all Cooper features. It uses a dual-network architecture
to enforce true network isolation — CLI containers physically cannot reach the internet, even if a tool
ignores proxy environment variables or opens raw sockets.

## Docker Networks

Cooper creates two Docker networks at `cooper up` startup:

- **`cooper-external`** — regular Docker bridge network. Has a default gateway, can reach the internet.
  Only the proxy container is on this network.
- **`cooper-internal`** — Docker bridge network created with `--internal` flag. Has NO default gateway
  and NO route to the internet. CLI containers and the proxy container are both on this network.

The `--internal` flag is the key security mechanism. Containers on an internal network can reach each
other (by container name via Docker DNS) but cannot reach anything outside the network — not even the
host machine's network interfaces. This is enforced at the network layer, not by environment variables.

## Container Network Topology

```
┌─────────────────────────────────────────────────────────────────────┐
│ Host Machine                                                        │
│                                                                     │
│  cooper up (TUI process, runs directly on host):                    │
│    - Execution bridge API on 127.0.0.1:4343 + {gateway-ip}:4343    │
│    - TUI control panel                                              │
│                                                                     │
│  Host services:                                                     │
│    - PostgreSQL on 0.0.0.0:5432                                     │
│    - Redis on 0.0.0.0:6379                                          │
│    - (must bind to 0.0.0.0 or Docker gateway IP, not 127.0.0.1)    │
│                                                                     │
│  ┌─── cooper-external network (regular bridge, has internet) ───┐   │
│  │                                                               │   │
│  │  ┌──────────────────────────────────────────────┐            │   │
│  │  │ cooper-proxy container                        │            │   │
│  │  │ (on BOTH cooper-external AND cooper-internal) │            │   │
│  │  │                                               │            │   │
│  │  │  Squid Proxy (SSL bump)                       │            │   │
│  │  │    listens on 0.0.0.0:3128                    │            │   │
│  │  │                                               │            │   │
│  │  │  External ACL Helper                          │            │   │
│  │  │    stdin/stdout to Squid                      │            │   │
│  │  │                                               │            │   │
│  │  │  socat relays for host services:              │            │   │
│  │  │    *:4343 → host gateway:4343 (bridge)          │            │   │
│  │  │    0.0.0.0:5432 → host gateway:5432 (postgres) │           │   │
│  │  │    0.0.0.0:6379 → host gateway:6379 (redis)   │            │   │
│  │  └──────────────────────────────────────────────┘            │   │
│  │                                                               │   │
│  └───────────────────────────────────────────────────────────────┘   │
│                                                                     │
│  ┌─── cooper-internal network (--internal, NO internet) ────────┐   │
│  │                                                               │   │
│  │  cooper-proxy (also on this network, reachable as             │   │
│  │               "cooper-proxy" via Docker DNS)                  │   │
│  │                                                               │   │
│  │  ┌──────────────────────────────────────────────┐            │   │
│  │  │ barrel-{workspace}-{tool} container             │            │   │
│  │  │ (on cooper-internal ONLY)                     │            │   │
│  │  │                                               │            │   │
│  │  │  HTTP_PROXY=http://cooper-proxy:3128           │            │   │
│  │  │  HTTPS_PROXY=http://cooper-proxy:3128          │            │   │
│  │  │                                               │            │   │
│  │  │  socat port forwarders (entrypoint):          │            │   │
│  │  │    localhost:4343 → cooper-proxy:4343 (bridge) │            │   │
│  │  │    localhost:5432 → cooper-proxy:5432 (DB)    │            │   │
│  │  │    localhost:6379 → cooper-proxy:6379 (redis)  │            │   │
│  │  │                                               │            │   │
│  │  │  AI tools see:                                │            │   │
│  │  │    localhost:4343 = execution bridge           │            │   │
│  │  │    localhost:5432 = PostgreSQL                 │            │   │
│  │  │    HTTPS via proxy = SSL-bumped, monitored    │            │   │
│  │  │    Direct internet = IMPOSSIBLE (no route)    │            │   │
│  │  └──────────────────────────────────────────────┘            │   │
│  │                                                               │   │
│  └───────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────┘

Internet:
  Whitelisted domains (via Squid)  → anthropic.com, openai.com, etc.
  Non-whitelisted domains           → held pending in TUI monitor for approval
  Direct access (bypassing proxy)  → IMPOSSIBLE (--internal network, no route)
  Raw socket bypass                → IMPOSSIBLE (no gateway, no route exists)
```

## Why This Topology

**Dual-network isolation (the key security mechanism):**
- `cooper-internal` is created with `docker network create --internal cooper-internal`
- The `--internal` flag means: no default gateway, no route to any external network
- Containers on this network can ONLY reach other containers on the same network (via Docker DNS)
- This is enforced at the Linux networking layer — even raw sockets, `curl --noproxy '*'`, or
  any tool that ignores proxy env vars CANNOT reach the internet. There is simply no route.

**Proxy container (on both networks):**
- Connected to `cooper-external` at creation, then `docker network connect cooper-internal cooper-proxy`
- Created with `--add-host=host.docker.internal:host-gateway` so `host.docker.internal` resolves consistently for
  the proxy's host-service relay path on both Linux Docker Engine and Docker Desktop.
- Reaches the internet via `cooper-external` (for whitelisted/approved requests)
- Reachable from CLI containers as `cooper-proxy` via Docker DNS on `cooper-internal`
- Runs socat relays for host service access: listens on `cooper-internal`, forwards to host via `cooper-external`
- Squid config is volume-mounted (not baked in) so `cooper update` can hot-reload via `squid -k reconfigure`

**CLI containers (internal network only):**
- Connected to `cooper-internal` ONLY — physically isolated from the internet
- All HTTP/HTTPS traffic goes through `cooper-proxy:3128` (Docker DNS resolution on internal network)
- Host services accessed via two-hop socat: CLI socat → `cooper-proxy:{port}` → proxy socat → `host.docker.internal:{port}`
- `HTTP_PROXY`/`HTTPS_PROXY` env vars point to `cooper-proxy:3128` (not `host.docker.internal`)

**Execution bridge (runs on the host, inside `cooper up` process):**
- Always binds to `127.0.0.1:4343`
- Linux: also binds to `{docker-gateway-ip}:4343` so containers can reach it directly without exposing it to the LAN
- Linux: the Docker gateway IP is discovered at runtime via `docker network inspect cooper-external` (e.g., `172.17.0.1`)
- macOS: Docker Desktop tunnels `host.docker.internal` to the host loopback, so the extra gateway bind is not needed
- CLI containers reach it via: CLI socat (`localhost:4343`) → `cooper-proxy:4343` → proxy socat → `host.docker.internal:4343` → host
- The bridge port relay is auto-configured in the proxy container's entrypoint (not user-configured)

**Host service accessibility and HostRelay:**
- Linux: `host.docker.internal` resolves to the Docker bridge gateway IP (e.g., `172.17.0.1`), NOT `127.0.0.1`.
  Services bound strictly to `127.0.0.1` are not directly reachable through the socat relay chain.
- Linux: **HostRelay** (`docker/hostrelay.go`) mitigates this for port-forwarded services. For each forwarding rule,
  `cooper up` listens on `{gateway-ip}:{host-port}` and relays connections to `127.0.0.1:{host-port}` when needed.
  If the bind fails (service already on `0.0.0.0`), the relay is silently skipped.
- macOS: Docker Desktop tunnels `host.docker.internal` to the host machine directly, so loopback-bound services are
  reachable without HostRelay.
- The execution bridge binds to `127.0.0.1` on every platform, plus Docker gateway IPs on Linux only.

**socat port forwarding (two-hop relay):**
- **Inside CLI container** (entrypoint): `localhost:{port}` → `cooper-proxy:{port}` (Docker DNS on internal network)
  - Binds to `127.0.0.1` only, `fork,reuseaddr,backlog=5000`, auto-restart on failure
- **Inside proxy container** (entrypoint): `0.0.0.0:{port}` → `host.docker.internal:{port}` (via external network)
  - Forwards from internal-network-reachable address to host services
- User-configured ports + bridge port are both auto-generated in entrypoint templates from config
- This two-hop model is the cost of true network isolation — it replaces the simpler single-hop socat
  that sandb uses (where CLI connects directly to `host.docker.internal`)

## How Each Feature Uses the Network

| Feature | Network path |
|---|---|
| AI tool API calls | CLI → `cooper-proxy:3128` (internal) → Squid (SSL bump, whitelist) → internet (external) |
| Proxy monitor (approve/deny) | Squid → external ACL helper (stdin/stdout) → Unix socket → `cooper up` on host → TUI → user decision |
| Execution bridge | CLI socat → `cooper-proxy:4343` (internal) → proxy socat → `host.docker.internal:4343` (external) → host |
| Clipboard bridge | CLI shim/x11-bridge → socat → `cooper-proxy:{bridge_port}` → proxy socat → host bridge `/clipboard/*` |
| Host service access (DB, etc.) | CLI socat → `cooper-proxy:{port}` (internal) → proxy socat → `host.docker.internal:{port}` (external) → host (HostRelay if loopback-only on Linux) |
| Package registry blocking | CLI → `cooper-proxy:3128` → Squid → denied (not in whitelist) |
| Direct internet bypass | IMPOSSIBLE — `cooper-internal` has no gateway, no route to any external network |
| Raw socket bypass | IMPOSSIBLE — even without proxy env vars, no network route exists to the internet |

# Implementation Notes

- **External ACL helper IPC transport**: The ACL helper process runs inside the proxy container, but the TUI
  that displays approval prompts runs on the host (`cooper up`). These are separate processes in separate
  namespaces, so they cannot use in-process Go channels. The IPC mechanism is a Unix domain socket:
  - `cooper up` creates a socket at `~/.cooper/run/acl.sock` and listens for connections.
  - The socket file is volume-mounted into the proxy container at a known path.
  - The ACL helper (spawned by Squid via `external_acl_type`) reads domain requests from Squid on stdin,
    writes them to the Unix socket, waits for an approve/deny response, then writes OK/ERR back to Squid on stdout.
  - The `cooper up` host process reads pending requests from the socket, pushes them to the TUI via Go channels,
    receives the user's decision, and writes back through the socket.
  - This keeps the ACL helper simple (a small script/binary that bridges stdin/stdout ↔ Unix socket) and puts
    all decision logic in the host-side Go process where the TUI lives.
  - **Fail-closed behavior** (critical — this is a security boundary):
    - If the Unix socket file is missing: ACL helper returns ERR (deny) immediately to Squid.
    - If `cooper up` is not listening yet or crashes: socket connect/write fails, ACL helper returns ERR (deny).
    - If the socket read times out (e.g., `cooper up` is unresponsive): bounded timeout (same as the approval timer),
      then returns ERR (deny). Never hangs indefinitely.
    - On any unexpected error (malformed response, broken pipe, etc.): returns ERR (deny).
    - The ACL helper must NEVER fail open. Every error path results in deny.
- `cooper up` also starts a HTTP API server for the execution bridge on the host process (not in a container).
  - Bridge doesn't crash the entire TUI whenever there's an error, all errors are recoverable and logged properly.
- Implementation is using Golang, with bubbletea for TUI, matching `pgflock`.

# Code Architecture Overview

- High level code architecture is designed to maximize: Reliability, Maintainability, Testability, Security, and Readability of the code.
- Follows the same patterns as `pgflock`: Cobra CLI, BubbleTea TUI with Model-Update-View, embedded templates, channel-based state sync.
- All TUI pages are implemented in such a way that the UI (view) is separated with the model and controllers (input data/events and the event handlers):
  - There is a TUI cli test where we can select which page, then which state/event flows to test:
    - The mock data input and the event handlers are mocked.
    - Whenever there's a bug, we can create new scenarios on the CLI, effectively creating sort of a storybook to test the TUI screens.

## Test Strategy

Tests are organized by layer, each with clear scope:

- **Model/State Tests** (unit tests, pure Go):
  - Config loading, validation, defaults, version comparison logic.
  - Proxy ACL helper decision logic: given pending queue state + timeout, assert correct allow/deny.
  - Bridge route matching: given API path + bridge config, assert correct script resolution.
  - Name generator: uniqueness, format validation.
  - Template generation: given config, assert Dockerfile/squid.conf output matches expected.
  - These are the bulk of tests. All business logic should be testable without Docker.

- **TUI Model Tests** (unit tests, bubbletea teatest):
  - Given model state + message, assert new model state (no rendering needed).
  - Test tab navigation, approval key handling, timer countdown, history scrolling.
  - Use `teatest` package for automated golden file tests that capture terminal output and assert against snapshots.
  - TUI storybook CLI (`cooper tui-test`) for manual QA: select page, inject mock state/events, visually verify.

- **Docker Integration Tests** (integration tests, require Docker):
  - Config → Dockerfile generation → image build → container start → health check.
  - Volume mount verification (correct paths, permissions).
  - Security setting verification (cap-drop, seccomp, no-new-privileges).
  - These tests are slower, tagged with `//go:build integration` so they don't run in normal `go test`.

- **External ACL Helper Tests** (unit tests):
  - Stdin/stdout protocol: given domain input, assert correct OK/ERR output.
  - Timeout behavior: assert deny after configured seconds with no approval.
  - Approval flow: assert allow when approval signal received within timeout.
  - Concurrent requests: multiple domains pending simultaneously.
  - Fail-closed error paths (security-critical):
    - Socket file missing → immediate deny.
    - `cooper up` not listening (connection refused) → immediate deny.
    - Broken pipe mid-request → deny.
    - Malformed response from host process → deny.
    - Socket read timeout (host unresponsive) → deny after bounded timeout, never hang.

- **Bridge API Tests** (unit/integration tests):
  - HTTP handler tests: given request path, assert correct script execution and response.
  - Error handling: script not found, script timeout, script crash.
  - Concurrent execution: multiple bridge calls at once.
  - Bind address verification: bridge listens on `127.0.0.1` + Docker gateway IP only, NOT on `0.0.0.0` (LAN-exposed).

- **Proof Tests** (integration tests, require running container):
  - Verify each diagnostic check produces correct OK/FAIL output.
  - Network isolation: whitelisted domains pass, blocked domains fail, direct access fails.

- **Network Acceptance Tests** (first-class, critical — guards the core security guarantee):
  - **Direct egress impossible**: from inside a CLI container, attempt `curl --noproxy '*' https://example.com` (raw socket,
    bypassing proxy env vars). Must fail with "no route to host" or connection refused — NOT a proxy error.
    This validates the `--internal` network has no gateway.
  - **SSL bump works end-to-end**: from inside a CLI container, make an HTTPS request through the proxy to a whitelisted
    domain. Must succeed without certificate errors. Validates the full CA chain: generated → injected → trusted → SSL bump decryption.
  - **Host services reachable via two-hop relay**: from inside a CLI container, connect to a forwarded port (e.g., `localhost:5432`).
    Must successfully reach the host service. Validates: CLI socat → proxy (internal) → proxy socat → host (external).
  - **Execution bridge reachable**: from inside a CLI container, call `localhost:4343` bridge API. Must get a valid response.
    Validates the full bridge relay path.
  - These tests are part of `cooper proof` AND are standalone integration tests tagged `//go:build integration`.

## Module Structure

Following pgflock's patterns (Cobra CLI + BubbleTea + internal packages):

```
cooper/
├── main.go                          # CLI entry point (Cobra): configure, build, up, update, cli, proof, cleanup
├── go.mod / go.sum
│
├── meta/
│   └── version.go                   # Version string
│
├── cmd/
│   ├── acl-helper/
│   │   └── main.go                  # ACL helper binary (built into proxy image, bridges Squid stdin/stdout ↔ Unix socket)
│   └── cooper-x11-bridge/
│       ├── main.go                  # X11 CLIPBOARD selection owner (built into base image for x11 clipboard mode)
│       └── main_test.go             # Integration tests (atom interning, selection requests, INCR transfers)
│
├── internal/
│   ├── app/                         # Core application orchestration
│   │   ├── app.go                   # Application lifecycle
│   │   ├── configure.go             # Configure command orchestration
│   │   ├── cooper.go                # CooperApp main struct and startup sequence
│   │   ├── cooper_test.go
│   │   ├── events.go                # Application events
│   │   ├── mock.go                  # Test mocks
│   │   ├── testapp.go               # Test application helper
│   │   └── configure_test.go
│   │
│   ├── config/
│   │   ├── config.go                # JSON config loading, validation, defaults
│   │   ├── config_test.go           # Config validation, version comparison tests
│   │   ├── versions.go              # Host version detection (go version, node --version, etc.)
│   │   ├── ca.go                    # CA certificate generation and management
│   │   ├── ca_test.go
│   │   ├── resolve.go               # Version resolution via HTTP APIs (go.dev, nodejs.org, npm, etc.)
│   │   └── resolve_test.go
│   │
│   ├── configure/                   # `cooper configure` TUI wizard
│   │   ├── configure.go             # Main configure orchestration
│   │   ├── programming.go           # Programming Tool Setup flow
│   │   ├── aicli.go                 # AI CLI Tool Setup flow
│   │   ├── whitelist.go             # Proxy Whitelist Setup flow
│   │   ├── portforward.go           # Port Forwarding modal logic
│   │   ├── portfwdscreen.go         # Port Forwarding screen UI
│   │   ├── proxy.go                 # Proxy/Bridge port setup
│   │   ├── save.go                  # Save & Build options screen
│   │   ├── layout.go                # TUI layout helpers
│   │   ├── layout_test.go
│   │   ├── modal.go                 # Configure modal dialogs
│   │   ├── textinput.go             # Text input component for configure
│   │   └── configure_test.go
│   │
│   ├── templates/                   # Embedded templates (//go:embed *.tmpl)
│   │   ├── templates.go             # Template rendering from config + shim generation
│   │   ├── templates_test.go        # Golden file tests for generated output
│   │   ├── base.Dockerfile.tmpl     # Base image: OS + languages + x11-bridge multi-stage build + xclip/xvfb
│   │   ├── cli-tool.Dockerfile.tmpl # Per-AI-tool image layer (FROM cooper-base)
│   │   ├── proxy.Dockerfile.tmpl    # Proxy container Dockerfile template
│   │   ├── proxy-entrypoint.sh.tmpl # Proxy container entrypoint (socat relays, logrotate, Squid)
│   │   ├── squid.conf.tmpl          # Squid proxy config template
│   │   ├── entrypoint.sh.tmpl       # CLI container entrypoint (socat, aliases, clipboard shims/X11, welcome)
│   │   ├── doctor.sh                # Diagnostic script (embedded, used by proof — includes clipboard checks)
│   │   └── ERR_ACCESS_DENIED        # Custom Squid error page (embedded)
│   │
│   ├── docker/                      # Docker image + container management
│   │   ├── build.go                 # Image build (full and incremental)
│   │   ├── barrel.go                # CLI container lifecycle (create, exec, stop, list)
│   │   ├── proxy.go                 # Proxy container lifecycle
│   │   ├── health.go                # Container health checks, stats (CPU/mem)
│   │   ├── network.go               # Docker network creation/management (dual-network)
│   │   ├── network_test.go
│   │   ├── portforward.go           # socat rules file management and SIGHUP live reload
│   │   ├── hostrelay.go             # Host TCP relays: gateway IP → 127.0.0.1 for loopback-bound services
│   │   ├── seccomp.go               # Custom seccomp profile loader
│   │   └── seccomp-bwrap.json       # Seccomp profile allowing bubblewrap syscalls
│   │
│   ├── proxy/                       # Squid proxy interaction
│   │   ├── acl.go                   # ACL listener: Unix socket server, pending queue, timeout, approve/deny
│   │   ├── acl_test.go              # ACL decision logic tests
│   │   ├── helper.go                # ACL helper utilities
│   │   ├── monitor.go               # Request stream parsing, history (blocked/allowed)
│   │   ├── monitor_test.go
│   │   └── reconfigure.go           # Squid config hot-reload (squid -k reconfigure)
│   │
│   ├── bridge/                      # Execution bridge HTTP API
│   │   ├── server.go                # HTTP server on 127.0.0.1 + Docker gateway IP
│   │   ├── handler.go               # Route → script dispatch, stdout/stderr capture
│   │   ├── handler_test.go          # Route matching, error handling, concurrency tests
│   │   ├── config.go                # Bridge route config (API path → script path)
│   │   └── config_test.go
│   │
│   ├── proof/                       # `cooper proof` self-contained lifecycle test
│   │   └── proof.go                 # 7-phase integration test: preflight → startup → container → network → tools → AI CLI → portfwd
│   │
│   ├── names/                       # Random whiskey/cooper name generator
│   │   ├── names.go                 # Name list + uniqueness tracking
│   │   └── names_test.go
│   │
│   ├── auth/                        # Token resolution (env → ~/.cooper/secrets/ → login shell → file)
│   │   ├── resolve.go               # Token resolution logic per AI tool
│   │   └── resolve_test.go
│   │
│   ├── clipboard/                   # Clipboard bridge for image paste support
│   │   ├── types.go                 # ClipboardObject, StagedSnapshot, BarrelSession, ClipboardState
│   │   ├── manager.go               # Staged clipboard manager with TTL, per-barrel token auth
│   │   ├── manager_test.go
│   │   ├── reader_linux.go          # Host clipboard reader (Wayland wl-paste / X11 xclip detection)
│   │   ├── reader_linux_test.go
│   │   ├── http.go                  # HTTP endpoints: GET /clipboard/type, GET /clipboard/image
│   │   ├── http_test.go
│   │   ├── normalize.go             # Image format detection and PNG normalization pipeline
│   │   ├── normalize_test.go
│   │   ├── convert.go               # In-process image conversion (JPEG, GIF, BMP, TIFF, WebP → PNG)
│   │   ├── convert_external.go      # External conversion via ImageMagick (SVG, AVIF, HEIC, ICO)
│   │   ├── shims.go                 # Bash shim generators for xclip, xsel, wl-paste
│   │   ├── shims_test.go
│   │   ├── token.go                 # Token generation (32-byte random), file-based persistence
│   │   └── errors.go                # Sentinel errors (ErrNoImage, ErrOversized, ErrInvalidToken, etc.)
│   │
│   ├── logging/                     # Logging infrastructure
│   │   ├── logger.go
│   │   └── logger_test.go
│   │
│   ├── pubsub/                      # Publish-subscribe event system
│   │   ├── broker.go                # Event broker for TUI state sync
│   │   └── broker_test.go
│   │
│   ├── aclsrc/                      # ACL helper source embedding
│   │   ├── embed.go                 # Embeds acl-helper Go source for proxy image build
│   │   └── embed_test.go
│   │
│   ├── x11src/                      # X11 bridge source embedding
│   │   ├── embed.go                 # Embeds cooper-x11-bridge Go source for base image build
│   │   ├── main.go.src              # Exact copy of cmd/cooper-x11-bridge/main.go
│   │   └── embed_test.go            # Verifies embedded copy matches source
│   │
│   ├── tableutil/                   # Table formatting utilities
│   │   ├── table.go
│   │   └── table_test.go
│   │
│   └── tui/                         # BubbleTea TUI for `cooper up`
│       ├── model.go                 # Root model: active tab, global state, channels
│       ├── app.go                   # Root Update/View: message routing to sub-models, tab bar
│       ├── messages.go              # TUI event/message definitions
│       │
│       ├── theme/                   # Shared types, colors, styles
│       │   ├── types.go             # SubModel interface, tab constants, event types
│       │   └── styles.go            # Lipgloss styling, cooper color palette
│       │
│       ├── containers/              # Containers tab (list, CPU/mem, stop/restart)
│       │   ├── model.go
│       │   └── view.go
│       │
│       ├── proxymon/                # Proxy Monitor tab (two-pane approval UI)
│       │   ├── model.go             # Pending queue, timers, selected index
│       │   └── view.go              # Left: request list with timers, Right: detail pane
│       │
│       ├── history/                 # Blocked/Allowed history tabs (shared component, mode-based)
│       │   ├── model.go             # Scrollable list with detail view, ModeBlocked/ModeAllowed
│       │   └── view.go
│       │
│       ├── bridgeui/                # Execution Bridge (two separate tabs: logs + routes)
│       │   ├── model.go             # Bridge Logs tab: execution logs with detail view
│       │   ├── view.go              # Bridge Logs tab: rendering
│       │   └── routes.go            # Bridge Routes tab: route CRUD with modal editing
│       │
│       ├── portfwd/                 # Port Forwarding tab (runtime rule management)
│       │   └── model.go             # Rules list with add/edit/delete, live reload via socat SIGHUP
│       │
│       ├── settings/                # Runtime Settings tab (monitor timeout, log limits, clipboard settings)
│       │   └── model.go             # Number input with inline validation, immediate effect
│       │
│       ├── about/                   # About tab (versions, tool status, startup warnings)
│       │   └── model.go
│       │
│       ├── loading/                 # Loading/startup/shutdown screens
│       │   └── model.go             # Multi-step progress with animated barrel roll
│       │
│       ├── events/                  # Event message types for TUI
│       │   └── events.go            # Shared event/message definitions across TUI packages
│       │
│       └── components/              # Shared UI components
│           ├── modal.go             # Confirmation dialogs (exit, restart)
│           ├── tabs.go              # Tab bar navigation
│           ├── timer.go             # Countdown timer bars with color gradient
│           ├── table.go             # ScrollableList (vertical list with cursor selection)
│           └── viewport.go          # ScrollableContent (free-form scrollable text)
```

Key design patterns (matching pgflock):
- **Embedded templates** (`//go:embed`) — Dockerfile, squid.conf, entrypoint.sh, doctor.sh, error pages baked into binary. No external file dependencies at runtime.
- **Source embedding for Docker builds** — `internal/aclsrc/` and `internal/x11src/` embed Go source code that gets compiled inside Docker images during multi-stage builds, ensuring binary/source consistency.
- **Pub/sub event system** — `internal/pubsub/` broker decouples proxy monitor, bridge, clipboard, and TUI. Components publish events; TUI subscribes and refreshes.
- **Callback architecture** — TUI sets `onRestart`, `onShutdown`, `onQuit` callbacks. `main.go` calls them to control loading screen.
- **Sub-model composition** — each TUI tab is its own BubbleTea model implementing `SubModel` interface (Init/Update/View). Root model routes messages to active tab.
- **Multi-image architecture** — one Docker image per AI tool (`cooper-cli-{tool}`), all sharing `cooper-base`. Enables independent tool updates and per-tool containers.
- **ACL helper as separate binary** — `cmd/acl-helper/` compiled into proxy image; bridges Squid stdin/stdout to Unix socket.
- **X11 bridge as separate binary** — `cmd/cooper-x11-bridge/` compiled into base image; owns X11 CLIPBOARD selection for native clipboard consumers.
- **Dual clipboard strategy** — shim scripts (for tools that shell out to xclip/wl-paste) and X11 selection ownership (for tools with native clipboard integration). Strategy auto-selected per AI tool.
