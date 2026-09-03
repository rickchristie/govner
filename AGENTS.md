# Govner
Govner is a collection of Go development tools.

# Critical Behavior
- **ALWAYS fully finish your tasks** when executing anything. Never stop to ask "would you like to continue?" or anything similar.
  You are given tasks, fully complete them, don't waste our time. Exception: Destructive actions with irreversible consequences.
- **ALWAYS verify your work and assumptions**, don't just read the code, actually test what you're doing.
  Write tests, write scripts to test behaviors, run playwright to check console, take screenshots, run the commands.
- **NEVER blame without evidence**, don't say something like "X fail due to Y", find evidence!
  **Always investigate to find root cause**, if unable to find evidence, state why and clarify it's a hypothesis.
- **ALWAYS write proper documentation**, write *why* it was done this way, and *how* only if it's not obvious.
  Write for a human/yourself when revisiting this code in the future, what's important so futureself work faster, fewer mistakes, less tokens?
- **NEVER execute staging, unstaging, or stashing git changes**, this will cause **loss of verification work** in our collaboration!
- **NEVER remove or edit changes that are not yours**, you are not the only one working in the repository.
  When you absolutely require to touch changes that are not yours, ASK permission.
- **NEVER delete valid comments**, contextual comments are important for maintainability.
  Removing comments that contains business context (the "why") will cause **loss of context**.
  Only remove comments if they are no longer valid.
- **ALWAYS** use ASD-STE100 Simplified Technical English (STE) in ALL your outputs, e.g. when communicating with me, writing documentations, code comments, variable names, function names, entity names, tests, comments.
- **ALWAYS prioritize readability, maintainability, simplicity, elegance** of your code.
  Aim for low-cyclomatic complexity in your code, exit early whenever you can, it's okay to repeat lines if we reduce cyclomatic
  complexity or prevent interleaving conditionals.
- **DO NOT excessively nil check**. Treat pointer/interface parameters/struct fields as required unless explicitly documented optional.
  If caller sends nil, then nil pointer panic is fine. Fail fast, unless we truly need the recovery.

# Projects

Read README.md of the project before starting.

## Cooper

### Design
- Cooper supports the complete host state directories of all built-in CLI agents (e.g. `~/.grok`, `~/.codex`).
  Each `cooper cli [agent]` or `cooper vm [agent]` session mounts only the state directories of the selected agent.
  Mount the complete selected state read-write. This includes auth, sessions, all configs, conversation history, auto-memory, and future state.
  A user must be able to start a session on the host, exit it, and continue it in Cooper, or do the reverse.
  All host settings for the selected agent must apply directly in Cooper.
- Treat each CLI state root as host-owned data. Mount it read-write. Do not copy it or split its children into Cooper-owned state.
- `cooper cli [agent]` and `cooper vm [agent]` must give the same user experience. They must use the same workspace path, selected-agent mounts, tool versions, settings, environment, proxy policy, clipboard behavior, port forwarding, and other Cooper features.
- The execution boundary is the only functional difference. `cooper cli` uses a Docker barrel. `cooper vm` uses a virtual machine with its own Docker daemon, so the agent can do Docker development in allow-all mode. Never mount the host Docker or container-runtime socket in the VM.
- Use `cooper cli` for most work because it starts faster and uses fewer resources. Use `cooper vm` when the work needs Docker or a stronger kernel boundary.
- A Cooper VM must have no direct route to the internet or the host LAN. Enforce this rule outside the guest so guest root cannot change it. All guest processes, the guest Docker daemon, Docker builds, guest containers, and a nested Cooper proxy must use the host Cooper proxy.
- For Grok, mount the effective host `GROK_HOME` as one root. Use `~/.grok` when `GROK_HOME` is empty. Map it to `/home/user/.grok` in the barrel.
- Keep transient Grok leader transport in the per-barrel `/tmp` mount. A barrel must not attach to a Grok process on the host through the shared state root.
- Do not install a Grok requirements file or set behavior-related `GROK_*` values. These values override the host Grok config. Cooper can set path values that map host state or isolate process transport. Cooper's proxy enforces network policy separately.
- Keep image-installed CLI binaries outside mounted state roots. A host state mount must not hide the image version.
- Cooper cleanup must never delete or change a host CLI state root.
- Reject a Grok state root that overlaps the Cooper directory in either direction. Check the direct paths and the paths after existing symlinks are resolved.

### Tests and releases
- **Cooper test suites:** when validating `cooper`, run
  `go test -C ./cooper ./... > /tmp/cooper-go-test.txt 2>&1` and
  `timeout 90m ./cooper/test-e2e.sh > /tmp/cooper-e2e.txt 2>&1` from the
  repository root; use targeted package tests while iterating, but finish with
  both full suites. Keep `cooper` visible in the command rather than relying on
  an execution tool's hidden `workdir`, because Codex's permission-hook payload
  does not currently include that per-command directory.
- **Cooper Docker-build gate:** run
  `timeout 90m ./cooper/test-docker-build.sh all > /tmp/cooper-docker-build.txt 2>&1`.
  Its reviewed modes are `mirror`, `latest`, `pinned`, `all`, and `clean`.
  Cleanup-only runs use
  `./cooper/test-docker-build.sh clean > /tmp/cooper-docker-build-clean.txt 2>&1`.
- **Other Go modules:** validate Gowt with
  `go test -C ./gowt ./... > /tmp/gowt-go-test.txt 2>&1` and pgflock with
  `go test -C ./pgflock ./... > /tmp/pgflock-go-test.txt 2>&1`.
- **Project builds:** use the exact gitignored development binaries with full
  logs: `go build -C ./cooper -o ./cooper . > /tmp/cooper-build.txt 2>&1`,
  `go build -C ./gowt -o ./gowt . > /tmp/gowt-build.txt 2>&1`, or
  `go build -C ./pgflock -o ./pgflock . > /tmp/pgflock-build.txt 2>&1`.
- **Release previews:** from the repository root, run the matching
  `./scripts/release-{cooper,gowt,pgflock}.sh` script with stdout and stderr
  redirected to `/tmp/<project>-release-preview.txt`. Execute only the
  shell-quoted commands printed by that script; they are aligned with the
  repository permission hook and the version declared in `<project>/meta/version.go`.
  Keep the private `/tmp/<project>-release-tag-message.*` file created by the
  preview until its printed `git tag -F` step has completed.

# TUI Code

Before you change TUI code, read and follow [AGENTS.TUI.md](AGENTS.TUI.md).
