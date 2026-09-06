# Host Home and Agent State Path Parity

## Status

This document records an architecture investigation. It is not an approved implementation plan yet.
No Cooper behavior has changed as part of this investigation.

The investigation started after a Codex session created in Cooper could not be resumed reliably on
the host. The immediate fault was an absolute rollout path in the shared Codex SQLite database. The
investigation then found a wider class of home-directory compatibility risks.

Last updated: 2026-09-06.

## Goal

Cooper must let a user start an agent session on the host, stop it, continue it in a Docker or VM
barrel, stop it again, and continue it on the host. The same host authentication, configuration,
history, memory, skills, plugins, and other agent state must apply at each boundary.

This goal has two separate requirements:

1. Cooper must share every host-owned state root that the selected agent uses.
2. A shared path must keep the same meaning on the host and in the barrel.

Sharing the same bytes is not sufficient when an agent stores absolute paths or resolves paths from
the user home.

## Confirmed Codex Failure

The affected Codex thread used this incorrect SQLite rollout path:

```text
/home/ricky/.codex/sessions/2026/09/01/rollout-2026-09-01T18-14-20-01a05cad-5d3b-78e0-83ce-2f9abdcb50ac.jsonl
```

The example above was corrected on another machine to its actual host path. On the current machine,
the same defect appears in the opposite direction: Cooper-created rows use `/home/user/.codex`, but
the host can only see the files below `/home/ricky/.codex`.

A read-only query of `/home/ricky/.codex/state_5.sqlite` on 2026-09-06 produced these results:

| Stored rollout root | Thread rows | Files visible on the host |
| --- | ---: | ---: |
| `/home/ricky/.codex` | 235 | 235 |
| `/home/user/.codex` | 163 | 0 |
| Other roots | 0 | 0 |

All 163 missing rows use the current Cooper barrel path. All 235 host-path rows resolve to files.
This is direct evidence that Cooper wrote container-only absolute paths into host-owned Codex state.

## Codex Source Baseline

The official OpenAI Codex repository was cloned for this investigation:

```text
Repository: https://github.com/openai/codex
Temporary clone: /tmp/codex-cli-investigation.rbzWKB/codex
Inspected main commit: ac192cd7937b0d73edc6dffe009940ae53782dd4
Commit date: 2026-09-06 07:42:32 +0000
Installed host CLI: codex-cli 0.152.0
Matching source tag: rust-v0.152.0
```

The relevant state-root and rollout behavior is present in both the inspected main commit and the
installed `rust-v0.152.0` tag.

### `CODEX_HOME` resolution

Codex resolves its state root in `codex-rs/utils/home-dir/src/lib.rs`.

- A nonempty `CODEX_HOME` takes priority.
- An explicit `CODEX_HOME` must exist and must be a directory.
- Codex canonicalizes an explicit `CODEX_HOME`.
- When `CODEX_HOME` is empty or absent, Codex uses the operating-system home directory plus
  `.codex`.
- Codex does not canonicalize that default path in this function.

Source:
<https://github.com/openai/codex/blob/ac192cd7937b0d73edc6dffe009940ae53782dd4/codex-rs/utils/home-dir/src/lib.rs>

### Absolute rollout paths

Codex creates a new rollout path by appending `sessions/YYYY/MM/DD/<rollout-file>` to the resolved
Codex home. It does not store this path relative to `CODEX_HOME`.

Source:
<https://github.com/openai/codex/blob/ac192cd7937b0d73edc6dffe009940ae53782dd4/codex-rs/rollout/src/recorder.rs>

The first state database migration defines `threads.rollout_path` as required text. Codex writes the
complete path string to this field.

Source:
<https://github.com/openai/codex/blob/ac192cd7937b0d73edc6dffe009940ae53782dd4/codex-rs/state/migrations/0001_threads.sql>

### Resume fallback does not repair an existing different path

Current Codex resume code first asks SQLite for a rollout path. When that path is stale, direct
resume by thread ID can scan rollout filenames below the current `CODEX_HOME` and find the file.

Source:
<https://github.com/openai/codex/blob/ac192cd7937b0d73edc6dffe009940ae53782dd4/codex-rs/rollout/src/list.rs>

This fallback does not make the shared state portable again. Codex deliberately refuses to replace
an existing row when the found rollout path differs from the selected SQLite path. A thread can have
more than one immutable rollout after `thread/revert`, so a filesystem scan cannot always select the
correct rollout.

Source:
<https://github.com/openai/codex/blob/ac192cd7937b0d73edc6dffe009940ae53782dd4/codex-rs/rollout/src/state_db.rs>

The `rust-v0.152.0` test `find_thread_path_falls_back_when_db_path_is_stale` confirms both parts of
this behavior: direct lookup finds the file, and the stale SQLite path remains unchanged.

Relevant history:

- `b9decc0a0c4f0167a9797fd755726318dea15a81`, dated 2026-06-15, attempted to repair stale rollout
  paths. It remains on topic branches and is not the current main behavior.
- `4ef836f883c38ba6d39e6920f335ce6452b7de33`, dated 2026-08-12, added the current protection that
  preserves the selected path when more than one rollout can belong to a thread.

Cooper must not depend on Codex fallback or read repair to correct paths created at a different
filesystem location.

## Current Cooper Behavior

Cooper currently uses a stable container home:

```text
Container user: user
Container home: /home/user
```

The base image matches the host numeric UID and GID, but it creates a user named `user` with home
`/home/user`. Many image binaries, caches, shell files, clipboard files, and runtime files also use
that path.

For Codex, the current mount plan maps:

```text
Host source:      <host-home>/.codex
Container target: /home/user/.codex
```

Cooper does not currently set `CODEX_HOME` for the Codex workload. Codex therefore derives
`/home/user/.codex` from the container home and writes that absolute prefix to the shared database.

The current cross-agent mapping has related limits:

- Grok maps the effective host root to `/home/user/.grok` and sets
  `GROK_HOME=/home/user/.grok`.
- OpenCode maps five default Linux roots below the host home to their equivalent paths below
  `/home/user`. It does not resolve host XDG overrides.
- The common runtime environment does not set `HOME`, `USER`, `LOGNAME`, or XDG variables. Those
  values come from the image account and process environment.
