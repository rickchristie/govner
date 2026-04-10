# Plan: 4-Way Parallel Docker Test Execution

## Status

This is a handoff specification for another implementation session.

This document is intentionally normative. It is not brainstorming. The next
agent should treat the design decisions, invariants, migration order, and
pitfalls here as the default contract unless new local evidence proves a
specific point wrong.

## Why This Plan Exists

Cooper's Docker-backed tests are now much faster than before, but `go test ./...`
still serializes more than it should.

Recent verified baseline after the earlier speedups:

- `go test ./internal/app` completed in about `106.7s`
- `go test ./...` in `cooper/` completed in about `118.8s`
- `internal/docker` and `internal/testdriver` each complete quickly on their own
  when no package-level lock makes them wait

The remaining wall-clock waste is not primarily Docker build time anymore. The
main remaining problem is overly broad coordination:

- package `TestMain` still holds a shared cross-process lock for the entire
  package run
- the Docker runtime namespace is still process-global mutable state
- cleanup is still namespace-wide and destructive
- several helpers still assume one global runtime per process

This plan fixes that in two stages:

1. immediate overlap between Docker-backed packages
2. then selective `t.Parallel()` inside `internal/app`, capped at 4 active
   Docker runtimes across all package processes

## Context At Time Of Writing

This plan assumes the earlier test-speed work is already in place:

- fixed sleeps in `internal/app/cooper_test.go` were replaced with polling
- public internet dependency in proxy tests was replaced by a local HTTPS target
- repeated startup for proxy and CLI runtime tests was reduced by grouping them
  into:
  - `TestCooperApp_ProxyRuntimeScenarios`
  - `TestCooperApp_CLIRuntimeScenarios`

This plan does **not** redo that work. It builds on top of it.

## End-State Summary

The target end state is:

- shared Docker image bootstrap remains serialized
- active Docker runtimes are capped at **4** across all `go test` package
  processes
- runtime resources are isolated by unique namespace
- runtime cleanup only deletes the owning runtime
- `go test ./...` no longer makes `internal/app`, `internal/docker`, and
  `internal/testdriver` wait for one another just because one package is still
  running
- selected Docker-backed tests in `internal/app` can safely use `t.Parallel()`

This does **not** mean "remove all coordination". The desired coordination is:

- one short shared build lock
- one 4-slot shared runtime limiter
- no package-wide "single owner of everything" lock

## Non-Negotiable Requirements

The implementation MUST preserve all of the following:

1. Plain `go test ./...` inside `cooper/` must still run the Docker-backed tests.
2. Docker-backed tests must not interfere with a live `cooper up`.
3. Test images must keep using the existing shared image prefix
   `cooper-gotest-`, because it is already intentionally isolated from the shell
   scripts and from production images.
4. Tests must keep using dynamic host ports.
5. Waiting states must remain visible in logs. If a build lock, runtime slot,
   or readiness poll is still progressing, logs should continue printing.
6. Runtime cleanup must remain fail-fast, using the existing short test stop
   timeout behavior.
7. If cleanup fails, the runtime slot must still be released.
8. Another test runtime must never be able to delete this runtime's proxy,
   networks, or barrels.

## Explicit Non-Goals

These are intentionally out of scope:

- unlimited parallelism
- Windows or macOS Docker environments
- changing production runtime names
- changing the existing shared test image prefix
- making tests use different image prefixes concurrently
- parallelizing every Docker-backed test immediately
- refactoring unrelated production architecture just because it is nearby

## Terminology

This document uses the following terms precisely:

- **build lock**: exclusive cross-process lock used only while checking Docker
  availability, fingerprinting shared images, and rebuilding shared images
- **runtime slot**: one of four cross-process capacity tokens for "an active
  Docker runtime"
- **runtime**: one isolated Cooper Docker topology consisting of:
  - one proxy container
  - zero or more barrel containers
  - one internal network
  - one external network
  - zero or more helper containers such as the local HTTPS target
- **runtime lease**: the ownership object for one runtime slot plus one unique
  runtime namespace
- **package runtime**: transitional phase-1 runtime owned by a whole package
  process
- **fixture runtime**: final phase runtime owned by one test or one parent test
  with sequential subtests

## Verified Current State

This section is here to remove ambiguity for the next agent. The references are
"current at time of writing"; if line numbers drift, search by the function
names shown below.

### 1. Broad package locking exists today

In `internal/testdocker/bootstrap.go`:

```go
const lockPath = "/tmp/cooper-gotest.lock"

func AcquireLock() (*Lock, error)
func SetupPackageNamed(name string, ensureImages bool) (*Lock, error)
```

`SetupPackageNamed(...)` currently:

- acquires the shared lock
- checks Docker availability
- sets `docker.SetImagePrefix(...)`
- sets `docker.SetRuntimeNamespace(...)`
- sets `docker.SetStopTimeoutSeconds(...)`
- calls `docker.CleanupRuntime()`
- optionally rebuilds shared images
- returns the lock for the caller to hold until package end

That makes the lock much broader than "build bootstrap".

### 2. Package `TestMain` holds that lock for the whole package run

Current package mains:

- `internal/app/testmain_test.go`
- `internal/testdriver/testmain_test.go`
- `internal/docker/testmain_test.go`

