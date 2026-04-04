# Plan: Barrel Self-Managed Cache

## Summary

Replace host-mounted language caches with cooper-owned cache directories under `~/.cooper/cache/`.
During `cooper build`, seed these directories by copying from host caches (detected per-OS).
During `cooper up`, mount them read-write into the container. The AI CLI can then run
`go get`, `npm install`, `pip install` freely — provided the user approves the network
requests through the proxy monitor. Remove `GOFLAGS=-mod=readonly` since the module cache
is no longer a read-only host mount. Change default monitor timeout from 5s to 30s to give
users time to review install requests.

## Current State

### Host cache mounts (barrel.go:263-293)

```
Go:     $GOPATH/pkg/mod          → /home/user/go/pkg/mod          (ro)
Go:     ~/.cache/go-build        → /home/user/.cache/go-build     (rw)
Node:   ~/.npm                   → /home/user/.npm                (ro)
Python: ~/.cache/pip             → /home/user/.cache/pip          (ro)
```

These are mounted directly from the host at container start time in `appendLanguageCacheMounts`.

### Host dir pre-creation (barrel.go:324-330)

`ensureBarrelHostDirs` creates these host directories before Docker mounts them:

```go
filepath.Join(homeDir, ".npm"),
filepath.Join(homeDir, ".cache", "pip"),
filepath.Join(homeDir, ".cache", "go-build"),
filepath.Join(gopath, "pkg", "mod"),
```

### GOFLAGS (barrel.go:144-149)

```go
if isGoEnabled(cfg) {
    args = append(args, "-e", "GOFLAGS=-mod=readonly")
}
```

This prevents `go get` / `go mod tidy` from modifying `go.mod`/`go.sum` inside the
container. With self-managed caches, we remove this restriction — the AI can install
dependencies, subject to proxy approval.

### Default monitor timeout (config.go:100)

```go
MonitorTimeoutSecs: 5,
```

## Target State

### Cooper-managed cache directories

```
~/.cooper/cache/go-mod/     → /home/user/go/pkg/mod          (rw)
~/.cooper/cache/go-build/   → /home/user/.cache/go-build     (rw)
~/.cooper/cache/npm/        → /home/user/.npm                (rw)
~/.cooper/cache/pip/        → /home/user/.cache/pip          (rw)
```

All four are mounted **read-write**. The container owns these caches.

### Cache seeding during `cooper build`

During build, detect host cache locations using tool commands, then copy contents into
`~/.cooper/cache/` directories. This is a one-time operation — subsequent builds skip
seeding if the target directories already contain data.

### No GOFLAGS=-mod=readonly

Removed. The AI can freely run `go get`, `go mod tidy`, etc. The proxy still controls
what network requests are allowed.

### Default monitor timeout: 30 seconds

Changed from 5s to 30s. This gives users enough time to review and approve `go get` /
`npm install` / `pip install` requests that the AI triggers.

---

## Verified Assumptions

### 1. Go module cache directories have 0555 permissions (CONFIRMED)

Versioned module directories (e.g., `mongo-driver@v1.14.0`) are `555`. Files inside are
`444`. Top-level grouping directories (e.g., `go.mongodb.org`) are `775`. This means
`cp -a` preserves these restrictive permissions and a `chmod -R u+w` is required after
copying.

### 2. `cp -a` followed by `chmod -R u+w` correctly fixes Go module cache permissions (CONFIRMED)

Tested: copied a sample module directory with `cp -a`, confirmed directories were `555`.
After `chmod -R u+w`, directories changed to `755`. Files changed from `444` to `644`.
The cache is fully writable after this operation.

### 3. npm and pip caches have normal permissions (CONFIRMED)

- npm (`~/.npm`): all directories `775`, no special treatment needed
- pip (`~/.cache/pip`): all directories `775`, no special treatment needed

No `chmod` needed for these two.

### 4. `go env GOMODCACHE` and `go env GOCACHE` return correct platform-specific paths (CONFIRMED)

On Linux: `GOMODCACHE=/home/ricky/go/pkg/mod`, `GOCACHE=/home/ricky/.cache/go-build`.
These commands respect `$GOPATH` and platform defaults. On macOS, `GOCACHE` would return
`~/Library/Caches/go-build` instead of `~/.cache/go-build`. Using `go env` is the
cross-platform-correct way to find these.

### 5. `npm config get cache` returns correct path (CONFIRMED)

