# Two-Phase Build/Test Architecture Plan

## Executive Summary

This document outlines the implementation of a two-phase test execution model for gowt:

1. **Build Phase**: Compile all test binaries in parallel (parallelism = CPU cores)
2. **Test Phase**: Run pre-compiled binaries alphabetically (sequential)

This separation improves performance by eliminating resource contention between compilation and test execution.

---

## Table of Contents

1. [Current Architecture Analysis](#current-architecture-analysis)
2. [Proposed Architecture](#proposed-architecture)
3. [Technical Challenges & Solutions](#technical-challenges--solutions)
4. [Implementation Plan](#implementation-plan)
5. [File Changes Summary](#file-changes-summary)
6. [Risk Mitigation](#risk-mitigation)
7. [Testing Strategy](#testing-strategy)

---

## Current Architecture Analysis

### Component Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         main.go                                  │
│  - Entry point                                                   │
│  - Creates RealTestRunner                                        │
│  - Creates App and starts BubbleTea program                      │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                          app.go                                  │
│  - BubbleTea Model (Init, Update, View)                          │
│  - Manages screens: Tree, Log, Help                              │
│  - Handles messages: TestEventMsg, TestDoneMsg, TickMsg, etc.    │
│  - Orchestrates test runs via TestRunner interface               │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                        runner.go                                 │
│  - TestRunner interface: Start(), StartSingle(), CleanCache()    │
│  - EventStream interface: Events(), Stderr(), Done(), Kill()     │
│  - RealTestRunner: executes `go test -json`                      │
│  - realEventStream: goroutines for stdout/stderr parsing         │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                      model/model.go                              │
│  - TestTree: holds all packages and test nodes                   │
│  - ProcessEvent(): updates tree based on JSON events             │
│  - Handles: run, pass, fail, skip, output, build-output, etc.    │
│  - Manages counts, status propagation, log buffers               │
└─────────────────────────────────────────────────────────────────┘
```

### Current Test Execution Flow

```
User runs: gowt ./...
     │
     ▼
main.go: NewRealTestRunner() + NewLiveApp(args, runner)
     │
     ▼
App.Init(): calls startTests() + tickCmd()
     │
     ▼
startTests(): runner.Start(args) → runs `go test -json ./...`
     │
     ▼
Go internally: compiles + runs tests INTERLEAVED
     │
     ▼
realEventStream: reads JSON events → sends to channel
     │
     ▼
App.Update(): receives TestEventMsg → tree.ProcessEvent()
     │
     ▼
TreeView renders updated tree
```

### Key Abstractions to Preserve

1. **TestRunner interface** (`runner.go:16-23`)
   - `Start(args []string) (EventStream, error)` - Run tests
   - `StartSingle(pkg, testName string) (EventStream, error)` - Run single test
   - `CleanCache() error` - Clean test cache

2. **EventStream interface** (`runner.go:25-36`)
   - `Events() <-chan model.TestEvent` - JSON test events
   - `Stderr() <-chan string` - Raw stderr output
   - `Done() <-chan TestResult` - Completion signal
   - `Kill() error` - Terminate process

3. **TestTree.ProcessEvent()** (`model/model.go:107-141`)
   - Already handles `build-output` and `build-fail` events
   - All event processing funnels through this method

---

## Proposed Architecture

### New Component: TwoPhaseRunner

Add a new orchestrator that wraps the existing runner:

```
┌─────────────────────────────────────────────────────────────────┐
│                    TwoPhaseRunner (NEW)                          │
│                                                                  │
│  Phase 1: Build                                                  │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  1. Discover packages: go list ./...                       │ │
│  │  2. Build in parallel: go test -c -o <path> ./pkg          │ │
│  │  3. Collect errors, emit BuildProgressMsg                  │ │
│  │  4. If errors: emit BuildErrorsMsg (stop here)             │ │
│  │  5. If success: emit BuildCompleteMsg                      │ │
│  └────────────────────────────────────────────────────────────┘ │
│                              │                                   │
│                              ▼                                   │
│  Phase 2: Test                                                   │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │  1. Sort packages alphabetically                           │ │
│  │  2. Run each: ./pkg.test | go tool test2json -p pkg        │ │
│  │  3. Stream events through existing EventStream interface   │ │
│  └────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
```

### State Machine

```
                    ┌──────────────┐
                    │    Init      │
                    └──────┬───────┘
                           │ startTwoPhase()
                           ▼
                    ┌──────────────┐
                    │ Discovering  │ ← go list
                    └──────┬───────┘
                           │ PackagesDiscoveredMsg
                           ▼
                    ┌──────────────┐
                    │   Building   │ ← parallel go test -c
                    │              │   (shows progress bar)
                    └──────┬───────┘
                           │
              ┌────────────┴────────────┐
              │                         │
              ▼                         ▼
    ┌──────────────────┐      ┌──────────────────┐
    │  BuildErrorView  │      │   Testing        │
    │  (browse errors) │      │   (tree view)    │
    └──────────────────┘      └────────┬─────────┘
                                       │ TestDoneMsg
                                       ▼
                              ┌──────────────────┐
                              │    Complete      │
                              └──────────────────┘
```

### Key Design Decisions

#### Decision 1: New File vs Extending runner.go

**Choice: New file `twophase.go`**

Rationale:
- `runner.go` remains unchanged (zero regression risk)
- Clear separation of concerns
- Easier to test independently
- Can be disabled/enabled via flag if needed

#### Decision 2: Build Error Display

**Choice: Reuse LogView with a "build errors" mode**

Rationale:
- LogView already handles scrolling, line wrapping, copy-to-clipboard
- Minimal new code required
- Consistent UX

#### Decision 3: Handling Pre-built Binary Execution

**Choice: Use `go tool test2json` for output conversion**

```bash
./pkg.test -test.v 2>&1 | go tool test2json -p "package/path"
```

Rationale:
- Produces identical JSON format to `go test -json`
- Existing ProcessEvent() works unchanged
- Available in all Go installations

#### Decision 4: Single Test Reruns

**Choice: Use pre-built binary if available**

Rationale:
- Faster rerun (no recompilation)
- Fall back to `go test -run` if binary missing

#### Decision 5: Flag Handling

**Choice: Preserve existing behavior - all flags passed through**

- Build phase: `go test -c` ignores test-only flags safely
- Test phase: Pass `-test.*` equivalents to binary
- Race/cover/tags affect build, handled correctly

---

## Technical Challenges & Solutions

### Challenge 1: Package Discovery

**Problem**: Need list of packages with tests before building.

**Solution**:
```go
// In twophase.go
func discoverTestPackages(patterns []string) ([]string, error) {
    // Build go list command
    args := append([]string{"list", "-f",
        "{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}"},
        patterns...)
    cmd := exec.Command("go", args...)
    output, err := cmd.Output()
    if err != nil {
        return nil, err
    }

    // Parse output (one package per line)
    var packages []string
    for _, line := range strings.Split(string(output), "\n") {
        if pkg := strings.TrimSpace(line); pkg != "" {
            packages = append(packages, pkg)
        }
    }
    sort.Strings(packages)
    return packages, nil
}
```

### Challenge 2: Parallel Build with Controlled Concurrency

**Problem**: Build all packages in parallel, but limit to CPU count.

**Solution**:
```go
type BuildResult struct {
    Package    string
    BinaryPath string
    Stderr     string  // Build errors
    Err        error
}

func (r *TwoPhaseRunner) buildPackages(ctx context.Context, packages []string) <-chan BuildResult {
    results := make(chan BuildResult)
    sem := make(chan struct{}, runtime.NumCPU())
    var wg sync.WaitGroup

    for _, pkg := range packages {
        wg.Add(1)
        go func(pkg string) {
            defer wg.Done()
            sem <- struct{}{}        // Acquire
            defer func() { <-sem }() // Release

            // Build: go test -c -o <path> <pkg>
            binaryPath := r.binaryPath(pkg)
            cmd := exec.CommandContext(ctx, "go", "test", "-c",
                "-o", binaryPath, pkg)
            stderr, err := cmd.CombinedOutput()

            results <- BuildResult{
                Package:    pkg,
                BinaryPath: binaryPath,
                Stderr:     string(stderr),
                Err:        err,
            }
        }(pkg)
    }

    go func() {
        wg.Wait()
        close(results)
    }()

    return results
}
```

### Challenge 3: Binary Output Path Management

**Problem**: Each package needs unique binary path.

**Solution**:
```go
func (r *TwoPhaseRunner) binaryPath(pkg string) string {
    // Sanitize package path for filesystem
    safe := strings.ReplaceAll(pkg, "/", "_")
    safe = strings.ReplaceAll(safe, ".", "_")
    return filepath.Join(r.tempDir, safe+".test")
}

func (r *TwoPhaseRunner) initTempDir() error {
    // Create unique temp dir per working directory
    hash := sha256.Sum256([]byte(r.workDir))
    r.tempDir = filepath.Join(os.TempDir(),
        fmt.Sprintf("gowt-%x", hash[:8]))
    return os.MkdirAll(r.tempDir, 0755)
}
```

### Challenge 4: Running Pre-built Binary with JSON Output

**Problem**: Test binaries don't have `-json` flag.

**Solution**:
```go
func (r *TwoPhaseRunner) runTest(ctx context.Context, pkg, binaryPath string) EventStream {
    // Create pipes
    testCmd := exec.CommandContext(ctx, binaryPath, "-test.v")
    testStdout, _ := testCmd.StdoutPipe()
    testStderr, _ := testCmd.StderrPipe()

    // Pipe through test2json
    jsonCmd := exec.CommandContext(ctx, "go", "tool", "test2json",
        "-p", pkg)
    jsonCmd.Stdin = io.MultiReader(testStdout, testStderr)
    jsonStdout, _ := jsonCmd.StdoutPipe()

    // Start both commands
    testCmd.Start()
    jsonCmd.Start()

    // Create EventStream from jsonStdout
    return newPipeEventStream(jsonCmd, jsonStdout, pkg)
}
```

### Challenge 5: Aggregating Events from Sequential Tests

**Problem**: Need single EventStream for App, but running tests sequentially.

**Solution**: Create aggregating EventStream:
```go
type sequentialEventStream struct {
    events   chan model.TestEvent
    stderr   chan string
    done     chan TestResult
    packages []string         // In order
    binaries map[string]string
    current  int
    ctx      context.Context
    cancel   context.CancelFunc
}

func (s *sequentialEventStream) runNext() {
    if s.current >= len(s.packages) {
        s.done <- TestResult{ExitCode: 0}
        return
    }

    pkg := s.packages[s.current]
    binary := s.binaries[pkg]

    // Run test and forward events
    stream := runPrebuiltTest(s.ctx, pkg, binary)
    go func() {
        for event := range stream.Events() {
            s.events <- event
        }
        for line := range stream.Stderr() {
            s.stderr <- line
        }
        result := <-stream.Done()
        s.current++

        if result.ExitCode != 0 {
            // Continue running other tests, track failure
        }
        s.runNext()
    }()
}
```

### Challenge 6: Build Error Display

**Problem**: When builds fail, need browsable error view.

**Solution**: Add new screen mode to App:
```go
const (
    ScreenTree Screen = iota
    ScreenLog
    ScreenHelp
    ScreenBuildErrors  // NEW
)

// In App
type App struct {
    // ... existing fields ...
    buildErrors []BuildError  // NEW: stores build failures
    buildErrorView view.BuildErrorView  // NEW: view for errors
}
```

But actually, we can reuse LogView more simply:

```go
// When build fails, create a synthetic "build errors" node
func (a *App) handleBuildErrors(errors []BuildResult) {
    // Create a package node for each failed build
    for _, err := range errors {
        if err.Err == nil {
            continue
        }
        // ProcessEvent already handles build-output and build-fail
        a.tree.ProcessEvent(model.TestEvent{
            Action:  "build-output",
            Package: err.Package,
            Output:  err.Stderr,
        })
        a.tree.ProcessEvent(model.TestEvent{
            Action:  "build-fail",
            Package: err.Package,
        })
    }
    // Tree now shows failed packages, user can navigate and view logs
}
```

**This is the minimal change approach** - no new view needed!

### Challenge 7: Progress Display During Build

**Problem**: Show build progress to user.

**Solution**: Add progress info to TreeView header:
```go
// In treeview.go - extend header rendering
func (v TreeView) renderHeader() string {
    if v.buildPhase {
        return fmt.Sprintf("Building... (%d/%d packages)",
            v.buildComplete, v.buildTotal)
    }
    // ... existing header logic
}
```

### Challenge 8: Cancellation

**Problem**: User may quit/stop during build or test phase.

**Solution**: Use context cancellation:
```go
type TwoPhaseRunner struct {
    ctx    context.Context
    cancel context.CancelFunc
}

func (r *TwoPhaseRunner) Kill() error {
    r.cancel()  // Cancels all in-progress builds and tests
    return nil
}
```

---

## Implementation Plan

### Phase 0: Preparation (No Functional Changes)

**Step 0.1**: Add test infrastructure
- Create `twophase_test.go` with test helpers
- Add mock for `go list` and `go test -c` commands
- Verify `go tool test2json` availability

### Phase 1: Package Discovery

**Step 1.1**: Create `twophase.go` with TwoPhaseRunner struct
```go
// twophase.go
package main

type TwoPhaseRunner struct {
    patterns    []string           // Package patterns (e.g., "./...")
    tempDir     string             // Directory for compiled binaries
    packages    []string           // Discovered packages
    binaries    map[string]string  // pkg -> binary path
    parallelism int                // Max concurrent builds
    ctx         context.Context
    cancel      context.CancelFunc
}

func NewTwoPhaseRunner(patterns []string) *TwoPhaseRunner {
    ctx, cancel := context.WithCancel(context.Background())
    return &TwoPhaseRunner{
        patterns:    patterns,
        parallelism: runtime.NumCPU(),
        binaries:    make(map[string]string),
        ctx:         ctx,
        cancel:      cancel,
    }
}
```

**Step 1.2**: Implement package discovery
```go
func (r *TwoPhaseRunner) DiscoverPackages() ([]string, error) {
    args := append([]string{"list", "-f",
        "{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}"},
        r.patterns...)
    cmd := exec.CommandContext(r.ctx, "go", args...)
    // ... parse output ...
}
```

**Step 1.3**: Add discovery message types to app.go
```go
type PackagesDiscoveredMsg struct {
    Packages []string
    Err      error
}
```

### Phase 2: Build Phase

**Step 2.1**: Implement parallel build orchestration
```go
type BuildProgressMsg struct {
    Package    string
    Completed  int  // Count of completed builds
    Total      int  // Total packages
    Err        error
    Stderr     string
}

type BuildCompleteMsg struct {
    Binaries map[string]string  // pkg -> binary path
    Errors   []BuildError       // Any build failures
}

type BuildError struct {
    Package string
    Stderr  string
}
```

**Step 2.2**: Implement temp directory management
```go
func (r *TwoPhaseRunner) InitTempDir() error
func (r *TwoPhaseRunner) CleanTempDir() error  // Called on explicit clean
func (r *TwoPhaseRunner) BinaryPath(pkg string) string
```

**Step 2.3**: Implement build worker pool
```go
func (r *TwoPhaseRunner) Build(packages []string) <-chan BuildProgressMsg
```

### Phase 3: Test Phase

**Step 3.1**: Implement pre-built binary execution
```go
type prebuiltEventStream struct {
    testCmd   *exec.Cmd
    jsonCmd   *exec.Cmd  // go tool test2json
    events    chan model.TestEvent
    stderr    chan string
    done      chan TestResult
}

func (r *TwoPhaseRunner) RunPrebuilt(pkg, binaryPath string) EventStream
```

**Step 3.2**: Implement sequential test orchestration
```go
type TestPhaseRunner struct {
    packages []string           // Sorted alphabetically
    binaries map[string]string  // pkg -> binary path
    current  int
}

func (r *TestPhaseRunner) Start() EventStream  // Aggregated stream
```

### Phase 4: App Integration

**Step 4.1**: Add new App states
```go
type Phase int

const (
    PhaseDiscovery Phase = iota
    PhaseBuild
    PhaseTest
    PhaseDone
)

type App struct {
    // ... existing fields ...

    // Two-phase mode (nil = legacy mode)
    twoPhase     *TwoPhaseRunner
    phase        Phase
    buildTotal   int
    buildDone    int
    buildErrors  []BuildError
}
```

**Step 4.2**: Add message handlers in Update()
```go
case PackagesDiscoveredMsg:
    // Transition to build phase
    a.buildTotal = len(msg.Packages)
    // Start parallel builds

case BuildProgressMsg:
    // Update progress counter
    a.buildDone++
    if msg.Err != nil {
        a.buildErrors = append(a.buildErrors, ...)
    }
    // Update header display

case BuildCompleteMsg:
    if len(msg.Errors) > 0 {
        // Show build errors in tree (packages with StatusFailed)
        // Allow user to browse errors
    } else {
        // Transition to test phase
        a.phase = PhaseTest
        // Start sequential test execution
    }
```

**Step 4.3**: Update TreeView header for build progress
```go
// Add to TreeView struct
type TreeView struct {
    // ... existing fields ...
    phase       Phase
    buildDone   int
    buildTotal  int
}

func (v TreeView) SetPhase(phase Phase, done, total int) TreeView
```

### Phase 5: Polish & Edge Cases

**Step 5.1**: Handle cancellation during build
- Context cancellation propagates to all build commands
- Kill() terminates process groups

**Step 5.2**: Handle rerun with two-phase
- "Rerun All" clears temp dir and restarts from discovery
- "Rerun Single Test" uses pre-built binary if available

**Step 5.3**: Handle flags properly
```go
// Parse args to separate build flags from test flags
type ParsedArgs struct {
    Patterns   []string  // ./..., ./pkg/...
    BuildFlags []string  // -race, -cover, -tags
    TestFlags  []string  // -v, -count, -run, -timeout
}

func ParseArgs(args []string) ParsedArgs
```

**Step 5.4**: Add opt-in/opt-out mechanism
```go
// Detect if two-phase should be used
// Could be controlled by:
// - Environment variable: GOWT_TWO_PHASE=1
// - Flag: gowt --two-phase ./...
// For now: make it the default, with --legacy to use old behavior
```

---

## File Changes Summary

### New Files

| File | Description | Lines (est.) |
|------|-------------|--------------|
| `twophase.go` | TwoPhaseRunner implementation | ~250 |
| `twophase_test.go` | Tests for two-phase runner | ~150 |
| `args.go` | Argument parsing for build/test flags | ~80 |

### Modified Files

| File | Changes | Risk |
|------|---------|------|
| `app.go` | Add phase state, new message handlers | Medium |
| `main.go` | Use TwoPhaseRunner instead of RealTestRunner | Low |
| `view/treeview.go` | Add build progress to header | Low |

### Unchanged Files (Preserved)

| File | Reason |
|------|--------|
| `runner.go` | Interface and RealTestRunner preserved for single-test reruns |
| `model/model.go` | ProcessEvent already handles build events |
| `view/logview.go` | Reused for build error display |
| `view/helpview.go` | No changes needed |
| `view/modal.go` | No changes needed |
| `view/icons.go` | No changes needed |

---

## Risk Mitigation

### Risk 1: Breaking Existing Functionality

**Mitigation**:
- Keep `RealTestRunner` completely unchanged
- Two-phase is additive, not replacing
- Add `--legacy` flag to use old behavior
- All existing tests continue to pass

### Risk 2: test2json Compatibility

**Mitigation**:
- Verify `go tool test2json` exists at startup
- Fall back to legacy mode if unavailable
- Test with Go 1.16+ (test2json available since 1.10)

### Risk 3: Performance Regression

**Mitigation**:
- Benchmark before/after on large codebases
- Two-phase adds overhead for small test suites
- Consider auto-detecting: use legacy for <5 packages

### Risk 4: Temp Directory Issues

**Mitigation**:
- Use OS temp directory (auto-cleaned on reboot)
- Hash working directory for unique namespace
- Add explicit cleanup command: `gowt --clean-cache`

### Risk 5: Edge Cases in Package Discovery

**Mitigation**:
- Handle empty test files (no tests to run)
- Handle build tags (`// +build integration`)
- Preserve existing argument passthrough

---

## Testing Strategy

### Unit Tests

1. **discoverTestPackages()**: Mock `go list` output
2. **buildPackages()**: Mock `go test -c`, verify parallelism
3. **runPrebuilt()**: Mock binary execution, verify JSON output
4. **ParseArgs()**: Test flag categorization

### Integration Tests

1. **Happy path**: Build and run simple test package
2. **Build failure**: Verify error display
3. **Cancellation**: Verify clean shutdown
4. **Rerun**: Verify cache behavior

### Manual Testing Checklist

- [ ] `gowt ./...` runs all tests two-phase
- [ ] Build errors display in tree, navigable
- [ ] Progress shows during build phase
- [ ] Tests run alphabetically
- [ ] Stop works during build and test phases
- [ ] Rerun works correctly
- [ ] Single test rerun uses cached binary
- [ ] `--legacy` uses old behavior
- [ ] `-race` flag works correctly
- [ ] Large codebase performance acceptable

---

## Appendix: Detailed Code Snippets

### A1: Complete TwoPhaseRunner Interface

```go
// TwoPhaseRunner orchestrates two-phase test execution:
// Phase 1: Build all test binaries in parallel
// Phase 2: Run tests sequentially, alphabetically
type TwoPhaseRunner interface {
    // DiscoverPackages finds all packages with tests
    DiscoverPackages() ([]string, error)

    // Build compiles test binaries in parallel
    // Returns channel of progress updates
    Build(packages []string) <-chan BuildProgressMsg

    // Run executes pre-built tests sequentially
    // Returns aggregated EventStream compatible with App
    Run(packages []string, binaries map[string]string) EventStream

    // Kill terminates all running operations
    Kill() error

    // CleanBinaries removes cached test binaries
    CleanBinaries() error
}
```

### A2: App Message Flow Diagram

```
Init()
  │
  ├─► startTwoPhase() ──► DiscoverPackages()
  │                              │
  │                              ▼
  │                     PackagesDiscoveredMsg
  │                              │
  ├◄─────────────────────────────┘
  │
  ├─► startBuild() ──► Build()
  │                       │
  │                       ├──► BuildProgressMsg (per package)
  │                       │         │
  ├◄──────────────────────┼─────────┘
  │                       │
  │                       └──► BuildCompleteMsg
  │                                  │
  ├◄─────────────────────────────────┘
  │
  ├─► startTestPhase() ──► Run()
  │                          │
  │                          ├──► TestEventMsg (reuses existing)
  │                          │         │
  ├◄─────────────────────────┼─────────┘
  │                          │
  │                          └──► TestDoneMsg (reuses existing)
  │                                    │
  ├◄───────────────────────────────────┘
  │
  ▼
Done
```

### A3: TreeView Header States

```go
func (v TreeView) renderHeader() string {
    switch v.phase {
    case PhaseDiscovery:
        return "Discovering packages..."
    case PhaseBuild:
        pct := float64(v.buildDone) / float64(v.buildTotal) * 100
        bar := renderProgressBar(pct, 20)
        return fmt.Sprintf("Building [%s] %d/%d packages",
            bar, v.buildDone, v.buildTotal)
    case PhaseTest:
        // Existing header logic with elapsed time and counts
        return v.renderTestHeader()
    case PhaseDone:
        return v.renderTestHeader()  // Same as test phase
    }
}

func renderProgressBar(pct float64, width int) string {
    filled := int(pct / 100 * float64(width))
    return strings.Repeat("█", filled) +
           strings.Repeat("░", width-filled)
}
```

---

## Implementation Order Summary

1. **Week 1: Foundation**
   - Create `twophase.go` with struct and interfaces
   - Implement `DiscoverPackages()`
   - Add unit tests

2. **Week 2: Build Phase**
   - Implement parallel build orchestration
   - Add temp directory management
   - Add `BuildProgressMsg` handling to App

3. **Week 3: Test Phase**
   - Implement pre-built binary execution with test2json
   - Implement sequential test orchestration
   - Integrate with existing EventStream

4. **Week 4: Integration & Polish**
   - Update TreeView header
   - Handle edge cases (cancellation, rerun, flags)
   - Add `--legacy` flag
   - Performance testing

---

## Open Questions (To Resolve Before Implementation)

1. **Default Behavior**: Should two-phase be default, or opt-in?
   - Recommendation: Make it default, add `--legacy` for old behavior

2. **Build Error Navigation**: Should we auto-expand failed packages?
   - Recommendation: Yes, auto-expand packages with build errors

3. **Parallel Test Execution**: Should we add option for parallel test execution?
   - Recommendation: Not in v1, keep sequential for predictable ordering

4. **Binary Cache Persistence**: How long to keep compiled binaries?
   - Recommendation: Keep until explicit clean or reboot (OS temp dir)
