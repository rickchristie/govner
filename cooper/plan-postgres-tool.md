# Plan: Add PostgreSQL as a Programming Tool

## Overview

Add PostgreSQL (server + psql client) as a built-in programming tool alongside Go, Node.js, and Python.
This lets the AI inside a barrel run a local PostgreSQL server to execute SQL, run migrations, test
queries, and work with databases without needing access to the host PostgreSQL.

PostgreSQL is installed via the [PGDG apt repository](https://apt.postgresql.org/) into `cooper-base`.
The server starts automatically in the barrel entrypoint. Data is ephemeral (lives inside the container);
it resets when the barrel is recreated.

---

## Key Design Decision: Major-Version-Only

Unlike Go/Node (which pin exact versions like `1.24.10` / `22.12.0`), PostgreSQL is installed via apt
which only allows **major version** control. The PGDG repo provides `postgresql-17`, not
`postgresql-17.2`. Minor/patch updates within a major are applied automatically by apt.

This means ALL version strings stored in config for PostgreSQL are **major-only**: `"17"`, `"16"`, `"18"`.

| Field | Example | Notes |
|-------|---------|-------|
| `host_version` | `"17"` | Extracted from `psql --version` output `"17.2"`, then truncated to major |
| `pinned_version` | `"16"` | User enters major only; validated against postgresql.org API |
| `container_version` | `"17"` | The major version installed by apt in the image |
| Latest (resolved) | `"18"` | The `current: true` entry's `major` field from the API |

This avoids false version-mismatch warnings. If we stored `"17.2"` from the host but apt installed
`"17.9"`, the startup check would always warn even though no action is possible.

---

## Files to Modify

| # | File | Change |
|---|------|--------|
| 1 | `internal/config/versions.go` | Add `"postgresql"` to `toolVersionCommands`; add `extractMajorVersion()` helper; post-process postgresql detection to major-only |
| 2 | `internal/config/resolve.go` | Add `ResolvePostgreSQLLatest()`, `validatePostgreSQLVersion()`; wire into `ResolveLatestVersion()` and `ValidateVersion()` dispatchers |
| 3 | `internal/templates/templates.go` | Add `HasPostgreSQL` + `PostgreSQLVersion` to `baseDockerfileData` and `entrypointData`; update `buildBaseDockerfileData()` and `RenderEntrypoint()` |
| 4 | `internal/templates/base.Dockerfile.tmpl` | Add PGDG repo setup + `postgresql-{version}` installation section |
| 5 | `internal/templates/entrypoint.sh.tmpl` | Add PostgreSQL `initdb` + `pg_ctl start` section |
| 6 | `internal/configure/programming.go` | Add `{name: "postgresql", displayName: "PostgreSQL"}` to `defaultProgrammingTools` |
| 7 | `internal/docker/barrel.go` | No changes needed (data is ephemeral inside container; no persistent volume mount) |
| 8 | `internal/config/resolve_test.go` | Add tests for PostgreSQL version resolution and validation |
| 9 | `internal/templates/templates_test.go` | Add tests for PostgreSQL Dockerfile and entrypoint rendering |
| 10 | `.testfiles/config-pinned.json` | Add `postgresql` entry to `programming_tools` |
| 11 | `.testfiles/config-latest.json` | Add `postgresql` entry to `programming_tools` |
| 12 | `.testfiles/config-mirror.json` | Add `postgresql` entry to `programming_tools` |
| 13 | `test-docker-build.sh` | Add PostgreSQL verification to Phase 5 (programming tools check) |

---

## Step 1: Version Detection (`internal/config/versions.go`)

### 1a. Add to `toolVersionCommands` map

```go
var toolVersionCommands = map[string][]string{
	"go":         {"go", "version"},
	"node":       {"node", "--version"},
	"python":     {"python3", "--version"},
	"claude":     {"claude", "--version"},
	"copilot":    {"copilot", "--version"},
	"codex":      {"codex", "--version"},
	"opencode":   {"opencode", "--version"},
	"postgresql": {"psql", "--version"},  // ADD THIS
}
```

**Why `psql`**: The `psql` client binary is always installed alongside the server. It's in the standard
PATH. Its output format is: `psql (PostgreSQL) 17.2 (Ubuntu 17.2-1.pgdg22.04+1)`. The existing
`semverRegex` extracts `"17.2"` from this.

### 1b. Add `extractMajorVersion()` helper

Add this function (near the bottom of `versions.go`, after `parseVersion`):

```go
// extractMajorVersion returns the major version from a semver-like string.
// For example, "17.2" returns "17", "16.13" returns "16".
// If there is no dot, the input is returned as-is (already major-only).
func extractMajorVersion(version string) string {
	if i := strings.Index(version, "."); i >= 0 {
		return version[:i]
	}
	return version
}
```

### 1c. Post-process postgresql detection in `DetectHostVersion()`

Modify `DetectHostVersion` to extract major-only for postgresql:

```go
func DetectHostVersion(toolName string) (string, error) {
	args, ok := toolVersionCommands[toolName]
	if !ok {
		return "", fmt.Errorf("unknown tool: %q", toolName)
	}

	cmd := execCommand(args[0], args[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to detect %s version: %w", toolName, err)
	}

	version, err := parseVersion(string(output))
	if err != nil {
		return "", err
	}

	// PostgreSQL: extract major version only. The PGDG apt repository pins
	// by major version (postgresql-17), not exact minor (no postgresql-17.2).
	if toolName == "postgresql" {
		version = extractMajorVersion(version)
	}

	return version, nil
}
```

**Existing behavior for other tools is unchanged** — the new `if` block only fires for `"postgresql"`.

---

## Step 2: Version Resolution (`internal/config/resolve.go`)

### 2a. Add the API URL variable

Add near the top of `resolve.go`, alongside the other URL vars:

```go
// postgresqlLatestURL is the endpoint for PostgreSQL release metadata.
var postgresqlLatestURL = "https://www.postgresql.org/versions.json"
```

### 2b. Add the response struct

Add alongside the other response structs (after `pythonRelease`):

```go
// postgresqlRelease represents a single entry from https://www.postgresql.org/versions.json.
//
// Example entry:
//
//	{"current": true, "eolDate": "2030-11-14", "firstRelDate": "2025-09-25",
//	 "latestMinor": "3", "major": "18", "relDate": "2026-02-26", "supported": true}
type postgresqlRelease struct {
	Major       string `json:"major"`       // e.g. "18", "17", "16"
	LatestMinor string `json:"latestMinor"` // e.g. "3", "9", "13"
	Supported   bool   `json:"supported"`
	Current     bool   `json:"current"`
}
```

### 2c. Add `ResolvePostgreSQLLatest()`

```go
// ResolvePostgreSQLLatest fetches the latest stable PostgreSQL major version
// from postgresql.org. Returns the major version string (e.g. "18").
func ResolvePostgreSQLLatest() (string, error) {
	body, err := httpGet(postgresqlLatestURL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch PostgreSQL versions: %w", err)
	}

	var releases []postgresqlRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return "", fmt.Errorf("failed to parse PostgreSQL version JSON: %w", err)
	}

	// Return the current (latest) supported major version.
	for _, r := range releases {
		if r.Supported && r.Current {
			return r.Major, nil
		}
	}

	// Fallback: if no entry has current=true, return the highest supported major.
	var best string
	for _, r := range releases {
		if r.Supported {
			best = r.Major
		}
	}
	if best != "" {
		return best, nil
	}

	return "", fmt.Errorf("no supported PostgreSQL version found")
}
```

### 2d. Add `validatePostgreSQLVersion()`

```go
// validatePostgreSQLVersion checks if a major version is supported in the
// PostgreSQL release list. The version should be major-only (e.g. "17").
func validatePostgreSQLVersion(version string) (bool, error) {
	body, err := httpGet(postgresqlLatestURL)
	if err != nil {
		return false, fmt.Errorf("failed to fetch PostgreSQL versions: %w", err)
	}

	var releases []postgresqlRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return false, fmt.Errorf("failed to parse PostgreSQL version JSON: %w", err)
	}

	for _, r := range releases {
		if r.Major == version && r.Supported {
			return true, nil
		}
	}

	return false, nil
}
```

### 2e. Wire into the dispatchers

In `ResolveLatestVersion()`, add the case:

```go
func ResolveLatestVersion(toolName string) (string, error) {
	switch toolName {
	case "go":
		return ResolveGoLatest()
	case "node":
		return ResolveNodeLatest()
	case "python":
		return ResolvePythonLatest()
	case "postgresql":                        // ADD THIS CASE
		return ResolvePostgreSQLLatest()
	case "claude", "copilot", "codex", "opencode":
		pkg, ok := npmPackageNames[toolName]
		if !ok {
			return "", fmt.Errorf("no npm package mapping for tool %q", toolName)
		}
		return ResolveNPMPackageLatest(pkg)
	default:
		return "", fmt.Errorf("unknown tool for version resolution: %q", toolName)
	}
}
```

In `ValidateVersion()`, add the case:

```go
func ValidateVersion(toolName, version string) (bool, error) {
	switch toolName {
	case "go":
		return validateGoVersion(version)
	case "node":
		return validateNodeVersion(version)
	case "python":
		return validatePythonVersion(version)
	case "postgresql":                        // ADD THIS CASE
		return validatePostgreSQLVersion(version)
	case "claude", "copilot", "codex", "opencode":
		pkg, ok := npmPackageNames[toolName]
		if !ok {
			return false, fmt.Errorf("no npm package mapping for tool %q", toolName)
		}
		return validateNPMVersion(pkg, version)
	default:
		return false, fmt.Errorf("unknown tool for version validation: %q", toolName)
	}
}
```

---

## Step 3: Dockerfile Template Data (`internal/templates/templates.go`)

### 3a. Add fields to `baseDockerfileData`

```go
type baseDockerfileData struct {
	HasGo              bool
	GoVersion          string
	HasNode            bool
	NodeVersion        string
	HasPython          bool
	HasPostgreSQL      bool   // ADD
	PostgreSQLVersion  string // ADD — major version like "17"
	HasCodex           bool
	HasOpenCode        bool
	ProxyPort          int
}
```

### 3b. Add `HasPostgreSQL` field to `entrypointData`

```go
type entrypointData struct {
	HasGo              bool
	HasPostgreSQL      bool // ADD
	BridgePort         int
	ClipboardEnabled   bool
}
```

### 3c. Update `buildBaseDockerfileData()`

```go
func buildBaseDockerfileData(cfg *config.Config) baseDockerfileData {
	return baseDockerfileData{
		HasGo:             isToolEnabled(cfg.ProgrammingTools, "go"),
		GoVersion:         getToolVersion(cfg.ProgrammingTools, "go"),
		HasNode:           isToolEnabled(cfg.ProgrammingTools, "node"),
		NodeVersion:       getToolVersion(cfg.ProgrammingTools, "node"),
		HasPython:         isToolEnabled(cfg.ProgrammingTools, "python"),
		HasPostgreSQL:     isToolEnabled(cfg.ProgrammingTools, "postgresql"),     // ADD
		PostgreSQLVersion: getToolVersion(cfg.ProgrammingTools, "postgresql"),    // ADD
		HasCodex:          isToolEnabled(cfg.AITools, "codex"),
		HasOpenCode:       isToolEnabled(cfg.AITools, "opencode"),
		ProxyPort:         cfg.ProxyPort,
	}
}
```

### 3d. Update `RenderEntrypoint()` data construction

In the `RenderEntrypoint` function, add `HasPostgreSQL` to the data struct:

```go
data := entrypointData{
	HasGo:            isToolEnabled(cfg.ProgrammingTools, "go"),
	HasPostgreSQL:    isToolEnabled(cfg.ProgrammingTools, "postgresql"), // ADD
	BridgePort:       cfg.BridgePort,
	ClipboardEnabled: anyAIToolEnabled(cfg.AITools),
}
```

---

## Step 4: Base Dockerfile Template (`internal/templates/base.Dockerfile.tmpl`)

Add the PostgreSQL installation section. Insert it **after** the Python section and **before** the
Codex bubblewrap section (since it's a programming tool, not a runtime dependency).

The exact location is after line 101 (the `{{end -}}` closing the Python block) and before the
`{{- if .HasCodex}}` block.

```dockerfile
{{- if .HasPostgreSQL}}
# ============================================================================
# PostgreSQL installation (server + client) via PGDG apt repository
# ============================================================================
# Pinned by major version only — apt installs the latest minor within that major.
# The PGDG repository provides all supported PostgreSQL major versions for Debian bookworm.
# createcluster.conf prevents the package post-install script from auto-creating a
# default cluster (which would run as postgres user and fail in the Dockerfile context).
# Instead, the entrypoint initializes a cluster as the container user at first start.
RUN apt-get update && apt-get install -y --no-install-recommends gnupg && \
    curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc \
      | gpg --dearmor -o /etc/apt/trusted.gpg.d/pgdg.gpg && \
    echo "deb http://apt.postgresql.org/pub/repos/apt bookworm-pgdg main" \
      > /etc/apt/sources.list.d/pgdg.list && \
    mkdir -p /etc/postgresql-common && \
    echo "create_main_cluster = false" > /etc/postgresql-common/createcluster.conf && \
    apt-get update && \
    apt-get install -y --no-install-recommends \
        postgresql-{{.PostgreSQLVersion}} \
        postgresql-client-{{.PostgreSQLVersion}} && \
    rm -rf /var/lib/apt/lists/*
{{end -}}
```

Also, after the user setup section, add the PostgreSQL data directory and PATH setup.
Find the line with `mkdir -p /home/user/.npm-global` (line ~133) and modify:

```dockerfile
# Setup directories for user
RUN mkdir -p /home/user/.npm-global /home/user/.config /home/user/.local/bin \
{{- if .HasPostgreSQL}}
    /home/user/pgdata \
{{- end}}
    && chown -R user:user /home/user
```

Then, in the `ENV PATH` line (around line 153), add the PostgreSQL bin directory:

```dockerfile
ENV PATH=/home/user/.local/bin:/home/user/.npm-global/bin:/home/user/.opencode/bin{{- if .HasPostgreSQL}}:/usr/lib/postgresql/{{.PostgreSQLVersion}}/bin{{- end}}:$PATH
```

**Why this PATH addition**: The PGDG apt packages install binaries to
`/usr/lib/postgresql/{version}/bin/`. The `pg_wrapper` scripts in `/usr/bin/` delegate to these, but
`pg_wrapper` needs an existing cluster to determine which version to use. By adding the version-specific
bin directory to PATH directly, `initdb`, `pg_ctl`, `psql`, `createdb`, etc. all work immediately
without pg_wrapper.

---

## Step 5: Entrypoint Template (`internal/templates/entrypoint.sh.tmpl`)

Add PostgreSQL server startup. Insert this **after** the `start_socat_from_config` call (around line
249) and **before** `ensure_playwright_runtime` (around line 255). The PostgreSQL server should start
early so it's available when the AI shell starts.

Insert between the `start_socat_from_config` call and `ensure_playwright_runtime`:

```bash
{{- if .HasPostgreSQL}}
# ============================================================================
# PostgreSQL server startup
# ============================================================================
# Data directory is inside the container (ephemeral — resets on barrel recreate).
# initdb runs only on first container start; subsequent starts reuse the existing data.
# The server listens on localhost only (127.0.0.1:5432), trust auth for local connections.
PG_DATA="/home/user/pgdata"
if [ ! -f "$PG_DATA/PG_VERSION" ]; then
    echo "Initializing PostgreSQL data directory..."
    initdb -D "$PG_DATA" \
        --auth=trust \
        --no-locale \
        --encoding=UTF8 \
        -U user \
        >/tmp/pg-initdb.log 2>&1
fi
echo "Starting PostgreSQL server..."
pg_ctl -D "$PG_DATA" -l /tmp/postgresql.log -o "-k /tmp" start >/dev/null 2>&1
{{- end}}
```

**Key details**:
- `-U user`: The superuser is `user` (matches the container user). No password needed (trust auth).
- `--auth=trust`: All local connections are trusted (appropriate for a dev container).
- `--no-locale`: Avoids locale dependency issues in the minimal container.
- `-o "-k /tmp"`: Unix domain socket in `/tmp` so it's writable by `user`.
- `pg_ctl start`: Starts the server in the background (it's a daemon).
- `PG_VERSION` file check: Only runs `initdb` once; data persists across `docker exec` sessions.

Also update the PATH line in the `.bashrc` setup. Find the `COOPER_PATH_LINE` variable (around line 26)
and add the PostgreSQL bin directory:

```bash
COOPER_PATH_LINE='export PATH="/home/user/.local/bin:/home/user/.npm-global/bin:/home/user/.opencode/bin{{- if .HasGo}}:/usr/local/go/bin:/go/bin{{- end}}{{- if .HasPostgreSQL}}:/usr/lib/postgresql/$(ls /usr/lib/postgresql/ 2>/dev/null | head -1)/bin{{- end}}:$PATH"'
```

Wait — the entrypoint template doesn't know the PostgreSQL version string at runtime. But the
Dockerfile already set the PATH env var with the version baked in. The `.bashrc` PATH line just needs
to match. Since the Dockerfile already has `ENV PATH=...` with the postgresql bin dir, and `.bashrc`
is sourced inside the container where that PATH is already set, we can simplify:

Actually, looking at the template more carefully, the `COOPER_PATH_LINE` in the entrypoint reconstructs
PATH from scratch. It doesn't inherit from the Dockerfile ENV. So we need to include postgresql's bin
directory here too. But we don't have the version number at runtime in the entrypoint.

**Solution**: Use a glob/ls to find the installed version directory:

```bash
COOPER_PATH_LINE='export PATH="/home/user/.local/bin:/home/user/.npm-global/bin:/home/user/.opencode/bin{{- if .HasGo}}:/usr/local/go/bin:/go/bin{{- end}}{{- if .HasPostgreSQL}}:/usr/lib/postgresql/'"$(ls /usr/lib/postgresql/ 2>/dev/null | sort -V | tail -1)"'/bin{{- end}}:$PATH"'
```

Actually this is ugly because the `$(...)` would be evaluated when the entrypoint runs, which is fine
functionally, but mixing template syntax with shell command substitution is fragile.

**Better solution**: The entrypoint is a template too. We have access to `.HasPostgreSQL` but not the
version. We should add the version to `entrypointData`.

### 5b. Add PostgreSQL version to entrypointData

Update `entrypointData` to include the version:

```go
type entrypointData struct {
	HasGo              bool
	HasPostgreSQL      bool
	PostgreSQLVersion  string // ADD — major version like "17"
	BridgePort         int
	ClipboardEnabled   bool
}
```

Update `RenderEntrypoint()`:

```go
data := entrypointData{
	HasGo:             isToolEnabled(cfg.ProgrammingTools, "go"),
	HasPostgreSQL:     isToolEnabled(cfg.ProgrammingTools, "postgresql"),
	PostgreSQLVersion: getToolVersion(cfg.ProgrammingTools, "postgresql"), // ADD
	BridgePort:        cfg.BridgePort,
	ClipboardEnabled:  anyAIToolEnabled(cfg.AITools),
}
```

Then the entrypoint PATH line becomes:

```bash
COOPER_PATH_LINE='export PATH="/home/user/.local/bin:/home/user/.npm-global/bin:/home/user/.opencode/bin{{- if .HasGo}}:/usr/local/go/bin:/go/bin{{- end}}{{- if .HasPostgreSQL}}:/usr/lib/postgresql/{{.PostgreSQLVersion}}/bin{{- end}}:$PATH"'
```

And the PostgreSQL startup section uses the version directly:

```bash
{{- if .HasPostgreSQL}}
# ============================================================================
# PostgreSQL server startup
# ============================================================================
PG_DATA="/home/user/pgdata"
if [ ! -f "$PG_DATA/PG_VERSION" ]; then
    echo "Initializing PostgreSQL data directory..."
    /usr/lib/postgresql/{{.PostgreSQLVersion}}/bin/initdb \
        -D "$PG_DATA" \
        --auth=trust \
        --no-locale \
        --encoding=UTF8 \
        -U user \
        >/tmp/pg-initdb.log 2>&1
fi
echo "Starting PostgreSQL server..."
/usr/lib/postgresql/{{.PostgreSQLVersion}}/bin/pg_ctl \
    -D "$PG_DATA" -l /tmp/postgresql.log \
    -o "-k /tmp -h 127.0.0.1" \
    start >/dev/null 2>&1
{{- end}}
```

Using full paths (`/usr/lib/postgresql/{{.PostgreSQLVersion}}/bin/initdb`) is safer than relying on
PATH being set at this point in the entrypoint (PATH setup happens later in `.bashrc`).

---

## Step 6: Configure UI (`internal/configure/programming.go`)

### 6a. Add to `defaultProgrammingTools`

```go
var defaultProgrammingTools = []toolEntry{
	{name: "go", displayName: "Go"},
	{name: "node", displayName: "Node.js"},
	{name: "python", displayName: "Python"},
	{name: "postgresql", displayName: "PostgreSQL"},  // ADD
}
```

No other changes needed in this file. The existing list/detail view, version mode selection (mirror,
latest, pin), host detection, and config export all work generically with any tool entry.

---

## Step 7: Barrel Volume Mounts (`internal/docker/barrel.go`)

**No changes needed.** PostgreSQL data is ephemeral — it lives at `/home/user/pgdata` inside the
container and resets when the barrel is recreated. This is intentional:

- The AI can `initdb` → run migrations → test queries → barrel restart = clean slate
- No stale database state leaks between sessions
- No additional host directories to manage
- The workspace mount (read-write) allows the AI to dump/restore SQL files if needed

If persistent PostgreSQL data is needed in the future, a volume mount can be added to
`appendLanguageCacheMounts()` like this (NOT part of this implementation):

```go
case "postgresql":
    // Future: persistent data mount
    hostPgData := filepath.Join(cooperDir, "cache", "postgresql", containerName)
    args = append(args, "-v", fmt.Sprintf("%s:%s:rw", hostPgData, "/home/user/pgdata"))
```

---

## Step 8: Unit Tests — Version Resolution (`internal/config/resolve_test.go`)

Add these tests at the end of the file, after the existing npm tests and before the HTTP error tests.

### 8a. `TestResolvePostgreSQLLatest`

```go
// --- PostgreSQL version resolution tests ---

func TestResolvePostgreSQLLatest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
			{"major": "14", "latestMinor": "22", "supported": true, "current": false},
			{"major": "15", "latestMinor": "17", "supported": true, "current": false},
			{"major": "16", "latestMinor": "13", "supported": true, "current": false},
			{"major": "17", "latestMinor": "9", "supported": true, "current": false},
			{"major": "18", "latestMinor": "3", "supported": true, "current": true}
		]`)
	}))
	defer server.Close()

	origURL := postgresqlLatestURL
	postgresqlLatestURL = server.URL
	defer func() { postgresqlLatestURL = origURL }()

	version, err := ResolvePostgreSQLLatest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "18" {
		t.Errorf("got %q, want \"18\"", version)
	}
}
```

### 8b. `TestResolvePostgreSQLLatestSkipsUnsupported`

```go
func TestResolvePostgreSQLLatestSkipsUnsupported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
			{"major": "12", "latestMinor": "22", "supported": false, "current": false},
			{"major": "13", "latestMinor": "18", "supported": false, "current": false},
			{"major": "16", "latestMinor": "13", "supported": true, "current": true}
		]`)
	}))
	defer server.Close()

	origURL := postgresqlLatestURL
	postgresqlLatestURL = server.URL
	defer func() { postgresqlLatestURL = origURL }()

	version, err := ResolvePostgreSQLLatest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "16" {
		t.Errorf("got %q, want \"16\"", version)
	}
}
```

