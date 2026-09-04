package main

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	model "github.com/rickchristie/govner/gowt/model"
	"github.com/rickchristie/govner/gowt/util"
	view "github.com/rickchristie/govner/gowt/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type capturedTestRun struct {
	events []model.TestEvent
	stderr []string
	result TestResult
}

// captureTestRun consumes all three channels together. This is the same
// contract used by the TUI: completion is not enough until both output streams
// close, and bounded-channel suffixes must also be replayed.
func captureTestRun(t *testing.T, stream EventStream) capturedTestRun {
	t.Helper()
	events := stream.Events()
	stderr := stream.Stderr()
	done := stream.Done()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()

	var captured capturedTestRun
	for events != nil || stderr != nil || done != nil {
		select {
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			captured.events = append(captured.events, event)
		case line, ok := <-stderr:
			if !ok {
				stderr = nil
				continue
			}
			captured.stderr = append(captured.stderr, line)
		case result, ok := <-done:
			if !ok {
				done = nil
				continue
			}
			captured.result = result
		case <-timer.C:
			_ = stream.Kill()
			t.Fatal("go test did not finish within 30 seconds")
		}
	}
	captured.events = append(captured.events, captured.result.PendingEvents...)
	captured.stderr = append(captured.stderr, captured.result.PendingStderr...)
	return captured
}

func replayCapturedRun(t *testing.T, captured capturedTestRun) App {
	t.Helper()
	app := NewLiveApp(nil, nil)
	for _, event := range captured.events {
		app.processTestEvent(event)
	}
	for _, line := range captured.stderr {
		app.processStderrLine(line)
	}
	updated, _ := app.Update(TestDoneMsg{
		Err:      captured.result.Err,
		ExitCode: captured.result.ExitCode,
		RunGen:   app.runGen,
	})
	return appFromModel(t, updated)
}

func TestRealGoFailureDiagnosticsReachTheUser(t *testing.T) {
	fixtureDir, err := filepath.Abs(filepath.Join("testdata", "buildfail"))
	require.NoError(t, err)
	t.Setenv("GOWORK", "off")

	tests := []struct {
		name        string
		packageArg  string
		packagePath string
		marker      string
		vet         bool
		amd64Only   bool
		wantKind    model.FailureKind
		wantHeader  string
	}{
		{name: "source compiler", packageArg: "./source", marker: "missingSourceSymbol"},
		{name: "syntax", packageArg: "./syntax", marker: "syntax error"},
		{name: "internal test compiler", packageArg: "./internaltest", marker: "missingInternalTestSymbol"},
		{name: "external test compiler", packageArg: "./externaltest", marker: "missingExternalTestSymbol"},
		{name: "package setup", packageArg: "./mixed", marker: "found packages one"},
		{name: "import cycle", packageArg: "./importcycle/a", marker: "import cycle not allowed"},
		{
			name:        "module package resolution",
			packageArg:  "example.com/gowt-build-failures/missingpackage",
			packagePath: "example.com/gowt-build-failures/missingpackage",
			marker:      "no required module provides package",
		},
		{name: "vet", packageArg: "./vet", marker: "gowt-vet-marker", vet: true},
		{name: "assembler", packageArg: "./assembler", marker: "GOWT_NOT_A_REAL_INSTRUCTION", amd64Only: true},
		{name: "linker", packageArg: "./linker", marker: "relocation target", amd64Only: true},
		{
			name: "test main", packageArg: "./testmain", marker: "gowt-testmain-marker",
			wantKind: model.FailureKindPackage, wantHeader: "PACKAGE FAILED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.amd64Only && runtime.GOARCH != "amd64" {
				t.Skip("the fixture uses amd64 assembly to select this tool failure")
			}
			args := []string{"-C", fixtureDir, "-count=1"}
			if !tt.vet {
				args = append(args, "-vet=off")
			}
			args = append(args, tt.packageArg)

			stream, err := NewRealTestRunner().Start(args)
			require.NoError(t, err)
			captured := captureTestRun(t, stream)
			require.NoError(t, captured.result.Err)
			assert.NotZero(t, captured.result.ExitCode)
			require.NotEmpty(t, captured.events)

			app := replayCapturedRun(t, captured)
			wantKind := tt.wantKind
			if wantKind == model.FailureKindNone {
				wantKind = model.FailureKindBuild
			}
			wantHeader := tt.wantHeader
			if wantHeader == "" {
				wantHeader = "BUILD FAILED"
			}
			packagePath := tt.packagePath
			if packagePath == "" {
				packagePath = "example.com/gowt-build-failures/" + strings.TrimPrefix(tt.packageArg, "./")
			}
			node := app.tree.GetNode(packagePath)
			require.NotNil(t, node)
			assert.Equal(t, model.StatusFailed, node.Status)
			assert.Equal(t, wantKind, node.FailureKind)
			assert.NotEmpty(t, node.FailureSummary)
			nodeRaw := node.GetFullOutput(app.tree.RawLogBuffer)
			assert.Contains(t, nodeRaw, tt.marker)
			assert.Contains(t, node.GetFullOutput(app.tree.ProcessedLogBuffer), tt.marker)
			assert.Nil(t, app.tree.GetNode(packagePath+".test"), "a test binary is not a user package")
			for _, event := range captured.events {
				if event.Output != "" && (event.Action == "build-output" || event.Package == packagePath) {
					assert.Contains(t, nodeRaw, util.StripANSI(event.Output))
				}
			}

			// Every payload from both process streams must remain in the shared raw
			// store. The package log assertion above also checks useful attribution.
			allRaw := app.tree.RawLogBuffer.Slice(model.BufferRef{Start: 0, End: app.tree.RawLogBuffer.Len()})
			for _, event := range captured.events {
				if event.Output != "" {
					assert.Contains(t, allRaw, util.StripANSI(event.Output))
				}
			}
			for _, line := range captured.stderr {
				assert.Contains(t, allRaw, util.StripANSI(line))
			}

			logView := view.NewLogView().SetData(node, app.tree.ProcessedLogBuffer, app.tree.RawLogBuffer)
			logView, _, _ = logView.Update(tea.WindowSizeMsg{Width: 140, Height: 24})
			rendered := util.StripANSI(logView.View())
			assert.Contains(t, rendered, wantHeader)
			assert.Contains(t, rendered, tt.marker)
		})
	}
}

