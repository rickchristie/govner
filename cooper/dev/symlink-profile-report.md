# Managed profile implementation report

Policy update (2026-09-18): Linux build now requires live-profile setup.
The original optional-conversion choice below is superseded by the user's
instruction. See [build profile verification](build-profile-report.md) for the
change and its later test results. The results below describe the initial
2026-09-17 implementation; standard `/tmp/` suite log names can be reused.

Date: 2026-09-17. Development environment: a Cooper Codex VM on Linux x86-64.
All conversion experiments use fabricated accounts and disposable state.
The running user VM and real host roots have not been converted.

## Decisions from the plan

| Decision | Implemented behavior and reason |
| --- | --- |
| D1: save and backup | Managed save validates/binds identity and captures supported credential variables. Live directory writes already persist. `profiles backup` makes independent copies; this keeps normal save/load independent of history size. |
| D2: account changes | Load a fresh profile before login. Refuse a different identity in a mapped live profile. Restore the original login or use `profiles restore` with an independent backup. Retain the replaced live generation. |
| D3: complete roots and files | Keep every catalog root. Directory roots use links. Standalone files use checked copy reconciliation, capped at 16 MiB. Claude followed links in the native probes, but generic temporary-file rename can replace a file link. |
| D4: shared roots | One binding per public root path. The last load owns a shared host binding. Show partial selections and each root's owner. Refuse save of a partial selection. Named sessions use their own complete roots. |
| D5: migration | Superseded on 2026-09-18: Linux build always initializes or converts the store before building images. Preview and manual conflict repair remain available. Launch does not perform conversion. |
| D6: format/platform | Schema-1 copy stores remain readable. A schema-2 descriptor plus one immutable selected view is authoritative after conversion. Linux conversion only; macOS keeps copy mode. |
| D7: recovery | Retain migration originals, file replacement copies, old views, and replaced live roots. `profiles prune-recovery --yes` removes checked recovery and unused metadata separately. Normal load does no bulk cleanup. |

The implementation keeps the existing private generation directory shape for
live data. A generation stops changing its path after conversion. This avoids
a second runtime source validator for a literal `live` subdirectory. Credential
updates have separate immutable revisions, and selectors carry the active
catalog. Old selector metadata is not an independent content backup.

Native Codex 0.154.0 records the resolved physical rollout path when
`CODEX_HOME` points through a link. A negative test removed that physical
path and reproduced `no rollout found`. Each directory root now records its
physical locations. Restore replaces directory entries at their existing
paths; it does not add an alias or VM mount on each restore. Detach retains
links to the ordinary host root. Re-conversion or relocation preserves the
former physical names as links to the new roots. Both runtimes mount only
the selected root at these exact names. They do not receive a store parent,
index, or another account. Read-only hook overlays apply to every alias.
Relocation must retain the old compatibility paths. This avoids rewriting
native databases but prevents removal of all old store directories.
Aliases count toward the existing 40-mount VM limit. Normal switches and
repeated restores keep the path count fixed; re-conversion and relocation can
increase it.

Migration uses a verified copy even on one filesystem. This is a deliberate
change from the proposed move optimization. It keeps original copy-mode
snapshots and host roots independent until the user removes recovery. It also
uses the same procedure across filesystems. The normal switch still moves
only selector metadata. Detach also copies so that interruption cannot remove
the last live generation.

## Research and evidence

