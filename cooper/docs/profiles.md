# Account profiles

A profile is a writable copy of one harness's complete state roots. It includes
credentials stored in those roots, configuration, sessions, history, plugins,
memory, and new files added by the harness. Profiles use the same root catalog
as live host mounts. Cooper does not select individual session files or change
paths inside a database.

Use profiles to keep personal, work, and test accounts separate. The Linux or
macOS account used to build the image stays the same. Switching an AI account
does not require an image rebuild.

## Start with two accounts

Run profile management commands on the physical host. Close that harness and
stop runtimes that use its state first, including idle Docker barrels and VMs.
Use the Runtimes tab in `cooper up` to stop them. Docker must be available so
Cooper can check actual mounts.

1. Log in to the personal account with the host harness, then exit it.
2. Save the first profile. Its name is always `Default`:

   ```bash
   cooper save codex
   ```

3. Start a fresh work profile:

   ```bash
   cooper load codex Work
   ```

   Cooper saves the outgoing account first. If `Work` does not exist, Cooper
   creates empty state and marks it as awaiting login. The personal settings,
   sessions, and credentials are not used to start the new account.
4. Log in to the work account with Codex on the host, then exit it. Bind and
   save that account:

   ```bash
   cooper save codex
   ```

5. Switch back when needed:

   ```bash
   cooper load codex Default
   ```

Each load saves changes to the outgoing account. Switching back restores its
complete saved state. Loading the current profile does not discard new host
sessions. A new profile cannot start a named runtime until its login is bound
by `save`.

These examples use Codex. The harness choices are `claude`, `codex`, `copilot`,
`grok`, and `opencode`. The supported login forms are listed below.

## Use the host or a saved profile

| Command | State source |
| --- | --- |
| `cooper cli codex` | Live host roots |
| `cooper vm codex` | Live host roots |
| `cooper cli codex Work` | Saved Work roots, read-write |
| `cooper vm codex Work` | The same saved Work roots, read-write |

A named profile changes only the mount sources. The home, workspace, mount
targets, and path environment stay the same. Thus, a stored absolute session
path still points to the correct location. The complete profile store and
other profiles are not mounted into the runtime.

Changes made in a named session are saved directly in that profile. They do
not appear in the host harness until `cooper load` loads the profile. A named
session does not change the host's selected profile. Two profiles can have
separate runtimes in the same workspace. VM restart keeps its profile selection.
The Runtimes details and session title show the profile name.

The workspace, Git configuration, Cooper settings, proxy rules, and developer
caches remain shared according to the normal mount policy. Profiles are an
account-state feature, not a separate workspace or network policy. Do not run
two copies of the same conversation at once. The harness's own file and
conversation locks still apply.

## Save without choosing the wrong destination

`cooper save <harness>` reads the current local account identity. It does not
trust the last-loaded marker alone, and it does not accept an existing profile
name as a destination. For example, `cooper save codex Default` is invalid.

- A known account is saved to its existing profile.
- A new account asks for a new, unused name. In a script, use
  `cooper save codex --name Playground`.
- A fresh pending profile binds the new login. If that login already belongs
  to another profile, Cooper refuses the save.
- Missing or unsupported identity stops the operation. Cooper keeps a recovery
  copy when it cannot safely associate the host state with an account.

Names start with a letter and contain at most 40 letters, digits, underscores,
or hyphens. Lookup is case-insensitive. `Work` and `work` name the same profile.
`--name` can create a profile only for an unmapped account; it cannot overwrite
another named account. On load, `--name` names an unmapped **outgoing** account.

The host marker records the last successful save or load. It is not proof of
the account currently selected after a manual login. Account checks run again
before a save and against the copied credentials before publication. A local
identity check does not contact the provider or prove that a token is valid.
An expired login may still need renewal on the host. Renew it, exit the
harness, and save again.

## When both copies changed

Cooper records a common state digest when it saves or loads. It compares the
host, saved profile, and common state before replacing a profile:

- Only host changes: save the host state.
- Only saved changes: keep them; loading that profile brings them to the host.
- Equal changes: record the shared state without another full copy.
- Different changes on both sides, or no common state: keep both and ask for
  an explicit choice.

