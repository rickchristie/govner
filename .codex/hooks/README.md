# Govner Codex permission hook

`allow_govner_dev.py` auto-approves recurring development commands only when
the complete shell request matches a reviewed shape. A recognized test or
release preview with only a command-format defect is denied with a canonical
retry message. Unknown or semantically different commands produce no hook
decision, so Codex keeps its normal approval prompt.

The initial policy is based on Govner/Cooper Codex sessions through
2026-07-26. It auto-approves:

- `go test` and `go vet` for packages inside this repository;
- `go build` when the binary is written under `/tmp`, each module's exact
  conventional gitignored development binary, and Cooper's exact
  current-version release artifact targets;
- every mode of the repo-owned `cooper/test-e2e.sh` and
  `cooper/test-docker-build.sh`, including their isolated cleanup modes;
- all three `scripts/release-*.sh` release generators, whose only write is a
  private tag-message file under `/tmp`, and the exact build, tag, push, and
  Go-proxy indexing steps they print;
- repo-scoped `git add`, non-interactive `git commit`, default/configured or
  explicit-`origin` `git fetch`, read-only release-ref checks with
  `git ls-remote origin`, and non-force `git push`;
- `go run ./cmd/cooper-test-driver` with its known scenarios and bounded flags;
- read-only Docker listings plus image/network inspection for Cooper-prefixed
  resources;
- narrowly filtered Cooper process probes and the known bridge/lock `lsof`
  probes;
- read-only inspection of files under this repository or `/tmp`.

Tests and repo-owned workflow scripts must capture both stdout and stderr
under `/tmp`. Run these canonical commands from the repository root:

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

Keep the working directory visible in the command, either with a path such as
`./cooper/test-e2e.sh` or an explicit `cd cooper && ...`. In the observed Codex
`PermissionRequest` payload, `cwd` was the session's repository root and the
shell tool's per-call `workdir` was omitted. The hook cannot safely infer that
hidden directory: approving a bare `./test-e2e.sh` from the session root could
authorize a different script if a later tool call selected another directory.
A bare path is accepted only when the payload's own `cwd` resolves it to the
reviewed Cooper script.

## Release and Git boundaries

The release generators are previews: they read the declared project version,
existing tags, and Git history; write the multiline tag annotation to a
private `mktemp` file under `/tmp`; then print shell-quoted commands. Keeping
the message in a file avoids emitting Bash `$'...'` syntax, which the hook
intentionally rejects. The hook allows the preview only with complete `/tmp`
logging and separately validates every executable release step:

- release tags must equal `<project>/v<Version>` from that project's
  `meta/version.go`; a generated `-F` annotation must use that same project's
  six-character `mktemp`-shaped message path, owned by the current user with
  private permissions, bounded content, and the expected project/version
  header;
- release-tag and branch pushes must target `origin` without force, deletion,
  mirror, or destination-refspec options;
- remote release preflights must query `origin` for concrete branch refs or a
  project's currently declared release tag (optionally its standard peeled
  `^{}` ref), and must capture stdout and stderr under `/tmp`;
- Cooper artifacts must use one of the three platforms printed by
  `release-cooper.sh`, be built from `cooper/`, and land under the current
  `dist/cooper/v<Version>/` directory;
- Go-proxy indexing must use `https://proxy.golang.org`, a known Govner module,
  its current declared version, and complete `/tmp` logging.

The requested Git policy allows repo-wide or literal repo paths for `git add`,
common non-interactive commit forms (including amend-without-editor),
default/configured fetches and pushes, plus explicit `origin` operations. The
read-only `ls-remote` exception is narrower: it accepts concrete release
preflight refs on `origin`, with full `/tmp` logging. It rejects forced
ignored-file staging, indirect pathspec files, editor-only commits, arbitrary
remote URLs or helpers, wildcard remote-ref queries, force pushes, remote-ref
deletion, custom destination refspecs, and explicitly named non-origin
remotes.

The tracked `.vscode/tasks.json` is also inventoried by the unit suite. Its two
plain Go build steps map to the exact development-binary policy above, and all
seven Cooper script tasks map to reviewed script modes. The desktop
notification/read wrappers, installed `gowt` TUI tasks, and interactive
`cooper tui-test` process are IDE-only behavior rather than headless Codex
commands; agents use the logged build/test forms above. Directly copying an
unlogged script task receives canonical retry guidance.

## Guided workflow retries

Codex's `PermissionRequest` hook cannot rewrite the requested command. For a
small set of unambiguous workflow-format mistakes, the hook instead returns a
`deny` decision whose message says that the command did not run and tells the
agent to retry immediately from the repository root. This avoids presenting a
useless approval prompt for a command that can never match repository policy.

Guidance is emitted for:

- the ambiguous root-level `./test-e2e.sh` spelling;
- reviewed Cooper test scripts missing complete `/tmp` capture;
- safe repo-local `go test` and `go vet` commands missing complete capture;
- those same tests using a single `/tmp` `tee` sink; and
- a single leading `cd cooper && ...` or the historical
  `set -o pipefail; ... | tee ...; write-status` wrapper; and
- an otherwise exact unlogged `scripts/release-*.sh` preview.

The guidance path revalidates the test identity, arguments, working directory,
and shell structure before suggesting a replacement. It does not intercept
unknown test arguments, excessive timeouts, unsafe Go flags or packages,
mixed mutation, Docker wrappers, destructive Git operations, unrelated
network operations, or other unreviewed commands. Those continue through
normal approval.

The hook deliberately does not auto-approve:

- force/delete/mirror Git operations, arbitrary remotes, or Git configuration;
- network operations except origin fetch/push and the exact Go-proxy release
  indexing command;
- `go get`, `go install`, `go mod`, or arbitrary `go run`;
- unlogged or nonconventional worktree build outputs;
- direct `docker run`, `docker exec`, container inspection, builds, cleanup,
  or network mutation;
- arbitrary binaries or scripts assembled in `/tmp`;
- reads outside Govner and `/tmp`, sensitive paths, shell expansion, command
  substitution, background jobs, or unrecognized shell pipelines.

Those boundaries matter because direct Docker commands can mount host paths,
reach networks, expose live-container environment, or remove resources. A
repo-owned test is a smaller trust boundary: its behavior is reviewable with
the code and its namespaced cleanup is tested. The hook therefore approves the
whole reviewed test script, not standalone copies of its internal Docker
commands.

## Updating the policy

Add positive and negative examples to `allow_govner_dev_cases.json` first.
Every new command class needs a rationale and denial cases for the nearest
unsafe variants. Every tracked shell script must also be classified as a test
workflow, release generator, or intentionally manual/non-host script; the unit
suite enforces that inventory. The release-generator test also creates an
isolated Git fixture and validates every command actually printed by all three
release scripts, including a changelog containing quotes and command-like
text. Then update the hook and run:

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
