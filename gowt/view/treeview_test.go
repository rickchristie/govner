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

func TestFilterModeString(t *testing.T) {
	assert.Equal(t, "All", FilterAll.String())
	assert.Equal(t, "Focus", FilterFocus.String())
	assert.Equal(t, "All", FilterMode(99).String())
}

func TestNewTreeViewAndStateSetters(t *testing.T) {
	view := NewTreeView()

	assert.NotNil(t, view.tree)
	assert.Equal(t, FilterAll, view.filter)
	assert.Zero(t, view.cursor)
	assert.False(t, view.running)
	assert.False(t, view.stopped)
	assert.Nil(t, view.Init())

	tree := model.NewTestTree()
	view = view.SetData(tree).SetRunning(true).SetStopped(true).SetErrored(true).SetElapsed(2.5)
	assert.Same(t, tree, view.GetTree())
	assert.True(t, view.running)
	assert.True(t, view.stopped)
	assert.True(t, view.errored)
	assert.InDelta(t, 2.5, tree.Elapsed, 0.0001)
}

func TestTreeViewTickAndUpdateEvent(t *testing.T) {
	view := NewTreeView()
	view.selectorAnim = 2
	view = view.Tick()

	assert.Equal(t, 1, view.animFrame)
	assert.Equal(t, 1, view.selectorAnim)

	view = view.UpdateEvent(model.TestEvent{
		Action: "run", Package: "example.com/project/pkg", Test: "TestOne",
	})
	assert.NotNil(t, view.tree.GetNode("example.com/project/pkg/TestOne"))
	assert.False(t, view.cachedNodesValid)
}

func TestTreeViewWindowSizing(t *testing.T) {
	view, cmd, request := NewTreeView().Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	assert.Nil(t, cmd)
	assert.Nil(t, request)
	assert.True(t, view.ready)
	assert.Equal(t, 100, view.width)
	assert.Equal(t, 30, view.height)
	assert.Equal(t, 100, view.viewport.Width)
	assert.Equal(t, 26, view.viewport.Height)

	view, _, _ = view.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	assert.Equal(t, 80, view.viewport.Width)
	assert.Equal(t, 16, view.viewport.Height)
}

func TestTreeViewNavigationAndSelection(t *testing.T) {
	tree := treeWithEvents(
		model.TestEvent{Action: "start", Package: "c.example/pkg"},
		model.TestEvent{Action: "start", Package: "a.example/pkg"},
		model.TestEvent{Action: "start", Package: "b.example/pkg"},
	)
	view := NewTreeView().SetData(tree)
	view, _, _ = view.Update(tea.WindowSizeMsg{Width: 100, Height: 6})

	assert.Equal(t, "a.example/pkg", view.GetSelectedNode().FullPath)

	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, "b.example/pkg", view.GetSelectedNode().FullPath)
	assert.Greater(t, view.selectorAnim, 0)

	view, _, _ = view.Update(runeKey("j"))
	assert.Equal(t, "c.example/pkg", view.GetSelectedNode().FullPath)
	view, _, _ = view.Update(runeKey("j"))
	assert.Equal(t, "c.example/pkg", view.GetSelectedNode().FullPath, "down is clamped")

	view, _, _ = view.Update(runeKey("k"))
	assert.Equal(t, "b.example/pkg", view.GetSelectedNode().FullPath)
	view, _, _ = view.Update(runeKey("g"))
	assert.Equal(t, "a.example/pkg", view.GetSelectedNode().FullPath)
	view, _, _ = view.Update(runeKey("G"))
	assert.Equal(t, "c.example/pkg", view.GetSelectedNode().FullPath)

	_, _, request := view.Update(tea.KeyMsg{Type: tea.KeyEnter})
	selected := requireTreeRequest[SelectTestRequest](t, request)
	assert.Equal(t, "c.example/pkg", selected.Node.FullPath)
}

func TestTreeViewPageNavigationAndScroll(t *testing.T) {
	tree := model.NewTestTree()
	for _, pkg := range []string{"a", "b", "c", "d", "e", "f"} {
		tree.ProcessEvent(model.TestEvent{Action: "start", Package: pkg})
	}
	view := NewTreeView().SetData(tree)
	view, _, _ = view.Update(tea.WindowSizeMsg{Width: 80, Height: 7})

	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	assert.Equal(t, 3, view.cursor)
	assert.Equal(t, 1, view.scrollTop)

	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	assert.Equal(t, 5, view.cursor)
	assert.Equal(t, 3, view.scrollTop)

	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	assert.Equal(t, 2, view.cursor)
	assert.Equal(t, 2, view.scrollTop)

	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	assert.Zero(t, view.cursor)
	assert.Zero(t, view.scrollTop)
}

