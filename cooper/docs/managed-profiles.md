# Live profiles on Linux

Live profiles remove the history-size cost from normal account switches. Each
profile owns complete state directories. Host paths point through one selector
to the selected directories. `load` changes that selector; it does not copy or
hash session history. Docker and VM mounts use fixed directory sources and
keep the original application paths. They also provide exact compatibility
paths for native session records that contain resolved directory names.

On Linux, `cooper build` always prepares live profile storage. The required
`Setting up live profiles...` step also runs from configure's Save & Build.
There is no skip flag or storage-mode setting. Existing saved profiles are
converted as one store before image builds start. A new installation gets an
empty live store; the first save registers the chosen harness's host state.
Later builds check metadata and root links without copying or hashing history.

## Build and convert a store

Run these commands on the physical host. Exit host agents, other applications
that share their state, and related Cooper runtimes before the first conversion.
Keep each executable outside its state roots. An empty store needs no login
and does not move any host root during build.

```bash
cooper build
cooper profiles
```

Build checks accounts, paths, writers, links, file kinds, free space, and
conflicts under the mutation lock. A failed profile step stops the build before
Docker image builds start. The output gives the recovery record after conversion.
Already converted stores can be checked while an agent runs because this check
does not replace roots.

`cooper profiles migrate --dry-run` remains an optional inspection command.
It reads the proposed conversion without changing state. If both host and
saved state changed, inspect them, then use `cooper profiles migrate --conflict host`
or `cooper profiles migrate --conflict saved` and retry the build. This resolves
a conflict; it does not enable an optional feature. The choice applies to
conflicting profiles across that migration. Use the same `--config` value as build.

An inner Cooper build can initialize an empty local store. It refuses to
convert existing profiles because a container or VM cannot check all physical
host writers. Run that build on the physical host.

Migration makes one verified copy of each profile. It retains the old saved
generations and renames original host roots to recovery siblings. This costs
space and time once, but gives an independent original on either filesystem.
It avoids a cross-filesystem move that silently becomes a partial copy.
Normal loads do not repeat this work. Initial copies can expand sparse files;
leave space for their logical size, directory entries, and metadata.

Keep the same Cooper directory, user account, home, and path environment.
Migration does not change OS accounts or require an image rebuild.
All profiles of one harness must use the same root paths and path settings.
Migration refuses nested roots or a shared path with different root rules.

## Save and switch

```bash
cooper load codex Work
# Log in to Work on the host, then exit Codex.
cooper save codex
cooper load codex Default
```

A missing name creates empty complete roots. `save` binds the login to that
pending profile. After binding, `save` checks the current account and captures
supported credential variables. Directory writes are already in the profile.
It does not make a history backup. A normal load checks outgoing and incoming
accounts, saves any standalone file changes, and selects the incoming roots.

Run profile changes from outside every selected state root, including on the
first save. This also applies to worktrees inside `.codex` and paths reached
through symlinks. Replacing a root from inside it would leave the shell in
the retained recovery directory, where later relative writes would be lost
from the live profile.

Always load a new empty profile **before** changing accounts. If you log out
or select a different account inside an existing live profile, you change its
only live credentials. Cooper refuses an account mismatch. Sign back into
the original account, or restore an independent backup. A retained selector
or an old migration copy cannot undo all later account changes.

Stable OAuth refresh remains in the selected directory. It does not change a
profile's account when stable identity fields remain the same. No host
keyring, session bus, or Docker socket is added by this feature. The existing
file-auth rules for Antigravity and other harnesses still apply.

A named session uses its profile's captured credential environment. An
ordinary session uses the host environment. A conflicting host key can stop
an ordinary managed launch or a host load; set or unset the reported variable
locally. Cooper does not print its value or change the parent shell.

## Shared roots and standalone files

