package model

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func processEvents(t *testing.T, tree *TestTree, events ...TestEvent) {
	t.Helper()
	for _, event := range events {
		tree.ProcessEvent(event)
	}
}

func rawOutput(t *testing.T, tree *TestTree, node *TestNode) string {
	t.Helper()
	require.NotNil(t, node)
	if node.RawLog == nil {
		return ""
	}
	var output strings.Builder
	for _, ref := range node.RawLog.Refs {
		output.Write(tree.RawLogBuffer.SliceBytes(ref))
	}
	return output.String()
}

func processedOutput(t *testing.T, tree *TestTree, node *TestNode) string {
	t.Helper()
	require.NotNil(t, node)
	if node.ProcessedLog == nil {
		return ""
	}
	var output strings.Builder
	for _, ref := range node.ProcessedLog.Refs {
		output.Write(tree.ProcessedLogBuffer.SliceBytes(ref))
	}
	return output.String()
}

func TestNewTestTreeInitializesAllState(t *testing.T) {
	tree := NewTestTree()

	require.NotNil(t, tree)
	assert.Empty(t, tree.Packages)
	assert.Empty(t, tree.NodeIndex)
	assert.Empty(t, tree.OutputLineBuffer)
	assert.NotNil(t, tree.RawLogBuffer)
	assert.NotNil(t, tree.ProcessedLogBuffer)
	assert.Zero(t, tree.TotalCount)
	assert.Nil(t, tree.GetNode("missing"))
}

func TestProcessEventSkipsEventsWithoutPackageIdentity(t *testing.T) {
	tree := NewTestTree()

	changed := tree.ProcessEvent(TestEvent{Action: "run", Test: "TestIgnored"})

	assert.False(t, changed)
	assert.Empty(t, tree.Packages)
	assert.Empty(t, tree.NodeIndex)
}

func TestProcessEventUsesImportPathForBuildEvents(t *testing.T) {
	tree := NewTestTree()

	assert.True(t, tree.ProcessEvent(TestEvent{
		Action:     "build-output",
		ImportPath: "example.com/project/broken",
		Output:     "broken.go:4: undefined: missing\n",
	}), "the first build output creates a visible package node")
	assert.True(t, tree.ProcessEvent(TestEvent{
		Action:     "build-fail",
		ImportPath: "example.com/project/broken",
	}))

	node := tree.GetNode("example.com/project/broken")
	require.NotNil(t, node)
	assert.Equal(t, StatusFailed, node.Status)
	assert.Equal(t, FailureKindBuild, node.FailureKind)
	assert.Equal(t, "broken.go:4: undefined: missing", node.FailureSummary)
	assert.Equal(t, "broken.go:4: undefined: missing\n", rawOutput(t, tree, node))
	assert.Equal(t, 1, node.FailedCount)
	assert.Equal(t, 1, tree.FailedCount)
	assert.Zero(t, tree.TotalCount, "a package build failure is not a test node")
}

func TestTestBuildIDUsesOwningPackageAndFailedBuildMetadata(t *testing.T) {
	const (
		pkg     = "example.com/project/service"
		buildID = pkg + " [" + pkg + ".test]"
	)
	tree := NewTestTree()
	processEvents(t, tree,
		TestEvent{Action: "build-output", ImportPath: buildID, Output: "# " + buildID + "\n"},
		TestEvent{Action: "build-output", ImportPath: buildID, Output: "./service_test.go:9:2: undefined: missingSymbol\n"},
		TestEvent{Action: "build-fail", ImportPath: buildID},
		TestEvent{Action: "start", Package: pkg},
		TestEvent{Action: "output", Package: pkg, Output: "FAIL\t" + pkg + " [build failed]\n"},
		TestEvent{Action: "fail", Package: pkg, FailedBuild: buildID},
	)

	assert.Len(t, tree.Packages, 1, "a Go build ID must not create a duplicate package")
	assert.Nil(t, tree.GetNode(buildID))
	node := tree.GetNode(pkg)
	require.NotNil(t, node)
	assert.Equal(t, StatusFailed, node.Status)
	assert.Equal(t, FailureKindBuild, node.FailureKind)
	assert.Equal(t, "./service_test.go:9:2: undefined: missingSymbol", node.FailureSummary)
	assert.Contains(t, rawOutput(t, tree, node), "undefined: missingSymbol")
	assert.Contains(t, rawOutput(t, tree, node), "[build failed]")
	assert.Equal(t, 1, tree.FailedCount)
}

func TestFailedBuildLinksDependencyDiagnosticsToAffectedPackage(t *testing.T) {
	const (
		dependency = "example.com/project/generated"
		pkg        = "example.com/project/service"
	)
	tree := NewTestTree()
	processEvents(t, tree,
		TestEvent{Action: "build-output", ImportPath: dependency, Output: "generated.go:7: undefined: missingType\n"},
		TestEvent{Action: "build-fail", ImportPath: dependency},
		TestEvent{Action: "start", Package: pkg},
		TestEvent{Action: "output", Package: pkg, Output: "FAIL\t" + pkg + " [build failed]\n"},
		TestEvent{Action: "fail", Package: pkg, FailedBuild: dependency},
	)

	node := tree.GetNode(pkg)
	require.NotNil(t, node)
	assert.Equal(t, FailureKindBuild, node.FailureKind)
	assert.Equal(t, "generated.go:7: undefined: missingType", node.FailureSummary)
	assert.Contains(t, rawOutput(t, tree, node), "generated.go:7: undefined: missingType")
	assert.Contains(t, processedOutput(t, tree, node), "generated.go:7: undefined: missingType")
}