- Docker and the VM do not resolve a relative host `GROK_HOME` in the same way. Docker resolves it
  from the Cooper process working directory. The VM joins it to the host home. The VM also trims the
  value, while Grok treats every nonempty value verbatim. This is a separate `cooper cli` and
  `cooper vm` parity defect for relative or whitespace-valued overrides.

Relevant Cooper files include:

- `internal/workload/mountplan.go`
- `internal/workload/environment.go`
- `internal/workload/paths.go`
- `internal/docker/grokpaths.go`
- `internal/docker/barrel.go`
- `internal/vm/lifecycle.go`
- `internal/vmguest/mounts.go`
- `internal/vmguest/docker.go`
- `internal/config/barrel_env.go`
- `internal/templates/base.Dockerfile.tmpl`
- `internal/templates/cli-tool.Dockerfile.tmpl`
- `internal/templates/entrypoint.sh.tmpl`

The shared workload mount plan is a good architecture boundary. The defect is the path-remapping
policy, not the fact that Cooper uses a mount plan.

## Narrow Codex Fix

A narrow fix for the confirmed rollout defect would do the following at runtime for Codex only:

```text
Host source:       /home/ricky/.codex
Container target:  /home/ricky/.codex
Container variable: CODEX_HOME=/home/ricky/.codex
Container user:    user
Container HOME:    /home/user
```

Cooper would calculate the effective host Codex root with the same rules as Codex:

1. Use a nonempty host `CODEX_HOME`.
2. Verify and canonicalize that explicit path.
3. Otherwise, use the logical host home plus `.codex`.
4. Mount the source at the same effective absolute path in Docker and the VM.
5. Set workload `CODEX_HOME` to that container-visible path.
6. Reserve `CODEX_HOME` so a barrel environment setting cannot point Codex away from the authorized
   mount.

Setting `CODEX_HOME=/home/user/.codex` while keeping the current target does not fix the defect. It
continues to store a container-only prefix. Setting `CODEX_HOME=/home/ricky/.codex` without moving
the mount also does not work because that directory does not contain the shared state.

This narrow fix prevents new bad rollout paths. It does not satisfy the full Cooper host-state goal.

## Confirmed Codex Uses of the User Home Outside `CODEX_HOME`

The `rust-v0.152.0` source has direct user-home behavior outside the Codex state root.

### User skills

Codex loads the current user skill root from:

```text
$HOME/.agents/skills
```

It also keeps the older `$CODEX_HOME/skills` root for compatibility. Sharing only `.codex` misses
current user skills stored under `.agents`.

Source:
<https://github.com/openai/codex/blob/ac192cd7937b0d73edc6dffe009940ae53782dd4/codex-rs/ext/skills/src/host_roots.rs>

### Personal plugin marketplaces

Codex discovers personal marketplace manifests below the user home. Supported home-relative paths
include:

```text
.agents/plugins/marketplace.json
.agents/plugins/api_marketplace.json
.claude-plugin/marketplace.json
.cursor-plugin/marketplace.json
```

Some plugin checkout flows also write the personal marketplace at
`$HOME/.agents/plugins/marketplace.json`.

Sources:

- <https://github.com/openai/codex/blob/ac192cd7937b0d73edc6dffe009940ae53782dd4/codex-rs/core-plugins/src/marketplace.rs>
- <https://github.com/openai/codex/blob/ac192cd7937b0d73edc6dffe009940ae53782dd4/codex-rs/core-plugins/src/remote/share/checkout.rs>

### Tilde path expansion

Codex expands paths that start with `~` through the operating-system user home. This path type is
used while Codex reads path-valued configuration fields. A shared `config.toml` can therefore have
different behavior when `HOME` differs, even when `CODEX_HOME` points to the same directory.

Source:
<https://github.com/openai/codex/blob/ac192cd7937b0d73edc6dffe009940ae53782dd4/codex-rs/utils/absolute-path/src/lib.rs>

Other TUI features also resolve home-relative paths for transcript export, editor temporary files,
links, themes, and custom pet assets. Some of these paths are only user-interface conveniences, but
they prove that `CODEX_HOME` is not a complete home abstraction.

### Child-process environment

Codex treats `HOME`, `USER`, and `LOGNAME` as core Unix environment variables for commands that it
starts. A different barrel home therefore also affects tools invoked by Codex, such as shells,
package managers, Git, SSH, MCP servers, and user scripts.

Source:
<https://github.com/openai/codex/blob/ac192cd7937b0d73edc6dffe009940ae53782dd4/codex-rs/protocol/src/shell_environment.rs>

### User name and password database

Codex uses user identity separately from the state root in a small number of places. For example,
realtime prompt setup uses the real name or user name, and shell detection reads the current user
entry from the password database. These uses do not cause the confirmed rollout defect, but they
show that `HOME` alone is not the complete process identity.

Sources:

- <https://github.com/openai/codex/blob/ac192cd7937b0d73edc6dffe009940ae53782dd4/codex-rs/core/src/realtime_prompt.rs>
- <https://github.com/openai/codex/blob/ac192cd7937b0d73edc6dffe009940ae53782dd4/codex-rs/shell-command/src/shell_detect.rs>

## Revised Architecture Direction

The narrow `CODEX_HOME` fix solves the known session-path failure. It does not give full host and
barrel parity. The stronger candidate rule is:

> Every host-owned path that an agent can persist or refer to should keep the same absolute path at
> every execution boundary.

The barrel should project the host user context instead of translating it to `/home/user`.

Candidate projected values are:

- Numeric UID and GID.
- Logical host home path in `HOME`.
- The same home path in the barrel password-database entry.
- Host login name in `USER` and `LOGNAME`, when it is valid and safe in the Linux image.
- The same login name in the password-database entry when practical.
- Effective XDG configuration, data, state, and cache roots.
- Effective agent-specific roots such as `CODEX_HOME` and `GROK_HOME`.
- The host shell only when Cooper supports and installs a compatible shell. Do not point the user
  entry at a host shell path that does not exist in the image.

The exact home path is more important than the user name. A user named `ricky` commonly has
`/home/ricky` on Linux, but can have `/Users/ricky` on macOS or any administrator-selected path.
Linux containers can use `/Users/ricky` as a home path. Cooper must not derive a path from the user
name.

Matching the user name can still improve parity for programs that inspect `USER`, `LOGNAME`, or the
password database. It must be a supporting identity value, not the state-root locator.

## Proposed Home Projection Shape

