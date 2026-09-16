package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/tui"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

func TestMainScreensFitAndKeepCombinedPaneControls(t *testing.T) {
	for _, size := range [][2]int{{120, 36}, {80, 24}} {
		cfg := config.DefaultConfig()
		cfg.BridgeRoutes = []config.BridgeRoute{{APIPath: "/run", ScriptPath: "/scripts/run.sh"}}
		a := app.NewTestApp(cfg, nil, nil)
		m := tui.NewModel(a)
		now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
		wireMainScreens(m, a, cfg, cfg, func() time.Time { return now })
		seedTUIRecords(m, now)
		m.SetSize(size[0], size[1])
		for _, test := range []struct {
			tab  theme.TabID
			text []string
		}{
			{theme.TabHistory, []string{"Allowed", "Blocked"}},
			{theme.TabSquidLogs, []string{"package-259", "y Copy"}},
			{theme.TabBridge, []string{"Routes", "Bridge Logs", "/run", "Enter Output"}},
			{theme.TabRuntime, []string{"Settings", "About", "Cooper v"}},
		} {
			m.SetActiveTab(test.tab)
			view := m.View()
			if len(strings.Split(view, "\n")) != size[1] {
				t.Fatalf("tab %v exceeds height %v", test.tab, size)
			}
			for _, line := range strings.Split(view, "\n") {
				if lipgloss.Width(line) > size[0] {
					t.Fatalf("tab %v exceeds width %v", test.tab, size)
				}
			}
			for _, want := range test.text {
				if !strings.Contains(view, want) {
					t.Fatalf("tab %v at %v lacks %q:\n%s", test.tab, size, want, view)
				}
			}
		}
		m.SetActiveTab(theme.TabBridge)
		m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if !strings.Contains(m.View(), "Save") {
			t.Fatalf("route editor hides Save at %v:\n%s", size, m.View())
		}
	}
}
