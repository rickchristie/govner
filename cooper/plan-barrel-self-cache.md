# Plan: Barrel Self-Managed Cache

## Decision

Replace direct host language-cache mounts with Cooper-managed cache directories under
`~/.cooper/cache/`.

Important: **do not seed or import host caches during `cooper build`, `cooper update`,
or barrel startup**.

The new model is:

- `cooper build` stays self-contained and image-focused
- `cooper update` stays self-contained and rebuild-focused
- barrel startup creates and mounts Cooper-owned cache directories
- the caches begin empty and fill naturally during normal package-manager usage

This is a deliberate simplification. It avoids host-tool detection, avoids copy and
permission-repair logic, avoids macOS-specific cache import behavior, and keeps the
implementation easy to reason about.

## Why This Direction

The earlier seeding idea caused several architectural problems:

- it pushed runtime cache concerns into `cooper build`
- it would require host-side `go`, `npm`, `pip`, `cp`, and `chmod`
- it would create `build` vs `update` divergence unless duplicated
- it would require partial-copy recovery or atomic staging
- it would make future macOS support harder, not easier
- it would make tests shell-heavy and brittle

Runtime-only Cooper caches avoid all of that.

## Goals

- Use Cooper-owned cache directories instead of host cache directories
- Keep `cooper build` and `cooper update` free of cache import logic
- Remove `GOFLAGS=-mod=readonly`
- Change fresh-config default monitor timeout from 5 to 30 seconds
- Keep existing saved configs backward compatible
- Make the implementation easy to unit test without Docker or shelling out

## Non-Goals

- No cache seeding
- No cache import
- No cache cleanup or GC
- No cache migration from host paths
- No config migration that rewrites existing `monitor_timeout_secs`

If cache import is ever added later, it should be an explicit separate command such as
`cooper cache import`, not hidden inside `build`, `update`, or startup.

## Verified Assumptions

These are the assumptions that still matter for the runtime-only design.

### 1. Barrel startup already owns mount-directory preparation

Verified in `cooper/internal/docker/barrel.go`:

- `StartBarrel(...)` calls `ensureBarrelHostDirs(...)` before `docker run`
- that function already exists specifically to create bind-mount directories

This means the new Cooper cache directories fit naturally into existing startup
responsibility. No build-time lifecycle hook is required.

### 2. Docker bind mounts should still have their host directories pre-created

Verified in current Cooper behavior:

- Playwright support directories are already pre-created before mount
- the code comment explicitly notes this avoids Docker creating root-owned directories

The new language cache directories should follow the same pattern.

### 3. Container-side cache paths must stay under `/home/user`

Verified in `cooper/internal/docker/barrel.go`:

- `containerHome` is `/home/user`
- auth mounts, cache mounts, and Playwright mounts are all built around that constant

The new cache helper should continue using `containerHome` and should not introduce
host-home-dependent container paths.

### 4. `main.go` does not need cache changes for this design

Verified from current lifecycle split:

- `main.go` handles config loading, build/update orchestration, and CLI entrypoints
- barrel runtime behavior is implemented under `cooper/internal/docker/barrel.go`

Because this design is runtime-only and has no seeding/import step, `main.go` should
stay unchanged.

### 5. Changing `DefaultConfig()` to 30 seconds does not rewrite existing saved configs

Verified in `cooper/internal/config/config.go`:

- `DefaultConfig()` is used for fresh config creation
- `LoadConfig()` unmarshals existing persisted values
- `applyMissingDefaults()` only fills truly missing or zero-value fields

Therefore:

- fresh configs will pick up `30`
- existing configs that already have `monitor_timeout_secs: 5` will keep `5`

This is the intended compatibility behavior.

### 6. The old-config timeout tests must remain old-config tests

Verified in current test layout:

- some tests use `DefaultConfig()` and therefore should move to 30
- some tests model existing persisted config values and may still legitimately use 5

Do not blindly replace every `5` in tests with `30`.

## Current Code Surfaces

These are the relevant current code points the implementation must update:

### `cooper/internal/docker/barrel.go`

Current responsibilities:

- `StartBarrel(...)` calls `ensureBarrelHostDirs(...)`
- `appendVolumeMounts(...)` calls `appendLanguageCacheMounts(args, homeDir, cfg)`
- `appendLanguageCacheMounts(...)` mounts host cache paths
- `ensureBarrelHostDirs(...)` pre-creates host cache directories
- `StartBarrel(...)` injects `GOFLAGS=-mod=readonly` when Go is enabled

