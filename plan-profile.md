# Cooper account profiles

Status: implemented and verified. All required development gates passed.

## Goal and accepted behavior

Provide reliable account switching for the supported coding harnesses. Keep
one shared list of complete state roots. Preserve the host account, absolute
home, workspace, agent paths, and path environment in Docker and VM mode.
Switching an AI account must not require another image build or Linux user.

The user approved implementation after this plan. No further approval is
needed for code, tests, isolated test data, or deterministic TUI captures.
Do not operate on the user's real credentials or load their real host state
while testing. Do not stage or commit this task without a later request.
`plan-antigravity-support.md` is unrelated user work and must stay unchanged.

Commands:

| Command | Contract |
| --- | --- |
| `cooper save <harness>` | Identify the account and save to its mapped profile. Never accept an arbitrary existing destination. |
| First save | Create `Default` and bind it to the identified account. |
| Save a new identified account | Ask for a new unused name, unless a newly loaded profile is awaiting this login. |
| `cooper load <harness> <profile>` | Save the current identified profile first, then restore the selected profile into host state. |
| Load a missing profile | Preserve current state, create empty agent state, load it, and remember the name awaiting login. |
| `cooper cli <harness>` / `cooper vm <harness>` | Continue to use live host roots. |
| `cooper cli <harness> <profile>` / `cooper vm <harness> <profile>` | Mount the selected profile read-write at the original host paths. |
| Profiles tab | List profiles, show loaded/pending/in-use state, save, load/create, and delete through one service. |

`Default` is a saved profile, not an alias for live host state. Names are
case-insensitive identifiers with a preserved display name. `default` selects
`Default`. Reject path separators, dot segments, control characters, empty
names, and names outside a short documented ASCII name grammar. Use internal
random IDs for storage and runtime identity; renaming must not be needed to
make the first release useful.

A saved profile is a writable state view. Agent changes persist there.
Host state and a saved profile can therefore diverge. No implicit merge is
allowed, and no timestamp-only overwrite decision is sufficient.

## Existing code and design boundaries

Starting revision: `5e66aa72528e6405a28e636fbe08464deacef0a2`.

- `cooper/internal/workload/agentpaths.go`: complete selected state roots,
  path overrides, nested-root removal, optional files, and environment.
- `cooper/internal/workload/mountplan.go`: common plan, validation, ownership,
  directory setup, and content-independent runtime reuse digest.
- `cooper/internal/docker/barrel.go`: Docker start, naming, labels, reuse.
- `cooper/internal/vm/lifecycle.go` and `metadata.go`: VM start/restart and
  persisted launch inputs. Restart must retain the selected profile.
- `cooper/internal/launch/session.go`: shared shell/session policy.
- `cooper/internal/auth/resolve.go`: environment, workspace token cache,
  login-shell fallback, and terminal metadata. Named profiles must not read
  another account from these host fallbacks.
- `cooper/internal/app`: business boundary used by the TUI.
- `cooper/internal/tui`: presentation and typed messages. Follow
  `AGENTS.TUI.md`; do not add filesystem or Docker calls to the screen.
- `cooper/dev/README.md`: inexpensive local and prepared VM validation.

Keep the following concerns separate:

1. Root discovery: which complete paths belong to the selected harness.
2. Account recognition: which credential identity the state represents.
3. Profile storage: state copies and metadata owned by Cooper.
4. Host selection: which profile was loaded, and its common base digest.
5. Runtime selection: which profile one Docker/VM instance uses.
6. Presentation: prompts, progress, tables, and errors.

A profile ID is not an account ID. A loaded-profile marker is provenance,
not proof of the account after a manual login. A credential's changing token
bytes are not necessarily a changed account. Account recognition does not
prove that credentials remain accepted by the remote service.

## Package architecture

Use small packages and explicit values. The concrete file split can change
when that improves readability; preserve the dependency direction.

```text
CLI commands                 Profiles screen
      |                             |
      +-------- application service +
                       |
               profile operations
               /        |         \
         root policy  state store  account reader
                       |
              copy / digest / journal

Docker and VM start -> shared state selection -> common mount plan
```

Implemented package boundaries:

- `internal/profiles/types.go`: profile, selection, result, and typed errors.
- `internal/profiles/store.go`: validated private paths and JSON metadata.
- `internal/profiles/copy.go`: generic complete-tree copy and content digest.
- `internal/profiles/transaction.go`: recoverable replacement of host roots.
- `internal/profiles/service.go`: save/load/create/delete and conflict rules.
- `internal/profileauth`: pure bounded readers for reviewed credential
  formats and supported credential environment. Synthetic fixtures only.
- `internal/statelock`: per-UID launch and mutation coordination.
- `internal/profiles/selection.go`: validated shared Docker/VM selection.
- `internal/profilemanager`: real host context and actual runtime-use checks.
- `internal/app/profiles.go`: production dependencies and TUI-facing methods.
- `profile_commands.go`: Cobra parsing and terminal prompts only.
- `internal/tui/profileui`: model, typed messages, view, and fake-service tests.

Example service boundary (names may follow existing package conventions):

```go
type Service interface {
    List(context.Context) ([]Summary, error)
    Save(context.Context, SaveRequest) (Result, error)
    Load(context.Context, LoadRequest) (Result, error)
    Delete(context.Context, string, string) error // harness, profile name
}

type SaveRequest struct {
    Harness string
    // Only supplies a name for a new account; never selects an existing target.
    NewName string
    ConflictChoice ConflictChoice // empty, host, or saved
}
```

Prompt requirements are typed results/errors such as `NameRequired`,
`IdentityUnknown`, `AccountConflict`, `StateConflict`, and `StateInUse`.
Do not parse error strings to choose a screen action. A prompted retry must
re-read state and validate its inputs under the lock. No lock spans human
input. Failure paths must leave a usable CLI/TUI and a concrete reason.

Use required interfaces with production defaults at composition boundaries.
Do not add nil checks throughout domain code. Filesystem fault injection
must be narrow (copy/publish/rename boundaries), not an imitation filesystem
that makes real file behavior disappear from tests.

## State and storage model

Use a private, schema-versioned store under the selected Cooper directory:

```text
~/.cooper/profiles/
  index.json
  transaction.json                 # only while a host load needs recovery
  harnesses/<harness>/<internal-id>/<generation-id>/
    snapshot.json                  # recovery description; index is authoritative
    roots/<root-id>/...
    credentials.json
  recovery/<operation-id>/...
```

The index is the sole metadata authority. A save writes a new data generation
and publishes the complete index with one atomic rename. Keep one prior
generation for recovery. Only host load needs a multi-root rename journal.
The operation lock is shared across Cooper directories for the same UID,
because two stores can select the same host roots. Launch holds a shared
lock through startup; mutations hold an exclusive lock.

Storage names come from validated IDs, never unchecked user paths. Store
folders have mode 0700 and metadata/credential files mode 0600. Preserve
original file modes inside state roots, including executable files. Do not
put raw account tokens in logs, table rows, mount labels, or non-secret
runtime metadata. Avoid full secret-bearing command output in errors.

A profile manifest records at least schema, ID, harness, display name,
creation/update time, account identity key and display information, build
account/home contract, root IDs/kinds/original absolute targets, path
environment (including unset versus empty), and expected snapshot digest.
Credential environment is stored separately with private permissions.

The host selection records the profile ID, pending-login state, effective
root mapping, and common-base digest. Each harness has its own selection.
Account identity indexes are derived from validated manifests where simple;
avoid two mutable sources of truth. OpenCode identity is a sorted set of
provider identities, not one assumed global account.

Preserve optional-path absence. Loading an absent optional file removes the
outgoing profile's file; it must not silently retain the previous account's
config. Fresh profiles contain empty required directories and absent optional
files. Record the complete selected scope, including optional files that can
appear after login. New roots added to the catalog must never fall back to
live host data for a named profile. Either provision an empty reviewed root
or require a clear refresh; test the chosen rule.

## Account recognition and credentials

Research the installed/pinned harness formats before implementing adapters.
Use supported status output or narrow, bounded, read-only decoders. Do not
read actual user credential files to create fixtures. Record primary source
links and versions below. Do not run a login or network token refresh as an
identity check. Unknown fields are allowed; malformed required fields,
ambiguous methods, and unsupported identity sources fail closed.