Resolve a conflict with `--conflict host` or `--conflict saved` on the original
save or load command. The choice applies to the outgoing profile. For example:

```bash
cooper load codex Default --conflict saved
```

The host conflict copy is kept in recovery storage. Replacing the saved profile
keeps its previous generation. Cooper does not merge databases or directories.
Read the command's recovery path before removing any copy. A plain save with
only saved changes leaves the host as it is; use load to put that state on the
host.

## Supported local identity

Identity adapters read small, bounded credential/configuration documents.
They never query an account service or run the harness. Malformed, ambiguous,
and unsupported credentials fail without putting token contents in errors.

| Harness | Recognized identity | Important limits |
| --- | --- | --- |
| Codex | ChatGPT user plus account/workspace ID from saved tokens; OpenAI API credential | File auth with the default OpenAI provider. Keyring/automatic/ephemeral stores and custom config profiles/providers require separate support. |
| Claude | Linux OAuth account UUID plus organization UUID, with stored access/refresh credentials; Anthropic API credential | Keychain OAuth on macOS, external OAuth tokens/helpers, and third-party cloud modes are not copied as a supported login. |
| Copilot | Host plus login from a stored plaintext token; supported token environment | OS keychain state is outside the root snapshot. A file login must explicitly use plaintext storage or disable keytar. |
| OpenCode | Sorted provider identity set; API credentials; OAuth records with stable account IDs | An opaque OAuth token without a stable account ID cannot safely select a profile. External cloud/helper credentials need separate support. |
| Grok | Stored scope set and stable user/organization IDs for Grok/OIDC; API credentials | External auth providers, arbitrary auth-file paths, and legacy web-login records are not supported profile identities. |

OAuth token refresh keeps an identity when the stable account identifiers stay
the same. Organization/workspace IDs separate enterprise and personal accounts
where the harness exposes them. API credentials use a fingerprint; Cooper
cannot infer that two different keys belong to the same person. Key rotation
therefore creates a new identity. OpenCode can store several providers at once;
adding or replacing a provider changes that complete identity set.

The initial fixtures cover Codex 0.117.0, Claude 2.1.87, Copilot 1.0.12,
OpenCode 1.3.7, and Grok 1.0.4. A future login format needs an adapter test before
Cooper can use it to replace an existing profile. Normal host-state launches
remain available for login modes that profiles cannot identify.

### Credential environment

Named sessions use the profile's captured supported credential and provider
variables. A captured unset value stays unset. Host shell discovery, Cooper's
normal token cache, and custom barrel environment cannot fill these values
from another account. Credentials are passed at exec time; they are not put
in container labels or VM metadata.

Cooper cannot change the environment of the parent shell. Before host load,
current supported credential variables must match the target profile. The
error names the variable to set or unset without printing its value. A fresh
profile requires those variables to be unset. File-based logins make host
switching simpler. Named Docker/VM sessions can use captured environment
credentials without changing the host shell.

Project `.env` files, custom provider variable names, external credential
commands, and OS keychains are outside this supported credential catalog.
A copied config can still refer to such an external source. Keep those sources
consistent with the selected account. Profiles do not copy arbitrary files
outside the supported roots or make arbitrary authentication plugins portable.

## Manage profiles in the TUI or CLI

The Profiles tab in `cooper up` uses the same service as the commands:

| Key | Action |
| --- | --- |
| Up/Down | Select a profile and its harness |
| `h` | Choose the harness for a host save or new load |
| `s` | Save that harness's host state |
| `n` | Enter a name to load or create |
| Enter | Load the selected profile |
| `d` | Confirm deletion of an unused profile |
| `i` | Read the full account, saved time, and last result; scroll long errors |
| `r` | Refresh the list and use status |

Name, conflict, and delete forms keep their own keyboard input. A completed
operation still updates the screen if you switched tabs. Normal shutdown
cancels operations and waits for copy cleanup or load rollback.

Use `cooper profiles` to list accounts and
`cooper profiles delete codex Work` to delete an unused profile. Scripts must
use `--yes` for deletion. The profile selected on the host and profiles mounted
by running containers cannot be deleted. Load another profile and stop related
runtimes first. Deletion removes that saved profile and its retained generation;
it does not remove host roots or unresolved recovery copies.