`internal/app/testmain_test.go` currently does this:

```go
lock, err := testdocker.SetupPackageNamed("internal/app", true)
code := m.Run()
_ = docker.CleanupRuntime()
_ = lock.Release()
```

That is why other packages wait even after image build has already finished.

### 3. `internal/docker` still duplicates its own lock/bootstrap logic

`internal/docker/testmain_test.go` does not reuse the common `testdocker`
bootstrap fully. It has its own:

- lock path
- lock bookkeeping
- Docker availability check
- runtime namespace activation

That duplication must be removed as part of this refactor.

### 4. Runtime identity is still global mutable state

`internal/docker/runtime_names.go` currently exposes:

```go
var runtimeNamespace = "cooper"

func SetRuntimeNamespace(namespace string)
func RuntimeNamespace() string
func ProxyContainerName() string
func ExternalNetworkName() string
func InternalNetworkName() string
func ProxyHost() string
func BarrelNamePrefix() string
```

This is the main reason multiple runtimes cannot safely coexist in one package
process today.

### 5. Stop timeout is also still global mutable state

`internal/docker/stop.go` currently exposes:

```go
var stopTimeoutSeconds = -1

func SetStopTimeoutSeconds(seconds int)
```

That also becomes unsafe once parallel runtimes exist in one process.

### 6. Cleanup is namespace-wide and destructive

`internal/docker/cleanup.go`:

```go
func CleanupRuntime() error {
    barrelNames, _ := listAllRuntimeBarrelNames()
    for _, barrelName := range barrelNames {
        _ = StopBarrel(barrelName)
    }
    _ = StopProxy()
    _ = RemoveNetworks()
}
```

This is correct only when one owner owns the whole namespace.

### 7. Barrel startup removes same-name containers

`internal/docker/barrel.go`:

```go
_ = exec.Command("docker", "rm", "-f", name).Run()
```

This is safe only if names are unique per runtime.

### 8. `CooperApp` itself still uses global Docker helpers

`internal/app/cooper.go` currently imports `internal/docker` directly and calls
global runtime-sensitive helpers such as:

```go
docker.EnsureNetworks()
docker.StartProxy(a.cfg, a.cooperDir)
docker.GetGatewayIP(docker.ExternalNetworkName())
docker.ListBarrels()
docker.StopBarrel(name)
docker.StopProxy()
docker.IsProxyRunning()
docker.ReloadSocat(...)
```

This means test isolation cannot stop at test helpers alone. `CooperApp` must
also stop assuming one process-global runtime.

### 9. Test helpers still depend on global runtime state

Examples:

- `internal/app/cooper_test.go`
  - repeated `docker.SetImagePrefix(testImagePrefix)`
  - `cleanupDocker(t)` calls global `docker.CleanupRuntime()`
  - `trustProxyHTTPSTarget(...)` uses global `docker.ProxyContainerName()`
- `internal/testdocker/target.go`
  - `StartHTTPSTarget(...)` uses `docker.RuntimeNamespace()` and
    `docker.ExternalNetworkName()`
- `internal/testdriver/driver.go`
  - constructor calls `docker.SetImagePrefix(prefix)`
  - constructor calls `testdocker.AcquireLock()`
  - cleanup calls global runtime cleanup

### 10. Some tests still mutate process-global environment

At time of writing, `internal/app/cooper_test.go` contains:

```go
t.Setenv("CLAUDECODE", "1")
```

This is inside the CLI runtime suite. That specific path cannot be placed under
a parallel parent test until it is refactored away from process-global env
mutation.

### 11. Shared image prefix is intentionally fixed

The current shared test image prefix in `internal/testdocker/bootstrap.go` is:

```go
const ImagePrefix = "cooper-gotest-"
```

Keep this. Do not change it. The user explicitly wanted it not to clash with
the shell-based test scripts, and that problem is already solved.

## Critical Design Decisions

These choices are deliberate and should not be re-litigated during
implementation unless new evidence forces a change.

### Decision 1: Split build coordination from runtime coordination

The current single broad lock should become:

- one **build lock** for shared image/bootstrap work
- one **runtime slot limiter** for active runtime capacity

Do **not** keep a single lock that covers both.

### Decision 2: Phase 1 must be transitional, not perfect

The first milestone should improve package overlap **before** the full
`docker.Runtime` refactor is complete.

That means Phase 1 is allowed to keep package-global runtime activation inside a
package process, as long as:

- each package gets a **unique package runtime namespace**
- packages no longer hold the build lock while running tests

This is the shortest safe path to immediate wall-clock wins.

### Decision 3: Final runtime isolation is explicit, not implicit

The final design should use an explicit runtime value or fixture, not hidden
global mutation.

Tests and `CooperApp` must stop depending on:

- `SetRuntimeNamespace(...)`
- `SetStopTimeoutSeconds(...)`
- global cleanup helpers for runtime-sensitive work

### Decision 4: Keep shared image prefix fixed for this refactor

Do **not** expand scope into "fully per-runtime image prefix" unless new
evidence makes it necessary.

Reason:

- the entire suite uses the same shared test images
- templates and proof code currently consume `docker.GetImageBase()` and
  siblings globally
- image-prefix parallelism is not the bottleneck this refactor is solving

