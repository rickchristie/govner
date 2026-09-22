# Cooper technical guide

Setup and all user workflows are in [README.md](README.md). This file holds
implementation contracts and development checks, not dated test results.

## Architecture

[internal/app](internal/app/) is the application boundary. TUI screens call
small interfaces and receive typed results. [internal/workload](internal/workload/)
defines the shared launch and mount policy. Docker and VM backends consume
that policy; they must not maintain separate agent-root lists.

CLI and VM sessions preserve the same host account, absolute workspace, state
targets, image versions, environment, proxy rules, clipboard, ports, and bridge.
Only the execution boundary differs. CLI uses a Docker container; VM uses a
guest with its own Docker daemon.

Images contain the invoking host login name, primary group, UID, GID, and home.
[internal/usercontext](internal/usercontext/) uses account records, not
`USER` or `LOGNAME` as identity authority. The `cooper.account` image label
is checked at launch. A changed OS account requires a rebuild; selecting an
AI profile does not.

## Host state and mount policy

[internal/workload/agentpaths.go](internal/workload/agentpaths.go) is the root
catalog for ordinary sessions, named profiles, both backends, and cleanup
protection. Mount complete selected roots read-write. New children then work
without a Cooper release. Do not copy only known auth or history files.

| Agent | Complete roots and path rules |
| --- | --- |
| Claude | `CLAUDE_CONFIG_DIR` or `~/.claude`; optional global config file. Explicit empty override means the workspace, unlike an unset value. |
| Codex | `CODEX_HOME` or `~/.codex`; `~/.agents`, `~/.claude-plugin`, and `~/.cursor-plugin`. Explicit CODEX_HOME must exist and is canonicalized. |
| Copilot | `COPILOT_HOME` or `~/.copilot`, effective cache root, and legacy XDG roots only when their variables are explicit and COPILOT_HOME is absent. |
| OpenCode | Effective XDG config/data/state/cache roots with `/opencode`; `~/.opencode`; explicit config directory/file and custom database parent. Mount the database parent for WAL files; `:memory:` adds no root. |
| Grok | Non-empty `GROK_HOME`, otherwise `~/.grok`; shared `~/.agents`. Preserve the effective host setting, including relative-path semantics. |
| Antigravity | Complete `~/.gemini`; ordinary ADC sessions also use the credential-file parent or default `~/.config/gcloud`. |

Resolve path overrides at launch from the same workspace and host environment.
Do not rewrite absolute paths in native session databases. Keep public targets
unchanged. Do not expose other agents' private roots for session discovery.
A child symlink does not grant access to a new outside root.

Validate direct paths and paths after existing links resolve. Refuse whole-home
sources, protected sources, and overlaps with Cooper state. A Grok root and
Cooper's configuration directory must not contain each other. Named profiles
also require exact registered root ownership.

The workspace is read-write. Git configuration and `.git/hooks` are read-only.
Protect hooks through every workspace and selected-state path that reaches
them. The VM supervisor must apply the same limits before exporting a root;
guest-side overlays alone cannot protect against guest root mounting a tag
again. Missing normal hook directories can be created; linked `.git` or hook
paths are refused.

A private runtime home overlay holds workload-owned `.cooper` and `.docker`,
not the physical host's directories. Image-installed binaries live under
`/opt/cooper`, outside mounted state. Runtime caches under `/var/lib/cooper`
and `/go` do not depend on a selected account's XDG roots.

Grok leader transport uses `/tmp/cooper-grok-leader.sock` in each runtime's
private temporary mount. This prevents attachment to a host or another
runtime's leader. Cooper sets no behavior-related `GROK_*` values and supplies
no requirements file. Preserve host behavior settings.

## Profiles

Profiles are Linux-only. There is no migration from the old copy or symlink
formats. Reject those stores and leave their data intact. Build and Save &
Build have no profile setup step.

### Storage and account binding

The active root stays at its public path. Each inactive root is a sibling
named `<root>.cooper-<24-hex-profile-id>`. Each move stays in that root's parent,
so roots can be on different filesystems without a cross-filesystem copy.
Metadata uses schema 3 under `~/.cooper/profiles`: `index.json`, private
`credentials/` records, and an operation journal when needed.