Every profile retains its complete root catalog. For a shared host path such
as `.agents`, the last load owns that host binding. For example, loading Grok
can make the Codex host selection partial. Codex's independent profile root
still exists. The Profiles tab shows `Shared roots differ`; its details name
the owner of each host root. Reload Codex to select all its roots before save.
An ordinary launch sees the actual host bindings. A named launch sees all the
named profile's roots. Switching a shared root is refused while a runtime or
known host writer uses that root.

Standalone files such as `.claude.json` stay regular files on the host.
Applications can replace a file through temporary-file rename, so a generic
file symlink is not a reliable contract. Cooper uses checked copies for these
files, with a 16 MiB limit per file. Active named sessions use the same host
file; inactive named sessions use the profile file. Save and load reconcile
changes using a common digest. If both copies differ, `--conflict host` or
`--conflict saved` selects one and retains the other for recovery. This small
file work does not scan any directory history.

Do not run concurrent host and runtime writers against the same state. A
bind-mounted standalone file cannot guarantee visibility after a native host
writer replaces that file. Directory child replacements, including token
refresh and SQLite WAL files, stay within the shared complete root.

## Independent backup and restore

Exit writers, then choose a new absolute backup directory outside the store
and all state roots:

```bash
cooper profiles backup "$HOME/cooper-backup-2026-09-17"
cooper profiles restore codex Default "$HOME/cooper-backup-2026-09-17"
```

Backup copies all complete profiles, active standalone files, and captured
credentials into a private copy-mode store. It refuses an existing destination
and verifies source and copied contents before publication. Keep this folder
private: it contains login credentials. A failed backup keeps its private
`.cooper-backup-<id>` staging folder for inspection; it is not a published
backup.

Restore replaces one existing profile with the same stored account and
profile ID. It checks backup contents and identity before publication. It
retains replaced roots, including unexpected login changes, in recovery
siblings. It replaces directory entries at the existing physical paths, so
native session records and the runtime mount count stay stable. Compatibility
links from previous conversions continue to reach the restored roots.
If that profile supplies host bindings, they select the restored
state. Other profiles keep their selections. Restore does not infer account
identity from a profile name.

## Detach and move the store

```bash
cooper profiles detach
```

Detach copies the latest complete live roots back to ordinary host paths. It
also writes independent copy-mode profile snapshots, including current
standalone files. Replaced live roots and migration recovery remain.
Host directories no longer depend on the selector. Detach is for recovery or
relocation; the next Linux build converts the store to live profiles again.
Detach requires all related writers to stop and has the same interruption
recovery as migration.

Native clients can record resolved paths in their databases. Codex does this
for session files when `CODEX_HOME` resolves through a link. Cooper keeps each
former physical root path as a compatibility link, including after detach.
These links point to ordinary host roots in copy mode. Keep their parent
directories and the old store location; removing them can break native
conversation resume even though the session bytes still exist. Prune removes
retained data but preserves these required links. Cooper does not rewrite
native databases to remove absolute paths.

For relocation, detach first, make an independent copy of the detached
`profiles` directory, and retain the old store. Place that copy under the new
Cooper directory. Use the same host account, home, and path settings. Inspect
with `cooper --config /new/cooper profiles`, then run `cooper --config /new/cooper build`.
Do not move a live store, change `current` by hand, or downgrade a binary as a
substitute for detach. A changed root catalog requires the previous compatible
Cooper version to detach before the catalog update.

## Interrupted operations and recovery

```bash
cooper profiles recover
```

New launches and mutations stop while a managed journal exists. Recovery
validates exact registered paths, metadata, parent device/inode pairs, staged
entries, and use checks. Before selector publication it restores the old
entries. After publication it finishes the committed operation. It never
chooses an arbitrary old view as a rollback target. Repeated recovery is safe.

Initial setup writes a durable copy-mode catalog before it creates live data
or views. If setup stops before its journal exists, that catalog remains
authoritative and the next build can retry. Prepared views alone do not select
an account or permit host root replacement. Recovery reports a missing or
damaged catalog even when no journal exists; it does not invent an empty store.