Returns `/home/ricky/.npm` on Linux. On macOS, also returns `~/.npm` by default.
Cross-platform safe.

### 6. `pip cache dir` returns correct path (CONFIRMED)

Returns `/home/ricky/.cache/pip` on Linux. On macOS, would return
`~/Library/Caches/pip`. Cross-platform safe.

### 7. Disk usage of host caches (MEASURED)

```
GOMODCACHE:       9.1 GB
GOCACHE:          3.6 GB
~/.npm:           2.3 GB
~/.cache/pip:     113 MB
Total:           ~15.1 GB (potential duplication)
```

Seeding copies all of this. Users should be informed of the disk space cost on first build.

### 8. Container user UID/GID matches host user (CONFIRMED)

The base Dockerfile template (`base.Dockerfile.tmpl:122-134`) creates the container user
with `USER_UID` and `USER_GID` build args, which are set to `os.Getuid()` and
`os.Getgid()` at build time (`main.go:316-318`). Files copied on the host (owned by host
user) will have matching ownership inside the container. No `chown` needed.

### 9. `~/.cooper/cache/` directory already exists (CONFIRMED)

It already exists and contains `ms-playwright/`. New subdirectories (`go-mod`, `go-build`,
`npm`, `pip`) will be created alongside it.

### 10. rsync is available and supports incremental copy (CONFIRMED)

`rsync 3.2.7` is installed. `rsync -a` preserves permissions and only copies changed
files, making subsequent seeding runs fast. However, rsync may not be available on all
systems (notably fresh macOS installs without Homebrew). We should use `cp -a` as the
primary mechanism since it's universally available, and the seeding only runs when the
target is empty anyway.

---

## Implementation

### Step 1: Add cache seeding function to `cooper/internal/docker/cache.go` (NEW FILE)

Create a new file `cooper/internal/docker/cache.go` with the cache seeding logic.

