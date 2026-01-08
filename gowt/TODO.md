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
- [x] **File**: `gowt/messages.go` (new file)
- [x] **Scope**: Define all new message types for two-phase execution
- [x] **Completed**: Created `messages.go` with `Phase`, `PackagesDiscoveredMsg`, `BuildProgressMsg`, `BuildCompleteMsg`, `BuildError`

---

## Phase 1: Package Discovery

### Task 1.1: Create TwoPhaseRunner Struct
- [x] **File**: `gowt/twophase.go` (new file)
- [x] **Completed**: Created TwoPhaseRunner struct with all required fields and NewTwoPhaseRunner constructor

### Task 1.2: Implement Package Discovery
- [x] **File**: `gowt/twophase.go`
- [x] **Completed**: Implemented `DiscoverPackages()` using `go list` with TestGoFiles/XTestGoFiles filter

### Task 1.3: Implement Temp Directory Management
- [x] **File**: `gowt/twophase.go`
- [x] **Completed**: Implemented `initTempDir()`, `CleanTempDir()`, `binaryPath()` using SHA256 hash of working directory

---

## Phase 2: Parallel Build

### Task 2.1: Implement Build Worker Pool
- [x] **File**: `gowt/twophase.go`
- [x] **Completed**: Implemented `Build()` with semaphore-based parallelism using `runtime.NumCPU()` workers

### Task 2.2: Implement Build Flag Parsing
- [x] **File**: `gowt/args.go` (new file)
- [x] **Completed**: Created `ParseArgs()` and `ConvertToTestFlags()` to separate build vs test flags

---

## Phase 3: Test Execution

### Task 3.1: Implement Pre-built Binary Execution
- [x] **File**: `gowt/twophase.go`
- [x] **Completed**: Implemented `runSinglePackage()` piping binary output through `go tool test2json`

### Task 3.2: Implement Sequential Test Orchestration
- [x] **File**: `gowt/twophase.go`
- [x] **Completed**: Implemented `sequentialEventStream` that runs packages one at a time in alphabetical order

### Task 3.3: Implement Kill/Cancellation
- [x] **File**: `gowt/twophase.go`
- [x] **Completed**: Implemented `Kill()` with context cancellation and process group termination

---

## Phase 4: App Integration

### Task 4.1: Add Phase State to App
- [x] **File**: `gowt/app.go`
- [x] **Completed**: Added `twoPhase`, `phase`, `buildTotal`, `buildDone`, `buildErrors`, `buildChan` fields to App

### Task 4.2: Add Discovery Phase Handler
- [x] **File**: `gowt/app.go`
- [x] **Completed**: Added `PackagesDiscoveredMsg` handler that starts build phase

### Task 4.3: Add Build Phase Handler
- [x] **File**: `gowt/app.go`
- [x] **Completed**: Added `BuildProgressMsg` and `BuildCompleteMsg` handlers

### Task 4.4: Add Test Phase Handler
- [x] **File**: `gowt/app.go`
- [x] **Completed**: Added `startTestPhase()` that uses existing `TestStartedMsg` flow

---

## Phase 5: TreeView Updates

### Task 5.1: Add Build Progress to Header
- [x] **File**: `gowt/view/treeview.go`
- [x] **Completed**: Added `buildDone`, `buildTotal` fields and `renderBuildHeader()` method

### Task 5.2: Update App to Set TreeView Phase
- [x] **File**: `gowt/app.go`
- [x] **Completed**: Added `SetBuildProgress()` calls at phase transitions

---

## Phase 6: Rerun & Single Test Support

### Task 6.1: Update Rerun to Use Two-Phase
- [x] **File**: `gowt/app.go`
- [x] **Completed**: Updated `startRerun()` and `CacheCleanedMsg` handler to reset TwoPhaseRunner and restart from discovery

### Task 6.2: Single Test Rerun with Pre-built Binary
- [x] **File**: `gowt/twophase.go`
- [x] **Completed**: Implemented `RunSingleTest()` and `singleTestEventStream` for running specific tests from pre-built binaries

---

## Phase 7: CLI & Polish

### Task 7.1: Add Legacy Mode Flag
- [x] **File**: `gowt/main.go`
- [x] **Completed**: Added `--legacy` / `-L` flag that uses `RealTestRunner` instead of `TwoPhaseRunner`

### Task 7.2: Add Cache Clean Command
- [x] **File**: `gowt/main.go`
- [x] **Completed**: Added `--clean-cache` flag that removes temp directory

### Task 7.3: Verify test2json Availability
- [x] **File**: `gowt/twophase.go`
- [x] **Completed**: Added `CheckTest2JsonAvailable()` that falls back to legacy mode if unavailable

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

### Files Created/Modified
- `messages.go` - New: Phase enum and message types
- `twophase.go` - New: TwoPhaseRunner implementation
- `args.go` - New: Argument parsing for build/test flags
- `app.go` - Modified: Added two-phase state and handlers
- `view/treeview.go` - Modified: Added build progress display
- `main.go` - Modified: Added CLI flags and two-phase mode

### Files NOT Modified (as planned)
- `runner.go` - Kept unchanged for single test reruns and legacy mode
- `model/model.go` - Already handles build events
- `view/logview.go` - Reuses for build error display
- `view/helpview.go` - No changes needed
- `view/modal.go` - No changes needed
- `view/icons.go` - No changes needed

### Testing Commands
```bash
# Run gowt on itself (two-phase mode - default)
cd gowt && go run . ./...

# Test with race detector
cd gowt && go run . -race ./...

# Test legacy mode
cd gowt && go run . --legacy ./...

# Clean cache
cd gowt && go run . --clean-cache

# Show help
cd gowt && go run . --help
```

---

## Progress Tracking

| Phase | Status | Notes |
|-------|--------|-------|
| Phase 0: Foundation | Complete | messages.go created |
| Phase 1: Discovery | Complete | twophase.go created |
| Phase 2: Build | Complete | Parallel build with semaphore |
| Phase 3: Test Execution | Complete | Sequential execution via test2json |
| Phase 4: App Integration | Complete | All handlers implemented |
| Phase 5: TreeView | Complete | Build progress header added |
| Phase 6: Rerun Support | Complete | Full rerun and single test rerun |
| Phase 7: CLI & Polish | Complete | --legacy, --clean-cache flags |
| Phase 8: Testing | Pending | Integration tests not yet written |

---

## Completion Checklist

Before marking the project complete, verify:
- [x] All tasks marked with `[x]}` (except Phase 8)
- [x] `go build` succeeds
- [x] `go test ./...` passes
- [ ] Manual testing with `gowt ./...` works
- [ ] Build progress displays correctly
- [ ] Test results display correctly
- [ ] Rerun works correctly
- [ ] Stop/cancellation works during build and test phases
- [ ] `--legacy` flag works
- [ ] `--clean-cache` flag works