Current language cache behavior:

```text
$GOPATH/pkg/mod      -> /home/user/go/pkg/mod          (ro)
~/.cache/go-build    -> /home/user/.cache/go-build     (rw)
~/.npm               -> /home/user/.npm                (ro)
~/.cache/pip         -> /home/user/.cache/pip          (ro)
```

### `cooper/internal/config/config.go`

Current default:

```go
MonitorTimeoutSecs: 5,
```

### `cooper/internal/templates/doctor.sh`

Current diagnostic assumes `GOFLAGS=-mod=readonly` is expected.

### `cooper/test-e2e.sh`

Current e2e startup logic:

- creates host cache directories in `${HOME_DIR}`
- mounts host cache directories
- injects `GOFLAGS=-mod=readonly`
- asserts readonly cache behavior for Go/NPM/Pip

## Target Behavior

### New language cache mounts

The only language caches mounted into the barrel should be:

```text
~/.cooper/cache/go-mod     -> /home/user/go/pkg/mod          (rw)
~/.cooper/cache/go-build   -> /home/user/.cache/go-build     (rw)
~/.cooper/cache/npm        -> /home/user/.npm                (rw)
~/.cooper/cache/pip        -> /home/user/.cache/pip          (rw)
```

These directories:

- live entirely under `cooperDir`
- persist across barrel runs
- start empty if they do not exist
- are created before Docker bind-mounts them

### Build/update lifecycle

No cache logic belongs in:

- `runBuild`
- `runUpdate`
- template generation
- image builds

### Go behavior

After the change:

- there is no `GOFLAGS=-mod=readonly`
- `go get`, `go mod tidy`, and module downloads work normally inside the barrel
- Go module cache writes go to `~/.cooper/cache/go-mod`
- Go build cache writes go to `~/.cooper/cache/go-build`

### Timeout behavior

After the change:

- new configs default to `MonitorTimeoutSecs: 30`
- old configs that already contain `5` keep `5`

There is no migration step that rewrites saved configs.

## Architecture

### Source Of Truth

Do not hardcode cache mount paths in multiple places.

Introduce one small pure helper that computes enabled language cache specs from
`cooperDir` and `cfg`.

That helper must be used by:

- `appendLanguageCacheMounts(...)`
- directory-precreation logic
- unit tests

### Thin IO, Fat Pure Helpers

Prefer this shape:

- pure functions decide paths and lists
- thin wrappers call `os.UserHomeDir()` and `os.MkdirAll(...)`

This gives better testability than embedding path logic directly inside filesystem code.

## File-By-File Implementation

### 1. Add `cooper/internal/docker/cachepaths.go`

Create a new file for pure path-selection logic.

Recommended contents:

```go
package docker

import (
    "path/filepath"

    "github.com/rickchristie/govner/cooper/internal/config"
)

type cacheMountSpec struct {
    Name          string
    HostPath      string
    ContainerPath string
}

func languageCacheSpecs(cooperDir string, cfg *config.Config) []cacheMountSpec {
    var specs []cacheMountSpec

    for _, tool := range cfg.ProgrammingTools {
        if !tool.Enabled {
            continue
        }
        switch tool.Name {
        case "go":
            specs = append(specs,
                cacheMountSpec{
                    Name:          "go-mod",
                    HostPath:      filepath.Join(cooperDir, "cache", "go-mod"),
                    ContainerPath: filepath.Join(containerHome, "go", "pkg", "mod"),
                },
                cacheMountSpec{
                    Name:          "go-build",
                    HostPath:      filepath.Join(cooperDir, "cache", "go-build"),
                    ContainerPath: filepath.Join(containerHome, ".cache", "go-build"),
                },
            )
        case "node":
            specs = append(specs, cacheMountSpec{
                Name:          "npm",
                HostPath:      filepath.Join(cooperDir, "cache", "npm"),
                ContainerPath: filepath.Join(containerHome, ".npm"),
            })
        case "python":
            specs = append(specs, cacheMountSpec{
                Name:          "pip",
                HostPath:      filepath.Join(cooperDir, "cache", "pip"),
                ContainerPath: filepath.Join(containerHome, ".cache", "pip"),
            })
        }
    }

    return specs
}
```

Recommended additional pure helper in the same file:

```go
func barrelMountDirs(homeDir, toolName, cooperDir string, cfg *config.Config) []string {
    var dirs []string

    switch toolName {
    case "claude":
        dirs = append(dirs, filepath.Join(homeDir, ".claude"))
    case "copilot":
        dirs = append(dirs, filepath.Join(homeDir, ".copilot"))
    case "codex":
        dirs = append(dirs, filepath.Join(homeDir, ".codex"))
    case "opencode":
        dirs = append(dirs,
            filepath.Join(homeDir, ".config", "opencode"),
            filepath.Join(homeDir, ".local", "share", "opencode"),
        )
    }

    for _, spec := range languageCacheSpecs(cooperDir, cfg) {
        dirs = append(dirs, spec.HostPath)
    }

    dirs = append(dirs,
        filepath.Join(cooperDir, "fonts"),
        filepath.Join(cooperDir, "cache", "ms-playwright"),
    )

    return dirs
}
```

Why this helper is recommended:

- `ensureBarrelMountDirs(...)` becomes a thin wrapper
- the dir list becomes directly unit-testable
- auth dirs, cache dirs, and Playwright dirs are computed in one place

### 2. Modify `cooper/internal/docker/barrel.go`

#### 2a. Update `StartBarrel(...)`

Current:

```go
if err := ensureBarrelHostDirs(absWorkspace, toolName, cooperDir); err != nil {
    return fmt.Errorf("create host directories: %w", err)
}
```

Change to:

```go
if err := ensureBarrelMountDirs(toolName, cooperDir, cfg); err != nil {
    return fmt.Errorf("create mount directories: %w", err)
}
```

Notes:

- `absWorkspace` is not needed by the mount-dir helper
- rename the error message too, not just the function

#### 2b. Remove the `GOFLAGS` block

Delete this block entirely:

```go
if isGoEnabled(cfg) {
    args = append(args, "-e", "GOFLAGS=-mod=readonly")
}
```

Do not replace it with any new Go-specific env var.

#### 2c. Update `appendVolumeMounts(...)`

Current call:

```go
args = appendLanguageCacheMounts(args, homeDir, cfg)
```

Change to:

```go
args = appendLanguageCacheMounts(args, cooperDir, cfg)
```

Do not remove `homeDir` from `appendVolumeMounts(...)`, because it is still needed for
auth mounts and `.gitconfig`.

#### 2d. Rewrite `appendLanguageCacheMounts(...)`

Current function uses `homeDir`, `GOPATH`, and host cache directories.

Replace with:

```go
func appendLanguageCacheMounts(args []string, cooperDir string, cfg *config.Config) []string {
    for _, spec := range languageCacheSpecs(cooperDir, cfg) {
        args = append(args, "-v", fmt.Sprintf("%s:%s:rw", spec.HostPath, spec.ContainerPath))
    }
    return args
}
```

Important:

- all language cache mounts become `:rw`
- there is no special readonly case anymore
- there is no `GOPATH` lookup anymore
- there is no `homeDir` lookup inside this function anymore

#### 2e. Rename and simplify `ensureBarrelHostDirs(...)`

Rename to:

```go
func ensureBarrelMountDirs(toolName, cooperDir string, cfg *config.Config) error
```

Implementation pattern:

```go
func ensureBarrelMountDirs(toolName, cooperDir string, cfg *config.Config) error {
    homeDir, err := os.UserHomeDir()
    if err != nil {
        return fmt.Errorf("get home dir: %w", err)
    }

    for _, dir := range barrelMountDirs(homeDir, toolName, cooperDir, cfg) {
        if err := os.MkdirAll(dir, 0755); err != nil {
            return fmt.Errorf("mkdir %s: %w", dir, err)
        }
    }
    return nil
}
```

Important removals:

- remove all `GOPATH` logic
- remove all creation of `${HOME}/.npm`
- remove all creation of `${HOME}/.cache/pip`
- remove all creation of `${HOME}/.cache/go-build`
- remove all creation of `${GOPATH}/pkg/mod`

Important retentions:

- keep auth dir creation
- keep `fonts`
- keep `cache/ms-playwright`

### 3. Do not modify `cooper/main.go`

There should be **no cache-related changes** in `main.go`.

Specifically, do not add:

- cache seeding in `runBuild`
- cache seeding in `runUpdate`
- host tool detection
- copy logic
- chmod logic

If the implementation session finds itself adding cache logic to build/update, that is
a sign it has drifted from the plan.

### 4. Modify `cooper/internal/config/config.go`

Change the default:

```go
MonitorTimeoutSecs: 5,
```

to:

```go
MonitorTimeoutSecs: 30,
```