## Storage, recovery, and cleanup

Data lives under `<Cooper directory>/profiles`. Store directories have mode
0700; metadata and captured environment files have mode 0600. State files keep
their permission bits. Each save writes a new generation before it publishes
one atomic index update. It retains one previous generation. Unchanged saves
do not rewrite complete roots.

Host load stages each incoming root beside its destination, on that root's
filesystem. It writes a journal, moves original roots to recovery siblings,
installs all staged roots, then publishes the host selection. Failure before
commit rolls back. The next save/load/delete command recovers an interrupted
load before it starts new work. New sessions in that Cooper configuration are
blocked while a journal remains. Reuse the same Cooper directory and path
settings after an interruption; a different configuration is not a recovery
command.

A successful load keeps the previous host state and a record in
`profiles/recovery/<id>/host.json`. The record names the original roots; their
copies remain in `.cooper-profile-<id>-<root>-before` siblings. A later successful
load removes the older host recovery only after it validates paths and content.
Conflicts and unknown identities have separate complete copies under
`profiles/recovery/<id>/roots`, with `snapshot.json` descriptions. They are
retained for manual review. They contain credentials and are not export logs.

If data, parent directories, or the journal changed during recovery, Cooper
refuses to overwrite them. Keep the journal, index, and all copies. Restore the
original path settings and close writers before retrying the operation. Do not
delete a journal to bypass the check. For a manual recovery, first back up the
whole store and the recorded siblings; restore complete root sets together.
In particular, keep a SQLite database with its journal/WAL files.

The copy preserves file bytes, directory structure, symlink text, and permission
bits. It does not follow symlinks or create writable hard-link aliases. Sockets
and FIFOs are process transport and are omitted. Devices are rejected. It does
not preserve inode numbers, ownership from another user, times, extended
attributes, or sparse allocation. Sparse file bytes are preserved. A symlink
to an external path still needs that path to be available under normal mounts.

Cooper's per-UID lock coordinates its own launches and profile operations,
including other runtime namespaces. Use checks inspect actual Docker mounts
and known host harness processes. Other tools do not take this lock. Close
writers before copying; copy digests detect concurrent changes but do not
turn live databases into an atomic filesystem snapshot.

`down`, runtime cleanup, VM cache cleanup, and normal builds preserve profiles.
Full configuration removal refuses a Cooper directory with a `profiles` entry.
Move the complete profile store to a safe location before intentional full
configuration deletion. Review recorded host recovery siblings too.

## Extend and test the feature

`internal/workload/agentpaths.go` is the one root catalog. New children of an
existing root need no change. Add a new root there and test its source, target,
absence, and override rules. Old profiles then require an explicit host save
before named use. No new root is silently mounted from live host state.

`internal/profiles` owns storage, copying, account mapping, conflicts, recovery,
and selection. It uses injected identity/use checks and narrow copy, rename,
and publication boundaries for fault tests. `internal/profileauth` owns only
local login recognition and the supported credential variables.
`internal/profilemanager` connects those packages to the real host. Docker and
VM consume the same selection and `internal/launch` owns session credentials.
The TUI knows only the application service interface.

Start with `./cooper/test-vm-dev.sh unit`. This includes the profile packages
without Docker or VM startup. The tests use real small files, fabricated
credentials, injected I/O failures, and a Python SQLite/WAL fixture when
`python3` is available. The command integration test uses isolated Docker
barrels. For the VM boundary, prepare Codex once and run:

```bash
./cooper/test-vm-dev.sh prepare-agent codex
./cooper/test-vm-dev.sh profiles codex
```

This checks Docker-to-VM saved state, all Codex roots, credentials, restart,
status, and cleanup. It starts two VMs and performs two guest image imports;
restart is the reason for the second. It does not rebuild or export a host
image during the runtime check. The full release gate stays separate.

The deterministic screen fixture is
`cooper tui-test --screen profiles --profile-scenario populated`. Other fixed
scenarios are `empty`, `unmapped`, `conflict`, `busy`, and `error`. They never
read or write real account state.
