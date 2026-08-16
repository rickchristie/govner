package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	model "github.com/rickchristie/govner/gowt/model"
	view "github.com/rickchristie/govner/gowt/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeEventStream struct {
	events  chan model.TestEvent
	stderr  chan string
	done    chan TestResult
	killErr error
	kills   int
}

func newFakeEventStream() *fakeEventStream {
	return &fakeEventStream{
		events: make(chan model.TestEvent, 16),
		stderr: make(chan string, 16),
		done:   make(chan TestResult, 2),
	}
}

func (s *fakeEventStream) Events() <-chan model.TestEvent { return s.events }
func (s *fakeEventStream) Stderr() <-chan string          { return s.stderr }
func (s *fakeEventStream) Done() <-chan TestResult        { return s.done }
func (s *fakeEventStream) Kill() error {
	s.kills++
	return s.killErr
}

type fakeTestRunner struct {
	stream          EventStream
	startErr        error
	startSingleErr  error
	cleanErr        error
	startArgs       []string
	startCalls      int
	startSinglePkg  string
	startSingleTest string
	startSingleArgs []string
	startSingleCall int
	cleanCalls      int
}

func (r *fakeTestRunner) Start(args []string) (EventStream, error) {
	r.startCalls++
	r.startArgs = append([]string(nil), args...)
	return r.stream, r.startErr
}

func (r *fakeTestRunner) StartSingle(args []string, pkg, testName string) (EventStream, error) {
	r.startSingleCall++
	r.startSingleArgs = append([]string(nil), args...)
	r.startSinglePkg = pkg
	r.startSingleTest = testName
	return r.stream, r.startSingleErr
}

func (r *fakeTestRunner) CleanCache() error {
	r.cleanCalls++
	return r.cleanErr
}

func appFromModel(t *testing.T, teaModel tea.Model) App {
	t.Helper()
	app, ok := teaModel.(App)
	require.True(t, ok, "model has type %T", teaModel)
	return app
}

func TestNewAppForLoadedResults(t *testing.T) {
	tree := model.NewTestTree()
	app := NewApp(tree)

	assert.Equal(t, ScreenTree, app.screen)
	assert.Same(t, tree, app.tree)
	assert.Same(t, tree, app.treeView.GetTree())
	assert.False(t, app.running)
	assert.Nil(t, app.runner)
	assert.Nil(t, app.Init())
}

func TestNewLiveApp(t *testing.T) {
	runner := &fakeTestRunner{}
	app := NewLiveApp([]string{"-race", "./..."}, runner)

	assert.Equal(t, ScreenTree, app.screen)
	assert.True(t, app.running)
	assert.Equal(t, []string{"-race", "./..."}, app.testArgs)
	assert.Same(t, runner, app.runner)
	assert.False(t, app.startTime.IsZero())
	assert.NotNil(t, app.Init())
}

func TestStartTestsSuccessAndFailure(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		stream := newFakeEventStream()
		runner := &fakeTestRunner{stream: stream}
		app := NewLiveApp([]string{"./..."}, runner)

		msg := app.startTests()()
		started, ok := msg.(TestStartedMsg)
		require.True(t, ok, "message has type %T", msg)
		assert.Same(t, stream, started.Stream)
		assert.Equal(t, []string{"./..."}, runner.startArgs)
		assert.Equal(t, 1, runner.startCalls)
	})

	t.Run("failure", func(t *testing.T) {
		startErr := errors.New("cannot start")
		runner := &fakeTestRunner{startErr: startErr}
		app := NewLiveApp(nil, runner)
		app.runGen = 4

		msg := app.startTests()()
		done, ok := msg.(TestDoneMsg)
		require.True(t, ok, "message has type %T", msg)
		assert.ErrorIs(t, done.Err, startErr)
		assert.Equal(t, 1, done.ExitCode)
		assert.Equal(t, 4, done.RunGen)
	})
}

func TestStartSingleTestSuccessAndFailure(t *testing.T) {
	stream := newFakeEventStream()
	runner := &fakeTestRunner{stream: stream}
	app := NewLiveApp([]string{"-race", "-tags=integration", "./..."}, runner)

	msg := app.startSingleTest("example.com/pkg", "TestOne/child")()
	started, ok := msg.(TestStartedMsg)
	require.True(t, ok)
	assert.Same(t, stream, started.Stream)
	assert.Equal(t, "example.com/pkg", runner.startSinglePkg)
	assert.Equal(t, "TestOne/child", runner.startSingleTest)
	assert.Equal(t, []string{"-race", "-tags=integration", "./..."}, runner.startSingleArgs)

	runner.startSingleErr = errors.New("single failed")
	app.runGen = 7
	msg = app.startSingleTest("pkg", "Test")()
	done, ok := msg.(TestDoneMsg)
	require.True(t, ok)
	assert.Equal(t, 1, done.ExitCode)
	assert.Equal(t, 7, done.RunGen)
	assert.ErrorIs(t, done.Err, runner.startSingleErr)
}