So the supported automated-test contract remains:

- one shared image prefix: `cooper-gotest-`
- many isolated runtime namespaces

`internal/testdriver/driver.go` may continue to expose an image-prefix option
for manual tooling if desired, but the automated Go test suite for this
refactor must keep using the shared test prefix only. Do not attempt to make
multiple image prefixes run concurrently as part of this change.

### Decision 5: Runtime slot acquisition is not re-entrant

The old broad build lock was re-entrant per process. Runtime slots must **not**
reuse that behavior.

Reason:

- one package process may legitimately need multiple active runtimes once
  `t.Parallel()` is enabled
- each active runtime must consume one slot

So:

- build lock: may remain process-local re-entrant if useful
- runtime slot: one acquisition == one actual slot file held

### Decision 6: Never hold the build lock while waiting for a runtime slot

Required acquisition order:

1. ensure shared images under the build lock
2. release the build lock
3. acquire a runtime slot
4. create/use the runtime

Do not reverse this. Doing so would create unnecessary contention and can cause
priority inversion.

## Proposed Architecture

## A. Shared build coordination

### Paths

Default state directory:

```text
/tmp
```

Default build lock path:

```text
/tmp/cooper-gotest-build.lock
```

Default build dir:

```text
/tmp/cooper-gotest-build
```

### Testability override

Add one internal override for unit-testing the coordinator itself:

```text
COOPER_TESTDOCKER_STATE_DIR
```

Behavior:

- if unset: use `/tmp`
- if set: use that directory for build lock, runtime slot files, and build dir

This is not a user-facing feature. It exists so `internal/testdocker` can test
coordination logic hermetically with `t.TempDir()` and subprocesses.

### Suggested API

```go
func EnsureSharedImages(label string) error
```

Behavior:

- acquires build lock
- checks Docker availability
- checks fingerprint/build stamp
- rebuilds shared images if needed
- logs build/cache state
- releases build lock before returning

This should replace the package-level "setup and keep the lock" model.

### Reference implementation shape

```go
func EnsureSharedImages(label string) error {
    lock, err := acquireBuildLock(label)
    if err != nil {
        return err
    }
    defer lock.Release()

    logBuild(label, "checking Docker availability")
    if err := requireDocker(); err != nil {
        return err
    }

    docker.SetImagePrefix(ImagePrefix)

    if err := ensureTestImagesLocked(label); err != nil {
        return err
    }

    logBuild(label, "shared image bootstrap complete")
    return nil
}
```

## B. Runtime slot limiter

### Paths

Exactly four slots:

```text
/tmp/cooper-gotest-runtime-slot-0.lock
/tmp/cooper-gotest-runtime-slot-1.lock
/tmp/cooper-gotest-runtime-slot-2.lock
/tmp/cooper-gotest-runtime-slot-3.lock
```

If `COOPER_TESTDOCKER_STATE_DIR` is set, these move under that directory.

### Contract

- at most four active runtime leases across all package processes
- one lease holds exactly one slot file
- slot wait logs must print at least once per second while blocked
- slot locks rely on kernel file-lock release on process exit

### Suggested API

```go
type RuntimeSlot struct {
    ID   int
    file *os.File
}

func AcquireRuntimeSlot(label string) (*RuntimeSlot, error)
func (s *RuntimeSlot) Release() error
```

### Reference implementation shape

```go
func AcquireRuntimeSlot(label string) (*RuntimeSlot, error) {
    start := time.Now()
    attempt := 0

    for {
        attempt++
        for i := 0; i < 4; i++ {
            path := runtimeSlotPath(i)
            f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
            if err != nil {
                return nil, fmt.Errorf("open runtime slot %s: %w", path, err)
            }
            if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
                logRuntime(label, "acquired runtime slot=%d after %s", i, time.Since(start).Round(time.Millisecond))
                return &RuntimeSlot{ID: i, file: f}, nil
            }
            _ = f.Close()
        }

        logRuntime(label, "all runtime slots busy on attempt=%d after %s", attempt, time.Since(start).Round(time.Millisecond))
        time.Sleep(1 * time.Second)
    }
}
```

## C. Runtime namespace object

The final implementation should introduce an explicit runtime value in
`internal/docker`.

### Suggested type

```go
type Runtime struct {
    Namespace          string
    StopTimeoutSeconds int
}
```

### Important note about image prefix

Do **not** add per-runtime image prefix in this refactor. Keep the shared image
prefix process-wide and fixed to `cooper-gotest-` for tests.

### Required methods

The following methods should exist on `docker.Runtime`:

```go
func (r Runtime) ProxyContainerName() string
func (r Runtime) ExternalNetworkName() string
func (r Runtime) InternalNetworkName() string
func (r Runtime) ProxyHost() string
func (r Runtime) BarrelNamePrefix() string

func (r Runtime) EnsureNetworks() error
func (r Runtime) RemoveNetworks() error
func (r Runtime) StartProxy(cfg *config.Config, cooperDir string) error
func (r Runtime) StopProxy() error
func (r Runtime) IsProxyRunning() (bool, error)
func (r Runtime) ProxyExec(cmd string) (string, error)
func (r Runtime) ReconfigureSquid() error

func (r Runtime) StartBarrel(cfg *config.Config, workspaceDir, cooperDir, toolName string) error
func (r Runtime) StopBarrel(name string) error
func (r Runtime) RestartBarrel(name string) error
func (r Runtime) ListBarrels() ([]BarrelInfo, error)
func (r Runtime) IsBarrelRunning(name string) (bool, error)

func (r Runtime) CleanupRuntime() error
func (r Runtime) ReloadSocat(cooperDir string, bridgePort int, rules []config.PortForwardRule) error
```

