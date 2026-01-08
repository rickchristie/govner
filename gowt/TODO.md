# Two-Phase Build/Test Implementation TODO

> **EXECUTOR INSTRUCTIONS**: After completing any task, update this file by:
> 1. Marking the completed task with `[x]`
> 2. Adding completion notes if relevant
> 3. Updating any dependent tasks with new information discovered
> 4. If blocked, add a `BLOCKED:` note explaining why

---

## Overview

Implement a two-phase test execution model for gowt:
1. **Build Phase**: Compile all test binaries in parallel (parallelism = CPU cores)
2. **Test Phase**: Run pre-compiled binaries alphabetically (sequential)

**Reference**: See `PLAN.md` for detailed architecture diagrams and design decisions.

---

## Phase 0: Foundation & Message Types

### Task 0.1: Create Message Types
- [ ] **File**: `gowt/messages.go` (new file)
- [ ] **Scope**: Define all new message types for two-phase execution
- [ ] **Details**:
  ```go
  // PackagesDiscoveredMsg - sent after go list completes
  type PackagesDiscoveredMsg struct {
      Packages []string
      Err      error
  }

  // BuildProgressMsg - sent per-package during build
  type BuildProgressMsg struct {
      Package    string
      Completed  int
      Total      int
      Err        error
      Stderr     string
  }

  // BuildCompleteMsg - sent when all builds finish
  type BuildCompleteMsg struct {
      Binaries map[string]string  // pkg -> binary path
      Errors   []BuildError
  }

  // BuildError - represents a single build failure
  type BuildError struct {
      Package string
      Stderr  string
  }

  // Phase enum for app state
  type Phase int
  const (
      PhaseDiscovery Phase = iota
      PhaseBuild
      PhaseTest
      PhaseDone
  )
  ```
- [ ] **Tests**: None needed (type definitions only)
- [ ] **Dependencies**: None

---

## Phase 1: Package Discovery

### Task 1.1: Create TwoPhaseRunner Struct
- [ ] **File**: `gowt/twophase.go` (new file)
- [ ] **Scope**: Define TwoPhaseRunner struct and constructor
- [ ] **Details**:
  ```go
  type TwoPhaseRunner struct {
      patterns    []string           // Package patterns (e.g., "./...")
      tempDir     string             // Directory for compiled binaries
      workDir     string             // Working directory
      packages    []string           // Discovered packages
      binaries    map[string]string  // pkg -> binary path
      parallelism int                // Max concurrent builds
      buildFlags  []string           // Flags for go test -c
      testFlags   []string           // Flags for test binary
      ctx         context.Context
      cancel      context.CancelFunc
  }

  func NewTwoPhaseRunner(patterns []string) *TwoPhaseRunner
  ```
- [ ] **Key files to read first**: `runner.go` (understand existing interfaces)
- [ ] **Tests**: Basic constructor test
- [ ] **Dependencies**: Task 0.1

### Task 1.2: Implement Package Discovery
- [ ] **File**: `gowt/twophase.go`
- [ ] **Scope**: Add `DiscoverPackages()` method
- [ ] **Details**:
  - Run `go list -f "{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}" <patterns>`
  - Parse output (one package per line)
  - Sort packages alphabetically
  - Return sorted list
- [ ] **Command to use**:
  ```bash
  go list -f "{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}" ./...
  ```
- [ ] **Error handling**: Return error with stderr if command fails
- [ ] **Tests**:
  - Test with mock command execution
  - Test parsing of output with empty lines
  - Test sorting
- [ ] **Dependencies**: Task 1.1

### Task 1.3: Implement Temp Directory Management
- [ ] **File**: `gowt/twophase.go`
- [ ] **Scope**: Add temp dir methods
- [ ] **Details**:
  ```go
  func (r *TwoPhaseRunner) initTempDir() error
  func (r *TwoPhaseRunner) cleanTempDir() error
  func (r *TwoPhaseRunner) binaryPath(pkg string) string
  ```
  - Use OS temp directory with hash of working directory for uniqueness
  - Sanitize package path for filesystem (replace `/` and `.` with `_`)
  - Binary name format: `<sanitized_pkg>.test`
- [ ] **Tests**:
  - Test path sanitization
  - Test uniqueness across working directories
- [ ] **Dependencies**: Task 1.1

---

## Phase 2: Parallel Build

### Task 2.1: Implement Build Worker Pool
- [ ] **File**: `gowt/twophase.go`
- [ ] **Scope**: Add `Build()` method with parallel execution
- [ ] **Details**:
  ```go
  func (r *TwoPhaseRunner) Build(packages []string) <-chan BuildProgressMsg
  ```
  - Use semaphore pattern with `runtime.NumCPU()` workers
  - Each worker runs: `go test -c -o <binary_path> <package>`
  - Send `BuildProgressMsg` after each package completes
  - Respect context cancellation