```go
package docker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/config"
)

// CacheSeedResult holds the result of seeding a single cache.
type CacheSeedResult struct {
	Name    string // e.g., "go-mod", "npm"
	Seeded  bool   // true if data was copied
	Skipped bool   // true if target already had data
	Err     error  // non-nil if seeding failed (non-fatal)
}

// SeedLanguageCaches copies host language caches into ~/.cooper/cache/ directories.
// This is called during `cooper build`. Each cache is only seeded if the target
// directory is empty (first run). Errors are non-fatal — a failed seed just means
// the AI will need to download dependencies from scratch.
//
// cooperDir is the path to ~/.cooper/.
// cfg is used to determine which programming tools are enabled.
func SeedLanguageCaches(cooperDir string, cfg *config.Config) []CacheSeedResult {
	var results []CacheSeedResult

	for _, tool := range cfg.ProgrammingTools {
		if !tool.Enabled {
			continue
		}
		switch tool.Name {
		case "go":
			results = append(results, seedGoModCache(cooperDir))
			results = append(results, seedGoBuildCache(cooperDir))
		case "node":
			results = append(results, seedNpmCache(cooperDir))
		case "python":
			results = append(results, seedPipCache(cooperDir))
		}
	}

	return results
}

// seedGoModCache copies the host Go module cache into ~/.cooper/cache/go-mod/.
// After copying, chmod -R u+w is applied because Go sets module directories to 0555.
func seedGoModCache(cooperDir string) CacheSeedResult {
	name := "go-mod"
	target := filepath.Join(cooperDir, "cache", name)

	if dirHasContents(target) {
		return CacheSeedResult{Name: name, Skipped: true}
	}

	// Use `go env GOMODCACHE` to find the host cache location (cross-platform).
	hostPath, err := goEnv("GOMODCACHE")
	if err != nil {
		return CacheSeedResult{Name: name, Err: fmt.Errorf("resolve GOMODCACHE: %w", err)}
	}
	if !dirHasContents(hostPath) {
		return CacheSeedResult{Name: name, Skipped: true}
	}

	if err := os.MkdirAll(target, 0755); err != nil {
		return CacheSeedResult{Name: name, Err: fmt.Errorf("mkdir %s: %w", target, err)}
	}

	// cp -a preserves symlinks, timestamps, and ownership.
	// Trailing /. copies contents of hostPath into target (not hostPath itself).
	if err := cpDir(hostPath, target); err != nil {
		return CacheSeedResult{Name: name, Err: fmt.Errorf("copy: %w", err)}
	}

	// Go module cache has 0555 dirs and 0444 files. Make writable.
	if err := chmodWritable(target); err != nil {
		return CacheSeedResult{Name: name, Err: fmt.Errorf("chmod: %w", err)}
	}

	return CacheSeedResult{Name: name, Seeded: true}
}

// seedGoBuildCache copies the host Go build cache into ~/.cooper/cache/go-build/.
func seedGoBuildCache(cooperDir string) CacheSeedResult {
	name := "go-build"
	target := filepath.Join(cooperDir, "cache", name)

	if dirHasContents(target) {
		return CacheSeedResult{Name: name, Skipped: true}
	}

	hostPath, err := goEnv("GOCACHE")
	if err != nil {
		return CacheSeedResult{Name: name, Err: fmt.Errorf("resolve GOCACHE: %w", err)}
	}
	if !dirHasContents(hostPath) {
		return CacheSeedResult{Name: name, Skipped: true}
	}

	if err := os.MkdirAll(target, 0755); err != nil {
		return CacheSeedResult{Name: name, Err: fmt.Errorf("mkdir %s: %w", target, err)}
	}

	if err := cpDir(hostPath, target); err != nil {
		return CacheSeedResult{Name: name, Err: fmt.Errorf("copy: %w", err)}
	}

	return CacheSeedResult{Name: name, Seeded: true}
}

// seedNpmCache copies the host npm cache into ~/.cooper/cache/npm/.
func seedNpmCache(cooperDir string) CacheSeedResult {
	name := "npm"
	target := filepath.Join(cooperDir, "cache", name)

	if dirHasContents(target) {
		return CacheSeedResult{Name: name, Skipped: true}
	}

	// Use `npm config get cache` to find the host cache location (cross-platform).
	out, err := exec.Command("npm", "config", "get", "cache").Output()
	if err != nil {
		return CacheSeedResult{Name: name, Err: fmt.Errorf("resolve npm cache: %w", err)}
	}
	hostPath := strings.TrimSpace(string(out))
	if !dirHasContents(hostPath) {
		return CacheSeedResult{Name: name, Skipped: true}
	}

	if err := os.MkdirAll(target, 0755); err != nil {
		return CacheSeedResult{Name: name, Err: fmt.Errorf("mkdir %s: %w", target, err)}
	}

	if err := cpDir(hostPath, target); err != nil {
		return CacheSeedResult{Name: name, Err: fmt.Errorf("copy: %w", err)}
	}

	return CacheSeedResult{Name: name, Seeded: true}
}

// seedPipCache copies the host pip cache into ~/.cooper/cache/pip/.
func seedPipCache(cooperDir string) CacheSeedResult {
	name := "pip"
	target := filepath.Join(cooperDir, "cache", name)

	if dirHasContents(target) {
		return CacheSeedResult{Name: name, Skipped: true}
	}

	// Use `pip cache dir` to find the host cache location (cross-platform).
	// Try pip3 first (common on systems where python3 is default), fall back to pip.
	hostPath, err := pipCacheDir()
	if err != nil {
		return CacheSeedResult{Name: name, Err: fmt.Errorf("resolve pip cache: %w", err)}
	}
	if !dirHasContents(hostPath) {
		return CacheSeedResult{Name: name, Skipped: true}
	}

	if err := os.MkdirAll(target, 0755); err != nil {
		return CacheSeedResult{Name: name, Err: fmt.Errorf("mkdir %s: %w", target, err)}
	}

	if err := cpDir(hostPath, target); err != nil {
		return CacheSeedResult{Name: name, Err: fmt.Errorf("copy: %w", err)}
	}

	return CacheSeedResult{Name: name, Seeded: true}
}

// goEnv runs `go env <key>` and returns the trimmed output.
func goEnv(key string) (string, error) {
	out, err := exec.Command("go", "env", key).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// pipCacheDir runs `pip3 cache dir` (or `pip cache dir` as fallback) and returns
// the trimmed output.
func pipCacheDir() (string, error) {
	out, err := exec.Command("pip3", "cache", "dir").Output()
	if err != nil {
		out, err = exec.Command("pip", "cache", "dir").Output()
		if err != nil {
			return "", fmt.Errorf("neither pip3 nor pip available: %w", err)
		}
	}
	return strings.TrimSpace(string(out)), nil
}

// dirHasContents returns true if dir exists and contains at least one entry.
func dirHasContents(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

// cpDir copies the contents of src into dst using `cp -a src/. dst/`.
// The trailing /. ensures contents are copied, not the directory itself.
func cpDir(src, dst string) error {
	cmd := exec.Command("cp", "-a", src+"/.", dst+"/")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, string(output))
	}
	return nil
}

// chmodWritable runs `chmod -R u+w` on the given directory.
// This is needed for Go module cache directories which are set to 0555.
func chmodWritable(dir string) error {
	cmd := exec.Command("chmod", "-R", "u+w", dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, string(output))
	}
	return nil
}
```