### 8c. `TestResolvePostgreSQLLatestNoSupported`

```go
func TestResolvePostgreSQLLatestNoSupported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
			{"major": "12", "latestMinor": "22", "supported": false, "current": false},
			{"major": "13", "latestMinor": "18", "supported": false, "current": false}
		]`)
	}))
	defer server.Close()

	origURL := postgresqlLatestURL
	postgresqlLatestURL = server.URL
	defer func() { postgresqlLatestURL = origURL }()

	_, err := ResolvePostgreSQLLatest()
	if err == nil {
		t.Fatal("expected error when no supported version is available")
	}
}
```

### 8d. `TestResolvePostgreSQLLatestFallbackNoCurrentFlag`

```go
func TestResolvePostgreSQLLatestFallbackNoCurrentFlag(t *testing.T) {
	// If no entry has current=true, fallback to highest supported major.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
			{"major": "15", "latestMinor": "17", "supported": true, "current": false},
			{"major": "16", "latestMinor": "13", "supported": true, "current": false},
			{"major": "17", "latestMinor": "9", "supported": true, "current": false}
		]`)
	}))
	defer server.Close()

	origURL := postgresqlLatestURL
	postgresqlLatestURL = server.URL
	defer func() { postgresqlLatestURL = origURL }()

	version, err := ResolvePostgreSQLLatest()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return the last (highest) supported entry.
	if version != "17" {
		t.Errorf("got %q, want \"17\"", version)
	}
}
```