- [ ] **Key implementation points**:
  - Use `sync.WaitGroup` to track completion
  - Use buffered channel for semaphore
  - Capture stderr for error messages
  - Continue building other packages even if one fails
- [ ] **Tests**:
  - Test parallel execution (verify concurrency limit)
  - Test cancellation behavior
  - Test error collection
- [ ] **Dependencies**: Task 1.3

### Task 2.2: Implement Build Flag Parsing
- [ ] **File**: `gowt/args.go` (new file)
- [ ] **Scope**: Parse command-line args to separate build vs test flags
- [ ] **Details**:
  ```go
  type ParsedArgs struct {
      Patterns   []string  // ./..., ./pkg/...
      BuildFlags []string  // -race, -cover, -tags, -ldflags, etc.
      TestFlags  []string  // -v, -count, -run, -timeout, etc.
  }

  func ParseArgs(args []string) ParsedArgs
  ```
  - Patterns: args that look like paths (start with `.` or `/` or no `-`)
  - Build flags: `-race`, `-cover`, `-coverprofile`, `-tags`, `-ldflags`, `-mod`, `-trimpath`
  - Test flags: `-v`, `-count`, `-run`, `-timeout`, `-parallel`, `-short`, `-bench`, `-benchtime`
- [ ] **Tests**:
  - Test various flag combinations
  - Test edge cases (flags with values, quoted args)
- [ ] **Dependencies**: None

---

## Phase 3: Test Execution

### Task 3.1: Implement Pre-built Binary Execution
- [ ] **File**: `gowt/twophase.go`
- [ ] **Scope**: Add method to run a single pre-built test binary
- [ ] **Details**:
  ```go
  func (r *TwoPhaseRunner) runPrebuilt(ctx context.Context, pkg, binaryPath string) EventStream
  ```
  - Run: `<binary> -test.v`
  - Pipe output through: `go tool test2json -p <package>`
  - Return EventStream compatible with existing App
- [ ] **Implementation approach**:
  - Create pipe between test binary stdout and test2json stdin
  - Parse JSON output same as existing `realEventStream`
  - Handle stderr separately
- [ ] **Key files to read first**: `runner.go` (see `realEventStream` implementation)
- [ ] **Tests**:
  - Test with actual compiled binary
  - Test JSON output matches expected format
- [ ] **Dependencies**: Task 2.1

### Task 3.2: Implement Sequential Test Orchestration
- [ ] **File**: `gowt/twophase.go`
- [ ] **Scope**: Add aggregating EventStream for sequential execution
- [ ] **Details**:
  ```go
  type sequentialEventStream struct {
      events   chan model.TestEvent
      stderr   chan string
      done     chan TestResult
      packages []string
      binaries map[string]string
      current  int
      ctx      context.Context
      cancel   context.CancelFunc
  }

  func (r *TwoPhaseRunner) Run(packages []string, binaries map[string]string) EventStream
  ```
  - Run tests one at a time in alphabetical order
  - Forward all events through single channel
  - Continue to next package even if current fails
  - Send final TestResult after all packages complete
- [ ] **Interface compliance**: Must implement `EventStream` interface from `runner.go`
- [ ] **Tests**:
  - Test sequential ordering
  - Test event aggregation
  - Test failure handling (continue after failure)
- [ ] **Dependencies**: Task 3.1

### Task 3.3: Implement Kill/Cancellation
- [ ] **File**: `gowt/twophase.go`
- [ ] **Scope**: Add proper cancellation support
- [ ] **Details**:
  - `Kill()` should cancel context and terminate all running processes
  - Use process groups for clean subprocess termination
  - Ensure channels are properly closed
- [ ] **Key files to read first**: `runner.go` (see existing Kill implementation)
- [ ] **Tests**:
  - Test Kill during build phase
  - Test Kill during test phase
  - Verify no zombie processes
- [ ] **Dependencies**: Task 3.2

---

## Phase 4: App Integration