func TestTreeViewExpandCollapseAndParentNavigation(t *testing.T) {
	tree := treeWithEvents(model.TestEvent{
		Action: "run", Package: "pkg", Test: "TestParent/child",
	})
	view := NewTreeView().SetData(tree)

	view, _, _ = view.Update(runeKey("l"))
	pkg := tree.GetNode("pkg")
	require.True(t, pkg.Expanded)
	assert.Len(t, view.cachedNodes, 2)

	view, _, _ = view.Update(runeKey("j"))
	assert.Equal(t, "pkg/TestParent", view.GetSelectedNode().FullPath)
	view, _, _ = view.Update(runeKey("l"))
	assert.True(t, tree.GetNode("pkg/TestParent").Expanded)
	assert.Len(t, view.cachedNodes, 3)

	view, _, _ = view.Update(runeKey("j"))
	assert.Equal(t, "pkg/TestParent/child", view.GetSelectedNode().FullPath)
	view, _, _ = view.Update(runeKey("h"))
	assert.Equal(t, "pkg/TestParent", view.GetSelectedNode().FullPath)

	view, _, _ = view.Update(runeKey("h"))
	assert.False(t, tree.GetNode("pkg/TestParent").Expanded)
	assert.Len(t, view.cachedNodes, 2)
	view, _, _ = view.Update(runeKey("h"))
	assert.Equal(t, "pkg", view.GetSelectedNode().FullPath)
}

func TestTreeViewToggleExpandAll(t *testing.T) {
	tree := treeWithEvents(
		model.TestEvent{Action: "run", Package: "pkg", Test: "TestParent/child"},
		model.TestEvent{Action: "run", Package: "other", Test: "TestOther"},
	)
	view := NewTreeView().SetData(tree)

	view, _, _ = view.Update(runeKey("e"))
	assert.True(t, view.expanded)
	for _, node := range tree.NodeIndex {
		assert.True(t, node.Expanded, node.FullPath)
	}
	assert.Len(t, view.cachedNodes, 5)

	view, _, _ = view.Update(runeKey("e"))
	assert.False(t, view.expanded)
	assert.Zero(t, view.cursor)
	for _, node := range tree.NodeIndex {
		assert.False(t, node.Expanded, node.FullPath)
	}
	assert.Len(t, view.cachedNodes, 2)
}

func TestTreeViewReplacementResetsExpandAllState(t *testing.T) {
	first := treeWithEvents(model.TestEvent{Action: "run", Package: "pkg/one", Test: "TestOne/child"})
	view := NewTreeView().SetData(first)
	view, _, _ = view.Update(runeKey("e"))
	require.True(t, view.expanded)

	second := treeWithEvents(model.TestEvent{Action: "run", Package: "pkg/two", Test: "TestTwo/child"})
	view = view.SetData(second)
	assert.False(t, view.expanded)
	view, _, _ = view.Update(runeKey("e"))
	assert.True(t, second.GetNode("pkg/two").Expanded, "the first toggle on a new tree must expand")
}

func TestTreeViewNewCollapsedNodesRefreshExpandAllState(t *testing.T) {
	tree := treeWithEvents(model.TestEvent{Action: "run", Package: "pkg", Test: "TestOne/child"})
	view := NewTreeView().SetData(tree)
	view, _, _ = view.Update(runeKey("e"))
	require.True(t, view.expanded)

	tree.ProcessEvent(model.TestEvent{Action: "run", Package: "other", Test: "TestTwo/child"})
	view = view.SetData(tree)
	assert.False(t, view.expanded)
	view, _, _ = view.Update(runeKey("e"))
	assert.True(t, tree.GetNode("other").Expanded)
}

func TestTreeViewFilterFocus(t *testing.T) {
	tree := treeWithEvents(
		model.TestEvent{Action: "fail", Package: "failed/pkg", Test: "TestFailed"},
		model.TestEvent{Action: "run", Package: "running/pkg", Test: "TestRunning"},
		model.TestEvent{Action: "pass", Package: "passed/pkg", Test: "TestPassed"},
	)
	tree.GetNode("failed/pkg").Expanded = true
	tree.GetNode("running/pkg").Expanded = true
	view := NewTreeView().SetData(tree)

	view, _, _ = view.Update(runeKey(" "))
	assert.Equal(t, FilterFocus, view.filter)
	assert.Zero(t, view.cursor)
	assert.Zero(t, view.scrollTop)
	assert.Equal(t, []string{
		"failed/pkg",
		"failed/pkg/TestFailed",
		"running/pkg",
		"running/pkg/TestRunning",
	}, visiblePaths(view))

	view, _, _ = view.Update(runeKey(" "))
	assert.Equal(t, FilterAll, view.filter)
	assert.Contains(t, visiblePaths(view), "passed/pkg")
}

