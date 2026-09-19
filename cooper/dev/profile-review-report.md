# Live profile review fixes

Date: 2026-09-18. Follow-up to the
[required build setup](build-profile-report.md).

## Findings and changes

All three review findings were valid.

1. First save resolved new roots without the working-directory check used by
   copy-mode commands. The managed store could not check those roots because
   they were not registered yet. First save now checks each resolved root
   with the same guard as copy mode, before copying or replacing it.
2. Initial build setup could create `views/` before it had an index or a
   transaction journal. A later build then treated the store as damaged.
   Migration now writes and syncs the authoritative copy-mode catalog before
   creating any live data or views. A process exit before the journal leaves
   a valid catalog for retry. Recovery also validates the catalog when no
   journal exists; it cannot report success for missing managed metadata.
3. Live profile tests called Linux-only migration on macOS. All managed test
   files and the benchmark now have Linux constraints. The managed SQLite
   case moved to its own Linux file. Copy-mode tests, including committed
   SQLite WAL copying, remain enabled on macOS.

The working-directory tests reproduced misplaced relative writes before the
fix. They now check six layouts: the root itself, a nested worktree, a workspace
link, a state-root link, a custom root, and a shared root. Refused saves must
make no copies, retain the same directory, and leave later relative writes
at the original path.

Initialization tests stop real subprocesses at seven boundaries: the initial
catalog, partial view directories, a complete view, the journal, selector
replacement, selector sync, and transaction finish. Recovery runs twice, then
build and first save must succeed without moving unsaved host state during
setup. A separate test verifies refusal when managed metadata remains but the
descriptor and journal are absent. Recovery does not guess an empty catalog
for a store that has lost its authority.

## Verification

The source digest is
`c1c87415c60325fcf46164d4f668e6f967a051fdd641fbe2c722a90413953970`.
It excludes Markdown reports.

| Check | Result | Log under `/tmp/` |
| --- | --- | --- |
| Failure reproduction | Confirmed working-directory movement, unrecoverable pre-journal setup, and false recovery success before the fixes | `cooper-profile-review-reproduction.txt` |
| Focused Go packages | Passed: profiles, profilelink, profilemanager, buildflow | `cooper-profile-review-targeted.txt` |
| Full Go suite | Passed: 58 tested packages and six packages without tests; `GOFLAGS=-p=1` schedules packages separately | `cooper-go-test.txt` |
| Full E2E suite | Passed: 390 checks, zero failures; cleanup completed | `cooper-e2e.txt` |
| Docker build matrix | 173 checks passed; latest mode failed at the existing Antigravity 1.2.6 driver guard. Mirror and pinned each passed 86 checks | `cooper-docker-build.txt` |
| Docker build cleanup | Passed | `cooper-docker-build-clean.txt` |
| Race tests | Passed: profiles and profilelink | `cooper-profile-review-race.txt` |
| VM unit boundary | Passed with Docker and QEMU blocked | `cooper-vm-unit.txt`, `cooper-vm-dev-unit-f770810125a1.json` |
| Codex VM preparation | Passed; reused the prepared guest base with no VM start | `cooper-profile-review-prepare-codex.txt`, `cooper-vm-dev-prepare-agent-bf8b00350835.json` |
| Prepared Codex profiles | Passed in Docker and VM, including restart and cleanup; two disposable VMs | `cooper-profile-review-vm-codex.txt`, `cooper-vm-dev-profiles-fefbc2242ca3.json` |
| Darwin test selection | Only copy-mode profile tests remain enabled | `cooper-profile-review-darwin-files.json` |
| Darwin ARM64 test compilation | Passed | `cooper-profile-review-darwin-compile.txt` |
| Linux development build | Passed | `cooper-build.txt` |
| Darwin ARM64 application build | Passed | `cooper-profile-review-darwin-build.txt` |
| Final source and documentation | Go format, local links, `git diff --check`, and source digests passed | Checked directly |

Darwin compilation checks do not substitute for running tests on macOS.

The prepared profile test passed in 164.26 seconds. Its report matches the
source digest above and records two VM starts, two guest image loads, no host
image export during the runtime check, and completed runtime removal. It checks
complete roots, canonical session paths, host and credential isolation,
Docker-to-VM continuation, restart, status, and profile retention during cleanup.

The complete Docker build gate remains **not passed**. Latest mode selects
Antigravity 1.2.6, but its browser driver is not reviewed; only 1.2.2 is
supported. That check stops template generation before profile setup. No
version check was changed for this review.

The first full Go and E2E attempts exhausted the VM's 30 GiB Docker filesystem.
Both logs contain `no space left on device`. Two Go packages also exceeded
the test process deadline. The E2E run was stopped after its build failure.
The failure logs are retained as `cooper-profile-review-go-disk-full.txt`,
`cooper-profile-review-e2e-disk-full.txt`, and
`cooper-profile-review-go-only-disk-full.txt`.

Cleanup used the Docker build gate's scoped `clean` mode after checking that
no container used those images. It also removed only exited build containers
whose IDs appeared in this run's logs. No global prune ran. Subsequent Docker
suites run separately to limit peak disk use.

A second Go run passed the main package but reached the process deadline in
the application and Docker packages. The application package waited 7 minutes
30 seconds for the shared Docker test lock. The Docker package was still
waiting when Go stopped it after 11 minutes. That log is retained as
`cooper-profile-review-go-package-timeout.txt`. The successful full Go run used
`GOFLAGS=-p=1` with the required command to start one package at a time. This
keeps lock waiting from consuming another package's test deadline.

After the full Go pass, cleanup checked every container's image ID before
removing that run's unused `cooper-gotest-` image references. The log is
`cooper-profile-review-go-image-cleanup.txt`. This left 7.3 GiB free before
the E2E retry.

After the prepared profile test, cleanup checked the cache owner, daemon,
exclusive lease, recorded image IDs, and all container references. It removed
only that Codex fixture's image tags and manifest. Prepared guest bases,
archives, and verification reports remain. The log is
`cooper-profile-review-vm-image-cleanup.txt`.
The final Docker filesystem had 7.3 GiB free. Only the original development
agent and a pre-existing stopped container remained. No Git changes were staged.

These tests use temporary homes and fabricated accounts. They do not convert
real profiles or complete physical-host OAuth acceptance. The full VM release
gate remains a separate release check.