### Required compatibility wrappers

Production code outside the new explicit test paths can keep using wrappers over
the default runtime:

```go
var defaultRuntime = Runtime{
    Namespace:          "cooper",
    StopTimeoutSeconds: -1,
}

func SetRuntimeNamespace(ns string) { defaultRuntime.Namespace = normalizeNamespace(ns) }
func RuntimeNamespace() string      { return defaultRuntime.Namespace }
func ProxyContainerName() string    { return defaultRuntime.ProxyContainerName() }
func EnsureNetworks() error         { return defaultRuntime.EnsureNetworks() }
func CleanupRuntime() error         { return defaultRuntime.CleanupRuntime() }
```

That compatibility layer is important for incremental migration. But new test
helpers must not keep depending on it.

### Namespace format

Use a debuggable but bounded namespace:

```text
cooper-gotest-<label>-p<PID>-s<SLOT>-r<SEQ>
```

Example:

```text
cooper-gotest-app-p31415-s2-r001
```

### Namespace sanitization rules

These rules should be explicit to avoid drift:

- lowercase only
- allowed chars: `a-z`, `0-9`, `-`
- collapse repeated dashes
- trim leading/trailing dashes
- if empty after sanitization: use `runtime`
- truncate label component to **12 chars**

Reason for the 12-char cap:

- barrel names derive from namespace + workspace base + tool name
- keeping namespace short preserves headroom for long workspace names

Reference helper:

```go
func sanitizeRuntimeLabel(label string) string {
    label = strings.ToLower(label)
    label = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(label, "-")
    label = strings.Trim(label, "-")
    label = regexp.MustCompile(`-+`).ReplaceAllString(label, "-")
    if label == "" {
        label = "runtime"
    }
    if len(label) > 12 {
        label = label[:12]
        label = strings.Trim(label, "-")
        if label == "" {
            label = "runtime"
        }
    }
    return label
}
```

## D. Runtime lease

### Suggested type

```go
type RuntimeLease struct {
    Label     string
    Slot      *RuntimeSlot
    Runtime   docker.Runtime
    released  bool
}

func AcquireRuntimeLease(label string) (*RuntimeLease, error)
func (l *RuntimeLease) Release() error
func (l *RuntimeLease) CleanupAndRelease() error
```

### Contract

`CleanupAndRelease()` must:

1. attempt runtime cleanup
2. release the runtime slot even if cleanup failed
3. aggregate errors

Reference shape:

```go
func (l *RuntimeLease) CleanupAndRelease() error {
    if l == nil || l.released {
        return nil
    }

    var errs []string
    if err := l.Runtime.CleanupRuntime(); err != nil {
        errs = append(errs, fmt.Sprintf("cleanup runtime %s: %v", l.Runtime.Namespace, err))
    }
    if err := l.Release(); err != nil {
        errs = append(errs, fmt.Sprintf("release runtime slot for %s: %v", l.Runtime.Namespace, err))
    }
    if len(errs) > 0 {
        return fmt.Errorf(strings.Join(errs, "; "))
    }
    return nil
}
```

## E. Runtime-aware `CooperApp`

This is a required part of the final design. If the app itself keeps using
global Docker helpers, test isolation will remain incomplete.

### Required app change

Add a runtime field:

```go
type CooperApp struct {
    cfg       *config.Config
    cooperDir string
    runtime   docker.Runtime
    // existing fields...
}
```

### Constructor strategy

Keep production call sites simple by adding a runtime-aware constructor, not by
forcing every caller to wire options immediately.

Recommended shape:

```go
func NewCooperApp(cfg *config.Config, cooperDir string) *CooperApp {
    return NewCooperAppWithRuntime(cfg, cooperDir, docker.DefaultRuntime())
}

func NewCooperAppWithRuntime(cfg *config.Config, cooperDir string, rt docker.Runtime) *CooperApp {
    // existing init...
}
```

### Required call-site migration inside `CooperApp`

Examples of what must change:

Before:

```go
if err := docker.EnsureNetworks(); err != nil { ... }
if err := docker.StartProxy(a.cfg, a.cooperDir); err != nil { ... }
if ip, err := docker.GetGatewayIP(docker.ExternalNetworkName()); err == nil { ... }
barrels, _ := docker.ListBarrels()
if err := docker.StopBarrel(name); err != nil { ... }
docker.StopProxy()
```

After:

```go
if err := a.runtime.EnsureNetworks(); err != nil { ... }
if err := a.runtime.StartProxy(a.cfg, a.cooperDir); err != nil { ... }
if ip, err := docker.GetGatewayIP(a.runtime.ExternalNetworkName()); err == nil { ... }
barrels, _ := a.runtime.ListBarrels()
if err := a.runtime.StopBarrel(name); err != nil { ... }
_ = a.runtime.StopProxy()
```