Cooper must not mount the complete host home. That would expose unrelated credentials and files.
Instead, Cooper can create a barrel-local writable home skeleton at the same absolute path and mount
only authorized paths below or outside it.

Example on Linux:

```text
HOME=/home/ricky

/home/ricky                         barrel-local writable skeleton
/home/ricky/.codex                  host Codex state, read-write overlay
/home/ricky/.agents                 host shared agent state, read-write overlay if required
/home/ricky/.gitconfig              host Git configuration, read-only overlay
/home/ricky/Personal/govner         host workspace, read-write overlay
```

Example on macOS:

```text
HOME=/Users/ricky

/Users/ricky                        barrel-local writable skeleton
/Users/ricky/.codex                 host Codex state, read-write overlay
/Users/ricky/.agents                host shared agent state, read-write overlay if required
/Users/ricky/Personal/govner        host workspace, read-write overlay
```

Parent mounts must be installed before child overlays. Cooper's current mount-plan depth sorting is
useful for this rule. Docker and the VM guest must implement the same ordering and targets.

## Cooper-Owned Paths

The current image uses `/home/user` for more than the agent state. It contains or refers to:

- Installed agent binaries.
- npm global binaries.
- OpenCode binaries.
- Shell startup files.
- Clipboard shims and Xauthority files.
- Playwright and language caches.
- Nested Cooper state.
- VM guest Docker configuration.

A home projection must keep Cooper-owned data separate from projected host-owned paths. Candidate
stable locations include `/opt/cooper`, `/usr/local/lib/cooper`, `/var/lib/cooper`, and explicit
per-runtime mounts. Language caches can keep explicit environment variables and do not need to
follow `HOME`.

Do not let a host-state mount hide an image-installed agent binary. This requirement already exists
for Grok and OpenCode and must remain true after any home redesign.

## Existing Damaged Codex Rows

A prevention fix does not repair existing `/home/user/.codex/...` rows.

Do not perform a broad automatic replacement without validation. The SQLite database is host-owned,
and a thread can have more than one rollout after a revert.

A future repair operation should:

1. Require all Codex processes that use the state root to stop.
2. Back up the SQLite database and its active journal or WAL state safely.
3. Select only rows with a known old Cooper prefix.
4. Replace that exact prefix with the effective host Codex root.
5. Require the resulting file to exist.
6. Read the rollout metadata and require its thread ID to match the database row.
7. Refuse ambiguous or mismatched rows.
8. Apply validated changes in one SQLite transaction.
9. Run SQLite integrity checks and Codex resume/list verification.
10. Keep the backup location visible to the user.

An explicit repair command is safer than an automatic startup mutation. This choice remains open.

## Cross-Agent Source Audit Baseline

The investigation also inspected the current source of Grok Build and OpenCode. Both projects are
open source. The clones are temporary evidence and are not Cooper dependencies.

| Agent | Official repository | Inspected commit | Commit date | Branch | License | Temporary clone |
| --- | --- | --- | --- | --- | --- | --- |
| Grok Build | <https://github.com/xai-org/grok-build> | `72a61251fcffb464bcc687aeb5a998e5a98ec0c9` | 2026-09-01 | `main` | Apache-2.0 | `/tmp/cooper-home-agent-audit.HXHtcK/grok-build` |
| OpenCode | <https://github.com/anomalyco/opencode> | `337fd144d2ba144743368f78d9579a99cce175bd` | 2026-09-06 | `dev` | MIT | `/tmp/cooper-home-agent-audit.HXHtcK/opencode` |

The Grok repository is a periodic export from the SpaceXAI monorepo. Its `SOURCE_REV` file records
monorepo revision `a549186d9d39311f2d3ee4208db62af8c65aa476`.

The OpenCode source pins `xdg-basedir` version 5.1.0. That exact dependency was inspected at commit
`8cceade858e4da18cb971bf1844f086e9e213563`, dated 2021-08-05, in:

```text
/tmp/cooper-home-agent-audit.HXHtcK/xdg-basedir
```

Installed binaries supplied two isolated runtime checks:

- `grok 1.0.4 (d846eb93d9) [stable]`
- `opencode 1.17.18`

The installed versions are not the inspected source commits. Runtime results below are therefore
supporting evidence for stable path rules, not proof that every current source path ran locally.

## Grok Build Source Audit

### Repository status

Grok Build is open source. The official repository contains the Rust source for the CLI, TUI, and
agent runtime under the Apache-2.0 license. Its README states that SpaceXAI periodically syncs the
repository from its monorepo.

Sources:

- <https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/README.md>
- <https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/LICENSE>
- <https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/SOURCE_REV>

### `GROK_HOME` resolution

Grok resolves its state root in `crates/codegen/xai-dirs/src/lib.rs`.

- A nonempty `GROK_HOME` wins.
- Grok uses an explicit `GROK_HOME` verbatim. It does not canonicalize or require an absolute path.
- When `GROK_HOME` is empty or absent, Grok gets the operating-system home. On Unix, the source
  documents `HOME` with a password-database fallback. On Windows, it uses `USERPROFILE`.
- For the default only, Grok canonicalizes the home when possible and appends `.grok`.
- `grok_home()` creates the selected directory and caches the result for the life of the process.

Source:
<https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-dirs/src/lib.rs>

For an absolute explicit value, Cooper must preserve the exact effective `GROK_HOME` spelling that
the host process uses. This includes a custom root outside the host home. Cooper must set the same
value and mount the root at that same absolute target. A remapped target such as
`/home/user/.grok` is not equivalent for all Grok features.

A relative `GROK_HOME` is an edge case because Grok resolves it from its current working directory,
but Docker bind sources and Cooper mount specifications need absolute paths. Installed Grok 1.0.4
was run in an isolated directory with `GROK_HOME=relative-grok-state`. It created state below that
working directory. Cooper must not reinterpret this value as relative to the host home. The final
design must either resolve it from the exact host launch directory or reject it as unsupported with
a clear error. Docker and the VM must make the same decision.

### Native state surface

The complete Grok root contains more than authentication and sessions. The inspected source and
user guide place these items in that root:

- `config.toml`, `pager.toml`, `lsp.json`, and managed configuration.
- `auth.json` and `mcp_credentials.json`.
- `sessions`, `memory`, `skills`, `plugins`, and `agents`.
- `installed-plugins`, `plugin-data`, and `trusted-plugins`.
- Logs, trace exports, crash recovery, active-session state, worktrees, and downloaded bundles.
- Update downloads, managed binaries, and shell completions.
- The default leader socket and leader lock.