func TestLinkerBuildIDMovesToOwningPackageOnce(t *testing.T) {
	const (
		pkg     = "example.com/project/linker"
		buildID = pkg + ".test"
	)
	tree := NewTestTree()
	processEvents(t, tree,
		TestEvent{Action: "build-output", ImportPath: buildID, Output: "relocation target missingSymbol not defined\n"},
		TestEvent{Action: "build-fail", ImportPath: buildID},
	)
	assert.True(t, tree.HasFailedPackage())

	final := TestEvent{Action: "fail", Package: pkg, FailedBuild: buildID}
	tree.ProcessEvent(final)
	tree.ProcessEvent(final)

	assert.Len(t, tree.Packages, 1)
	assert.Nil(t, tree.GetNode(buildID))
	node := tree.GetNode(pkg)
	require.NotNil(t, node)
	assert.Equal(t, FailureKindBuild, node.FailureKind)
	assert.Equal(t, "relocation target missingSymbol not defined\n", rawOutput(t, tree, node))
	assert.Equal(t, 1, tree.FailedCount)
}

func TestCorrectedPackageResultClearsNonTestFailureCount(t *testing.T) {
	tree := NewTestTree()
	processEvents(t, tree,
		TestEvent{Action: "build-output", ImportPath: "pkg", Output: "compile failed\n"},
		TestEvent{Action: "build-fail", ImportPath: "pkg"},
	)
	assert.Equal(t, 1, tree.FailedCount)

	tree.ProcessEvent(TestEvent{Action: "pass", Package: "pkg"})
	node := tree.GetNode("pkg")
	require.NotNil(t, node)
	assert.Equal(t, StatusPassed, node.Status)
	assert.Equal(t, FailureKindNone, node.FailureKind)
	assert.Zero(t, tree.FailedCount)
}

func TestFailedBuildWithoutBuildEventsStillMarksPackage(t *testing.T) {
	const pkg = "example.com/project/setup"
	tree := NewTestTree()
	processEvents(t, tree,
		TestEvent{Action: "start", Package: pkg},
		TestEvent{Action: "output", Package: pkg, Output: "found packages one and two in ./setup\n"},
		TestEvent{Action: "fail", Package: pkg, FailedBuild: pkg},
	)

	node := tree.GetNode(pkg)
	require.NotNil(t, node)
	assert.Equal(t, StatusFailed, node.Status)
	assert.Equal(t, FailureKindBuild, node.FailureKind)
	assert.Equal(t, "found packages one and two in ./setup", node.FailureSummary)
	assert.Equal(t, 1, tree.FailedCount)
}

func TestMarkCommandFailureUsesDiagnosticsWithoutParsingTheirSeverity(t *testing.T) {
	tree := NewTestTree()
	assert.False(t, tree.HasFailedPackage())
	processEvents(t, tree, TestEvent{
		Action: "output", Package: "go test", Output: "go: unknown flag -bad-flag\n",
	})

	node := tree.GetNode("go test")
	require.NotNil(t, node)
	assert.Equal(t, StatusPending, node.Status, "diagnostic text alone is not a failure")

	tree.MarkCommandFailure("go test")
	assert.Equal(t, StatusFailed, node.Status)
	assert.Equal(t, FailureKindCommand, node.FailureKind)
	assert.Equal(t, "go: unknown flag -bad-flag", node.FailureSummary)
	assert.Equal(t, 1, tree.FailedCount)
	assert.True(t, tree.HasFailedPackage())
}

func TestMarkBuildFailureClassifiesPlainStderrDiagnostics(t *testing.T) {
	tree := NewTestTree()
	processEvents(t, tree, TestEvent{
		Action: "output", Package: "pkg", Output: "file.go:3: undefined: missing\n",
	})
	tree.MarkBuildFailure("pkg")

	node := tree.GetNode("pkg")
	require.NotNil(t, node)
	assert.Equal(t, StatusFailed, node.Status)
	assert.Equal(t, FailureKindBuild, node.FailureKind)
	assert.Equal(t, "file.go:3: undefined: missing", node.FailureSummary)
}

func TestPackageScopeFailureIsCountedAndKeepsItsDiagnostic(t *testing.T) {
	const pkg = "example.com/project/testmain"
	tree := NewTestTree()
	processEvents(t, tree,
		TestEvent{Action: "start", Package: pkg},
		TestEvent{Action: "output", Package: pkg, Output: "test setup could not continue\n"},
		TestEvent{Action: "output", Package: pkg, Output: "FAIL\t" + pkg + "\t0.01s\n"},
		TestEvent{Action: "fail", Package: pkg, Elapsed: 0.01},
	)

	node := tree.GetNode(pkg)
	require.NotNil(t, node)
	assert.Equal(t, StatusFailed, node.Status)
	assert.Equal(t, FailureKindPackage, node.FailureKind)
	assert.Equal(t, "test setup could not continue", node.FailureSummary)
	assert.Equal(t, 1, tree.FailedCount)
}

func TestDiagnosticSummaryKeepsRootCauseAheadOfToolSummary(t *testing.T) {
	summary := diagnosticSummary("", "file.s:2: unrecognized instruction GOWT_BAD\n")
	summary = diagnosticSummary(summary, "asm: assembly of file.s failed\nexit status 1\n")
	assert.Equal(t, "file.s:2: unrecognized instruction GOWT_BAD", summary)

	summary = diagnosticSummary("", "package example.com/cycle\nimports child\nimport cycle not allowed\n")
	assert.Equal(t, "import cycle not allowed", summary)
}