Use Linux `renameat2(RENAME_NOREPLACE)` with no copy fallback. Refuse unsupported
filesystems, root links, root/nested mount points, overlaps, replaced parents,
and a current working directory inside affected state. Record parent and
directory identities; an unexpected directory replacement must not be adopted.

Standalone files use the same journal. Preserve optional absence. An app can
atomically replace a standalone file, so check its current entry at the switch
rather than require the original file inode forever. Normal save/load checks
bounded metadata and roots, not history contents.

First save registers Default in place and creates missing directory roots.
It does not copy history. A later save verifies the mapped account and captures
supported credential variables. Load a new empty selection before changing
accounts. Unknown local identity can leave a pending selection with its state
retained; a named runtime requires a verified binding. Never let a supplied
name choose an existing save destination.

`~/.agents` is shared host state, not an account root. Profiles do not rotate,
back up, restore, or delete it. Shared account roots among other tools follow
the last applicable load; partial selections must remain visible.

### Identity and environment

[internal/profileauth](internal/profileauth/) reads bounded local auth records.
OAuth identity uses stable user, organization, workspace, and billing scope.
API credentials use fingerprints. Do not use an email, last-loaded marker, or
access-token hash as OAuth identity. Local checks do not verify remote login
acceptance. Refuse unknown or ambiguous identity without logging token values.

Supported forms and user actions are in the README. Keyrings and arbitrary
external helpers are not filesystem profile state. Codex custom providers and
keyring modes, opaque OAuth identities, and Antigravity ADC/WIF are outside the
named-profile contract.

Capture both set and unset values from the supported credential/provider
environment. Named sessions must not fall back to host tokens, token caches, or
global barrel variables from another account. Shell startup must not replace
the selected credential or helper paths. Unknown variables and project
`.env` files remain outside this guarantee.

Host load cannot alter the parent shell. Require supported variables to match
the incoming selection; require them unset for a new empty profile. Check
outgoing file identity with its saved environment because the shell may already
contain incoming credentials. Report variable names, never values.

### Concurrency and runtime isolation

[internal/statelock](internal/statelock/) provides one startup/mutation lock.
Ask for stop-app confirmation outside the lock, then reacquire it and recheck
selection, account, root identity, and use. Retry confirmation if the outgoing
profile changed. Runtime launch holds a shared lock until mounts exist.

The use guard checks Docker/VM mounts and visible same-user Linux process
commands, file descriptors, working directories, and memory mappings. Known
use blocks even with `--yes`. This check is not proof that no reader or writer
exists. Permissions, namespaces, idle apps, and startup races prevent that
guarantee. Open descriptors can keep the outgoing inode after a rename.

Named runtimes mount only exact selected roots at unchanged public targets.
Do not mount a parent store, historical alias, recovery directory, or another
profile. Map a workspace below a selected root to that selection; reject paths
that expose other profiles. Apply read-only hooks through all resulting paths.

Runtime identity contains the profile ID and a digest of source paths and
directory identities. Switch and restore must prevent reuse of stale mounts
without rebuilding images.

### Transactions and recovery

A switch across multiple roots is not atomic. Write and sync `transaction.json`
before moving original data. Check parent and entry identity for each move.
The atomic index update, with its transaction ID, is the commit point.

Block new launches while a journal exists. Recovery reconstructs the permitted
operation plan and checks exact recorded paths and identities. Before commit,
roll back to the outgoing selection; after commit, finish the incoming
selection. Unexpected data stops recovery and leaves the journal for review.
Never trust arbitrary move paths from a JSON file.

Metadata reads are bounded, strict, and no-follow. Validate duplicate names,
account mappings, root shapes, and recorded ownership. Directories use mode
0700; metadata and credential files use 0600. Preserve existing root permissions.

A crash before journal publication can leave an empty new root or an
unregistered `.cooper-restore-<id>` stage. Do not select it automatically.
Retain it for manual review against the original state and backup. Profiles
do not defend against a malicious process with the same host UID.

### Backup, restore, and cleanup

