  Testability Analysis: gowt

  Current State

  The codebase is a well-structured Bubble Tea TUI app with clear separation:
  - main.go: App orchestration, process lifecycle
  - model/: Data structures and event processing
  - view/: UI components (TreeView, LogView, HelpView)
  - util/: Helpers (JSON formatting - well tested!)

  However, there are significant testability gaps.

  ---
  Major Testability Issues

  1. Direct External I/O in App (High Impact)

  main.go:156-189, 192-234, 335-382, 897-922

  The App struct directly calls:
  - exec.Command("go", "test", ...)
  - exec.Command("go", "clean", "-testcache")
  - Clipboard tools (wl-copy, xclip, pbcopy, etc.)

  // Current: Hard to test
  func (a *App) startTests() tea.Cmd {
      cmdArgs := append([]string{"test", "-json"}, a.testArgs...)
      a.cmd = exec.Command("go", cmdArgs...)  // ← Direct exec
      ...
  }

  Solution: Extract interfaces for dependency injection:

  // testrunner.go
  type TestRunner interface {
      Start(ctx context.Context, args []string) (EventStream, error)
  }

  type EventStream interface {
      Events() <-chan TestEventMsg
      Stderr() <-chan StderrMsg
      Done() <-chan TestDoneMsg
      Kill() error
  }

  // clipboard.go
  type ClipboardWriter interface {
      Write(text string) error
  }

  // cache.go  
  type CacheCleaner interface {
      Clean() error
  }

  Then inject via options:

  type AppConfig struct {
      Runner    TestRunner
      Clipboard ClipboardWriter
      Cache     CacheCleaner
      Clock     func() time.Time
  }

  func NewLiveApp(args []string, cfg AppConfig) App {
      if cfg.Runner == nil {
          cfg.Runner = &RealTestRunner{}
      }
      ...
  }

  ---
  2. Goroutines Coupled to App State (High Impact)

  main.go:248-284, 286-325

  The readEvents() and readStderr() methods:
  - Access a.stdout, a.stderr directly
  - Send to a.events, a.stderrCh, a.done channels
  - Are started as goroutines inline

  // Current: Untestable goroutine spawning
  go a.readEvents(runGen)
  go a.readStderr(runGen)

  Solution: Encapsulate in a testable TestProcess type:

  type TestProcess struct {
      cmd      *exec.Cmd
      events   chan TestEventMsg
      stderr   chan StderrMsg
      done     chan TestDoneMsg
      runGen   int
  }

  func (p *TestProcess) Start() error { ... }
  func (p *TestProcess) Wait() { ... }
  func (p *TestProcess) Kill() error { ... }

  // For testing: MockTestProcess that emits predefined events

  ---
  3. model.ProcessEvent Lacks Unit Tests (High Impact)

  model/model.go:104-141

  ProcessEvent is the core business logic—it handles all test events:
  - run, pass, fail, skip, output
  - build-output, build-fail
  - Count propagation, status updates

  This is pure logic (given tree + event → updated tree) and highly testable, but has no tests!

  Solution: Add comprehensive tests:

  func TestProcessEvent_RunMarksTestAsRunning(t *testing.T) {
      tree := model.NewTestTree()
      tree.ProcessEvent(model.TestEvent{
          Action:  "run",
          Package: "pkg/foo",
          Test:    "TestFoo",
      })

      node := tree.GetNode("pkg/foo/TestFoo")
      assert.Equal(t, model.StatusRunning, node.Status)
      assert.Equal(t, 1, tree.RunningCount)
  }

  func TestProcessEvent_PassUpdatesCountsAndStatus(t *testing.T) { ... }
  func TestProcessEvent_PropagatesFailureToParent(t *testing.T) { ... }
  func TestProcessEvent_CachedOutputMarksPackageCached(t *testing.T) { ... }

  ---
  4. View Components Untested (Medium Impact)

  view/treeview.go, view/logview.go, view/helpview.go

  These are Bubble Tea models that can be tested by:
  1. Sending tea.Msg and checking resulting state
  2. Verifying emitted requests

  Solution: Add unit tests for views:

  func TestTreeView_DownKeyMovesCursor(t *testing.T) {
      tree := buildTestTree(3) // 3 packages
      tv := view.NewTreeView().SetData(tree)

      tv, _, _ = tv.Update(tea.KeyMsg{Type: tea.KeyDown})
      assert.Equal(t, 1, tv.cursor)
  }

  func TestTreeView_EnterEmitsSelectRequest(t *testing.T) {
      tree := buildTestTree(1)
      tv := view.NewTreeView().SetData(tree)

      _, _, req := tv.Update(tea.KeyMsg{Type: tea.KeyEnter})
      assert.IsType(t, view.SelectTestRequest{}, req)
  }

  func TestLogView_ToggleModeSwitch(t *testing.T) {
      lv := view.NewLogView().SetData(node, procBuf, rawBuf)
      assert.Equal(t, view.LogModeProcessed, lv.viewMode)

      lv, _, _ = lv.Update(tea.KeyMsg{Type: tea.KeySpace})
      assert.Equal(t, view.LogModeRaw, lv.viewMode)
  }

  ---
  5. Time Dependencies (Medium Impact)

  main.go:77, 135, 328-332, 553

  Elapsed time uses time.Now() and time.Since() directly:

  a.startTime = time.Now()
  a.tree.Elapsed = time.Since(a.startTime).Seconds()

  Solution: Inject a clock interface:

  type Clock interface {
      Now() time.Time
  }

  type realClock struct{}
  func (realClock) Now() time.Time { return time.Now() }

  // In tests:
  type mockClock struct { t time.Time }
  func (c mockClock) Now() time.Time { return c.t }

  ---
  6. Monolithic App.Update Method (Medium Impact)

  main.go:384-750

  The Update method is ~370 lines with:
  - Modal handling (3 separate modals)
  - Message type switching
  - Event processing
  - View delegation

  Solution: Extract handlers:

  func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
      // Modal interception
      if a.showQuitModal {
          return a.handleQuitModal(msg)
      }

      switch msg := msg.(type) {
      case tea.WindowSizeMsg:
          return a.handleResize(msg)
      case TestEventMsg:
          return a.handleTestEvent(msg)
      case TestDoneMsg:
          return a.handleTestDone(msg)
      ...
      }

      return a.delegateToActiveView(msg)
  }

  ---
  7. Shared Mutable State Between App and Views (Low Impact)

  The tree *model.TestTree pointer is shared between App and TreeView:

  // main.go
  a.tree.ProcessEvent(event)      // Mutates tree
  a.treeView = a.treeView.SetData(a.tree)  // Same pointer

  // treeview.go  
  v.tree = tree  // Holds the same pointer

  This creates implicit coupling—changes in App affect views without explicit notification.

  Solution: Either:
  - Keep current approach (simpler, works fine)
  - Or use immutable updates with explicit diffs (more complex)

  ---
  Recommended Refactoring Priority

  Phase 1: Foundation (Enables testing)

  | Task                              | Files              | Impact |
  |-----------------------------------|--------------------|--------|
  | Extract TestRunner interface      | new file + main.go | High   |
  | Extract ClipboardWriter interface | new file + main.go | Medium |
  | Add DI to NewLiveApp              | main.go            | High   |
  | Create mock implementations       | new test files     | High   |

  Phase 2: Model Tests (Core logic coverage)

  | Task                          | Files                   | Impact |
  |-------------------------------|-------------------------|--------|
  | Test ProcessEvent all actions | model/model_test.go     | High   |
  | Test count propagation        | model/model_test.go     | High   |
  | Test status propagation       | model/model_test.go     | Medium |
  | Test LogBuffer operations     | model/logbuffer_test.go | Low    |

  Phase 3: View Tests (UI behavior coverage)

  | Task                     | Files                 | Impact |
  |--------------------------|-----------------------|--------|
  | Test TreeView navigation | view/treeview_test.go | Medium |
  | Test TreeView filtering  | view/treeview_test.go | Medium |
  | Test LogView mode toggle | view/logview_test.go  | Medium |
  | Test LogView search      | view/logview_test.go  | Medium |

  Phase 4: Integration (End-to-end)

  | Task                          | Files        | Impact |
  |-------------------------------|--------------|--------|
  | Add mock test output fixtures | testdata/    | Medium |
  | Test App with mock runner     | main_test.go | High   |
  | Test full event flow          | main_test.go | Medium |

  ---
  Summary

  The biggest wins would be:

  1. Extract TestRunner interface → Enables testing App logic without running real processes
  2. Add model.ProcessEvent tests → Covers the core business logic
  3. Add view component tests → Covers UI behavior

  The codebase is already well-organized—the main issue is direct I/O coupling in main.go. Once interfaces are extracted, the existing
  clean architecture makes testing straightforward.