func TestProcessEventAcceptsAllGoTestNamedWorkloads(t *testing.T) {
	tree := NewTestTree()
	for _, name := range []string{
		"TestUnit", "FuzzParser", "BenchmarkLookup", "ExampleClient", "custom/generated-case",
	} {
		tree.ProcessEvent(TestEvent{Action: "run", Package: "pkg", Test: name})
		tree.ProcessEvent(TestEvent{Action: "pass", Package: "pkg", Test: name})
		node := tree.GetNode("pkg/" + name)
		require.NotNil(t, node, name)
		assert.Equal(t, StatusPassed, node.Status, name)
	}
}

func TestBenchmarkTerminalSemantics(t *testing.T) {
	const pkg = "example.com/project/bench"

	t.Run("package pass reconciles benchmark without test terminal", func(t *testing.T) {
		tree := NewTestTree()
		processEvents(t, tree,
			TestEvent{Action: "start", Package: pkg},
			TestEvent{Action: "run", Package: pkg, Test: "BenchmarkLookup"},
			TestEvent{Action: "output", Package: pkg, Test: "BenchmarkLookup", Output: "BenchmarkLookup-8  1  10 ns/op\n"},
			TestEvent{Action: "pass", Package: pkg},
		)

		node := tree.GetNode(pkg + "/BenchmarkLookup")
		require.NotNil(t, node)
		assert.Equal(t, StatusPassed, node.Status)
		assert.Zero(t, tree.RunningCount)
		assert.Equal(t, 1, tree.PassedCount)
	})

	t.Run("bench action is terminal", func(t *testing.T) {
		tree := NewTestTree()
		processEvents(t, tree,
			TestEvent{Action: "output", Package: pkg, Test: "BenchmarkLookup", Output: "benchmark log\n"},
			TestEvent{Action: "bench", Package: pkg, Test: "BenchmarkLookup"},
			TestEvent{Action: "pass", Package: pkg},
		)

		node := tree.GetNode(pkg + "/BenchmarkLookup")
		require.NotNil(t, node)
		assert.Equal(t, StatusPassed, node.Status)
		assert.Zero(t, tree.RunningCount)
		assert.Equal(t, 1, tree.PassedCount)
	})
}

func TestPackageTerminalReconcilesNestedUnfinishedWorkloads(t *testing.T) {
	tests := []struct {
		action string
		status TestStatus
		stats  []int
	}{
		{action: "pass", status: StatusPassed, stats: []int{2, 0, 0, 0, 0}},
		{action: "fail", status: StatusFailed, stats: []int{0, 2, 0, 0, 0}},
		{action: "skip", status: StatusSkipped, stats: []int{0, 0, 2, 0, 0}},
	}

	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			const pkg = "example.com/project/pkg"
			tree := NewTestTree()
			processEvents(t, tree,
				TestEvent{Action: "run", Package: pkg, Test: "TestParent/child"},
				TestEvent{Action: tt.action, Package: pkg},
			)

			assert.Equal(t, tt.status, tree.GetNode(pkg).Status)
			assert.Equal(t, tt.status, tree.GetNode(pkg+"/TestParent").Status)
			assert.Equal(t, tt.status, tree.GetNode(pkg+"/TestParent/child").Status)
			assert.Equal(t, tt.stats, statsSlice(tree))
			assert.Equal(t, 2, tree.TotalCount)
		})
	}
}

func TestRepeatedAndCorrectedTerminalEventsKeepCountsConsistent(t *testing.T) {
	const pkg = "example.com/project/pkg"
	tree := NewTestTree()
	processEvents(t, tree,
		TestEvent{Action: "run", Package: pkg, Test: "TestResult"},
		TestEvent{Action: "pass", Package: pkg, Test: "TestResult"},
	)

	assert.False(t, tree.ProcessEvent(TestEvent{
		Action: "pass", Package: pkg, Test: "TestResult",
	}), "a duplicate terminal fact must not change counts")
	assert.Equal(t, []int{1, 0, 0, 0, 0}, statsSlice(tree))

	assert.True(t, tree.ProcessEvent(TestEvent{
		Action: "fail", Package: pkg, Test: "TestResult",
	}))
	assert.Equal(t, []int{0, 1, 0, 0, 0}, statsSlice(tree))

	assert.True(t, tree.ProcessEvent(TestEvent{
		Action: "skip", Package: pkg, Test: "TestResult",
	}))
	assert.Equal(t, []int{0, 0, 1, 0, 0}, statsSlice(tree))

	assert.True(t, tree.ProcessEvent(TestEvent{
		Action: "bench", Package: pkg, Test: "TestResult",
	}))
	assert.Equal(t, []int{1, 0, 0, 0, 0}, statsSlice(tree))
}

func TestOutputSeverityDoesNotChangeTestStatus(t *testing.T) {
	const pkg = "example.com/project/pkg"
	tree := NewTestTree()
	processEvents(t, tree,
		TestEvent{Action: "start", Package: pkg},
		TestEvent{Action: "run", Package: pkg, Test: "TestExpectedError"},
	)
	for _, output := range []string{
		"{\"level\":\"error\",\"message\":\"expected service failure\"}\n",
		"--- FAIL: expected text from a fixture (0.00s)\n",
		"FAIL\n",
		"panic: expected and recovered\n",
		"fatal error text used by a parser test\n",
	} {
		assert.False(t, tree.ProcessEvent(TestEvent{
			Action: "output", Package: pkg, Test: "TestExpectedError", Output: output,
		}))
	}

	node := tree.GetNode(pkg + "/TestExpectedError")
	require.NotNil(t, node)
	assert.Equal(t, StatusRunning, node.Status)
	assert.Zero(t, tree.FailedCount)

	processEvents(t, tree,
		TestEvent{Action: "pass", Package: pkg, Test: "TestExpectedError"},
		TestEvent{Action: "pass", Package: pkg},
	)

	assert.Equal(t, StatusPassed, node.Status)
	assert.Equal(t, StatusPassed, tree.GetNode(pkg).Status)
	assert.Equal(t, []int{1, 0, 0, 0, 0}, statsSlice(tree))
}

