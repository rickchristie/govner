package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

func TestNarrowTabBarKeepsEveryActiveTabVisible(t *testing.T) {
	for _, tab := range theme.AllTabs {
		for _, width := range []int{30, 60, 80} {
			bar := NewTabBar(theme.AllTabs, tab.ID)
			bar.Width = width
			view := bar.View()
			if !strings.Contains(view, tab.Label) || lipgloss.Width(view) > width {
				t.Fatalf("active tab %s at %d: %q", tab.Label, width, view)
			}
		}
	}
}
