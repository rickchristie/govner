# Required profile setup during build

Date: 2026-09-18. This change replaces the optional Linux conversion policy
from the [initial implementation report](symlink-profile-report.md).
The user requires a visible, mandatory step in `cooper build`.

The [review follow-up](profile-review-report.md) records the later fixes for
first-save working directories, interrupted initialization, and Linux test
selection. It has the verification results for that source revision.

## Behavior

Both `cooper build` and configure's Save & Build call the same build flow.
After host authentication setup, `Setting up live profiles...` runs before
the first Docker image build. There is no skip flag or storage-mode setting.

- New stores start with an empty live catalog. Build needs no account login
  and does not replace unsaved host roots. The first save registers complete
  roots with the existing migration/recovery rules.
- Existing saved copies are converted once. The build reports the recovery
  record, and retains originals. A failed conversion stops image builds.
- Repeat builds check bounded metadata and root links. They do not scan
  history, write a new selector, or require active agents to stop.
- A build inside Cooper can initialize empty local metadata. Its mutation
  guard refuses conversion of existing profiles on a partial host view.
- `detach` remains a recovery/export tool. A later Linux build converts the
  detached store again. Preview and manual conflict repair remain available.
- macOS retains its current profile storage because live directory conversion
  remains Linux-only. This is a platform limit, not a selectable Linux mode.

An unused build-created store must not block full configuration cleanup.
Cleanup therefore accepts only its exact empty layout and valid initial
metadata. Saved accounts, recovery with replaced roots, additional files,
redirected paths, and corrupt metadata retain the existing refusal.

## Verification

Unit cases cover empty initialization, first live save/load, conversion before
any image build, repeat builds without copies or writer checks, failure
reporting, changed host links, inner-build limits, and unused-store cleanup.
All fixtures use fake accounts and temporary homes. No real profile store is
converted during development.

The final source digest is
`e7a4449fcfb720d0d69f28dac3b20a509ec502c25dc7604627fc5966a49d4cac`.
The digest covers implementation and test inputs; it excludes Markdown reports.

| Check | Result | Log under `/tmp/` |
| --- | --- | --- |
| VM unit boundary | Passed, with Docker and QEMU blocked. No VM starts. | `cooper-vm-unit.txt`, `cooper-vm-dev-unit-f8b5525b92c0.json` |
| Build-flow integration | Passed for automatic setup, repeat builds, failure before images, and inner-build limits. | `cooper-build-profile-flow-test.txt` |
| CLI/profile integration | Passed in the main and application packages. | `cooper-build-profile-integration.txt` |
| Full Go suite | Passed: 58 tested packages, with six other packages that have no tests. Main package 478.214 s; application package 440.252 s. | `cooper-go-test.txt` |
| Full shell E2E suite | Passed: 390 checks, zero failures; test cleanup completed. Its real build created the schema-2 store before images. | `cooper-e2e.txt` |
| Docker build matrix, all modes | 173 checks passed and one failed. Mirror and pinned each passed 86 checks. Latest stopped at the existing driver check for Antigravity 1.2.6; only 1.2.2 is reviewed. | `cooper-docker-build.txt` |
| Race tests | Passed for profilelink, profiles, profileauth, profilemanager, workload, and buildflow. | `cooper-build-profile-race.txt` |
| Prepared Codex profiles | Passed in Docker and VM, including VM restart and cleanup. Two test VMs; no host image export during the runtime check. | `cooper-build-profile-vm-codex.txt`, `cooper-vm-dev-profiles-f84c3ca98967.json` |
| Prepared Antigravity profiles | Passed in Docker and VM with fabricated file-auth tokens, including restart and cleanup. Two test VMs; no host image export during the runtime check. | `cooper-build-profile-vm-antigravity.txt`, `cooper-vm-dev-profiles-754416a71cc3.json` |
| Linux development build | Passed. | `cooper-build.txt` |
| Darwin ARM64 build | Passed; live conversion remains Linux-only. | `cooper-build-profile-darwin.txt` |
| Build TUI | Inspected at 100×28 and 80×24, both following output and scrolled to profile setup. | `cooper-build-profile-step-{100,80}.png`, `cooper-build-profile-start-{100,80}.png` |
| CLI help | Build, migrate, and detach describe mandatory Linux setup and recovery behavior. | `cooper-build-profile-{help,migrate-help,detach-help}.txt` |
| Source and documentation | Go format, shell/Python syntax, 74 local document links, and `git diff --check` passed. The final source digest matches the runtime reports. | Checked directly. |

The two runtime reports match the final source digest. The tests started four
disposable VMs in total, checked selected roots and credentials, and removed
their runtimes. These tests do not make a provider OAuth refresh request.
Preparation logs are `cooper-build-profile-prepare-codex.txt` and
`cooper-build-profile-prepare-antigravity.txt`.

Disk space was measured through Docker's filesystem. Two unused Go-test image
tags were removed after checking every container's image ID. After the profile
tests, cleanup checked cache ownership, the lease, exact manifest image IDs,
and container use before removing the two agents' test image tags and manifests.
Prepared bases, archives, logs, and the running development VM were retained.
The Docker filesystem had 5.8 GiB free before the final build gate.
The reviewed `test-docker-build.sh clean` command passed after the matrix;
its log is `/tmp/cooper-docker-build-clean.txt`.

Both mirror and pinned builds created schema-2 profile stores. Their later
rebuilds used those stores successfully. Latest mode stopped during template
generation, before profile setup, with this error:

```text
Antigravity "1.2.6" has no reviewed browser driver; supported native release: 1.2.2
```

The complete Docker build gate is therefore **not passed**. This is a provider
version support limit, not a profile conversion failure. No version check was
weakened. This report does not approve a release.

The real host login/refresh acceptance and full VM release gate from the
initial report remain separate. The build-default change does not add support
for Antigravity browser drivers beyond the reviewed 1.2.2 release. This run's
latest mode resolves to 1.2.6; the earlier report recorded 1.2.5.