func TestCorrectedChildResultClearsAggregateFailure(t *testing.T) {
	const pkg = "example.com/project/pkg"
	tree := NewTestTree()
	processEvents(t, tree,
		TestEvent{Action: "start", Package: pkg},
		TestEvent{Action: "run", Package: pkg, Test: "TestParent"},
		TestEvent{Action: "run", Package: pkg, Test: "TestParent/child"},
		TestEvent{Action: "fail", Package: pkg, Test: "TestParent/child"},
	)

	parent := tree.GetNode(pkg + "/TestParent")
	require.NotNil(t, parent)
	assert.Equal(t, StatusFailed, parent.Status)

	tree.ProcessEvent(TestEvent{Action: "pass", Package: pkg, Test: "TestParent/child"})
	assert.Equal(t, StatusRunning, parent.Status)
	assert.Equal(t, StatusRunning, tree.GetNode(pkg).Status)

	processEvents(t, tree,
		TestEvent{Action: "pass", Package: pkg, Test: "TestParent"},
		TestEvent{Action: "pass", Package: pkg},
	)

	assert.Equal(t, StatusPassed, parent.Status)
	assert.Equal(t, StatusPassed, tree.GetNode(pkg).Status)
	assert.Equal(t, []int{2, 0, 0, 0, 0}, statsSlice(tree))
}

func TestParentFailureCountsItsOwnTerminalEvent(t *testing.T) {
	const pkg = "example.com/project/pkg"
	tree := NewTestTree()
	processEvents(t, tree,
		TestEvent{Action: "start", Package: pkg},
		TestEvent{Action: "run", Package: pkg, Test: "TestParent"},
		TestEvent{Action: "run", Package: pkg, Test: "TestParent/child"},
		TestEvent{Action: "fail", Package: pkg, Test: "TestParent/child"},
		TestEvent{Action: "fail", Package: pkg, Test: "TestParent"},
		TestEvent{Action: "fail", Package: pkg},
	)

	assert.Equal(t, StatusFailed, tree.GetNode(pkg).Status)
	assert.Equal(t, StatusFailed, tree.GetNode(pkg+"/TestParent").Status)
	assert.Equal(t, []int{0, 2, 0, 0, 0}, statsSlice(tree))
}

func TestResultCorrectionsKeepAllTreeAggregatesConsistent(t *testing.T) {
	tree := NewTestTree()
	packages := []string{"example.com/project/one", "example.com/project/two"}
	tests := []string{"TestParent/first", "TestParent/second", "TestStandalone", "BenchmarkLookup"}
	testActions := []string{"run", "pause", "cont", "pass", "fail", "skip", "bench", "output"}
	packageActions := []string{"start", "pass", "fail", "skip"}

	for step := 0; step < 400; step++ {
		pkg := packages[step%len(packages)]
		var event TestEvent
		if step%11 == 0 {
			event = TestEvent{Action: packageActions[(step/11)%len(packageActions)], Package: pkg}
		} else {
			event = TestEvent{
				Action:  testActions[step%len(testActions)],
				Package: pkg,
				Test:    tests[(step/3)%len(tests)],
				Output:  "{\"level\":\"error\",\"message\":\"expected\"}\n",
			}
		}
		tree.ProcessEvent(event)
		assertTreeAggregates(t, tree, step)
	}
}

type expectedTreeState struct {
	passed  int
	failed  int
	skipped int
	running int
	total   int
	status  TestStatus
}

func assertTreeAggregates(t *testing.T, tree *TestTree, step int) {
	t.Helper()
	var global expectedTreeState
	for _, pkg := range tree.Packages {
		expected := expectedNodeState(t, pkg, step)
		global.passed += expected.passed
		global.failed += expected.failed
		global.skipped += expected.skipped
		global.running += expected.running
		global.total += expected.total
	}
	message := fmt.Sprintf("after event %d", step)
	assert.Equal(t, global.passed, tree.PassedCount, message)
	assert.Equal(t, global.failed, tree.FailedCount, message)
	assert.Equal(t, global.skipped, tree.SkippedCount, message)
	assert.Equal(t, global.running, tree.RunningCount, message)
	assert.Equal(t, global.total, tree.TotalCount, message)
}

func expectedNodeState(t *testing.T, node *TestNode, step int) expectedTreeState {
	t.Helper()
	state := expectedTreeState{status: node.eventStatus}
	if node.Parent != nil {
		state.total = 1
		switch node.eventStatus {
		case StatusPassed:
			state.passed = 1
		case StatusFailed:
			state.failed = 1
		case StatusSkipped:
			state.skipped = 1
		case StatusRunning:
			state.running = 1
		}
	}

	hasPassedChild := false
	hasSkippedChild := false
	hasRunningChild := false
	for _, child := range node.Children {
		childState := expectedNodeState(t, child, step)
		state.passed += childState.passed
		state.failed += childState.failed
		state.skipped += childState.skipped
		state.running += childState.running
		state.total += childState.total
		switch childState.status {
		case StatusFailed:
			state.status = StatusFailed
		case StatusRunning:
			hasRunningChild = true
		case StatusPassed:
			hasPassedChild = true
		case StatusSkipped:
			hasSkippedChild = true
		}
	}
	if state.status != StatusFailed {
		switch {
		case node.eventStatus == StatusRunning || hasRunningChild:
			state.status = StatusRunning
		case node.eventStatus == StatusPassed:
			state.status = StatusPassed
		case node.eventStatus == StatusSkipped:
			state.status = StatusSkipped
		case hasPassedChild:
			state.status = StatusPassed
		case hasSkippedChild:
			state.status = StatusSkipped
		default:
			state.status = StatusPending
		}
	}

	message := fmt.Sprintf("node %s after event %d", node.FullPath, step)
	assert.Equal(t, state.status, node.Status, message)
	assert.Equal(t, state.passed, node.PassedCount, message)
	assert.Equal(t, state.failed, node.FailedCount, message)
	assert.Equal(t, state.skipped, node.SkippedCount, message)
	assert.Equal(t, state.running, node.RunningCount, message)
	assert.Equal(t, state.total, node.TotalCount, message)
	return state
}

