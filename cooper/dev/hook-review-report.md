# Profile workspace hook protection

Date: 2026-09-19. Follow-up to the
[profile review fixes](profile-review-report.md).

## Cause and change

The P1 finding was valid. The mount plan added read-only Git hook overlays
only for canonical aliases. When the launch workspace was already canonical,
the public state target still exposed the same hooks through a writable bind.

The plan now checks every selected state directory, including its primary
target. It maps the hook source into each target and adds one read-only
overlay per distinct path. Existing target ordering keeps parent mounts before
these overlays. Both managed profiles and ordinary linked state use this rule.

The VM supervisor also protects hooks inside each state export that contains
the workspace. Guest root can mount a virtiofs tag again without guest-side
overlays. The read-only limit must therefore apply before virtiofs exports the
state, as it already did for the workspace export. Unrelated state exports
do not receive workspace files. A running VM keeps its existing exports until
restart; the new limits apply when its supervisor is created again.

## Regression coverage

The shared-plan regression failed before the fix for a canonical launch.
The real Docker regression then confirmed that a hook file could be created
through the public path. With the fix, both public and canonical launches
must reject hook creation, replacement, rename, and removal. The test covers
ordinary and named profiles and verifies that workspace files remain writable.
It uses a temporary home and fabricated credentials.

Plan tests also cover historical canonical paths and ordinary linked state.
They retain the rejection checks for standalone aliases, parent storage, and
sibling accounts. A separate VM test confirms that every state export has
the required read-only overlay; it failed before the export fix.

The prepared profile test starts the VM from a canonical worktree. It checks
hook operations through all public and recorded paths, then checks writes
directly at each host-side export. The same checks run after restart. This
adds no VM starts to the existing profile test.

## Verification

The source digest is
`7f3f2bee12d3b142f567bae9f4733e04362873b6dc0d0d264b42a5699d4b0fa1`.
It excludes Markdown reports. Logs are under `/tmp/`.

| Check | Result | Log |
| --- | --- | --- |
| Shared-plan reproduction | Canonical launch failed before the fix | `cooper-hook-review-reproduction.txt` |
| Docker reproduction | Canonical named launch could create a public hook before the fix | `cooper-hook-review-docker-reproduction.txt` |
| Linked host-state reproduction | Both launch paths rejected the required overlays before the validation fix | `cooper-hook-review-host-link-reproduction.txt` |
| VM export reproduction | All three state exports lacked hook overlays before the fix | `cooper-hook-review-vm-export-reproduction.txt` |
| Focused packages | Passed: workload and VM | `cooper-hook-review-targeted.txt` |
| Real Docker access | Passed: public and canonical launches, ordinary and named profiles | `cooper-hook-review-docker.txt` |
| Race tests | Passed: workload and VM | `cooper-hook-review-race.txt` |
| VM unit boundary | Passed with Docker and QEMU blocked | `cooper-vm-unit.txt`, `cooper-vm-dev-unit-c30742eb2e1d.json` |
| Full Go suite | Passed: 58 tested packages and six packages without tests; `GOFLAGS=-p=1` avoids cross-package Docker lock waits | `cooper-go-test.txt` |
| Codex VM preparation | Passed; cached guest base, no preparation VM start | `cooper-hook-review-prepare-codex.txt`, `cooper-vm-dev-prepare-agent-bc8c49c33615.json` |
| Prepared Codex profiles | Passed: public and canonical hook operations, host-side export writes, restart, isolation, and cleanup; two VM starts | `cooper-hook-review-vm-codex.txt`, `cooper-vm-dev-profiles-f05b4d841c21.json` |
| Prepared Codex parity | Passed: account, workspace, version, complete selected state, isolation, writes, and cleanup; one VM start | `cooper-hook-review-parity-codex.txt`, `cooper-vm-dev-parity-35d0396166b5.json` |
| Full E2E suite | Passed: 390 checks, zero failures; cleanup completed | `cooper-e2e.txt` |
| Docker build matrix | Mirror and pinned each passed 86 checks; latest failed at the existing Antigravity 1.2.7 driver guard; 173 checks passed in total | `cooper-docker-build.txt` |
| Docker build cleanup | Passed | `cooper-docker-build-clean.txt` |
| Darwin ARM64 application build | Passed | `cooper-hook-review-darwin-build.txt` |
| Darwin ARM64 workload test compilation | Passed | `cooper-hook-review-darwin-compile.txt` |
| Linux development build | Passed | `cooper-build.txt` |
| Final source and documentation | Go format, `git diff --check`, report source digests, and local links passed | Checked directly |

The complete Docker build gate remains **not passed**. Latest mode selected
Antigravity 1.2.7, but the browser driver supports the reviewed 1.2.2 release.
The build stopped during template generation. This guard was not changed for
the hook fix.

The prepared profile check passed in 161.99 seconds, including two VM starts,
two guest image loads, no host image export during the runtime test, and
completed runtime removal. Parity passed in 103.96 seconds with one VM start
and one guest image load. Both reports match the source digest above.

The Go and Docker suites ran separately to limit disk use. Go image cleanup
held the shared test lock and checked all container image references before
removing only this run's unused `cooper-gotest-` images. Prepared VM image
cleanup checked the cache owner, daemon, exclusive lease, run records, and
exact image IDs. It retained guest bases, archives, and reports. The logs are
`cooper-hook-review-go-image-cleanup.txt` and
`cooper-hook-review-vm-image-cleanup.txt`. No global Docker prune ran.

The first build-matrix cleanup attempt found Go 1.24.10 on the default path
and stopped before cleanup. Repeating it with Go 1.25.0 completed. The first
log remains as `cooper-hook-review-clean-toolchain.txt`. Final cleanup left
7.2 GiB free on the VM's Docker filesystem and only the original development
agent and pre-existing stopped container. No Git changes were staged.

macOS execution was not tested. These tests use temporary homes and fabricated
accounts; they do not change the developer's real state. The full VM release
gate and physical-host acceptance remain separate release checks.