Identity states:

- Identified and mapped: save to that profile, subject to conflict checks.
- Identified but unmapped: create `Default` on first save, use a pending name,
  or request a new unused name.
- No login / unverified identity: keep a recovery snapshot and leave existing
  named profiles unchanged. Do not interpret this as a new account.
- Pending profile with an account already assigned elsewhere: report a
  conflict; do not rebind or overwrite either profile.

Prefer provider + stable account ID + organization/workspace when available.
Treat a stable API-key fingerprint as credential identity, not a claim about
its human owner; a changed API key is a new unknown mapping until explicitly
registered. Never use a rotating access/refresh token hash as a stable OAuth
account ID. Do not use email alone when organization identity is available.

Named runtime sessions receive only their captured supported credential
values plus ordinary terminal metadata. Disable the host token cache,
login-shell credential lookup, and host token-file fallback for that path.
User-configured Cooper environment must not reintroduce a different
supported credential. Non-profile sessions keep existing behavior.

A child process cannot modify its parent's environment. Host `load` must
check credential/path overrides before touching roots, and give an exact
remediation when the invoking shell would override the loaded state. It
must not pretend a folder replacement switched an environment-only login.
OS keychain/external-helper authentication needs an explicit supported
adapter; an unverified method uses recovery behavior rather than guessing.

Initial sources:

- Claude CLI status command:
  https://code.claude.com/docs/en/cli-reference
- Claude authentication and storage:
  https://code.claude.com/docs/en/authentication
- OpenCode auth schema at v1.3.7:
  https://raw.githubusercontent.com/anomalyco/opencode/v1.3.7/packages/opencode/src/auth/index.ts
- OpenCode provider authentication:
  https://opencode.ai/docs/cli/#auth
- Codex 0.117.0 file storage and token claims:
  https://raw.githubusercontent.com/openai/codex/rust-v0.117.0/codex-rs/login/src/auth/storage.rs
  https://raw.githubusercontent.com/openai/codex/rust-v0.117.0/codex-rs/login/src/token_data.rs
- Claude 2.1.87 installed package: `installOAuthTokens` updates `oauthAccount`
  before writing `claudeAiOauth`. The adapter requires both files.
- Copilot 1.0.12 installed package: `config.json` selects
  `last_logged_in_user`, then `logged_in_users[0]`. File tokens use
  `copilot_tokens[host + ":" + login]`. Keychain precedence must be disabled
  or file storage must be explicitly selected.
- Grok 1.0.4 installed release: `auth.json` is a map of scope to credential.
  Required fields are `key`, `auth_mode`, `create_time`, and `user_id`.
  Its decoder accepts `grok`, `web_login`, `oidc`, `external`, and `api_key`;
  Cooper rejects obsolete `web_login` and unverified external helpers. This
  was tested with fabricated JSON through the actual binary under
  `unshare -Urn`, with no network interface or real credentials. Grok
  identity includes the complete saved scope set, user, organization, and issuer.

## Save rules

1. Resolve and validate host roots and path context without creating or
   deleting host state. Check ownership, symlinks, overlap, and runtime use.
2. Obtain the operation lock. Recover or reject an incomplete prior operation.
3. Identify the source account and select its mapped destination. A supplied
   new name cannot override an existing profile or a known mapping.
4. Copy all selected roots into a new private staging directory. Preserve
   regular files and symlinks; never follow internal symlinks into unrelated
   host data. Skip sockets/FIFOs as process transport, report that omission,
   and reject device nodes. Keep SQLite databases and sidecars together.
5. Compare source before and after the copy and verify the staged content.
   Abort if source changes. Identity is checked again before publication.
6. Check the destination against the recorded common base. If only host
   state changed, save it. If only saved state changed, preserve it. If both
   changed differently, keep both and report a conflict. Equal contents need
   no replacement. Without a proven common base, never assume overwrite is
   safe merely because the account ID matches.
7. Publish the complete staged copy; keep a recovery version until metadata
   is committed. Update selection and manifest only after success.

