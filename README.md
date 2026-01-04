# ⚙ Govner

A collection of Go development tools.

## Tools

### [gowt](./gowt/) - Go Test Watcher TUI

![gowt demo](/gowt/docs/peek.gif)

Terminal UI for running and viewing Go test results in real-time.

```bash
go install github.com/rickchristie/govner/gowt@latest
```

Make sure `$GOPATH/bin` is in your `PATH`. Add this to your `~/.bashrc`, `~/.zshrc`, or `~/.profile`:

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

---

### [pgflock](./pgflock/) - PostgreSQL Test Database Pool

![pgflock demo](/pgflock/doc/peek.gif)

Spawn, lock, and control memory-backed PostgreSQL databases for testing backend code. Features a beautiful TUI for monitoring database usage in real-time.

```bash
go install github.com/rickchristie/govner/pgflock@latest
```

---

### [sandb](./sandb/) - AI Sandbox

Drop-in Docker sandbox for AI coding assistants with network isolation and domain whitelisting.

Supports Claude Code, GitHub Copilot CLI, and other CLI-based AI assistants. All network traffic is routed through a Squid proxy that only allows whitelisted domains (AI APIs, GitHub, npm, Go proxy).

```bash
cp -r sandb/ /path/to/your/project/
sandb/install.sh
sandb/cli/build.sh
sandb/proxy/start.sh
sandb/shell.sh
```
