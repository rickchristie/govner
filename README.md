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

```mermaid
flowchart LR
    subgraph host["🖥️ Host Network"]
        proxy["🛡️ Squid Proxy<br/><small>localhost:3128</small>"]
        services["🗄️ Local Services<br/><small>Postgres · Redis · etc.</small>"]
    end

    subgraph container["🐳 Sandboxed Container <small>(bridge network)</small>"]
        ai["🤖 AI Assistants<br/><small>Claude Code · Copilot CLI</small>"]
        socat["🔌 socat<br/><small>port forwarding</small>"]
    end

    allowed["✅ anthropic.com<br/>github.com<br/>npmjs.org"]
    blocked["🚫 All Other<br/>Domains"]

    ai -->|"all traffic"| proxy
    proxy --> allowed
    proxy -.-> blocked
    socat <-.->|"localhost:5432"| services
```

Drop-in Docker sandbox for AI coding assistants with network isolation and domain whitelisting.

Supports Claude Code, GitHub Copilot CLI, and other CLI-based AI assistants. All network traffic is routed through a Squid proxy that only allows whitelisted domains (AI APIs, GitHub, npm).

```bash
# Run from your project directory
curl -sL https://github.com/rickchristie/govner/archive/refs/heads/main.tar.gz | tar -xz --strip-components=1 govner-main/sandb
sandb/install.sh
sandb/cli/build.sh
sandb/proxy/start.sh
sandb/shell.sh
```