Backup is an explicit independent copy into a new absolute destination. Verify
content and credentials. Copies preserve bytes, directory layout, symlink text,
permission bits, and file/directory modification times without following child
links. Omit sockets and FIFOs. Inode numbers, hard-link relationships, sparse
allocation, ACLs, extended attributes, access times, and symlink timestamps are
not guaranteed. Use filesystem backup tools if those properties are required.

Restore requires the same profile ID and account and intact registered roots
and parents. Verify the backup, copy to sibling stages, then replace entries at
fixed source paths. Keep old roots as recovery data. Repeated restores must not
add mounts.

Delete only an inactive, unused profile through checked recorded roots.
Recovery pruning is explicit and permanent. Runtime/cache cleanup must preserve
all profile state. Full configuration removal refuses a non-empty or unknown
profile store. Do not clear history, profile siblings, or host roots to repair
a runtime problem.

## Network and VM boundary

CLI workloads use the internal Docker network. Squid connects that network to
the external network and applies destination and selective TLS policy. Static
domain rules can splice TLS; path rules and live request review require
inspection. Unknown restricted Grok API paths fail closed.

Monitor's exact-host permissions last only for one `cooper up` and apply to
all attached workloads. Revocation affects later checks; these permissions do
not edit persistent rules. An allowed destination can receive private data.

The ACL helper consumes explicit destination, port, and source fields:
`%DST %>rP %SRC %DATA`. Squid can append an empty `-` ACL-data field. Parse
it without confusing the source IP for a port; reject invalid fields before
showing a request. Keep wire-format regression tests.

### VM services

The networkless supervisor runs as the invoking user with a read-only root,
all capabilities dropped, and only the required KVM device and mount sources.
QEMU uses `-nodefaults -nic none`, an explicit device list, and its process
sandbox. No host Docker or container-runtime socket is exported.

A separate relay has minimal internal-network access, not agent state,
workspace, KVM, or the host Docker socket. The path is:

```text
guest process or guest Docker
  -> virtio-serial -> supervisor Unix socket -> service relay
  -> Cooper proxy, bridge, or an explicitly configured host port
```

Guest root cannot add a missing network device. The guest supplies no arbitrary
relay destination. The host service map is checked for each stream; a missing
relay fails closed. HTTP/HTTPS port eligibility is not a general network grant.
The guest Docker daemon, builds, containers, and nested Cooper proxy all use
the same outer policy.

Each relay stream has a two-minute idle limit shared by both directions.
Successful reads or writes refresh both endpoints, so an active download does
not expire because no new request bytes arrive. Traffic cannot extend the
fixed 30-minute connection lifetime. These limits retain bounded resource use
without cutting off one-way downloads or uploads.

Virtiofs exports only approved roots. Soft UID/GID mapping maps guest file
operations to the invoking host user. Read-only limits are enforced on the
host side. The guest manifest is read-only. Guest Docker belongs to the guest;
its socket is available only there.

One managed nested VM is supported for self-development. Depth 1 can use KVM;
depth 2 receives no KVM device or CPU virtualization flags; managed depth 3 is
refused. This is not a universal ban on guest root running an unmanaged VMM.
All nested work remains inside the outer network and filesystem limits.

### Assets and limits

Guest Ubuntu/Docker assets have pinned sizes and SHA256 hashes. Preparation
creates an immutable versioned base; each VM uses a qcow2 overlay. Imported
agent archives must match the selected image ID. Restart rotates relay and
clipboard tokens. Cleanup validates ownership and exact runtime/image IDs.

The boundary does not remove kernel, QEMU, KVM, Docker, CPU, denial-of-service,
or side-channel risks. It does not protect writable workspace/state from the
agent, nor data sent to an approved host. Host bridge scripts and forwarded
services add explicit host authority.

### Embedded virtiofsd

[internal/vmpayload](internal/vmpayload/) contains the compressed static Linux
x86-64 virtiofsd 1.14.0. This version supports unprivileged soft UID/GID mapping;
1.10 attempted to restore UID 0 and failed in this supervisor design.