func TestPackageEventLifecycle(t *testing.T) {
	tests := []struct {
		action        string
		wantStatus    TestStatus
		wantElapsed   float64
		wantChanged   bool
		output        string
		wantRawOutput string
	}{
		{action: "start", wantStatus: StatusRunning, wantChanged: true},
		{action: "pass", wantStatus: StatusPassed, wantElapsed: 1.25, wantChanged: true},
		{action: "fail", wantStatus: StatusFailed, wantElapsed: 1.25, wantChanged: true},
		{action: "skip", wantStatus: StatusSkipped, wantChanged: true},
		{
			action:        "output",
			wantStatus:    StatusPending,
			wantChanged:   true,
			output:        "package output\n",
			wantRawOutput: "package output\n",
		},
		{action: "unknown", wantStatus: StatusPending, wantChanged: true},
	}

	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			tree := NewTestTree()
			changed := tree.ProcessEvent(TestEvent{
				Action:  tt.action,
				Package: "example.com/project/pkg",
				Elapsed: 1.25,
				Output:  tt.output,
			})

			node := tree.GetNode("example.com/project/pkg")
			require.NotNil(t, node)
			assert.Equal(t, tt.wantChanged, changed)
			assert.Equal(t, tt.wantStatus, node.Status)
			assert.Equal(t, tt.wantElapsed, node.Elapsed)
			assert.Equal(t, tt.wantRawOutput, rawOutput(t, tree, node))
		})
	}
}

func TestTestEventLifecycleAndCounts(t *testing.T) {
	tree := NewTestTree()
	pkg := "example.com/project/pkg"
	path := pkg + "/TestLifecycle"

	assert.True(t, tree.ProcessEvent(TestEvent{Action: "run", Package: pkg, Test: "TestLifecycle"}))
	node := tree.GetNode(path)
	require.NotNil(t, node)
	assert.Equal(t, StatusRunning, node.Status)
	assert.Equal(t, 1, tree.RunningCount)
	assert.Equal(t, 1, tree.TotalCount)

	assert.True(t, tree.ProcessEvent(TestEvent{Action: "pause", Package: pkg, Test: "TestLifecycle"}))
	assert.Equal(t, StatusPending, node.Status)
	assert.Zero(t, tree.RunningCount)

	assert.True(t, tree.ProcessEvent(TestEvent{Action: "cont", Package: pkg, Test: "TestLifecycle"}))
	assert.Equal(t, StatusRunning, node.Status)
	assert.Equal(t, 1, tree.RunningCount)

	assert.False(t, tree.ProcessEvent(TestEvent{
		Action:  "output",
		Package: pkg,
		Test:    "TestLifecycle",
		Output:  "hello\n",
	}))
	assert.Equal(t, "hello\n", rawOutput(t, tree, node))

	assert.True(t, tree.ProcessEvent(TestEvent{
		Action:  "pass",
		Package: pkg,
		Test:    "TestLifecycle",
		Elapsed: 0.42,
	}))
	assert.Equal(t, StatusPassed, node.Status)
	assert.InDelta(t, 0.42, node.Elapsed, 0.0001)
	assert.Zero(t, tree.RunningCount)
	assert.Equal(t, 1, tree.PassedCount)
	assert.Equal(t, 1, tree.Packages[pkg].PassedCount)
	assert.Equal(t, []int{1, 0, 0, 0, 0}, statsSlice(tree))
}

func TestTerminalTestEventsUpdateTheirOwnCounters(t *testing.T) {
	tests := []struct {
		action      string
		wantStatus  TestStatus
		wantPassed  int
		wantFailed  int
		wantSkipped int
	}{
		{action: "pass", wantStatus: StatusPassed, wantPassed: 1},
		{action: "fail", wantStatus: StatusFailed, wantFailed: 1},
		{action: "skip", wantStatus: StatusSkipped, wantSkipped: 1},
	}

	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			tree := NewTestTree()
			processEvents(t, tree,
				TestEvent{Action: "run", Package: "pkg", Test: "TestResult"},
				TestEvent{Action: tt.action, Package: "pkg", Test: "TestResult", Elapsed: 2.5},
			)

			node := tree.GetNode("pkg/TestResult")
			require.NotNil(t, node)
			assert.Equal(t, tt.wantStatus, node.Status)
			assert.Equal(t, tt.wantPassed, tree.PassedCount)
			assert.Equal(t, tt.wantFailed, tree.FailedCount)
			assert.Equal(t, tt.wantSkipped, tree.SkippedCount)
			assert.Zero(t, tree.RunningCount)
			assert.InDelta(t, 2.5, node.Elapsed, 0.0001)
		})
	}
}

func statsSlice(tree *TestTree) []int {
	passed, failed, skipped, running, cached := tree.ComputeAllStats()
	return []int{passed, failed, skipped, running, cached}
}