This confirms the existing Cooper rule: mount the complete effective Grok root read-write. Do not
copy or split its children. Keep the image-installed Grok executable outside that mount.

Sources:

- <https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-pager/docs/user-guide/05-configuration.md>
- <https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-pager/docs/user-guide/02-authentication.md>

### Session storage is relative to the current Grok root

Grok stores each session below:

```text
<GROK_HOME>/sessions/<encoded-absolute-cwd>/<session-id>/
```

The directory contains `summary.json`, `updates.jsonl`, `chat_history.jsonl`, and other JSON or
JSONL state. `updates.jsonl` is the authoritative conversation log.

Grok does persist the absolute working directory in two forms:

- A short working directory is URL-encoded into the parent directory name.
- A long working directory uses a slug and hash, with the original absolute value in `.cwd`.
- The serialized session `Info` object also contains `cwd: String`.

The JSONL adapter calculates the session directory from its current root and the stored working
directory. The relocation scanner starts at the current `<GROK_HOME>/sessions` tree. The inspected
ordinary session path does not use a database field that contains the old absolute `GROK_HOME`.

Result: a different `GROK_HOME` target does not by itself make a normal Grok session invisible. The
same absolute workspace path is still required because session grouping and resume use the working
directory. Cooper already maps the workspace to its host absolute path. That behavior must remain.

One user-guide table calls the session transcript format SQLite. The session guide and the current
storage source show per-session JSON and JSONL files. Grok uses SQLite for some indexes, but it does
not use SQLite as the authoritative conversation transcript in this inspected source.

Sources:

- <https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-config/src/paths.rs>
- <https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-shared/src/session/info.rs>
- <https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-shell/src/session/storage/jsonl/mod.rs>
- <https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-shell/src/session/storage/relocation/mod.rs>
- <https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-pager/docs/user-guide/17-sessions.md>

### Plugin state has confirmed absolute-path dependencies

Grok's managed plugin registry is not portable across a state-root remap.

`<GROK_HOME>/installed-plugins/registry.json` serializes these values:

- `InstalledRepo.path`: the absolute installed repository path.
- `InstallKind::Local.source_path`: the local source path for a copied plugin.

The in-memory registry root is not serialized, but the repository paths are. Plugin discovery reads
`repo.path`, appends an optional subdirectory, and requires that path to be a directory. Local
refresh reads both `source_path` and `repo.path` as filesystem paths. The inspected source has no
root-rebase step for either value.

For example, a plugin installed inside current Cooper state can record:

```text
/home/user/.grok/installed-plugins/example-12345678
```

The host sees the same file at a different path, such as:

```text
/home/ricky/.grok/installed-plugins/example-12345678
```

The registry lookup then misses the installed plugin. A registry written on the host fails in the
opposite direction inside current Cooper.

Plugin trust has the same path-identity requirement. Grok stores one canonical absolute plugin root
per line in `<GROK_HOME>/trusted-plugins`. It canonicalizes the current plugin path again for each
lookup. A different absolute prefix can make a prior grant fail. The plugin identifier also hashes
the canonical absolute plugin root. A changed root can therefore change the identifier and the
corresponding `<GROK_HOME>/plugin-data/<plugin-id>` directory.

This is a confirmed Grok portability defect in current Cooper's path-remapping policy. It affects
plugins and trust even though ordinary Grok session lookup is relative to the current Grok root.

Sources:

- <https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-agent/src/plugins/install_registry.rs>
- <https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-agent/src/plugins/git_install.rs>
- <https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-agent/src/plugins/local_refresh.rs>
- <https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-agent/src/plugins/discovery.rs>
- <https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-agent/src/plugins/trust.rs>

### Grok uses the user home outside `GROK_HOME`

`GROK_HOME` is not a complete home abstraction. Grok also reads these home-relative surfaces:

- `$HOME/.agents` for shared skills and commands. This root is always active.
- `$HOME/.claude` and `$HOME/.cursor` for compatible skills, rules, agents, plugins, hooks, MCP
  settings, and sessions. The compatibility cells are enabled by default in the inspected source.
- `CODEX_HOME`, or `$HOME/.codex`, for Codex foreign-session discovery.
- `CLAUDE_CONFIG_DIR`, or `$HOME/.claude`, for Claude foreign-session discovery.
- `$HOME/.gitignore` for a user ignore file.
- `$HOME/.zsh_history` or `$HOME/.bash_history` for shell-history features.
- Fish completion files below `$HOME/.config/fish` during a Grok update.
- User-configured `~` paths, including plugin sources and plugin install directories.

The installed Grok 1.0.4 `inspect --json` command was run with isolated default and custom homes.
It reported every Claude, Cursor, and Codex compatibility cell as enabled by default. No real host
agent state or credentials were used in this check.

This finding creates a policy question. `$HOME/.agents` is shared agent state and is needed for full
Codex and Grok behavior. Claude and Cursor roots are foreign compatibility inputs for Grok. Mounting
them would improve Grok parity but would conflict with Cooper's selected-agent isolation rule. Do
not silently choose one side or change Grok compatibility settings. Resolve this conflict as an
explicit Cooper requirement.

Sources:

- <https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-agent/src/prompt/skills.rs>
- <https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-agent/src/prompt/agents_md.rs>
- <https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-agent/src/plugins/discovery.rs>
- <https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-foreign-sessions/src/codex/mod.rs>
- <https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-foreign-sessions/src/claude.rs>
- <https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-agent/src/prompt/ignore.rs>

### Child processes receive the barrel home

Grok's default shell environment policy inherits the complete process environment. Even its
restricted `core` mode retains `HOME`, `USER`, `LOGNAME`, `SHELL`, and `PATH`. Terminal setup can
load shell startup files from the current home. A container-only home therefore affects commands,
MCP servers, hooks, package managers, Git, SSH, and user scripts that Grok starts.

Source:
<https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-tools/src/util/shell_env_policy.rs>

### Leader transport must stay isolated

By default, Grok places `leader.sock` and `leader.lock` in `GROK_HOME`. A nonempty
`GROK_LEADER_SOCKET` moves the socket and puts the lock beside it. These files coordinate live
processes. They are not portable user state.