### Task 4.1: Add Phase State to App
- [ ] **File**: `gowt/app.go`
- [ ] **Scope**: Add two-phase state fields to App struct
- [ ] **Details**:
  ```go
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
  - Update `NewLiveApp()` to create TwoPhaseRunner
  - Modify `Init()` to start discovery phase
- [ ] **Preserve**: All existing functionality must work
- [ ] **Tests**: None (integration testing later)
- [ ] **Dependencies**: Tasks 0.1, 1.1

### Task 4.2: Add Discovery Phase Handler
- [ ] **File**: `gowt/app.go`
- [ ] **Scope**: Handle `PackagesDiscoveredMsg` in Update()
- [ ] **Details**:
  ```go
  case PackagesDiscoveredMsg:
      if msg.Err != nil {
          // Handle discovery error
          return a, nil
      }
      a.buildTotal = len(msg.Packages)
      a.phase = PhaseBuild
      // Start parallel builds
      return a, a.startBuildPhase(msg.Packages)
  ```
  - Add `startDiscovery()` command that calls `twoPhase.DiscoverPackages()`
  - Add `startBuildPhase()` command that calls `twoPhase.Build()`
- [ ] **Tests**: None (integration testing later)
- [ ] **Dependencies**: Task 4.1

### Task 4.3: Add Build Phase Handler
- [ ] **File**: `gowt/app.go`
- [ ] **Scope**: Handle `BuildProgressMsg` and `BuildCompleteMsg` in Update()
- [ ] **Details**:
  ```go
  case BuildProgressMsg:
      a.buildDone++
      if msg.Err != nil {
          a.buildErrors = append(a.buildErrors, BuildError{
              Package: msg.Package,
              Stderr:  msg.Stderr,
          })
      }
      // Update display (TreeView will show progress)
      return a, a.waitForBuildProgress()

  case BuildCompleteMsg:
      if len(a.buildErrors) > 0 {
          // Emit build errors as events to tree
          for _, err := range a.buildErrors {
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
          a.treeView = a.treeView.SetData(a.tree)
      }

      if len(msg.Binaries) > 0 {
          a.phase = PhaseTest
          return a, a.startTestPhase(msg.Binaries)
      }
      return a, nil
  ```
- [ ] **Tests**: None (integration testing later)
- [ ] **Dependencies**: Task 4.2

### Task 4.4: Add Test Phase Handler
- [ ] **File**: `gowt/app.go`
- [ ] **Scope**: Connect test phase to existing event handling
- [ ] **Details**:
  - `startTestPhase()` should call `twoPhase.Run()` and return EventStream
  - Reuse existing `TestEventMsg`, `TestDoneMsg`, `StderrMsg` handlers
  - The sequential EventStream should be drop-in compatible
- [ ] **Key insight**: Existing handlers in Update() should work unchanged
- [ ] **Tests**: None (integration testing later)
- [ ] **Dependencies**: Task 4.3

---

## Phase 5: TreeView Updates

### Task 5.1: Add Build Progress to Header
- [ ] **File**: `gowt/view/treeview.go`
- [ ] **Scope**: Update header rendering for build phase
- [ ] **Details**:
  - Add fields to TreeView: `phase Phase`, `buildDone int`, `buildTotal int`
  - Add setter: `SetPhase(phase Phase, done, total int) TreeView`
  - Modify `renderHeader()`:
    ```go
    if v.phase == PhaseBuild {
        return v.renderBuildHeader()
    }
    // ... existing header logic

    func (v TreeView) renderBuildHeader() string {
        pct := float64(v.buildDone) / float64(v.buildTotal) * 100
        bar := v.renderProgressBar(v.buildDone, 0, 0, v.buildTotal, 20)
        return fmt.Sprintf("%s GOWT Building [%s] %d/%d packages",
            v.getStatusGear(), bar, v.buildDone, v.buildTotal)
    }
    ```
- [ ] **Key files to read first**: `view/treeview.go` (see `renderHeader()`)
- [ ] **Tests**: None (visual testing)
- [ ] **Dependencies**: Task 0.1

### Task 5.2: Update App to Set TreeView Phase
- [ ] **File**: `gowt/app.go`
- [ ] **Scope**: Call TreeView.SetPhase() at phase transitions
- [ ] **Details**:
  - In discovery handler: `a.treeView = a.treeView.SetPhase(PhaseDiscovery, 0, 0)`
  - In build progress handler: `a.treeView = a.treeView.SetPhase(PhaseBuild, a.buildDone, a.buildTotal)`
  - In test phase handler: `a.treeView = a.treeView.SetPhase(PhaseTest, 0, 0)`
  - In done handler: `a.treeView = a.treeView.SetPhase(PhaseDone, 0, 0)`
- [ ] **Tests**: None (integration testing later)
- [ ] **Dependencies**: Task 5.1

---

## Phase 6: Rerun & Single Test Support

### Task 6.1: Update Rerun to Use Two-Phase
- [ ] **File**: `gowt/app.go`
- [ ] **Scope**: Modify `startRerun()` to restart from discovery
- [ ] **Details**:
  - Kill current processes
  - Clean test cache
  - Reset two-phase runner state
  - Start from discovery phase
- [ ] **Tests**: Manual testing
- [ ] **Dependencies**: Task 4.4

### Task 6.2: Single Test Rerun with Pre-built Binary
- [ ] **File**: `gowt/app.go`
- [ ] **Scope**: Modify `startLogRerun()` to use cached binary if available
- [ ] **Details**:
  ```go
  func (a *App) startLogRerun() tea.Cmd {
      // Check if we have a pre-built binary for this package
      if binary, ok := a.twoPhase.binaries[pkg]; ok {
          // Run single test using pre-built binary
          return a.runSingleTestWithBinary(binary, testName)
      }
      // Fall back to existing behavior
      return a.startSingleTest(pkg, testName)
  }
  ```
  - Use `-test.run` flag with the binary
- [ ] **Tests**: Manual testing
- [ ] **Dependencies**: Task 6.1

---

## Phase 7: CLI & Polish

### Task 7.1: Add Legacy Mode Flag
- [ ] **File**: `gowt/main.go`
- [ ] **Scope**: Add `--legacy` flag to use old behavior
- [ ] **Details**:
  - Parse `--legacy` or `-L` flag
  - If set, use `RealTestRunner` instead of `TwoPhaseRunner`
  - Update help text
- [ ] **Tests**: None (CLI testing)
- [ ] **Dependencies**: Task 4.4

### Task 7.2: Add Cache Clean Command
- [ ] **File**: `gowt/main.go`
- [ ] **Scope**: Add `--clean-cache` flag to remove temp binaries
- [ ] **Details**:
  - Parse `--clean-cache` flag
  - Remove temp directory for all working directories
  - Exit after cleaning
  - Update help text
- [ ] **Tests**: None (CLI testing)
- [ ] **Dependencies**: Task 1.3

### Task 7.3: Verify test2json Availability
- [ ] **File**: `gowt/twophase.go`
- [ ] **Scope**: Check for `go tool test2json` at startup
- [ ] **Details**:
  ```go
  func (r *TwoPhaseRunner) checkDependencies() error {
      cmd := exec.Command("go", "tool", "test2json", "-h")
      if err := cmd.Run(); err != nil {
          return fmt.Errorf("go tool test2json not available: %w", err)
      }
      return nil
  }
  ```
  - Call in constructor or before first use
  - Return clear error message if unavailable
- [ ] **Tests**: Test with mocked command
- [ ] **Dependencies**: Task 1.1

---

## Phase 8: Testing & Documentation

### Task 8.1: Create Integration Tests
- [ ] **File**: `gowt/twophase_test.go` (new file)
- [ ] **Scope**: End-to-end tests for two-phase execution
- [ ] **Tests to write**:
  - Happy path: discover, build, run simple package
  - Build failure: verify error display
  - Cancellation during build
  - Cancellation during test
  - Rerun behavior
  - Single test rerun with cached binary
- [ ] **Dependencies**: All previous tasks

### Task 8.2: Update README
- [ ] **File**: `gowt/README.md`
- [ ] **Scope**: Document two-phase behavior
- [ ] **Details**:
  - Explain build phase progress display
  - Document `--legacy` flag
  - Document `--clean-cache` flag
  - Note performance benefits
- [ ] **Dependencies**: Task 7.1, Task 7.2

---

## Implementation Notes

### Key Interfaces to Preserve
- `TestRunner` interface in `runner.go` (lines 16-23)
- `EventStream` interface in `runner.go` (lines 25-36)
- `TestTree.ProcessEvent()` already handles `build-output` and `build-fail` events

### Files to NOT Modify
- `runner.go` - Keep unchanged for single test reruns and legacy mode
- `model/model.go` - Already handles build events
- `view/logview.go` - Reuse for build error display
- `view/helpview.go` - No changes needed
- `view/modal.go` - No changes needed
- `view/icons.go` - No changes needed

### Testing Commands
```bash
# Run gowt on itself
cd gowt && go run . ./...

# Test with race detector
cd gowt && go run . -race ./...

# Test legacy mode
cd gowt && go run . --legacy ./...

# Clean cache
cd gowt && go run . --clean-cache
```

---

## Progress Tracking

| Phase | Status | Notes |
|-------|--------|-------|
| Phase 0: Foundation | Not Started | |
| Phase 1: Discovery | Not Started | |
| Phase 2: Build | Not Started | |
| Phase 3: Test Execution | Not Started | |
| Phase 4: App Integration | Not Started | |
| Phase 5: TreeView | Not Started | |
| Phase 6: Rerun Support | Not Started | |
| Phase 7: CLI & Polish | Not Started | |
| Phase 8: Testing | Not Started | |

---

## Completion Checklist

Before marking the project complete, verify:
- [ ] All tasks marked with `[x]`
- [ ] `go build` succeeds
- [ ] `go test ./...` passes
- [ ] Manual testing with `gowt ./...` works
- [ ] Build progress displays correctly
- [ ] Test results display correctly
- [ ] Rerun works correctly
- [ ] Stop/cancellation works during build and test phases
- [ ] `--legacy` flag works
- [ ] `--clean-cache` flag works