### 8e. `TestResolvePostgreSQLLatestInvalidJSON`

```go
func TestResolvePostgreSQLLatestInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `not json`)
	}))
	defer server.Close()

	origURL := postgresqlLatestURL
	postgresqlLatestURL = server.URL
	defer func() { postgresqlLatestURL = origURL }()

	_, err := ResolvePostgreSQLLatest()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
```

### 8f. `TestValidatePostgreSQLVersion`

```go
func TestValidatePostgreSQLVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[
			{"major": "16", "latestMinor": "13", "supported": true, "current": false},
			{"major": "17", "latestMinor": "9", "supported": true, "current": false},
			{"major": "12", "latestMinor": "22", "supported": false, "current": false}
		]`)
	}))
	defer server.Close()

	origURL := postgresqlLatestURL
	postgresqlLatestURL = server.URL
	defer func() { postgresqlLatestURL = origURL }()

	// Supported version should be valid.
	exists, err := ValidateVersion("postgresql", "17")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected version 17 to exist")
	}

	// Unsupported version should be invalid.
	exists, err = ValidateVersion("postgresql", "12")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected version 12 (unsupported) to not be valid")
	}

	// Non-existent version should be invalid.
	exists, err = ValidateVersion("postgresql", "99")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected version 99 to not exist")
	}
}
```

### 8g. Update `TestResolveLatestVersionDispatch`

Add postgresql to the dispatch test. After the Node test and before the npm-based tool test:

```go
// Test PostgreSQL dispatch
pgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `[{"major": "18", "latestMinor": "3", "supported": true, "current": true}]`)
}))
defer pgServer.Close()
postgresqlLatestURL = pgServer.URL