| Plan item | Finding and coverage |
| --- | --- |
| R1: native writes | Codex 0.154.0 native MCP registration writes through a directory link. Claude 2.1.87 native MCP registration follows its settings link, including a missing target. OpenCode 1.3.7 creates and migrates its SQLite database through the linked data root. Copilot 1.0.12, Grok 1.0.4, and Antigravity 1.2.2 pass offline startup probes. Real account writes for these three remain a host acceptance task. |
| R2: logical/canonical paths | Custom `CODEX_HOME`, a user root alias, spaces, Unicode, and ordinary/named target parity have filesystem tests. Selected-profile worktrees are allowed; sibling profiles and control directories are refused. Profile changes require a working directory outside state roots. Native Codex with a local model fixture resumes the same thread after switch, restore, detach, copy-mode switch, prune, re-migration, and relocation. Runtime mounts include only recorded canonical aliases of the selected roots. Authenticated physical-host resume remains an acceptance task. |
| R3: internal links | Conversion checks child links without following them during copy. Internal relative links can remain. Escaping/absolute links, cycles, and hard-linked state are refused before host replacement. New host aliases and private control links have separate validators. |
| R4: shared roots | Codex/Grok fixtures prove one `.agents` host binding, partial selection reporting, save refusal, and reload. Each named profile keeps its complete root set. |
| R5: identity/refresh | Tests cover mapped identity mismatch, pending login, named versus host credentials, desktop session-bus observations, and restore of an unexpected changed account without deleting that changed state. Prepared Antigravity tests use fabricated token replacement; this is not a provider OAuth request. |
| R6: filesystem/attributes | One-time copy verifies source before/after and copied content. Host entry replacement stays within its parent filesystem. Parent inode/device checks protect recovery. Content, read/write/execute permission bits, and file/directory modification times are retained. Foreign ownership, ACLs/xattrs, nested/root mounts, and devices are refused. Sparse contents survive but allocation can expand; set-ID/sticky bits, access time, and symlink timestamps are outside the copy contract. |
| R7: selector/journal | Fault tests stop at journal, every Codex root installation, selector publication, selector sync, and completion. A child process exits without defers at migration checkpoints and between every root rename during Claude restore/detach, including physical compatibility paths. Restore/detach recovery is repeated. File switch, ENOSPC at selector rename, and changed-parent refusal also have tests. |
| R8: writers/races | Cooper uses the existing per-UID shared/exclusive lock. Mode is rechecked after acquiring it. Use checks inspect actual runtime mounts, known native processes, and visible open state files. Tests include an arbitrary shell writer. Unrelated hidden descriptors and writers that start after a check cannot be made safe by this advisory lock; users must stop writers. |
| R9: runtime isolation | Both runtimes use the shared resolved mount input. Managed sources remain exact private generation roots. No selector/store parent or sibling account is mounted. Prepared parity tests convert ordinary host roots for all six agents. Codex seeds a native canonical session path before Docker/VM resume. Prepared Codex/Antigravity profile tests cover restored named roots, old canonical paths, other-account marker refusal, restart, credentials, and cleanup. All use fabricated stores. Results are recorded below. |
| R10: formats | Legacy tests stay in place. New tests reject missing/old descriptors with a live selector, unknown fields, redirected selector/root links, public control parents, and another Cooper directory. A reproduced failure covered loss of both descriptor and selector: retained views or credential metadata now prevent creation of an empty store. Root-catalog changes refuse use; detach with the previous compatible version first. |
| R11: detach/restore | Tests prove latest directory/file writes survive backup, restore, detach, and a subsequent copy-mode switch. Backup publication refuses a destination created during the copy. Interrupted restore/detach recovers on either side of publication. Restore requires the same account and profile ID. Native Codex resume proves relocation with retained compatibility links. Replaced roots remain in journaled recovery siblings until explicit prune. |
| R12: SQLite | A Python fixture exits after a committed WAL write without closing the database. Switching away/back preserves WAL state; both logical and canonical opens return `integrity_check = ok` and the committed row. Existing complete-copy WAL tests remain. Concurrent SQLite writers across host/guest are not supported. |
| R13: platform/API | Uses Go 1.25 anchored roots, no-follow bounded metadata reads, private ownership checks, Linux mount/attribute checks, and the existing runtime source validator. It does not expose a broader host path to compensate for links. |
| R14: performance | Six 20-switch runs are recorded below, with a 2 GiB sparse session and zero or 5,000 extra history files. A copy hook refuses any history copy. A separate Linux inotify check first proves that reads are observable, then proves that save, switch, and same-profile load never open the watched history file. The benchmark uses a fake use guard; real Docker/process discovery is additional work. |

