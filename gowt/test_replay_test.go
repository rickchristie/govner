package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rickchristie/govner/gowt/model"
	"github.com/rickchristie/govner/gowt/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The trace keeps the event order from a parallel run. Slashes in a case name
// create tree groups without their own Go events. A package failure must not
// turn those groups into extra failed tests.
func TestSlashNameReplayKeepsOnlyTheFailedBranch(t *testing.T) {
	path := filepath.Join("testdata", "replay", "slash-name-package-failure.jsonl")
	t.Run("load", func(t *testing.T) {
		tree, err := loadTestResults(path)
		require.NoError(t, err)
		assertSlashNameReplay(t, NewApp(tree))
	})

	t.Run("live", func(t *testing.T) {
		file, err := os.Open(path)
		require.NoError(t, err)
		defer file.Close()

		app := NewLiveApp(nil, nil)
		decoder := json.NewDecoder(file)
		for decoder.More() {
			var event model.TestEvent
			require.NoError(t, decoder.Decode(&event))
			updated, _ := app.Update(TestEventMsg{Event: event, RunGen: app.runGen})
			app = appFromModel(t, updated)
		}
		updated, _ := app.Update(TestDoneMsg{ExitCode: 1, RunGen: app.runGen})
		app = appFromModel(t, updated)
		assert.Equal(t, 1, app.exitCode)
		assertSlashNameReplay(t, app)
	})
}

func assertSlashNameReplay(t *testing.T, app App) {
	t.Helper()
	const pkg = "example.com/replay/service"
	assert.Equal(t, 24, app.tree.TotalCount)
	assert.Equal(t, 21, app.tree.PassedCount)
	assert.Equal(t, 3, app.tree.FailedCount)
	assert.Zero(t, app.tree.RunningCount)

	var failed []string
	for path, node := range app.tree.NodeIndex {
		if node.Status == model.StatusFailed {
			failed = append(failed, path)
		}
	}
	assert.ElementsMatch(t, []string{
		pkg,
		pkg + "/TestLookup",
		pkg + "/TestLookup/#00",
		pkg + "/TestLookup/#00/returns_records",
	}, failed)

	group := app.tree.GetNode(pkg + "/TestAccess/#00/accepts_read")
	require.NotNil(t, group)
	assert.Equal(t, model.StatusPassed, group.Status)
	assert.Equal(t, group.TotalCount, group.PassedCount)
	assert.Zero(t, group.FailedCount)

	updated, _ := app.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	app = appFromModel(t, updated)
	updated, _ = app.Update(tea.KeyMsg{Type: tea.KeySpace})
	app = appFromModel(t, updated)
	updated, _ = app.Update(runeKeyForApp("e"))
	app = appFromModel(t, updated)
	output := util.StripANSI(app.View())
	assert.Contains(t, output, "Space Focus")
	assert.Contains(t, output, "returns_records")
	assert.NotContains(t, output, "TestAccess")
	assert.NotContains(t, output, "empty_query")
}
