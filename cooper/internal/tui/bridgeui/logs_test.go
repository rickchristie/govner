package bridgeui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/tui/events"
)

type logCopier struct{ text string }

func (c *logCopier) CopyText(_ context.Context, text string) error { c.text = text; return nil }

func TestExecutionCopyIncludesHiddenOutputAndKeepsSelectedRecord(t *testing.T) {
	c := &logCopier{}
	m := NewLogsModel(2, c)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	output := strings.Repeat("long output ", 30)
	m.Update(events.BridgeLogMsg{Log: app.ExecutionLog{Route: "/first", Stdout: output, Stderr: "failure detail"}})
	m.Update(events.BridgeLogMsg{Log: app.ExecutionLog{Route: "/second", Stdout: "later"}})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd == nil {
		t.Fatal("copy command is missing")
	}
	m.Update(cmd())
	for _, want := range []string{"/first", output, "failure detail"} {
		if !strings.Contains(c.text, want) {
			t.Fatalf("copy is missing %q", want)
		}
	}
	m.Update(events.BridgeLogMsg{Log: app.ExecutionLog{Route: "/third"}})
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m.Update(cmd())
	if !strings.Contains(c.text, output) {
		t.Fatal("new record replaced the open output")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.showDetail {
		t.Fatal("Esc did not return to execution list")
	}
}
