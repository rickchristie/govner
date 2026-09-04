package view

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	model "github.com/rickchristie/govner/gowt/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogTree(status model.TestStatus, outputs ...string) (*model.TestTree, *model.TestNode) {
	tree := model.NewTestTree()
	tree.ProcessEvent(model.TestEvent{Action: "run", Package: "pkg", Test: "TestLog"})
	for _, output := range outputs {
		tree.ProcessEvent(model.TestEvent{
			Action: "output", Package: "pkg", Test: "TestLog", Output: output,
		})
	}
	if status != model.StatusRunning {
		tree.ProcessEvent(model.TestEvent{
			Action: string(status), Package: "pkg", Test: "TestLog", Elapsed: 0.25,
		})
	}
	return tree, tree.GetNode("pkg/TestLog")
}

func TestNewLogViewAndAnimation(t *testing.T) {
	view := NewLogView()
	assert.Equal(t, LogModeProcessed, view.viewMode)
	assert.Equal(t, scrollOffsetBottom, view.processedYOffset)
	assert.Equal(t, scrollOffsetBottom, view.rawYOffset)
	assert.Nil(t, view.Init())
	assert.False(t, view.IsAnimating())

	view = view.TriggerCopyAnimation(true)
	assert.True(t, view.IsAnimating())
	assert.True(t, view.copyAnimSuccess)
	assert.Equal(t, 20, view.copyAnimTime)
	view = view.Tick()
	assert.Equal(t, 1, view.animFrame)
	assert.Equal(t, 19, view.copyAnimTime)
}

func TestLogViewSetDataBuildsRenderers(t *testing.T) {
	tree, node := testLogTree(model.StatusRunning, "first\n", "second\n")
	view := NewLogView().SetData(node, tree.ProcessedLogBuffer, tree.RawLogBuffer)

	assert.Same(t, node, view.GetNode())
	assert.True(t, view.autoScroll)
	require.NotNil(t, view.renderer)
	require.NotNil(t, view.rawRenderer)
	assert.Equal(t, "first\nsecond\n", stripAnsi(view.renderer.String()))
	assert.Equal(t, "first\nsecond\n", view.rawRenderer.String())
	assert.True(t, view.gotoBottom)

	view = view.SetData(nil, tree.ProcessedLogBuffer, tree.RawLogBuffer)
	assert.Nil(t, view.GetNode())
	assert.Nil(t, view.renderer)
	assert.Nil(t, view.rawRenderer)
	assert.False(t, view.autoScroll)
}

func TestLogViewWindowSizingAndContent(t *testing.T) {
	tree, node := testLogTree(model.StatusPassed, "line one\n", "line two\n")
	view := NewLogView().SetData(node, tree.ProcessedLogBuffer, tree.RawLogBuffer)
	view, cmd, request := view.Update(tea.WindowSizeMsg{Width: 40, Height: 10})

	assert.Nil(t, cmd)
	assert.Nil(t, request)
	assert.True(t, view.ready)
	assert.Equal(t, 40, view.viewport.Width)
	assert.Equal(t, 7, view.viewport.Height)
	content := stripAnsi(view.getContent())
	assert.Contains(t, content, "line one")
	assert.Contains(t, content, "line two")
	assert.Contains(t, content, "end of log")

	view, _, _ = view.Update(tea.WindowSizeMsg{Width: 20, Height: 8})
	assert.Equal(t, 20, view.viewport.Width)
	assert.Equal(t, 5, view.viewport.Height)
}

func TestLogViewShowsNoOutput(t *testing.T) {
	node := &model.TestNode{Name: "TestEmpty", FullPath: "pkg/TestEmpty", Package: "pkg"}
	view := NewLogView().SetData(node, model.NewLogBuffer(), model.NewLogBuffer())
	assert.Equal(t, "  (no output)", view.getContent())

	assert.Equal(t, "No test selected", NewLogView().View())
}