### Step 2: Modify `appendLanguageCacheMounts` in `barrel.go`

**File:** `cooper/internal/docker/barrel.go`
**Function:** `appendLanguageCacheMounts` (lines 260-293)

Replace the entire function body. Instead of mounting from host paths, mount from
`~/.cooper/cache/` directories.

**Before (current code):**

```go
func appendLanguageCacheMounts(args []string, homeDir string, cfg *config.Config) []string {
	for _, tool := range cfg.ProgrammingTools {
		if !tool.Enabled {
			continue
		}
		switch tool.Name {
		case "go":
			gopath := os.Getenv("GOPATH")
			if gopath == "" {
				gopath = filepath.Join(homeDir, "go")
			}
			hostModCache := filepath.Join(gopath, "pkg", "mod")
			hostBuildCache := filepath.Join(homeDir, ".cache", "go-build")
			containerModCache := filepath.Join(containerHome, "go", "pkg", "mod")
			containerBuildCache := filepath.Join(containerHome, ".cache", "go-build")
			args = append(args,
				"-v", fmt.Sprintf("%s:%s:ro", hostModCache, containerModCache),
				"-v", fmt.Sprintf("%s:%s:rw", hostBuildCache, containerBuildCache),
			)
		case "node":
			hostNpm := filepath.Join(homeDir, ".npm")
			containerNpm := filepath.Join(containerHome, ".npm")
			args = append(args, "-v", fmt.Sprintf("%s:%s:ro", hostNpm, containerNpm))
		case "python":
			hostPip := filepath.Join(homeDir, ".cache", "pip")
			containerPip := filepath.Join(containerHome, ".cache", "pip")
			args = append(args, "-v", fmt.Sprintf("%s:%s:ro", hostPip, containerPip))
		}
	}
	return args
}
```

**After (new code):**

```go
// appendLanguageCacheMounts adds cache volume mounts from cooper-managed
// directories (~/.cooper/cache/) based on which programming tools are enabled.
// All mounts are read-write so the container can install new dependencies.
func appendLanguageCacheMounts(args []string, cooperDir string, cfg *config.Config) []string {
	for _, tool := range cfg.ProgrammingTools {
		if !tool.Enabled {
			continue
		}
		switch tool.Name {
		case "go":
			goModCache := filepath.Join(cooperDir, "cache", "go-mod")
			goBuildCache := filepath.Join(cooperDir, "cache", "go-build")
			containerModCache := filepath.Join(containerHome, "go", "pkg", "mod")
			containerBuildCache := filepath.Join(containerHome, ".cache", "go-build")
			args = append(args,
				"-v", fmt.Sprintf("%s:%s:rw", goModCache, containerModCache),
				"-v", fmt.Sprintf("%s:%s:rw", goBuildCache, containerBuildCache),
			)
		case "node":
			npmCache := filepath.Join(cooperDir, "cache", "npm")
			containerNpm := filepath.Join(containerHome, ".npm")
			args = append(args, "-v", fmt.Sprintf("%s:%s:rw", npmCache, containerNpm))
		case "python":
			pipCache := filepath.Join(cooperDir, "cache", "pip")
			containerPip := filepath.Join(containerHome, ".cache", "pip")
			args = append(args, "-v", fmt.Sprintf("%s:%s:rw", pipCache, containerPip))
		}
	}
	return args
}
```

**Critical:** The function signature changes from `homeDir string` to `cooperDir string`.
Update the call site in `appendVolumeMounts` (line 214):

**Before:**

```go
args = appendLanguageCacheMounts(args, homeDir, cfg)
```

**After:**

```go
args = appendLanguageCacheMounts(args, cooperDir, cfg)
```

### Step 3: Update `ensureBarrelHostDirs` in `barrel.go`

