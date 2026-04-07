# Plan: 4-Way Parallel Docker Test Execution

## Decision

Refactor Cooper's Docker-backed test infrastructure so that:

- shared Docker image bootstrap remains serialized
- runtime execution is isolated per test runtime
- at most **4 Docker-backed test runtimes** execute concurrently across all `go test` processes
- `go test ./...` no longer makes `internal/app`, `internal/docker`, and `internal/testdriver` wait for one another just because one package is still running tests

This plan is intentionally split into:

1. a **shared image bootstrap layer**
2. a **cross-process runtime-slot limiter**
3. a **per-runtime Docker namespace object**
4. test-helper migration so selected Docker-backed tests can safely use `t.Parallel()`

The end state is not “remove all coordination.” The end state is:

- one short global lock for image/bootstrap work
- one cross-process cap of 4 active Docker runtimes
- no global mutable runtime namespace shared between tests

## Why This Exists

Current `go test ./...` behavior is slower than it should be because the package-level lock is too broad.

Today:

- `internal/app`, `internal/docker`, and `internal/testdriver` each acquire the same lock
- they hold that lock for the entire `m.Run()`
- they all use the same runtime namespace: `cooper-gotest`
- package cleanup removes all runtime resources in that namespace

That design is safe, but it serializes unrelated package test execution.

Recent verified baseline:

- full suite: `real 118.78s`
- `internal/app`: `103.998s`
- `internal/docker`: `110.298s`
- `internal/testdriver`: `117.770s`

These package durations being close to wall time is evidence that package runtimes are effectively serialized or waiting on each other instead of running independently.

## Goals

- Allow up to 4 concurrent Docker-backed test runtimes across package processes.
- Keep shared images cached and reused.
- Prevent collisions with a live `cooper up`.
- Prevent one test runtime from deleting another runtime's containers/networks.
- Preserve or improve progress logging so waiting/building/running states are visible.
- Make `internal/app` Docker-backed tests eligible for gradual `t.Parallel()` adoption.

## Non-Goals

- Unlimited parallelism.
- Rewriting all tests at once.
- Changing production runtime names for non-test usage.
- Parallelizing non-Docker tests that are already cheap.
- Supporting non-Linux Docker environments as part of this refactor.

## Verified Current Constraints

### 1. Package `TestMain` currently holds the shared lock for the entire package run

Verified in:

