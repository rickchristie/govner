package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	model "github.com/rickchristie/govner/gowt/model"
	view "github.com/rickchristie/govner/gowt/view"
)

// Screen represents which screen is currently active
type Screen int

const (
	ScreenTree Screen = iota
	ScreenLog
	ScreenHelp
)

// --- Messages for async test event streaming ---

// TestEventMsg is sent when a new test event is received
type TestEventMsg struct {
	Event  model.TestEvent
	RunGen int // Generation counter to distinguish between runs
}

// TestDoneMsg is sent when all tests have completed
type TestDoneMsg struct {
	Err           error
	ExitCode      int
	RunGen        int // Generation counter to distinguish between runs
	PendingEvents []model.TestEvent
	PendingStderr []string
}

// TestStartedMsg is sent when the test command has started
type TestStartedMsg struct {
	Stream EventStream
	RunGen int
}

// TickMsg is used for elapsed time updates
type TickMsg time.Time

// StderrMsg is sent when stderr output is received
type StderrMsg struct {
	Line   string
	RunGen int // Generation counter to distinguish between runs
}

// CacheCleanedMsg is sent when go clean -testcache completes
type CacheCleanedMsg struct {
	Err    error
	RunGen int
}

// LogCacheCleanedMsg is sent when go clean -testcache completes for single test rerun
type LogCacheCleanedMsg struct {
	Err     error
	Package string // Package to run test in
	Test    string // Test name to run (for -run flag)
	RunGen  int
}

// LogsCopiedMsg reports the result of the asynchronous clipboard operation.
type LogsCopiedMsg struct{ Err error }

// App is the main TUI application model
type App struct {
	screen     Screen
	prevScreen Screen // Screen to return to when closing help
	treeView   view.TreeView
	logView    view.LogView
	helpView   view.HelpView
	tree       *model.TestTree
	width      int
	height     int
	startTime  time.Time
	running    bool
	exitCode   int
	testArgs   []string // Arguments to pass to go test

	// Test runner abstraction
	runner    TestRunner
	stream    EventStream // Current test run's event stream
	clipboard Clipboard

	// Stderr package tracking
	stderrPkg string // Current package for stderr output

	// Quit confirmation modal
	showQuitModal   bool
	quitModalChoice int // 0 = Yes, 1 = No

	// Rerun confirmation modal
	showRerunModal   bool
	rerunModalChoice int // 0 = Yes, 1 = No

	// Log rerun confirmation modal (rerun single test from log view)
	showLogRerunModal   bool
	logRerunModalChoice int             // 0 = Yes, 1 = No
	logRerunNode        *model.TestNode // The test to rerun

	// Stop confirmation modal
	showStopModal   bool
	stopModalChoice int // 0 = Yes, 1 = No

	// Run generation counter to distinguish between test runs
	runGen int

	// Operational failures are explicit modal state so a failed command can
	// never look like a successful green "Done" run.
	showErrorModal bool
	errorTitle     string
	errorMessage   string
}

// NewApp creates a new app for viewing pre-loaded results
func NewApp(tree *model.TestTree) App {
	return newApp(tree, newSystemClipboard())
}

func newApp(tree *model.TestTree, clipboard Clipboard) App {
	if clipboard == nil {
		clipboard = newSystemClipboard()
	}
	// Loaded fixtures have no execution boundary. Disable rerun affordances at
	// the view layer as well as guarding the controller so help remains truthful
	// and an invalid key sequence cannot reach a nil TestRunner.
	tv := view.NewTreeView().SetRerunEnabled(false)
	tv = tv.SetData(tree)
	hv := view.NewHelpView().SetClipboardHint(clipboard.Hint()).SetRerunEnabled(false)

	return App{
		screen:    ScreenTree,
		treeView:  tv,
		logView:   view.NewLogView().SetRerunEnabled(false),
		helpView:  hv,
		tree:      tree,
		running:   false,
		clipboard: clipboard,
	}
}

