# Gowt test guide

Gowt's tests are organized around observable behavior. The model suite checks
the `go test -json` event-to-state contract, the view suite checks Bubble Tea
messages and terminal rendering, and the root suite uses fake application
boundaries for process, clipboard, and lifecycle behavior.

The automated suite must be deterministic and must not require a terminal, an
outside Go workspace, a display server, or a real clipboard. Use the storybook
for real-terminal inspection.

## Required validation

Run these commands from the repository root and keep their full logs:

```sh
go test -C ./gowt ./... > /tmp/gowt-go-test.txt 2>&1
go test -C ./gowt -race ./... > /tmp/gowt-race.txt 2>&1
go vet -C ./gowt ./... > /tmp/gowt-vet.txt 2>&1
go build -C ./gowt -o ./gowt . > /tmp/gowt-build.txt 2>&1
```

The Windows compile gate protects the platform-specific process implementation:

```sh
GOOS=windows GOARCH=amd64 go build -C ./gowt \
  -o /tmp/gowt-windows-amd64.exe . \
  > /tmp/gowt-windows-build.txt 2>&1
```

For coverage review:

```sh
go test -C ./gowt ./... \
  -coverprofile=/tmp/gowt-cover.out \
  > /tmp/gowt-cover.txt 2>&1
go -C ./gowt tool cover \
  -func=/tmp/gowt-cover.out \
  > /tmp/gowt-cover-func.txt 2>&1
```

Coverage is a review aid, not the acceptance criterion. Every regression test
should name the contract it protects; executing a line without asserting its
effect does not establish reliability.

The 2026-08-16 post-fix baseline is 93.6% overall: root 86.3%, model 96.8%,
utility 96.8%, and view 96.1%. The uncovered root paths are primarily the real
TTY owner and platform/error fallbacks; their injectable boundaries and
observable results are covered without opening a terminal in unit tests.

After the 2026-08-17 lifecycle and benchmark regressions, the baseline is 93.4%
overall: root 87.4%, model 96.9%, utility 96.8%, and view 95.4%. The aggregate
shift reflects the new platform-specific cleanup paths; their portable contract
is exercised through the bounded-drain and build-constraint tests.

## Manual terminal QA

`gowt --storybook` opens deterministic fixtures for passed, failed, skipped,
cached, fuzz, structured-log, Unicode, long-line, blank-line, and build-error
states. Use it to inspect color, resize behavior, navigation, search, modals,
and Processed/Raw switching in a real terminal without running another project.

The fixture is built by `newStorybookTree` and is tested like other startup
paths, so manual screenshots always begin from the same state.

## Suite map

### Root application package

- `runner_test.go`
  - original `go test` flag forwarding and single-rerun flag preservation
  - complete split-value/boolean flag classification and double-dash spellings
  - escaped test/subtest regexes and benchmark selection
  - JSON/stderr streaming, malformed JSON diagnostics, partial final records,
    and records larger than 1 MiB
  - successful/failing exit codes, cache cleanup, process termination, and
    output draining when descendants retain inherited descriptors
  - mutually exclusive platform process implementations, including iOS
- `app_test.go`
  - loaded/live constructors and typed asynchronous messages
  - generation filtering for starts, events, stderr, cache completion, and done
  - final event draining in `Update`, elapsed ticks, and output-buffer flushing
  - stderr attribution, including diagnostics without a package header
  - rerun, single-rerun, stop, quit, error, and clipboard modal state
  - tree/log/help routing and build-event relevance through `ImportPath`
  - clipboard command selection, failure, and timeout behavior
  - saved JSON loading, malformed data, missing files, and multi-megabyte events
- `main_test.go`
  - help and long version flags, including `-v` forwarding to `go test`
  - missing load arguments, usage, terminal errors, and final exit propagation
- `storybook_test.go`
  - deterministic fixture states and injected terminal startup

`runLoadModeWithProgram`, `runLiveModeWithProgram`, and
`runStorybookModeWithProgram` accept the terminal runner as an internal
application boundary. Production still has one `runProgram` path; tests can
exercise orchestration without opening a TTY.

### Model package

- `model_test.go`
  - package, test, subtest, fuzz, benchmark, example, and build lifecycles
  - benchmark `bench` terminals and package-terminal reconciliation
  - run/pause/continue/pass/fail/skip transitions and aggregate counts
  - node indexing, depth, parents, sorting, flattening, and cached propagation
  - split output reassembly, blank lines, final partial lines, and per-node
    buffers
  - raw/processed fan-out to packages and test ancestors
  - Go marker and JSON formatting plus CSI/OSC/control sanitization
- `logbuffer_test.go`
  - references, invalid bounds, node-log metrics, full/incremental rendering,
    empty logs, overlapping references, and rebuilds

Event tests call real `TestTree.ProcessEvent`; they do not pre-fill derived
counts. This catches drift between event transitions and rendered aggregates.

### View package

- `treeview_test.go`
  - navigation, paging, scrolling, expansion, replacement, and parent selection
  - All/Focus filtering, including package-only setup/build failures
  - search input, Unicode deletion, ancestor inclusion, ordering, and clearing
  - typed requests and running/done/stopped/failed/empty rendering
- `logview_test.go`
  - Processed/Raw renderers, independent offsets, and safe mode changes
  - late renderer creation, incremental streaming, shared-pointer completion,
    and end markers
  - search lifecycle, ANSI-safe highlighting, match navigation, and streaming
  - grapheme-safe, ANSI-aware terminal-cell wrapping
- `helpview_test.go`
  - source content, sizing, scrolling, closing, and injected clipboard hints
