# Proxy and control panel changes

Work checked on 2026-09-15 inside a Cooper Codex VM.

## Behavior

- Squid sends the destination, numeric port, source IP, and empty ACL data
  marker explicitly. The listener also accepts the old generated format.
  Invalid ports and source IPs are denied before review.
- History combines allowed and blocked requests, with an `f` filter and
  the existing separate capacity settings.
- The access-log tailer starts at the final 200 lines. It follows new lines,
  waits for incomplete lines, and handles file replacement and truncation.
  The Squid tab keeps 200 lines and opens at the newest output.
- Squid and Bridge output share a selectable text viewport. Click or drag
  to select lines, then `y` to copy their complete text. A copy runs through
  the application clipboard writer with a five-second deadline. Copy
  failures are shown in the log. Copy does not stage workload access.
- Bridge combines routes and execution logs. Runtime combines Settings
  and About. `[` and `]`, or a click in a pane, select focus. Each pane
  has its own scroll position. Runtimes remains a separate tab.
- Monitor `w` allows the selected exact host without a confirmation step.
  Access ends when this `cooper up` exits. `s` opens a full host list; `r`
  removes the selected host. Permission results reach Monitor even after
  a tab change.

The old session dialog was appended after a complete body with a carriage
return. The root frame then cut off those added rows. The manager now owns
the body while it is open. The combined screen component gives an open
editor the full body and keeps its keyboard focus.

The whitelist is unchanged. In particular, `antigravity-unleash.goog`,
`play.googleapis.com`, and `x.ai` remain outside it. See the
[network investigation](background-network-findings.md) and the separate
[Go cache check](go-cache-check.md).

## Validation

Local checks cover socket metadata, malformed input, bounded tailing,
rotation, partial lines, selection while output arrives, full Unicode text
copy, copy errors, pane focus, inactive event routing, merged history
limits, session host removal, and the fixed root frame.

The shared production screen tree is exercised by a deterministic fixture
with a fixed clock and fabricated records. Layout tests check 120×36 and
80×24, including the route editor and session manager.

Passed: the full Go suite, all 390 E2E checks, focused tests, race tests for
the proxy/tailer/TUI, fixed layout tests, the offline VM development suite,
and the development build. Application tests cover the new clipboard boundary.
The Go cache audit passed offline reuse and identified one cached module
with a missing signed checksum lookup; see its separate report.

The Docker build `all` gate finished with 173 passed checks and one failure.
Mirror and Pin each passed all 86 checks; the initial binary build also
passed. Latest stopped before image creation because the official Antigravity
manifest now reports 1.2.3. The existing, unchanged check in
`internal/templates/antigravity.go` accepts only native 1.2.2, whose embedded
Playwright client has a reviewed driver. The same check is present in the
baseline commit. This run does not qualify Antigravity 1.2.3 or pass the
complete release build gate. Accepting that release requires a separate
driver and native behavior review; the restriction was kept in place.
The fetched public manifest is `/tmp/cooper-antigravity-latest-manifest.json`.

The first gate attempts stopped on host proxy denials for Go downloads,
npm metadata, and capture images. The Go download response included
`Server: squid/6.12` and `X-Squid-Error: ERR_ACCESS_DENIED 0`.
After the user enabled host session access, probes reached the required
download hosts. A real Debian image pull, both capture image preparations,
and the Go suite's test image builds succeeded. Permanent rules were not
changed. The [checked download-host list](build-download-hosts.md) covers the
current build templates, version resolvers, installers, download redirects,
and positive external checks in the E2E suite. No further proxy denial caused
the final Docker build failure.

The first E2E run after network access passed the agent and proxy checks,
then stopped at the loopback listener check because this VM image has no
`ss` command. Its shell pipeline exits before the test can print a failure.
Debian's `iproute2` and required libraries were extracted under
`/tmp/cooper-test-deps`, and the real `ss` binary successfully found a local
test listener. The repeated gate uses that tool through `PATH` and
`LD_LIBRARY_PATH`. No test assertion was removed or skipped. The initial
log is `/tmp/cooper-e2e-missing-ss.txt`.
The gate now checks for `ss` before it builds images, and prints the log
path when an isolation fixture build fails. The missing-tool path was
tested directly; shell syntax validation also passed.

