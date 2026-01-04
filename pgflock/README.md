# pgflock

## Shepherd your test databases

![pgflock demo](doc/peek.gif)

### Spawn, lock, control memory-backed Postgres databases for testing backend code.

**Testing against actual database have immense benefits:**
1. We can test that our complex SQL queries actually work as intended.
2. We can create fuzzy data and test indexes actually works.
3. No need to create mocks for database code.
4. No chances of issues due to mocks not behaving as an actual database would.
5. Upgrading database? No problem! Just re-run the test against the new database version.

**For backend tests, we want to:**
- Run our tests in parallel.
- Complete each individual tests as fast as possible.
- Isolate each individual tests from each other.
- Re-run tests without wrecking our SSDs when resetting db for each test.

**`pgflock` provides:**
- Postgres instance and databases that run within docker, using memory filesystem so it's fast, and safe for SSD.
- Control over the number of docker instances, number of databases within each docker instances.
- Lock server so your parallel tests don't step over each other, you can grant each individual test direct control over database.
- Beautiful TUI where you can monitor your database harness usages in real-time.

## Installation

```bash
go install github.com/rickchristie/govner/pgflock@latest
```

Make sure `$GOPATH/bin` is in your `PATH`. Add this to your shell profile (`~/.bashrc`, `~/.zshrc`, or `~/.profile`):

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

Then reload your shell or run `source ~/.bashrc`.

## Quick Start

```bash
# 1. Configure pgflock (creates .pgflock/ directory)
pgflock configure

# 2. Build the PostgreSQL Docker image
pgflock build

# 3. Start the pool with TUI
pgflock up
```

## Commands

### `pgflock configure`

Interactive wizard to configure pgflock. Creates `.pgflock/` directory with:
- `config.yaml` - Configuration file
- `Dockerfile` - PostgreSQL Docker image
- `init.sh` - Database initialization script
- `postgresql.conf` - PostgreSQL configuration

Configuration options:
- Docker name prefix (default: current directory name)
- Number of PostgreSQL instances and starting port
- Databases per instance (default: 10)
- tmpfs/shm size for performance
- Locker server port (default: 9191)
- PostgreSQL settings (user, password, extensions, etc.)

### `pgflock build`

Builds the PostgreSQL Docker image using the generated Dockerfile.

### `pgflock up`

Starts the database pool:
1. Starts PostgreSQL Docker containers
2. Waits for PostgreSQL to be ready
3. Starts the locker server
4. Opens the TUI dashboard

**Flags:**
- `-i, --instances <n>` - Number of PostgreSQL instances (overrides config)
- `-d, --databases <n>` - Databases per instance (overrides config)

**Examples:**
```bash
# Use config defaults
pgflock up

# Override to use 2 instances with 5 databases each
pgflock up -i 2 -d 5

# Quick single-instance setup for local development
pgflock up --instances 1 --databases 3
```

**TUI Controls:**
- `q` - Quit (stops containers and server)
- `r` - Restart containers (unlocks all databases)
- `space` - Toggle between locked-only view and all databases view
- `u` - Force unlock selected database
- `c` - Copy psql connection command to clipboard
- `j/k` or arrow keys - Navigate database list

**Clipboard Support:**

The `c` key copies the psql connection command to your clipboard. Supported clipboard tools:
- **Wayland**: `wl-copy` (install with `sudo apt install wl-clipboard`)
- **X11**: `xclip` or `xsel` (install with `sudo apt install xclip`)
- **macOS**: `pbcopy` (built-in)
- **Windows/WSL**: `clip.exe` (built-in)

### `pgflock down`

Stops all PostgreSQL containers.

### `pgflock status`

Shows status of containers and locker server.

### `pgflock connect <port> <dbname>`

Connect to a database via psql. Example:
```bash
pgflock connect 5432 tester1
```

### `pgflock tail [port]`

Streams logs from a PostgreSQL container (equivalent to `docker logs --follow --tail 100`). If no port is specified, uses the starting port from config.

```bash
# Tail logs from the first instance (default port)
pgflock tail

# Tail logs from a specific instance
pgflock tail 5433
```

### `pgflock restart`

Restarts the database pool via HTTP API. This unlocks all databases and restarts PostgreSQL containers. Useful for recovering from stuck tests or when running in automated environments where the TUI is not available (e.g., AI agents in Docker containers).

