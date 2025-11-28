package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
	Err      error
	ExitCode int
	RunGen   int // Generation counter to distinguish between runs
}

// TestStartedMsg is sent when the test command has started
type TestStartedMsg struct {
	Stream EventStream
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
	Err error
}

// LogCacheCleanedMsg is sent when go clean -testcache completes for single test rerun
type LogCacheCleanedMsg struct {
	Err     error
	Package string // Package to run test in
	Test    string // Test name to run (for -run flag)
}

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
	runner TestRunner
	stream EventStream // Current test run's event stream

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

	// Run generation counter to distinguish between test runs
	runGen int
}

// NewApp creates a new app for viewing pre-loaded results
func NewApp(tree *model.TestTree) App {
	tv := view.NewTreeView()
	tv = tv.SetData(tree)

	return App{
		screen:   ScreenTree,
		treeView: tv,
		logView:  view.NewLogView(),
		helpView: view.NewHelpView(),
		tree:     tree,
		running:  false,
	}
}

// NewLiveApp creates a new app that will run tests live
func NewLiveApp(args []string, runner TestRunner) App {
	tree := model.NewTestTree()
	tv := view.NewTreeView()
	tv = tv.SetData(tree)
	tv = tv.SetRunning(true)

	return App{
		screen:    ScreenTree,
		treeView:  tv,
		logView:   view.NewLogView(),
		helpView:  view.NewHelpView(),
		tree:      tree,
		running:   true,
		testArgs:  args,
		startTime: time.Now(),
		runner:    runner,
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
	return func() tea.Msg {
		stream, err := a.runner.Start(a.testArgs)
		if err != nil {
			return TestDoneMsg{Err: err, ExitCode: 1, RunGen: a.runGen}
		}
		return TestStartedMsg{Stream: stream}
	}
}

// startSingleTest starts go test for a specific package and test
func (a *App) startSingleTest(pkg, testName string) tea.Cmd {
	return func() tea.Msg {
		stream, err := a.runner.StartSingle(pkg, testName)
		if err != nil {
			return TestDoneMsg{Err: err, ExitCode: 1, RunGen: a.runGen}
		}
		return TestStartedMsg{Stream: stream}
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
		// First, try non-blocking reads to drain any pending events
		select {
		case event := <-events:
			return TestEventMsg{Event: event, RunGen: runGen}
		case line := <-stderr:
			return StderrMsg{Line: line, RunGen: runGen}
		default:
			// No pending events, now do a blocking wait
		}

		// Blocking wait - all channels
		select {
		case event := <-events:
			return TestEventMsg{Event: event, RunGen: runGen}
		case line := <-stderr:
			return StderrMsg{Line: line, RunGen: runGen}
		case result := <-done:
			// Before returning done, drain any remaining events
			for {
				select {
				case event := <-events:
					// Process this event directly on the tree
					// (We can only return one message)
					a.tree.ProcessEvent(event)
				case <-stderr:
					// Ignore remaining stderr after done
				default:
					// No more events, return done
					return TestDoneMsg{Err: result.Err, ExitCode: result.ExitCode, RunGen: runGen}
				}
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
	return func() tea.Msg {
		// Kill current test process if running
		if a.stream != nil {
			a.stream.Kill()
		}

		// Clean test cache
		err := a.runner.CleanCache()
		return CacheCleanedMsg{Err: err}
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

	return func() tea.Msg {
		// Kill current test process if running
		if a.stream != nil {
			a.stream.Kill()
		}

		// Clean test cache
		err := a.runner.CleanCache()
		return LogCacheCleanedMsg{Err: err, Package: pkg, Test: testName}
	}
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

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
					return a, a.startRerun()
				}
				// Cancel - hide modal
				a.showRerunModal = false
				return a, nil
			case "y", "Y":
				a.showRerunModal = false
				return a, a.startRerun()
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
					return a, a.startLogRerun()
				}
				// Cancel - hide modal
				a.showLogRerunModal = false
				return a, nil
			case "y", "Y":
				a.showLogRerunModal = false
				return a, a.startLogRerun()
			case "n", "N", "esc":
				a.showLogRerunModal = false
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
		// Test command started, store stream and begin waiting for events
		a.stream = msg.Stream
		cmds = append(cmds, a.waitForEvents())

	case TestEventMsg:
		// Ignore stale TestEventMsg from previous runs (e.g., after rerun)
		if msg.RunGen != a.runGen {
			break
		}
		// ProcessEvent returns true if tree visibility changed (status, counts, icons).
		// Skip expensive cache invalidation for log-only "output" events.
		if a.tree.ProcessEvent(msg.Event) {
			a.treeView = a.treeView.SetData(a.tree)
		}

		// Update log view if viewing it and event is relevant to the viewed node
		if a.screen == ScreenLog {
			node := a.logView.GetNode()
			if node != nil && isEventRelevantToNode(msg.Event, node) {
				// Get updated node from index (O(1) lookup)
				if updated := a.tree.GetNode(node.FullPath); updated != nil {
					// Incrementally update log content
					a.logView = a.logView.UpdateContent(updated)
				}
			}
		}

		// Continue waiting for more events
		if a.running {
			cmds = append(cmds, a.waitForEvents())
		}

	case TestDoneMsg:
		// Ignore stale TestDoneMsg from previous runs (e.g., after rerun)
		if msg.RunGen != a.runGen {
			break
		}
		a.running = false
		a.exitCode = msg.ExitCode
		// Update elapsed time one final time
		a.tree.Elapsed = time.Since(a.startTime).Seconds()
		a.treeView = a.treeView.SetData(a.tree)
		a.treeView = a.treeView.SetRunning(false)

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
		// Parse stderr output - lines starting with "# " indicate package name
		line := msg.Line
		if strings.HasPrefix(line, "# ") {
			// Extract package name (format: "# package/path")
			a.stderrPkg = strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}

		// Add stderr output to the current package as output event
		if a.stderrPkg != "" {
			event := model.TestEvent{
				Time:    time.Now(),
				Action:  "output",
				Package: a.stderrPkg,
				Output:  line,
			}
			// Stderr "output" events are log-only, ProcessEvent returns false.
			// Skip expensive cache invalidation.
			a.tree.ProcessEvent(event)
		}

		// Continue waiting for more events
		if a.running {
			cmds = append(cmds, a.waitForEvents())
		}

	case CacheCleanedMsg:
		if msg.Err != nil {
			// Cache clean failed, but we continue anyway
		}
		// Increment run generation to ignore stale messages from previous run
		a.runGen++
		// Reset and start tests
		a.tree = model.NewTestTree()
		a.treeView = a.treeView.SetData(a.tree)
		a.treeView = a.treeView.SetRunning(true)
		a.startTime = time.Now()
		a.running = true
		a.stderrPkg = ""
		cmds = append(cmds, a.startTests(), a.tickCmd())

	case LogCacheCleanedMsg:
		if msg.Err != nil {
			// Cache clean failed, but we continue anyway
		}
		// Increment run generation to ignore stale messages from previous run
		a.runGen++
		// Reset and start tests for single test
		a.tree = model.NewTestTree()
		a.treeView = a.treeView.SetData(a.tree)
		a.treeView = a.treeView.SetRunning(true)
		a.startTime = time.Now()
		a.running = true
		a.stderrPkg = ""
		// Start tests with specific package and test name
		cmds = append(cmds, a.startSingleTest(msg.Package, msg.Test), a.tickCmd())
		// Go back to tree view to see the test running
		a.screen = ScreenTree
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
				// Show rerun confirmation modal
				a.showRerunModal = true
				a.rerunModalChoice = 1 // Default to "No"
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
				// Show log rerun confirmation modal
				a.showLogRerunModal = true
				a.logRerunModalChoice = 1 // Default to "No"
				a.logRerunNode = req.Node

			case view.CopyLogsRequest:
				// Copy to clipboard and trigger animation
				if err := copyToClipboard(req.Logs); err == nil {
					a.logView = a.logView.TriggerCopyAnimation(true)
				} else {
					a.logView = a.logView.TriggerCopyAnimation(false)
				}
				// Start tick if not already running (for animation when tests are done)
				if !a.running {
					cmds = append(cmds, a.tickCmd())
				}
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

	return content
}

// isEventRelevantToNode checks if a test event is relevant to the node being viewed.
// An event is relevant if:
// - For a package node: all events in that package
// - For a test node: events for this exact test or its subtests
func isEventRelevantToNode(event model.TestEvent, node *model.TestNode) bool {
	// Must be same package
	if event.Package != node.Package {
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

// copyToClipboard copies text to the system clipboard
func copyToClipboard(text string) error {
	// Try different clipboard commands based on platform
	var cmd *exec.Cmd

	// Try wl-copy first (Wayland)
	if _, err := exec.LookPath("wl-copy"); err == nil {
		cmd = exec.Command("wl-copy")
	} else if _, err := exec.LookPath("xclip"); err == nil {
		// xclip (X11 Linux)
		cmd = exec.Command("xclip", "-selection", "clipboard")
	} else if _, err := exec.LookPath("xsel"); err == nil {
		// xsel (X11 Linux)
		cmd = exec.Command("xsel", "--clipboard", "--input")
	} else if _, err := exec.LookPath("pbcopy"); err == nil {
		// macOS
		cmd = exec.Command("pbcopy")
	} else if _, err := exec.LookPath("clip.exe"); err == nil {
		// Windows/WSL
		cmd = exec.Command("clip.exe")
	} else {
		return fmt.Errorf("no clipboard command found (install wl-copy, xclip, or xsel)")
	}

	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// loadTestResults loads test events from a JSON file
func loadTestResults(path string) (*model.TestTree, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	tree := model.NewTestTree()
	scanner := bufio.NewScanner(file)

	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		var event model.TestEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		tree.ProcessEvent(event)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	return tree, nil
}