**File:** `cooper/internal/docker/barrel.go`
**Function:** `ensureBarrelHostDirs` (lines 295-344)

Remove host language cache directory creation. Add cooper cache directory creation instead.

**Remove these lines (324-330):**

```go
// Language cache dirs (always needed).
dirs = append(dirs,
    filepath.Join(homeDir, ".npm"),
    filepath.Join(homeDir, ".cache", "pip"),
    filepath.Join(homeDir, ".cache", "go-build"),
    filepath.Join(gopath, "pkg", "mod"),
)
```

**Replace with:**

```go
// Cooper-managed language cache dirs (must exist before Docker mounts them).
dirs = append(dirs,
    filepath.Join(cooperDir, "cache", "go-mod"),
    filepath.Join(cooperDir, "cache", "go-build"),
    filepath.Join(cooperDir, "cache", "npm"),
    filepath.Join(cooperDir, "cache", "pip"),
)
```

**Also:** The `gopath` variable and its resolution (lines 303-306) are no longer needed
in this function. Remove:

```go
gopath := os.Getenv("GOPATH")
if gopath == "" {
    gopath = filepath.Join(homeDir, "go")
}
```

**Also:** The function signature needs `cooperDir` added as a parameter. Currently it has:

```go
func ensureBarrelHostDirs(absWorkspace, toolName, cooperDir string) error {
```

It already receives `cooperDir` — good. But it currently uses `homeDir` for the cache
dirs. After this change, it uses `cooperDir` instead. The `homeDir` variable is still
needed for the auth dirs (lines 310-322) and Playwright dirs (lines 332-337), so keep it.

### Step 4: Remove `GOFLAGS=-mod=readonly` from `barrel.go`

**File:** `cooper/internal/docker/barrel.go`
**Lines:** 144-149

**Delete these lines:**

```go
// If Go is enabled, set GOFLAGS=-mod=readonly to prevent the AI from
// modifying go.mod/go.sum inside the container. Dependencies must be
// installed on the host.
if isGoEnabled(cfg) {
    args = append(args, "-e", "GOFLAGS=-mod=readonly")
}
```

### Step 5: Add cache seeding step to `runBuild` in `main.go`

**File:** `cooper/main.go`
**Function:** `runBuild` (lines 258-372)

Add a new step between step 1 (template generation) and the existing version resolution.
Insert after line 272 (after directory creation, before version resolution):

```go
// 2. Seed language caches from host (first build only).
fmt.Fprintln(os.Stderr, "Seeding language caches...")
seedResults := docker.SeedLanguageCaches(cooperDir, cfg)
for _, r := range seedResults {
    if r.Err != nil {
        fmt.Fprintf(os.Stderr, "  Warning: %s cache seed failed: %v\n", r.Name, r.Err)
    } else if r.Seeded {
        fmt.Fprintf(os.Stderr, "  Seeded %s cache from host.\n", r.Name)
    } else if r.Skipped {
        fmt.Fprintf(os.Stderr, "  %s cache already populated, skipping.\n", r.Name)
    }
}
```

The step numbers of subsequent steps in the comments shift by 1. Update them:

```
// 2. Seed language caches from host (first build only).   <-- NEW
// 3. Resolve latest versions...                           <-- was 2 (implicit)
// 4. Ensure CA certificate exists.                        <-- was 2
// 5. Write ACL helper source...                           <-- was 3
// 6. Stage CA files into build contexts.                  <-- was 4
// 7. Build proxy image.                                   <-- was 5
// 8. Build base image.                                    <-- was 6
// 9. Build each enabled AI tool image.                    <-- was 7
// 10. Build user-custom images.                           <-- was 8
// 11. Update ContainerVersion in config.                  <-- was 9
```

### Step 6: Change default `MonitorTimeoutSecs` from 5 to 30

**File:** `cooper/internal/config/config.go`
**Line:** 100

**Before:**

```go
MonitorTimeoutSecs: 5,
```

**After:**

```go
MonitorTimeoutSecs: 30,
```

**IMPORTANT:** Existing config files will already have `"monitor_timeout_secs": 5` saved.
The change only affects new `cooper configure` runs. This is intentional — existing users
keep their current timeout. If they want 30s, they change it in the TUI settings screen.

### Step 7: Update `doctor.sh` diagnostic script

**File:** `cooper/internal/templates/doctor.sh`
**Lines:** 337-346

**Before:**

