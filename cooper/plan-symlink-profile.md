# Plan: profiles with managed symbolic links

Policy update (2026-09-18): the user requires live profiles to be a mandatory,
visible step in Linux `cooper build`. This replaces the optional conversion
decision D5 below. Build initializes new stores and converts existing copies;
configure's Save & Build uses the same step. See
[build profile verification](dev/build-profile-report.md).

Implementation status (2026-09-17): conversion, live selection, backup/restore,
detach, recovery, retention, CLI, and TUI are implemented. This document remains
the design record. See [the implementation report](dev/symlink-profile-report.md)
for final decisions, deviations, test evidence, and physical-host acceptance
limits, and [the user guide](docs/managed-profiles.md) for commands.

The sections below record the original plan and its baseline behavior.
Use the implementation report for current status. Do not run a manual move
or link conversion on real profiles.

Prepared: 2026-09-17. Code baseline: commit b8a40b6.

## 1. Objective and scope

Make normal profile switching independent of the size of session history.
Keep one writable set of complete state roots for each account profile. Let
the normal host paths refer to the selected set through managed symbolic
links. Docker and VM sessions must continue to mount only the selected
agent's complete roots at the same application paths.

The common load path must read bounded metadata and identity documents,
check state use, and change the host selection. It must not copy or hash
session trees. A first conversion, explicit backup, import, or recovery can
still require a complete copy.

This is a storage and behavior change, not a replacement of one copy call
with a rename call. It changes save, conflict handling, host ownership,
recovery, cleanup, and runtime selection.

### Required outcomes

- A conversation can move between the host, Docker, and VM without a state
  copy during each profile switch.
- Complete directories remain the unit of state. Do not select only auth
  files or omit sessions, plugins, memory, caches inside an agent root, or
  future files.
- A named profile and its active host paths refer to the same writable state.
- A runtime receives no other profile, store index, selector, migration
  journal, or host keyring.
- Account identity, credential environment, path settings, and image account
  checks remain effective.
- A failed conversion or switch does not delete the only remaining data.
- Users who do not use profiles keep ordinary live host mounts.
- The design has an explicit way to restore ordinary host directories.

### Outside this change

Do not add a keyring bridge, account login service, background file watcher,
general filesystem overlay, host mount daemon, or database merge service.
Do not change network policy, CLI versions, workspace paths, image accounts,
or the host-only account-management policy.

Do not promise simultaneous use of one conversation or database from the
host and a VM. Existing application locks and filesystem limits still apply.

## 2. Current behavior and evidence

Read [profiles.md](docs/profiles.md) before implementation. The current
profile is an independent writable copy, not a reference to live host state.

### Current load

[Load](internal/profiles/load.go) does the following:

1. Takes the state lock and recovers an unfinished transaction.
2. Resolves the complete root catalog and checks running state users.
3. Checks the incoming account, root settings, and credential environment.
4. Saves changes from the outgoing host account to its mapped profile.
5. Computes full content digests of the incoming and host trees.
6. If they differ, copies incoming roots beside their host destinations.
7. Checks the copies and old roots, writes a journal, renames old roots into
   recovery locations, and installs the prepared roots.
8. Checks installed state, publishes the selection, and retains recovery.

The final installation already uses renames. The main bulk work comes from
full copies, repeated full-content hashes, and file and directory syncs.
Loading the current profile can still scan complete trees.

[Save](internal/profiles/save.go) compares host state, saved state, and their
common digest. It preserves both sides when they have conflicting changes.
[Capture](internal/profiles/snapshot.go) checks a source before and after a
copy and checks the copy. These checks have a purpose: a live source or
incomplete copy must not silently become the saved account.

[Select](internal/profiles/selection.go) already uses a faster path for a
named runtime. It reads metadata and small identity documents, then mounts
the saved roots directly. It does not hash the complete session trees.

### Measured baseline

An isolated probe on 2026-09-15 called the production profile service with
fake identities. Each account had one 128 MiB file, 256 small session files,
and a small identity file. It ran inside a Cooper VM through virtiofs.

| Operation | Seconds | Logical reads, MiB | Logical writes, MiB |
| --- | ---: | ---: | ---: |
| First save | 1.357 | 513 | 128 |
| Switch; outgoing state unchanged | 3.014 | 1,411 | 128 |
| Load current profile; no changes | 0.912 | 513 | Less than 1 |
| Switch; outgoing state changed | 4.442 | 1,924 | 257 |
| Select named profile for a runtime | 0.003 | Less than 1 | 0 |

These counts are process rchar and wchar from /proc/self/io. They include
cached reads and are not physical disk traffic. The probe excluded real
Docker and process-use discovery. Do not use these times as a physical-host
performance promise.

A same-mount directory rename took 0.085 ms and retained the directory inode.
A symlink replacement took 0.169 ms. An open handle still read the old target,
while a fresh path lookup read the new target.

The temporary probe source was
cooper/.test-tmp/profile-load-probe/main.go. Its output was
/tmp/cooper-profile-load-probe.txt. These files are disposable; the facts and
fixture description above must remain useful without them. Add a maintained
benchmark fixture during implementation.

### Additional checks made for this plan

A disposable Python filesystem probe on 2026-09-17 confirmed:

- Replacing a symlink to a file with a temporary file removes the symlink.
  The former target stays unchanged.
- Replacing a child file through a symlink to a directory keeps the directory
  symlink and changes the child in the target directory.
- Moving a directory can break a relative child link that points outside
  that directory, even when the link text is unchanged.

These are filesystem checks. They do not prove which write operations a
particular harness uses. Native harness checks remain required below.

Minimal reproduction of the standalone-file problem; all data is temporary:

    python3 - <<'PY'
    import os
    from pathlib import Path
    import tempfile

    with tempfile.TemporaryDirectory(prefix="cooper-link-check-") as temporary:
        base = Path(temporary)
        saved = base / "saved.json"
        saved.write_text("old")
        host = base / "host.json"
        host.symlink_to(saved)
        replacement = base / "replacement.json"
        replacement.write_text("new")
        os.replace(replacement, host)
        assert not host.is_symlink()
        assert host.read_text() == "new"
        assert saved.read_text() == "old"
    PY

## 3. Assumptions and decisions

Separate confirmed requirements from proposals. A proposal in this plan is
not evidence that the user approved a change in profile behavior.

### Confirmed requirements

| ID | Requirement | Source |
| --- | --- | --- |
| C1 | Store complete selected-agent roots and preserve account separation. | Repository design and current profile contract |
| C2 | Host, Docker, and VM must use the same effective agent state and paths. | Repository design |
| C3 | Login, logout, and account changes belong on the host. | User direction |
| C4 | Automatic token refresh must write to the shared selected state. | User direction and mounted-state design |
| C5 | Do not expose the host keyring or whole profile store to workloads. | User direction and current mount boundary |
| C6 | Profile data must survive runtime and cache cleanup. | Repository design |
| C7 | Reduce work from large histories during load. | User request |