Relevant operating-system contracts are documented in the
[rename manual](https://man7.org/linux/man-pages/man2/rename.2.html),
[fsync manual](https://man7.org/linux/man-pages/man2/fsync.2.html), and
[Go 1.25 os.Root documentation](https://pkg.go.dev/os@go1.25.0#Root).
The runtime and database limits also follow
[Docker bind-mount behavior](https://docs.docker.com/engine/storage/bind-mounts/),
[SQLite file alias hazards](https://sqlite.org/howtocorrupt.html), and
[SQLite WAL limits](https://sqlite.org/wal.html).

## Reproducible checks

Use the cached Go 1.25 toolchain where the default Go is older. Run profile
unit suites and real runtime checks in sequence because they share the
per-UID state lock. Use `GOFLAGS=-p=1` for the full Go suite to keep Docker
package setup from waiting inside another package's test time limit.

```bash
./cooper/test-vm-dev.sh unit > /tmp/cooper-vm-unit.txt 2>&1
GOFLAGS=-p=1 go test -C ./cooper ./... > /tmp/cooper-go-test.txt 2>&1
timeout 90m ./cooper/test-e2e.sh > /tmp/cooper-e2e.txt 2>&1
timeout 90m ./cooper/test-docker-build.sh all > /tmp/cooper-docker-build.txt 2>&1
go test -C ./cooper -race ./internal/profilelink ./internal/profiles ./internal/profileauth ./internal/profilemanager ./internal/workload > /tmp/cooper-managed-race.txt 2>&1
go test -C ./cooper ./internal/profiles -run '^$' -bench '^BenchmarkManagedSwitch$' -benchtime=20x -count=3 > /tmp/cooper-managed-benchmark.txt 2>&1
COOPER_NATIVE_PROFILE_CODEX=/opt/cooper/npm/bin/codex go test -C ./cooper ./internal/profiles -run '^TestManagedNativeCodexResume$' -count=1 -v > /tmp/cooper-native-codex-managed.txt 2>&1
./cooper/dev/test-profile-links.sh > /tmp/cooper-native-profile-links.txt 2>&1
./cooper/test-vm-dev.sh prepare-agent codex > /tmp/cooper-managed-prepare-codex.txt 2>&1
./cooper/test-vm-dev.sh parity codex > /tmp/cooper-managed-parity-codex.txt 2>&1
./cooper/test-vm-dev.sh profiles codex > /tmp/cooper-managed-profiles-codex.txt 2>&1
./cooper/test-vm-dev.sh prepare-agent antigravity > /tmp/cooper-managed-prepare-antigravity.txt 2>&1
./cooper/test-vm-dev.sh parity antigravity > /tmp/cooper-managed-parity-antigravity.txt 2>&1
./cooper/test-vm-dev.sh profiles antigravity > /tmp/cooper-managed-profiles-antigravity.txt 2>&1
for agent in claude copilot opencode grok; do
    ./cooper/test-vm-dev.sh prepare-agent "$agent" > "/tmp/cooper-managed-prepare-$agent.txt" 2>&1 || exit
    ./cooper/test-vm-dev.sh parity "$agent" > "/tmp/cooper-managed-parity-$agent.txt" 2>&1 || exit
done
go build -C ./cooper -o ./cooper . > /tmp/cooper-build.txt 2>&1
```

Native probes use already prepared images, no network, no host mounts, and
fresh fake homes. OpenCode reports that `models.dev` cannot be reached in this
probe; local database migration still completes. Codex reports that it will
not create PATH helper aliases below `/tmp`; its config write still completes.
These messages are not a successful provider request.

The native Codex conversation test uses a real installed binary and a local
HTTP model fixture with a fixed answer. It uses fabricated identity files,
checks the same thread ID, and requires the prior assistant turn in each
resumed request. It needs no provider key. The final local run completed in
48.12 seconds, including relocation. The negative missing-path evidence is
in `/tmp/cooper-codex-session-probe.txt`; the successful lifecycle test is in
`/tmp/cooper-native-codex-managed.txt`.

The cross-filesystem fixture uses `/tmp` on fuseblk and `/var/tmp` on overlayfs
in this VM. It verifies migration and detach contents across those filesystems.
This is Linux 6.8.0-136-generic; it is not an NFS, SELinux, or rootless Docker
acceptance result.

The final benchmark used six runs of 20 switches. Both fixtures included a
2 GiB sparse session added after conversion:

| Extra history files | Switch times, ms | Median, ms |
| --- | --- | ---: |
| 0 | 87.00, 86.88, 85.59 | 86.88 |
| 5,000 | 87.19, 88.56, 88.81 | 88.56 |

These are filesystem/service times inside this VM. They exclude real use
discovery and do not promise the same wall-clock time on a physical host.

The final unit boundary and race checks passed after the stable-path restore
change. The Linux inotify history check passed. Linux and Darwin builds passed.
The CLI migration and restore help commands were also checked.

One E2E retry stopped at the Copilot image commit with `no space left on
device`; the daemon filesystem had only 715 MiB free. The reviewed Docker
build-test cleanup removed its own mirror/pinned images and restored 5.5 GiB
free space. The failure log is `/tmp/cooper-e2e-no-space.txt`. The final retry
results are recorded below. No release gate, tag, or publication is implied
by this development report.

## Final suite results

| Check | Result | Log under `/tmp/` |
| --- | --- | --- |
| Full Go suite | Passed: 58 packages; six other packages have no tests. Main package 478.44 s; application package 435.77 s. | `cooper-go-test.txt` |
| Full Docker E2E gate | Passed: 390 checks, zero failures. Test cleanup completed. | `cooper-e2e.txt` |
| Docker build gate, all modes | 173 checks passed and one failed. Mirror and pinned each passed 86 checks. Latest refused Antigravity 1.2.5 because only 1.2.2 has a reviewed browser driver. | `cooper-docker-build.txt` |
| VM unit boundary | Passed with Docker and QEMU blocked. | `cooper-vm-unit.txt` |
| Profile, identity, use, and mount race tests | Passed for all five selected packages. | `cooper-managed-race.txt` |
| Native Codex lifecycle | Passed with Codex 0.154.0 and a local model fixture, 48.12 s. | `cooper-native-codex-managed.txt` |
| Native link probes | Passed with the limited per-agent coverage in R1. | `cooper-native-profile-links.txt` |
| Linux development build | Passed. | `cooper-build.txt` |
| Darwin build | Passed; conversion stays Linux-only. | `cooper-managed-darwin-build.txt` |
| TUI visual checks | Profile list at 100×28 and details at 80×24 inspected. | `cooper-managed-profiles-final.png`, `cooper-managed-profile-details-final.png` |
| Documentation and source format | Local document links, shell/Python syntax, and `git diff --check` passed. | Checked directly. |

The latest-mode refusal is the same version check observed before the final
profile changes. This work does not add a driver for Antigravity 1.2.5 or
change that check. The complete Docker build gate is therefore **not passed**,
and this report is not release approval. Reviewed mirror/pinned test cleanup
completed after the gate and left 5.8 GiB free. Two unused Go-test agent image tags were also removed,
after checking all containers, to keep the 30 GiB Docker filesystem from
filling during pinned builds. No active runtime image was removed.

## Prepared Docker and VM results

All eight runtime commands passed on the final source digest
`2093c516caa072df6a1d414e3b6720ad2905a1082faceb35c34be7e4d4688ee3`.
They started ten disposable VMs. Each command used prepared inputs, exported
no host image, removed its runtime, and kept the real development VM running.
Times below include the test wrapper and cleanup.

| Agent | Command | Seconds | Report under `/tmp/` |
| --- | --- | ---: | --- |
| Codex | `parity codex` | 108.02 | `cooper-vm-dev-parity-bab2b0c379c3.json` |
| Codex | `profiles codex` | 179.24 | `cooper-vm-dev-profiles-8a5c3cebec5a.json` |
| Antigravity | `parity antigravity` | 94.28 | `cooper-vm-dev-parity-8d5dce60b561.json` |
| Antigravity | `profiles antigravity` | 160.97 | `cooper-vm-dev-profiles-1e95f2bdfd7b.json` |
| Claude | `parity claude` | 88.93 | `cooper-vm-dev-parity-e55a359a7efe.json` |
| Copilot | `parity copilot` | 91.49 | `cooper-vm-dev-parity-3e9323f55efc.json` |
| OpenCode | `parity opencode` | 87.66 | `cooper-vm-dev-parity-83299890b14c.json` |
| Grok | `parity grok` | 87.31 | `cooper-vm-dev-parity-e3cb828d7209.json` |

These runs used Codex 0.117.0, Antigravity 1.2.2, Claude 2.1.87,
Copilot 1.0.12, OpenCode 1.3.7, and Grok 1.0.4. The separate native Codex
lifecycle test uses 0.154.0. Test image tags were removed after all runtime
checks, with ownership and active-container checks. Prepared base files,
archives, and reports remain available. This freed space for the full suites.

## Physical-host acceptance before general adoption

Use a disposable host account or test home first. Keep the current development
VM and its real state unchanged. For each installed supported native version:

1. Record the version and effective root settings. Save two test profiles,
   preview conversion, and convert only that test store.
2. Load a fresh profile, log in on the host, exit, and save. Start the host
   harness again and confirm the same account. Check that its root links remain.
3. Continue a test conversation from host to Docker, Docker to VM, and back.
   Check messages, configuration, plugins, and any stored absolute file paths.
4. Cause or wait for a real token refresh on the same account. Confirm the
   refreshed file is visible on the host and in the selected profile. For
   Antigravity, keep the existing file-auth wrapper and desktop keyring running;
   do not forward its bus to either runtime.
5. Exit each writer before switching. Confirm other-profile marker files are
   absent from the runtime. Check a shared `.agents` selection in the TUI.
6. Make an independent backup. Add test state, restore the backup, and confirm
   the replaced state remains in recovery. Detach and confirm ordinary roots
   contain the latest selected state. Retain the backup before any prune.

A real host conversion, authenticated resume, provider refresh, SELinux or
rootless Docker acceptance, and the full VM release gate remain separate from
the disposable development tests. Linux build now performs conversion
automatically. Use the disposable host checks above before the first build
against production profiles.