Also update the nearby comment so it no longer says "monitor timeout 5s".

Recommended updated comment:

```go
// DefaultConfig returns a configuration with sensible defaults.
// Proxy port 3128 (Squid standard), bridge port 4343, monitor timeout 30s,
// history limits 500 entries.
```

### 5. Modify `cooper/internal/templates/doctor.sh`

Replace the GOFLAGS section with this exact behavior:

```bash
# Check GOFLAGS
if [ -n "${GOFLAGS:-}" ]; then
    if echo "$GOFLAGS" | grep -q "mod=readonly"; then
        warn "GOFLAGS includes -mod=readonly (unexpected for Cooper-managed caches)"
    else
        info "GOFLAGS set without mod=readonly: ${GOFLAGS}"
    fi
else
    pass "GOFLAGS not set (Go modules are writable)"
fi
```

Rationale:

- absence of GOFLAGS is now the expected success case
- `mod=readonly` is now a warning, not a pass

### 6. Modify `cooper/test-e2e.sh`

This file needs a careful update because it has multiple startup fixtures and explicit
mount assertions.

#### 6a. Replace host cache directory setup

Current setup creates:

```bash
mkdir -p "${HOME_DIR}/.npm"
mkdir -p "${HOME_DIR}/.cache/pip"
mkdir -p "${HOME_DIR}/.cache/go-build"
```

Replace with Cooper cache directory setup:

```bash
mkdir -p "${CONFIG_DIR}/cache/go-mod" 2>/dev/null || true
mkdir -p "${CONFIG_DIR}/cache/go-build" 2>/dev/null || true
mkdir -p "${CONFIG_DIR}/cache/npm" 2>/dev/null || true
mkdir -p "${CONFIG_DIR}/cache/pip" 2>/dev/null || true
```

Do not add host cache setup back anywhere else in the file.

#### 6b. Replace startup mount arguments

Wherever the test manually builds `docker run` args for a barrel, replace:

```bash
"-v" "${GOPATH}/pkg/mod:/home/user/go/pkg/mod:ro"
"-v" "${HOME_DIR}/.cache/go-build:/home/user/.cache/go-build:rw"
"-v" "${HOME_DIR}/.npm:/home/user/.npm:ro"
"-v" "${HOME_DIR}/.cache/pip:/home/user/.cache/pip:ro"
```

with:

```bash
"-v" "${CONFIG_DIR}/cache/go-mod:/home/user/go/pkg/mod:rw"
"-v" "${CONFIG_DIR}/cache/go-build:/home/user/.cache/go-build:rw"
"-v" "${CONFIG_DIR}/cache/npm:/home/user/.npm:rw"
"-v" "${CONFIG_DIR}/cache/pip:/home/user/.cache/pip:rw"
```

There are multiple occurrences in the file. Use grep and update all of them:

```bash
rg -n 'pkg/mod|\\.cache/go-build|\\.npm|\\.cache/pip|GOFLAGS=-mod=readonly' cooper/test-e2e.sh
```

Do not update only the first block and forget the later manual barrel runs.

#### 6c. Remove GOFLAGS injection

Delete all occurrences of:

```bash
-e "GOFLAGS=-mod=readonly"
```

#### 6d. Invert GOFLAGS assertion

Replace logic like:

```bash
goflags=$(barrel_exec 'echo $GOFLAGS')
if echo "$goflags" | grep -q "mod=readonly"; then
    pass "GOFLAGS includes -mod=readonly"
else
    fail "GOFLAGS not set correctly (got: ${goflags})"
fi
```

with:

```bash
goflags=$(barrel_exec 'echo $GOFLAGS')
if echo "$goflags" | grep -q "mod=readonly"; then
    fail "GOFLAGS unexpectedly includes -mod=readonly"
else
    pass "GOFLAGS does not include -mod=readonly"
fi
```

#### 6e. Update mount assertions

Any assertion that expects host paths must be changed to Cooper cache paths.

Expected mount strings:

```text
${CONFIG_DIR}/cache/go-mod:/home/user/go/pkg/mod:rw
${CONFIG_DIR}/cache/go-build:/home/user/.cache/go-build:rw
${CONFIG_DIR}/cache/npm:/home/user/.npm:rw
${CONFIG_DIR}/cache/pip:/home/user/.cache/pip:rw
```

### 7. Modify `cooper/REQUIREMENTS.md`

Update documentation so it describes the new runtime-only cache model.

Replace the old host-preload narrative with these facts:

- Cooper mounts only `~/.cooper/cache/...` into barrels
- these caches are read-write
- they are not seeded during build/update
- dependency installation occurs inside the barrel through the proxy
- fresh configs use 30-second monitor timeout

Also remove language implying:

- host caches are mounted directly
- Go modules are always readonly
- dependency caches must be preloaded on the host

## Test Plan

### A. Add `cooper/internal/docker/cachepaths_test.go`

Create a new unit test file for pure cache-path logic.

Recommended test cases:

```go
func TestLanguageCacheSpecs_EmptyWhenNoToolsEnabled(t *testing.T) {
    cfg := &config.Config{
        ProgrammingTools: []config.ToolConfig{
            {Name: "go", Enabled: false},
            {Name: "node", Enabled: false},
            {Name: "python", Enabled: false},
        },
    }

    got := languageCacheSpecs("/tmp/cooper", cfg)
    if len(got) != 0 {
        t.Fatalf("len(specs) = %d, want 0", len(got))
    }
}

func TestLanguageCacheSpecs_GoNodePython(t *testing.T) {
    cooperDir := "/tmp/cooper"
    cfg := &config.Config{
        ProgrammingTools: []config.ToolConfig{
            {Name: "go", Enabled: true},
            {Name: "node", Enabled: true},
            {Name: "python", Enabled: true},
        },
    }

    got := languageCacheSpecs(cooperDir, cfg)

    want := []cacheMountSpec{
        {
            Name:          "go-mod",
            HostPath:      filepath.Join(cooperDir, "cache", "go-mod"),
            ContainerPath: "/home/user/go/pkg/mod",
        },
        {
            Name:          "go-build",
            HostPath:      filepath.Join(cooperDir, "cache", "go-build"),
            ContainerPath: "/home/user/.cache/go-build",
        },
        {
            Name:          "npm",
            HostPath:      filepath.Join(cooperDir, "cache", "npm"),
            ContainerPath: "/home/user/.npm",
        },
        {
            Name:          "pip",
            HostPath:      filepath.Join(cooperDir, "cache", "pip"),
            ContainerPath: "/home/user/.cache/pip",
        },
    }

    if !reflect.DeepEqual(got, want) {
        t.Fatalf("specs mismatch\ngot:  %#v\nwant: %#v", got, want)
    }
}
```

Also add a targeted test for `barrelMountDirs(...)` if you implement that helper:

```go
func TestBarrelMountDirs_ClaudeIncludesAuthCachesAndPlaywright(t *testing.T) {
    homeDir := "/tmp/home"
    cooperDir := "/tmp/cooper"
    cfg := &config.Config{
        ProgrammingTools: []config.ToolConfig{
            {Name: "go", Enabled: true},
            {Name: "node", Enabled: true},
        },
    }

    got := barrelMountDirs(homeDir, "claude", cooperDir, cfg)

    wantContains := []string{
        filepath.Join(homeDir, ".claude"),
        filepath.Join(cooperDir, "cache", "go-mod"),
        filepath.Join(cooperDir, "cache", "go-build"),
        filepath.Join(cooperDir, "cache", "npm"),
        filepath.Join(cooperDir, "fonts"),
        filepath.Join(cooperDir, "cache", "ms-playwright"),
    }

    for _, want := range wantContains {
        if !slices.Contains(got, want) {
            t.Fatalf("mount dir list missing %q; got=%v", want, got)
        }
    }
}
```

If `slices.Contains` is unavailable in the file already, use a small local helper.

### B. Add/Update tests for `ensureBarrelMountDirs(...)`

If you keep only the thin wrapper, it may be enough to test the pure helper and leave
the wrapper untested.

If you do test the wrapper directly:

- use `t.TempDir()` for `cooperDir`
- use `t.Setenv("HOME", homeDir)` only if you have verified `os.UserHomeDir()` behaves
  predictably in this environment
- keep the test local and filesystem-only
- do not require Docker

Because `os.UserHomeDir()` can be environment-sensitive, the preferred path is still:

- test `barrelMountDirs(...)` thoroughly
- keep `ensureBarrelMountDirs(...)` thin

### C. Update `cooper/internal/config/config_test.go`

Do **not** destroy backward-compat coverage by rewriting old-config fixtures from 5 to
30.

Make these changes instead:

#### C1. Add a fresh-default test

```go
func TestDefaultConfigMonitorTimeoutIs30Seconds(t *testing.T) {
    cfg := DefaultConfig()
    if cfg.MonitorTimeoutSecs != 30 {
        t.Fatalf("MonitorTimeoutSecs = %d, want 30", cfg.MonitorTimeoutSecs)
    }
}
```