### Working assumptions to verify

| ID | Assumption | Required check or decision |
| --- | --- | --- |
| A1 | Linux is the first target for the managed mode. | Confirm rollout scope; retain the existing macOS path until tested. |
| A2 | Normal agent writes work through directory symlinks. | R1; test each supported native harness. |
| A3 | Logical host paths remain usable after the move. | R2; test realpath behavior, stored paths, and environment overrides. |
| A4 | Routine save can become identity registration instead of a full backup. | D1; this changes user-visible behavior. |
| A5 | New accounts are created by loading empty state before login. | D2; arbitrary login changes inside a live profile cannot preserve its old credentials automatically. |
| A6 | A managed profile is authoritative writable state, not an immutable snapshot. | D1 and D2; document the loss of implicit snapshot semantics. |
| A7 | The physical host provides reliable local rename and directory-sync behavior. | R6; test actual supported filesystems. |
| A8 | Each managed root can be moved into the store during first conversion. | R3 and R6; external links, bind mounts, and filesystems can prevent this. |
| A9 | Complete root identity can be checked without a content scan on each load. | Check links, manifests, ownership, root type, and small account documents; do not claim to detect arbitrary content edits. |
| A10 | The current use guard can cover all affected aliases and roots. | R8; include other harnesses that share a root. |
| A11 | One global host selection view is small enough to publish on each change. | R7 and benchmarks; bound metadata and never put session entries in it. |
| A12 | Existing profiles can be converted without losing IDs or account mappings. | R10; include both-side conflicts and unfinished schema-1 transactions. |
| A13 | A supported filesystem can preserve the needed state attributes. | R6; inventory modes, ACLs, xattrs, sparse files, hard links, and special files. |
| A14 | A canonical path inside the store need not expose store control data. | R2 and R9; verify exact mounts and path-dependent harness behavior. |
| A15 | A profile store remains at a stable absolute location. | R11; define relocation and detach before the first real conversion. |

### Product decisions before conversion code

| ID | Recommended direction | Decision that remains open |
| --- | --- | --- |
| D1 | Treat managed save as validate/register; make backups explicit. | Decide how much previous automatic recovery behavior must remain. Full backups on every save or load would retain the size cost. |
| D2 | Load a new empty profile before a new login. Block a mismatched mapped account from named use. | Decide the supported recovery flow when a user changes the account inside an already mapped live profile. |
| D3 | Preserve one complete root catalog for each profile. | Select a supported strategy for standalone files; a generic file symlink is not sufficient. |
| D4 | Track host bindings per physical path, including shared paths. | Approve the shared-root behavior in section 7 before enabling overlapping harnesses. |
| D5 | Use an explicit migration with a preview, followed by managed mode. | Decide final command names and whether a later release makes adoption part of first save. Do not migrate on ordinary launch or build. |
| D6 | Keep the previous implementation for stores not yet migrated. | Decide the Linux/macOS release boundary and the period of format compatibility. |
| D7 | Retain migration recovery until an explicit later action removes it. | Decide retention and user-visible backup status. Do not put bulk recovery deletion in normal load. |

Resolve these decisions with concrete research results. Do not ask for a
general approval of unexplained data-loss or compatibility tradeoffs.

## 4. Research required before implementation

Use fabricated state and disposable homes first. Record executable version,
platform, filesystem, commands, expected behavior, actual behavior, and logs.
Do not print credentials or use real logout as a probe without user direction.

Each item needs a short report and a regression fixture when possible. A
documented unknown is not a passing result.

### R1. Native writes through directory and file links

Test each enabled supported version of Claude, Codex, Copilot, OpenCode,
Grok, and Antigravity. Test startup, settings changes, session creation,
resume, token-file replacement, logout, and new login with test accounts or
local fixtures where the native client permits them.

Record use of realpath, lstat, no-follow opens, temporary siblings, rename,
unlink-and-create, directory replacement, and locks. Use file-operation
tracing only on fake credentials; sanitize output.

Standalone roots need separate checks:

- Default Claude settings at ~/.claude.json.
- An external OPENCODE_CONFIG file.
- Optional files and directories that do not exist until a login or
  configuration write. Include mkdir and mkdir-all behavior through links.

Pass condition: writes remain in the intended profile, or an explicit
compatible fallback is designed and tested. Do not mount the complete home
or an unrelated parent directory to make a file write work.

### R2. Logical paths, canonical paths, and resume

The current resolver canonicalizes an explicit CODEX_HOME. After adoption,
that can produce a path inside profile storage instead of the old host path.
Grok also has canonical-home rules. These details affect mount targets and
stored absolute paths, not only source validation.

For every path override in [agentpaths.go](internal/workload/agentpaths.go):

1. Record the host path supplied by the user, the host realpath, the path
   used by the native harness, and the runtime target.
2. Test an unset value, an explicit default, an absolute custom path, a
   relative custom path, and existing user symlinks.
3. Create a conversation on the host; resume in Docker and VM; return to
   the host. Inspect only non-secret path fields needed to explain failures.
4. Test a saved absolute path that refers to the original logical root and
   one that refers to the new physical root.

Preferred result: preserve application-facing paths and resolve only mount
sources. If a native client requires its physical path in both environments,
design an exact additional alias for that selected root and prove isolation.
Do not expose the store parent or silently rewrite conversation databases.

This is a release gate. A symlink layout that breaks resume does not meet
the objective, even if load is fast.

### R3. Symlinks and other links inside a state root

Inventory link text and resolution without reading credential contents.
Classify internal relative links, external relative links, absolute links
back to the logical host root, links between roots, loops, dangling links,
and hard links that reach another profile or an external file.

Moving bytes without changing relative links is not enough. A link that
escapes its original parent can resolve differently inside the store.
An absolute link through the active host path can make an inactive named
profile refer to the account currently active on the host.

Preferred first policy: refuse conversion for an unsupported external alias
with a precise path and reason. Preserve it for the user to fix. Do not follow
it and copy unrelated data. If a path-preserving representation is proposed,
prove both host behavior and runtime isolation before accepting it.

Also test permission-bit, ACL, and hard-link differences between rename
adoption and the old copy behavior. A move preserves aliases that a copy may
have separated.

### R4. Shared and overlapping roots

Codex and Grok both include ~/.agents. Antigravity, Gemini CLI, and the
Antigravity desktop product can use ~/.gemini. Custom environment values can
make other roots overlap.

