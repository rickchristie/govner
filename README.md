# Govner

A collection of Go tools for fast local development loops and controlled AI-assisted coding.

## Tools

### [cooper](./cooper/) — Barrel-proof containers for undiluted AI

![cooper trailer](/cooper/docs/trailer.gif)

Run Claude Code, GitHub Copilot CLI, OpenAI Codex CLI, and OpenCode inside network-isolated Docker barrels. A real-time TUI shows every outbound request and lets you approve or deny it before it leaves your machine.

- **True network isolation.** Barrels sit on a Docker `--internal` network with no route to the public internet — not even raw sockets escape.
- **Live request control.** Non-whitelisted HTTPS requests surface in the TUI with a countdown. Approve, deny, or let them time out.
- **Safe host access.** Forward local ports and expose a controlled execution bridge without handing AI tools a shell on your machine.
- **Built for AI workflows.** Clipboard image staging, Playwright support, per-tool images, and multi-workspace monitoring, out of the box.

```bash
go install github.com/rickchristie/govner/cooper@latest

cooper configure
cooper build
cooper up
cooper cli claude
```

See [`cooper/README.md`](./cooper/) for the full architecture, security model, and command reference.

---

### [pgflock](./pgflock/) — PostgreSQL test database pool

![pgflock demo](/pgflock/doc/peek.gif)

Spawn, lock, and share memory-backed PostgreSQL databases for backend tests, with a TUI that shows pool usage live.

- **Disposable, isolated databases.** Skip the Compose juggling — pgflock runs tmpfs-backed Postgres instances on demand, so tests are fast and don't grind your SSD.
- **Pool-and-lock semantics.** Keep databases warm and allocate one when a test needs it. Locks release the moment the process exits — even on panic, timeout, or `Ctrl+C`.
- **Live visibility.** The TUI makes active locks, ownership, and lifecycle obvious instead of mysterious.

```bash
go install github.com/rickchristie/govner/pgflock@latest
```

---

### [gowt](./gowt/) — Go test watcher TUI

![gowt demo](/gowt/docs/peek.gif)

A terminal UI for running Go tests and reading results as they stream in.

- **Live streaming.** Tests animate, pass, and fail in place — no scrolling through a wall of raw output.
- **Tight iteration loop.** Rerun everything or just the failures with a keystroke.
- **Focused, not heavyweight.** Built for the edit-test-fix cycle, not as a dashboard.

```bash
go install github.com/rickchristie/govner/gowt@latest
```

## Install notes

If a binary is not found after `go install`, add Go's bin directory to your shell profile:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```
