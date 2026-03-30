package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// TabBar renders a horizontal tab bar with active/inactive styling.
type TabBar struct {
	Tabs      []theme.TabInfo
	ActiveTab theme.TabID
	Width     int
}

// NewTabBar creates a tab bar from the given tab list with the specified
// active tab.
func NewTabBar(tabs []theme.TabInfo, active theme.TabID) TabBar {
	return TabBar{
		Tabs:      tabs,
		ActiveTab: active,
	}
}

// View renders the tab bar as a single line with active tab underlined.
func (t TabBar) View() string {
	var labels []string

	for _, tab := range t.Tabs {
		icon := tab.Icon + " "
		if tab.ID == t.ActiveTab {
			// Only underline the label text, not the icon.
			labels = append(labels, theme.TabActiveStyle.Render(icon)+theme.TabActiveStyle.Underline(true).Render(tab.Label))
		} else {
			labels = append(labels, theme.TabInactiveStyle.Render(icon+tab.Label))
		}
	}

	row := strings.Join(labels, "  ")

	// Truncate to terminal width if necessary.
	if t.Width > 0 {
		row = truncateToWidth(row, t.Width)
	}

	return row
}

// SetActive changes the active tab.
func (t *TabBar) SetActive(tab theme.TabID) {
	t.ActiveTab = tab
}

// Next cycles to the next tab. Wraps around at the end.
func (t *TabBar) Next() {
	if len(t.Tabs) == 0 {
		return
	}
	for i, tab := range t.Tabs {
		if tab.ID == t.ActiveTab {
			t.ActiveTab = t.Tabs[(i+1)%len(t.Tabs)].ID
			return
		}
	}
	// Active tab not found; reset to first.
	t.ActiveTab = t.Tabs[0].ID
}

// Prev cycles to the previous tab. Wraps around at the beginning.
func (t *TabBar) Prev() {
	if len(t.Tabs) == 0 {
		return
	}
	for i, tab := range t.Tabs {
		if tab.ID == t.ActiveTab {
			prev := (i - 1 + len(t.Tabs)) % len(t.Tabs)
			t.ActiveTab = t.Tabs[prev].ID
			return
		}
	}
	t.ActiveTab = t.Tabs[0].ID
}

// truncateToWidth trims a rendered string to fit within maxWidth.
// This is a best-effort approach using lipgloss width measurement.
func truncateToWidth(s string, maxWidth int) string {
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	// Truncate rune-by-rune until it fits.
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes)) > maxWidth {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}