Build a graph of logical paths, resolved paths, mount identity, and catalog
users. Include existing profiles, current host paths, and profiles in other
Cooper directories. Do not identify an overlap only by the catalog root ID.

Test two profiles of one harness, two harnesses that share a root, an
unmanaged harness that uses a managed path, and nested/aliased overrides.
Use the policy choices in section 7. A shared root must not become a hidden
writable alias between two supposedly independent saved accounts.

### R5. Account changes, refresh, and credential environment

Test stable-identity token refresh, a missing token, expired local tokens,
logout, login to a different account, API-key rotation, organization/workspace
changes, and multi-provider OpenCode identities.

With shared live state, identity validation after logout cannot recover the
previous token. Demonstrate this with fake state and make the product decision
explicit. Do not describe a later identity check as prevention of an earlier
host write.

Keep the existing distinction between unnamed sessions using host credential
resolution and named sessions using captured credential environment. A managed
source must not accidentally make an unnamed launch use named-profile token
rules merely because it now has an associated profile ID.

Pass condition: correct accounts in host, Docker, VM, fresh launch, and
restart; supported refresh updates the same selected state; mismatches do not
silently relabel an existing profile.

### R6. Filesystems, move, durability, and attributes

Check the actual physical host mount layout. Do not infer it from virtiofs
paths in a Cooper VM. Test same-mount rename, separate mounts of the same
underlying filesystem, different filesystems, symlinked parents, read-only
mounts, and roots that are mount points.