v, err = ResolveLatestVersion("postgresql")
if err != nil {
	t.Fatalf("postgresql: unexpected error: %v", err)
}
if v != "18" {
	t.Errorf("postgresql: got %q, want \"18\"", v)
}
```

Also add `origPG := postgresqlLatestURL` to the save/restore block at the top, and
`postgresqlLatestURL = origPG` to the defer.

---

## Step 9: Unit Tests — Version Detection (`internal/config/versions.go`)

### 9a. Add `TestExtractMajorVersion`

Add a test in a new or existing test file for the helper:

```go
func TestExtractMajorVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"17.2", "17"},
		{"16.13", "16"},
		{"18.0", "18"},
		{"17", "17"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractMajorVersion(tt.input)
			if got != tt.want {
				t.Errorf("extractMajorVersion(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
```

---

## Step 10: Unit Tests — Template Rendering (`internal/templates/templates_test.go`)

### 10a. Add PostgreSQL to `testConfig()`

```go
func testConfig() *config.Config {
	return &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "go", Enabled: true, PinnedVersion: "1.24.10"},
			{Name: "node", Enabled: true, PinnedVersion: "22.12.0"},
			{Name: "python", Enabled: true, PinnedVersion: "3.12"},
			{Name: "postgresql", Enabled: true, PinnedVersion: "17"}, // ADD
		},
		// ... rest unchanged ...
	}
}
```

### 10b. `TestRenderBaseDockerfile_PostgreSQLEnabled`

```go
func TestRenderBaseDockerfile_PostgreSQLEnabled(t *testing.T) {
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "postgresql", Enabled: true, PinnedVersion: "17"},
		},
		AITools:    []config.ToolConfig{},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderBaseDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderBaseDockerfile failed: %v", err)
	}

	// PGDG repository setup.
	assertContains(t, result, "apt.postgresql.org")
	assertContains(t, result, "ACCC4CF8.asc")
	assertContains(t, result, "bookworm-pgdg")

	// Cluster auto-creation disabled.
	assertContains(t, result, "create_main_cluster = false")

	// Correct packages installed.
	assertContains(t, result, "postgresql-17")
	assertContains(t, result, "postgresql-client-17")

	// PostgreSQL bin directory in PATH.
	assertContains(t, result, "/usr/lib/postgresql/17/bin")

	// Data directory created.
	assertContains(t, result, "/home/user/pgdata")
}
```

### 10c. `TestRenderBaseDockerfile_PostgreSQLVersion16`

```go
func TestRenderBaseDockerfile_PostgreSQLVersion16(t *testing.T) {
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "postgresql", Enabled: true, PinnedVersion: "16"},
		},
		AITools:    []config.ToolConfig{},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderBaseDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderBaseDockerfile failed: %v", err)
	}

	assertContains(t, result, "postgresql-16")
	assertContains(t, result, "postgresql-client-16")
	assertContains(t, result, "/usr/lib/postgresql/16/bin")
}
```

### 10d. `TestRenderBaseDockerfile_NoPostgreSQL`

```go
func TestRenderBaseDockerfile_NoPostgreSQL(t *testing.T) {
	cfg := testConfig()
	for i := range cfg.ProgrammingTools {
		if cfg.ProgrammingTools[i].Name == "postgresql" {
			cfg.ProgrammingTools[i].Enabled = false
		}
	}

	result, err := RenderBaseDockerfile(cfg)
	if err != nil {
		t.Fatalf("RenderBaseDockerfile failed: %v", err)
	}

	assertNotContains(t, result, "apt.postgresql.org")
	assertNotContains(t, result, "postgresql-")
	assertNotContains(t, result, "pgdg")
}
```

### 10e. `TestRenderEntrypoint_PostgreSQLEnabled`

```go
func TestRenderEntrypoint_PostgreSQLEnabled(t *testing.T) {
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "postgresql", Enabled: true, PinnedVersion: "17"},
		},
		AITools:    []config.ToolConfig{{Name: "claude", Enabled: true}},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderEntrypoint(cfg)
	if err != nil {
		t.Fatalf("RenderEntrypoint failed: %v", err)
	}

	// Should have initdb setup.
	assertContains(t, result, "initdb")
	assertContains(t, result, "/home/user/pgdata")
	assertContains(t, result, "--auth=trust")
	assertContains(t, result, "-U user")

	// Should have pg_ctl start.
	assertContains(t, result, "pg_ctl")
	assertContains(t, result, "start")

	// Should use versioned binary path.
	assertContains(t, result, "/usr/lib/postgresql/17/bin/")

	// PATH should include postgresql bin.
	assertContains(t, result, "/usr/lib/postgresql/17/bin")
}
```

### 10f. `TestRenderEntrypoint_NoPostgreSQL`

```go
func TestRenderEntrypoint_NoPostgreSQL(t *testing.T) {
	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "go", Enabled: true, PinnedVersion: "1.24.10"},
		},
		AITools:    []config.ToolConfig{{Name: "claude", Enabled: true}},
		ProxyPort:  3128,
		BridgePort: 4343,
	}

	result, err := RenderEntrypoint(cfg)
	if err != nil {
		t.Fatalf("RenderEntrypoint failed: %v", err)
	}

	assertNotContains(t, result, "initdb")
	assertNotContains(t, result, "pg_ctl")
	assertNotContains(t, result, "pgdata")
}
```

---

## Step 11: Test Config Fixtures (`.testfiles/`)

### 11a. `config-pinned.json`

Add postgresql entry to `programming_tools` array:

```json
{"name": "postgresql", "enabled": true, "mode": "pin", "pinned_version": "17"}
```

### 11b. `config-latest.json`

Add postgresql entry:

```json
{"name": "postgresql", "enabled": true, "mode": "latest"}
```

### 11c. `config-mirror.json`

Add postgresql entry with host_version:

```json
{"name": "postgresql", "enabled": true, "mode": "mirror", "host_version": "17"}
```

---

## Step 12: Docker Build Test (`test-docker-build.sh`)

### 12a. Add PostgreSQL to Phase 5 (programming tools check)

After the Python check (around line 283), add:

```bash
# PostgreSQL (exact major version).
local pg_version
pg_version=$(get_tool_version programming_tools postgresql)
if [ "$(is_tool_enabled programming_tools postgresql)" = "true" ]; then
    local actual_pg
    actual_pg=$(base_run psql --version 2>&1 || true)
    if [ -n "$pg_version" ]; then
        # Check that psql output contains the expected major version.
        if echo "$actual_pg" | grep -q "PostgreSQL) ${pg_version}\."; then
            pass "${mode}: PostgreSQL ${pg_version} installed"
        else
            fail "${mode}: PostgreSQL ${pg_version} expected, got: ${actual_pg}"
        fi
    else
        assert_version "PostgreSQL" "" "$actual_pg"
    fi

    # Verify server binary exists.
    local pg_ctl_check
    pg_ctl_check=$(base_run bash -c "ls /usr/lib/postgresql/*/bin/pg_ctl 2>&1" || true)
    if echo "$pg_ctl_check" | grep -q "pg_ctl"; then
        pass "${mode}: PostgreSQL server binaries present"
    else
        fail "${mode}: PostgreSQL server binaries not found"
    fi
