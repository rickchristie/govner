# Antigravity version selection

The image renderer rejected every native version except 1.2.2. This broke
Mirror after a host upgrade and broke Latest when the official manifest
advanced. The check came from a browser driver workaround: native 1.2.2
requires Playwright 1.57.0, but its old driver download endpoint returns 404.
That dependency did not justify a harness version allowlist.

## Change

Mirror, Latest, and Pin keep their selected native version. After the image
checks the official archive digest and native version, a small shell helper
reads the required driver version from the executable's compiled browser
cache path. Cooper installs that exact npm driver and checks its version.
The helper does not start agy or read host state. Missing or conflicting
dependency metadata causes a specific build error.

The native executable has no readable Go module metadata or public driver
version command. The cache path gives the dependency required by that
executable. This is an observed binary format, not a published compatibility
API. A future change to that format can require a new dependency reader;
it does not justify a release-number allowlist or a silent version change.

The host file-auth wrapper and complete selected state mounts are unchanged.
Official archive URL, SHA-512, platform, and exact native version checks
remain in place. Saved official release records are a cache for historical
builds, not a list of permitted versions. The VM test pin is now 1.2.7.

`AGENTS.md` now requires every harness to support Mirror, Latest, and Pin
without a version allowlist. `.gitignore` excludes Python bytecode caches
created by local checks.

## Native evidence

The official amd64 archives for 1.2.2 and 1.2.7 contain the same driver cache
version: 1.57.0. Both native clients independently reported that requirement
when run with an intentionally wrong driver in an empty test home. These
probes used no host credentials and could not contact a provider.

The official arm64 1.2.7 archive also passed its manifest SHA-512 check and
returned 1.57.0 through the dependency reader. The arm64 executable was not
run on this amd64 host. Unit fixtures cover a changed driver version,
repeated paths, missing metadata, and conflicting versions. Resolver and
renderer tests accept a synthetic future release in each version mode.

## npm metadata correction

The earlier release gates timed out while reading complete Codex and
OpenCode package histories. Cooper needed only one version record. The
[official npm registry API](https://github.com/npm/registry/blob/main/docs/REGISTRY-API.md)
provides `GET /{package}/{version}`, including the `latest` tag.

Version validation and Latest resolution now use that smaller response.
Exact Mirror and Pin checks still reject a response with a different
version. HTTP 404 means the requested version is absent; server failures,
denials, invalid JSON, and missing version fields remain errors. No timeout,
registry source, or proxy rule changed.

The official Codex 0.117.0 and TypeScript Latest version probes each completed
in about 0.36 seconds. Their compressed bodies were 1,419 and 1,537 bytes.
The earlier full-history responses were about 14.6 MB and 24.8 MB before
compression. This explains the unnecessary transfer cost, without claiming
to identify the underlying network speed limit.

## Verification

Source digest:
`a514bebb4707dd03817d4700191262ab4497a9d266c8163565ef84f203074896`.
The VM fixture digest excludes Markdown. Checks run inside the existing
Cooper Codex VM with Go 1.25.0 and its separate Docker daemon.

| Check | Result | Log under `/tmp` |
| --- | --- | --- |
| Offline VM unit suite | Passed; 21.740 seconds, no Docker or VM starts | `cooper-agy-driver-vm-unit.txt` |
| Antigravity, config, and template race tests | Passed | `cooper-agy-driver-race.txt` |
| Changed packages, Darwin arm64 | Compilation passed; no macOS execution | `cooper-agy-driver-darwin-compile.txt` |
| Antigravity 1.2.7 preparation | Passed; 59.712 seconds, reused the guest base, one image export | `cooper-agy-driver-prepare.txt` |
| Antigravity 1.2.7 Docker/VM parity | Passed; 95.335 seconds, one VM start and import | `cooper-agy-driver-parity.txt` |
| Antigravity profiles and restart | Passed; 159.851 seconds, two VM starts and imports | `cooper-agy-driver-profiles.txt` |
| Full Go suite | Passed on retry; 58 packages, with cached results included | `cooper-go-test.txt` |
| Full shell E2E suite | Passed; 390 checks, cleanup complete | `cooper-e2e.txt` |
| Full Docker build matrix | Passed; 259 checks across Mirror, Latest, and Pin | `cooper-docker-build.txt` |
| Development build | Passed; binary reports 0.6.0 | `cooper-build.txt` |

Parity ran the installed native client against a local model server in both
runtimes. It checked a response, saved conversation restore, terminal exit,
complete selected state, and matching paths. Profile tests checked live
aliases, account isolation, Docker-to-VM state, restart, protected hooks,
and cleanup. These checks use fabricated accounts; they do not establish
real provider token refresh.

The first full Go run passed 57 packages. The main package exceeded its
ten-minute timeout while rebuilding images after its cleanup test had removed
them. The stack was in `docker.BuildImage`, called by
`TestRunDownRemovesRuntimeArtifactsWithoutRemovingConfigOrImages`. Its log is
retained as `cooper-agy-driver-go-cold.txt`. The full retry passed with prepared
images, the same assertions, and the same timeout. The main package took
505.415 seconds. `GOFLAGS=-p=1` serialized packages that use the shared Docker
test lock. Test selection and assertions were unchanged.

The matrix built native Antigravity 1.2.2 in Mirror and Pin, and 1.2.7 in
Latest. The prepared VM checks separately pinned 1.2.7. Each matrix mode
checked the detected driver and the native response/conversation fixture.
Completed Mirror and Latest images were removed after the next mode started
to keep enough disk space. Only recorded test references were removed; the
normal cleanup command removed the remaining matrix images. Prepared bases,
archives, and logs remain. The development VM was not stopped.

Logs and structured VM reports are copied under
`dist/cooper/v0.6.0/evidence/version-fix/`. The first failed Go attempt remains
there with the passing retry. Python ignore rules, shell syntax, formatting,
and `git diff --check` also passed.

The full VM release gate and real-account acceptance still require the
physical host. No release tag or push is part of this preparation.