// NewLiveApp creates a new app that will run tests live
func NewLiveApp(args []string, runner TestRunner) App {
	return newLiveApp(args, runner, newSystemClipboard())
}

func newLiveApp(args []string, runner TestRunner, clipboard Clipboard) App {
	if clipboard == nil {
		clipboard = newSystemClipboard()
	}
	tree := model.NewTestTree()
	tv := view.NewTreeView()
	tv = tv.SetData(tree)
	tv = tv.SetRunning(true)

	hv := view.NewHelpView().SetClipboardHint(clipboard.Hint())
	return App{
		screen:    ScreenTree,
		treeView:  tv,
		logView:   view.NewLogView(),
		helpView:  hv,
		tree:      tree,
		running:   true,
		testArgs:  args,
		startTime: time.Now(),
		runner:    runner,
		clipboard: clipboard,
	}
}

func (a App) Init() tea.Cmd {
	if !a.running {
		return nil
	}

	// Start the test command
	return tea.Batch(
		a.startTests(),
		a.tickCmd(),
	)
}

// startTests starts the go test command
func (a *App) startTests() tea.Cmd {
	runGen := a.runGen
	return func() tea.Msg {
		stream, err := a.runner.Start(a.testArgs)
		if err != nil {
			return TestDoneMsg{Err: err, ExitCode: 1, RunGen: runGen}
		}
		return TestStartedMsg{Stream: stream, RunGen: runGen}
	}
}

// startSingleTest starts go test for a specific package and test
func (a *App) startSingleTest(pkg, testName string) tea.Cmd {
	runGen := a.runGen
	originalArgs := append([]string(nil), a.testArgs...)
	return func() tea.Msg {
		stream, err := a.runner.StartSingle(originalArgs, pkg, testName)
		if err != nil {
			return TestDoneMsg{Err: err, ExitCode: 1, RunGen: runGen}
		}
		return TestStartedMsg{Stream: stream, RunGen: runGen}
	}
}

// waitForEvents returns a command that waits for the next event
// Prioritizes events and stderr over done to avoid race conditions
func (a *App) waitForEvents() tea.Cmd {
	if a.stream == nil {
		return nil
	}

	runGen := a.runGen
	events := a.stream.Events()
	stderr := a.stream.Stderr()
	done := a.stream.Done()

	return func() tea.Msg {
		for {
			select {
			case event, ok := <-events:
				if !ok {
					events = nil
					continue
				}
				return TestEventMsg{Event: event, RunGen: runGen}
			case line, ok := <-stderr:
				if !ok {
					stderr = nil
					continue
				}
				return StderrMsg{Line: line, RunGen: runGen}
			default:
			}

			select {
			case event, ok := <-events:
				if !ok {
					events = nil
					continue
				}
				return TestEventMsg{Event: event, RunGen: runGen}
			case line, ok := <-stderr:
				if !ok {
					stderr = nil
					continue
				}
				return StderrMsg{Line: line, RunGen: runGen}
			case result, ok := <-done:
				if !ok {
					return TestDoneMsg{Err: fmt.Errorf("test stream closed without a result"), ExitCode: 1, RunGen: runGen}
				}
				msg := TestDoneMsg{Err: result.Err, ExitCode: result.ExitCode, RunGen: runGen}
				appendResultPending := func() TestDoneMsg {
					// Channel values are the delivered prefix; queue overflow is the
					// suffix. Preserve that ordering because run/output/terminal event
					// order changes model state.
					msg.PendingEvents = append(msg.PendingEvents, result.PendingEvents...)
					msg.PendingStderr = append(msg.PendingStderr, result.PendingStderr...)
					return msg
				}
				// The real stream closes both data channels before Done. Fakes and
				// alternative runners may leave them open, so drain what is pending
				// without waiting. State mutation remains in Update.
				for events != nil || stderr != nil {
					select {
					case event, open := <-events:
						if !open {
							events = nil
						} else {
							msg.PendingEvents = append(msg.PendingEvents, event)
						}
					case line, open := <-stderr:
						if !open {
							stderr = nil
						} else {
							msg.PendingStderr = append(msg.PendingStderr, line)
						}
					default:
						return appendResultPending()
					}
				}
				return appendResultPending()
			}
		}
	}
}