The next run passed 385 checks, then failed the final Python-only build on
the Jedi 0.20.0 wheel hash. A fresh download has the expected SHA-256 and
contains 4,884,812 bytes. The reported failed hash exactly matches its
first 1,789,568 bytes, which proves that the failed copy was incomplete.
Hash verification was kept enabled. The failure logs are
`/tmp/cooper-e2e-jedi-failure.txt` and
`/tmp/cooper-e2e-python-only-truncated-build.txt`.
Restarting the failed install container with its original command passed;
its log is `/tmp/cooper-jedi-pip-retry.txt`.
The following complete E2E run passed all 390 checks with zero failures.

Logs:

| Check | Log |
| --- | --- |
| Focused tests | `/tmp/cooper-targeted.txt` |
| Race tests for proxy, tailer, and TUI | `/tmp/cooper-race.txt` |
| Fixed screen layout tests | `/tmp/cooper-screen-layout.txt` |
| Offline VM development suite | `/tmp/cooper-vm-unit.txt` |
| Development build | `/tmp/cooper-build.txt` |
| Full Go suite | `/tmp/cooper-go-test.txt` |
| E2E gate | `/tmp/cooper-e2e.txt` |
| Docker build gate, all modes | `/tmp/cooper-docker-build.txt` |
| VHS image preparation | `/tmp/tui-capture-prepare.txt` |
| Xvfb image preparation | `/tmp/tui-capture-xvfb-prepare.txt` |

No physical VM release gate is needed for this routine TUI and proxy parser
change. The full release gate remains required before a release.

## Image inspection

All captures use fabricated records, a fixed clock, and containers with no
network. VHS captures use a 1280×720 canvas, 16-pixel padding, and DejaVu Sans
Mono at size 18. Xvfb captures use XTerm at 80×24 with the same font and
size; its output is 1200×720 pixels. Every generated PNG was opened and
inspected.

The first small route editor capture exposed a clipped bottom border.
The editor now removes blank rows when space is short and keeps long input
values on one display row. It preserves the complete value for saving.
A regression test checks the border, actions, cursor, and validation error
inside the 80×24 root's 80×18 body. The second capture shows the full box.

The inspected views show the correct source IP and numeric port, immediate
session allowance, a visible host manager and removal result, combined
history and its filter, full Bridge output and copy feedback, the recent
Squid lines and copy feedback, and separate focus and scroll positions in
the combined panels. XTerm lacks some emoji glyphs with this font; the
adjacent text labels remain visible.

Final capture evidence:

| View | VHS PNG | Xvfb PNG |
| --- | --- | --- |
| Monitor | `/tmp/cooper-monitor.png` | `/tmp/cooper-monitor-small.png` |
| Immediate session allowance | `/tmp/cooper-monitor-allowed.png` | — |
| Session manager | `/tmp/cooper-session-v2.png` | `/tmp/cooper-session-small-v2.png` |
| Session removal | `/tmp/cooper-session-removed.png` | — |
| Combined history | `/tmp/cooper-history.png` | — |
| History filter | `/tmp/cooper-history-filter.png` | — |
| History detail | `/tmp/cooper-history-detail.png` | — |
| Squid selection and copy | `/tmp/cooper-squid-copy.png` | — |
| Bridge panes | `/tmp/cooper-bridge.png` | `/tmp/cooper-bridge-small.png` |
| Bridge output and copy | `/tmp/cooper-bridge-copy.png` | `/tmp/cooper-bridge-copy-small.png` |
| Route editor | `/tmp/cooper-bridge-editor-v2.png` | `/tmp/cooper-bridge-editor-small-v2.png` |
| Route validation and long input | — | `/tmp/cooper-bridge-editor-error-small-v2.png` |
| Runtime panes | `/tmp/cooper-runtime.png` | `/tmp/cooper-runtime-small.png` |
| About focus and scroll | — | `/tmp/cooper-runtime-about-small.png` |

To repeat the image checks, build the static fixture and use the repository
wrapper:

```bash
GOTOOLCHAIN=go1.25.0 CGO_ENABLED=0 go build -C ./cooper \
  -o /tmp/cooper-tui-capture . > /tmp/cooper-tui-capture-build.txt 2>&1
./scripts/capture-tui.sh --output /tmp/cooper-session.png \
  --wait-regex 'SESSION ACCESS' --type w --type s \
  -- /tmp/cooper-tui-capture tui-test --screen monitor \
  > /tmp/cooper-session-capture.txt 2>&1
```

Also capture `history`, `squid-logs`, `bridge`, and `runtime` with the
matching `--screen` value. On Bridge, `]`, Enter, and `y` exercise output
selection and copying. Repeat the combined panels at 80×24 with Xvfb.
All capture containers use fabricated data and no network.