func TestNestedTestsBuildHierarchyAndPropagateAggregates(t *testing.T) {
	tree := NewTestTree()
	pkg := "example.com/project/pkg"

	processEvents(t, tree,
		TestEvent{Action: "run", Package: pkg, Test: "TestParent/child/grandchild"},
		TestEvent{Action: "fail", Package: pkg, Test: "TestParent/child/grandchild"},
	)

	pkgNode := tree.GetNode(pkg)
	parent := tree.GetNode(pkg + "/TestParent")
	child := tree.GetNode(pkg + "/TestParent/child")
	leaf := tree.GetNode(pkg + "/TestParent/child/grandchild")
	require.NotNil(t, pkgNode)
	require.NotNil(t, parent)
	require.NotNil(t, child)
	require.NotNil(t, leaf)

	assert.Equal(t, 0, pkgNode.Depth)
	assert.Equal(t, 1, parent.Depth)
	assert.Equal(t, 2, child.Depth)
	assert.Equal(t, 3, leaf.Depth)
	assert.Same(t, pkgNode, parent.Parent)
	assert.Same(t, parent, child.Parent)
	assert.Same(t, child, leaf.Parent)
	assert.Equal(t, []int{3, 3, 3, 1}, []int{
		tree.TotalCount,
		pkgNode.TotalCount,
		parent.TotalCount,
		leaf.TotalCount,
	})
	assert.Equal(t, 1, tree.FailedCount)
	assert.Equal(t, 1, pkgNode.FailedCount)
	assert.Equal(t, 1, parent.FailedCount)
	assert.Equal(t, 1, child.FailedCount)
	assert.Equal(t, 1, leaf.FailedCount)
	assert.Equal(t, StatusFailed, pkgNode.Status)
	assert.Equal(t, StatusFailed, parent.Status)
	assert.Equal(t, StatusFailed, child.Status)
	assert.Equal(t, StatusFailed, leaf.Status)
}

func TestRepeatedEventsReuseExistingNodes(t *testing.T) {
	tree := NewTestTree()
	event := TestEvent{Action: "run", Package: "pkg", Test: "TestOne/subtest"}

	tree.ProcessEvent(event)
	first := tree.GetNode("pkg/TestOne/subtest")
	tree.ProcessEvent(event)

	assert.Same(t, first, tree.GetNode("pkg/TestOne/subtest"))
	assert.Len(t, tree.Packages["pkg"].Children, 1)
	assert.Len(t, tree.Packages["pkg"].Children[0].Children, 1)
	assert.Equal(t, 2, tree.TotalCount)
	assert.Equal(t, 1, tree.RunningCount, "a duplicate run event must not double-count running tests")
}

func TestOutputIsReassembledAndSharedWithAncestors(t *testing.T) {
	tree := NewTestTree()
	pkg := "example.com/project/pkg"
	testName := "TestParent/child"

	assert.True(t, tree.ProcessEvent(TestEvent{
		Action: "output", Package: pkg, Test: testName, Output: "split ",
	}), "the first output event creates visible package and test nodes")
	leaf := tree.GetNode(pkg + "/" + testName)
	require.NotNil(t, leaf)
	assert.Empty(t, rawOutput(t, tree, leaf))
	assert.Equal(t, "split ", tree.OutputLineBuffer[leaf.FullPath])

	assert.False(t, tree.ProcessEvent(TestEvent{
		Action: "output", Package: pkg, Test: testName, Output: "line\nnext line\n",
	}))

	parent := tree.GetNode(pkg + "/TestParent")
	pkgNode := tree.GetNode(pkg)
	want := "split line\nnext line\n"
	assert.Equal(t, want, rawOutput(t, tree, leaf))
	assert.Equal(t, want, rawOutput(t, tree, parent))
	assert.Equal(t, want, rawOutput(t, tree, pkgNode))
	assert.Equal(t, want, leaf.GetFullOutput(tree.RawLogBuffer))
	assert.NotEmpty(t, processedOutput(t, tree, leaf))
	assert.NotEmpty(t, processedOutput(t, tree, parent))
	assert.NotEmpty(t, processedOutput(t, tree, pkgNode))
	assert.NotContains(t, tree.OutputLineBuffer, leaf.FullPath)
}

func TestOutputBuffersAreIndependentPerNode(t *testing.T) {
	tree := NewTestTree()
	tree.ProcessEvent(TestEvent{Action: "output", Package: "pkg", Test: "TestOne", Output: "one"})
	tree.ProcessEvent(TestEvent{Action: "output", Package: "pkg", Test: "TestTwo", Output: "two"})
	tree.ProcessEvent(TestEvent{Action: "output", Package: "pkg", Test: "TestOne", Output: " done\n"})
	tree.ProcessEvent(TestEvent{Action: "output", Package: "pkg", Test: "TestTwo", Output: " done\n"})

	assert.Equal(t, "one done\n", rawOutput(t, tree, tree.GetNode("pkg/TestOne")))
	assert.Equal(t, "two done\n", rawOutput(t, tree, tree.GetNode("pkg/TestTwo")))
	assert.Equal(t, "one done\ntwo done\n", rawOutput(t, tree, tree.GetNode("pkg")))
}

func TestOutputPreservesBlankLinesAndFlushesFinalPartialLine(t *testing.T) {
	tree := NewTestTree()
	tree.ProcessEvent(TestEvent{Action: "run", Package: "pkg", Test: "TestOutput"})
	tree.ProcessEvent(TestEvent{
		Action: "output", Package: "pkg", Test: "TestOutput", Output: "first\n\nfinal",
	})
	node := tree.GetNode("pkg/TestOutput")
	assert.Equal(t, "first\n\n", rawOutput(t, tree, node))

	tree.ProcessEvent(TestEvent{Action: "fail", Package: "pkg", Test: "TestOutput"})
	assert.Equal(t, "first\n\nfinal", rawOutput(t, tree, node))
	assert.Empty(t, tree.OutputLineBuffer)
}