Do not leave a mixed state where half the app uses `a.runtime` and half still
uses global Docker naming helpers.

## F. Runtime-aware test helpers and fixtures

The final test path should own runtimes explicitly.

### Suggested fixture

```go
type Fixture struct {
    Lease     *RuntimeLease
    Runtime   docker.Runtime
    CooperDir string
    Config    *config.Config
    App       *app.CooperApp
}

func NewFixture(t *testing.T, label string, mutate func(*config.Config)) *Fixture
func (f *Fixture) StartApp(t *testing.T) *app.CooperApp
func (f *Fixture) StartAppAndBarrel(t *testing.T, toolName string) (*app.CooperApp, string)
func (f *Fixture) StartHTTPSTarget(t *testing.T, domains ...string) *HTTPSTarget
func (f *Fixture) Cleanup(t *testing.T)
```

### Required behavior

`NewFixture(...)` must:

1. ensure shared images are ready
2. acquire a runtime lease
3. create a temp Cooper dir
4. assign dynamic ports
5. render config/templates
6. create a runtime-aware `CooperApp`
7. register cleanup that only touches this lease's runtime namespace

### Reference shape

```go
func NewFixture(t *testing.T, label string, mutate func(*config.Config)) *Fixture {
    t.Helper()

    if err := EnsureSharedImages("fixture:" + label); err != nil {
        t.Fatalf("ensure shared images: %v", err)
    }

    lease, err := AcquireRuntimeLease(label)
    if err != nil {
        t.Fatalf("acquire runtime lease: %v", err)
    }

    cooperDir, cfg := setupCooperDirForRuntime(t, lease.Runtime, mutate)
    fx := &Fixture{
        Lease:     lease,
        Runtime:   lease.Runtime,
        CooperDir: cooperDir,
        Config:    cfg,
        App:       app.NewCooperAppWithRuntime(cfg, cooperDir, lease.Runtime),
    }

    t.Cleanup(func() {
        fx.Cleanup(t)
    })
    return fx
}
```

### Runtime-aware HTTPS target

`internal/testdocker/target.go` must stop using global runtime helpers.

Current shape:

```go
func StartHTTPSTarget(domains ...string) (*HTTPSTarget, error)
```

Required shape:

```go
func StartHTTPSTarget(rt docker.Runtime, domains ...string) (*HTTPSTarget, error)
```

and its container name/network usage must come from `rt`, not from
`docker.RuntimeNamespace()` or `docker.ExternalNetworkName()`.

### Runtime-aware helper migration examples

Before:

```go
func cleanupDocker(t *testing.T) {
    _ = docker.CleanupRuntime()
}
```

After:

```go
func cleanupDocker(t *testing.T, rt docker.Runtime) {
    t.Helper()
    _ = rt.CleanupRuntime()
}
```

Before:

```go
cmd := exec.Command("docker", "exec", docker.ProxyContainerName(), "ps", "aux")
```

After:

```go
cmd := exec.Command("docker", "exec", rt.ProxyContainerName(), "ps", "aux")
```

## G. Concrete end-state example

This section shows the intended shape once the refactor is complete enough for
parallel parent tests.

### Final `TestMain` shape

```go
func TestMain(m *testing.M) {
    logTestMain("ensuring shared images")
    if err := testdocker.EnsureSharedImages("internal/app"); err != nil {
        fmt.Fprintf(os.Stderr, "app docker bootstrap failed: %v\n", err)
        os.Exit(1)
    }
    os.Exit(m.Run())
}
```

### Final fixture-backed parallel parent

```go
func TestCooperApp_ProxyRuntimeScenarios(t *testing.T) {
    t.Parallel()

    fx := testdocker.NewFixture(t, "proxy-suite", func(cfg *config.Config) {
        cfg.MonitorTimeoutSecs = 1
    })

    app, barrelName := fx.StartAppAndBarrel(t, "claude")
    if app == nil || barrelName == "" {
        t.Fatal("expected running app and barrel")
    }

    target := fx.StartHTTPSTarget(t, "api.anthropic.com", "example.com")
    fx.TrustTargetInProxy(t, target)

    t.Run("WhitelistedDomainPassthrough", func(t *testing.T) {
        out, err := exec.Command(
            "docker", "exec", barrelName,
            "curl", "-k", "--silent", "--show-error",
            "--connect-timeout", "2", "--max-time", "3",
            "https://api.anthropic.com",
        ).CombinedOutput()
        if err != nil {
            t.Fatalf("curl allowed target: %v\n%s", err, string(out))
        }
    })

    t.Run("BlockedDomainDenied", func(t *testing.T) {
        out, err := exec.Command(
            "docker", "exec", barrelName,
            "curl", "-k", "--silent", "--show-error",
            "--connect-timeout", "2", "--max-time", "3",
            "https://example.com",
        ).CombinedOutput()
        if err == nil {
            t.Fatalf("expected blocked target to fail, got success: %s", string(out))
        }
    })
}
```

### Final runtime-aware app construction