The copy algorithm must not use hard links for mutable profile files.
Reflink support can be a tested optional optimization, never a requirement.
Do not scan or copy on every runtime launch. Content comparison belongs to
save/load operations; normal runtime reuse still hashes path/inode identity.

## Load and fresh-profile rules

1. Validate the requested name, account/home contract, path environment, and
   target profile before modifying host state.
2. Save current host state through the same save service. An unknown account
   can request a new name. Unverified state is preserved separately; any
   unresolved conflict prevents replacement.
3. Check again whether the requested profile exists (the preceding save may
   have created it). If absent, create an empty pending profile.
4. If the selected profile is already loaded and state is consistent, return
   without discarding host changes. A pending empty profile can be selected
   again without requiring a login first.
5. Stage each incoming host root on its target filesystem. A multi-root
   change is not one atomic rename. Use a durable journal that records old,
   staged, installed, and missing paths before each rename.
6. Rename old roots aside, install staged roots, and publish host-selection
   metadata only when every root succeeds. Recover by rollback after an
   interrupted or failed operation; keep old data until recovery is complete.
7. Show which profile was saved and loaded. For a fresh profile, instruct the
   user to log in on the host and run `cooper save <harness>`.

Keep one complete recovery version for a successful replacement. Do not
silently delete unresolved conflict or failed-transaction data to enforce a
retention limit. Expose recovery paths and document exact recovery behavior.
If conflicts need a resolution action, require a reviewed current/saved
choice that revalidates both digests; never restore arbitrary named-save
arguments as an overwrite bypass.

## Concurrency, paths, and process safety

Serialize store mutations across harnesses because roots such as `.agents`
are shared. Coordinate launch with save/load/delete so a runtime cannot begin
using roots between a use check and replacement. Inspect actual mount sources
and identities, including other runtime namespaces. Refuse replacement of
roots used by a live Docker barrel, VM supervisor, or detected local harness.
Idle-but-running containers still hold bind mounts. Do not stop them silently.
Document the host process visibility boundary and require the host harness to
exit; a file-tree stability check cannot guarantee a live database snapshot.

Do not modify the physical host through a nested session's incomplete view.
Host save/load commands must distinguish a real host from a Cooper workload;
tests use explicitly isolated homes and injected boundaries. Runtime profile
selection remains available at the supported nesting depth.

Root validation covers direct and resolved paths; Cooper-store overlap,
complete home, protected system paths, workspace overlap during replacement,
nested/duplicate roots, root symlinks, broken symlinks, filesystem boundaries,
and arbitrary paths in tampered metadata. User-writable metadata is never
permission to delete an unvalidated destination. Use rooted/no-follow file
operations where necessary so symlink swaps cannot escape checked roots.
Stored state roots have their own ownership type; do not weaken HostState
validation or pretend profile state is disposable runtime/cache data.

## Docker, VM, and session integration

Resolve a state selection once and pass it to both execution boundaries:

```go
type Selection struct {
    ID string // empty means live host state
    Name      string
    Paths     workload.AgentPaths
    Credentials []workload.EnvVar
}
```

Mount sources point into the selected profile. Targets remain the recorded
host absolute paths. The path environment comes from that same selection.
Validate the built account and path contract before startup. Reject a
profile from another account/home or an unsafe/obsolete mapping before any
runtime is stopped or recreated.

Use workspace + harness + profile ID for runtime identity; retain existing
names for no-profile launches. Add profile ID/name to runtime labels and TUI
summaries. Include profile selection in reuse checks and persist it for VM
restart. A profile root replacement must change reuse identity. Token files,
shell markers, temporary homes, clipboard, ports, and proxy policy retain
per-runtime isolation. No profile store or unrelated profile is mounted into
an agent; only the selected roots are visible.

Normal `down`, cache cleanup, build cleanup, and runtime cleanup preserve
profiles. Full configuration deletion must not erase saved accounts under a
generic configuration prompt. Profile deletion is explicit, validates its
identity, refuses loaded/in-use profiles, and never follows a profile symlink
into host state. The Profiles screen invokes this same service.

## TUI and CLI interaction