- [internal/app/testmain_test.go](/home/ricky/Personal/govner/cooper/internal/app/testmain_test.go#L13)
- [internal/testdriver/testmain_test.go](/home/ricky/Personal/govner/cooper/internal/testdriver/testmain_test.go#L13)
- [internal/docker/testmain_test.go](/home/ricky/Personal/govner/cooper/internal/docker/testmain_test.go#L27)

Current pattern:

- acquire shared lock in `TestMain`
- run `m.Run()`
- cleanup runtime
- release lock

This is the direct cause of package-level serialization.

### 2. `testdocker.SetupPackageNamed()` uses one fixed runtime namespace for all package tests

Verified in [internal/testdocker/bootstrap.go](/home/ricky/Personal/govner/cooper/internal/testdocker/bootstrap.go#L145).

Current behavior:

- `docker.SetImagePrefix(ImagePrefix)`
- `docker.SetRuntimeNamespace(RuntimeNamespace)`
- `docker.CleanupRuntime()`

Where:

- `ImagePrefix = "cooper-gotest-"`
- `RuntimeNamespace = "cooper-gotest"`

This is safe only when one package owns the runtime at a time.

### 3. Runtime naming is currently process-global mutable state

Verified in [internal/docker/runtime_names.go](/home/ricky/Personal/govner/cooper/internal/docker/runtime_names.go#L5).

Current design:

```go
var runtimeNamespace = "cooper"

func SetRuntimeNamespace(namespace string)
func RuntimeNamespace() string
func ProxyContainerName() string
func ExternalNetworkName() string
func InternalNetworkName() string
func BarrelNamePrefix() string
```

This means concurrent tests in the same package process cannot safely use different runtime namespaces.

### 4. Runtime cleanup is namespace-wide and destructive

Verified in [internal/docker/cleanup.go](/home/ricky/Personal/govner/cooper/internal/docker/cleanup.go#L9).

`CleanupRuntime()`:

- lists all barrels in the current namespace
- stops them all
- stops proxy
- removes namespace-scoped networks

That is correct for one-owner runtime management and unsafe for parallel tests sharing a namespace.

### 5. Barrel startup removes any existing same-name container

Verified in [internal/docker/barrel.go](/home/ricky/Personal/govner/cooper/internal/docker/barrel.go#L87).

`StartBarrel()` currently does:

```go
_ = exec.Command("docker", "rm", "-f", name).Run()
```

This is only safe if container names are unique per runtime namespace.

### 6. The runtime driver also uses the shared lock directly

Verified in [internal/testdriver/driver.go](/home/ricky/Personal/govner/cooper/internal/testdriver/driver.go#L68).

`Driver.New(...)` currently calls `testdocker.AcquireLock()`, so it participates in the same broad serialization.

### 7. Dynamic host ports already exist and should remain

Verified in [internal/testdocker/bootstrap.go](/home/ricky/Personal/govner/cooper/internal/testdocker/bootstrap.go#L183).

This part is already correct:

- each test config gets dynamic `ProxyPort`
- each test config gets dynamic `BridgePort`

Port isolation does not need a redesign. It only needs to remain part of every isolated runtime fixture.

### 8. Cross-process coordination is required, not optional

Reason:

- `go test ./...` runs packages in separate processes
- a plain in-process semaphore is not enough

The concurrency cap of 4 must therefore be enforced with **cross-process** coordination.

## Required End-State Architecture

## 1. Separate image bootstrap from runtime execution

Replace the current “one lock for everything” design with two different coordination mechanisms.

### A. Shared image bootstrap lock

Purpose:

- protect shared build directory
- protect shared build stamp
- protect image rebuild/update

This remains **exclusive** and cross-process.

Suggested lock path:

```text
/tmp/cooper-gotest-build.lock
```

Scope of this lock:

- `docker info` check if desired
- fingerprint check
- rebuilding shared images if needed
- updating build stamp

It must be released before package tests begin executing.

### B. Runtime slot limiter

Purpose:

- cap active Docker-backed runtimes to 4
- allow multiple packages/tests to run at once
- prevent host overload and noisy Docker churn

Suggested slot files:

```text
/tmp/cooper-gotest-runtime-slot-0.lock
/tmp/cooper-gotest-runtime-slot-1.lock
/tmp/cooper-gotest-runtime-slot-2.lock
/tmp/cooper-gotest-runtime-slot-3.lock
```

Acquisition rule:

- try `flock(LOCK_EX|LOCK_NB)` on each slot file
- first free slot wins
- if none are free, log once per second while waiting

Release rule:

- release slot at test-runtime cleanup, not package end unless the package intentionally owns one runtime for its whole run

## 2. Introduce an explicit per-runtime Docker object

Global namespace mutation must be removed from runtime operations used by tests.

Introduce a runtime-scoped type:

```go
type Runtime struct {
    Namespace          string
    ImagePrefix        string
    StopTimeoutSeconds int
}
```

All namespace-sensitive functions should move behind methods on that type.

Examples:

```go
func (r Runtime) ProxyContainerName() string
func (r Runtime) ExternalNetworkName() string
func (r Runtime) InternalNetworkName() string
func (r Runtime) ProxyHost() string
func (r Runtime) BarrelNamePrefix() string

func (r Runtime) EnsureNetworks() error
func (r Runtime) CleanupRuntime() error
func (r Runtime) StartBarrel(cfg *config.Config, workspaceDir, cooperDir, toolName string) error
func (r Runtime) StopBarrel(name string) error
func (r Runtime) IsBarrelRunning(name string) (bool, error)
func (r Runtime) IsProxyRunning() (bool, error)
```

Production convenience wrappers can remain:

```go
var defaultRuntime = Runtime{Namespace: "cooper", ...}

func CleanupRuntime() error {
    return defaultRuntime.CleanupRuntime()
}
```

But tests must stop depending on those global wrappers for isolated concurrent runtimes.

## 3. Introduce a test runtime lease

The test layer should allocate one isolated runtime per Docker-backed test or per shared-fixture parent test.

Suggested API:

```go
type RuntimeLease struct {
    SlotID    int
    Namespace string
    Runtime   docker.Runtime
    release   func() error
}

func AcquireRuntimeLease(label string) (*RuntimeLease, error)
func (l *RuntimeLease) Release() error
```

Namespace format should be unique and debuggable:

```text
cooper-gotest-<label>-p<PID>-s<SLOT>-r<COUNTER>
```

Example:

```text
cooper-gotest-app-p31415-s2-r001
```

Requirements:

- namespace must be short enough for Docker names
- namespace must not collide across concurrent package processes
- namespace must be visible in logs

## 4. Add a runtime-aware test fixture helper

Current tests manually do:

- `docker.SetImagePrefix(...)`
- `setupCooperDir(t)`
- `startAppAndBarrel(...)`
- `cleanupDocker(t)`

This should become one runtime-aware fixture path.

Suggested API:

```go
type Fixture struct {
    Runtime   docker.Runtime
    Lease     *testdocker.RuntimeLease
    CooperDir string
    Config    *config.Config
}

func NewFixture(t *testing.T, label string, mutator func(*config.Config)) *Fixture
func (f *Fixture) StartApp(t *testing.T) *CooperApp
func (f *Fixture) StartAppAndBarrel(t *testing.T, tool string) (*CooperApp, string)
func (f *Fixture) Cleanup(t *testing.T)
```

Behavior:

- ensure shared images first without holding the runtime slot longer than needed
- acquire one runtime slot
- allocate unique namespace
- create temp cooper dir
- assign dynamic ports
- use `f.Runtime` everywhere instead of package globals
- cleanup only `f.Runtime.Namespace`

## 5. Keep image names shared; isolate only runtime resources

This part should stay shared:

- image prefix `cooper-gotest-`
- built shared images
- build stamp

This part must become per-runtime:

- proxy container name
- internal network name
- external network name
- barrel name prefix
- local HTTPS target helper names
- cleanup scope

## Test Migration Plan

## Phase 1: Narrow the lock without parallelizing tests yet

Goal:

- let `internal/app`, `internal/docker`, and `internal/testdriver` overlap during `go test ./...`

Steps:

1. Move shared image rebuild logic behind the build lock only.
2. Change package `TestMain` to:
   - ensure shared images
   - release build lock
   - create a unique package runtime namespace
   - run `m.Run()`
3. Keep package tests sequential for now.

Expected result:

- immediate wall-clock reduction
- minimal behavioral risk

## Phase 2: Eliminate global runtime namespace dependency

Goal:

- make it possible for multiple Docker-backed tests to run concurrently in the same package process

Steps:

1. Introduce `docker.Runtime`.
2. Convert namespace-sensitive helpers away from `SetRuntimeNamespace`.
3. Convert test helpers and testdriver to use explicit runtime objects.

This is the key architectural step. Without it, `t.Parallel()` in `internal/app` remains unsafe.

## Phase 3: Add cross-process 4-slot limiter

Goal:

- bound total concurrent Docker runtimes

Steps:

1. Implement slot-file lock acquisition.
2. Add logging:
   - waiting for runtime slot
   - acquired slot N
   - runtime namespace chosen
   - released slot N
3. Acquire slot per Docker-backed runtime fixture.

## Phase 4: Parallelize selected `internal/app` tests

Start with tests that:

- already use isolated temp dirs
- do not depend on package-global state
- do not intentionally inspect another test's runtime

Good initial candidates:

- start/stop tests
- bridge health/route tests
- `ProxyRuntimeScenarios`
- `CLIRuntimeScenarios`
- socat tests
- mounted-volume ownership test
- clipboard runtime tests that already create their own app/manager state

Conservative rule:

- add `t.Parallel()` only after the test uses the new fixture/runtime object

## Phase 5: Migrate `internal/docker` and `internal/testdriver`

`internal/docker`:

- remove duplicated lock code in [internal/docker/testmain_test.go](/home/ricky/Personal/govner/cooper/internal/docker/testmain_test.go#L14)
- reuse the common `testdocker` coordination primitives

`internal/testdriver`:

- replace direct `AcquireLock()` usage in [internal/testdriver/driver.go](/home/ricky/Personal/govner/cooper/internal/testdriver/driver.go#L68)
- acquire a runtime lease instead

## Logging Requirements

This refactor must improve observability, not reduce it.

Required logs:

- build lock wait/acquire/release
- runtime slot wait/acquire/release
- selected namespace per fixture
- image rebuild vs cache hit
- runtime cleanup start/finish

Example log lines:

```text
[cooper test bootstrap][internal/app][12:00:00] shared images already up to date
[cooper test runtime][internal/app][12:00:00] waiting for runtime slot
[cooper test runtime][internal/app][12:00:01] acquired runtime slot=2 namespace=cooper-gotest-app-p31415-s2-r001
[cooper test runtime][internal/app][12:00:09] released runtime slot=2 namespace=cooper-gotest-app-p31415-s2-r001
```

## Acceptance Criteria

All of the following must be true:

1. `go test ./...` passes while a live `cooper up` is running.
2. No test runtime uses the production namespace `cooper`.
3. No test runtime deletes another runtime's containers or networks.
4. Shared test images are still built once and reused from cache.
5. At most 4 Docker-backed runtimes are active at the same time.
6. `internal/app`, `internal/docker`, and `internal/testdriver` can overlap in wall-clock time.
7. Selected `internal/app` Docker-backed tests can safely use `t.Parallel()`.
8. Waiting/building/running states are visible in logs.

## Reference Implementation Sketch

### Cross-process slot acquisition

```go
func AcquireRuntimeSlot(label string) (*RuntimeSlot, error) {
    for {
        for i := 0; i < 4; i++ {
            path := fmt.Sprintf("/tmp/cooper-gotest-runtime-slot-%d.lock", i)
            f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
            if err != nil {
                return nil, err
            }
            if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
                return &RuntimeSlot{ID: i, file: f}, nil
            }
            _ = f.Close()
        }
        logf(label, "all runtime slots busy, waiting...")
        time.Sleep(1 * time.Second)
    }
}
```

### Runtime construction

```go
func NewTestRuntime(label string, slotID int) docker.Runtime {
    pid := os.Getpid()
    seq := atomic.AddUint64(&runtimeSeq, 1)
    ns := fmt.Sprintf("cooper-gotest-%s-p%d-s%d-r%03d", sanitize(label), pid, slotID, seq)
    return docker.Runtime{
        Namespace:          ns,
        ImagePrefix:        testdocker.ImagePrefix,
        StopTimeoutSeconds: testdocker.TestStopTimeoutSeconds,
    }
}
```

### Test fixture usage

```go
func TestCooperApp_ProxyRuntimeScenarios(t *testing.T) {
    t.Parallel()

    fx := testdocker.NewFixture(t, "proxy-runtime", func(cfg *config.Config) {
        cfg.MonitorTimeoutSecs = 1
    })

    app, barrelName := fx.StartAppAndBarrel(t, "claude")
    _ = app
    _ = barrelName

    target := fx.StartHTTPSTarget(t, "api.anthropic.com", "example.com")
    fx.TrustTargetInProxy(t, target)

    // assertions...
}
```

## Risks

### 1. Partial runtime refactor leaves mixed global and explicit namespace usage

This is the main risk.

If some calls still use package-global `docker.ProxyContainerName()` while others use `runtime.ProxyContainerName()`, tests will become flaky and cleanup will be unsafe.

Rule:

- once a test helper is migrated, it must use explicit runtime methods only

### 2. Cross-process slot leaks

If a crashed process leaves a slot lock held, later test runs may wait forever.

Mitigation:

- file locks released automatically on process exit
- logs should print slot wait state once per second

### 3. Testdriver/manual tools bypass fixture ownership rules

`internal/testdriver` currently manages runtime lifecycle directly.

Mitigation:

- migrate it onto the same runtime-lease abstraction before enabling broad parallelism

## Final Recommendation

Implement this in two safe milestones:

1. narrow the lock to image bootstrap and introduce unique package runtime namespaces
2. then refactor to explicit runtime objects plus a 4-slot cross-process limiter, and only then add `t.Parallel()` to selected Docker-backed tests

Do not jump straight to `t.Parallel()` while `SetRuntimeNamespace()` and `CleanupRuntime()` still operate on global mutable state.
