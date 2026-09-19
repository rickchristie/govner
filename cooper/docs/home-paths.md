# Account and state paths

## Why paths must match

Agent state contains absolute paths. Codex stores rollout paths in SQLite;
Grok uses workspace and plugin paths in session and trust records; OpenCode
stores paths to snapshots, worktrees, and tool output. A mount that changes
`/home/ricky/.codex` to `/home/user/.codex` exposes the files but can leave
these records pointing at files that do not exist.

Cooper therefore preserves the host account home, workspace path, selected
state paths, and path environment in both `cooper cli` and `cooper vm`.
Existing records are not rewritten. Old records that already refer to the
former `/home/user` layout need separate recovery; changing paths in a live
agent database is not part of image build or launch.

## Build and launch inputs

`internal/usercontext` reads the process UID and GID and their account names
from the host account database. It reads the logical home with Go's
`os.UserHomeDir`. It does not infer a home from a username or use `USER` and `LOGNAME` as account authority.

`cooper build` passes that record to the base image. The image creates the
account with the same name, group, IDs, and home and sets matching `HOME`,
`USER`, and `LOGNAME`. Cooper provides Bash as the workload shell. Linux
homes, macOS `/Users/...` homes, and homes whose final component differs
from the account name use the same rule. System groups retain their numeric
ownership when their names conflict with the host group. A conflicting
image user causes a build error.

The image label `cooper.account` records the build input. Launch checks it
before mounting host data and asks for `cooper build` when an image is old
or belongs to another account. `cooper up` does not rewrite image accounts.
This keeps account setup in one build step. Sharing one image between
different Linux accounts without a rebuild is outside this design.

Selected-agent paths are launch inputs. A supported path override does not
require another image. Both execution modes resolve the same mount list
and environment. Reuse includes the image, mounts, and environment, so a
new root or changed override cannot silently reuse a previous state view.

## The shared root list

`internal/workload/agentpaths.go` is the runtime root list. Each entry states
its base rule, relative path, file or directory kind, and whether it is
optional. The resolver produces `MountSpec` values with separate source and
target fields. Ordinary directory roots use the host paths. Named profiles
and live profiles prepared by Linux builds use private fixed sources and keep
the original targets. See [Live profiles](managed-profiles.md).

| Agent | Roots |
| --- | --- |
| Claude | `CLAUDE_CONFIG_DIR` or `~/.claude`; existing `~/.claude.json` |
| Copilot | `COPILOT_HOME` or `~/.copilot`; effective Copilot cache; existing XDG migration roots |
| Codex | `CODEX_HOME` or `~/.codex`; `~/.agents`; `~/.claude-plugin`; `~/.cursor-plugin` |
| OpenCode | Each effective XDG root plus `/opencode`; `~/.opencode`; configured extra paths described below |
| Grok | `GROK_HOME` or `~/.grok`; `~/.agents` |
| Antigravity | Complete `~/.gemini`; existing supported Google ADC directory |

All selected roots are host-owned and mounted read-write, including auth,
settings, sessions, memory, plugins, and future children. `.agents` is shared
state for Codex and Grok. Grok's optional foreign-session search does not
grant it access to other agents' private roots. Selecting Grok does not
mount `.claude`, `.cursor`, or `.codex`.

To support a new directory in an agent release, add an entry to this list
and a behavior test. Do not add a second mount switch in Docker or VM code.
New children inside an existing root need no change. The image catalog's
home directories are installation preparation, not runtime mount policy.

### Path override rules

- An absent `CLAUDE_CONFIG_DIR` uses `~/.claude`. An explicitly empty value
  uses the launch directory. Cooper preserves that distinction and follows
  Claude's Unicode normalization of the config directory. With a nonempty
  custom root, the global Claude JSON file is inside that root.
- Copilot uses `COPILOT_HOME` or `~/.copilot`. Its separate cache uses
  `COPILOT_CACHE_HOME`, otherwise `~/Library/Caches/copilot` on macOS or
  `${XDG_CACHE_HOME:-~/.cache}/copilot` on Linux. Cooper gives the Linux
  workload the effective host cache path. Existing `.copilot` children of
  explicitly set XDG config and state roots are also shared when Copilot's
  own migration can read them. A custom `COPILOT_HOME` disables that migration.
- An empty `CODEX_HOME` uses the logical home plus `.codex`. An explicit
  value must name an existing directory. Cooper resolves a relative value
  from the launch directory and follows symlinks, as Codex does.
- An explicit `GROK_HOME` keeps its exact environment value. A relative
  value is relative to the workspace where Cooper starts. The default uses
  the resolved home when available, then `.grok`. Grok leader transport
  stays at `/tmp/cooper-grok-leader.sock` in each workload.
- OpenCode uses `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, and
  `XDG_CACHE_HOME`, with the normal home defaults. Cooper shares only their
  `opencode` children. Relative XDG values retain their working-directory
  meaning. `OPENCODE_CONFIG_DIR` adds a complete directory;
  `OPENCODE_CONFIG` adds an existing file.
- `OPENCODE_DB=:memory:` adds no mount. A database below the data root is
  already covered. A database outside that root must exist before launch;
  Cooper mounts its containing directory so SQLite journal, WAL, and shared
  memory files stay together. Use a directory that contains only the
  selected OpenCode database and its related files. A path that would export
  the complete home or overlap Cooper runtime data is rejected.

Mount and cleanup checks examine direct paths and resolved symlinks. Ordinary
state cannot overlap the Cooper directory. Build's live-profile conversion
permits only exact registered aliases to private profile roots. State cannot
replace protected workload system paths or expose the complete host home.
Symlinks inside a selected root do not grant access to arbitrary external
targets; those targets need their own supported root or workspace mount.

## Private runtime data

The complete host home is never mounted. A private home below
`~/.cooper/tmp/{runtime}/.cooper-home` supplies writable shell defaults and
application scratch files at the build account's home path. Workspace and
selected state mounts overlay it. Cooper cleanup can remove this private
home but must not remove or change selected host state.

Image binaries use `/opt/cooper/bin`, `/opt/cooper/npm`, and
`/opt/cooper/python`. Language caches, fonts, clipboard authentication, and
browser downloads use explicit Cooper paths below `/var/lib/cooper` or
`/go`. Host XDG settings cannot move these files into agent state. A mounted
host installer directory cannot hide the image's selected CLI version.

The VM uses the same home and mount targets. Its Docker socket belongs to
the guest. Its `$HOME/.cooper` and `$HOME/.docker` contain private runtime
data; neither is the physical host's configuration directory. Nested image
builds trust the outer public Cooper CA before network downloads start.
They still use the outer proxy and have no direct network route.

## Account profiles

A named profile selects complete roots from this same catalog. Copy-mode
stores use independent snapshots; converted stores use live directories.
It preserves the built account, mount targets, path environment, and absolute
paths in stored records. Runtime identity includes the profile ID; a new saved
generation changes the mount digest. Named sessions use their captured
credential environment and never fall back to live host credentials.

Profile roots use a separate durable ownership class. Their sources must be
validated private profile storage; their targets retain the normal state-path
checks. Ordinary managed sessions resolve registered host aliases to fixed
profile sources. Each recorded physical root path also receives the same
selected root. This permits native clients to open stored canonical session
paths without mounting the profile index, selector, parent store, or another
account. Read-only hook overlays apply to each alias. See
[Account profiles](profiles.md) for save/load policy and recovery.