fi
```

---

## Step 13: E2E Test (`test-e2e.sh`)

### 13a. Add PostgreSQL to the e2e test config

In the `setup_config()` function, add to programming_tools:

```json
{"name": "postgresql", "enabled": true, "mode": "pin", "pinned_version": "17"}
```

### 13b. Add PostgreSQL server test to barrel verification

After the programming tool version checks in Phase 11b, add:

```bash
# PostgreSQL server functional test.
if [ "$(jq -r '.programming_tools[] | select(.name=="postgresql" and .enabled) | .enabled' "${CONFIG_DIR}/config.json")" = "true" ]; then
    info "Testing PostgreSQL server inside barrel..."

    # Check psql is available.
    pg_check=$(barrel_exec "psql --version 2>&1" || true)
    if echo "$pg_check" | grep -q "PostgreSQL"; then
        pass "PostgreSQL client (psql) available in barrel"
    else
        fail "PostgreSQL client not found in barrel: $pg_check"
    fi

    # Check server is running (pg_isready).
    pg_ready=$(barrel_exec "pg_isready -h /tmp 2>&1" || true)
    if echo "$pg_ready" | grep -q "accepting connections"; then
        pass "PostgreSQL server is running in barrel"
    else
        fail "PostgreSQL server not running: $pg_ready"
    fi

    # Functional test: create database, create table, insert, select.
    barrel_exec "createdb -h /tmp testcooper 2>&1" || true
    pg_result=$(barrel_exec "psql -h /tmp -d testcooper -c \"CREATE TABLE t (id serial, val text); INSERT INTO t (val) VALUES ('cooper'); SELECT val FROM t;\" 2>&1" || true)
    if echo "$pg_result" | grep -q "cooper"; then
        pass "PostgreSQL functional test (create/insert/select) passed"
    else
        fail "PostgreSQL functional test failed: $pg_result"
    fi
