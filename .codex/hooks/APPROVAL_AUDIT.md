# Govner Codex approval audit

This audit records why the repository policy approves some recurring
commands, guides some formatting-only workflow retries, and leaves all other
requests for Codex's normal permission prompt. It is a policy review, not a
copy of conversation transcripts.

## Scope and method

The initial review covered 46 completed Codex session files whose recorded
working directory was Govner or Cooper. The sessions span 2026-03-28 through
2026-07-26.

Both legacy `exec_command` calls and newer `exec` calls were inspected. Calls
were deduplicated by tool-call ID, current policy-development probes were
excluded, and parallel tool requests were expanded into their individual
commands. The resulting corpus contained:

- 250 approval-request tool calls;
- 255 individual escalated commands;
- 134 historical command shapes accepted by this policy;
- 11 formatting-only test shapes denied with canonical retry guidance; and
- 110 historical command shapes left for normal approval.

The 2026-07-27 expansion added the user-requested Git operations and completed
the repository script/release inventory. It changed eight historical Git
requests from manual to allowed and changed no previously allowed request to
denied.

The replay count measures exact command shapes, not whether a task can be run
without a prompt. For example, an unlogged `./test-e2e.sh` request is rejected
with a canonical retry message, while the required logged form is
auto-approved.

A live Codex integration check found one important payload boundary: a
`PermissionRequest` includes the session `cwd` and command text, but the
observed payload did not include the shell tool's per-call `workdir`. Therefore
the policy deliberately rejects a bare `./test-e2e.sh` when the payload says
the session is at the repository root. Canonical commands now expose `cooper`
in the command text, allowing the hook to prove which repo-owned script will
run instead of trusting an invisible directory override.

## Safe and auto-approved

The following operations have a small, reviewable boundary and recurring
historical evidence:

| Operation | Required constraints | Historical shapes accepted |
| --- | --- | ---: |
| Go tests, vet, and builds | Govner packages only; complete `/tmp` logs; binaries go to `/tmp` or one of the three exact gitignored development paths | 81 |
| Cooper end-to-end and image-build scripts | Exact repo-owned scripts and reviewed arguments; bounded timeout; full `/tmp` logging | 9 |
| Cooper test driver | Exact package, known scenarios, bounded flags, full `/tmp` logging | 8 |
| Docker diagnostics | List operations, or image/network inspection limited to Cooper-prefixed resources; no container inspection | 20 |
| Narrow process and file diagnostics | Known Cooper filters, bridge/lock probes, and reads limited to Govner or `/tmp` | 8 |
| Git add/commit/fetch/ls-remote/push | Repo-scoped add, non-interactive commit, default/configured or origin fetch/push, and concrete read-only release-ref checks on origin; destructive options denied | 8 |

This deliberately favors repo-owned scripts over equivalent handwritten Docker
commands. The scripts are versioned with the code, namespace their resources,
and have tests for their cleanup behavior.

The historical count above remains scoped to sessions through 2026-07-26. A
2026-07-27 release added one newly observed read-only shape:
`git ls-remote origin` for `refs/heads/main`, the selected project's currently
declared release tag, and its standard peeled `^{}` ref, with complete `/tmp`
capture. It is recorded in the policy corpus but is not retroactively included
in the historical count.

Canonical full-suite commands:

```bash
go test -C ./cooper ./... > /tmp/cooper-go-test.txt 2>&1
go test -C ./gowt ./... > /tmp/gowt-go-test.txt 2>&1
go test -C ./pgflock ./... > /tmp/pgflock-go-test.txt 2>&1
go build -C ./cooper -o ./cooper . > /tmp/cooper-build.txt 2>&1
go build -C ./gowt -o ./gowt . > /tmp/gowt-build.txt 2>&1
go build -C ./pgflock -o ./pgflock . > /tmp/pgflock-build.txt 2>&1
timeout 90m ./cooper/test-e2e.sh > /tmp/cooper-e2e.txt 2>&1
timeout 90m ./cooper/test-docker-build.sh all > /tmp/cooper-docker-build.txt 2>&1
./cooper/test-docker-build.sh clean > /tmp/cooper-docker-build-clean.txt 2>&1
```

## Repository workflow inventory

Every tracked shell file is classified, and a unit test fails if a new tracked
shell script is added without an explicit policy decision.

