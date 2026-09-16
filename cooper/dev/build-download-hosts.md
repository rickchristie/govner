# Build and test download hosts

Checked on 2026-09-15 from the Cooper Codex VM. This list covers the current
build templates, version resolvers, native installers, known download
redirects, and positive external checks in the E2E suite. It is a record for
session approval, not a proposed default whitelist.

| Use | Hosts |
| --- | --- |
| Docker image registry and downloads | `auth.docker.io`, `registry-1.docker.io`, `production.cloudflare.docker.com`, `production.cloudfront.docker.com` |
| TUI capture images | `ghcr.io`, `pkg-containers.githubusercontent.com` |
| System packages and Squid source | `deb.debian.org`, `dl-cdn.alpinelinux.org`, `www.squid-cache.org` |
| Go toolchains, modules, and checksums | `go.dev`, `dl.google.com`, `proxy.golang.org`, `sum.golang.org` |
| Node.js, npm, and version metadata | `nodejs.org`, `endoflife.date`, `registry.npmjs.org` |
| Python packages | `pypi.org`, `files.pythonhosted.org` |
| GitHub release metadata and artifacts | `github.com`, `api.github.com`, `release-assets.githubusercontent.com` |
| Claude installer and releases | `claude.ai`, `downloads.claude.ai` |
| Grok binary releases | `x.ai` |
| Antigravity release metadata | `antigravity-cli-auto-updater-974169037036.us-central1.run.app` |
| Antigravity archives and Grok release fallback | `storage.googleapis.com` |
| Positive provider checks in the E2E suite | `api.anthropic.com`, `api.openai.com`, `auth.x.ai`, `cli-chat-proxy.grok.com` |

The source inputs are under `internal/templates`, `internal/config/resolve.go`,
and `internal/antigravity/release.go`. The test scripts are `test-e2e.sh`,
`test-docker-build.sh`, and the repository's `scripts/capture-tui.sh`.
The current Claude installer names `downloads.claude.ai`. The Antigravity
manifest names `storage.googleapis.com`. A GitHub release probe followed its
redirect to `release-assets.githubusercontent.com`.

The host probes and actual Debian image pull succeeded after the user
enabled session access. Both capture image preparations, the full Go suite,
all 390 E2E checks, and Mirror/Pin Docker builds then passed. A root-path
response such as 401 or 403 from an origin does not prove that an artifact
download will pass; the actual build results provide that check. Probe logs
are under `/tmp/cooper-download-probes` and the plain host list is
`/tmp/cooper-required-domains.txt`.

The Latest Docker build stopped at Cooper's existing Antigravity version
check. The official manifest returned 1.2.3, while the reviewed browser
driver is for 1.2.2. More host permission cannot resolve that version limit.
See [the verification report](proxy-tui-verification.md).

Hosts can change when an upstream installer or release changes. Review a
new redirect when it appears. Do not add the test suite's denied destinations
or the background hosts `antigravity-unleash.goog` and `play.googleapis.com`
to make tests pass. The build use of `x.ai` does not require a permanent
permission for Grok startup requests. See [the background request
findings](background-network-findings.md).

The user grants these permissions through the physical host's Monitor.
They end when that `cooper up` exits. This check changed no permanent rule.