func TestLogViewRawAndProcessedModes(t *testing.T) {
	tree, node := testLogTree(model.StatusPassed,
		"=== RUN   TestLog\n",
		"plain line\n",
	)
	view := NewLogView().SetData(node, tree.ProcessedLogBuffer, tree.RawLogBuffer)
	view, _, _ = view.Update(tea.WindowSizeMsg{Width: 80, Height: 12})

	assert.NotContains(t, stripAnsi(view.getContent()), "=== RUN")
	view, _, request := view.Update(runeKey(" "))
	assert.Nil(t, request)
	assert.Equal(t, LogModeRaw, view.viewMode)
	assert.Contains(t, stripAnsi(view.getContent()), "=== RUN")

	view, _, _ = view.Update(runeKey(" "))
	assert.Equal(t, LogModeProcessed, view.viewMode)
	assert.NotContains(t, stripAnsi(view.getContent()), "=== RUN")
}

func TestLogViewActionRequests(t *testing.T) {
	tree, node := testLogTree(model.StatusPassed, "\x1b[31mcopy me\x1b[0m\n")
	view := NewLogView().SetData(node, tree.ProcessedLogBuffer, tree.RawLogBuffer)

	_, _, request := view.Update(runeKey("?"))
	requireLogRequest[ShowLogHelpRequest](t, request)

	_, _, request = view.Update(tea.KeyMsg{Type: tea.KeyEsc})
	requireLogRequest[BackRequest](t, request)

	_, _, request = view.Update(runeKey("r"))
	rerun := requireLogRequest[LogRerunTestRequest](t, request)
	assert.Same(t, node, rerun.Node)

	_, _, request = view.Update(runeKey("c"))
	copyRequest := requireLogRequest[CopyLogsRequest](t, request)
	assert.Equal(t, "copy me\n", copyRequest.Logs)

	view, _, _ = view.Update(runeKey(" "))
	_, _, request = view.Update(runeKey("c"))
	copyRequest = requireLogRequest[CopyLogsRequest](t, request)
	assert.Equal(t, "copy me\n", copyRequest.Logs)
}

func TestLogViewCopyAndRerunAreNoOpsWithoutContent(t *testing.T) {
	view := NewLogView()
	_, _, request := view.Update(runeKey("c"))
	assert.Nil(t, request)
	_, _, request = view.Update(runeKey("r"))
	assert.Nil(t, request)

	node := &model.TestNode{Name: "TestEmpty", FullPath: "pkg/TestEmpty", Package: "pkg"}
	view = view.SetData(node, model.NewLogBuffer(), model.NewLogBuffer())
	_, _, request = view.Update(runeKey("c"))
	assert.Nil(t, request)
}

func TestLogViewUpdateContentAppendsNewLogs(t *testing.T) {
	tree, node := testLogTree(model.StatusRunning, "first\n")
	view := NewLogView().SetData(node, tree.ProcessedLogBuffer, tree.RawLogBuffer)
	view, _, _ = view.Update(tea.WindowSizeMsg{Width: 80, Height: 10})

	tree.ProcessEvent(model.TestEvent{
		Action: "output", Package: "pkg", Test: "TestLog", Output: "second\n",
	})
	view = view.UpdateContent(node)
	assert.Equal(t, "first\nsecond\n", stripAnsi(view.renderer.String()))
	assert.Equal(t, "first\nsecond\n", view.rawRenderer.String())

	other := *node
	other.FullPath = "pkg/TestOther"
	before := view.renderer.String()
	view = view.UpdateContent(&other)
	assert.Equal(t, before, view.renderer.String())
}