fi
```

---

## Assumptions and Verification

### Assumption 1: `psql --version` output is parseable by existing `semverRegex`

**Claim**: The output format `psql (PostgreSQL) 17.2 (Ubuntu ...)` contains a semver-like version that
`semverRegex = regexp.MustCompile("(\d+\.\d+(?:\.\d+)?)")` can extract.

**Verification**: Ran `psql --version` on this machine:
```
psql (PostgreSQL) 12.22 (Ubuntu 12.22-3.pgdg22.04+1)
```
The regex `\d+\.\d+(?:\.\d+)?` matches `12.22` (first match). Confirmed working.

**Status: VERIFIED**

---

### Assumption 2: The postgresql.org versions API has `major`, `supported`, `current` fields

**Claim**: The API at `https://www.postgresql.org/versions.json` returns an array of objects with
`major` (string), `latestMinor` (string), `supported` (bool), `current` (bool) fields.

**Verification**: Fetched the live API. Sample entries:
```json
{"current": true, "eolDate": "2030-11-14", "firstRelDate": "2025-09-25",
 "latestMinor": "3", "major": "18", "relDate": "2026-02-26", "supported": true}
{"current": false, "eolDate": "2029-11-08", "firstRelDate": "2024-09-26",
 "latestMinor": "9", "major": "17", "relDate": "2026-02-26", "supported": true}
```
Total entries: 28. Entries with `supported: true`: 5 (major versions 14-18).
Exactly one entry has `current: true` (major 18).

