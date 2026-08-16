package view

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestNewHelpViewAndSources(t *testing.T) {
	view := NewHelpView()
	assert.Equal(t, HelpSourceTree, view.source)
	assert.Nil(t, view.Init())
	assert.Contains(t, stripAnsi(view.renderContent()), "Tree")

	view = view.SetSource(HelpSourceLog)
	assert.Equal(t, HelpSourceLog, view.source)
	assert.Contains(t, stripAnsi(view.renderContent()), "Log Markers")
}

func TestHelpViewWindowSizingAndSourceRefresh(t *testing.T) {
	view, cmd, request := NewHelpView().Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	assert.Nil(t, cmd)
	assert.Nil(t, request)
	assert.True(t, view.ready)
	assert.Equal(t, 80, view.viewport.Width)
	assert.Equal(t, 19, view.viewport.Height)

	view.viewport.SetYOffset(5)
	view = view.SetSource(HelpSourceLog)
	assert.Zero(t, view.viewport.YOffset)
	assert.Contains(t, stripAnsi(view.viewport.View()), "Scroll up/down")

	view, _, _ = view.Update(tea.WindowSizeMsg{Width: 60, Height: 15})
	assert.Equal(t, 60, view.viewport.Width)
	assert.Equal(t, 14, view.viewport.Height)
}

func TestHelpViewNavigationAndCloseRequests(t *testing.T) {
	view, _, _ := NewHelpView().Update(tea.WindowSizeMsg{Width: 60, Height: 8})
	view = view.SetSource(HelpSourceLog)

	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.GreaterOrEqual(t, view.viewport.YOffset, 0)
	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	assert.Greater(t, view.viewport.YOffset, 0)
	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	view, _, _ = view.Update(tea.KeyMsg{Type: tea.KeyUp})

	for _, key := range []tea.KeyMsg{tea.KeyMsg{Type: tea.KeyEsc}, runeKey("?"), runeKey("q")} {
		_, _, request := view.Update(key)
		requireHelpRequest[CloseHelpRequest](t, request)
	}
}

func TestHelpViewRendering(t *testing.T) {
	treeView := NewHelpView().SetClipboardHint("\n             (no clipboard tool found)")
	rendered := stripAnsi(treeView.View())
	assert.Contains(t, rendered, "GOWT Help")
	assert.Contains(t, rendered, "Navigation")
	assert.Contains(t, rendered, "Toggle filter")
	assert.Contains(t, rendered, "Rerun all tests")
	assert.NotContains(t, rendered, "Rerun all failed tests")
	assert.Contains(t, rendered, "Status Icons")

	logView := treeView.SetSource(HelpSourceLog)
	rendered = stripAnsi(logView.View())
	assert.Contains(t, rendered, "Search")
	assert.Contains(t, rendered, "Toggle view mode")
	assert.Contains(t, rendered, "no clipboard tool found")
}

func TestPadRightUsesVisualWidth(t *testing.T) {
	assert.Equal(t, "abc  ", padRight("abc", 5))
	assert.Equal(t, "界   ", padRight("界", 5))
	assert.Equal(t, "already-long", padRight("already-long", 5))
}

func TestClipboardHintIsInjected(t *testing.T) {
	view := NewHelpView().SetClipboardHint("\n             (injected hint)")
	view = view.SetSource(HelpSourceLog)
	assert.Contains(t, stripAnsi(view.View()), "injected hint")
}

func TestHelpRenderKeyAlignment(t *testing.T) {
	view := NewHelpView()
	line := stripAnsi(view.renderKey("✓", "Passed"))
	assert.True(t, strings.HasSuffix(line, "Passed\n"))
	assert.GreaterOrEqual(t, strings.Index(line, "Passed"), 12)
}