func TestWaitForEvents(t *testing.T) {
	t.Run("no stream", func(t *testing.T) {
		app := NewApp(model.NewTestTree())
		assert.Nil(t, app.waitForEvents())
	})

	t.Run("event", func(t *testing.T) {
		stream := newFakeEventStream()
		app := NewApp(model.NewTestTree())
		app.stream = stream
		app.runGen = 2
		stream.events <- model.TestEvent{Action: "run", Package: "pkg", Test: "TestOne"}

		msg := app.waitForEvents()()
		event, ok := msg.(TestEventMsg)
		require.True(t, ok, "message has type %T", msg)
		assert.Equal(t, "TestOne", event.Event.Test)
		assert.Equal(t, 2, event.RunGen)
	})

	t.Run("stderr", func(t *testing.T) {
		stream := newFakeEventStream()
		app := NewApp(model.NewTestTree())
		app.stream = stream
		app.runGen = 3
		stream.stderr <- "failure\n"

		msg := app.waitForEvents()()
		stderr, ok := msg.(StderrMsg)
		require.True(t, ok, "message has type %T", msg)
		assert.Equal(t, "failure\n", stderr.Line)
		assert.Equal(t, 3, stderr.RunGen)
	})

	t.Run("done", func(t *testing.T) {
		stream := newFakeEventStream()
		app := NewApp(model.NewTestTree())
		app.stream = stream
		app.runGen = 5
		pendingEvent := model.TestEvent{Action: "output", Package: "pkg", Output: "queued\n"}
		stream.done <- TestResult{
			ExitCode:      9,
			Err:           errors.New("done error"),
			PendingEvents: []model.TestEvent{pendingEvent},
			PendingStderr: []string{"queued stderr\n"},
		}

		msg := app.waitForEvents()()
		done, ok := msg.(TestDoneMsg)
		require.True(t, ok, "message has type %T", msg)
		assert.Equal(t, 9, done.ExitCode)
		assert.Equal(t, 5, done.RunGen)
		assert.EqualError(t, done.Err, "done error")
		assert.Equal(t, []model.TestEvent{pendingEvent}, done.PendingEvents)
		assert.Equal(t, []string{"queued stderr\n"}, done.PendingStderr)
	})

	t.Run("blocking event", func(t *testing.T) {
		stream := newFakeEventStream()
		app := NewApp(model.NewTestTree())
		app.stream = stream
		result := make(chan tea.Msg, 1)
		go func() { result <- app.waitForEvents()() }()
		stream.events <- model.TestEvent{Action: "run", Package: "pkg", Test: "TestBlocking"}
		select {
		case msg := <-result:
			event, ok := msg.(TestEventMsg)
			require.True(t, ok)
			assert.Equal(t, "TestBlocking", event.Event.Test)
		case <-time.After(time.Second):
			t.Fatal("waitForEvents did not unblock")
		}
	})

	t.Run("closed data channels before done", func(t *testing.T) {
		stream := newFakeEventStream()
		close(stream.events)
		close(stream.stderr)
		stream.done <- TestResult{ExitCode: 2}
		app := NewApp(model.NewTestTree())
		app.stream = stream

		msg := app.waitForEvents()()
		done, ok := msg.(TestDoneMsg)
		require.True(t, ok)
		assert.Equal(t, 2, done.ExitCode)
	})

	t.Run("done closes without result", func(t *testing.T) {
		stream := newFakeEventStream()
		close(stream.events)
		close(stream.stderr)
		close(stream.done)
		app := NewApp(model.NewTestTree())
		app.stream = stream

		done := app.waitForEvents()().(TestDoneMsg)
		require.Error(t, done.Err)
		assert.Contains(t, done.Err.Error(), "closed without a result")
	})
}

func TestTickCommand(t *testing.T) {
	app := NewApp(model.NewTestTree())
	start := time.Now()
	msg := app.tickCmd()()
	_, ok := msg.(TickMsg)
	assert.True(t, ok, "message has type %T", msg)
	assert.GreaterOrEqual(t, time.Since(start), 90*time.Millisecond)
}

func TestStartRerunKillsCurrentStreamAndCleansCache(t *testing.T) {
	stream := newFakeEventStream()
	cleanErr := errors.New("cache clean failed")
	runner := &fakeTestRunner{cleanErr: cleanErr}
	app := NewLiveApp(nil, runner)
	app.stream = stream

	msg := app.startRerun()()
	cleaned, ok := msg.(CacheCleanedMsg)
	require.True(t, ok)
	assert.ErrorIs(t, cleaned.Err, cleanErr)
	assert.Equal(t, 1, stream.kills)
	assert.Equal(t, 1, runner.cleanCalls)
}