- Upstream commit: `c2540f8db14caba81c1e37fba23fc7bf2cd7f0dd`.
- Source archive SHA256: `52b66e449ca583b4f050a2bff327ff812211a2c349b4130279fcfc6a64540f04`.
- Executable SHA256: `3bde9d848edf61fd30448dbd98c016533500ecffbf990455a0bd72e34b7a26b3`.

[dev/build-virtiofsd.sh](dev/build-virtiofsd.sh) pins the builder, source archive,
Cargo lock, Alpine packages, and final hash. Runtime preparation checks size and
hash before use. Keep the bundled
[Apache license](internal/vmpayload/LICENSE-APACHE) and
[BSD license](internal/vmpayload/LICENSE-BSD-3-Clause) with the payload.

## Build and agent integration

Resolve Mirror, Latest, and Pin to exact inputs before template rendering.
There is no version allowlist. Reject missing requested artifacts rather than
substitute Latest. Keep network clients bounded and injectable.

Desired and built values in `config.json` include tool versions, implicit
language servers, and the base Node runtime. Save Only can reuse built implicit
versions only when the relevant desired runtime still matches. Update always
regenerates and reloads proxy configuration, even if no image rebuild is needed.
Initial configure enables detected AI tools in Mirror mode.

The Go language-server build uses the official module proxy and checksum
database. Bounded retries preserve completed downloads but do not disable
checksums or fall back to an unreviewed mirror. Agent module caches are runtime
mounts, not mounts in Dockerfile build steps. Cached module archives without
signed checksum records can still need network access.

For build permission review, inspect the templates, version resolvers, and
actual redirects. Common sources include Docker registries, Debian/Alpine,
Go's module and download services, npm, PyPI, GitHub release assets, Claude's
download service, xAI releases, and Antigravity metadata/Google Storage.
Do not turn a temporary build permission into a default runtime rule.
Background feature, telemetry, and changelog hosts are not automatically
required model hosts.

### Add an agent

1. Identify the native CLI, official artifact, version command, architectures,
   and license. An SDK or similarly named desktop app is not the CLI.
2. Add catalog metadata in [internal/aitool](internal/aitool/). Use the catalog
   executable name in launch, version, proof, UI, and test paths.
3. Resolve exact artifacts and helpers. Keep binaries outside state roots and
   protect built-in names from custom-directory collisions.
4. Trace native state in a private empty home. Cover auth modes, overrides,
   settings, sessions, plugins, and shutdown. Add complete roots to the shared
   catalog, including absent optional roots and cleanup protection.
5. Extend auth selectors, captured set/unset environment, stable local
   identity, and known-writer detection. Fail closed for unsupported auth.
6. Test Default, pending login, switching, account mismatch, token refresh,
   backup/restore, shared roots, and selected-only runtime mounts.
7. Observe required destinations in real native flows. Use exact defaults and
   ownership-aware rule migration. A string in a binary is not sufficient.
8. Add test pins, image matrices, finite VM commands, and permission-hook cases.
   Run local fixtures, prepared parity/profile tests, then the full required
   checks. Real login and provider use need explicit account authority.

### Antigravity details

The host wrapper sets `DBUS_SESSION_BUS_ADDRESS=unix:path=/dev/null` only for
the native `agy` process so it uses file OAuth under
`~/.gemini/antigravity-cli`. Child programs that need D-Bus can also be affected.
Other desktop processes retain their bus. Do not migrate or delete keyring data.

Physical-host build adds marked shell blocks while preserving other content
and existing shell-file links. Sourcing setup clears conflicting `agy` aliases
and functions. Check the wrapper before file-profile launch. A missing wrapper
or file login prints the relogin instruction and exits without a session;
malformed auth is an error. Inner builds skip physical-host setup. Cleanup
retains the wrapper so it does not silently change future login mode.

Release metadata resolves opaque per-architecture archive URLs and SHA512
digests. Freeze those in build inputs. The native binary is under
`/opt/cooper/libexec/agy`, with its launcher under `/opt/cooper/bin`.
Read the required Playwright driver version from the selected binary and
install that exact official driver/Node runtime. Do not use the host browser
cache or silently update the native binary at runtime.