func TestTreeViewFocusIsEmptyWithoutFailuresOrRunningTests(t *testing.T) {
	tree := treeWithEvents(model.TestEvent{
		Action: "pass", Package: "passed/pkg", Test: "TestPassed",
	})
	view := NewTreeView().SetData(tree)
	view.filter = FilterFocus
	view.cachedNodesValid = false
	view = view.refreshCache()

	assert.Empty(t, view.cachedNodes)
	assert.Nil(t, view.GetSelectedNode())
	assert.Contains(t, stripAnsi(view.renderTree()), "No tests to display")
}

func TestTreeViewFocusIncludesPackageLevelFailureWithoutNamedTests(t *testing.T) {
	tree := treeWithEvents(
		model.TestEvent{Action: "start", Package: "pkg/setup"},
		model.TestEvent{Action: "fail", Package: "pkg/setup"},
	)
	view := NewTreeView().SetData(tree)
	view.filter = FilterFocus
	view.cachedNodesValid = false
	view = view.refreshCache()
	require.Len(t, view.cachedNodes, 1)
	assert.Equal(t, "pkg/setup", view.cachedNodes[0].FullPath)
}

func TestTreeViewSearchIncludesAncestorsAndSortsMatches(t *testing.T) {
	tree := treeWithEvents(
		model.TestEvent{Action: "run", Package: "z/pkg", Test: "TestNeedleB/child"},
		model.TestEvent{Action: "run", Package: "a/pkg", Test: "TestNeedleA"},
		model.TestEvent{Action: "run", Package: "m/pkg", Test: "TestOther"},
	)
	view := NewTreeView().SetData(tree)

	view, _, _ = view.Update(runeKey("/"))
	assert.True(t, view.searchMode)
	view, _, _ = view.Update(runeKey("Needle"))
	assert.Equal(t, "Needle", view.searchQuery)
	assert.Zero(t, view.cursor)

	assert.Equal(t, []string{
		"a/pkg",
		"a/pkg/TestNeedleA",
		"z/pkg",
		"z/pkg/TestNeedleB",
	}, visiblePaths(view))
	view = view.refreshCache()
	assert.Equal(t, -1, view.searchMatches[tree.GetNode("a/pkg")])
	assert.Equal(t, 4, view.searchMatches[tree.GetNode("a/pkg/TestNeedleA")])

	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	assert.Equal(t, "Needl", view.searchQuery)
	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, view.searchMode)
	assert.Equal(t, "Needl", view.searchQuery)

	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Empty(t, view.searchQuery)
	assert.Equal(t, []string{"a/pkg", "m/pkg", "z/pkg"}, visiblePaths(view))
}

func TestTreeViewSearchCancelClearsQuery(t *testing.T) {
	view := NewTreeView().SetData(treeWithEvents(model.TestEvent{
		Action: "run", Package: "pkg", Test: "TestOne",
	}))
	view, _, _ = view.Update(runeKey("/"))
	view, _, _ = view.Update(runeKey("Test"))
	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyEsc})

	assert.False(t, view.searchMode)
	assert.Empty(t, view.searchQuery)
	assert.Zero(t, view.cursor)
	assert.Zero(t, view.scrollTop)
}

func TestTreeSearchBackspaceRemovesWholeRune(t *testing.T) {
	view := NewTreeView()
	view, _, _ = view.Update(runeKey("/"))
	view, _, _ = view.Update(runeKey("界"))
	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	assert.Empty(t, view.searchQuery)
	assert.True(t, utf8.ValidString(view.searchQuery))
}

func TestTreeViewActionRequests(t *testing.T) {
	view := NewTreeView().SetData(treeWithEvents(model.TestEvent{
		Action: "run", Package: "pkg", Test: "TestOne",
	}))

	_, _, request := view.Update(runeKey("?"))
	requireTreeRequest[ShowHelpRequest](t, request)

	_, _, request = view.Update(runeKey("r"))
	requireTreeRequest[RerunAllRequest](t, request)

	_, _, request = view.Update(runeKey("R"))
	assert.Nil(t, request, "uppercase R is intentionally unbound")

	_, _, request = view.Update(runeKey("q"))
	requireTreeRequest[QuitRequest](t, request)

	_, _, request = view.Update(runeKey("s"))
	assert.Nil(t, request)
	view = view.SetRunning(true)
	_, _, request = view.Update(runeKey("s"))
	requireTreeRequest[StopRequest](t, request)
}

