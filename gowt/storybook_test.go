package main

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	model "github.com/rickchristie/govner/gowt/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStorybookTreeContainsManualQAStates(t *testing.T) {
	tree := newStorybookTree()
	assert.Greater(t, tree.PassedCount, 0)
	assert.Greater(t, tree.FailedCount, 0)
	assert.Greater(t, tree.SkippedCount, 0)
	assert.Greater(t, tree.CachedCount, 0)
	assert.Equal(t, model.StatusFailed, tree.GetNode("example.com/gowt/story/build").Status)
	assert.NotNil(t, tree.GetNode("example.com/gowt/story/skip/FuzzParser/seed#0"))
	allLogs := tree.ProcessedLogBuffer.Slice(model.BufferRef{
		Start: 0, End: tree.ProcessedLogBuffer.Len(),
	})
	assert.Contains(t, allLogs, "9007199254740993")
}

func TestRunStorybookModeUsesInjectedTerminalBoundary(t *testing.T) {
	called := false
	err := runStorybookModeWithProgram(func(teaModel tea.Model) (tea.Model, error) {
		called = true
		app, ok := teaModel.(App)
		require.True(t, ok)
		assert.False(t, app.running)
		assert.Greater(t, app.tree.TotalCount, 0)
		return app, nil
	})
	require.NoError(t, err)
	assert.True(t, called)

	wantErr := errors.New("terminal unavailable")
	err = runStorybookModeWithProgram(func(tea.Model) (tea.Model, error) {
		return nil, wantErr
	})
	require.ErrorIs(t, err, wantErr)
}

func TestStorybookRerunActionsAreDisabled(t *testing.T) {
	app := NewApp(newStorybookTree())
	assert.NotContains(t, app.View(), "Rerun")

	updated, _ := app.Update(runeKeyForApp("r"))
	app = appFromModel(t, updated)
	assert.False(t, app.showRerunModal)

	updated, cmd := app.Update(runeKeyForApp("y"))
	app = appFromModel(t, updated)
	assert.Nil(t, cmd)
	assert.NotPanics(t, func() {
		if cmd != nil {
			_ = cmd()
		}
	})

	updated, _ = app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	app = appFromModel(t, updated)
	require.Equal(t, ScreenLog, app.screen)
	assert.NotContains(t, app.View(), "Rerun")

	updated, _ = app.Update(runeKeyForApp("r"))
	app = appFromModel(t, updated)
	assert.False(t, app.showLogRerunModal)

	updated, _ = app.Update(runeKeyForApp("?"))
	app = appFromModel(t, updated)
	require.Equal(t, ScreenHelp, app.screen)
	assert.NotContains(t, app.View(), "Rerun this test")
}