func TestStartLogRerun(t *testing.T) {
	tests := []struct {
		name     string
		node     *model.TestNode
		wantPkg  string
		wantTest string
	}{
		{
			name: "package",
			node: &model.TestNode{
				Name: "pkg", FullPath: "example.com/project/pkg",
				Package: "example.com/project/pkg",
			},
			wantPkg: "example.com/project/pkg",
		},
		{
			name: "test",
			node: &model.TestNode{
				Name: "child", FullPath: "example.com/project/pkg/TestOne/child",
				Package: "example.com/project/pkg",
			},
			wantPkg: "example.com/project/pkg", wantTest: "TestOne/child",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream := newFakeEventStream()
			runner := &fakeTestRunner{}
			app := NewLiveApp(nil, runner)
			app.stream = stream
			app.logRerunNode = tt.node

			msg := app.startLogRerun()()
			cleaned, ok := msg.(LogCacheCleanedMsg)
			require.True(t, ok)
			assert.Equal(t, tt.wantPkg, cleaned.Package)
			assert.Equal(t, tt.wantTest, cleaned.Test)
			assert.Equal(t, 1, stream.kills)
			assert.Equal(t, 1, runner.cleanCalls)
		})
	}

	app := NewLiveApp(nil, &fakeTestRunner{})
	assert.Nil(t, app.startLogRerun())
}

func TestAppUpdateStoresStreamAndProcessesCurrentEvents(t *testing.T) {
	stream := newFakeEventStream()
	app := NewLiveApp(nil, &fakeTestRunner{})

	updated, cmd := app.Update(TestStartedMsg{Stream: stream})
	app = appFromModel(t, updated)
	assert.Same(t, stream, app.stream)
	assert.NotNil(t, cmd)

	updated, cmd = app.Update(TestEventMsg{
		Event: model.TestEvent{Action: "run", Package: "pkg", Test: "TestOne"},
	})
	app = appFromModel(t, updated)
	assert.NotNil(t, app.tree.GetNode("pkg/TestOne"))
	assert.NotNil(t, cmd, "a live app keeps waiting for events")

	updated, _ = app.Update(TestEventMsg{
		Event:  model.TestEvent{Action: "run", Package: "pkg", Test: "TestStale"},
		RunGen: 99,
	})
	app = appFromModel(t, updated)
	assert.Nil(t, app.tree.GetNode("pkg/TestStale"))
}

func TestAppRejectsStaleStartedStream(t *testing.T) {
	current := newFakeEventStream()
	stale := newFakeEventStream()
	app := NewLiveApp(nil, &fakeTestRunner{})
	app.runGen = 3
	app.stream = current

	updated, cmd := app.Update(TestStartedMsg{Stream: stale, RunGen: 2})
	app = appFromModel(t, updated)
	assert.Same(t, current, app.stream)
	assert.Equal(t, 1, stale.kills, "the stale process must not be leaked")
	assert.Nil(t, cmd)

	stoppedStart := newFakeEventStream()
	app.running = false
	updated, cmd = app.Update(TestStartedMsg{Stream: stoppedStart, RunGen: 3})
	app = appFromModel(t, updated)
	assert.Equal(t, 1, stoppedStart.kills, "a start completing after Stop must be terminated")
	assert.Same(t, current, app.stream)
	assert.Nil(t, cmd)
}

func TestAppUpdateWindowSizeAndLiveLogContent(t *testing.T) {
	app := NewLiveApp(nil, &fakeTestRunner{})
	app.tree.ProcessEvent(model.TestEvent{
		Action: "run", Package: "pkg", Test: "TestOne",
	})
	app.tree.ProcessEvent(model.TestEvent{
		Action: "output", Package: "pkg", Test: "TestOne", Output: "initial output\n",
	})
	node := app.tree.GetNode("pkg/TestOne")
	app.screen = ScreenLog
	app.logView = view.NewLogView().SetData(
		node, app.tree.ProcessedLogBuffer, app.tree.RawLogBuffer,
	)

	updated, _ := app.Update(tea.WindowSizeMsg{Width: 90, Height: 25})
	app = appFromModel(t, updated)
	assert.Equal(t, 90, app.width)
	assert.Equal(t, 25, app.height)

	updated, _ = app.Update(TestEventMsg{Event: model.TestEvent{
		Action: "output", Package: "pkg", Test: "TestOne", Output: "live output\n",
	}})
	app = appFromModel(t, updated)
	assert.Contains(t, app.View(), "live output")

	updated, _ = app.Update(TestDoneMsg{ExitCode: 0, RunGen: 0})
	app = appFromModel(t, updated)
	assert.False(t, app.running)
	assert.Contains(t, app.View(), "live output")
}

func TestAppUpdateCompletesCurrentRunAndIgnoresStaleCompletion(t *testing.T) {
	app := NewLiveApp(nil, &fakeTestRunner{})
	app.startTime = time.Now().Add(-time.Second)

	updated, _ := app.Update(TestDoneMsg{ExitCode: 3, RunGen: 99})
	stale := appFromModel(t, updated)
	assert.True(t, stale.running)

	updated, _ = app.Update(TestDoneMsg{ExitCode: 3, RunGen: 0})
	app = appFromModel(t, updated)
	assert.False(t, app.running)
	assert.Equal(t, 3, app.exitCode)
	assert.GreaterOrEqual(t, app.tree.Elapsed, 0.9)
	assert.Contains(t, app.View(), "Failed")
}