#### C2. Keep old-config coverage

A test fixture representing an older saved config may still contain:

```go
MonitorTimeoutSecs: 5,
```

That is correct when the purpose of the test is to simulate an older persisted config.

Do not "fix" that fixture to 30 if the test is about backward compatibility.

### D. Update `cooper/internal/app/cooper_test.go`

Update only tests that rely on fresh `DefaultConfig()` values.

Concrete example:

Current precondition in `TestCooperApp_UpdateSettings`:

```go
if cfg.MonitorTimeoutSecs != 5 {
    t.Fatalf("precondition: MonitorTimeoutSecs = %d, want 5", cfg.MonitorTimeoutSecs)
}
```

Change to:

```go
if cfg.MonitorTimeoutSecs != 30 {
    t.Fatalf("precondition: MonitorTimeoutSecs = %d, want 30", cfg.MonitorTimeoutSecs)
}
```

Do **not** change tests that explicitly set `cfg.MonitorTimeoutSecs = 5` to speed up a
timeout scenario. Those are scenario-specific, not default-specific.

### E. Update e2e tests

After modifying `cooper/test-e2e.sh`, ensure the script now validates:

- Cooper cache mount sources under `${CONFIG_DIR}/cache/...`
- no `GOFLAGS=-mod=readonly`
- all language cache mounts are read-write

## Verification Commands

The implementation session should use commands like these while working:

```bash
rg -n 'appendLanguageCacheMounts|ensureBarrelHostDirs|GOFLAGS=-mod=readonly' cooper/internal/docker
rg -n 'MonitorTimeoutSecs: 5|monitor timeout 5s' cooper/internal/config cooper/internal/app
rg -n 'pkg/mod|\\.cache/go-build|\\.npm|\\.cache/pip|GOFLAGS=-mod=readonly' cooper/test-e2e.sh
```

After implementation, verify:

```bash
go test ./cooper/internal/docker/... ./cooper/internal/config/... ./cooper/internal/app/...  # as appropriate for package layout
```

And run targeted greps:

```bash
rg -n 'GOFLAGS=-mod=readonly' cooper
rg -n '\\$GOPATH/pkg/mod|\\.npm:ro|\\.cache/pip:ro' cooper/internal cooper/test-e2e.sh cooper/REQUIREMENTS.md
```

Expected outcomes:

- runtime code should no longer inject `GOFLAGS=-mod=readonly`
- runtime code should no longer mount host language caches
- docs and e2e tests should reflect Cooper-managed caches

## Acceptance Criteria

The change is complete when all of the following are true:

1. Barrels mount only Cooper-managed language caches under `cooperDir/cache/...`.
2. `appendLanguageCacheMounts(...)` no longer uses `homeDir` or `GOPATH`.
3. `ensureBarrelMountDirs(...)` no longer creates host language cache directories.
4. `StartBarrel(...)` no longer injects `GOFLAGS=-mod=readonly`.
5. `main.go` contains no cache seeding/import logic.
6. `DefaultConfig()` uses 30 seconds.
7. Backward-compat tests still preserve old saved configs with timeout 5.
8. `doctor.sh` treats missing `GOFLAGS` as success.
9. `test-e2e.sh` expects Cooper cache mounts and no readonly module flag.
10. The implementation remains easy to explain:
    all language cache path decisions come from one pure helper.

## Files Expected To Change

| File | Change |
|------|--------|
| `cooper/internal/docker/cachepaths.go` | new pure helper file |
| `cooper/internal/docker/cachepaths_test.go` | new unit tests |
| `cooper/internal/docker/barrel.go` | mount path refactor, helper rename, GOFLAGS removal |
| `cooper/internal/config/config.go` | fresh default timeout from 5 to 30 |
| `cooper/internal/config/config_test.go` | new default-timeout test, preserve old-config coverage |
| `cooper/internal/app/cooper_test.go` | update fresh-default expectation(s) |
| `cooper/internal/templates/doctor.sh` | invert GOFLAGS diagnostic |
| `cooper/test-e2e.sh` | update mount setup, mount assertions, GOFLAGS expectations |
| `cooper/REQUIREMENTS.md` | update docs to runtime-only cache model |

## Files That Should Not Change

| File | Reason |
|------|--------|
| `cooper/main.go` | cache import/seeding is intentionally out of scope |
