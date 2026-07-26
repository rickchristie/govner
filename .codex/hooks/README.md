# Govner Codex permission hook

`allow_govner_dev.py` auto-approves recurring development commands only when
the complete shell request matches a reviewed shape. A recognized test with
only a command-format defect is denied with a canonical retry message. Unknown
or semantically different commands produce no hook decision, so Codex keeps
its normal approval prompt.

The initial policy is based on Govner/Cooper Codex sessions through
2026-07-26. It auto-approves:

- `go test` and `go vet` for packages inside this repository;
- `go build` only when the binary is written under `/tmp`;
- the repo-owned `cooper/test-e2e.sh` and `cooper/test-docker-build.sh`;
- `go run ./cmd/cooper-test-driver` with its known scenarios and bounded flags;
- read-only Docker listings plus image/network inspection for Cooper-prefixed
  resources;
- narrowly filtered Cooper process probes and the known bridge/lock `lsof`
  probes;
- read-only inspection of files under this repository or `/tmp`.

Tests and repo-owned test scripts must capture both stdout and stderr under
`/tmp`. Run these canonical Cooper release-gate commands from the repository
root:

```bash
go test -C ./cooper ./... > /tmp/cooper-go-test.txt 2>&1
timeout 90m ./cooper/test-e2e.sh > /tmp/cooper-e2e.txt 2>&1
```

Keep the working directory visible in the command, either with a path such as
`./cooper/test-e2e.sh` or an explicit `cd cooper && ...`. In the observed Codex
`PermissionRequest` payload, `cwd` was the session's repository root and the
shell tool's per-call `workdir` was omitted. The hook cannot safely infer that
hidden directory: approving a bare `./test-e2e.sh` from the session root could
authorize a different script if a later tool call selected another directory.
A bare path is accepted only when the payload's own `cwd` resolves it to the
reviewed Cooper script.

## Guided test retries

Codex's `PermissionRequest` hook cannot rewrite the requested command. For a
small set of unambiguous test-only mistakes, the hook instead returns a `deny`
decision whose message says that the test did not run and tells the agent to
retry immediately from the repository root. This avoids presenting a useless
approval prompt for a command that can never match the repository policy.

Guidance is emitted for:

- the ambiguous root-level `./test-e2e.sh` spelling;
- reviewed Cooper test scripts missing complete `/tmp` capture;
- safe repo-local `go test` and `go vet` commands missing complete capture;
- those same tests using a single `/tmp` `tee` sink; and
- a single leading `cd cooper && ...` or the historical
  `set -o pipefail; ... | tee ...; write-status` wrapper.

The guidance path revalidates the test identity, arguments, working directory,
and shell structure before suggesting a replacement. It does not intercept
unknown test arguments, excessive timeouts, unsafe Go flags or packages,
mixed mutation, Docker wrappers, Git/network operations, or other unreviewed
commands. Those continue through normal approval.

The hook deliberately does not auto-approve:

- Git writes or network operations;
- `go get`, `go install`, `go mod`, or arbitrary `go run`;
- direct `docker run`, `docker exec`, container inspection, builds, cleanup,
  or network mutation;
- arbitrary binaries or scripts assembled in `/tmp`;
- reads outside Govner and `/tmp`, sensitive paths, shell expansion, command
  substitution, background jobs, or unrecognized shell pipelines.

Those boundaries matter because direct Docker commands can mount host paths,
reach networks, expose live-container environment, or remove resources. A
repo-owned test is a smaller trust boundary: its behavior is reviewable with
the code and its namespaced cleanup is tested.

## Updating the policy

Add positive and negative examples to `allow_govner_dev_cases.json` first.
Every new command class needs a rationale and denial cases for the nearest
unsafe variants. Then update the hook and run:

```bash
PYTHONPYCACHEPREFIX=/tmp/govner-hook-pycache \
  python3 .codex/hooks/allow_govner_dev_test.py \
  > /tmp/govner-hook-tests.txt 2>&1
```

Project hooks load only for trusted repositories. Codex also requires review
of a new or changed non-managed hook hash; use `/hooks` once after cloning or
after modifying this hook. Re-review is also required after changing only the
guidance logic because the hook file's hash changes. The config resolves the
script from the Git root so starting Codex from `cooper/` still uses the same
policy.