Ordinary ADC uses the parent of `GOOGLE_APPLICATION_CREDENTIALS`, resolved
relative to the workspace, or default `~/.config/gcloud`. It does not use
`CLOUDSDK_CONFIG`. Reject whole-home and Cooper-state overlaps. Named profiles
support file OAuth and explicit Gemini API mode, not ADC/WIF.

Do not weaken Cooper's isolation to make Antigravity's optional native sandbox
work. A native sandbox configuration can be incompatible with the reviewed
runtime policy. Provider proof needs a structured success marker, conversation
ID, completed turn, and nonzero output; exit zero alone is insufficient.

## Verification

Run commands from the repository root. Keep complete logs under `/tmp`.
Use temporary homes and fabricated credentials for normal tests. Do not move
or mount the developer's real profile data into a fixture. Real login, refresh,
model requests, and account acceptance tests need explicit authority and are
not replaced by a local model fixture.

### Local and Docker checks

Start VM-related work with the offline unit command:

```sh
./cooper/test-vm-dev.sh unit > /tmp/cooper-vm-unit.txt 2>&1
```

It runs an explicit package set with Docker, QEMU, and network access blocked.
Install the declared Go toolchain and modules first; this mode cannot download
them. It also needs the local tools used by the unit fixtures, including Bash,
curl, jq, and clipboard test dependencies.

Use targeted package tests while iterating. Finish Cooper code changes with:

```sh
go test -C ./cooper ./... > /tmp/cooper-go-test.txt 2>&1
timeout 90m ./cooper/test-e2e.sh > /tmp/cooper-e2e.txt 2>&1
timeout 90m ./cooper/test-docker-build.sh all > /tmp/cooper-docker-build.txt 2>&1
go build -C ./cooper -o ./cooper . > /tmp/cooper-build.txt 2>&1
```

The full Go suite includes Docker-backed packages. Cross-process bootstrap
locks protect shared test images. `GOFLAGS=-p=1` can avoid package lock waits
without changing coverage. Do not claim an excluded package set passed the
full suite.

The Docker matrix has `mirror`, `latest`, `pinned`, `all`, and `clean`
modes. Mirror requires real host binaries for enabled tools, not version stubs.
Linux shell E2E needs `ss` from iproute2. For exact test-image cleanup:

```sh
./cooper/test-docker-build.sh clean > /tmp/cooper-docker-build-clean.txt 2>&1
```

The installed-Codex resume fixture uses a local model endpoint:

```sh
COOPER_NATIVE_PROFILE_CODEX=/absolute/path/to/codex \
  go test -C ./cooper ./internal/profiles -run '^TestNativeCodexResume$' \
  -count=1 -v > /tmp/cooper-native-profile.txt 2>&1
```

This checks native local history behavior, not provider OAuth or billing.

### Prepared VM development

Use [test-vm-dev.sh](test-vm-dev.sh) for bounded checks. Run one command at a
time for a given UID.

| Command after `./cooper/test-vm-dev.sh` | Purpose | Runtime VM starts |
| --- | --- | --- |
| `prepare` | Prepare the small generic image, helpers, and guest base | Preparation can need one |
| `smoke` | Boundary, mounts, guest Docker, proxy, bridge, ports, clipboard, reuse, stop | 1 |
| `mounts` | Bind refresh, timezone, ownership, virtiofs | 1 |
| `lifecycle restart` | Restart and state continuity | 2 |
| `lifecycle resources` | Resource-change lifecycle | 2 |
| `lifecycle relay` | Relay failure and recovery | 2 |
| `lifecycle agent` | Agent replacement lifecycle | 2 |
| `prepare-agent <agent>` | Prepare one real built-in tool | Preparation can need one |
| `parity <agent>` | Selected tool version, account, paths, state, and writes | 1 |
| `profiles codex` or `profiles antigravity` | Profile mounts, credentials, restart, restore, hooks, cleanup | 2 |
| `clean` | Remove owned unused runtime/image resources | 0 |
| `clean-cache` | Explicitly remove unused prepared cache | 0 |

Example for a profile change:

```sh
./cooper/test-vm-dev.sh prepare-agent codex > /tmp/cooper-vm-prepare-codex.txt 2>&1
./cooper/test-vm-dev.sh parity codex > /tmp/cooper-vm-parity-codex.txt 2>&1
./cooper/test-vm-dev.sh profiles codex > /tmp/cooper-vm-profiles-codex.txt 2>&1
```

Repeat the matching prepare/parity/profile commands for Antigravity when its
state or credentials change. Test pins are in
[internal/vmdev/config.go](internal/vmdev/config.go).

Runtime commands require prepared inputs and cannot build, download, or export
a host image. One guest archive import per new VM is expected. Stale source,
binary, account, daemon, image, or configuration identity requires preparation
again. The cache digest includes dirty and new source files but excludes
Markdown; before/after checks detect source changes during preparation.

Prepared data is under `cooper/.test-tmp/vm-dev-cache/`, owned by UID and
Docker-daemon identity. A per-UID lease bounds contention; profile tests also
use the shared host-state lock. Stable fake homes and short `/tmp` socket
aliases prevent path changes and Unix socket length failures. Hard-linked
archives must not be chmod/chown modified.

`COOPER_VM_PREPARED_BASE` can reuse a normal schema-1 guest base after metadata
and hash checks. Preparation stages it read-only. Reports are unique
`/tmp/cooper-vm-dev-*.json` and log files; runtime logs remain under
`cooper/.test-tmp/vm-dev-runs/`. Preparation VM starts and runtime starts are
separate report fields.

Cleanup checks the lease, owner, daemon, run records, and exact IDs. It does
not run global Docker prune or remove host agent state. Keep prepared data
between checks. Archive size and copy counters are not physical SSD-write
measurements; such claims require the host block-device counters and workload
context.

### Runtime test driver and UI

[internal/testdriver](internal/testdriver/) drives the real application,
Docker, bridge, clipboard, persistence, and cleanup without a terminal:

```sh
go run -C ./cooper ./cmd/cooper-test-driver --scenario clipboard-smoke \
  > /tmp/cooper-driver-clipboard.txt 2>&1
go run -C ./cooper ./cmd/cooper-test-driver --scenario barrel-env-smoke \
  > /tmp/cooper-driver-env.txt 2>&1
```

It supports `--prefix`, `--keep`, `--disable-host-clipboard`, and
`--timeout`. Keep reusable runtime assertions here; deterministic `tui-test`
fixtures test presentation, not real runtime behavior. Follow
[AGENTS.TUI.md](../AGENTS.TUI.md) for material UI changes.

### Release gate

Run the full VM gate only before a release, on the physical Linux host:

```sh
timeout 120m ./cooper/test-vm.sh > /tmp/cooper-vm-gate.txt 2>&1
```

The outer bound includes the agent matrix and two separate 30-minute inner
self-host bounds. Do not use this gate as a routine development or cost
baseline. Passing a prepared profile does not replace it. A failed download
or unavailable prerequisite means the affected gate has not passed.

After all required checks, run the release preview from the repository root:

```sh
./scripts/release-cooper.sh > /tmp/cooper-release-preview.txt 2>&1
```

The preview reads `cooper/meta/version.go` and prints shell-quoted commands;
it does not publish the release. Review and execute only those commands, with
explicit release authority. Keep the private
`/tmp/cooper-release-tag-message.*` file until its printed `git tag -F` step
has completed. The commands use the Cooper module tag prefix, `origin`, and
non-force pushes. They also check the exact remote refs and module version.

Keep full logs and the project path visible in development commands. The
[repository permission policy](../.codex/hooks/README.md) explains the reviewed
command forms; hook approval does not replace user authority.

## Deferred designs

These earlier proposals are not current behavior:

- A built-in PostgreSQL programming tool. The proposed design uses a
  major-only version, a stable binary path, and localhost port 15432 to avoid
  the forwarded 5432 service. Acceptance would require real readiness and SQL
  checks, not only `psql --version`.
- Four-way Docker test execution. It needs separate image-bootstrap locking,
  isolated per-run namespaces, a shared four-runtime limit, and exact-owner
  cleanup before selected tests can safely run in parallel.

The old copy/symlink profile proposals are superseded by the profile contract
above; they are not alternative supported storage modes.