```go
func NewFixture(t *testing.T, label string, mutate func(*config.Config)) *Fixture {
    t.Helper()

    if err := EnsureSharedImages("fixture:" + label); err != nil {
        t.Fatalf("ensure shared images: %v", err)
    }

    lease, err := AcquireRuntimeLease(label)
    if err != nil {
        t.Fatalf("acquire runtime lease: %v", err)
    }

    cooperDir, cfg := setupCooperDirForRuntime(t, lease.Runtime, mutate)
    appInstance := app.NewCooperAppWithRuntime(cfg, cooperDir, lease.Runtime)

    fx := &Fixture{
        Lease:     lease,
        Runtime:   lease.Runtime,
        CooperDir: cooperDir,
        Config:    cfg,
        App:       appInstance,
    }
    t.Cleanup(func() { fx.Cleanup(t) })
    return fx
}
```

## Migration Strategy

This section is the required implementation order.

Do not jump straight to `t.Parallel()` first.

## Phase 1: Narrow the lock and allow package overlap

### Goal

Get immediate package-level overlap in `go test ./...` without first rewriting
every runtime-sensitive helper.

### What changes in this phase

- keep tests inside each package sequential
- keep global runtime activation inside each package process temporarily
- stop holding the build lock across `m.Run()`
- give each package process its own unique runtime namespace and one runtime slot

### Required new helper

Add a transitional helper in `internal/testdocker`, for example:

```go
type PackageRuntime struct {
    Lease *RuntimeLease
}

func AcquirePackageRuntime(label string) (*PackageRuntime, error)
func (p *PackageRuntime) ActivateGlobals()
func (p *PackageRuntime) CleanupAndRelease() error
```

`ActivateGlobals()` is explicitly transitional and should do:

```go
func (p *PackageRuntime) ActivateGlobals() {
    docker.SetImagePrefix(ImagePrefix)
    docker.SetRuntimeNamespace(p.Lease.Runtime.Namespace)
    docker.SetStopTimeoutSeconds(TestStopTimeoutSeconds)
}
```

### Required `TestMain` behavior in this phase

Example target shape:

```go
func TestMain(m *testing.M) {
    logTestMain("ensuring shared images")
    if err := testdocker.EnsureSharedImages("internal/app"); err != nil {
        fmt.Fprintf(os.Stderr, "app docker bootstrap failed: %v\n", err)
        os.Exit(1)
    }

    logTestMain("acquiring package runtime")
    pkgRuntime, err := testdocker.AcquirePackageRuntime("internal/app")
    if err != nil {
        fmt.Fprintf(os.Stderr, "app runtime lease failed: %v\n", err)
        os.Exit(1)
    }
    pkgRuntime.ActivateGlobals()

    code := m.Run()

    logTestMain("cleaning package runtime")
    if err := pkgRuntime.CleanupAndRelease(); err != nil {
        fmt.Fprintf(os.Stderr, "app runtime cleanup failed: %v\n", err)
        if code == 0 {
            code = 1
        }
    }

    os.Exit(code)
}
```

### Files to change in Phase 1

- `internal/testdocker/bootstrap.go`
- `internal/app/testmain_test.go`
- `internal/testdriver/testmain_test.go`
- `internal/docker/testmain_test.go`

### Verification for Phase 1

After this phase, even before per-test fixtures exist:

- `go test ./...` must still pass
- logs should show packages acquiring different package runtime namespaces
- packages should overlap in wall-clock time instead of waiting on one broad lock

### Why this phase exists

It cuts the biggest remaining package-level waste early, with lower risk than
the full runtime-object migration.

## Phase 2: Introduce explicit `docker.Runtime`

### Goal

Remove the hidden global runtime dependency from Docker operations.

### Files that must be migrated

At minimum, the runtime-sensitive surface includes:

- `internal/docker/runtime_names.go`
- `internal/docker/stop.go`
- `internal/docker/network.go`
- `internal/docker/proxy.go`
- `internal/docker/barrel.go`
- `internal/docker/cleanup.go`
- `internal/docker/portforward.go`
- `internal/docker/health.go`
- `internal/app/cooper.go`
- `internal/testdocker/target.go`
- `internal/app/cooper_test.go`
- `internal/testdriver/driver.go`

### Important rule

Once a helper is migrated, it must not keep falling back to global runtime
wrappers internally.

Bad:

```go
func (r Runtime) StopProxy() error {
    return stopAndRemoveContainer(ProxyContainerName())
}
```

Good:

```go
func (r Runtime) StopProxy() error {
    return stopAndRemoveContainer(r.ProxyContainerName(), r.StopTimeoutSeconds)
}
```

### Required stop-timeout migration

`stopAndRemoveContainer(...)` should stop reading the global
`stopTimeoutSeconds` in the explicit-runtime path.

Recommended shape:

```go
func stopAndRemoveContainer(name string, timeoutSeconds int) error
```

with wrappers:

```go
func StopProxy() error {
    return defaultRuntime.StopProxy()
}
```

## Phase 3: Introduce runtime-aware fixtures

### Goal

Move `internal/app` and `internal/testdriver` tests off package-global Docker
state.

### `internal/app` target

Replace direct reliance on:

- repeated `docker.SetImagePrefix(testImagePrefix)`
- `setupCooperDir(t)` without runtime context
- `cleanupDocker(t)` with global cleanup
- direct `docker.ProxyContainerName()` in assertions

with a fixture that owns:

- one runtime lease
- one temp Cooper dir
- one runtime-aware `CooperApp`