func TestAppCompletionProcessesPendingDataAndSurfacesOperationalErrors(t *testing.T) {
	app := NewLiveApp(nil, &fakeTestRunner{})
	app.width, app.height = 100, 24
	runErr := errors.New("JSON stream failed")
	updated, _ := app.Update(TestDoneMsg{
		Err: runErr, ExitCode: 1, RunGen: 0,
		PendingEvents: []model.TestEvent{
			{Action: "run", Package: "pkg", Test: "TestFinal"},
			{Action: "output", Package: "pkg", Test: "TestFinal", Output: "last diagnostic"},
			{Action: "fail", Package: "pkg", Test: "TestFinal"},
		},
		PendingStderr: []string{"startup warning without package\n"},
	})
	app = appFromModel(t, updated)

	node := app.tree.GetNode("pkg/TestFinal")
	require.NotNil(t, node)
	assert.Equal(t, "last diagnostic", node.GetFullOutput(app.tree.RawLogBuffer))
	assert.NotNil(t, app.tree.GetNode("go test"), "unscoped stderr must remain visible")
	assert.True(t, app.showErrorModal)
	assert.Contains(t, app.View(), "Test run failed")
	assert.Contains(t, app.View(), runErr.Error())
	assert.Contains(t, app.View(), "Failed")
}

func TestAppOperationalFailureForcesNonzeroExitCode(t *testing.T) {
	app := NewLiveApp(nil, &fakeTestRunner{})
	runErr := errors.New("JSON stream failed")

	updated, _ := app.Update(TestDoneMsg{Err: runErr, ExitCode: 0, RunGen: 0})
	app = appFromModel(t, updated)

	assert.Equal(t, 1, app.exitCode)
	assert.True(t, app.showErrorModal)
}

func TestBuildEventRelevanceUsesImportPath(t *testing.T) {
	node := &model.TestNode{Package: "example.com/project/pkg", FullPath: "example.com/project/pkg"}
	assert.True(t, isEventRelevantToNode(model.TestEvent{
		Action: "build-output", ImportPath: node.Package,
	}, node))
	assert.False(t, isEventRelevantToNode(model.TestEvent{
		Action: "build-output", ImportPath: "example.com/project/other",
	}, node))
}

func TestAppUpdateTick(t *testing.T) {
	app := NewLiveApp(nil, &fakeTestRunner{})
	app.startTime = time.Now().Add(-time.Second)

	updated, cmd := app.Update(TickMsg(time.Now()))
	app = appFromModel(t, updated)
	assert.GreaterOrEqual(t, app.tree.Elapsed, 0.9)
	assert.NotNil(t, cmd)

	app.running = false
	app.logView = app.logView.TriggerCopyAnimation(true)
	updated, cmd = app.Update(TickMsg(time.Now()))
	app = appFromModel(t, updated)
	assert.NotNil(t, cmd, "copy animation schedules ticks after tests finish")

	app.logView = view.NewLogView()
	_, cmd = app.Update(TickMsg(time.Now()))
	assert.Nil(t, cmd)
}

func TestAppUpdateParsesStderrByPackage(t *testing.T) {
	stream := newFakeEventStream()
	app := NewLiveApp(nil, &fakeTestRunner{})
	app.stream = stream

	updated, _ := app.Update(StderrMsg{Line: "# example.com/project/pkg\n", RunGen: 0})
	app = appFromModel(t, updated)
	assert.Equal(t, "example.com/project/pkg", app.stderrPkg)

	updated, _ = app.Update(StderrMsg{Line: "file.go:3: undefined: nope\n", RunGen: 0})
	app = appFromModel(t, updated)
	node := app.tree.GetNode("example.com/project/pkg")
	require.NotNil(t, node)
	assert.Equal(t,
		"# example.com/project/pkg\nfile.go:3: undefined: nope\n",
		node.GetFullOutput(app.tree.RawLogBuffer),
	)

	updated, _ = app.Update(StderrMsg{
		Line: "# stale/pkg\n", RunGen: 99,
	})
	app = appFromModel(t, updated)
	assert.Nil(t, app.tree.GetNode("stale/pkg"))
	assert.Equal(t, "example.com/project/pkg", app.stderrPkg)
}

