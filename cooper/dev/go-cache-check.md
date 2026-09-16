# Go cache check in Cooper VM

Checked on 2026-09-15 inside the running Codex VM.

The module and build caches are mounted correctly. No cache or whitelist
change was needed. This check did not establish which module or version
caused the user's earlier approval prompts.

| Purpose | Effective path | Live mount |
| --- | --- | --- |
| Module downloads and source | `/go/pkg/mod` | Read-write virtiofs, tag `cooper-m-007` |
| Compiled packages and test results | `/var/lib/cooper/cache/go-build` | Read-write virtiofs, tag `cooper-m-016` |

`go env` reports those paths as `GOMODCACHE` and `GOCACHE`. Docker inspection
confirms both bind mounts in the agent container. `/proc/self/mountinfo`
confirms that their backing filesystems are host exports through virtiofs,
outside the disposable container layer. The caches contained about 2.6 GiB
and 2.7 GiB respectively during the check.

`internal/workload/mountplan.go` supplies the same cache catalog to CLI and
VM sessions. Sources are `~/.cooper/cache/go-mod` and
`~/.cooper/cache/go-build`. The guest mounts each manifest export before it
starts the selected agent container. No cache clear runs on normal entry.

## Reuse test

Two new containers used the running image and these two cache mounts. Each
container had `--network none`; the repository was mounted read-only. They
did not mount agent state. Each container was removed after its command.
The running Codex container and VM stayed active.

Both ran this package test with the cached Go 1.25.0 toolchain:

```text
GOPROXY=off GOTOOLCHAIN=local
go test -C /workspace/cooper ./internal/tui/components
```

Checksum verification stayed enabled. The first run passed. The second run
passed with `(cached)`. This proves reuse across container replacement,
including external module dependencies. It is not a physical VM restart
test. The log is `/tmp/cooper-go-cache-reuse.txt`.

## Checksum lookup test

An explicit download outside the repository's `go.sum` exposed a narrower
cache gap. Bubble Tea v1.3.10 has cached source, `.info`, `.mod`, `.zip`, and
`.ziphash` files, but no signed lookup under
`cache/download/sumdb/sum.golang.org/lookup/`.

In a fresh container with `--network none`, a fresh `GOPATH`, and
`GOPROXY=off`, `go mod download github.com/charmbracelet/bubbletea@v1.3.10`
fails because it needs a lookup. A second isolated control changes only
`GOSUMDB=off`; it succeeds from the existing archive. This is a diagnostic
control, not a Cooper setting or a proposed fix. The logs are
`/tmp/cooper-go-checksum-reuse.txt` and
`/tmp/cooper-go-checksum-control.txt`.

The same fresh-checkpoint test for `github.com/pelletier/go-toml/v2@v2.2.4`
passes with verification enabled. This version has a cached signed lookup.
Go also creates the new `GOPATH/pkg/sumdb/sum.golang.org/latest` checkpoint
without network access. Its log is `/tmp/cooper-go-checksum-cached.txt`.

These checks prove that a cached archive does not guarantee an offline
operation in a different checksum context. They do not identify the exact
commands behind the user's earlier prompts.

## Why an approval can still occur

A Docker image build has its own filesystem and layer cache. The selected
agent's mounted Go cache is not a mount inside Dockerfile `RUN` steps.
During this check, the E2E build installed `gopls` v0.20.0 and downloaded
its modules in that image. The downloads are in `/tmp/cooper-e2e.txt`.
The install is defined in `internal/templates/base.Dockerfile.tmpl`.
Such build requests are separate from interactive cache reuse and do not
show a missing VM mount.

A cache contains specific module versions. A new module, a new version, an
update query, or a missing checksum record can still require network access.
Go also stores downloaded toolchains in the module cache. This VM has Go
1.24.10 installed, while the repository requires Go 1.25.0. The explicit
Go 1.25.0 download made during this task is one known new cache entry.
See the [Go module cache reference](https://go.dev/ref/mod#module-cache).

Go keeps checksum lookup records and tree tiles under
`GOMODCACHE/cache/download/sumdb`; those records persist in the module
mount. Its small trusted tree checkpoint lives separately at
`GOPATH/pkg/sumdb/sum.golang.org/latest`. That path is not a separate Cooper
cache mount. Go can reconstruct it from a cached signed lookup. The go-toml test recreated
that checkpoint offline from its cached signed lookup, so losing the
checkpoint does not require a network request by itself. See the
[Go checksum client](https://go.dev/src/cmd/go/internal/modfetch/sumdb.go).

The Monitor host name alone cannot identify a cache miss. If a known,
unchanged package repeatedly asks for access, record the exact Go command,
effective cache paths, and the requested module/version from the Squid log.
Keep `proxy.golang.org` and `sum.golang.org` subject to the existing proxy
policy. Do not disable checksum verification to suppress prompts.