| Tracked workflow | Reviewed behavior | Policy |
| --- | --- | --- |
| `cooper/test-e2e.sh` | Builds a gitignored Cooper binary, creates only `test-e2e-*` Docker resources and gitignored fixtures, runs the lifecycle/clipboard/TUI gates, and trap-cleans those resources | Auto-approved for normal and `clean`, with full `/tmp` capture |
| `cooper/test-docker-build.sh` | Builds the gitignored binary; exercises mirror/latest/pinned images, namespaced containers, ownership mounts, and cleanup | Auto-approved for mirror/latest/pinned/all/clean, with full `/tmp` capture |
| `scripts/release-cooper.sh` | Reads the current version, tags, and log; writes a private `/tmp` tag-message file; prints shell-quoted artifact/tag/push/index commands | Auto-approved preview; each printed executable step is separately validated |
| `scripts/release-gowt.sh` | Reads the current version, tags, and log; writes a private `/tmp` tag-message file; prints shell-quoted tag/push/index commands | Auto-approved preview; each printed executable step is separately validated |
| `scripts/release-pgflock.sh` | Reads the current version, tags, and log; writes a private `/tmp` tag-message file; prints shell-quoted tag/push/index commands | Auto-approved preview; each printed executable step is separately validated |
| `scripts/convert-agents.sh` | Overwrites sibling tracked `AGENTS.md` files from `CLAUDE.md` | Intentionally manual; maintenance mutation, not build/test/release |
| `cooper/internal/templates/doctor.sh` | Container-installed diagnostic that probes container networking, tools, proxy policy, and clipboard state | Intentionally not a host script; exercised through reviewed Cooper workflows |

The test scripts' internal Docker build/run/exec/remove operations are covered
by approving the exact repo-owned script as one reviewed boundary. Copying an
internal Docker command out of the script loses its argument, prefix, cleanup,
and lifecycle invariants, so standalone Docker mutation remains manual.

The other tracked workflow registry is `.vscode/tasks.json`. Its two Go build
steps and seven Cooper script tasks map to the logged hook forms above. Four
installed-`gowt` tasks and the interactive `cooper tui-test` task remain
IDE-owned UI processes: their equivalent headless Go suites are auto-approved,
while desktop notification/read wrappers and interactive TUIs are not treated
as unattended Codex shell commands. A unit test fixes this 14-task
classification and fails when a task is added without review.

Generated Dockerfiles and entrypoint templates are inputs to Cooper/pgflock
runtime code rather than host entry points. They are exercised through the
reviewed Go and Cooper script workflows; extracting a standalone `docker
build` or `docker run` from a template remains manual.

The release generators do not execute their printed release commands.
Consequently, their current-version artifact directories, three Cooper
cross-builds, annotated project tags, origin tag pushes, and exact official
Go-proxy indexing calls each have their own validator and escape tests.
Annotated-tag commands read the multiline message from a project-matched
private `mktemp` file. This avoids embedding repository-controlled changelog
subjects in Bash `$'...'` syntax and avoids predictable `/tmp` paths that can
be pre-created as symlinks. The hook also checks that the file is a
current-user-owned, private, bounded regular file with the expected
project/version header before approving `git tag -F`.

## Guided retry, normal approval, or further discussion

The 121 non-auto-approved historical commands fall into these mechanical
groups:

| Group | Count | Why it is not auto-approved |
| --- | ---: | --- |
| Direct `docker run` | 72 | Can mount host paths, select arbitrary entrypoints, use networks, inject credentials, and execute unreviewed shell programs |
| `docker exec` | 10 | Reads or changes live containers and can expose their environment, tokens, and mutable state |
| Docker mutation | 4 | Creates, stops, or removes containers, networks, images, or related resources |
| External network access | 3 | Downloads code or artifacts whose content can change independently of this repository |
| Ad-hoc `/tmp` binaries | 7 | The executable's provenance and behavior are not represented by the command line |
| Unsupported Go operations | 3 | `go get`, arbitrary `go run`, or an unlogged/unscoped application build can download dependencies or create runtime resources |
| Other higher-impact compound commands | 4 | Mixed mutation, scratch setup, or application/resource lifecycle behavior needs case-specific review |
| Noncanonical test-related requests | 16 | 11 formatting-only requests now receive canonical retry guidance; 5 mutate test state, probe broadly, or wrap tests in direct Docker commands and remain manual |
| Broad host process reads | 2 | Full command-line listings can expose unrelated process arguments; filtered Cooper probes are already supported |

Several direct `docker run --rm --network none` requests are plausible
candidates for a future constrained profile. They are not approved yet because
the historical commands also vary mounts, entrypoints, environment variables,
and inline scripts. A safe expansion should first specify allowed images,
read-only mount roots, network mode, environment names, entrypoints, and
cleanup guarantees, with denial cases for each escape route.

Unlogged or `tee`-based test requests do not need a broader allow policy. The
hook denies the 11 historically observed formatting-only requests with a
canonical redirected replacement so the agent can retry and preserve the
repository's required diagnostic logs. The remaining five are deliberately
not treated as formatting errors.

## Review invariants

Future policy changes should preserve these properties:

1. Every positive corpus case has the nearest meaningful negative cases.
2. Guidance is limited to a completely parsed, otherwise safe workflow whose
   replacement is unambiguous.
3. Unknown syntax produces no decision and therefore keeps Codex's prompt.
4. Shell substitution, expansion, background work, and unrecognized pipelines
   fail closed.
5. Test profiles and all diagnostic logs stay under `/tmp`; worktree writes
   are limited to exact gitignored development/release artifact paths.
6. A new direct-Docker, network, destructive-Git, or arbitrary-executable
   class requires an explicit policy decision rather than being inferred from
   a superficially similar command.