func TestLogViewUpdateContentCreatesLateRenderers(t *testing.T) {
	tree, node := testLogTree(model.StatusRunning)
	view := NewLogView().SetData(node, tree.ProcessedLogBuffer, tree.RawLogBuffer)
	view, _, _ = view.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	assert.Nil(t, view.renderer)
	assert.Nil(t, view.rawRenderer)

	tree.ProcessEvent(model.TestEvent{
		Action: "output", Package: "pkg", Test: "TestLog", Output: "late\n",
	})
	view = view.UpdateContent(node)
	require.NotNil(t, view.renderer)
	require.NotNil(t, view.rawRenderer)
	assert.Equal(t, "late\n", stripAnsi(view.renderer.String()))
	assert.Contains(t, stripAnsi(view.viewport.View()), "late")
}

func TestLogViewCompletionDisablesAutoScroll(t *testing.T) {
	tree, node := testLogTree(model.StatusRunning, "running\n")
	oldNode := *node
	view := NewLogView().SetData(&oldNode, tree.ProcessedLogBuffer, tree.RawLogBuffer)
	view, _, _ = view.Update(tea.WindowSizeMsg{Width: 80, Height: 6})
	require.True(t, view.autoScroll)

	completed := oldNode
	completed.Status = model.StatusPassed
	view = view.UpdateContent(&completed)
	assert.False(t, view.autoScroll)
}

func TestLogViewDetectsCompletionThroughSharedNodePointer(t *testing.T) {
	tree, node := testLogTree(model.StatusRunning, "running\n")
	view := NewLogView().SetData(node, tree.ProcessedLogBuffer, tree.RawLogBuffer)
	view, _, _ = view.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	require.True(t, view.autoScroll)

	tree.ProcessEvent(model.TestEvent{Action: "pass", Package: "pkg", Test: "TestLog"})
	require.Equal(t, model.StatusPassed, node.Status, "the shared pointer is mutated in place")
	view = view.UpdateContent(node)
	assert.False(t, view.autoScroll)
	assert.Contains(t, stripAnsi(view.viewport.View()), "end of log")
}

func TestLogViewChangingNodeOrModeClearsRendererSpecificSearch(t *testing.T) {
	tree, node := testLogTree(model.StatusPassed, "needle processed\n")
	view := NewLogView().SetData(node, tree.ProcessedLogBuffer, tree.RawLogBuffer)
	view, _, _ = view.Update(tea.WindowSizeMsg{Width: 80, Height: 8})
	view, _, _ = view.Update(runeKey("/"))
	view, _, _ = view.Update(runeKey("needle"))
	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.True(t, view.searchActive)

	view, _, _ = view.Update(runeKey(" "))
	assert.Equal(t, LogModeRaw, view.viewMode)
	assert.False(t, view.searchActive)
	assert.Empty(t, view.searchQuery)
	assert.Empty(t, view.highlightedContent.String())

	view.searchActive = true
	view.searchQuery = "old node"
	other := &model.TestNode{Name: "TestOther", FullPath: "pkg/TestOther", Package: "pkg"}
	view = view.SetData(other, model.NewLogBuffer(), model.NewLogBuffer())
	assert.False(t, view.searchActive)
	assert.Empty(t, view.searchQuery)
}

func TestProcessedSearchDoesNotRewriteANSIControlParameters(t *testing.T) {
	tree, node := testLogTree(model.StatusPassed,
		"{\"level\":\"info\",\"message\":\"styled\"}\n",
		"visible 38\n",
	)
	view := NewLogView().SetData(node, tree.ProcessedLogBuffer, tree.RawLogBuffer)
	view, _, _ = view.Update(tea.WindowSizeMsg{Width: 100, Height: 10})
	view, _, _ = view.Update(runeKey("/"))
	view, _, _ = view.Update(runeKey("38"))
	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyEnter})

	plain := stripAnsi(view.highlightedContent.String())
	assert.Contains(t, plain, "message: styled")
	assert.Contains(t, plain, "visible 38")
}

