package squidlog

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rickchristie/govner/cooper/internal/tui/events"
)

func TestLogTabOpensAtLatestBoundedHistory(t *testing.T) {
	m := New()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	for i := 0; i < 600; i++ {
		m.Update(events.SquidLogLineMsg{Line: fmt.Sprintf("line-%03d", i)})
	}
	m.Update(tea.KeyMsg{Type: tea.KeyHome})
	view := m.View(80, 10)
	if !strings.Contains(view, "line-400") || strings.Contains(view, "line-399") {
		t.Fatal("history does not start at the final 200 records")
	}
	m.Update(events.TabActivatedMsg{})
	if !strings.Contains(m.View(80, 10), "line-599") {
		t.Fatal("opening tab must show latest records")
	}
}