Add a Profiles tab with harness, profile name, account summary, saved time,
and loaded/pending/in-use state. Use existing table, viewport, text entry,
and modal components. The selected row supplies actions. Allow save for a
harness even in the empty state. A new-name form applies only when the
service requests a new mapping; a load/create form accepts harness and name.
Delete has a clear target confirmation and preserves selection on failure.

All work runs as commands and returns typed result messages, including
operation errors. Root routing must deliver results even if the user changes
tabs. Do not block Update, read files in View, or expose tokens. Provide a
fixed fake storybook in `cooper tui-test --screen profiles` with populated,
empty, pending, busy, prompt, and error states as practical.

CLI prompts use command input/output and a typed service result. Do not
start recursive shell commands to implement save/load. Noninteractive use
has an explicit new-name option that is only valid for creating an unmapped
profile; it must fail if it would select an existing destination. Usage text
must make clear that `cooper save codex Default` is not accepted.

## Test matrix and execution order

Local tests first. Tests use fabricated accounts, tokens, roots, and an
isolated home. Never pass real provider credentials to tests or screenshots.

| Area | Required cases |
| --- | --- |
| Identity | Each reviewed harness format, organization separation, token refresh with stable identity, API credential changes, multi-provider ordering, ambiguous/malformed/oversized files, no login, no secret output |
| Names | First Default, case-insensitive lookup, new names only, traversal/control/length errors, duplicate mapping, unknown harness |
| Save | Correct mapped destination after manual login, pending first login, pending wrong existing account, unknown identity recovery, cancellation/copy/disk failure, replacement removes old-only files |
| Conflict | Host only changed, profile only changed, equal independent changes, both changed, absent common base, changed roots/path overrides, active profile runtime |
| Load | Automatic outgoing save, Default -> Work -> Default preserves bytes, missing Work creates empty roots, pending login binds once, same-profile no-op, absent optional files remove outgoing files |
| Copy | Nested trees, empty files/directories, executables, symlinks inside/outside/broken, special files, sparse/large files, SQLite/WAL fixture, concurrent writer detection, no hard-link aliasing |
| Recovery | Failure before and after each root rename, before metadata commit, interrupted journal reopen, corrupt/tampered journal, recovery failure keeps data, no broad cleanup |
| Mounts | All five harness root maps, selected profile only, unchanged targets/env, no host credential fallback, profile path boundary validation, newly added roots fail closed |
| Runtime | Same workspace with two profiles, no-profile compatibility, reuse and replacement, VM restart preserves selection, active-use refusal, launch/mutation race |
| TUI | Empty/populated, save/name prompt, load/create, delete/cancel, busy/error, late result after tab change, resize and modal key routing |
| Cleanup | Runtime/down/cache preserve profiles, full-config cleanup preserves accounts or refuses, symlink traps cannot remove external data |

Use real temporary files for copy and recovery tests. Fault injection targets
rename/publication boundaries. Use a small subprocess for crash/lock tests.
Run a real SQLite integrity probe after round-trip replacement without adding
a database dependency to production profile copying.

Extend the local VM unit package list for new pure packages. Reuse prepared
VM fixtures for profile mount/parity assertions. Avoid a new full VM gate or
repeated image exports. Test all root maps locally and in small Docker checks;
run focused prepared VM checks for the common integration and a real agent.
Preserve all existing release coverage and the release-only full VM gate.

Final required checks from repository root, with logs:

```sh
GOTOOLCHAIN=auto ./cooper/test-vm-dev.sh unit > /tmp/cooper-profile-unit.txt 2>&1
GOTOOLCHAIN=auto GOFLAGS=-p=1 go test -C ./cooper ./... > /tmp/cooper-go-test.txt 2>&1
GOTOOLCHAIN=auto timeout 90m ./cooper/test-e2e.sh > /tmp/cooper-e2e.txt 2>&1
GOTOOLCHAIN=auto timeout 90m ./cooper/test-docker-build.sh all > /tmp/cooper-docker-build.txt 2>&1
GOTOOLCHAIN=auto go build -C ./cooper -o ./cooper . > /tmp/cooper-build.txt 2>&1
```