func TestTreeViewSetDataClampsCursor(t *testing.T) {
	view := NewTreeView().SetData(treeWithEvents(
		model.TestEvent{Action: "start", Package: "a"},
		model.TestEvent{Action: "start", Package: "b"},
	))
	view.cursor = 1
	view = view.SetData(model.NewTestTree())

	assert.Zero(t, view.cursor)
	assert.Nil(t, view.GetSelectedNode())
}

func TestTreeViewSortsByCompletedCountThenPath(t *testing.T) {
	tree := treeWithEvents(
		model.TestEvent{Action: "pass", Package: "a", Test: "TestOne"},
		model.TestEvent{Action: "pass", Package: "b", Test: "TestOne"},
		model.TestEvent{Action: "pass", Package: "b", Test: "TestTwo"},
		model.TestEvent{Action: "run", Package: "c", Test: "TestOne"},
	)
	view := NewTreeView().SetData(tree)

	assert.Equal(t, []string{"b", "a", "c"}, visiblePaths(view))

	nodes := []*model.TestNode{
		{Name: "running", Status: model.StatusRunning},
		{Name: "failed-z", FailedCount: 1},
		{Name: "failed-a", Status: model.StatusFailed},
	}
	sortNodesByFocusPriority(nodes)
	assert.Equal(t, []string{"failed-a", "failed-z", "running"}, nodeNames(nodes))
}

func TestTreeViewRenderingStates(t *testing.T) {
	tree := treeWithEvents(
		model.TestEvent{Action: "pass", Package: "pkg", Test: "TestPassed", Elapsed: 0.2},
		model.TestEvent{Action: "fail", Package: "pkg", Test: "TestFailed", Elapsed: 0.3},
		model.TestEvent{Action: "skip", Package: "pkg", Test: "TestSkipped", Elapsed: 0.4},
	)
	tree.Elapsed = 1.2
	tree.GetNode("pkg").Expanded = true
	view := NewTreeView().SetData(tree)
	view, _, _ = view.Update(tea.WindowSizeMsg{Width: 120, Height: 20})

	rendered := stripAnsi(view.View())
	assert.Contains(t, rendered, "GOWT")
	assert.Contains(t, rendered, "✗ 1")
	assert.Contains(t, rendered, "✓ 1")
	assert.Contains(t, rendered, "⊘ 1")
	assert.Contains(t, rendered, "Done")
	assert.Contains(t, rendered, "TestPassed")
	assert.Contains(t, rendered, "TestFailed")
	assert.Contains(t, rendered, "TestSkipped")
	assert.Contains(t, rendered, "[r Rerun]")

	view = view.SetRunning(true)
	running := stripAnsi(view.View())
	assert.Contains(t, running, "[s Stop]")
	assert.NotContains(t, running, "[r Rerun]")

	view = view.SetRunning(false).SetStopped(true)
	assert.Contains(t, stripAnsi(view.renderHeader()), "Stopped")
}

func TestTreeViewHelpBarModes(t *testing.T) {
	tree := treeWithEvents(model.TestEvent{
		Action: "run", Package: "pkg", Test: "TestNeedle",
	})
	view := NewTreeView().SetData(tree)
	view.width = 120
	view.height = 20

	assert.Contains(t, stripAnsi(view.renderHelpBar()), "[Space All]")
	view.filter = FilterFocus
	assert.Contains(t, stripAnsi(view.renderHelpBar()), "[Space Focus]")
	view.running = true
	assert.Contains(t, stripAnsi(view.renderHelpBar()), "[s Stop]")

	view.searchMode = true
	view.searchQuery = "Needle"
	view.cachedNodesValid = false
	searching := stripAnsi(view.renderHelpBar())
	assert.Contains(t, searching, "/Needle")
	assert.Contains(t, searching, "matches")
	assert.Contains(t, searching, "Enter Confirm")

	view.searchMode = false
	active := stripAnsi(view.renderHelpBar())
	assert.Contains(t, active, "[Esc Clear]")
	assert.Contains(t, active, "[/ New Search]")

	view.searchMode = true
	view.searchQuery = "missing"
	view.cachedNodesValid = false
	assert.Contains(t, stripAnsi(view.renderHelpBar()), "no matches")
}

