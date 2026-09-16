package components

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

type paneRecorder struct {
	messages []tea.Msg
	modal    bool
	text     string
}

func (p *paneRecorder) Init() tea.Cmd { return nil }
func (p *paneRecorder) Update(msg tea.Msg) (theme.SubModel, tea.Cmd) {
	p.messages = append(p.messages, msg)
	return p, nil
}
func (p *paneRecorder) View(w, h int) string { return FixedFrame{Width: w, Height: h}.View(p.text) }
func (p *paneRecorder) ModalActive() bool    { return p.modal }

func TestSplitFocusCoordinatesAndInactiveEvents(t *testing.T) {
	a, b := &paneRecorder{text: "routes"}, &paneRecorder{text: "logs"}
	m := NewSplit("Routes", a, "Logs", b)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	a.messages, b.messages = nil, nil
	m.Update(tea.MouseMsg{X: 51, Y: 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if len(a.messages) != 0 || len(b.messages) != 1 {
		t.Fatal("mouse went to the wrong pane")
	}
	got := b.messages[0].(tea.MouseMsg)
	if got.X != 3 || got.Y != 2 {
		t.Fatalf("pane coordinates = %d,%d", got.X, got.Y)
	}
	m.Update(TextCopiedMsg{Target: "bridge"})
	if len(a.messages) != 1 || len(b.messages) != 2 {
		t.Fatal("background result did not reach both children")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(a.messages) != 2 {
		t.Fatal("pane key did not change focus")
	}
	a.modal = true
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	if !m.ModalActive() || m.focus != 0 {
		t.Fatal("pane focus escaped an open modal")
	}
	if strings.Contains(m.View(120, 24), "logs") {
		t.Fatal("modal was limited to its original pane")
	}
}

func TestSplitViewsFitAndDoNotChangeInteractionState(t *testing.T) {
	m := NewSplit("Routes", &paneRecorder{text: "one"}, "Logs", &paneRecorder{text: "two"})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	for _, size := range [][2]int{{120, 30}, {80, 18}, {40, 10}} {
		view := m.View(size[0], size[1])
		if len(strings.Split(view, "\n")) != size[1] {
			t.Fatalf("wrong frame height at %v", size)
		}
		for _, line := range strings.Split(view, "\n") {
			if lipgloss.Width(line) > size[0] {
				t.Fatalf("line exceeds frame at %v", size)
			}
		}
		if !strings.Contains(view, "Routes") || !strings.Contains(view, "Logs") {
			t.Fatalf("pane header missing at %v", size)
		}
	}
	if m.width != 120 || m.height != 30 {
		t.Fatal("View changed mouse geometry")
	}
}