Use the available toolchain override when this VM's installed Go is older
than go.mod. Run heavy Docker gates in sequence and watch the daemon's disk,
not just workspace free space. Preserve the current session and unrelated
images. Run only owned exact-ID cleanup.

Run Go packages in sequence with `GOFLAGS=-p=1`. In the first parallel full
run, the main package passed, but other packages exhausted their test time
limit while waiting for the repository's shared Docker lock. The app package
waited 6 minutes 57 seconds; the testdriver package waited 10 minutes 52 seconds.
The original log is `/tmp/cooper-profile-go-test-parallel.txt`. Serial package
execution preserves coverage and removes this lock wait.

Build a static capture binary under `/tmp`, use `scripts/capture-tui.sh`, and
inspect every generated PNG with the image tool. Cover normal and small
terminal sizes plus a material interaction state. Keep captures outside Git.

## Milestones and completion evidence

- [x] Write this plan after reading the root policy and execution boundaries.
- [x] Verify the initial account recognition formats with primary sources and safe fixtures.
  Unsupported keychains, helpers, and opaque OAuth identities fail closed.
- [x] Implement state types, validated storage, copy/digest, and recovery tests.
- [x] Implement identity mapping and save/load/conflict service tests.
- [x] Integrate common profile selection, credentials, CLI, Docker, and VM.
- [x] Add application boundary, Profiles screen, storybook, and behavior tests.
- [x] Update requirements, user/developer documentation, and cleanup rules.
- [x] Run local, focused Docker/VM, full required gates, and TUI visual checks.
- [x] Review the final diff and record evidence and remaining platform limits.

The goal is complete only when the agreed commands and TUI are implemented,
required checks pass, and limitations are stated accurately. Keep this plan
updated during execution; do not mark completion because time or context is
low. The user has requested implementation, not a release or a push.

### Completion evidence

- The final full Go suite passed with serial package execution:
  `/tmp/cooper-go-test.txt`. The command package took 456.32 seconds, the app
  package 412.74 seconds, Docker 6.20 seconds, and the test driver 24.46 seconds.
  The remaining packages, including all new profile packages and TUI tests,
  passed in the same run.
- The full shell E2E suite passed all 345 checks and completed its cleanup:
  `/tmp/cooper-e2e.txt`. This includes real harness clipboard interaction,
  selected-agent mounts, network policy, and implicit tool isolation.
- The first Docker build gate passed latest and pinned mode, but mirror mode
  could not find Claude in this Codex-only VM's `PATH`. The original result
  is `/tmp/cooper-profile-docker-build-missing-tools.txt`. The repeat uses
  the actual pinned test binaries already installed under
  `/tmp/cooper-home-tools/bin` and `/tmp/cooper-home-tools/npm/node_modules/.bin`.
  No version-output stub is used. Completed test-mode images are removed by
  exact owned IDs when needed to fit the VM's 30 GB disk.
- The complete Docker build repeat passed: 82 checks in each of mirror,
  latest, and pinned mode, with no failures. The log is
  `/tmp/cooper-docker-build.txt`. Its cleanup command also passed and removed
  the remaining test images and directories.
- The required development build passed (`/tmp/cooper-build.txt`), as did
  the 16 permission-hook tests (`/tmp/cooper-profile-hooks.txt`). The final
  diff has no whitespace or Go formatting errors. No changes were staged.
- Copy, service, identity, lock, workload, session, Docker, and VM package
  tests pass in `/tmp/cooper-profile-integration.txt`.
- Real filesystem tests cover Default → new Work → Default round trips,
  pending login, optional-file removal, account conflicts, host/profile
  divergence, and failure before/after each of eight root renames.
- Interrupted-load tests reopen the service after each rename and recover
  the original host state, including explicit `CODEX_HOME`.
- Partial-copy/disk-full tests leave the old index and host roots intact and
  remove unjournaled staging copies. Metadata failure tests distinguish a
  committed index from an uncommitted load. A late writer retains its data,
  the original backups, and the journal for review.