A Linux rename can fail with EXDEV between mount points. A shell mv can then
copy and remove. Cooper must select and report its migration method explicitly;
it must not obtain a silent copy fallback by invoking mv.
[Linux rename reference](https://man7.org/linux/man-pages/man2/rename.2.html).

Test directory sync and failures after a successful rename. File sync alone
does not make the parent directory entry durable.
[Linux fsync reference](https://man7.org/linux/man-pages/man2/fsync.2.html).

Record the treatment of ownership, modes, ACLs, xattrs, sparse files, nested
mounts, hard links, sockets, FIFOs, and devices. Do not broaden privilege or
copy a device to make migration pass.

Prefer same-mount rename for eligible roots. For EXDEV, use a reviewed
copy-verify-publish migration and retain the original. Filesystems without
required guarantees must retain the old mode or receive a clear refusal.

### R7. Selector and journal prototype

Prototype the schema and single-selector design in section 5 on fake roots.
Test every failure boundary in section 9, including process termination.
Measure view size with many profiles and bindings.

Compare it with per-root symlink swaps plus a journal. Prefer one selector
because routine switches then have one namespace commit point. Keep the
prototype small; do not introduce a generic transaction framework.

Pass condition: one authoritative selected view, bounded metadata work,
recoverable first adoption, and no claim of an atomic snapshot across two
independent application path lookups.

### R8. Running writers and startup races

Review [HostUsage](internal/profilemanager/usage.go), Linux process discovery,
and the per-UID [state lock](internal/statelock/lock.go).

Test logical aliases, canonical paths, idle running barrels, VM supervisors,
stopped runtimes with restart metadata, an unavailable Docker daemon,
unreadable process state, two Cooper directories, and two runtime namespaces.
Include other harnesses that use an affected shared root.

Cooper's lock coordinates Cooper operations. It cannot stop an arbitrary host
program from starting after a process scan. Preserve the requirement to close
host writers, narrow the mutation window, and state the remaining boundary.
Do not claim a host process scan is an exclusive application lock.

### R9. Docker, virtiofs, and mount isolation

Use real small Docker and prepared VM fixtures. Resolve sources under the
startup lock and pass fixed selected directories, not the host selector path,
to the runtime.

Inspect actual mounts in the physical-host Docker daemon, VM supervisor,
guest, and inner agent container. Test selection changes, root replacement,
normal file writes, reuse, restart, and nested Cooper.
Record rootless Docker, user-namespace mapping, and SELinux/AppArmor settings
where applicable. A move can retain attributes that a copy did not retain;
do not relabel the home or store recursively to hide a mount failure.

Docker bind sources are paths on the daemon host. Nested mounts are included
by default unless configured otherwise; account for that when testing
unexpected submounts. [Docker bind mount reference](https://docs.docker.com/engine/storage/bind-mounts/).

Place a harmless marker in another profile and in store control metadata.
Attempts to read either through direct paths, .. components, a child symlink,
or a canonical-path alias must fail unless that path is separately authorized
by the existing workspace policy. Do not use a workspace containing the
whole store for this isolation proof.

### R10. Existing stores and version compatibility

Build schema-1 fixtures with outgoing host-only changes, saved-only changes,
equal changes, both-side conflicts, unknown identities, pending login,
retained generations, recovery siblings, and interrupted transactions.
Include a changed root-catalog policy and changed credential-variable policy.

Decide how to convert the full store without a second mutable authority.
An older binary must refuse the new format. It must not initialize a fresh
empty index when it does not understand the new format. An older binary's
cleanup path must continue to see a protected profiles entry.

Test mixed binary versions against one store and a second Cooper directory
that points to the same host aliases. Do not let the second configuration
claim or repair links owned by the first.

Before the first external move, install a durable barrier that the current
schema-1 CheckReady path also recognizes. That path checks
profiles/transaction.json. A new journal only under transactions would not
block an older ordinary launcher after the migration process exits. Define
and test the barrier format so old recovery code refuses it without writes.

### R11. Detach, relocation, uninstall, and restore

Test moving the Cooper directory, changing its configuration path, changing
the host home, restoring a backup elsewhere, deleting an inactive profile,
and full configuration cleanup.

Managed links make profile storage part of live host state. The existing
advice to move the profiles directory before configuration deletion is no
longer sufficient. Define a controlled detach or relocation operation first.

An unrelated application removing a host link must not cause Cooper to delete
its replacement or recreate a missing profile as an empty account.

### R12. SQLite and other persistent locks

Keep database, WAL, shared-memory, and journal files together. The current
custom OPENCODE_DB rule already selects its containing directory.

Test database access through logical and canonical paths, settings-file
replacement, clean close and reopen, process interruption, database integrity,
and host-to-Docker-to-VM sequential use. Record the database library versions.
SQLite documents alias and open-file rename hazards; symlink handling by one
library version is not proof for all harnesses.
[SQLite corruption guidance](https://sqlite.org/howtocorrupt.html).

SQLite WAL has shared-memory requirements. A successful sequential resume
test is not evidence that concurrent host/guest database writers are safe.
Test relevant locking and virtiofs behavior separately.
[SQLite WAL reference](https://sqlite.org/wal.html).

### R13. Platform and security API support

Keep the implementation within the repository's Go 1.25 contract. Review
available anchored rename, link, lstat, and sync APIs before writing helpers.
Go os.Root restricts traversal but permits internal relative symlinks and
does not itself prohibit mount boundaries or device files. Those need
explicit policy. [Go 1.25 os.Root](https://pkg.go.dev/os@go1.25.0#Root).

Test Linux first. Evaluate macOS, Docker Desktop file sharing, case-insensitive
filesystems, Unicode paths, and platform keychain limits before enabling
managed mode there. Symlinks alone do not make an unsupported login supported.

### R14. Baseline and performance acceptance

Separate lock wait, use checks, identity checks, metadata preparation,
selector replacement, sync, and any copy/scan time.

Use 0 MiB, 128 MiB, and multi-GiB state trees; also vary file count. Include
many small session files, deep trees, and a closed SQLite database. Keep the
profile and root counts fixed when testing dependence on history size.

Hard requirement: routine managed save/load does not walk, hash, or copy
unchanged session content. Enforce it with instrumented filesystem boundaries
or syscall evidence, not only timing. Proposed user target: under one second
for a normal switch on the reference host when Docker discovery is healthy.
Report slower external discovery separately; this target needs baseline data.

Use warm and cold-process runs without global cache drops. Do not confuse
logical byte counts with host SSD writes.

## 5. Proposed storage model

### One live root set per profile

Keep random profile IDs separate from display names. Preserve existing IDs
during migration. Names and account mappings retain their current rules.

Proposed layout, with illustrative IDs:

    <cooper-dir>/profiles/
      index.json                       # New-format descriptor; old code refuses it.
      current -> views/<view-id>        # Only authority for the selected view.
      views/
        <view-id>/
          state.json                   # Catalog, host selections, root bindings.
          roots/
            <binding-id> -> ../../../harnesses/codex/<profile-id>/live/roots/codex-state
      harnesses/
        codex/
          <profile-id>/
            live/
              roots/
                codex-state/
                shared-agents/
                claude-marketplace/
                cursor-marketplace/
            credentials/
              <revision-id>.json       # Immutable captured environment.
      transactions/
      recovery/

The exact names are proposals. Reuse existing storage helpers where they
fit. The new-format descriptor must not contain a second active-profile map.
The selected view's state.json is the authority for catalog and binding
metadata. Retained old views are metadata records, not backups of live data.

Host aliases for supported directory roots are installed once:

    ~/.codex -> <cooper-dir>/profiles/current/roots/<codex-binding-id>
    ~/.agents -> <cooper-dir>/profiles/current/roots/<shared-binding-id>

A view has one binding for each distinct managed host path. Preparing a new
view changes only the bindings for the requested operation and retains the
others. Replacing current selects all bindings from that view.

This avoids one separate active selector per harness, which cannot by itself
represent a host path shared by Codex and Grok. It also avoids a series of
host-root link swaps on every normal load.

### Metadata model

Use distinct concepts. Do not reuse HostPath for both the public host alias
and a profile's physical storage location.

    Profile
      ID, Harness, Name, AccountIdentity
      HostAccount, RootPolicy, PathEnvironment
      CredentialRevision
      Roots[]: catalog ID, application target, kind, presence rule
      LoginState: pending | bound | mismatch
      RegisteredAt

    HostBinding
      ID
      LogicalPath
      OriginalPathDescription
      ProfileID, RootID
      Kind

    View
      Schema, ID, PreviousViewID, TransactionID
      Profiles[]
      Bindings[]
      HostSelections[]: requested profile and binding status

    Transaction
      Schema, ID, Operation, OldViewID, NewViewID
      ExpectedParentsAndLinks[]
      AdoptedRoots[]
      RecoveryLocations[]

These are design fields, not a final Go API. Use validated enums where a
state is finite. Keep credentials in private files, never in log messages,
runtime labels, view IDs, or non-secret digests.

Captured environment changes create a new private credential revision before
view publication. The new view references it. Do not overwrite a shared
credentials.json before commit: that would change the old view's credential
meaning even if the selector switch failed. Token files written by the
harness remain live application state inside the selected roots.

Root presence must reflect live filesystem state where appropriate. A file
that a harness creates after registration must not stay invisible because an
old manifest recorded it absent.

### Ownership and links

Control directories remain private to the host account: mode 0700 for
directories and 0600 for metadata. Preserve supported state-file modes.

Only known selector and view-entry symlinks are allowed in control paths.
Canonical profile root directories and their parents remain real directories.
Validate a link against the selected view and catalog; a path-prefix match
under profiles is not enough.

Application child symlinks retain their documented state semantics, subject
to R3. They are not permission to follow arbitrary links during copy,
recovery, removal, or mount construction.

Use relative links inside the private store where supported by anchored
operations. Host alias link spelling and relocation behavior must be decided
in R11. Do not use a shell ln command as the transaction implementation.

The host account owns the store and can change its files outside Cooper.
The access boundary here protects workloads through exact mounts; it is not
a security boundary against another process with the same host UID.
Validation must still reject unexpected paths and must not turn corruption
into an automatic overwrite or delete operation.

## 6. Proposed command behavior

### First save and migration

For a user without profiles, first save can eventually adopt eligible host
roots after it identifies the account and presents the storage change.
For the first release, prefer an explicit migration/adoption operation.
Ordinary cli, vm, configure, and build must not move user state.

A migration preview must show roots, copy versus rename, affected shared
paths, required free space, unsupported layouts, and recovery locations.
It must not print credentials. Final command names are D5, not existing CLI
syntax.

### Save an already managed profile

Save reads the selected live roots and small local identity documents.
It verifies the account mapping, path policy, and credential environment.
It registers a pending account or updates small metadata. It does not copy
or hash the full live tree.

Keep the rule that an existing profile name cannot select a save destination.
For a mapped account, the identity determines the mapping.

Do not report a completed backup when only registration occurred. Replace
misleading Saved timestamps with labels that state what was recorded. A
separate backup command or action, if selected in D1, has separate progress,
storage, retention, and result information.

### Load an existing managed profile

Validate both account state and affected host bindings, check state users,
prepare the next metadata view, and replace the selector. Outgoing writes
already exist in that profile's live roots.

Loading the current complete selection is a metadata check and no-op.
It must not compute a full digest or update every file's timestamp.

Loading another account still checks parent-shell credentials. Cooper cannot
change its parent's environment. A file path change must not hide an API key
that overrides the selected account.

### Root-policy changes

New children inside an existing directory remain shared automatically.
A new catalog root requires an explicit checked adoption or profile update.
Do not fill a missing root in an inactive profile from another account's
current host state. Preview the new binding, identity implications, source,
and absence rule. Keep old profiles blocked from incompatible named use
until their metadata and complete root set have been updated.

### Create and bind a new account

Retain the established workflow:

    cooper load antigravity Work
    # Run agy on the host and sign in to Work, then exit it.
    cooper save antigravity

The first command creates empty complete state for a missing profile. It
does not seed the new account with another account's sessions or credentials.
Pending state cannot start a named runtime until local identity is bound.
Loading away from nonempty pending state must preserve it and report the
required next action.

### Unexpected login change inside a mapped profile

A host logout or login writes directly into the mapped profile. The old
independent saved copy no longer exists.

On a mismatch, retain all data, mark the mapping unusable for named launch,
and report the mismatch without tokens. Do not overwrite another existing
profile, silently rename the mapping, or claim the previous account was
preserved. Recovery can require a prior explicit backup or a new login to
the old account.

If the user requires automatic preservation of pre-login state, decide that
before implementation. Symlink switching alone cannot provide it without
another snapshot or a controlled account-change operation.

### Runtime selection

An unnamed runtime uses the effective live host roots and host credential
rules. A named runtime uses that profile's roots and captured credential
rules. Both paths use the same validated mount-selection service.

Automatic file-token refresh writes into the selected root. It needs no
copy-back step. Refresh with the same stable account identity does not create
a new profile.

### Delete, detach, and cleanup

Refuse deletion of any profile referenced by the active host bindings, a
pending transaction, a required recovery record, or a running runtime.
Check actual sources, including ordinary unnamed sessions.

Remove an inactive profile from the authoritative catalog before removing
its unreferenced data. Treat a crash that leaves unreferenced data as
recoverable. Do not let an old retained view become a path to deleted data
through an automatic rollback.

Detach must restore ordinary complete host roots at the original paths.
Use rename when eligible and verified copy when required. Retain enough
state to undo an interrupted detach, and state whether the remaining profile
is an independent copy or no longer managed. Never leave a link pointing at
a store that cleanup will remove.

Normal down, build, runtime cleanup, and VM cache cleanup must preserve live
profiles and host aliases. Full configuration deletion must refuse while
managed roots or unresolved recovery depend on the store. Unknown or corrupt
metadata must not authorize deletion.

## 7. Shared roots and standalone files

These are compatibility gates, not optional follow-up work.

### Shared roots

The root catalog remains unchanged. Each saved profile retains its own
complete state, including its copy of a shared catalog root.

Recommended policy to evaluate: preserve the current effect that the last
host load determines the shared path, but record each binding explicitly.
Close all affected harnesses before a switch. Show a harness as fully loaded
only if all its required bindings belong to that selection.

Example:

1. Codex/Default selects its codex-state and shared-agents roots.
2. Grok/Work is loaded and selects its grok-state and shared-agents roots.
3. Codex/Default still owns its original shared-agents tree, but ~/.agents
   now points to Grok/Work's tree.
4. A named Codex/Default runtime still mounts Codex/Default's complete set.
   An unnamed Codex runtime follows the current host bindings.

Do not label step 3 as a complete host Codex/Default selection. A save in
this mixed state must not silently copy the Grok root into Codex or silently
alias the profiles. Decide whether save refuses with a precise explanation
or requires an explicit import of the shared root. Reloading a complete
selection must preserve the displaced profile's live root.

An alternative is to keep the shared root global across all profiles. That
changes the complete-profile isolation contract and is not selected here.

Until D4 is resolved and tested, refuse the conflicting managed operation
before mutation. Do not claim full rollout completion while supported
Codex/Grok use is still blocked by an unresolved policy.

### Standalone files

A stable directory link generally survives replacement of a child file.
A link at the file path itself can be replaced by the application. File
bind mounts can have a related atomic-replacement problem in runtimes.

Evaluate these options in order:

1. A native supported path layout that puts the file inside an already
   selected complete directory, with unchanged effective host behavior.
2. A file link only when the native writer has verified compatible behavior.
   Include future-version failure detection.
3. A documented narrow copy fallback for standalone files, with conflict and
   recovery rules. This is a hybrid design and requires a decision under D3.
4. Keep the affected layout in the existing copy mode until a compatible
   solution exists.

Do not silently set behavior-changing environment values, expose the whole
home, omit the file, or copy it only on load while losing later host writes.
A hybrid design needs a clear rule for writes from both host and named
runtime, and can still have conflicts.

Test absence as well as presence. A dangling file link may change create,
exclusive-create, existence, and no-follow behavior. Do not use one as an
untested substitute for a missing optional file.

## 8. Runtime paths and access limits

Add a managed-source selection path in the shared service. Do not bypass
ValidateHostOwnedPath by allowing every host symlink below .cooper.

For a registered host alias:

1. Read the authoritative view under the state lock.
2. Validate the alias, binding, profile, root ID, account, and root kind.
3. Obtain the fixed canonical root source from that profile.
4. Pass it as ProfileState with the original application target.
5. Keep host-versus-named credential selection as a separate property.

All direct and indirect launch paths must use this logic: ordinary CLI,
named CLI, ordinary VM, named VM, VM restart, and internal mount preparation.
The current empty-profile-ID shortcuts in profilemanager and vm lifecycle
need special attention.

A managed alias owned by another Cooper configuration must use a checked
owner-store resolution path or receive a clear refusal. It must not fall
through generic host-path handling and bypass that store's recovery barrier.

Do not pass profiles/current as a bind source that is re-resolved later.
Do not mount the store to make a host symlink resolve in the guest. The
guest should receive the selected directory itself at its required path.

[RuntimeDigest](internal/workload/mountplan.go) already uses resolved source
path, device, inode, mounts, and path environment. Retain that behavior and
include any new selection contract fields that affect reuse. Ordinary token
or session writes must not recreate a runtime. A source or root replacement
must prevent stale reuse.

A runtime mount does not become a new profile because a host selector
changes. Continue to reject a switch that conflicts with running users.
Record restart selection independently from the mutable host selector.
Do not let a stopped VM resume on a different account without a checked
mount plan and the existing restart/recreation policy.

Review runtime names and labels separately from credentials. Inferring a
managed profile ID for status must not cause the launcher to load captured
credentials on an unnamed host-state launch.

## 9. Switch transaction and recovery

### Routine switch after conversion

Proposed order:

1. Acquire the existing exclusive per-UID state lock.
2. Recover or refuse any existing journal; do not start a second operation.
3. Read the current view and validate its schema and registered aliases.
4. Validate outgoing and incoming identities and credential scope.
5. Check every affected physical root and logical alias for state users.
6. Create a private new view with complete metadata and relative root links.
   Validate it, sync its files and directories, and retain the old view.
7. Write and sync a journal naming the expected old and new view IDs.
8. Recheck expected selector, alias parents, and affected root identity.
9. Create a temporary selector beside current; rename it over current;
   sync the selector's parent directory.
10. Verify the selected view and finish the journal. Release the lock.

The selector rename is the namespace commit point. The chosen view contains
its catalog and host bindings, so there is no separate active-index update
that can disagree with it.

A single rename does not make two application opens atomic as a group.
Writers must still stop. First adoption and detach change external host
entries and need the wider journal described in section 10.

Routine switch must not recursively sync or hash the live state tree.
It makes routing metadata durable. The harness remains responsible for
durability of its own completed writes. Record this distinction from the
old copy-and-sync behavior under D1 and R6.

### Recovery rules

| Failure point | Required result |
| --- | --- |
| Before a durable journal | Old view stays selected; private incomplete metadata can be removed after validation. |
| Journal exists; selector is old | Roll back prepared metadata or complete an explicitly recoverable operation. Never guess from timestamps. |
| Rename reports failure | Inspect the selector; do not assume it stayed old. |
| Selector is new; parent sync failed | Treat outcome as uncertain until the actual selector and journal are checked; retain both views. |
| Selector is new; journal still exists | Validate new view and finish the committed operation. |
| Selector, parent, or root was changed externally | Refuse to overwrite; retain journal and data and report exact affected paths. |
| Metadata is missing or invalid | Do not create empty replacement profile roots. Require recovery. |
| Cancellation after commit | Report the committed selection and complete required cleanup; do not blindly undo a successful switch. |

Crash tests must terminate a subprocess at each persistent boundary and
restart a fresh service instance. An injected error in one live process is
not sufficient evidence for recovery.

Do not remove the last valid view, unresolved journal, or only root copy
in a best-effort cleanup defer.

## 10. Migration, rollback, and format compatibility

### Before changing host roots

1. Run on the physical host. Keep CheckHost refusal inside ordinary Cooper
   workloads; their process and mount view is incomplete.
2. Recover any schema-1 transaction with the existing code first.
3. Read all profile metadata, previous generations, and recovery records.
4. Resolve the full root and shared-path graph. Check ownership, parents,
   links, mount boundaries, writer use, identities, and path environment.
5. Compute the required one-time comparisons. Classify the live host and
   saved state using the existing conflict rules.
6. Produce a concrete preview and recovery plan. Refuse unsupported layouts
   before changing any host entry.

Do not move an existing profile merely because its display name matches the
host. Account identity and the current mapping remain authoritative.

### Choosing data during schema-1 conversion

| Host/saved state | Conversion rule |
| --- | --- |
| Equal | Select one verified canonical tree; retain the other as migration recovery. |
| Host changed only | Use host state for that mapped account; retain the previous saved state. |
| Saved changed only | Use saved state; preserve displaced host state until conversion is verified. |
| Both changed differently | Keep both and require the existing explicit conflict choice. No database merge. |
| Unknown or mismatched account | Preserve all state and stop; never assign it by last-loaded name alone. |
| Empty pending profile | Preserve pending status and root absence rules. |
| Nonempty unbound profile | Preserve it for binding or recovery; do not treat it as disposable empty state. |

Do not retire the schema-1 conflict code before all migration cases have
fixtures. Its full scans are acceptable for a one-time conversion.

### Moving and publishing

Prepare the new store metadata without changing the old authority. Convert
inactive saved roots to fixed live locations with a recorded mapping. A
rename is not an independent backup; preserve a verified copy where a
rollback promise requires one.

For each live host root, journal its original path, entry type, link text,
parent identity, intended destination, and recovery location before mutation.
Use a narrow anchored rename boundary. For a cross-mount copy, verify the
complete copy while writers are stopped, then retain the original.

Install each permanent host alias without overwriting an unexpected entry.
Sync the relevant parent directories. First adoption is a multi-root
operation, so the journal must restore every original entry after failure.
Do not claim one selector makes initial host-directory moves atomic.

After every alias and new view is valid, publish the new format and selector
with a defined commit record. Test old binary behavior at every intermediate
state. Keep a protected profiles entry throughout.

Prefer a whole-store format conversion initially. If one root layout cannot
be represented safely, leave the store in the old mode. Partial conversion
requires a separate design for overlapping roots and authority; do not add
it as an implicit fallback.

### Rollback and detach

Before commit, restore the original host entries and old store authority.
After successful migration and new agent writes, the old snapshot is stale.
Rollback must preserve or export the current live state before restoring
older state. A binary downgrade is not a data rollback.

Provide a tested detach/export path before general adoption. It must restore
complete root sets, include SQLite companion files, handle EXDEV, reject
busy state, and recover from failure. It must not delete migration recovery
without an explicit later action.

Document that manually moving profiles, deleting current, or running an old
binary cannot be used as a recovery procedure.

## 11. Code boundaries and likely changes

Confirm exact APIs during implementation; these are the current owners.

| Area | Existing files | Planned responsibility |
| --- | --- | --- |
| Root catalog and overrides | [agentpaths.go](internal/workload/agentpaths.go) | Keep one catalog; separate logical target rules from managed source resolution. |
| Path checks | [hostpaths.go](internal/workload/hostpaths.go), [profilepaths.go](internal/workload/profilepaths.go), [hostpath](internal/hostpath/resolve.go) | Validate only registered managed aliases; retain rejection of arbitrary overlap and traversal. |
| Profile model and store | [types.go](internal/profiles/types.go), [store.go](internal/profiles/store.go) | New format, views, bindings, identity state, and strict decoding. |
| Save/load | [service.go](internal/profiles/service.go), [save.go](internal/profiles/save.go), [load.go](internal/profiles/load.go) | Managed metadata operations; retain bounded identity checks. |
| Copy and recovery | [copy.go](internal/profiles/copy.go), [snapshot.go](internal/profiles/snapshot.go), [transaction.go](internal/profiles/transaction.go) | Migration, explicit backups, detach, and fault boundaries; remove bulk work only from normal managed switching. |
| Selection | [selection.go](internal/profiles/selection.go), [profilemanager service](internal/profilemanager/service.go) | Resolve managed ordinary and named sources through one policy. |
| Use checks | [usage.go](internal/profilemanager/usage.go), [usage_linux.go](internal/profilemanager/usage_linux.go), [usage_darwin.go](internal/profilemanager/usage_darwin.go), [statelock](internal/statelock/lock.go) | Cover physical roots, aliases, shared users, startup, restart, and separate configurations. |
| Mount policy | [mountplan.go](internal/workload/mountplan.go), [types.go](internal/workload/types.go) | Exact source ownership, fixed targets, safe directory preparation, reuse digest. |
| Docker | [barrel.go](internal/docker/barrel.go), [cleanup.go](internal/docker/cleanup.go) | Consume selection; verify actual mounts and stopped-runtime reuse. |
| VM | [lifecycle.go](internal/vm/lifecycle.go), [metadata.go](internal/vm/metadata.go), [supervisor.go](internal/vmhost/supervisor.go) | Fixed exports, checked restart, durable profile selection, no host socket. |
| Credentials | [profileauth](internal/profileauth), [launch session](internal/launch/session.go) | Keep identity and host/named credential rules separate from mount ownership. |
| CLI and cleanup | [profile_commands.go](profile_commands.go), [main.go](main.go), [vm_commands.go](vm_commands.go) | New operation entry points and truthful results; protect managed state during cleanup. |
| Application and TUI | [app profiles](internal/app/profiles.go), [TUI profiles](internal/tui/profileui) | Present service results, migration state, account mismatch, shared-root status, and backup status. |
| Documentation | [README](README.md), [profiles](docs/profiles.md), [home paths](docs/home-paths.md), [Antigravity](docs/antigravity.md) | Explain live state, host-only login, migration, recovery, cleanup, and unsupported cases. |

Add focused helpers or files within the existing packages. Avoid a second
profile engine inside Docker, VM, or TUI code. Follow
[AGENTS.TUI.md](../AGENTS.TUI.md) for any presentation change.

The implementation must also update [AGENTS.md](../AGENTS.md) where it
describes copied profiles and the blanket Cooper-directory overlap rule.
Describe the narrow managed-root exception without weakening ordinary
host-state and cleanup protection.

## 12. Implementation sequence

### Phase 0: research and behavior decisions

- Complete R1-R14 with a checked report for each required target.
- Resolve D1-D7. Update this plan with selected behavior and rejected options.
- Prototype root links, view selection, and file replacement with fake data.
- Reproduce the baseline and create deterministic performance fixtures.

Exit: no unresolved assumption about supported file roots, shared roots,
account changes, path parity, or recovery is hidden behind implementation.

### Phase 1: format and ownership

- Define the new descriptor, immutable views, bindings, and transaction data.
- Write strict parsing, bounded metadata, catalog checks, and version refusal.
- Add anchored link operations with narrow injectable failure boundaries.
- Implement registered-alias validation and safe source resolution.
- Protect profiles in cleanup before enabling any host adoption.

Exit: forged links, wrong owners, changed parents, missing roots, and unknown
schemas fail before mutation. Existing unmanaged behavior still passes.

### Phase 2: selection and runtime integration

- Add managed ordinary and named selections in the shared service.
- Keep credential source mode explicit.
- Update Docker, VM, restart, mount validation, and directory preparation.
- Retain effective paths, complete roots, runtime reuse checks, and isolation.

Exit: fake managed roots work in both execution modes with no store-wide
mount and no stale source after a checked selection change.

### Phase 3: transaction and recovery

- Implement view preparation and the single-selector commit.
- Implement journal recovery with old/new/unknown outcome checks.
- Add cancellation and subprocess-termination tests at every boundary.
- Implement metadata retention without bulk state cleanup on normal load.

Exit: every injected failure keeps a coherent recoverable selection and all
only copies. Routine switching has no full-tree I/O.

### Phase 4: command semantics

- Implement managed save, load, new pending profile, and identity mismatch.
- Implement the selected shared-root and standalone-file policies.
- Add exact errors and results for mixed bindings, busy state, and corruption.
- Preserve CLI/TUI service parity and credential environment handling.

Exit: the existing account workflow works under the approved new semantics;
all behavior changes have tests and plain user documentation.

### Phase 5: migration and detach

- Implement read-only preview and schema-1 conflict classification.
- Implement one-time move or verified copy with external-entry journaling.
- Preserve IDs, identities, credentials, pending state, and recovery.
- Implement rollback before commit and detach/export after adoption.
- Test old/new binary interactions and full cleanup refusal.

Exit: all migration and reverse-path fixtures pass before real host adoption.

### Phase 6: interface and documentation

- Update command help and output so save is not described as a backup.
- Show actual host selection, partial shared bindings, pending login,
  mismatched identity, and migration/recovery state.
- Keep long work in the service; keep TUI views pure.
- Update user guides, repository design rules, and operator recovery steps.
- Capture and inspect the changed TUI states.

Exit: users can identify which account and roots are active and can follow
recovery instructions without knowledge of internal filenames.

### Phase 7: acceptance and release

- Run the required suites and prepared runtime checks below.
- Run the physical-host native-client matrix and performance measurements.
- Test one deliberate migration interruption and successful recovery on
  disposable host state.
- Complete a real host migration only after the fixture gates pass.
- Run the full VM release gate before the release; record any limitation.

Do not tag or publish a release merely because unit tests pass.

## 13. Test matrix

Add tests at the lowest layer that proves each behavior. Use real temporary
files, fake accounts, and injected external boundaries. Do not write tests
that only repeat the implementation's own chosen path strings.

| Group | Required cases and assertions |
| --- | --- |
| Basic state | First adoption; Default and Work; fresh pending profile; bind login; same-profile no-op; switch away and back; new nested files persist. |
| Complete roots | Every catalog root; absent optional roots; cache roots; marketplace roots; custom database parent and companion files; future children. |
| Path settings | Unset versus empty values; custom home; custom Cooper directory; relative and absolute overrides; spaces; Unicode; symlinked home; pre-existing user root links. |
| Canonical paths | Explicit CODEX_HOME; native realpath; stored absolute paths; image binary outside state; host/Docker/VM conversation resume in both directions. |
| File roots | Normal overwrite; temporary-file rename; unlink/recreate; exclusive creation; no-follow open; absent-to-present transition; host and runtime changes. |
| Internal links | Internal relative link; external relative link; absolute active-root link; cross-root link; dangling link; cycle; hard links; changed link during validation. |
| Shared roots | Codex/Grok shared-agents; .gemini users; named versus ordinary launch; mixed host bindings; save after another harness load; runtime using an overlapping root. |
| Identity | Same account; stable refresh; missing identity; malformed file; logout; different account; account returns; API-key rotation; organization change; new name collision. |
| Environment | Named captured values; captured unset values; conflicting parent-shell API key; host token discovery; no secret in output, view IDs, logs, labels, or mount digest. |
| Host use | Known native writers; scripts that open test files; unreadable process environment; process exit during scan; Docker unavailable; source aliases; multiple namespaces. |
| Cooper concurrency | Save versus load; two loads; delete versus launch; startup versus switch; shared and exclusive lock timeout; another Cooper directory claiming a host path. |
| View publication | New view valid before publish; current changes once; unrelated bindings retained; metadata limit; unknown field; wrong schema; corrupted selector; missing view. |
| Crash recovery | Terminate before/after journal sync, each adoption move, each host alias, selector rename, directory sync, and finalization; fresh-process recovery is repeatable. |
| Error handling | ENOSPC, quota, permission denied, read-only filesystem, EXDEV, EIO, failed directory sync, changed parent, wrong file kind, unexpected destination. |
| Migration | Equal data; only host changed; only saved changed; both changed; unknown account; pending state; old transaction; recovery siblings; same/different filesystems. |
| Migration attributes | Modes; supported ACL/xattr behavior; sparse files; hard links; sockets and FIFOs; devices refused; nested mounts; roots that are mount points. |
| Format compatibility | Old binary on new descriptor; new binary on old store; interrupted format commit; missing descriptor; second config; root-catalog and credential-policy changes; rollback after new writes. |
| SQLite | Closed database with WAL; fake interrupted writer; logical and canonical opens; integrity_check; sequential cross-boundary use; no file-only database move. |
| Docker | Named and ordinary selected roots; fixed mounts; unchanged-write reuse; replaced-root refusal/recreation; idle running container; stopped container; rootless/user-namespace and security-label settings where supported; other-profile marker inaccessible. |
| VM | Same targets and UID/GID; supervisor export limits; guest and inner Docker mounts; profile restart; changed root; missing root; named account unchanged by host selection. |
| Nested use | Profile mutation still refused inside ordinary Cooper workloads; nested launch sees only the outer session's approved roots and proxy; no physical-host Docker socket. |
| Delete and cleanup | Selected profile; partially selected shared profile; runtime source in use; pending journal; corrupt metadata; normal down/build/cache cleanup; full config removal; symlink cannot redirect recursive deletion. |
| Detach and relocation | Same-mount move; EXDEV copy; partial failure; resumed detach; changed store path; restore to another path; no dangling host root; current writes retained. |
| Performance | Large bytes and many files do not add normal load/save tree reads; named selection remains fast; no-op bounded; no background cleanup reading old state; report discovery separately. |
| UI | Pending, managed, mixed, busy, mismatch, migration progress, recovery, error, empty list; cancel; switch tabs during work; resize; no filesystem work in View. |

Include negative isolation tests with marker files rather than real secrets.
Do not stop the user's current VM, move its mounted roots, or change real
accounts to make a test pass.

## 14. Validation commands and evidence

This plan is a documentation change. Do not run the full release gates merely
to validate this file. The following commands apply to implementation.
Run from the repository root and keep full logs.

Start with the required VM development unit boundary:

    ./cooper/test-vm-dev.sh unit > /tmp/cooper-vm-unit.txt 2>&1

During implementation, run targeted tests for profiles, workload,
profilemanager, hostpath, statelock, launch, Docker, and VM as affected.
Run meaningful race checks for transaction/selection and startup locking.

Finish implementation validation with the repository gates:

    go test -C ./cooper ./... > /tmp/cooper-go-test.txt 2>&1
    timeout 90m ./cooper/test-e2e.sh > /tmp/cooper-e2e.txt 2>&1
    timeout 90m ./cooper/test-docker-build.sh all > /tmp/cooper-docker-build.txt 2>&1
    go build -C ./cooper -o ./cooper . > /tmp/cooper-build.txt 2>&1

Use Go 1.25 or the required newer repository toolchain. Keep cooper visible
in command text. Do not clear shared caches to obtain a timing result.

Prepare the selected agent once, then use its prepared runtime inputs:

    ./cooper/test-vm-dev.sh prepare-agent codex > /tmp/cooper-prepare-codex.txt 2>&1
    ./cooper/test-vm-dev.sh parity codex > /tmp/cooper-parity-codex.txt 2>&1
    ./cooper/test-vm-dev.sh profiles codex > /tmp/cooper-profiles-codex.txt 2>&1
    ./cooper/test-vm-dev.sh prepare-agent antigravity > /tmp/cooper-prepare-antigravity.txt 2>&1
    ./cooper/test-vm-dev.sh parity antigravity > /tmp/cooper-parity-antigravity.txt 2>&1
    ./cooper/test-vm-dev.sh profiles antigravity > /tmp/cooper-profiles-antigravity.txt 2>&1

Extend the fixtures to include managed host aliases, migration, refresh,
selection change, and restart. Existing saved-profile tests alone do not
prove the new ordinary-launch path.

For each other supported harness, add host/Docker parity and the prepared VM
checks appropriate to its roots. Runtime checks must not hide image builds,
downloads, or exports. Follow [the VM development guide](dev/README.md).

For TUI changes, use the tui-visual-qa skill and inspect fixed small and large
screens in addition to behavior tests.

Before a Cooper release, run the full gate on a suitable physical Linux host:

    timeout 120m ./cooper/test-vm.sh > /tmp/cooper-vm-gate.txt 2>&1

Record the commit, versions, physical host, filesystem, commands, log paths,
pass/fail results, and remaining limitations. A skipped host check is not
a passed check. Follow the repository release-preview instructions for any
later tag or publish action.

## 15. Completion criteria

- [x] Product decisions D1-D7 are recorded with their implications.
- [ ] Research R1-R14 has evidence and all supported-layout blockers are closed.
- [x] Existing schema-1 stores remain usable until explicit conversion.
- [x] Host aliases and new-format metadata have one checked authority.
- [x] Normal managed load/save makes no complete history copy or digest pass.
- [x] Complete selected roots and effective path behavior match across boundaries
  in the prepared Docker/VM checks for all six agents.
- [x] Shared roots and standalone files have a tested, documented policy.
- [x] Account mismatch and host credential overrides cannot silently select
  another account.
- [ ] Refresh updates the selected state without host keyring access.
- [x] Runtime mounts expose only selected roots and required existing features.
- [x] Journal, root rename, selector, sync, and completion checkpoints have
  recovery tests, including process exits during migration, restore, and detach.
- [x] Detach/export and post-migration rollback preserve current writes.
- [x] Cleanup preserves live data even when metadata or links are invalid.
- [ ] Native host acceptance, prepared Docker/VM tests, and performance checks pass.
  Prepared Docker/VM tests and performance checks pass. Authenticated native
  acceptance on the physical host remains open; see the implementation report.
- [x] User docs, command help, TUI text, and repository design rules match the
  implemented semantics.
- [x] The release report distinguishes metadata rollback, content backup, and
  unsupported concurrent use.

## 16. Execution notes

Do not weaken path checks first and add managed ownership later. Do not
assume a successful basic symlink test proves native harness compatibility.
Do not convert the current agent's real mounted state from within its VM.

Keep research reports and fixtures with the implementation so a later
maintainer can see why a path rule or refusal exists. Prefer small functions
for preview, validate, prepare, publish, recover, and detach. Keep copy logic
available for the operations that still need it.

This plan authorizes no staging or commit action. The previous one-time
staging exception applied only to the 55 files committed in b8a40b6.