func TestCacheCleanedMessagesResetAndRestart(t *testing.T) {
	oldTree := model.NewTestTree()
	runner := &fakeTestRunner{stream: newFakeEventStream()}
	app := NewLiveApp(nil, runner)
	app.tree = oldTree
	app.stderrPkg = "old/pkg"
	app.running = false

	cleanErr := errors.New("cache unavailable")
	updated, cmd := app.Update(CacheCleanedMsg{Err: cleanErr})
	app = appFromModel(t, updated)
	assert.Same(t, oldTree, app.tree)
	assert.Zero(t, app.runGen)
	assert.False(t, app.running)
	assert.True(t, app.showErrorModal)
	assert.Contains(t, app.errorMessage, cleanErr.Error())
	assert.Nil(t, cmd)

	updated, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = appFromModel(t, updated)
	assert.False(t, app.showErrorModal)

	app.runGen = 1 // beginRerun invalidates the previous stream before cleaning.
	updated, cmd = app.Update(CacheCleanedMsg{RunGen: 1})
	app = appFromModel(t, updated)
	assert.NotSame(t, oldTree, app.tree)
	assert.Equal(t, 1, app.runGen)
	assert.True(t, app.running)
	assert.Empty(t, app.stderrPkg)
	assert.NotNil(t, cmd)

	app.screen = ScreenLog
	oldTree = app.tree
	app.runGen = 2
	updated, cmd = app.Update(LogCacheCleanedMsg{
		Package: "pkg", Test: "TestOne", RunGen: 2,
	})
	app = appFromModel(t, updated)
	assert.NotSame(t, oldTree, app.tree)
	assert.Equal(t, 2, app.runGen)
	assert.Equal(t, ScreenTree, app.screen)
	assert.True(t, app.running)
	assert.NotNil(t, cmd)
}

func TestCacheCleanCompletionFromOldGenerationIsIgnored(t *testing.T) {
	app := NewLiveApp(nil, &fakeTestRunner{})
	app.runGen = 4
	tree := app.tree

	updated, cmd := app.Update(CacheCleanedMsg{RunGen: 3})
	app = appFromModel(t, updated)
	assert.Same(t, tree, app.tree)
	assert.Equal(t, 4, app.runGen)
	assert.Nil(t, cmd)
}

func TestQuitModalKeyboardBehavior(t *testing.T) {
	stream := newFakeEventStream()
	app := NewLiveApp(nil, &fakeTestRunner{})
	app.stream = stream
	app.showQuitModal = true
	app.quitModalChoice = 1

	updated, _ := app.Update(runeKeyForApp("h"))
	app = appFromModel(t, updated)
	assert.Zero(t, app.quitModalChoice)

	updated, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = appFromModel(t, updated)
	assert.NotNil(t, cmd)
	assert.IsType(t, tea.QuitMsg{}, cmd())
	assert.Equal(t, 1, stream.kills)

	app.showQuitModal = true
	app.quitModalChoice = 0
	updated, _ = app.Update(runeKeyForApp("l"))
	app = appFromModel(t, updated)
	assert.Equal(t, 1, app.quitModalChoice)
	updated, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = appFromModel(t, updated)
	assert.False(t, app.showQuitModal)

	app.showQuitModal = true
	updated, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = appFromModel(t, updated)
	assert.False(t, app.showQuitModal)
}

func TestRerunModalKeyboardBehavior(t *testing.T) {
	stream := newFakeEventStream()
	runner := &fakeTestRunner{}
	app := NewLiveApp(nil, runner)
	app.stream = stream
	app.showRerunModal = true
	app.rerunModalChoice = 1

	updated, _ := app.Update(runeKeyForApp("h"))
	app = appFromModel(t, updated)
	assert.Zero(t, app.rerunModalChoice)
	updated, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = appFromModel(t, updated)
	assert.False(t, app.showRerunModal)
	cleaned, ok := cmd().(CacheCleanedMsg)
	require.True(t, ok)
	assert.NoError(t, cleaned.Err)
	assert.Equal(t, 1, cleaned.RunGen)
	assert.Equal(t, 1, stream.kills)

	app.showRerunModal = true
	app.rerunModalChoice = 1
	updated, cmd = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = appFromModel(t, updated)
	assert.False(t, app.showRerunModal)
	assert.Nil(t, cmd)

	app.showRerunModal = true
	updated, cmd = app.Update(runeKeyForApp("y"))
	assert.NotNil(t, cmd)
	app = appFromModel(t, updated)
	assert.False(t, app.showRerunModal)
}

func TestLogRerunModalKeyboardBehavior(t *testing.T) {
	node := &model.TestNode{
		Name: "TestOne", FullPath: "pkg/TestOne", Package: "pkg",
	}
	app := NewLiveApp(nil, &fakeTestRunner{})
	app.logRerunNode = node
	app.showLogRerunModal = true
	app.logRerunModalChoice = 1

	updated, _ := app.Update(runeKeyForApp("h"))
	app = appFromModel(t, updated)
	assert.Zero(t, app.logRerunModalChoice)
	updated, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = appFromModel(t, updated)
	assert.False(t, app.showLogRerunModal)
	message, ok := cmd().(LogCacheCleanedMsg)
	require.True(t, ok)
	assert.Equal(t, "pkg", message.Package)
	assert.Equal(t, "TestOne", message.Test)
	assert.Equal(t, 1, message.RunGen)

	app.showLogRerunModal = true
	app.logRerunModalChoice = 1
	updated, cmd = app.Update(runeKeyForApp("n"))
	app = appFromModel(t, updated)
	assert.False(t, app.showLogRerunModal)
	assert.Nil(t, cmd)
}