// tickCmd returns a command for updating elapsed time
func (a *App) tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// startRerun stops current tests, cleans cache, and restarts
func (a *App) startRerun() tea.Cmd {
	runGen := a.runGen
	return func() tea.Msg {
		var killErr error
		// Kill current test process if running
		if a.stream != nil {
			killErr = a.stream.Kill()
		}

		// Clean test cache
		err := errors.Join(killErr, a.runner.CleanCache())
		return CacheCleanedMsg{Err: err, RunGen: runGen}
	}
}

// startLogRerun stops current tests, cleans cache, and restarts with a single test
func (a *App) startLogRerun() tea.Cmd {
	node := a.logRerunNode
	if node == nil {
		return nil
	}

	pkg := node.Package
	var testName string

	// Check if this is a package node (FullPath == Package) or a test node
	if node.FullPath != node.Package {
		// Extract test name from FullPath by removing package prefix
		// FullPath format: "pkg/path/TestFoo/subtest" -> test name is "TestFoo/subtest"
		testName = strings.TrimPrefix(node.FullPath, node.Package+"/")
	}
	// If FullPath == Package, testName stays empty -> run all tests in package

	runGen := a.runGen
	return func() tea.Msg {
		var killErr error
		// Kill current test process if running
		if a.stream != nil {
			killErr = a.stream.Kill()
		}

		// Clean test cache
		err := errors.Join(killErr, a.runner.CleanCache())
		return LogCacheCleanedMsg{Err: err, Package: pkg, Test: testName, RunGen: runGen}
	}
}

func (a *App) beginRerun() tea.Cmd {
	if a.runner == nil {
		return nil
	}
	a.runGen++
	return a.startRerun()
}

func (a *App) beginLogRerun() tea.Cmd {
	if a.runner == nil {
		return nil
	}
	a.runGen++
	return a.startLogRerun()
}

func (a *App) stopCurrentRun() {
	var err error
	if a.stream != nil {
		err = a.stream.Kill()
	}
	// Invalidate commands already waiting on the killed stream before they can
	// turn a deliberate stop into a later failed completion.
	a.runGen++
	a.stream = nil
	a.running = false
	a.tree.FlushOutputBuffers()
	a.tree.Elapsed = time.Since(a.startTime).Seconds()
	a.treeView = a.treeView.SetData(a.tree).SetRunning(false).SetStopped(true)
	if err != nil {
		a.treeView = a.treeView.SetErrored(true)
		a.showOperationalError("Could not stop tests", err)
	}
}

func (a *App) showOperationalError(title string, err error) {
	if err == nil {
		return
	}
	a.showErrorModal = true
	a.errorTitle = title
	a.errorMessage = err.Error()
}

func (a *App) processTestEvent(event model.TestEvent) {
	if a.tree.ProcessEvent(event) {
		a.treeView = a.treeView.SetData(a.tree)
	}
	if a.screen != ScreenLog {
		return
	}
	node := a.logView.GetNode()
	if node == nil || !isEventRelevantToNode(event, node) {
		return
	}
	if updated := a.tree.GetNode(node.FullPath); updated != nil {
		a.logView = a.logView.UpdateContent(updated)
	}
}

func (a *App) processStderrLine(line string) {
	if strings.HasPrefix(line, "# ") {
		a.stderrPkg = strings.TrimSpace(strings.TrimPrefix(line, "# "))
	}
	pkg := a.stderrPkg
	if pkg == "" {
		pkg = "go test"
	}
	a.processTestEvent(model.TestEvent{
		Time:    time.Now(),
		Action:  "output",
		Package: pkg,
		Output:  line,
	})
}