func TestLogViewSearchLifecycleAndNavigation(t *testing.T) {
	tree, node := testLogTree(model.StatusPassed,
		"alpha\n",
		"match one\n",
		"middle\n",
		"match two\n",
	)
	view := NewLogView().SetData(node, tree.ProcessedLogBuffer, tree.RawLogBuffer)
	view, _, _ = view.Update(tea.WindowSizeMsg{Width: 80, Height: 7})

	view, _, _ = view.Update(runeKey("/"))
	assert.True(t, view.searchMode)
	view, _, _ = view.Update(runeKey("match"))
	assert.Equal(t, []int{1, 3}, view.searchMatches)
	assert.Zero(t, view.currentMatchIndex)
	assert.False(t, view.autoScroll)

	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, view.searchMode)
	assert.True(t, view.searchActive)
	assert.Contains(t, stripAnsi(view.highlightedContent.String()), "match")

	view, _, _ = view.Update(runeKey("n"))
	assert.Equal(t, 1, view.currentMatchIndex)
	view, _, _ = view.Update(runeKey("n"))
	assert.Zero(t, view.currentMatchIndex)
	view, _, _ = view.Update(runeKey("N"))
	assert.Equal(t, 1, view.currentMatchIndex)

	view, _, _ = view.Update(runeKey("/"))
	view, _, _ = view.Update(runeKey("none"))
	assert.Empty(t, view.searchMatches)
	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	assert.Equal(t, "non", view.searchQuery)
	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.False(t, view.searchMode)
	assert.False(t, view.searchActive)
	assert.Empty(t, view.searchQuery)
	assert.Empty(t, view.searchMatches)
}

func TestLogViewSearchAppendsStreamingMatches(t *testing.T) {
	tree, node := testLogTree(model.StatusRunning, "match first\n")
	view := NewLogView().SetData(node, tree.ProcessedLogBuffer, tree.RawLogBuffer)
	view, _, _ = view.Update(tea.WindowSizeMsg{Width: 80, Height: 7})
	view, _, _ = view.Update(runeKey("/"))
	view, _, _ = view.Update(runeKey("match"))
	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, []int{0}, view.searchMatches)

	tree.ProcessEvent(model.TestEvent{
		Action: "output", Package: "pkg", Test: "TestLog", Output: "match second\n",
	})
	view = view.UpdateContent(node)
	assert.Equal(t, []int{0, 1}, view.searchMatches)
	assert.Contains(t, stripAnsi(view.highlightedContent.String()), "match second")
}

func TestLogViewScrollingControlsAutoScroll(t *testing.T) {
	tree, node := testLogTree(model.StatusRunning,
		"1\n", "2\n", "3\n", "4\n", "5\n", "6\n", "7\n",
	)
	view := NewLogView().SetData(node, tree.ProcessedLogBuffer, tree.RawLogBuffer)
	view, _, _ = view.Update(tea.WindowSizeMsg{Width: 20, Height: 6})

	view, _, _ = view.Update(runeKey("g"))
	assert.False(t, view.autoScroll)
	assert.True(t, view.viewport.AtTop())

	view, _, _ = view.Update(runeKey("G"))
	assert.True(t, view.autoScroll)
	assert.True(t, view.viewport.AtBottom())

	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.False(t, view.autoScroll)
	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyDown})
}

func TestSoftWrapASCIIAndANSI(t *testing.T) {
	assert.Equal(t, "abcd\nefgh\nij", softWrap("abcdefghij", 4))
	assert.Equal(t, "short\nline", softWrap("short\nline", 10))
	assert.Equal(t, "unchanged", softWrap("unchanged", 0))
	assert.Equal(t, "", softWrap("", 4))

	ansi := "\x1b[31mabcdefgh\x1b[0m"
	assert.Equal(t, "abcd\nefgh", stripAnsi(softWrap(ansi, 4)))
}