func TestStopModalKeyboardBehavior(t *testing.T) {
	stream := newFakeEventStream()
	app := NewLiveApp(nil, &fakeTestRunner{})
	app.stream = stream
	app.showStopModal = true
	app.stopModalChoice = 1
	app.startTime = time.Now().Add(-time.Second)

	updated, _ := app.Update(runeKeyForApp("h"))
	app = appFromModel(t, updated)
	assert.Zero(t, app.stopModalChoice)
	updated, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = appFromModel(t, updated)
	assert.Nil(t, cmd)
	assert.False(t, app.running)
	assert.False(t, app.showStopModal)
	assert.Equal(t, 1, stream.kills)
	assert.Equal(t, 1, app.runGen)
	assert.GreaterOrEqual(t, app.tree.Elapsed, 0.9)
	assert.Contains(t, app.View(), "Stopped")

	app.showStopModal = true
	app.stopModalChoice = 1
	updated, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = appFromModel(t, updated)
	assert.False(t, app.showStopModal)
}

func TestStopFailureIsVisible(t *testing.T) {
	killErr := errors.New("permission denied")
	stream := newFakeEventStream()
	stream.killErr = killErr
	app := NewLiveApp(nil, &fakeTestRunner{})
	app.stream = stream
	app.showStopModal = true
	app.stopModalChoice = 0

	updated, _ := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = appFromModel(t, updated)
	assert.False(t, app.running)
	assert.True(t, app.showErrorModal)
	assert.Contains(t, app.errorMessage, killErr.Error())
}

func TestAppScreenNavigationAndRequests(t *testing.T) {
	tree := model.NewTestTree()
	tree.ProcessEvent(model.TestEvent{Action: "pass", Package: "pkg", Test: "TestOne"})
	app := NewApp(tree)
	app.width = 100
	app.height = 20

	updated, _ := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = appFromModel(t, updated)
	assert.Equal(t, ScreenLog, app.screen)
	assert.Equal(t, "pkg", app.logView.GetNode().FullPath)

	updated, _ = app.Update(runeKeyForApp("?"))
	app = appFromModel(t, updated)
	assert.Equal(t, ScreenHelp, app.screen)
	assert.Equal(t, ScreenLog, app.prevScreen)

	updated, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = appFromModel(t, updated)
	assert.Equal(t, ScreenLog, app.screen)

	updated, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = appFromModel(t, updated)
	assert.Equal(t, ScreenTree, app.screen)

	updated, _ = app.Update(runeKeyForApp("?"))
	app = appFromModel(t, updated)
	assert.Equal(t, ScreenHelp, app.screen)
	assert.Equal(t, ScreenTree, app.prevScreen)
	updated, _ = app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app = appFromModel(t, updated)
	assert.Equal(t, ScreenTree, app.screen)

	_, cmd := app.Update(runeKeyForApp("q"))
	assert.NotNil(t, cmd)
	assert.IsType(t, tea.QuitMsg{}, cmd())
}

func TestAppTreeActionRequestsOpenModals(t *testing.T) {
	app := NewLiveApp(nil, &fakeTestRunner{})

	updated, _ := app.Update(runeKeyForApp("r"))
	app = appFromModel(t, updated)
	assert.True(t, app.showRerunModal)
	assert.Equal(t, 1, app.rerunModalChoice)

	app.showRerunModal = false
	updated, _ = app.Update(runeKeyForApp("s"))
	app = appFromModel(t, updated)
	assert.True(t, app.showStopModal)
	assert.Equal(t, 1, app.stopModalChoice)

	app.showStopModal = false
	updated, _ = app.Update(runeKeyForApp("q"))
	app = appFromModel(t, updated)
	assert.True(t, app.showQuitModal)
	assert.Equal(t, 1, app.quitModalChoice)
}

func TestAppLogRerunRequestOpensModal(t *testing.T) {
	tree := model.NewTestTree()
	tree.ProcessEvent(model.TestEvent{Action: "pass", Package: "pkg", Test: "TestOne"})
	app := NewLiveApp(nil, &fakeTestRunner{})
	app.running = false
	app.tree = tree
	app.treeView = app.treeView.SetData(tree).SetRunning(false)
	app.width = 80
	app.height = 20
	updated, _ := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = appFromModel(t, updated)
	require.Equal(t, ScreenLog, app.screen)

	updated, _ = app.Update(runeKeyForApp("r"))
	app = appFromModel(t, updated)
	assert.True(t, app.showLogRerunModal)
	assert.Equal(t, 1, app.logRerunModalChoice)
	assert.Equal(t, "pkg", app.logRerunNode.FullPath)
}