- A final review reproduced a digest ambiguity: one binary file could encode
  the same hash input as two separate files. The regression failed before
  the fix (`/tmp/cooper-profile-digest-before.txt`). File content now enters
  the tree digest as a fixed-size SHA-256 value. The full profile package
  passes with the regression (`/tmp/cooper-profile-digest-after.txt`). The
  prepared runtime check, local tests, and race checks also pass after this
  change. The final full gates use this corrected version.
- Grok's required `user_id` must be present even in an API record where its
  value is empty. A second regression reproduced acceptance of a missing
  field (`/tmp/cooper-profile-grok-before.txt`). The reader now distinguishes
  an absent field from an empty user. All identity tests pass after the fix
  (`/tmp/cooper-profile-grok-after.txt`). This reader-only change is covered
  locally; it does not require another Codex VM import.
- A real SQLite database with WAL sidecars passes `PRAGMA integrity_check`
  after the profile round trip. Sparse-file tests verify bytes and independent
  writable copies; sparse allocation itself is not preserved.
- All five harnesses have local source/target and credential selection tests.
  The real Docker check covers separate profiles in one workspace, writable
  saved sessions, reuse, host isolation, labels, and active-use refusal.
- The final offline unit pass took 17.83 seconds with 0 VM starts, 0 image
  exports, and 0 guest imports:
  `/tmp/cooper-vm-dev-unit-41466161dbce.json`. Profile storage, identity, use
  checks, locks, and screen behavior also passed the race detector in
  `/tmp/cooper-profile-race.txt`.
- The prepared Codex check reuses its image archive. After the final reader
  fix, preparation took 10.59 seconds and did not start a VM, export an agent
  image, or import an image into a guest:
  `/tmp/cooper-vm-dev-prepare-agent-7b982acad70d.json`. The prepared cache now
  matches the final source, so later runtime checks can reuse it directly.
- The final `profiles codex` pass took 168.06 seconds. It checked all four Codex roots,
  host and credential isolation, Docker-to-VM session data, VM restart, labels,
  and cleanup. It recorded exactly 2 VM starts, 2 guest image imports, and
  0 image exports: `/tmp/cooper-vm-dev-profiles-a3d6d3e95ed8.json`. There were
  two complete profile runtime passes during this task: 4 starts and 4 imports
  in total, with no image exports. These counts measure test work; physical
  SSD writes were not measured in this VM.
- TUI behavior tests cover narrow layouts, typed prompts, long details with
  keyboard and mouse scrolling, pure rendering, results after a tab change,
  and cancellation with rollback before application shutdown.
- Final VHS captures use a static test binary and isolated, network-disabled
  containers with fabricated accounts. Inspected captures include populated
  state at 1280×760 and 800×500, the new-name prompt at 800×500, and error
  details at 800×500. The empty, conflict, and delete states were also captured
  and inspected. Files are `/tmp/cooper-profiles-{normal,small,name,error,empty,conflict,delete}.png`.
- The development binary also cross-compiles for Darwin/arm64. This checks
  compilation only; macOS runtime and keychain support were not inferred.
- No real credential files were read or changed for this work.
- The original Cooper session container remained running. The unrelated
  `plan-antigravity-support.md` was not changed. This completed plan remains
  as the requested design and verification record; user instructions are in
  `cooper/docs/profiles.md`.

### Defined limits

- Local identity support is limited to the reviewed formats above. Remote
  token validity, OS keychains, arbitrary helper commands, and arbitrary
  provider environment names are not inferred from a loaded marker.
- The per-UID lock coordinates active operations across Cooper directories.
  A crash journal belongs to its selected store. After an interruption, use
  the same Cooper directory and path settings to recover before launching.
- Close host harnesses and runtimes before saving or loading. Other programs
  do not take Cooper's lock; content checks cannot make a live database copy
  an atomic filesystem snapshot.
- Copy preserves file bytes, links, and permission bits. It omits transient
  sockets/FIFOs and does not preserve times, extended attributes, ownership
  from another user, or sparse allocation. Devices are rejected.
- A new root or supported credential variable requires an explicit host
  save before an older profile can launch. A named profile never fills a
  new root or credential from live host state.
- Full configuration deletion refuses a profile store. Runtime and cache
  cleanup preserve profiles. Recovery copies remain available for review.
