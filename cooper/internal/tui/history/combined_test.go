package history

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/tui/events"
)

func TestCombinedHistoryKeepsBothLimitsAndFilters(t *testing.T) {
	m := NewCombined(2, 1)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	for i := 0; i < 6; i++ {
		decision := app.DecisionDeny
		if i%2 == 0 {
			decision = app.DecisionAllow
		}
		m.Update(events.ACLDecisionMsg{Event: app.DecisionEvent{Request: app.ACLRequest{ID: fmt.Sprint(i), Domain: fmt.Sprintf("host-%d.test", i)}, Decision: decision}})
	}
	if len(m.Items) != 3 || len(m.list.Items) != 3 {
		t.Fatalf("history = %+v", m.Items)
	}
	view := m.View(100, 20)
	if !strings.Contains(view, "Allowed") || !strings.Contains(view, "Blocked") {
		t.Fatal("combined history does not show both results")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if len(m.list.Items) != 2 {
		t.Fatal("blocked filter must retain two records")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if len(m.list.Items) != 1 || !m.selectedEntry().Allowed {
		t.Fatal("allowed filter must retain one record")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.ModalActive() || !strings.Contains(m.View(100, 24), "host-4.test") {
		t.Fatal("filtered details are unavailable")
	}
}