func TestFlushOutputBuffersCommitsEveryNode(t *testing.T) {
	tree := NewTestTree()
	for _, testName := range []string{"TestOne", "TestTwo"} {
		tree.ProcessEvent(TestEvent{Action: "output", Package: "pkg", Test: testName, Output: testName})
	}
	require.Len(t, tree.OutputLineBuffer, 2)

	tree.FlushOutputBuffers()
	assert.Empty(t, tree.OutputLineBuffer)
	assert.Equal(t, "TestOne", rawOutput(t, tree, tree.GetNode("pkg/TestOne")))
	assert.Equal(t, "TestTwo", rawOutput(t, tree, tree.GetNode("pkg/TestTwo")))
}

func TestRawAndProcessedOutputRemoveTerminalControlSequences(t *testing.T) {
	tree := NewTestTree()
	output := "\x1b]8;;https://example.com\x07link\x1b]8;;\x07 " +
		"\x1b[31mred\x1b[0m\x07\r\n"
	tree.ProcessEvent(TestEvent{
		Action: "output", Package: "pkg", Test: "TestANSI", Output: output,
	})
	node := tree.GetNode("pkg/TestANSI")
	require.NotNil(t, node)
	assert.Equal(t, "link red\n", rawOutput(t, tree, node))
	assert.Equal(t, "link red\n", stripAnsi(processedOutput(t, tree, node)))
}