### `internal/testdriver` target

`Driver.New(...)` currently acquires the broad lock and relies on global runtime
cleanup. That must become:

- ensure shared images
- acquire a runtime lease
- construct `app.NewCooperAppWithRuntime(...)`
- cleanup only the driver's own runtime

### `internal/docker` target

The package tests should also migrate to explicit runtime values, even though
they are inside the same package. They should not keep relying on one global
`cooper-gotest` runtime namespace.

## Phase 4: Parallelize selected Docker-backed tests

### Goal

After runtime-aware fixtures exist, enable selective `t.Parallel()` with a hard
cap of 4 active runtimes globally.

### Important rule about shared-fixture parents

A parent test that owns one fixture/runtime may call `t.Parallel()`.

Its subtests must remain sequential if they share the same fixture/runtime.

Example of the correct pattern:

```go
func TestCooperApp_ProxyRuntimeScenarios(t *testing.T) {
    t.Parallel()

    fx := testdocker.NewFixture(t, "proxy-suite", func(cfg *config.Config) {
        cfg.MonitorTimeoutSecs = 1
    })

    app, barrelName := fx.StartAppAndBarrel(t, "claude")
    _ = app
    _ = barrelName

    target := fx.StartHTTPSTarget(t, "api.anthropic.com", "example.com")
    fx.TrustTargetInProxy(t, target)

    t.Run("WhitelistedDomainPassthrough", func(t *testing.T) {
        // no t.Parallel() here; shares one runtime
    })
    t.Run("BlockedDomainDenied", func(t *testing.T) {
        // no t.Parallel() here; shares one runtime
    })
}
```

### First-wave parallel candidates

These are the best initial candidates once they use the new fixture:

- start/stop tests
- bridge health and bridge route tests
- ACL flow tests
- `TestCooperApp_ProxyRuntimeScenarios`
- socat tests
- mounted-volume ownership test
- clipboard runtime tests that own isolated temp dirs and runtime state

### Tests that should remain sequential initially

At minimum, keep these sequential until further refactor:

- `TestCooperApp_CLIRuntimeScenarios`

Reason:

- it currently contains `t.Setenv("CLAUDECODE", "1")`
- it runs real CLI-token-resolution behavior
- it is a worse first candidate than the network/bridge/proxy suites

### Additional caution about host-mounted auth dirs

Barrels mount real host auth/config directories like:

- `~/.claude`
- `~/.claude.json`
- `~/.codex`
- `~/.copilot`
- `~/.config/opencode`

Those are mounted read-write today. Even if multiple tests only "read" auth,
tools may still write metadata, caches, or history files.

Therefore:

- parallelize CLI-executing tests conservatively
- prefer starting with tests that do not actually invoke the real tool CLI
- do not assume auth-dir sharing is harmless just because containers are
  isolated by runtime namespace

## Logging Requirements

Observability got called out explicitly by the user and must be preserved.

### Required log events

- build lock waiting
- build lock acquired
- shared images cache hit vs rebuild
- build lock released
- runtime slot waiting
- runtime slot acquired
- runtime namespace chosen
- runtime cleanup start
- runtime cleanup finish
- runtime slot released

### Required logging cadence

If blocked on either:

- build lock
- runtime slot
- readiness poll

then logs must print at least once per second so a stuck test is visibly stuck.

### Example log lines

```text
[cooper test bootstrap][internal/app][12:00:00] waiting for build lock /tmp/cooper-gotest-build.lock
[cooper test bootstrap][internal/app][12:00:00] acquired build lock after 12ms
[cooper test bootstrap][internal/app][12:00:00] shared Docker test images already up to date (stamp=...)
[cooper test bootstrap][internal/app][12:00:00] released build lock
[cooper test runtime][internal/app][12:00:00] waiting for runtime slot
[cooper test runtime][internal/app][12:00:01] acquired runtime slot=2 namespace=cooper-gotest-app-p31415-s2-r001
[cooper test runtime][internal/app][12:00:09] cleaning runtime namespace=cooper-gotest-app-p31415-s2-r001
[cooper test runtime][internal/app][12:00:10] released runtime slot=2 namespace=cooper-gotest-app-p31415-s2-r001
```

## Implementation Checklist By File

This section exists to reduce drift.

## `internal/testdocker/bootstrap.go`

Must gain:

- state-dir helper
- build-lock-specific helper
- runtime-slot-specific helper
- `EnsureSharedImages(...)`
- `AcquireRuntimeLease(...)`
- transitional `AcquirePackageRuntime(...)`

Must stop doing:

- broad lock acquisition for the entire package run

## `internal/app/testmain_test.go`

Phase 1:

- ensure images
- acquire package runtime
- activate globals
- run tests
- cleanup package runtime

Final state:

- ideally only ensure images in `TestMain`
- package tests themselves acquire fixtures/runtime leases

## `internal/testdriver/testmain_test.go`

Same package-level changes as `internal/app/testmain_test.go`.

## `internal/docker/testmain_test.go`

Must stop duplicating lock code.

Use common `testdocker` coordination primitives instead.

## `internal/docker/runtime_names.go`

Should become:

- normalization helpers
- `Runtime` naming methods
- default-runtime compatibility wrappers

It must stop being the only source of runtime identity for tests.