```bash
pgflock restart
```

Requires `pgflock up` to be running. This command calls the locker server's `/restart` endpoint.

## Client Library

Use the client library in your test code:

```go
import "github.com/rickchristie/govner/pgflock/client"

func TestSomething(t *testing.T) {
    // Lock a database (blocks until available)
    // Place this in the "Before" section of your test.
    connStr, err := client.Lock(9191, "my-test", "pgflock")
    if err != nil {
        t.Fatal(err)
    }

    // Place this in the "After" section of your test.
    defer client.Unlock(9191, "pgflock", connStr)

    // Use connStr to connect to database
    db, err := sql.Open("postgres", connStr)
    // ... run tests ...
}
```

### Additional Client Functions

```go
// Check locker health
err := client.HealthCheck(9191)

// Get full status with lock details (useful for debugging)
status, err := client.GetStatus(9191)
fmt.Printf("Locked: %d, Free: %d\n", status.LockedDatabases, status.FreeDatabases)
for _, lock := range status.Locks {
    fmt.Printf("  %s locked by %s for %ds\n", lock.ConnString, lock.Marker, lock.DurationSeconds)
}

// Restart the database pool (unlocks all + restarts containers)
// WARNING! This will interrupt other parallel tests you have running!
err = client.Restart(9191, "pgflock")

// Just unlock all databases without restarting containers
count, err := client.UnlockAll(9191, "pgflock")
```

### HTTP API

The locker server exposes these endpoints. All endpoints except health-check require authentication via `password` query parameter.

**Lock a database:**
```
GET /lock?marker=<marker>&password=<password>
```
Returns: Connection string (blocks until database available)

**Unlock a database:**
```
POST /unlock?marker=<marker>&password=<password>
Body: <connection-string>
```

**Health check (with full state):**
```
GET /health-check
```
Returns detailed state including all locked databases:
```json
{
  "status": "ok",
  "total": 20,
  "locked": 3,
  "free": 17,
  "waiting": 0,
  "auto_unlock_minutes": 5,
  "locks": [
    {
      "conn_string": "postgresql://...",
      "marker": "TestUserCreate",
      "locked_at": "2024-01-15T10:30:00Z",
      "duration_seconds": 45
    }
  ]
}
```

**Unlock all databases:**
```
POST /unlock-all?marker=<marker>&password=<password>
```
Returns: `{"status":"ok","unlocked":N}`

**Restart database pool:**
```
POST /restart?marker=<marker>&password=<password>
```
Unlocks all databases and restarts PostgreSQL containers. Blocks until restart is complete.
Returns: `{"status":"ok","message":"Restart completed successfully"}`

**Force unlock a specific database:**
```
POST /force-unlock?marker=<marker>&password=<password>
Body: <connection-string>
```

**Unlock by marker:**
```
POST /unlock-by-marker?marker=<marker>&password=<password>&target=<target-marker>
```
Unlocks all databases locked by the specified target marker.

## Configuration

Example `.pgflock/config.yaml`:

```yaml
docker_name_prefix: myproject
instance_count: 2
starting_port: 5432
databases_per_instance: 10
tmpfs_size: 1024m
shm_size: 1g
locker_port: 9191
auto_unlock_minutes: 5
pg_username: tester
password: pgflock
database_prefix: tester
extensions:
  - postgis
  - pg_trgm
postgres_version: "15"
encoding: UTF8
lc_collate: en_US.UTF-8
lc_ctype: en_US.UTF-8
max_connections: 100
```

With `instance_count: 2` and `starting_port: 5432`, pgflock creates two PostgreSQL instances on ports 5432 and 5433.

## How It Works

1. **Pool Initialization**: On `pgflock up`, containers start and all databases are added to an available pool.

2. **Lock Request**: When a test calls `Lock()`:
   - Waits for an available database from the pool
   - Resets the database (DROP + CREATE from test_template)
   - Returns the connection string

3. **Unlock Request**: When a test calls `Unlock()`:
   - Returns the database to the available pool
   - Next waiting lock request receives it

4. **Auto-unlock**: Databases locked for too long (default: 5 minutes) are automatically unlocked.

## License

MIT License - see [LICENSE](../LICENSE) for details.

## ✨ Made with Claude

🛠️ Built together with [Claude Code](https://claude.ai/code)