func TestProcessedOutputFiltersAndFormatsGoTestMarkers(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		want       string
		wantEmpty  bool
		wantSuffix string
	}{
		{name: "run marker", input: "=== RUN   TestOne\n", wantEmpty: true},
		{name: "pause marker", input: "=== PAUSE TestOne\n", wantEmpty: true},
		{name: "continue marker", input: "=== CONT  TestOne\n", wantEmpty: true},
		{name: "pass result", input: "--- PASS: TestOne (0.10s)\n", want: "✓ TestOne (0.10s)\n\n"},
		{name: "fail subtest result", input: "--- FAIL: TestOne/child (0.20s)\n", want: "✗ TestOne/\n      child (0.20s)\n\n"},
		{
			name:  "skip deep result",
			input: "--- SKIP: TestOne/child/grandchild (0.30s)\n",
			want:  "⊘ TestOne/\n      child/\n        grandchild (0.30s)\n\n",
		},
		{name: "plain output", input: "plain output\n", want: "plain output\n"},
		{name: "ANSI is removed", input: "\x1b[31mred\x1b[0m\n", want: "red\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripAnsi(processOutput(tt.input))
			if tt.wantEmpty {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestProcessedOutputFormatsJSON(t *testing.T) {
	got := stripAnsi(processOutput("{\"level\":\"info\",\"message\":\"ready\"}\n"))

	assert.Contains(t, got, "level: info")
	assert.Contains(t, got, "message: ready")
	assert.NotContains(t, got, `{"level"`)
	assert.True(t, strings.HasSuffix(got, "\n"))
}

func TestFormatTestResultWithoutDuration(t *testing.T) {
	got := stripAnsi(formatTestResult(
		"--- PASS: TestNoDuration",
		"--- PASS:",
		lipgloss.NewStyle(),
		"✓",
	))

	assert.Equal(t, "✓ TestNoDuration\n\n", got)
}

func TestCachedOutputMarksEntirePackageOnce(t *testing.T) {
	tree := NewTestTree()
	pkg := "example.com/project/pkg"
	processEvents(t, tree,
		TestEvent{Action: "pass", Package: pkg, Test: "TestOne"},
		TestEvent{Action: "pass", Package: pkg, Test: "TestParent/child"},
	)
	pkgNode := tree.GetNode(pkg)
	require.NotNil(t, pkgNode)
	total := tree.TotalCount
	require.Greater(t, total, 0)

	assert.True(t, tree.ProcessEvent(TestEvent{
		Action:  "output",
		Package: pkg,
		Output:  "ok  \texample.com/project/pkg\t(cached)\n",
	}))
	assert.True(t, pkgNode.Cached)
	assert.Equal(t, StatusPassed, pkgNode.Status)
	assert.Equal(t, total, pkgNode.CachedCount)
	assert.Equal(t, total, tree.CachedCount)

	for path, node := range tree.NodeIndex {
		if strings.HasPrefix(path, pkg) {
			assert.True(t, node.Cached, path)
			assert.Equal(t, StatusPassed, node.Status, path)
			assert.Equal(t, node.TotalCount, node.CachedCount, path)
		}
	}

	assert.True(t, tree.ProcessEvent(TestEvent{
		Action:  "output",
		Package: pkg,
		Output:  "ok  \texample.com/project/pkg\t(cached)\n",
	}))
	assert.Equal(t, total, tree.CachedCount, "the cached count must be idempotent")
}

func TestCachedOutputDoesNotSetResultStatus(t *testing.T) {
	pkg := "example.com/project/pkg"
	tests := []struct {
		name        string
		events      []TestEvent
		wantStatus  TestStatus
		wantFailed  int
		wantRunning int
	}{
		{
			name: "running",
			events: []TestEvent{
				{Action: "start", Package: pkg},
				{Action: "run", Package: pkg, Test: "TestResult"},
			},
			wantStatus:  StatusRunning,
			wantRunning: 1,
		},
		{
			name: "failed",
			events: []TestEvent{
				{Action: "run", Package: pkg, Test: "TestResult"},
				{Action: "fail", Package: pkg, Test: "TestResult"},
			},
			wantStatus: StatusFailed,
			wantFailed: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := NewTestTree()
			processEvents(t, tree, tt.events...)

			assert.True(t, tree.ProcessEvent(TestEvent{
				Action:  "output",
				Package: pkg,
				Output:  "ok  \texample.com/project/pkg\t(cached)\n",
			}))

			pkgNode := tree.GetNode(pkg)
			testNode := tree.GetNode(pkg + "/TestResult")
			require.NotNil(t, pkgNode)
			require.NotNil(t, testNode)
			assert.Equal(t, tt.wantStatus, pkgNode.Status)
			assert.Equal(t, tt.wantStatus, testNode.Status)
			assert.Zero(t, tree.PassedCount)
			assert.Equal(t, tt.wantFailed, tree.FailedCount)
			assert.Equal(t, tt.wantRunning, tree.RunningCount)
			assert.True(t, pkgNode.Cached)
			assert.True(t, testNode.Cached)
		})
	}
}

func TestCachedOutputDetectionIsConservativeForNormalLogs(t *testing.T) {
	assert.True(t, isCachedOutput("ok  \texample.com/pkg\t(cached)\n", "example.com/pkg"))
	assert.False(t, isCachedOutput("ok  \tother/pkg\t(cached)\n", "example.com/pkg"))
	assert.False(t, isCachedOutput("  ok example.com/pkg (cached)  ", "example.com/pkg"))
	assert.False(t, isCachedOutput("cache status: (cached)", "example.com/pkg"))
	assert.False(t, isCachedOutput("ok example.com/pkg 0.123s", "example.com/pkg"))
	assert.False(t, isCachedOutput("", "example.com/pkg"))
}

func TestSortedPackagesAndFlattenRespectExpansion(t *testing.T) {
	tree := NewTestTree()
	processEvents(t, tree,
		TestEvent{Action: "run", Package: "z.example/pkg", Test: "TestZ"},
		TestEvent{Action: "run", Package: "a.example/pkg", Test: "TestA/child"},
	)

	sorted := tree.GetSortedPackages()
	require.Len(t, sorted, 2)
	assert.Equal(t, "a.example/pkg", sorted[0].FullPath)
	assert.Equal(t, "z.example/pkg", sorted[1].FullPath)

	assert.Equal(t, []string{"a.example/pkg", "z.example/pkg"}, nodePaths(tree.Flatten()))
	sorted[0].Expanded = true
	sorted[0].Children[0].Expanded = true
	assert.Equal(t, []string{
		"a.example/pkg",
		"a.example/pkg/TestA",
		"a.example/pkg/TestA/child",
		"z.example/pkg",
	}, nodePaths(tree.Flatten()))
	assert.Equal(t, nodePaths(FlattenNode(sorted[0], 99)), []string{
		"a.example/pkg",
		"a.example/pkg/TestA",
		"a.example/pkg/TestA/child",
	})
}

func nodePaths(nodes []*TestNode) []string {
	paths := make([]string, len(nodes))
	for i, node := range nodes {
		paths[i] = node.FullPath
	}
	return paths
}

func TestNodeRelationshipHelpers(t *testing.T) {
	parent := &TestNode{Name: "parent", Depth: 2}
	first := &TestNode{Name: "first", Parent: parent, Expanded: true}
	second := &TestNode{Name: "second", Parent: parent}
	parent.Children = []*TestNode{first, second}

	assert.Equal(t, 2, parent.GetDepth())
	assert.True(t, parent.HasChildren())
	assert.False(t, first.HasChildren())
	assert.False(t, parent.IsLastChild())
	assert.False(t, first.IsLastChild())
	assert.True(t, second.IsLastChild())
	assert.False(t, first.HasExpandedSiblingBefore())
	assert.True(t, second.HasExpandedSiblingBefore())

	first.Expanded = false
	assert.False(t, second.HasExpandedSiblingBefore())
}

func TestNodeCountByStatusAndEmptyOutput(t *testing.T) {
	node := &TestNode{
		PassedCount:  2,
		FailedCount:  3,
		SkippedCount: 4,
		TotalCount:   9,
	}

	passed, failed, skipped, total := node.CountByStatus()
	assert.Equal(t, []int{2, 3, 4, 9}, []int{passed, failed, skipped, total})
	assert.Empty(t, node.GetFullOutput(NewLogBuffer()))
}

func TestShortPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "", want: ""},
		{input: "pkg", want: "pkg"},
		{input: "local/pkg", want: "local/pkg"},
		{input: "github.com/acme/project/internal/worker", want: "internal/worker"},
		{input: "github.com/acme/project/lib/worker/TestOne", want: "lib/worker/TestOne"},
		{input: "example.com/acme/project/my_package/child", want: "my_package/child"},
		{input: "github.com/acme/project", want: "project"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, ShortPath(tt.input))
			assert.Equal(t, tt.want, shortPackageName(tt.input))
		})
	}
}

func TestStripANSIHandlesCommonCSISequences(t *testing.T) {
	assert.Equal(t, "red plain", stripAnsi("\x1b[1;31mred\x1b[0m plain"))
	assert.Equal(t, "text", stripAnsi("\x1b[2Jtext\x1b[H"))
	assert.Equal(t, "before", stripAnsi("before\x1b[31"))
	assert.Equal(t, "link safe", stripAnsi("\x1b]8;;https://example.com\x07link\x1b]8;;\x07\x07 safe\r"))
}