func TestTreeViewRenderedNameAndSearchHighlight(t *testing.T) {
	view := NewTreeView()
	pkg := &model.TestNode{Name: "pkg", FullPath: "pkg"}
	testNode := &model.TestNode{Name: "TestNeedle", FullPath: "pkg/TestNeedle", Parent: pkg}

	assert.Equal(t, "selected", view.getRenderedName(testNode, true, "selected"))
	assert.Equal(t, "TestNeedle", view.getRenderedName(testNode, false, "TestNeedle"))
	assert.Equal(t, "pkg", stripAnsi(view.getRenderedName(pkg, false, "pkg")))
	assert.Equal(t, "pk", stripAnsi(view.getRenderedName(pkg, false, "pk")))

	view.searchQuery = "Needle"
	view.searchMatches = map[*model.TestNode]int{testNode: 4, pkg: -1}
	assert.Equal(t, "TestNeedle",
		stripAnsi(view.highlightSearchMatch(testNode, testNode.Name, false)))
	assert.Equal(t, "pkg", stripAnsi(view.highlightSearchMatch(pkg, pkg.Name, true)))
	assert.Equal(t, "TestNeedle",
		stripAnsi(view.getRenderedName(testNode, false, testNode.Name)))

	view.searchMatches[testNode] = len(testNode.Name)
	assert.Equal(t, "Test",
		stripAnsi(view.highlightSearchMatch(testNode, "Test", false)))
	delete(view.searchMatches, testNode)
	assert.Equal(t, "TestNeedle",
		stripAnsi(view.highlightSearchMatch(testNode, testNode.Name, false)))
}

func TestTreeViewRenderEmptyAndUnknownStatus(t *testing.T) {
	view := NewTreeView()
	view.width = 80
	view.height = 20
	assert.Contains(t, stripAnsi(view.View()), "No tests to display")

	node := &model.TestNode{Name: "mystery", NameWidth: 7, Status: model.TestStatus("mystery")}
	assert.Equal(t, "? ", view.renderStatusIcon(node))
	assert.Equal(t, "? ", view.getStatusIconRaw(node))
}

func TestTreeViewRenderHelpers(t *testing.T) {
	view := NewTreeView()

	assert.Equal(t, "", getIndent(0))
	assert.Equal(t, strings.Repeat("    ", 12), getIndent(12))
	assert.Equal(t, 1, numDigits(0))
	assert.Equal(t, 1, numDigits(9))
	assert.Equal(t, 2, numDigits(10))
	assert.Equal(t, 3, numDigits(999))
	assert.Equal(t, 5, numDigits(10000))

	assert.Equal(t, "plain", truncatePlainText("plain", 10))
	assert.Equal(t, "pla", truncatePlainText("plain", 3))
	assert.Equal(t, "界", truncatePlainText("界面", 2))
	assert.Equal(t, "", truncatePlainText("界面", 1))

	bar := view.renderProgressBar(5, 3, 2, 10, 20)
	assert.Equal(t, 20, lipgloss.Width(bar))
	assert.Equal(t, 20, lipgloss.Width(view.renderProgressBar(0, 0, 0, 0, 20)))
}

func TestTreeViewStatusIcons(t *testing.T) {
	view := NewTreeView()
	tests := []struct {
		status model.TestStatus
		cached bool
		raw    string
	}{
		{status: model.StatusPassed, raw: IconPassedRaw},
		{status: model.StatusPassed, cached: true, raw: IconCachedRaw},
		{status: model.StatusFailed, raw: IconFailedRaw},
		{status: model.StatusSkipped, raw: IconSkippedRaw},
		{status: model.StatusRunning, raw: GetSpinnerIconRaw(0)},
		{status: model.StatusPending, raw: IconPendingRaw},
	}
	for _, tt := range tests {
		node := &model.TestNode{Status: tt.status, Cached: tt.cached}
		assert.Equal(t, tt.raw, view.getStatusIconRaw(node))
		assert.Equal(t, strings.TrimSpace(tt.raw), strings.TrimSpace(stripAnsi(view.renderStatusIcon(node))))
	}
}

func visiblePaths(view TreeView) []string {
	nodes := view.getVisibleNodes()
	paths := make([]string, len(nodes))
	for i, node := range nodes {
		paths[i] = node.FullPath
	}
	return paths
}

func nodeNames(nodes []*model.TestNode) []string {
	names := make([]string, len(nodes))
	for i, node := range nodes {
		names[i] = node.Name
	}
	return names
}