```bash
# Check GOFLAGS
if [ -n "${GOFLAGS:-}" ]; then
    if echo "$GOFLAGS" | grep -q "mod=readonly"; then
        pass "GOFLAGS includes -mod=readonly"
    else
        info "GOFLAGS set but no -mod=readonly: ${GOFLAGS}"
    fi
else
    warn "GOFLAGS not set (Go modules not in readonly mode)"
fi
```

**After:**

```bash
# Check GOFLAGS (should NOT have -mod=readonly — cooper uses self-managed caches)
if [ -n "${GOFLAGS:-}" ]; then
    if echo "$GOFLAGS" | grep -q "mod=readonly"; then
        warn "GOFLAGS includes -mod=readonly (unexpected — cooper uses self-managed caches)"
    else
        info "GOFLAGS set: ${GOFLAGS}"
    fi
else
    pass "GOFLAGS not set (cooper manages module cache independently)"
fi
```

### Step 8: Update e2e test expectations in `test-e2e.sh`

**File:** `cooper/test-e2e.sh`

#### 8a: Remove GOFLAGS from barrel startup commands

Search for all occurrences of `-e" "GOFLAGS=-mod=readonly"` and remove them. There are
4 occurrences (lines 490-491, 813, 919, 1820). Remove each one.

For example, at line 490-491:

**Before:**

```bash
        # GOFLAGS (since Go is enabled).
        "-e" "GOFLAGS=-mod=readonly"
```

**Delete both lines.** Repeat for the other 3 occurrences.

#### 8b: Update GOFLAGS e2e test assertion

At lines 1515-1521:

**Before:**

```bash
# GOFLAGS set correctly.
goflags=$(barrel_exec 'echo $GOFLAGS')
if echo "$goflags" | grep -q "mod=readonly"; then
    pass "GOFLAGS includes -mod=readonly"
else
    fail "GOFLAGS not set correctly (got: ${goflags})"
fi
```

**After:**

```bash
# GOFLAGS should NOT include -mod=readonly (cooper uses self-managed caches).
goflags=$(barrel_exec 'echo $GOFLAGS')
if echo "$goflags" | grep -q "mod=readonly"; then
    fail "GOFLAGS unexpectedly includes -mod=readonly"
else
    pass "GOFLAGS does not include -mod=readonly (self-managed cache)"
fi
```

#### 8c: Update cache mount assertions in e2e tests

Find existing cache mount assertions in the e2e tests. These currently check for host
paths. Update them to check for `~/.cooper/cache/` paths instead.

Search for mount validation assertions related to `go/pkg/mod`, `.npm`, `.cache/pip`,
`.cache/go-build` in `test-e2e.sh` and update them to verify the new mount sources.

The mounts should now look like:

```
~/.cooper/cache/go-mod:/home/user/go/pkg/mod:rw
~/.cooper/cache/go-build:/home/user/.cache/go-build:rw
~/.cooper/cache/npm:/home/user/.npm:rw
~/.cooper/cache/pip:/home/user/.cache/pip:rw
```

Note all four are now `:rw` (previously go-mod, npm, and pip were `:ro`).

### Step 9: Update REQUIREMENTS.md

**File:** `cooper/REQUIREMENTS.md`

Update the following sections to reflect the new cache architecture:

#### 9a: Line 163-164 (user-facing description)

**Before:**

```
User is told that the AI CLI mounts host machine's module cache (like go mod cache, npm cache, pip cache, etc.) into the CLI container as
read-only volume, so they can read dependencies that are already in host machine, but cannot change them.
```

**After:**

```
Cooper seeds its own cache directories (~/.cooper/cache/) from the host's module caches during `cooper build`.
The AI CLI can install dependencies freely — network requests go through the proxy and require user approval.
```

#### 9b: Lines 343-354 (barrel volume mounts and dependency workflow)

**Before:**

```
- Language-specific caches (auto-configured based on enabled programming tools):
  - Go: `$GOPATH/pkg/mod` (read-only), `~/.cache/go-build` (read-write), `GOFLAGS=-mod=readonly`
  - Node: `~/.npm` (read-only)
  - Python: `~/.cache/pip` (read-only)
- Directories are created on host if they don't exist (`mkdir -p` before mount).
- Dependency workflow per ecosystem (host-preload model — same pattern for all, different commands):
  - Go: `go mod download` on host populates `$GOPATH/pkg/mod`, mounted read-only. `GOFLAGS=-mod=readonly` enforced.
  - Node: `npm install` on host populates `node_modules/` in workspace (rw) and `~/.npm` cache (ro). AI can use existing deps but not install new ones from registry.
  - Python: `pip install`, `pipenv install`, or `poetry install` on host. The workspace is mounted rw, so virtualenvs
    created inside the workspace (e.g., `.venv/`) are accessible inside the container. `~/.cache/pip` is mounted ro
    for cached wheels.
```