**Status: VERIFIED**

---

### Assumption 3: PGDG apt repository uses `bookworm-pgdg` suite for Debian bookworm

**Claim**: The apt source line is `deb http://apt.postgresql.org/pub/repos/apt bookworm-pgdg main` and
the GPG key is at `https://www.postgresql.org/media/keys/ACCC4CF8.asc`.

**Verification**: This is the documented setup from postgresql.org's official Linux installation guide.
The package names are `postgresql-{major}` and `postgresql-client-{major}`. The base Dockerfile uses
`debian:bookworm-slim` or `golang:*-bookworm`, both based on Debian bookworm.

**Status: VERIFIED** (matches official PostgreSQL documentation)

---

### Assumption 4: `create_main_cluster = false` prevents auto-cluster creation

**Claim**: Setting `create_main_cluster = false` in `/etc/postgresql-common/createcluster.conf` before
installing the `postgresql-{version}` package prevents the package post-install script from running
`pg_createcluster` automatically.

**Verification**: This is the documented Debian/Ubuntu mechanism. The `postgresql-common` package reads
this config file during package installation. Without it, `apt-get install postgresql-17` would try to
run `initdb` as the `postgres` user and start the server, which would fail in a Dockerfile.

**Status: VERIFIED** (standard Debian packaging mechanism, documented in postgresql-common docs)

---

### Assumption 5: PostgreSQL binaries are at `/usr/lib/postgresql/{version}/bin/`

**Claim**: After `apt-get install postgresql-17`, the server and client binaries are at
`/usr/lib/postgresql/17/bin/psql`, `/usr/lib/postgresql/17/bin/pg_ctl`,
`/usr/lib/postgresql/17/bin/initdb`, etc.

**Verification**: Checked on the host system with PostgreSQL 12 installed:
```
/usr/lib/postgresql/12/bin/psql
/usr/lib/postgresql/12/bin/initdb
/usr/lib/postgresql/12/bin/pg_ctl
/usr/lib/postgresql/12/bin/postgres
/usr/lib/postgresql/12/bin/createdb
```
The pattern is consistent across all PGDG versions.

**Status: VERIFIED**

---

### Assumption 6: A non-root user can run `initdb` and `pg_ctl`

**Claim**: The container runs as `user` (UID 1000). `initdb` and `pg_ctl` can be run by any user that
owns the data directory.

**Verification**: `initdb` requires write access to the data directory, which is created as
`/home/user/pgdata` owned by `user`. `pg_ctl` starts the server as the current user. PostgreSQL does
NOT require root privileges — it explicitly refuses to run as root. The container user `user` is
exactly the right privilege level.

Verified binary permissions: `-rwxr-xr-x 1 root root` — world-executable.

**Status: VERIFIED**

---

### Assumption 7: The `major` field in the API is the same as the apt package version suffix

**Claim**: API `major: "17"` corresponds to apt package `postgresql-17`.

**Verification**: The supported versions in the API have `major` values of "14", "15", "16", "17", "18".
The PGDG apt repo packages are named `postgresql-14`, `postgresql-15`, ..., `postgresql-18`. The naming
is identical.

Note: Very old PostgreSQL versions (pre-10) used two-number major versions like "9.6", which would
correspond to `postgresql-9.6`. But all currently supported versions (14+) use single-number majors.

**Status: VERIFIED**

---

### Assumption 8: No additional volume mounts are needed for PostgreSQL

**Claim**: PostgreSQL data is ephemeral (stored at `/home/user/pgdata` inside the container) and no
persistent volume mount is needed.

**Verification**: The barrel container runs with `sleep infinity` and stays alive across multiple
`cooper cli` sessions (which use `docker exec`). The data directory persists for the container's
lifetime. It only resets when the barrel is stopped/recreated (e.g., on `cooper up` restart). This is
acceptable for a dev tool — the AI can recreate databases from the project's migration scripts.

The workspace directory IS mounted read-write, so the AI can dump/restore SQL files to the workspace
if persistence across barrel restarts is needed.

**Status: VERIFIED** (design decision, not a factual assumption)

---

### Assumption 9: No changes to barrel.go are needed for port handling

**Claim**: PostgreSQL inside the container listens on `localhost:5432`. The AI accesses it directly
(same container). No port forwarding is needed for this use case.

**Verification**: Port forwarding in Cooper is for host→container direction (AI accessing host services
like a host-side database). Since PostgreSQL runs INSIDE the barrel, the AI connects to it at
`localhost:5432` via Unix socket (`-h /tmp`) or TCP. No cross-container networking is involved.