func TestAppCopyLogsRequestUsesClipboardAndAnimates(t *testing.T) {
	dir := t.TempDir()
	copiedFile := filepath.Join(dir, "copied")
	script := "#!/bin/sh\n/bin/cat > \"$GOWT_COPIED_FILE\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "wl-copy"), []byte(script), 0o755))
	t.Setenv("PATH", dir)
	t.Setenv("GOWT_COPIED_FILE", copiedFile)

	tree := model.NewTestTree()
	tree.ProcessEvent(model.TestEvent{
		Action: "output", Package: "pkg", Test: "TestOne", Output: "copy this\n",
	})
	tree.ProcessEvent(model.TestEvent{Action: "pass", Package: "pkg", Test: "TestOne"})
	app := NewApp(tree)
	app.width = 80
	app.height = 20
	app.screen = ScreenLog
	app.logView = view.NewLogView().SetData(
		tree.GetNode("pkg/TestOne"), tree.ProcessedLogBuffer, tree.RawLogBuffer,
	)

	updated, cmd := app.Update(runeKeyForApp("c"))
	app = appFromModel(t, updated)
	assert.False(t, app.logView.IsAnimating(), "clipboard work is asynchronous")
	assert.NotNil(t, cmd)
	resultMsg := cmd()
	if batch, ok := resultMsg.(tea.BatchMsg); ok {
		require.Len(t, batch, 1)
		resultMsg = batch[0]()
	}
	updated, _ = app.Update(resultMsg)
	app = appFromModel(t, updated)
	assert.True(t, app.logView.IsAnimating())
	content, err := os.ReadFile(copiedFile)
	require.NoError(t, err)
	assert.Equal(t, "copy this\n", string(content))
}

func TestAppViewScreensAndModals(t *testing.T) {
	app := NewApp(model.NewTestTree())
	app.width = 80
	app.height = 20
	assert.Contains(t, app.View(), "GOWT")

	app.screen = Screen(99)
	assert.Equal(t, "Unknown screen", app.View())

	app.screen = ScreenTree
	app.showQuitModal = true
	assert.Contains(t, app.View(), "Stop running tests?")
	app.showQuitModal = false
	app.showRerunModal = true
	assert.Contains(t, app.View(), "Rerun all tests?")
	app.showRerunModal = false
	app.showLogRerunModal = true
	assert.Contains(t, app.View(), "Rerun this test?")
	app.showLogRerunModal = false
	app.showStopModal = true
	assert.Contains(t, app.View(), "Stop running tests?")
}

func TestIsEventRelevantToNode(t *testing.T) {
	pkg := &model.TestNode{
		Name: "pkg", FullPath: "example.com/project/pkg", Package: "example.com/project/pkg",
	}
	testNode := &model.TestNode{
		Name: "TestOne", FullPath: pkg.FullPath + "/TestOne", Package: pkg.Package, Parent: pkg,
	}
	subtest := &model.TestNode{
		Name: "child", FullPath: testNode.FullPath + "/child", Package: pkg.Package, Parent: testNode,
	}

	assert.True(t, isEventRelevantToNode(model.TestEvent{Package: pkg.Package}, pkg))
	assert.True(t, isEventRelevantToNode(model.TestEvent{
		Package: pkg.Package, Test: "TestOne",
	}, testNode))
	assert.True(t, isEventRelevantToNode(model.TestEvent{
		Package: pkg.Package, Test: "TestOne/child",
	}, testNode))
	assert.True(t, isEventRelevantToNode(model.TestEvent{
		Package: pkg.Package, Test: "TestOne/child/grandchild",
	}, subtest))
	assert.False(t, isEventRelevantToNode(model.TestEvent{
		Package: pkg.Package, Test: "TestOther",
	}, testNode))
	assert.False(t, isEventRelevantToNode(model.TestEvent{
		Package: "other/pkg", Test: "TestOne",
	}, testNode))
}

