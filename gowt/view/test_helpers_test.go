package view

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	model "github.com/rickchristie/govner/gowt/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runeKey(value string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
}

func treeWithEvents(events ...model.TestEvent) *model.TestTree {
	tree := model.NewTestTree()
	for _, event := range events {
		tree.ProcessEvent(event)
	}
	return tree
}

func requireTreeRequest[T TreeViewRequest](t *testing.T, request TreeViewRequest) T {
	t.Helper()
	require.NotNil(t, request)
	typed, ok := request.(T)
	require.True(t, ok, "request has type %T", request)
	return typed
}

func requireLogRequest[T LogViewRequest](t *testing.T, request LogViewRequest) T {
	t.Helper()
	require.NotNil(t, request)
	typed, ok := request.(T)
	require.True(t, ok, "request has type %T", request)
	return typed
}

func requireHelpRequest[T HelpViewRequest](t *testing.T, request HelpViewRequest) T {
	t.Helper()
	require.NotNil(t, request)
	typed, ok := request.(T)
	require.True(t, ok, "request has type %T", request)
	return typed
}

func TestViewsSurviveTinyWindow(t *testing.T) {
	tree := treeWithEvents(model.TestEvent{
		Action: "run", Package: "pkg", Test: "TestOne",
	})
	node := tree.GetNode("pkg/TestOne")

	treeView, _, _ := NewTreeView().SetData(tree).Update(
		tea.WindowSizeMsg{Width: 1, Height: 1},
	)
	logView, _, _ := NewLogView().
		SetData(node, tree.ProcessedLogBuffer, tree.RawLogBuffer).
		Update(tea.WindowSizeMsg{Width: 1, Height: 1})
	helpView, _, _ := NewHelpView().Update(
		tea.WindowSizeMsg{Width: 1, Height: 1},
	)

	assert.NotPanics(t, func() { _ = treeView.View() })
	assert.NotPanics(t, func() { _ = logView.View() })
	assert.NotPanics(t, func() { _ = helpView.View() })
}
