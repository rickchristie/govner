# Govner Codex approval audit

This audit records why the initial repository policy approves some recurring
commands, guides some formatting-only test retries, and leaves all other
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
- 126 historical command shapes accepted by this policy;
- 11 formatting-only test shapes denied with canonical retry guidance; and
- 118 historical command shapes left for normal approval.

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
| Go tests, vet, and temporary builds | Govner packages only; tests log stdout and stderr under `/tmp`; binaries and profiles never write into the worktree | 81 |
| Cooper end-to-end and image-build scripts | Exact repo-owned scripts and reviewed arguments; bounded timeout; full `/tmp` logging | 9 |
| Cooper test driver | Exact package, known scenarios, bounded flags, full `/tmp` logging | 8 |
| Docker diagnostics | List operations, or image/network inspection limited to Cooper-prefixed resources; no container inspection | 20 |
| Narrow process and file diagnostics | Known Cooper filters, bridge/lock probes, and reads limited to Govner or `/tmp` | 8 |

This deliberately favors repo-owned scripts over equivalent handwritten Docker
commands. The scripts are versioned with the code, namespace their resources,
and have tests for their cleanup behavior.

Canonical full-suite commands:

```bash
go test -C ./cooper ./... > /tmp/cooper-go-test.txt 2>&1
timeout 90m ./cooper/test-e2e.sh > /tmp/cooper-e2e.txt 2>&1
```

## Guided retry, normal approval, or further discussion

The 129 non-auto-approved historical commands fall into these mechanical
groups:

| Group | Count | Why it is not auto-approved |
| --- | ---: | --- |
| Direct `docker run` | 72 | Can mount host paths, select arbitrary entrypoints, use networks, inject credentials, and execute unreviewed shell programs |
| `docker exec` | 10 | Reads or changes live containers and can expose their environment, tokens, and mutable state |
| Docker mutation | 4 | Creates, stops, or removes containers, networks, images, or related resources |
| Git writes | 8 | Staging, committing, and pushing change durable repository or remote state |
| External network access | 3 | Downloads code or artifacts whose content can change independently of this repository |
| Ad-hoc `/tmp` binaries | 7 | The executable's provenance and behavior are not represented by the command line |
| Unsupported Go operations | 3 | `go get`, arbitrary `go run`, or an application build can download dependencies or create runtime resources |
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
2. Guidance is limited to a completely parsed, otherwise safe test whose
   replacement is unambiguous.
3. Unknown syntax produces no decision and therefore keeps Codex's prompt.
4. Shell substitution, expansion, background work, and unrecognized pipelines
   fail closed.
5. Test artifacts and logs stay under `/tmp`; the worktree is not used as an
   output directory.
6. A new direct-Docker, network, Git-write, or arbitrary-executable class
   requires an explicit policy decision rather than being inferred from a
   superficially similar command.
