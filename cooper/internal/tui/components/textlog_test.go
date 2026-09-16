package components

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type textRecorder struct {
	text string
	err  error
}

func (r *textRecorder) CopyText(ctx context.Context, text string) error {
	if _, ok := ctx.Deadline(); !ok {
		panic("copy requires a deadline")
	}
	r.text = text
	return r.err
}

func TestTextLogDragCopiesFullLinesAndPausesFollow(t *testing.T) {
	r := &textRecorder{}
	v := NewTextLog("test", 200, r)
	v.Update(tea.WindowSizeMsg{Width: 24, Height: 8})
	v.SetContent("first\n\x1b[31m日本語 " + strings.Repeat("long", 30) + "\x1b[0m\nthird")
	v.Update(tea.MouseMsg{X: 3, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	v.Update(tea.MouseMsg{X: 3, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	v.Update(tea.MouseMsg{Action: tea.MouseActionRelease})
	if v.Follow {
		t.Fatal("selection must pause following")
	}
	cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd == nil {
		t.Fatal("copy command is missing")
	}
	// The command owns its selection snapshot while new output arrives.
	v.AppendLine("later")
	v.Update(cmd())
	want := "日本語 " + strings.Repeat("long", 30) + "\nthird"
	if r.text != want {
		t.Fatalf("copied %q, want complete selected lines %q", r.text, want)
	}
	if !strings.Contains(v.View(24, 8), "Copied 2 line(s)") {
		t.Fatal("copy result is hidden")
	}
	v.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if !v.Follow || v.selected {
		t.Fatal("End must resume following and clear selection")
	}
}

func TestTextLogEvictionKeepsSelectionOrClearsIt(t *testing.T) {
	r := &textRecorder{}
	v := NewTextLog("test", 3, r)
	v.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	v.SetContent("one\ntwo\nthree")
	v.Update(tea.MouseMsg{Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	v.AppendLine("four")
	cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	v.Update(cmd())
	if r.text != "two" {
		t.Fatalf("append moved selection to %q", r.text)
	}
	v.AppendLine("five")
	if v.selected {
		t.Fatal("evicted selection must be cleared")
	}
	if v.viewport.TotalLines() != 3 {
		t.Fatal("history limit was exceeded")
	}
}

func TestTextLogCopyErrorAndPureView(t *testing.T) {
	v := NewTextLog("test", 200, &textRecorder{err: errors.New("clipboard denied")})
	v.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	v.SetContent("one\ntwo\nthree\nfour\nfive\nsix\nseven")
	v.SelectAll()
	before := v.viewport.ScrollOffset
	v.View(20, 4)
	if v.viewport.ScrollOffset != before {
		t.Fatal("View changed the scroll offset")
	}
	cmd := v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	v.Update(cmd())
	if !strings.Contains(v.View(80, 8), "Copy failed: clipboard denied") {
		t.Fatal("copy error is hidden")
	}
	status := v.status
	v.Update(TextCopiedMsg{Target: "another"})
	if v.status != status {
		t.Fatal("another log's result replaced this status")
	}
}