func TestSoftWrapPreservesUnicodeGraphemesAndCellWidths(t *testing.T) {
	wrapped := softWrap("界🙂e\u0301界", 3)
	assert.True(t, utf8.ValidString(wrapped))
	for _, line := range strings.Split(wrapped, "\n") {
		assert.LessOrEqual(t, lipgloss.Width(line), 3, line)
	}
	assert.Equal(t, "界🙂e\u0301界", strings.ReplaceAll(wrapped, "\n", ""))
}

func TestLogSearchBackspaceRemovesWholeRune(t *testing.T) {
	view := NewLogView()
	view, _, _ = view.Update(runeKey("/"))
	view, _, _ = view.Update(runeKey("界"))
	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	assert.Empty(t, view.searchQuery)
	assert.True(t, utf8.ValidString(view.searchQuery))
}

func TestLogViewRendering(t *testing.T) {
	tree, node := testLogTree(model.StatusFailed, "failure\n")
	view := NewLogView().SetData(node, tree.ProcessedLogBuffer, tree.RawLogBuffer)
	view, _, _ = view.Update(tea.WindowSizeMsg{Width: 100, Height: 12})

	rendered := stripAnsi(view.View())
	assert.Contains(t, rendered, "GOWT")
	assert.Contains(t, rendered, "✗")
	assert.Contains(t, rendered, "pkg/TestLog")
	assert.Contains(t, rendered, "failure")
	assert.Contains(t, rendered, "[c Copy]")
	assert.Contains(t, rendered, "Processed")

	view = view.TriggerCopyAnimation(true)
	assert.Contains(t, stripAnsi(view.renderHelpBar()), "Copied!")
	view = view.TriggerCopyAnimation(false)
	assert.Contains(t, stripAnsi(view.renderHelpBar()), "No clipboard")
}

func TestLogViewBuildFailureHeaderOmitsInvalidEmptyTestCount(t *testing.T) {
	tree := model.NewTestTree()
	tree.ProcessEvent(model.TestEvent{
		Action: "build-output", ImportPath: "pkg", Output: "broken.go:3: undefined: missing\n",
	})
	tree.ProcessEvent(model.TestEvent{Action: "build-fail", ImportPath: "pkg"})
	node := tree.GetNode("pkg")
	require.NotNil(t, node)

	view := NewLogView().SetData(node, tree.ProcessedLogBuffer, tree.RawLogBuffer)
	header := stripAnsi(view.renderHeader())
	assert.Contains(t, header, "BUILD FAILED")
	assert.NotContains(t, header, "(1/0)")
	assert.NotContains(t, header, "(0/0)")
}

func TestLogViewHeaderNamesEveryNonTestFailureClass(t *testing.T) {
	tests := []struct {
		kind   model.FailureKind
		label  string
		total  int
		passed int
		failed int
		count  string
	}{
		{kind: model.FailureKindBuild, label: "BUILD FAILED"},
		{kind: model.FailureKindPackage, label: "PACKAGE FAILED", total: 1, passed: 1, failed: 1, count: "(1/1)"},
		{kind: model.FailureKindCommand, label: "COMMAND FAILED"},
	}
	for _, tt := range tests {
		node := &model.TestNode{
			Name: tt.label, FullPath: "pkg", Package: "pkg", Status: model.StatusFailed,
			FailureKind: tt.kind, TotalCount: tt.total, PassedCount: tt.passed, FailedCount: tt.failed,
		}
		header := stripAnsi(NewLogView().SetData(node, model.NewLogBuffer(), model.NewLogBuffer()).renderHeader())
		assert.Contains(t, header, tt.label)
		if tt.count != "" {
			assert.Contains(t, header, tt.count)
		}
		assert.NotContains(t, header, "(1/0)")
	}
}

