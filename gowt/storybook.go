package main

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	model "github.com/rickchristie/govner/gowt/model"
)

// runStorybookModeWithProgram uses the production terminal startup path while
// allowing tests to inspect the deterministic fixture without opening a TTY.
func runStorybookModeWithProgram(run func(tea.Model) (tea.Model, error)) error {
	if _, err := run(NewApp(newStorybookTree())); err != nil {
		return fmt.Errorf("error running storybook: %w", err)
	}
	return nil
}

// newStorybookTree exercises the states that are easy to miss during ordinary
// runs: nested failures, cache indicators, skips, structured logs, Unicode,
// blank lines, long wrapping, and build errors. Keep it deterministic so a
// screenshot or manual comparison means the same thing across machines.
func newStorybookTree() *model.TestTree {
	tree := model.NewTestTree()
	events := []model.TestEvent{
		{Action: "run", Package: "example.com/gowt/story/pass", Test: "TestExpectedError"},
		{Action: "output", Package: "example.com/gowt/story/pass", Test: "TestExpectedError", Output: "{\"level\":\"error\",\"message\":\"expected service failure\",\"request_id\":9007199254740993}\n"},
		{Action: "pass", Package: "example.com/gowt/story/pass", Test: "TestExpectedError", Elapsed: 0.12},
		{Action: "pass", Package: "example.com/gowt/story/pass", Elapsed: 0.14},

		{Action: "run", Package: "example.com/gowt/story/fail", Test: "TestCheckout/card/declined"},
		{Action: "output", Package: "example.com/gowt/story/fail", Test: "TestCheckout/card/declined", Output: "expected success\n\nreceived: 支払い失敗 🙂\n"},
		{Action: "output", Package: "example.com/gowt/story/fail", Test: "TestCheckout/card/declined", Output: "a deliberately long diagnostic line that demonstrates terminal-cell-aware wrapping without splitting Unicode grapheme clusters\n"},
		{Action: "fail", Package: "example.com/gowt/story/fail", Test: "TestCheckout/card/declined", Elapsed: 0.31},
		{Action: "fail", Package: "example.com/gowt/story/fail", Elapsed: 0.34},

		{Action: "run", Package: "example.com/gowt/story/skip", Test: "FuzzParser/seed#0"},
		{Action: "skip", Package: "example.com/gowt/story/skip", Test: "FuzzParser/seed#0", Elapsed: 0.01},
		{Action: "skip", Package: "example.com/gowt/story/skip"},

		{Action: "run", Package: "example.com/gowt/story/cached", Test: "ExampleClient"},
		{Action: "pass", Package: "example.com/gowt/story/cached", Test: "ExampleClient", Elapsed: 0.02},
		{Action: "output", Package: "example.com/gowt/story/cached", Output: "ok  \texample.com/gowt/story/cached\t(cached)\n"},

		{Action: "build-output", ImportPath: "example.com/gowt/story/build", Output: "./broken.go:8:2: undefined: missingSymbol\n"},
		{Action: "build-fail", ImportPath: "example.com/gowt/story/build"},
	}
	for _, event := range events {
		tree.ProcessEvent(event)
	}
	tree.FlushOutputBuffers()
	tree.Elapsed = 0.84
	for _, pkg := range tree.Packages {
		pkg.Expanded = true
		for _, child := range pkg.Children {
			child.Expanded = true
		}
	}
	return tree
}