- `modal_test.go`
  - box construction, overlays, Unicode cell placement, and ANSI sanitization
- `icons_test.go`
  - status icons and complete spinner cycles

Controller effects are asserted through typed requests. Leaf views do not
invoke shell, environment, clipboard, filesystem, or process operations.

### Utility package

- `ansi_test.go` covers CSI, OSC, and unsafe C0/C1 controls while retaining
  newlines and tabs.
- `format_test.go` covers duration units, rounding boundaries, and stable width.
- `json_test.go` and `json_edge_test.go` cover inline/multiline logs, nested
  containers, stable ordering, block scalars, input envelopes, and exact
  integer preservation above 2^53.

The former `json_integration_test.go` duplicated a sanitizer instead of calling
production code and only logged known-bad OSC behavior. The focused model,
utility, and view regressions now assert the real production path.

## 2026-08-16 reliability regression ledger

The review reproduced and fixed these defects. Each item has a permanent test:

1. `-v` is forwarded to `go test`; only `--version` prints Gowt's version.
2. CSI, OSC, C0, and C1 terminal controls are removed from displayed, Raw, and
   copied logs.
3. Selecting another node clears confirmed search/highlight state.
4. Switching Processed/Raw clears renderer-specific search/highlight state.
5. wrapping uses ANSI-aware grapheme and terminal-cell widths.
6. structured-log numbers use `json.Number`, preserving integers above 2^53.
7. every slash-delimited rerun pattern component is regex-escaped.
8. runner and file loading accept events larger than 1 MiB and report malformed
   live JSON as an operational error.
9. Raw logs preserve blank lines.
10. terminal events and run completion flush output without a final newline.
11. cached status requires the exact tab-delimited Go package status and the
    matching package path.
12. hosted module roots render as the repository name; subpackages retain only
    the path below the conventional domain/owner/repository prefix.
13. expand-all state is recomputed when a tree changes or receives new nodes.
14. LogView snapshots status so shared node pointers cannot hide completion.
15. unsupported uppercase `R` behavior was removed and lowercase `r` help now
    truthfully says it reruns all tests.
16. start, stream, cache, stop, and clipboard failures become explicit failed
    state and error modals instead of green Done state.
17. starts and cache completions carry a generation; rerun/stop invalidates old
    commands before asynchronous cleanup begins.
18. modal padding and insertion use terminal-cell widths.
19. both search inputs remove a complete UTF-8 rune on Backspace.
20. late renderers and completion-only changes immediately refresh the viewport.
21. named events are accepted from Go itself, including fuzz, benchmark,
    example, seed, and generated names—not only `Test*`.
22. live build-log relevance falls back from `Package` to `ImportPath`.
23. event-drain commands return typed pending data; only `Update` mutates tree
    state, eliminating the confirmed race.
24. package/test reruns retain original build/test flags and test-binary args
    while replacing package and selection operands.
25. process-group code is build-tagged, with a Windows implementation and a
    Windows compile gate.

Additional hardening from the fix pass includes bounded clipboard execution,
generation-safe user stops, visibility for package-only failures in Focus mode,
safe tiny-terminal dimensions, a deterministic storybook, and removal of dead
view requests/helpers. `TestNode.GetFullOutput` and `LogRenderer.LineCount`
remain intentionally supported, directly tested model query APIs.

## 2026-08-17 follow-up reliability ledger

A second review found five boundary cases. Each now has a regression test:

1. Gowt owns the child pipes and observes `Cmd.Wait` before waiting for EOF.
   Surviving process-group descendants are terminated; platforms without that
   primitive use a bounded drain interval. Complete and unterminated output
   already produced by the parent is delivered before `Done`.
2. `bench` is a passing terminal action. A terminal package event also
   reconciles pending/running descendants because Go versions may omit a
   benchmark-level terminal record.
3. selected reruns preserve all documented split-value build and test flags,
   including `-buildmode`, `-cpuprofile`, `-o`, `-compiler`, and `-pgo`.
   Boolean flags such as bare `-buildvcs` never consume a package operand.
4. single- and double-dash flag spellings share selection, JSON, and value
   semantics, both before and after `-args`. Gowt also keeps `-C` ahead of the
   injected `-json` flag because the Go command requires it to appear first;
   a bare `--` tail remains after Gowt's generated selection flags.
5. iOS selects exactly one process implementation; its implicit `darwin` tag
   no longer overlaps the fallback implementation.
6. stdout and stderr readers drain into internal queues that never wait on the
   bounded presentation channels. Saturating both 1,000-entry channels cannot
   block process completion; the terminal result carries the undelivered
   suffix, and tests assert that all 1,100 records from each real pipe arrive
   exactly once.
7. selected reruns preserve the split values of Go's unstable
   `-debug-actiongraph`, `-debug-runtime-trace`, and `-debug-trace` flags. This
   keeps the selected package from being consumed as a debug artifact path.

## State ownership invariants

- `Update` is the only application state-transition owner. Commands return
  typed facts and never call `TestTree.ProcessEvent`.
- `View` is pure. Tree rendering computes presentation without writing cache
  fields into shared nodes.
- Clipboard/environment detection is behind `Clipboard`; HelpView receives a
  string snapshot.
- one goroutine owns `Cmd.Wait` and observes process completion independently
  of output EOF. Caller-owned pipes let readers drain safely after that fact;
  orphan-held descriptors have a finite cleanup path. `Kill` only requests
  termination.
- `EventStream.Done` follows closure of both data channels. Values delivered on
  those channels are an ordered prefix; any suffix that could not fit without
  presentation backpressure is carried by `TestResult` and applied by
  `Update`. Completion therefore stays bounded while every process record is
  still made available to the model.