**After:**

```
- Cooper-managed language caches (seeded from host during `cooper build`, all read-write):
  - Go: `~/.cooper/cache/go-mod` → `/home/user/go/pkg/mod` (rw), `~/.cooper/cache/go-build` → `/home/user/.cache/go-build` (rw)
  - Node: `~/.cooper/cache/npm` → `/home/user/.npm` (rw)
  - Python: `~/.cooper/cache/pip` → `/home/user/.cache/pip` (rw)
- Cache seeding: During `cooper build`, host caches are detected via `go env`, `npm config get cache`, `pip cache dir` and
  copied into `~/.cooper/cache/`. Only runs when the target is empty (first build). Go module cache requires `chmod -R u+w`
  after copy because Go sets directories to 0555.
- Dependency workflow: The AI can run `go get`, `npm install`, `pip install` etc. freely. Network requests go through
  the proxy and require user approval. The default monitor timeout is 30 seconds.
```

### Step 10: Write unit tests for cache seeding in `cache_test.go`

**File:** `cooper/internal/docker/cache_test.go` (NEW FILE)

```go
package docker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestDirHasContents(t *testing.T) {
	// Empty dir.
	empty := t.TempDir()
	if dirHasContents(empty) {
		t.Error("empty dir should return false")
	}

	// Non-existent dir.
	if dirHasContents("/tmp/does-not-exist-cooper-test") {
		t.Error("non-existent dir should return false")
	}

	// Dir with one file.
	withFile := t.TempDir()
	os.WriteFile(filepath.Join(withFile, "test.txt"), []byte("hi"), 0644)
	if !dirHasContents(withFile) {
		t.Error("dir with file should return true")
	}
}

func TestSeedLanguageCaches_SkipsWhenTargetHasData(t *testing.T) {
	cooperDir := t.TempDir()

	// Pre-populate go-mod cache target with a file.
	goModTarget := filepath.Join(cooperDir, "cache", "go-mod")
	os.MkdirAll(goModTarget, 0755)
	os.WriteFile(filepath.Join(goModTarget, "marker"), []byte("exists"), 0644)

	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "go", Enabled: true},
		},
	}

	results := SeedLanguageCaches(cooperDir, cfg)

	// go-mod should be skipped (already has data).
	for _, r := range results {
		if r.Name == "go-mod" {
			if !r.Skipped {
				t.Errorf("go-mod: expected Skipped=true, got Seeded=%v Err=%v", r.Seeded, r.Err)
			}
		}
	}
}

func TestSeedLanguageCaches_SkipsDisabledTools(t *testing.T) {
	cooperDir := t.TempDir()

	cfg := &config.Config{
		ProgrammingTools: []config.ToolConfig{
			{Name: "go", Enabled: false},
			{Name: "node", Enabled: false},
			{Name: "python", Enabled: false},
		},
	}

	results := SeedLanguageCaches(cooperDir, cfg)
	if len(results) != 0 {
		t.Errorf("expected 0 results for disabled tools, got %d", len(results))
	}
}

func TestCpDir(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Create a file in src.
	os.WriteFile(filepath.Join(src, "hello.txt"), []byte("world"), 0644)
	os.MkdirAll(filepath.Join(src, "subdir"), 0755)
	os.WriteFile(filepath.Join(src, "subdir", "nested.txt"), []byte("deep"), 0644)

	if err := cpDir(src, dst); err != nil {
		t.Fatalf("cpDir: %v", err)
	}

	// Verify contents were copied.
	data, err := os.ReadFile(filepath.Join(dst, "hello.txt"))
	if err != nil || string(data) != "world" {
		t.Errorf("hello.txt: got %q, err %v", string(data), err)
	}
	data, err = os.ReadFile(filepath.Join(dst, "subdir", "nested.txt"))
	if err != nil || string(data) != "deep" {
		t.Errorf("subdir/nested.txt: got %q, err %v", string(data), err)
	}
}

func TestChmodWritable(t *testing.T) {
	dir := t.TempDir()

	// Create a 0555 directory with a 0444 file (simulating Go module cache).
	subdir := filepath.Join(dir, "mod@v1.0.0")
	os.MkdirAll(subdir, 0555)
	file := filepath.Join(subdir, "go.mod")
	os.WriteFile(file, []byte("module test"), 0444)

	if err := chmodWritable(dir); err != nil {
		t.Fatalf("chmodWritable: %v", err)
	}

	// Verify directory is writable.
	info, _ := os.Stat(subdir)
	if info.Mode().Perm()&0200 == 0 {
		t.Errorf("subdir should be writable, got %o", info.Mode().Perm())
	}

	// Verify file is writable.
	info, _ = os.Stat(file)
	if info.Mode().Perm()&0200 == 0 {
		t.Errorf("file should be writable, got %o", info.Mode().Perm())
	}
}
```