func TestCopyToClipboard(t *testing.T) {
	t.Run("copies with first available tool", func(t *testing.T) {
		dir := t.TempDir()
		output := filepath.Join(dir, "output")
		used := filepath.Join(dir, "used")
		t.Setenv("GOWT_OUTPUT", output)
		t.Setenv("GOWT_USED", used)
		for _, name := range []string{"wl-copy", "xclip"} {
			script := "#!/bin/sh\nprintf '%s' '" + name + "' > \"$GOWT_USED\"\n/bin/cat > \"$GOWT_OUTPUT\"\n"
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755))
		}
		t.Setenv("PATH", dir)

		require.NoError(t, copyToClipboard("clipboard text"))
		content, err := os.ReadFile(output)
		require.NoError(t, err)
		assert.Equal(t, "clipboard text", string(content))
		tool, err := os.ReadFile(used)
		require.NoError(t, err)
		assert.Equal(t, "wl-copy", string(tool))
	})

	t.Run("no tool", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		err := copyToClipboard("text")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no clipboard command found")
	})

	t.Run("tool failure", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "wl-copy"), []byte("#!/bin/sh\nexit 4\n"), 0o755,
		))
		t.Setenv("PATH", dir)
		assert.Error(t, copyToClipboard("text"))
	})

	for _, tt := range []struct {
		name     string
		tool     string
		wantArgs string
	}{
		{name: "xclip", tool: "xclip", wantArgs: "-selection clipboard"},
		{name: "xsel", tool: "xsel", wantArgs: "--clipboard --input"},
		{name: "pbcopy", tool: "pbcopy"},
		{name: "windows clip", tool: "clip.exe"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			argsPath := filepath.Join(dir, "args")
			outputPath := filepath.Join(dir, "output")
			t.Setenv("GOWT_ARGS", argsPath)
			t.Setenv("GOWT_OUTPUT", outputPath)
			script := "#!/bin/sh\nprintf '%s' \"$*\" > \"$GOWT_ARGS\"\n/bin/cat > \"$GOWT_OUTPUT\"\n"
			require.NoError(t, os.WriteFile(
				filepath.Join(dir, tt.tool), []byte(script), 0o755,
			))
			t.Setenv("PATH", dir)

			require.NoError(t, copyToClipboard("fallback"))
			args, err := os.ReadFile(argsPath)
			require.NoError(t, err)
			assert.Equal(t, tt.wantArgs, string(args))
			output, err := os.ReadFile(outputPath)
			require.NoError(t, err)
			assert.Equal(t, "fallback", string(output))
		})
	}
}

func TestSystemClipboardHint(t *testing.T) {
	t.Run("clipboard available", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "wl-copy"), []byte("#!/bin/sh\nexit 0\n"), 0o755,
		))
		t.Setenv("PATH", dir)
		assert.Empty(t, systemClipboard{}.Hint())
	})

	t.Run("wayland", func(t *testing.T) {
		t.Setenv("PATH", "")
		t.Setenv("WAYLAND_DISPLAY", "wayland-0")
		t.Setenv("DISPLAY", "")
		assert.Contains(t, systemClipboard{}.Hint(), "wl-clipboard")
	})

	t.Run("x11", func(t *testing.T) {
		t.Setenv("PATH", "")
		t.Setenv("WAYLAND_DISPLAY", "")
		t.Setenv("DISPLAY", ":0")
		assert.Contains(t, systemClipboard{}.Hint(), "xclip")
	})

	t.Run("headless", func(t *testing.T) {
		t.Setenv("PATH", "")
		t.Setenv("WAYLAND_DISPLAY", "")
		t.Setenv("DISPLAY", "")
		assert.Contains(t, systemClipboard{}.Hint(), "no clipboard tool")
	})
}

func TestSystemClipboardWriteTimesOut(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "wl-copy"), []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0o755,
	))
	t.Setenv("PATH", dir)

	err := (systemClipboard{timeout: 20 * time.Millisecond}).Write("text")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestLoadTestResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.json")
	content := strings.Join([]string{
		`{"Action":"run","Package":"example.com/pkg","Test":"TestOne"}`,
		`{"Action":"output","Package":"example.com/pkg","Test":"TestOne","Output":"hello\n"}`,
		`{"Action":"pass","Package":"example.com/pkg","Test":"TestOne","Elapsed":0.2}`,
	}, "\n")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	tree, err := loadTestResults(path)
	require.NoError(t, err)
	node := tree.GetNode("example.com/pkg/TestOne")
	require.NotNil(t, node)
	assert.Equal(t, model.StatusPassed, node.Status)
	assert.Equal(t, "hello\n", node.GetFullOutput(tree.RawLogBuffer))
	assert.Equal(t, 1, tree.PassedCount)
}

func TestLoadTestResultsErrors(t *testing.T) {
	_, err := loadTestResults(filepath.Join(t.TempDir(), "missing.json"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open file")

	path := filepath.Join(t.TempDir(), "invalid.json")
	require.NoError(t, os.WriteFile(path, []byte("not-json"), 0o600))
	_, err = loadTestResults(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode test event")
}

func TestLoadTestResultsAcceptsRecordsLargerThanOneMiB(t *testing.T) {
	large := strings.Repeat("x", 2*1024*1024)
	events := []model.TestEvent{
		{Action: "run", Package: "pkg", Test: "TestLarge"},
		{Action: "output", Package: "pkg", Test: "TestLarge", Output: large},
		{Action: "pass", Package: "pkg", Test: "TestLarge"},
	}
	path := filepath.Join(t.TempDir(), "large.json")
	file, err := os.Create(path)
	require.NoError(t, err)
	encoder := json.NewEncoder(file)
	for _, event := range events {
		require.NoError(t, encoder.Encode(event))
	}
	require.NoError(t, file.Close())

	tree, err := loadTestResults(path)
	require.NoError(t, err)
	node := tree.GetNode("pkg/TestLarge")
	require.NotNil(t, node)
	assert.Equal(t, large, node.GetFullOutput(tree.RawLogBuffer))
}

func runeKeyForApp(value string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
}