func (a *App) copyLogs(text string) tea.Cmd {
	clipboard := a.clipboard
	return func() tea.Msg {
		return LogsCopiedMsg{Err: clipboard.Write(text)}
	}
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	// Error dialogs own keyboard input, while background events may continue to
	// keep the model internally consistent.
	if a.showErrorModal {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "enter", "esc", "q":
				a.showErrorModal = false
				return a, nil
			default:
				return a, nil
			}
		}
	}

	// Handle quit modal keyboard input (but don't block other message types)
	if a.showQuitModal {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "left", "h":
				a.quitModalChoice = 0 // Yes
				return a, nil
			case "right", "l":
				a.quitModalChoice = 1 // No
				return a, nil
			case "enter":
				if a.quitModalChoice == 0 {
					// Kill the test process and quit
					if a.stream != nil {
						a.stream.Kill()
					}
					return a, tea.Quit
				}
				// Cancel - hide modal
				a.showQuitModal = false
				return a, nil
			case "y", "Y":
				if a.stream != nil {
					a.stream.Kill()
				}
				return a, tea.Quit
			case "n", "N", "esc", "q":
				a.showQuitModal = false
				return a, nil
			}
			// Ignore other keys while modal is open
			return a, nil
		}
		// Continue processing non-keyboard messages (events, ticks, etc.)
	}

	// Handle rerun modal keyboard input (but don't block other message types)
	if a.showRerunModal {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "left", "h":
				a.rerunModalChoice = 0 // Yes
				return a, nil
			case "right", "l":
				a.rerunModalChoice = 1 // No
				return a, nil
			case "enter":
				if a.rerunModalChoice == 0 {
					// Rerun: stop current tests, clean cache, restart
					a.showRerunModal = false
					return a, a.beginRerun()
				}
				// Cancel - hide modal
				a.showRerunModal = false
				return a, nil
			case "y", "Y":
				a.showRerunModal = false
				return a, a.beginRerun()
			case "n", "N", "esc":
				a.showRerunModal = false
				return a, nil
			}
			// Ignore other keys while modal is open
			return a, nil
		}
		// Continue processing non-keyboard messages (events, ticks, etc.)
	}

	// Handle log rerun modal keyboard input (but don't block other message types)
	if a.showLogRerunModal {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "left", "h":
				a.logRerunModalChoice = 0 // Yes
				return a, nil
			case "right", "l":
				a.logRerunModalChoice = 1 // No
				return a, nil
			case "enter":
				if a.logRerunModalChoice == 0 {
					// Rerun single test: stop current tests, clean cache, restart with specific test
					a.showLogRerunModal = false
					return a, a.beginLogRerun()
				}
				// Cancel - hide modal
				a.showLogRerunModal = false
				return a, nil
			case "y", "Y":
				a.showLogRerunModal = false
				return a, a.beginLogRerun()
			case "n", "N", "esc":
				a.showLogRerunModal = false
				return a, nil
			}
			// Ignore other keys while modal is open
			return a, nil
		}
		// Continue processing non-keyboard messages (events, ticks, etc.)
	}

	// Handle stop modal keyboard input (but don't block other message types)
	if a.showStopModal {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "left", "h":
				a.stopModalChoice = 0 // Yes
				return a, nil
			case "right", "l":
				a.stopModalChoice = 1 // No
				return a, nil
			case "enter":
				if a.stopModalChoice == 0 {
					// Stop the running tests
					a.showStopModal = false
					a.stopCurrentRun()
					return a, nil
				}
				// Cancel - hide modal
				a.showStopModal = false
				return a, nil
			case "y", "Y":
				a.showStopModal = false
				a.stopCurrentRun()
				return a, nil
			case "n", "N", "esc":
				a.showStopModal = false
				return a, nil
			}
			// Ignore other keys while modal is open
			return a, nil
		}
		// Continue processing non-keyboard messages (events, ticks, etc.)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height

	case TestStartedMsg:
		if msg.RunGen != a.runGen || !a.running {
			_ = msg.Stream.Kill()
			break
		}
		// Test command started, store stream and begin waiting for events
		a.stream = msg.Stream
		cmds = append(cmds, a.waitForEvents())

	case TestEventMsg:
		// Ignore stale TestEventMsg from previous runs (e.g., after rerun)
		if msg.RunGen != a.runGen {
			break
		}
		a.processTestEvent(msg.Event)

		// Continue waiting for more events
		if a.running {
			cmds = append(cmds, a.waitForEvents())
		}

	case TestDoneMsg:
		// Ignore stale TestDoneMsg from previous runs (e.g., after rerun)
		if msg.RunGen != a.runGen {
			break
		}
		for _, event := range msg.PendingEvents {
			a.processTestEvent(event)
		}
		for _, line := range msg.PendingStderr {
			a.processStderrLine(line)
		}
		a.tree.FlushOutputBuffers()
		a.running = false
		a.stream = nil
		a.exitCode = msg.ExitCode
		if msg.Err != nil && a.exitCode == 0 {
			// A successful child-process status cannot turn a decoder, pipe, or
			// other Gowt operational failure into shell success.
			a.exitCode = 1
		}
		// Update elapsed time one final time
		a.tree.Elapsed = time.Since(a.startTime).Seconds()
		a.treeView = a.treeView.SetData(a.tree)
		a.treeView = a.treeView.SetRunning(false)
		failedRun := msg.Err != nil || msg.ExitCode != 0
		a.treeView = a.treeView.SetErrored(failedRun)
		if msg.Err != nil {
			a.showOperationalError("Test run failed", msg.Err)
		}

		// Update log view with final state if viewing it
		if a.screen == ScreenLog {
			node := a.logView.GetNode()
			if node != nil {
				// Get updated node from index (O(1) lookup)
				if updated := a.tree.GetNode(node.FullPath); updated != nil {
					// Incrementally update log content
					a.logView = a.logView.UpdateContent(updated)
				}
			}
		}

	case TickMsg:
		// Always tick the log view for copy animation
		a.logView = a.logView.Tick()

		if a.running {
			// Use SetElapsed instead of SetData to avoid invalidating the visible nodes cache.
			// The elapsed time only affects the header display, not which nodes are visible.
			// This saves significant CPU by avoiding expensive sort+flatten operations every 100ms.
			a.tree.Elapsed = time.Since(a.startTime).Seconds()
			a.treeView = a.treeView.SetElapsed(a.tree.Elapsed)
			a.treeView = a.treeView.Tick() // Advance spinner animation
			// Note: LogView content is updated via UpdateContent in TestEventMsg,
			// not here, to avoid unnecessary formatting
			cmds = append(cmds, a.tickCmd())
		} else if a.logView.IsAnimating() {
			// Continue ticking for copy animation even when tests are done
			cmds = append(cmds, a.tickCmd())
		}

	case StderrMsg:
		// Ignore stale StderrMsg from previous runs (e.g., after rerun)
		if msg.RunGen != a.runGen {
			break
		}
		a.processStderrLine(msg.Line)

		// Continue waiting for more events
		if a.running {
			cmds = append(cmds, a.waitForEvents())
		}

	case CacheCleanedMsg:
		if msg.RunGen != a.runGen {
			break
		}
		if msg.Err != nil {
			a.running = false
			a.stream = nil
			a.exitCode = 1
			a.tree.FlushOutputBuffers()
			a.treeView = a.treeView.SetRunning(false).SetErrored(true)
			a.showOperationalError("Could not rerun tests", msg.Err)
			break
		}
		// Reset and start tests
		a.stream = nil
		a.tree = model.NewTestTree()
		a.treeView = a.treeView.SetData(a.tree)
		a.treeView = a.treeView.SetRunning(true)
		a.treeView = a.treeView.SetStopped(false)
		a.treeView = a.treeView.SetErrored(false)
		a.startTime = time.Now()
		a.running = true
		a.stderrPkg = ""
		cmds = append(cmds, a.startTests(), a.tickCmd())

	case LogCacheCleanedMsg:
		if msg.RunGen != a.runGen {
			break
		}
		if msg.Err != nil {
			a.running = false
			a.stream = nil
			a.exitCode = 1
			a.tree.FlushOutputBuffers()
			a.treeView = a.treeView.SetRunning(false).SetErrored(true)
			a.showOperationalError("Could not rerun test", msg.Err)
			break
		}
		// Reset and start tests for single test
		a.stream = nil
		a.tree = model.NewTestTree()
		a.treeView = a.treeView.SetData(a.tree)
		a.treeView = a.treeView.SetRunning(true)
		a.treeView = a.treeView.SetStopped(false)
		a.treeView = a.treeView.SetErrored(false)
		a.startTime = time.Now()
		a.running = true
		a.stderrPkg = ""
		// Start tests with specific package and test name
		cmds = append(cmds, a.startSingleTest(msg.Package, msg.Test), a.tickCmd())
		// Go back to tree view to see the test running
		a.screen = ScreenTree

	case LogsCopiedMsg:
		success := msg.Err == nil
		a.logView = a.logView.TriggerCopyAnimation(success)
		if msg.Err != nil {
			a.showOperationalError("Could not copy logs", msg.Err)
		}
		if !a.running {
			cmds = append(cmds, a.tickCmd())
		}
	}

	// Handle screen-specific updates
	switch a.screen {
	case ScreenTree:
		var request view.TreeViewRequest
		a.treeView, cmd, request = a.treeView.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

		if request != nil {
			switch req := request.(type) {
			case view.SelectTestRequest:
				a.logView = a.logView.SetData(req.Node, a.tree.ProcessedLogBuffer, a.tree.RawLogBuffer)
				a.logView, _, _ = a.logView.Update(tea.WindowSizeMsg{
					Width:  a.width,
					Height: a.height,
				})
				a.screen = ScreenLog

			case view.ShowHelpRequest:
				a.prevScreen = ScreenTree
				a.helpView = a.helpView.SetSource(view.HelpSourceTree)
				a.helpView, _, _ = a.helpView.Update(tea.WindowSizeMsg{
					Width:  a.width,
					Height: a.height,
				})
				a.screen = ScreenHelp

			case view.QuitRequest:
				if a.running {
					// Show confirmation modal
					a.showQuitModal = true
					a.quitModalChoice = 1 // Default to "No"
				} else {
					return a, tea.Quit
				}

			case view.RerunAllRequest:
				if a.runner != nil {
					// Show rerun confirmation modal
					a.showRerunModal = true
					a.rerunModalChoice = 1 // Default to "No"
				}

			case view.StopRequest:
				// Show stop confirmation modal
				a.showStopModal = true
				a.stopModalChoice = 1 // Default to "No"
			}
		}

	case ScreenLog:
		var request view.LogViewRequest
		a.logView, cmd, request = a.logView.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

		if request != nil {
			switch req := request.(type) {
			case view.BackRequest:
				a.screen = ScreenTree

			case view.ShowLogHelpRequest:
				a.prevScreen = ScreenLog
				a.helpView = a.helpView.SetSource(view.HelpSourceLog)
				a.helpView, _, _ = a.helpView.Update(tea.WindowSizeMsg{
					Width:  a.width,
					Height: a.height,
				})
				a.screen = ScreenHelp

			case view.LogRerunTestRequest:
				if a.runner != nil {
					// Show log rerun confirmation modal
					a.showLogRerunModal = true
					a.logRerunModalChoice = 1 // Default to "No"
					a.logRerunNode = req.Node
				}

			case view.CopyLogsRequest:
				cmds = append(cmds, a.copyLogs(req.Logs))
			}
		}

	case ScreenHelp:
		var request view.HelpViewRequest
		a.helpView, cmd, request = a.helpView.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

		if request != nil {
			switch request.(type) {
			case view.CloseHelpRequest:
				a.screen = a.prevScreen
				// Refresh the view with current window size
				if a.prevScreen == ScreenLog {
					a.logView, _, _ = a.logView.Update(tea.WindowSizeMsg{
						Width:  a.width,
						Height: a.height,
					})
				} else if a.prevScreen == ScreenTree {
					a.treeView, _, _ = a.treeView.Update(tea.WindowSizeMsg{
						Width:  a.width,
						Height: a.height,
					})
				}
			}
		}
	}

	return a, tea.Batch(cmds...)
}