func TestRealGoCommandFailureKeepsStderrDiagnostics(t *testing.T) {
	fixtureDir, err := filepath.Abs(filepath.Join("testdata", "buildfail"))
	require.NoError(t, err)
	missingDir := filepath.Join(fixtureDir, "directory-that-does-not-exist")

	stream, err := NewRealTestRunner().Start([]string{"-C", missingDir})
	require.NoError(t, err)
	captured := captureTestRun(t, stream)
	require.NoError(t, captured.result.Err)
	assert.NotZero(t, captured.result.ExitCode)
	require.NotEmpty(t, captured.stderr)

	app := replayCapturedRun(t, captured)
	node := app.tree.GetNode("go test")
	require.NotNil(t, node)
	assert.Equal(t, model.StatusFailed, node.Status)
	assert.Equal(t, model.FailureKindCommand, node.FailureKind)
	assert.Contains(t, node.GetFullOutput(app.tree.RawLogBuffer), "directory-that-does-not-exist")

	logView := view.NewLogView().SetData(node, app.tree.ProcessedLogBuffer, app.tree.RawLogBuffer)
	logView, _, _ = logView.Update(tea.WindowSizeMsg{Width: 120, Height: 18})
	rendered := util.StripANSI(logView.View())
	assert.Contains(t, rendered, "COMMAND FAILED")
	assert.Contains(t, rendered, "directory-that-does-not-exist")
}

func TestRealGoPassingErrorLogsRemainDiagnostics(t *testing.T) {
	fixtureDir, err := filepath.Abs(filepath.Join("testdata", "buildfail"))
	require.NoError(t, err)
	t.Setenv("GOWORK", "off")

	stream, err := NewRealTestRunner().Start([]string{
		"-C", fixtureDir, "-count=1", "-vet=off", "./passlogs",
	})
	require.NoError(t, err)
	captured := captureTestRun(t, stream)
	require.NoError(t, captured.result.Err)
	require.Zero(t, captured.result.ExitCode)

	app := replayCapturedRun(t, captured)
	const pkg = "example.com/gowt-build-failures/passlogs"
	node := app.tree.GetNode(pkg + "/TestExpectedErrorLogs")
	require.NotNil(t, node)
	assert.Equal(t, model.StatusPassed, node.Status)
	assert.Equal(t, model.StatusPassed, app.tree.GetNode(pkg).Status)
	assert.Equal(t, model.FailureKindNone, node.FailureKind)
	assert.Zero(t, app.tree.FailedCount)
	assert.Contains(t, node.GetFullOutput(app.tree.RawLogBuffer), "expected stdout service failure")
	assert.Contains(t, node.GetFullOutput(app.tree.RawLogBuffer), "expected stderr service failure")
}

func TestSavedBuildFailureTraceReplaysWithoutDuplicatePackage(t *testing.T) {
	tree, err := loadTestResults(filepath.Join("testdata", "replay", "test-build-failure.jsonl"))
	require.NoError(t, err)
	require.Len(t, tree.Packages, 1)

	node := tree.GetNode("example.com/replay/service")
	require.NotNil(t, node)
	assert.Equal(t, model.StatusFailed, node.Status)
	assert.Equal(t, model.FailureKindBuild, node.FailureKind)
	assert.Contains(t, node.GetFullOutput(tree.RawLogBuffer), "replayMissingSymbol")
	assert.Nil(t, tree.GetNode("example.com/replay/service [example.com/replay/service.test]"))
}