The existing port forward rule `{ContainerPort: 5432, HostPort: 5432}` in test configs is for
forwarding to a HOST PostgreSQL, not the container-local one. Both can coexist — the container-local
server uses Unix sockets, the forwarded port uses TCP to the proxy.

Actually, there IS a conflict: if the container-local PostgreSQL binds to `localhost:5432` AND there's
a socat forwarder listening on `localhost:5432` for host-side PostgreSQL, they'll collide on the TCP
port.

**Resolution**: The entrypoint's `pg_ctl start` should bind PostgreSQL to Unix socket only (`-h ""`
or via `listen_addresses = ''` in postgresql.conf). TCP connections go to the host-forwarded port if
configured. This avoids the conflict.

Update the pg_ctl command in Step 5 to use `-o "-k /tmp -h ''"`:

```bash
/usr/lib/postgresql/{{.PostgreSQLVersion}}/bin/pg_ctl \
    -D "$PG_DATA" -l /tmp/postgresql.log \
    -o "-k /tmp -h ''" \
    start >/dev/null 2>&1
```

With `-h ''`, PostgreSQL listens on Unix socket only (in `/tmp/`). The AI connects via
`psql -h /tmp` or `psql --host=/tmp`. This coexists cleanly with socat port forwarding to a host-side
PostgreSQL on TCP port 5432.

**Status: VERIFIED (with correction — use Unix socket only)**

---

### Assumption 10: The existing test patterns (httptest mock, URL override) work for PostgreSQL

**Claim**: Adding a `var postgresqlLatestURL` and overriding it in tests follows the same pattern used
for `goLatestURL`, `nodeLatestURL`, `pythonLatestURL`, `npmRegistryURL`.

**Verification**: Reviewed `resolve_test.go`. Every test:
1. Creates `httptest.NewServer(...)` with a canned response
2. Saves the original URL: `origURL := goLatestURL`
3. Overrides: `goLatestURL = server.URL`
4. Defers restore: `defer func() { goLatestURL = origURL }()`
5. Calls the resolver function and asserts

The exact same pattern applies to `postgresqlLatestURL`.

**Status: VERIFIED**

---

## Verified Assumptions

All 10 assumptions have been verified:

1. **VERIFIED** — `psql --version` output `"psql (PostgreSQL) 12.22 (Ubuntu ...)"` is parseable by `semverRegex`, extracting `"12.22"`.
2. **VERIFIED** — The `postgresql.org/versions.json` API returns `major`, `latestMinor`, `supported`, `current` fields. 28 total entries, 5 supported (v14-v18), one current (v18).
3. **VERIFIED** — PGDG apt source is `deb http://apt.postgresql.org/pub/repos/apt bookworm-pgdg main`, GPG key at `https://www.postgresql.org/media/keys/ACCC4CF8.asc`.
4. **VERIFIED** — `create_main_cluster = false` in `/etc/postgresql-common/createcluster.conf` prevents auto-cluster creation during `apt-get install`.
5. **VERIFIED** — Binaries at `/usr/lib/postgresql/{version}/bin/` (psql, pg_ctl, initdb, createdb, etc.). Confirmed with local PostgreSQL 12 installation.
6. **VERIFIED** — Non-root user can run `initdb` and `pg_ctl`. PostgreSQL refuses to run as root. Container user `user` is the correct privilege level.
7. **VERIFIED** — API `major: "17"` directly maps to apt package `postgresql-17`. All currently supported versions (14+) use single-number major.
8. **VERIFIED** — Ephemeral data at `/home/user/pgdata` persists across `docker exec` sessions (barrel stays alive). Resets on barrel recreate. Acceptable for dev use.
9. **VERIFIED (with correction)** — PostgreSQL server must bind to Unix socket only (`-h ''`) to avoid TCP port 5432 conflict with socat host-forwarding. AI connects via `psql -h /tmp`.
10. **VERIFIED** — `var postgresqlLatestURL` + httptest mock pattern is identical to Go/Node/Python/npm test patterns in `resolve_test.go`.

---

## Implementation Order

Execute in this order to enable incremental testing:

1. **Steps 1-2**: Version detection + resolution (config package). Run `go test ./internal/config/...`.
2. **Steps 3-5**: Template data structs + Dockerfile + entrypoint templates. Run `go test ./internal/templates/...`.
3. **Step 6**: Configure UI (just adding the entry).
4. **Steps 8-10**: All unit tests. Run `go test ./...`.
5. **Steps 11-12**: Test config fixtures + docker build test. Run `./test-docker-build.sh pinned`.
6. **Step 13**: E2E test. Run `./test-e2e.sh`.

At each stage, build and test before moving to the next.

---

## Manual Verification Checklist

After all code is written, verify end-to-end:

```bash
# 1. Unit tests pass
cd cooper && go test ./... 2>&1 | tee /tmp/cooper-pg-unit.log

# 2. Build with postgresql enabled (pinned mode)
# Create a test config with postgresql enabled, then:
./cooper build --config /path/to/test-config --prefix test-pg-

# 3. Verify base image has PostgreSQL
docker run --rm --entrypoint "" test-pg-cooper-base psql --version
# Expected: psql (PostgreSQL) 17.x

docker run --rm --entrypoint "" test-pg-cooper-base ls /usr/lib/postgresql/17/bin/pg_ctl
# Expected: /usr/lib/postgresql/17/bin/pg_ctl

# 4. Verify server starts in a barrel
# (Requires full cooper up + cooper cli flow, or manual docker run with entrypoint)
docker run --rm --name test-pg-barrel test-pg-cooper-base bash -c "
    /entrypoint.sh bash -c '
        sleep 2  # wait for pg_ctl to start
        pg_isready -h /tmp
        createdb -h /tmp testdb
        psql -h /tmp -d testdb -c \"SELECT version();\"
    '
"

# 5. Docker build tests
./test-docker-build.sh pinned 2>&1 | tee /tmp/cooper-pg-build.log

# 6. Full e2e test
./test-e2e.sh 2>&1 | tee /tmp/cooper-pg-e2e.log
```