## `internal/docker/stop.go`

Must support explicit timeout input from `docker.Runtime`.

## `internal/docker/network.go`

Must expose runtime methods or runtime-aware helpers for:

- ensure networks
- remove networks
- network names

## `internal/docker/proxy.go`

Must use explicit runtime naming and explicit stop timeout in runtime methods.

## `internal/docker/barrel.go`

Must use explicit runtime naming for:

- barrel name prefix
- internal network name
- proxy host

Container cleanup and list operations must scope to the explicit runtime.

## `internal/docker/cleanup.go`

Must become runtime-scoped, not global-by-default in test paths.

## `internal/docker/portforward.go`

Must stop using global `ProxyContainerName()` and `BarrelNamePrefix()` inside
runtime-aware paths.

## `internal/docker/health.go`

Any runtime-sensitive container name lookups must become runtime-aware.

## `internal/app/cooper.go`

Must gain a runtime field and a runtime-aware constructor.

All Docker operations that depend on runtime names must go through that field.

## `internal/app/cooper_test.go`

Must migrate from ad hoc globals to fixtures in the parallelized paths.

Tests that remain sequential temporarily can still use the package runtime in
Phase 1, but the final parallelized tests should stop calling:

- `docker.SetImagePrefix(...)`
- `cleanupDocker(t)` without runtime parameter
- global `docker.ProxyContainerName()`

## `internal/testdocker/target.go`

Must accept explicit runtime.

## `internal/testdriver/driver.go`

Must stop acquiring the broad package lock directly.

It should instead:

- ensure shared images
- acquire a runtime lease
- create `CooperApp` with explicit runtime
- cleanup only its own runtime

## Verification Strategy

The next implementation session should verify in this order.

## After Phase 1

Run:

```bash
GOCACHE=/tmp/go-build-cache go test -C /home/ricky/Personal/govner/cooper ./... -count=1
```

Confirm from logs:

- package bootstrap no longer holds the build lock during `m.Run()`
- `internal/app`, `internal/docker`, and `internal/testdriver` each get a
  different runtime namespace
- package times overlap

## After explicit runtime migration

Run at minimum:

```bash
GOCACHE=/tmp/go-build-cache go test -C /home/ricky/Personal/govner/cooper ./internal/app ./internal/docker ./internal/testdriver -count=1
```

Then:

```bash
GOCACHE=/tmp/go-build-cache go test -C /home/ricky/Personal/govner/cooper ./... -count=1
```

## Live-runtime safety check

While a real `cooper up` is running, run:

```bash
GOCACHE=/tmp/go-build-cache go test -C /home/ricky/Personal/govner/cooper ./... -count=1
```

Then confirm:

- the live proxy still owns its expected host port
- the live `cooper-*` networks still exist
- no test cleanup touched the live runtime

## Parallelization check

Once selected tests are marked `t.Parallel()`:

- verify logs show at most four simultaneous acquired runtime slots
- verify no more than four test runtime namespaces are active at once
- verify package and test overlap is visible in wall-clock behavior

## Risks And Pitfalls

### Risk 1: Mixed global and explicit runtime usage

This is the biggest technical risk.

Example bad state:

- `CooperApp.Start()` uses `a.runtime.StartProxy(...)`
- but `Stop()` still calls global `docker.StopProxy()`

That will cause cleanup to hit the wrong namespace.

### Risk 2: Leaving `t.Setenv(...)` inside a parallel parent

This will either fail outright or create process-wide env races.

Do not parallelize that suite until the env dependency is removed or isolated.

### Risk 3: Over-scoping the refactor into image-prefix parallelism

That is not the bottleneck this plan is trying to solve.

Keep the shared image prefix fixed for this implementation.

### Risk 4: Slot leaks due to incomplete cleanup paths

Always release the slot even if runtime cleanup fails.

### Risk 5: Over-parallelizing CLI/auth-mount tests too early

Even with runtime namespaces, host-mounted auth dirs remain a shared host
resource.

Start with network/bridge/proxy tests first.

## Acceptance Criteria

The refactor is complete when all of the following are true:

1. `go test ./...` in `cooper/` passes.
2. It still passes while a live `cooper up` is running.
3. Shared images are still built once and reused from cache.
4. `internal/app`, `internal/docker`, and `internal/testdriver` no longer wait
   on one broad package lock for the entire package run.
5. Active Docker runtimes are capped at four across all package processes.
6. Runtime cleanup is scoped to the owning namespace only.
7. At least one meaningful set of `internal/app` Docker-backed tests safely uses
   `t.Parallel()`.
8. Waiting states remain visible in logs at least once per second.
9. No Docker-backed test runtime uses the production namespace `cooper`.

## Final Recommendation

Implement this in the following exact order:

1. split build lock from runtime capacity
2. give each package process its own package runtime lease
3. remove duplicated lock code in `internal/docker`
4. introduce explicit `docker.Runtime`
5. make `CooperApp`, `testdriver`, `HTTPSTarget`, and selected test helpers
   runtime-aware
6. parallelize only the low-risk suites first
7. keep CLI runtime tests sequential until their process-global env and
   host-auth-dir risks are addressed

Do not start by sprinkling `t.Parallel()` over the current suite. The runtime
boundary must be made explicit first.
