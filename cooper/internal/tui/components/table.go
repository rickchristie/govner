package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// ListItem is a single entry in a ScrollableList.
type ListItem struct {
	ID      string
	Columns []string
	Data    interface{}
}

// ScrollableList is a vertical list with cursor selection and scroll
// management. The caller supplies a renderRow callback for custom
// per-row rendering.
type ScrollableList struct {
	Items        []ListItem
	SelectedIdx  int
	ScrollOffset int
	Height       int
	Width        int
}

// NewScrollableList creates an empty scrollable list.
func NewScrollableList(height, width int) ScrollableList {
	return ScrollableList{
		Height: height,
		Width:  width,
	}
}

// SetItems replaces the item list and clamps selection/scroll state.
func (l *ScrollableList) SetItems(items []ListItem) {
	l.Items = items
	if l.SelectedIdx >= len(items) {
		if len(items) > 0 {
			l.SelectedIdx = len(items) - 1
		} else {
			l.SelectedIdx = 0
		}
	}
	l.clampScroll()
}

// MoveUp moves the selection cursor up by one, scrolling if needed.
func (l *ScrollableList) MoveUp() {
	if l.SelectedIdx > 0 {
		l.SelectedIdx--
		l.clampScroll()
	}
}

// MoveDown moves the selection cursor down by one, scrolling if needed.
func (l *ScrollableList) MoveDown() {
	if l.SelectedIdx < len(l.Items)-1 {
		l.SelectedIdx++
		l.clampScroll()
	}
}

// Selected returns the currently selected item, or nil if the list is empty.
func (l ScrollableList) Selected() *ListItem {
	if len(l.Items) == 0 || l.SelectedIdx < 0 || l.SelectedIdx >= len(l.Items) {
		return nil
	}
	return &l.Items[l.SelectedIdx]
}

// View renders the visible portion of the list. renderRow is called for
// each visible item and must return a single-line string.
func (l ScrollableList) View(renderRow func(item ListItem, selected bool, width int) string) string {
	if len(l.Items) == 0 {
		return ""
	}

	visibleHeight := l.Height
	if visibleHeight <= 0 {
		visibleHeight = len(l.Items)
	}

	needsScroll := len(l.Items) > visibleHeight
	contentWidth := l.Width
	if needsScroll {
		// Reserve 1 column for the scroll indicator.
		contentWidth = l.Width - 1
	}
	if contentWidth < 1 {
		contentWidth = 1
	}

	end := l.ScrollOffset + visibleHeight
	if end > len(l.Items) {
		end = len(l.Items)
	}

	var rows []string
	for i := l.ScrollOffset; i < end; i++ {
		selected := i == l.SelectedIdx
		row := renderRow(l.Items[i], selected, contentWidth)
		rows = append(rows, row)
	}

	if !needsScroll {
		return strings.Join(rows, "\n")
	}

	// Build scroll indicator column.
	scrollCol := buildScrollIndicator(len(l.Items), l.ScrollOffset, visibleHeight)

	// Combine rows with scroll indicator.
	var combined []string
	for i, row := range rows {
		indicator := " "
		if i < len(scrollCol) {
			indicator = scrollCol[i]
		}
		combined = append(combined, row+indicator)
	}

	return strings.Join(combined, "\n")
}

// ClampScroll ensures the selected item is visible within the scroll window.
// Call this after directly setting SelectedIdx to keep the viewport in sync.
func (l *ScrollableList) ClampScroll() {
	l.clampScroll()
}

// HandleMouse processes mouse wheel events, scrolling by 3 lines per tick.
// Returns true if the event was handled.
func (l *ScrollableList) HandleMouse(msg tea.MouseMsg) bool {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		for i := 0; i < 3; i++ {
			l.MoveUp()
		}
		return true
	case tea.MouseButtonWheelDown:
		for i := 0; i < 3; i++ {
			l.MoveDown()
		}
		return true
	}
	return false
}

// clampScroll ensures the selected item is visible within the scroll window.
func (l *ScrollableList) clampScroll() {
	if l.Height <= 0 {
		l.ScrollOffset = 0
		return
	}

	// Ensure selected is below the scroll top.
	if l.SelectedIdx < l.ScrollOffset {
		l.ScrollOffset = l.SelectedIdx
	}

	// Ensure selected is above the scroll bottom.
	if l.SelectedIdx >= l.ScrollOffset+l.Height {
		l.ScrollOffset = l.SelectedIdx - l.Height + 1
	}

	// Clamp offset to valid range.
	maxOffset := len(l.Items) - l.Height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if l.ScrollOffset > maxOffset {
		l.ScrollOffset = maxOffset
	}
	if l.ScrollOffset < 0 {
		l.ScrollOffset = 0
	}
}

// buildScrollIndicator creates the vertical scroll track for the given
// list geometry. Returns one string per visible row.
func buildScrollIndicator(totalItems, scrollOffset, visibleHeight int) []string {
	if totalItems <= visibleHeight || visibleHeight <= 0 {
		result := make([]string, visibleHeight)
		for i := range result {
			result[i] = " "
		}
		return result
	}

	arrowUp := lipgloss.NewStyle().Foreground(theme.ColorDusty).Render("\u25b2")
	arrowDown := lipgloss.NewStyle().Foreground(theme.ColorDusty).Render("\u25bc")
	track := lipgloss.NewStyle().Foreground(theme.ColorStave).Render(theme.IconShade)
	thumb := lipgloss.NewStyle().Foreground(theme.ColorDusty).Render(theme.IconBlock)

	// Track area is between the arrows.
	trackHeight := visibleHeight - 2
	if trackHeight < 1 {
		trackHeight = 1
	}

	// Thumb size: proportional to visible / total, minimum 1.
	thumbSize := (visibleHeight * trackHeight) / totalItems
	if thumbSize < 1 {
		thumbSize = 1
	}

	// Thumb position.
	maxOffset := totalItems - visibleHeight
	if maxOffset < 1 {
		maxOffset = 1
	}
	thumbPos := (scrollOffset * (trackHeight - thumbSize)) / maxOffset
	if thumbPos < 0 {
		thumbPos = 0
	}
	if thumbPos+thumbSize > trackHeight {
		thumbPos = trackHeight - thumbSize
	}

	col := make([]string, visibleHeight)
	col[0] = arrowUp
	for i := 0; i < trackHeight; i++ {
		if i >= thumbPos && i < thumbPos+thumbSize {
			col[i+1] = thumb
		} else {
			col[i+1] = track
		}
	}
	col[visibleHeight-1] = arrowDown

	return col
}