### Step 11: Update `cooper_test.go` assertions

**File:** `cooper/internal/app/cooper_test.go`

Search for any test assertions that check for:
- `GOFLAGS` containing `mod=readonly`
- Cache mount paths containing host paths (e.g., `$GOPATH/pkg/mod`, `~/.npm`)
- Read-only (`:ro`) mount assertions for language caches

Update them to reflect:
- No `GOFLAGS=-mod=readonly` env var
- Cache mounts from `~/.cooper/cache/` directories
- All language cache mounts are `:rw`

### Step 12: Update existing default timeout in test expectations

**File:** `cooper/internal/config/config_test.go`
**Line:** 307

**Before:**

```go
MonitorTimeoutSecs: 5,
```

**After:**

```go
MonitorTimeoutSecs: 30,
```

Also update `cooper/internal/app/cooper_test.go` line 364-365:

**Before:**

```go
if cfg.MonitorTimeoutSecs != 5 {
    t.Fatalf("precondition: MonitorTimeoutSecs = %d, want 5", cfg.MonitorTimeoutSecs)
}
```

**After:**

```go
if cfg.MonitorTimeoutSecs != 30 {
    t.Fatalf("precondition: MonitorTimeoutSecs = %d, want 30", cfg.MonitorTimeoutSecs)
}
```

---

## Files Changed (Summary)

| File | Action | Description |
|------|--------|-------------|
| `cooper/internal/docker/cache.go` | **NEW** | Cache seeding logic: `SeedLanguageCaches`, per-cache seed functions, helpers |
| `cooper/internal/docker/cache_test.go` | **NEW** | Unit tests for cache seeding |
| `cooper/internal/docker/barrel.go` | **MODIFY** | Rewrite `appendLanguageCacheMounts` to use `cooperDir`; update `ensureBarrelHostDirs` to create cooper cache dirs; remove `GOFLAGS=-mod=readonly` |
| `cooper/main.go` | **MODIFY** | Add cache seeding step in `runBuild` |
| `cooper/internal/config/config.go` | **MODIFY** | Change `MonitorTimeoutSecs` default from `5` to `30` |
| `cooper/internal/templates/doctor.sh` | **MODIFY** | Invert GOFLAGS diagnostic (warn if present, pass if absent) |
| `cooper/test-e2e.sh` | **MODIFY** | Remove GOFLAGS from barrel commands; update cache mount and GOFLAGS assertions |
| `cooper/REQUIREMENTS.md` | **MODIFY** | Document new cache architecture |
| `cooper/internal/config/config_test.go` | **MODIFY** | Update default timeout expectation from `5` to `30` |
| `cooper/internal/app/cooper_test.go` | **MODIFY** | Update timeout precondition and any cache/GOFLAGS assertions |

## Out of Scope

- **macOS testing:** This plan is designed to be cross-platform via `go env`, `npm config
  get cache`, and `pip cache dir`. Actual macOS verification is deferred to when macOS
  support is added.
- **Cache cleanup / garbage collection:** No mechanism to clear or resize
  `~/.cooper/cache/`. Users can delete the directories manually; they will be recreated
  (empty) on next `cooper up`.
- **Re-seeding:** If the user wants to refresh from host caches, they delete
  `~/.cooper/cache/{name}/` and run `cooper build` again.
- **Config migration for existing users:** Existing users with `monitor_timeout_secs: 5`
  keep that value. The new default (30) only applies to fresh `cooper configure`.