func (a App) View() string {
	var content string
	switch a.screen {
	case ScreenTree:
		content = a.treeView.View()
	case ScreenLog:
		content = a.logView.View()
	case ScreenHelp:
		content = a.helpView.View()
	default:
		content = "Unknown screen"
	}

	// Overlay quit confirmation modal if shown
	if a.showQuitModal {
		content = view.RenderConfirmModal(
			content,
			"Stop running tests?",
			a.quitModalChoice == 0, // yesSelected
			a.width,
			a.height,
		)
	}

	// Overlay rerun confirmation modal if shown
	if a.showRerunModal {
		content = view.RenderConfirmModal(
			content,
			"Rerun all tests?",
			a.rerunModalChoice == 0, // yesSelected
			a.width,
			a.height,
		)
	}

	// Overlay log rerun confirmation modal if shown
	if a.showLogRerunModal {
		content = view.RenderConfirmModal(
			content,
			"Rerun this test?",
			a.logRerunModalChoice == 0, // yesSelected
			a.width,
			a.height,
		)
	}

	// Overlay stop confirmation modal if shown
	if a.showStopModal {
		content = view.RenderConfirmModal(
			content,
			"Stop running tests?",
			a.stopModalChoice == 0, // yesSelected
			a.width,
			a.height,
		)
	}

	if a.showErrorModal {
		content = view.RenderInfoModal(
			content,
			a.errorTitle,
			a.errorMessage,
			a.width,
			a.height,
		)
	}

	return content
}