Current Cooper sets the leader socket to `/tmp/cooper-grok-leader.sock`. Its per-barrel `/tmp` mount
keeps the socket and lock separate from the shared host root. This design is correct and must remain.
It prevents a barrel from attaching to a host Grok process through shared state.

Source:
<https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-shell/src/leader/lock.rs>

### Update behavior is only partly remap-safe

The Grok updater has an explicit Docker compatibility measure. It uses relative links between files
inside the Grok root so a managed binary link can survive a bind mount at a different home prefix.
The source comment names this case directly. The updater still resolves fish completions from the
separate user home.

This relative-link measure does not rebase the plugin registry or plugin trust paths. It is evidence
that Grok supports some root remapping, not evidence that all Grok state is portable.

Source:
<https://github.com/xai-org/grok-build/blob/72a61251fcffb464bcc687aeb5a998e5a98ec0c9/crates/codegen/xai-grok-update/src/auto_update.rs>

## OpenCode Source Audit

### Repository status

OpenCode is open source under the MIT license. The inspected official repository uses `dev` as its
current branch.

Sources:

- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/README.md>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/LICENSE>

### OpenCode has several state roots

OpenCode does not have one equivalent of `CODEX_HOME` or `GROK_HOME`. It imports four values from
`xdg-basedir` 5.1.0 and appends `opencode`:

| Purpose | Environment override | Linux default |
| --- | --- | --- |
| Data | `XDG_DATA_HOME` | `$HOME/.local/share/opencode` |
| Configuration | `XDG_CONFIG_HOME` | `$HOME/.config/opencode` |
| State | `XDG_STATE_HOME` | `$HOME/.local/state/opencode` |
| Cache | `XDG_CACHE_HOME` | `$HOME/.cache/opencode` |

OpenCode also uses:

```text
$HOME/.opencode
<operating-system temporary directory>/opencode
```

The four XDG values are calculated when the global module loads. `OPENCODE_TEST_HOME` changes the
reported test home but does not change the already calculated XDG roots. It is a test hook, not a
production state-root variable.

`xdg-basedir` uses a nonempty XDG value verbatim and does not enforce the XDG requirement that these
values be absolute. An isolated OpenCode 1.17.18 check with relative XDG values reported paths such
as `relative-data/opencode` and created the roots relative to its working directory. Cooper must not
silently resolve such a value against the host home. It must either reproduce the exact
working-directory meaning or reject the value with a clear error and documented support rule.

`OPENCODE_CONFIG_DIR` adds a custom configuration directory in the configuration loader and changes
the injected configuration service value. It does not replace every static `Global.Path.config`
use. `OPENCODE_CONFIG` can name one config file. `OPENCODE_DB` can name an absolute database, use
`:memory:`, or name a file relative to the data root.

Current Cooper only mounts the five default Linux home paths. It does not calculate host XDG roots
or custom OpenCode paths. A host that sets XDG variables or `OPENCODE_DB` can therefore use state
that is absent in Cooper. The right fix must resolve the effective host values first, mount each
authorized root at the same absolute target, and set the same path variables in the barrel.

Sources:

- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/core/src/global.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/core/src/flag/flag.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/opencode/src/config/paths.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/opencode/src/config/config.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/core/src/database/database.ts>
- <https://github.com/sindresorhus/xdg-basedir/blob/8cceade858e4da18cb971bf1844f086e9e213563/index.js>

### Runtime XDG verification

The exact `xdg-basedir` dependency was imported with an isolated `HOME=/tmp/host-home` and no XDG
variables. It returned:

```text
data=/tmp/host-home/.local/share
config=/tmp/host-home/.config
state=/tmp/host-home/.local/state
cache=/tmp/host-home/.cache
```

A second import set all four XDG variables to `/srv/...`; each explicit value won. The installed
OpenCode 1.17.18 `debug paths` command produced the same behavior in isolated `/tmp` homes. It also
confirmed that OpenCode uses `/tmp/opencode` for transient files on this host.

### Database and session path behavior

OpenCode stores its current SQLite database in the data root unless `OPENCODE_DB` changes it. The
production filename is `opencode.db`; development channels can use a channel-specific filename.

The SQLite schema deliberately stores absolute paths:

- `session.directory` is a required absolute path for new sessions.
- `project.worktree` is a required absolute path.
- `project.sandboxes` is an array of absolute paths.
- `project_directory.directory` is a required absolute path.
- `session.path` is relative to the project worktree when available.

The database path codec rejects nonabsolute values for fields that require an absolute path. Its
path migration only converts Windows separators to `/`. It does not replace a home or state-root
prefix.

Session creation writes the current instance directory. Session listing can filter by exact
`session.directory`. Project discovery writes the current worktree and sandbox paths. OpenCode
therefore requires the same workspace path across the host, Docker, and the VM. Cooper already
provides this path parity. It must not change that design while it fixes home parity.

Sources:

- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/core/src/database/database.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/core/src/database/path.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/core/src/session/sql.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/core/src/project/sql.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/core/src/database/migration/20260601010001_normalize_storage_paths.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/opencode/src/session/session.ts>

### Full tool-output paths are not portable across a data-root remap

OpenCode writes large tool output below:

```text
<XDG data root>/opencode/tool-output/
```

Both current message paths do the following:

1. Write the complete output to a file in that directory.
2. Return the complete absolute filename.
3. Put that filename in the visible truncation marker.
4. Persist it as `outputPath` or `outputPaths` in tool-call state.

When current Cooper maps a host data root to `/home/user/.local/share/opencode`, a barrel tool call
can persist `/home/user/.../tool-output/...`. The host transcript and SQLite row still exist, but the
host cannot open the saved complete output through that path. The reverse direction has the same
fault. This is a confirmed OpenCode feature-level portability defect in current Cooper.

It is less severe than the Codex rollout fault because it does not make the full session transcript
unavailable. It makes persisted full-output references stale immediately after a boundary change.
OpenCode also deletes managed full-output files after its normal seven-day retention period; that
expected cleanup does not explain an immediate path failure.

Sources:

- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/opencode/src/tool/truncation-dir.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/opencode/src/tool/truncate.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/opencode/src/tool/tool.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/opencode/src/tool/registry.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/core/src/tool-output-store.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/schema/src/session-message.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/core/src/session/message-updater.ts>

### Current OpenCode state evidence

