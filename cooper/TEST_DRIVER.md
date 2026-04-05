# Cooper Test Driver

`internal/testdriver` is Cooper's reusable runtime driver for end-to-end
verification below the TUI layer.

It is designed for scenarios where you want real Cooper behavior:
- real `CooperApp` startup and shutdown
- real Docker networks, proxy, barrels, and bridge
- real clipboard bridge HTTP endpoints
- persisted `config.json` verification
- barrel token lifecycle checks

This is different from `cooper tui-test`.

`cooper tui-test` is a fake-data visual QA mode for the Bubble Tea UI.
`internal/testdriver` is the real runtime harness that a future TUI driver can
build on top of.

## What It Solves

Before this package, ad hoc runtime checks tended to live in one-off test code
or temporary programs. That made it easy to verify one feature once, but hard
to reuse the same setup for the next feature.

The driver centralizes:
- temporary Cooper directory creation
- template rendering and CA generation
- app startup and teardown
- Docker cleanup
- barrel startup helpers
- clipboard token file helpers
- bridge HTTP helpers
- custom tool image helpers

## Package Layout

- `internal/testdriver`
  - reusable runtime driver and scenario helpers
- `cmd/cooper-test-driver`
  - thin CLI for running manual scenarios with the same driver code

## Current Scope

Today this is a runtime/app driver, not a terminal/TUI driver.

That distinction matters:
- runtime driver: drives `CooperApp`, Docker, bridge HTTP, and persisted state
- TUI driver: would drive keypresses, screen assertions, or a PTY session

The runtime layer should exist first so a future TUI driver can reuse its setup
instead of duplicating startup, cleanup, and fixture creation.

## Manual Usage

Run the built-in smoke scenario:

```bash
cd cooper
GOCACHE=/tmp/go-build-cache go run ./cmd/cooper-test-driver --scenario clipboard-smoke
```

Useful flags:

- `--prefix`: isolate Docker resources under a custom prefix
- `--keep`: keep the generated temporary Cooper directory after exit
- `--disable-host-clipboard`: skip host clipboard prerequisite checks
- `--timeout`: overall scenario timeout

Example:

```bash
cd cooper
GOCACHE=/tmp/go-build-cache go run ./cmd/cooper-test-driver \
  --scenario clipboard-smoke \
  --prefix test-mirror- \
  --keep
```

## Test Usage

Use the driver directly from integration tests:

```go
driver, err := testdriver.New(testdriver.Options{
	ImagePrefix:          testdriver.DefaultImagePrefix,
	DisableHostClipboard: true,
})
if err != nil {
	t.Fatal(err)
}
defer driver.Close()

ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
defer cancel()

if err := testdriver.RunClipboardSmoke(ctx, driver); err != nil {
	t.Fatal(err)
}
```

## Current Built-In Scenario

`clipboard-smoke` verifies:
- clipboard settings persist to `config.json`
- staged clipboard data is visible through `/clipboard/type`
- staged image bytes are retrievable through `/clipboard/image`
- barrel token rotation happens on restart
- barrel token revocation happens on stop
- custom images that set `COOPER_CLIPBOARD_MODE=off` keep that mode and are rejected by the clipboard bridge

## Extending It

Add new scenario helpers to `internal/testdriver` when a feature needs real
runtime validation.

Keep the package focused on reusable runtime primitives:
- start/stop Cooper
- create and manage barrels
- call bridge endpoints
- inspect persisted state
- assert container state

If later we want to test the real TUI, build a separate TUI driver on top of
these primitives rather than mixing Bubble Tea interactions into this package.