If a parent, entry, alias, or journal changed outside Cooper, recovery stops.
Keep the complete store and all `.cooper-managed-<id>-<number>-before` and
`-next` siblings. The record under `profiles/recovery/<id>/managed.json`
identifies those entries. Restore the original path settings and stop writers
before retrying. Do not delete a journal to force a launch.

If a native application replaced a host directory link, Cooper refuses to use
that replacement silently. Keep both the replacement directory and the
profile data. Restore the exact registered link only after moving the
replacement to a safe location. Do not delete the replacement to fix the
warning. A missing or corrupt descriptor does not create an empty store.

## Retention, deletion, and cleanup

Migration originals, file conflict copies, and pre-restore live generations
remain until an explicit cleanup. They are recovery data, not current backups.
Old view and credential metadata also remain until this command:

```bash
cooper profiles backup "$HOME/cooper-backup-before-prune"
cooper profiles prune-recovery --yes
```

Prune checks recorded sibling paths and entries before deletion, removes
unreferenced generations and metadata, and leaves current profiles and required
compatibility paths intact.
It refuses changed or unrecognized recovery data. It can do substantial disk
work. It never runs as part of save or load. Copy-mode recovery from before
conversion can remain for manual review.

Deletion refuses selected, partially selected, busy, or recovery-dependent
profiles. After required recovery is explicitly removed, deletion removes
the catalog entry before deleting unused data. A crash can leave unreferenced
data; it cannot make the catalog point to deleted credentials.

Normal down, build, and runtime/cache cleanup preserve live state. Full config
removal permits only the exact unused empty store created by build. Saved
profiles, unknown entries, and corrupt metadata stop full removal. Detach and
retain required compatibility paths before intentional config removal. Do not
remove live profiles as if they were disposable caches.

## Supported layouts and limits

Conversion supports Linux, private local profile storage, complete directory
roots, and regular standalone files. It preserves content, read/write/execute
permission bits, and file and directory modification times. Internal relative symlinks retain their
text. It rejects escaping/absolute child links, link cycles, hard-linked state,
foreign ownership/group, ACLs/xattrs, root mount points, nested mounts, and
special devices. Resolve these layouts before building. Sockets and
FIFOs are transient transport and are omitted. Sparse file contents survive;
sparse allocation, set-ID/sticky bits, access times, and symlink timestamps are
not preserved.
macOS stores remain in copy mode.

Only exact registered root sources enter Docker or virtiofs. Each recorded
physical path receives the same selected root as its public path. Historical
paths cannot expose another account. For a workspace below state, its protected
`.git/hooks` directory remains read-only at the public and canonical paths, regardless of
which path starts the session. Each state mount needs its own hook overlay
because another bind of the same root does not inherit that limit. VM state
exports also enforce this limit outside the guest, where guest root cannot
remove it by mounting a virtiofs tag again. The selector,
catalog, sibling profiles, credential revisions, and parent store stay outside
the workload. Root contents can change without runtime recreation. A source
path or inode change changes the runtime mount digest and requires the normal
runtime replacement path.

Compatibility paths count toward the VM's existing limit of 40 total mounts.
Normal save, load, and restore do not add paths. Re-conversion or relocation
can add them. Required native paths are retained during recovery cleanup.

A workspace can be a child of a selected state directory, such as a Codex
worktree. Cooper resolves that child to the selected profile before mounting
it. This does not permit a workspace in a sibling profile or a control
directory. Run profile changes from outside all state roots so the command
does not switch or remove its own working directory.

The per-UID lock coordinates Cooper commands across configurations. Use
checks inspect actual Docker mounts, known native harnesses, and visible open
file descriptors. Unrelated non-dumpable processes can hide descriptors.
Arbitrary native writers do not take Cooper's lock and can start after a check.
Close them first. This is not a filesystem snapshot or a general process lock.

The automated and native probe evidence, and the remaining physical-host
acceptance work, are recorded in [the implementation report](../dev/symlink-profile-report.md).