A read-only query of `/home/ricky/.local/share/opencode/opencode.db` on 2026-09-06 found:

| Stored value | Total | `/home/user` | `/home/ricky` | Other |
| --- | ---: | ---: | ---: | ---: |
| Session directory rows | 621 | 0 | 621 | 0 |
| Project worktree rows | 9 | 0 | 8 | 1 |
| Project-directory rows | 4 | 0 | 4 | 0 |

This confirms that the exact host workspace mount preserves the main session and project keys on
this machine.

The same query parsed tool-call JSON without printing transcript content. It found 73 unique,
standalone persisted tool-output paths:

| Tool-output prefix | References | Files that exist now |
| --- | ---: | ---: |
| `/home/user/...` | 8 | 0 |
| `/home/ricky/...` | 63 | 3 |
| Other | 2 | 0 |

The eight `/home/user` values use the exact current Cooper data-root target. None resolves on the
host. The database does not store writer-process provenance, so this count alone cannot name the
process that wrote each row. The current Cooper mount target and the confirmed OpenCode write path
together explain how these values are produced.

Most old host-path output files are also absent because OpenCode has a seven-day retention rule.
That expected cleanup is separate from the prefix defect. A cross-boundary regression test must
check a new output reference before retention cleanup can run.

### State-root contents

The source places these important values in the OpenCode roots:

- Data: authentication, MCP OAuth state, SQLite, legacy JSON storage, snapshots, worktrees, cloned
  repositories, plans, logs, and saved full tool output.
- Configuration: global config, plugins, agents, commands, themes, package files, and installed
  configuration dependencies.
- State: selected-model state and plugin metadata.
- Cache: downloaded helper binaries, ripgrep, language servers, and skill cache.
- Temporary root: process-temporary files under `opencode`.

This supports mounting each complete effective OpenCode root. It does not support mounting only one
database file or selected children.

Representative sources:

- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/opencode/src/auth/index.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/opencode/src/mcp/auth.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/opencode/src/storage/storage.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/opencode/src/snapshot/index.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/opencode/src/worktree/index.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/opencode/src/plugin/meta.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/core/src/ripgrep/binary.ts>

### OpenCode uses the operating-system home directly

OpenCode also calls `os.homedir()` outside its XDG root setup. Confirmed uses include:

- `$HOME/.opencode` configuration discovery.
- `~` expansion in configuration variables, prompt attachments, permissions, and shell path
  analysis.
- Shell startup through `.zshenv`, `.zshrc`, and `.bashrc`.
- Visual Studio Code extension search below `.vscode`, `.vscode-insiders`, `.vscode-server`, and
  `.vscode-server-insiders`.
- The default `DOTNET_CLI_HOME` for one language-server setup.
- Install and uninstall paths.

The TUI copies the complete current environment into its worker. The shell tool starts commands with
`process.env` plus plugin additions. Other process helpers also use environment inheritance. Thus,
the container `HOME`, `USER`, `LOGNAME`, `SHELL`, and XDG values affect tools that OpenCode starts.

Sources:

- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/core/src/shell.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/opencode/src/tool/shell.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/opencode/src/permission/index.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/opencode/src/session/prompt.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/opencode/src/lsp/server.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/opencode/src/cli/cmd/tui.ts>
- <https://github.com/anomalyco/opencode/blob/337fd144d2ba144743368f78d9579a99cce175bd/packages/core/src/cross-spawn-spawner.ts>

### OpenCode binary placement

OpenCode's installer can put its binary below the home, including `$HOME/.opencode/bin`. Current
Cooper installs its image copy at `/home/user/.local/bin` and places that directory before
`/home/user/.opencode/bin` in `PATH`. This keeps a mounted host `.opencode` root from hiding the
reviewed image binary.

A future projected home must keep this protection. Prefer a clear Cooper-owned binary directory,
such as `/opt/cooper/bin`, over a directory below the projected host home. Do not allow an OpenCode
state mount to select or replace the image binary.

## Comparative Result

The three agents confirm one general problem class, with different user-visible failures:

| Agent | Persisted absolute path | Effect of current `/home/user` remap |
| --- | --- | --- |
| Codex | SQLite `threads.rollout_path` | A session rollout can become unavailable to list or resume. This failure is confirmed in the current host database. |
| Grok Build | Session working directory; installed-plugin paths; local-plugin sources; plugin trust keys | Normal session lookup works only while the workspace path matches. Installed plugins, refresh, trust, identifiers, and plugin data can fail across the boundary. |
| OpenCode | Session directory; project worktree and sandboxes; complete tool-output file paths | Session grouping works only while the workspace path matches. The local database has eight saved output paths with the current Cooper prefix, and none resolves on the host. |

This evidence changes the status of the issue. It is not only a possible risk for other agents.
Absolute-path incompatibility is confirmed in source for Codex, Grok Build, and OpenCode. The exact
severity and affected feature differ by agent.

## Architecture Conclusion

The root cause is that Cooper shares host-owned bytes but changes the logical names of those bytes.
It also gives the agent a different home and account context. When an agent persists an absolute
path or expands a home-relative path, the shared state points into a namespace that exists on only
one side of the boundary.

Matching the host user name alone is not the root fix. The name `ricky` does not guarantee a home of
`/home/ricky`, and programs can read paths from `HOME`, XDG variables, agent variables, the password
database, configuration, or prior persistent records.

Setting only `CODEX_HOME` is also not the root fix. It can prevent the known Codex rollout defect if
the mount target has the same value, but it does not cover Codex shared skills, Grok plugins,
OpenCode XDG roots, home-relative configuration, or child processes.

The strongest current design direction is a host path projection:

1. Resolve the logical host home without deriving it from the user name.
2. Create a barrel-local writable home skeleton at that exact absolute path.
3. Set `HOME` to that exact path and make the barrel password-database entry agree.
4. Set `USER` and `LOGNAME`, and match the account name when it is valid and does not conflict.
5. Resolve every selected-agent root from the host environment with the agent's own rules.
6. Mount each complete host-owned root read-write at the same absolute path.
7. Set and protect the matching agent and XDG path variables in the barrel.
8. Mount only approved child paths. Never mount the complete host home.
9. Keep the exact host workspace path.
10. Keep Cooper binaries, caches, clipboard files, and other runtime data outside projected
    host-owned roots.