func TestLogViewHelpBarModes(t *testing.T) {
	tree, node := testLogTree(model.StatusPassed, "match\n", "other\n")
	view := NewLogView().SetData(node, tree.ProcessedLogBuffer, tree.RawLogBuffer)
	view, _, _ = view.Update(tea.WindowSizeMsg{Width: 120, Height: 10})

	assert.Contains(t, stripAnsi(view.renderHelpBar()), "[Space Processed]")
	view.viewMode = LogModeRaw
	assert.Contains(t, stripAnsi(view.renderHelpBar()), "[Space Raw]")

	view.searchMode = true
	view.searchQuery = "match"
	view.performSearch()
	searching := stripAnsi(view.renderHelpBar())
	assert.Contains(t, searching, "/match")
	assert.Contains(t, searching, "[1/1]")
	assert.Contains(t, searching, "Enter Confirm")

	view.searchQuery = "missing"
	view.performSearch()
	assert.Contains(t, stripAnsi(view.renderHelpBar()), "no matches")

	view.searchMode = false
	view.searchMatches = []int{0, 1}
	assert.Contains(t, stripAnsi(view.renderHelpBar()), "[n/N 2 matches]")
}

func TestLogViewStatusIconsAndCopySheen(t *testing.T) {
	view := NewLogView()
	for status, icon := range map[model.TestStatus]string{
		model.StatusPassed:  IconCharPassed,
		model.StatusFailed:  IconCharFailed,
		model.StatusSkipped: IconCharSkipped,
		model.StatusPending: IconCharPending,
		model.StatusRunning: SpinnerFrames[0],
	} {
		assert.Equal(t, icon, stripAnsi(view.renderStatusIcon(status)))
	}
	assert.Equal(t, "?", view.renderStatusIcon(model.TestStatus("unknown")))

	view.copyAnimTime = 20
	assert.Equal(t, "✓ Copied!", stripAnsi(view.renderCopyWithSheen()))
	view.copyAnimTime = 1
	assert.Equal(t, "✓ Copied!", stripAnsi(view.renderCopyWithSheen()))
}

func TestLogViewSearchHelpersNoOpWithoutValidMatch(t *testing.T) {
	view := NewLogView()
	view.performSearch()
	view.rebuildHighlightedContent()
	view.appendHighlightedContent()
	view.scrollToCurrentMatch()
	assert.Empty(t, view.searchMatches)

	view.searchActive = true
	view.searchQuery = "x"
	view.appendHighlightedContent()
	assert.Empty(t, view.highlightedContent.String())
}

func TestLogViewHeaderWithoutNode(t *testing.T) {
	assert.Contains(t, stripAnsi(NewLogView().renderHeader()), "GOWT")
}

func TestLogViewCompletedContentDoesNotMarkPendingOrRunningAsEnded(t *testing.T) {
	for _, status := range []model.TestStatus{model.StatusPending, model.StatusRunning} {
		node := &model.TestNode{
			Name: "Test", FullPath: "pkg/Test", Package: "pkg", Status: status,
		}
		buffer := model.NewLogBuffer()
		ref := buffer.Append("line\n")
		node.ProcessedLog = &model.NodeLog{Refs: []model.BufferRef{ref}}
		view := NewLogView().SetData(node, buffer, model.NewLogBuffer())
		assert.NotContains(t, stripAnsi(view.getContent()), "end of log")
	}
}

func TestLogViewModeKeepsIndependentOffsets(t *testing.T) {
	tree, node := testLogTree(model.StatusPassed, strings.Repeat("line\n", 20))
	view := NewLogView().SetData(node, tree.ProcessedLogBuffer, tree.RawLogBuffer)
	view, _, _ = view.Update(tea.WindowSizeMsg{Width: 20, Height: 6})
	view.viewport.SetYOffset(3)

	view, _, _ = view.Update(runeKey(" "))
	assert.Equal(t, 3, view.processedYOffset)
	view.viewport.SetYOffset(5)
	view, _, _ = view.Update(runeKey(" "))
	assert.Equal(t, 5, view.rawYOffset)
	assert.Equal(t, 3, view.viewport.YOffset)
}