// isEventRelevantToNode checks if a test event is relevant to the node being viewed.
// An event is relevant if:
// - For a package node: all events in that package
// - For a test node: events for this exact test or its subtests
func isEventRelevantToNode(event model.TestEvent, node *model.TestNode) bool {
	// Must be same package
	eventPackage := event.Package
	if eventPackage == "" {
		eventPackage = event.ImportPath
	}
	if eventPackage != node.Package {
		return false
	}

	// Get the test name from the node's FullPath
	// FullPath format: "pkg/path/TestName/subtest" -> test name is "TestName/subtest"
	nodeTest := strings.TrimPrefix(node.FullPath, node.Package)
	nodeTest = strings.TrimPrefix(nodeTest, "/")

	// If viewing a package node (no test name), all events in this package are relevant
	if nodeTest == "" {
		return true
	}

	// Event is relevant if it's for this exact test or a subtest
	if event.Test == nodeTest {
		return true
	}
	if strings.HasPrefix(event.Test, nodeTest+"/") {
		return true
	}

	return false
}

// loadTestResults loads test events from a JSON file
func loadTestResults(path string) (*model.TestTree, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	tree := model.NewTestTree()
	decoder := json.NewDecoder(file)
	for record := 1; ; record++ {
		var event model.TestEvent
		if err := decoder.Decode(&event); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode test event %d: %w", record, err)
		}
		tree.ProcessEvent(event)
	}
	tree.FlushOutputBuffers()
	return tree, nil
}