11. Keep live transports, such as the Grok leader socket, in per-barrel transient storage.
12. Apply the same path and environment model in Docker and the VM.

The mount plan remains the correct policy boundary. Its data model must describe path identity,
ownership, environment bindings, and transient exceptions instead of translating all user paths to
`/home/user`.

### Candidate shared resolution model

Docker and the VM must not resolve host variables independently. The host Cooper process should
resolve one immutable workload context and give the same context to both back ends:

```text
host account + host environment + host launch directory
                         |
                         v
              resolve agent context once
                         |
             +-----------+-----------+
             |                       |
             v                       v
       Docker mount/env args   VM metadata and mounts
```

A possible internal model is:

```go
type ResolvedUserContext struct {
    LogicalHome string
    UserName    string
    UID         int
    GID         int
    Environment []EnvVar
    Mounts      []MountSpec
}

type MountSpec struct {
    Source       string
    Target       string
    LogicalPath  string
    ResolvedPath string
    Ownership    Ownership
    Access       Access
}
```

The names are only examples. The important separation is this:

- `LogicalPath` is the path spelling that the agent sees, stores, and receives in environment
  values.
- `ResolvedPath` is the symlink-resolved path that Cooper uses for overlap and deletion-safety
  checks.
- `Source` is the host filesystem object.
- `Target` must equal the required agent-visible logical path for host-owned state.
- Ownership controls creation and cleanup. Cooper cleanup must never cross into `HostState`.

The host resolver should produce the parent home-skeleton mount before child state and workspace
overlays. Both renderers should consume this ordered result. The VM metadata must contain the
resolved values instead of asking the guest to read its own environment. This design removes the
current relative-`GROK_HOME` difference between Docker and the VM.

The final implementation can use different Go types. Keep one source of truth and test that both
renderers receive equivalent paths and environment values.

## Other Agent Risk

The same problem class can affect another agent when it does any of the following:

- Stores an absolute path in shared state.
- Uses `HOME`, a password-database home, or a user name outside its documented state root.
- Uses XDG variables or XDG default directories.
- Expands `~` in a shared configuration file.
- Stores plugin, skill, memory, or marketplace data outside its primary state root.
- Starts child processes that inherit a container-only home.
- Stores absolute paths to workspaces, extensions, commands, sockets, or cached assets.

This is now confirmed for Codex, Grok Build, and OpenCode. It remains a risk for an unaudited agent
until source inspection or runtime evidence confirms its behavior.

## Source Audit Method for Each Agent

For each open-source agent, record the exact repository and commit before drawing conclusions.

Inspect at least these path inputs:

- `HOME`, `USER`, `LOGNAME`, and password-database calls.
- `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, and `XDG_CACHE_HOME`.
- Agent-specific home and state variables.
- Literal `.config`, `.local`, `.cache`, and agent directory names.
- Tilde expansion.
- Session and conversation persistence schemas.
- SQLite, JSON, JSONL, and other indexes that store path strings.
- Auth, memory, skill, plugin, hook, and command locations.
- IPC sockets, lock files, and process discovery state.
- Paths passed to child processes.
- Update and binary-install locations.

Classify every result as one of:

1. Host-owned persistent state that Cooper must mount.
2. Cooper-owned cache that can use an explicit stable path.
3. Per-barrel transient state that Cooper must isolate.
4. Host path reference that must keep the same absolute name.
5. Display-only behavior with no persistence effect.

Do not infer a complete root list from documentation alone. Verify it in source and, when practical,
with an isolated runtime trace.

## Required Compatibility Tests

The final design needs tests for both `cooper cli` and `cooper vm`.

### User identity

- Linux home `/home/alice`.
- macOS-style home `/Users/alice` in a Linux container.
- A nonstandard home such as `/srv/users/alice`.
- A home path that contains spaces.
- Different user name and home basename.
- Host UID and GID values other than 1000.
- A host group name or user name that conflicts with an image account.
- An invalid Linux account name from a supported host.
- A home with existing symlink components.

### State roots

- Default agent state root.
- Explicit custom state root.
- Empty agent-specific environment value.
- Relative agent-specific environment value when the agent accepts or rejects it.
- State root with symlink components.
- State root outside the host home.
- State root that overlaps the Cooper directory in either direction.
- Shared cross-agent roots such as `.agents`.
- Selected-agent isolation: unrelated agent roots must remain absent.

### Cross-boundary lifecycle

- Create on host, resume in Docker, resume on host.
- Create in Docker, resume on host, resume in VM.
- Create in VM, resume on host, resume in Docker.
- List and picker behavior, not only direct resume by ID.
- Archive, unarchive, fork, revert, and compact where supported.
- Skills, plugins, hooks, MCP servers, memories, and custom commands.
- Home-relative paths in configuration.
- Child commands that print and use `HOME`, `USER`, and `LOGNAME`.
- No new container-only path prefix in host-owned state.

### Codex regressions

- Create a session on each boundary and assert that every new SQLite `rollout_path` starts with the
  effective host `CODEX_HOME`.
- Resume through the picker and by thread ID after each boundary change.
- Test a thread with more than one rollout after revert. Do not accept a filename-only repair that
  selects the wrong rollout.
- Test default, explicit, empty, symlinked, and outside-home `CODEX_HOME` values.
- Confirm that `$HOME/.agents/skills` has the same contents and path in each boundary.
- Confirm that personal marketplace and `~` configuration paths resolve to the projected home.

### Grok Build regressions

- Create on the host, resume in Docker, resume in the VM, and return to the host for both short and
  long working-directory names.
- Assert that the encoded session directory and `.cwd` metadata always contain the host workspace
  path.
- Install one remote test plugin and one local test plugin on each boundary. Discover, reload,
  refresh, and remove them after every boundary change.
- Assert that every `InstalledRepo.path`, local `source_path`, and `trusted-plugins` entry uses a path
  that exists with the same name in all boundaries.
- Assert that a plugin keeps the same plugin identifier and plugin-data directory after a boundary
  change.
- Test default, empty, custom, outside-home, and symlinked `GROK_HOME` values.
- Assert that `leader.sock` and `leader.lock` remain in the per-runtime temporary mount and never
  connect a barrel to a host leader.
- Test `$HOME/.agents` behavior. Add a separate policy test for disabled or enabled Claude, Cursor,
  and Codex compatibility roots after that policy is decided.
- Run a child shell command that prints its home and identity values.

### OpenCode regressions

- Run `opencode debug paths` on the host and in both Cooper boundaries. Require identical home,
  data, configuration, state, and cache paths, except for the intentionally isolated temporary root.
- Test default and explicit values for all four XDG variables.
- Test absolute and data-relative `OPENCODE_DB` values. Require the selected database path to exist
  with the same name in all boundaries.
- Create and list a session on every boundary. Assert that session directory, project worktree,
  project directory, and sandbox values use the exact host workspace paths.
- Produce a tool result large enough to create a saved full-output file. Resume on each boundary and
  open the persisted `outputPath` or every `outputPaths` value.
- Test global config, `$HOME/.opencode`, `OPENCODE_CONFIG`, and `OPENCODE_CONFIG_DIR`.
- Verify authentication, MCP authentication, plugins, agents, themes, model state, and downloaded
  helper tools through a boundary change.
- Run a child shell command that prints `HOME`, all XDG variables, `USER`, and `LOGNAME`.
- Assert that the image-installed OpenCode binary wins before any executable in mounted host state.

### Safety

- The complete host home is never mounted.
- Only selected-agent state roots are mounted.
- Host state mounts remain read-write unless a file is explicitly host configuration.
- Cooper cleanup does not delete or change host state roots.
- Cooper-owned cleanup cannot traverse into a host state root.
- Image-installed binaries stay visible.
- Parent and child mounts have deterministic order.
- Docker and VM manifests reject duplicate or unsafe targets.

### Existing Cooper gates

After implementation, run all required Cooper verification from the repository root:

```sh
go test -C ./cooper ./... > /tmp/cooper-go-test.txt 2>&1
timeout 90m ./cooper/test-e2e.sh > /tmp/cooper-e2e.txt 2>&1
timeout 90m ./cooper/test-docker-build.sh all > /tmp/cooper-docker-build.txt 2>&1
go build -C ./cooper -o ./cooper . > /tmp/cooper-build.txt 2>&1
```

Add focused tests before these full gates. Do not treat a direct-resume fallback as proof of state
interchangeability. Verify the stored path strings and the session picker.

## Open Questions

- Should Cooper use one projected host identity for every agent? The three audited agents all have
  confirmed home-based behavior, so a tool-specific identity model now has little evidence in its
  favor.
- Should the barrel password-database user name always match the host, or should Cooper use a stable
  internal name with the host home path?
- How should Cooper handle a host user name that Linux `useradd` rejects or that conflicts with an
  image account?
- Should a barrel-local home skeleton be an image directory, a container-local writable layer, or a
  Cooper-owned per-runtime mount?
- Which shared roots below `.agents`, `.claude-plugin`, or `.cursor-plugin` belong to Codex's complete
  supported state surface?
- Does `$HOME/.agents` count as shared selected-agent state for both Codex and Grok?
- How should Cooper reconcile Grok's default foreign-agent compatibility with the rule that a
  selected-agent barrel must not mount another agent's state root?
- Which XDG variables must Cooper preserve as host state roots, and which Cooper caches must use
  separate explicit locations?
- Must Cooper support `OPENCODE_DB`, `OPENCODE_CONFIG`, and `OPENCODE_CONFIG_DIR` when they point
  outside the standard OpenCode roots?
- Which spelling is authoritative when a host home or explicit state root contains a symlink:
  logical environment spelling or canonical filesystem spelling? The answer must follow each
  agent's own resolution rule.
- Will Cooper support relative `GROK_HOME` and relative XDG values by resolving them from the host
  launch directory, or reject them because they cannot define stable cross-boundary roots?
- Should Cooper include a guarded Codex state repair command for existing stale rollout rows?
- Should repair tooling also rebase known Cooper-created Grok plugin registry and trust entries, and
  OpenCode saved-output references?
- Can one general state-root descriptor replace the current tool-specific path branches without
  making agent behavior less explicit?

## Next Work

No implementation is approved by this document. Continue in this order after the architecture
questions above have answers:

1. Write one path-projection design for Docker and the VM. Include user identity, parent home
   creation, child overlays, custom roots, symlinks, XDG values, selected-agent isolation, and
   Cooper-owned paths.
2. Define an agent-root descriptor for Codex, Grok, and OpenCode. Keep each agent's resolution rules
   explicit even if the mount mechanism is general.
3. Decide the shared `.agents` and Grok foreign-compatibility policy.
4. Design guarded inspection and repair commands for state that current Cooper already wrote with a
   `/home/user` prefix. Do not mutate state during normal startup.
5. Add focused unit and integration tests from this document.
6. Run the full Cooper test and Docker-build gates.
7. Audit Claude Code and GitHub Copilot CLI next. If complete source is not available, use isolated
   homes, filesystem traces, process environments, and fixture state. Mark runtime inference
   separately from source-confirmed behavior.

## Investigation Verification

Completed read-only or isolated checks:

- Recounted the current Codex SQLite path groups and checked file existence.
- Queried aggregate OpenCode SQLite path groups without printing authentication or transcript
  content.
- Read the pinned Codex, Grok Build, OpenCode, and `xdg-basedir` source listed above.
- Imported `xdg-basedir` 5.1.0 under isolated default and explicit XDG environments.
- Ran installed OpenCode 1.17.18 `debug paths` under isolated default and explicit XDG homes.
- Ran installed Grok 1.0.4 `inspect --json` and `du` with isolated default and custom Grok homes.
- Confirmed with installed binaries that Grok and OpenCode accept relative root variables and
  resolve them from the working directory.
- Confirmed that Grok reported its Claude, Cursor, and Codex compatibility cells as enabled by
  default.

The host does not have `cargo` or `rustc`, so `cargo test -p xai-dirs` could not run. The source
contains focused tests for the root-resolution behavior, but they were not executed in this
investigation. No claim in this document depends only on an unexecuted test: each root rule is also
visible in the production path code, and the installed binary checks support the stable behavior.

The current host Grok root has no `installed-plugins/registry.json` or `trusted-plugins` file, so a
local Grok prefix count was not possible. This investigation did not install a real plugin or change
host Grok state. The Grok plugin finding comes from production read, write, discovery, refresh, and
trust code, plus its focused source tests.

Only this Markdown document changed for this exploration. Full Cooper tests and image builds are not
needed for a documentation-only change. Run them when an implementation changes Cooper behavior.
