package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// padToWidth pads or truncates a string to exactly targetWidth visible characters.
// Uses lipgloss for ANSI-aware width measurement and truncation.
func padToWidth(s string, targetWidth int) string {
	visW := lipgloss.Width(s)
	if visW > targetWidth {
		// Truncate to fit — use lipgloss to handle ANSI codes properly.
		return lipgloss.NewStyle().MaxWidth(targetWidth).Render(s)
	}
	if visW < targetWidth {
		return s + strings.Repeat(" ", targetWidth-visW)
	}
	return s
}

// ScrollableContent is a viewport for free-form text content with scrollbar
// and mouse wheel support. Unlike ScrollableList (which manages selectable
// items), this wraps arbitrary pre-rendered string content and handles
// vertical scrolling through it.
type ScrollableContent struct {
	ScrollOffset int
	lines        []string
	totalLines   int
}

// SetContent splits rendered content into lines for viewport display.
func (v *ScrollableContent) SetContent(content string) {
	v.lines = strings.Split(content, "\n")
	v.totalLines = len(v.lines)
	v.clampScroll(0) // ensure offset is valid
}

// View renders the visible portion of content with a scrollbar.
// width and height define the available area.
func (v *ScrollableContent) View(width, height int) string {
	if height <= 0 {
		return ""
	}

	needsScroll := v.totalLines > height
	contentWidth := width
	if needsScroll {
		contentWidth = width - 1 // reserve 1 column for scrollbar
	}

	// Extract visible lines.
	start := v.ScrollOffset
	end := start + height
	if end > v.totalLines {
		end = v.totalLines
	}

	// Pad each visible line to exactly contentWidth visible characters.
	// We use manual space-padding instead of lipgloss.Width() to avoid
	// interfering with pre-styled content (box borders, tables, etc.).
	var visibleLines []string
	for i := start; i < end; i++ {
		visibleLines = append(visibleLines, padToWidth(v.lines[i], contentWidth))
	}

	// Pad to fill height.
	emptyLine := strings.Repeat(" ", contentWidth)
	for len(visibleLines) < height {
		visibleLines = append(visibleLines, emptyLine)
	}

	if !needsScroll {
		return strings.Join(visibleLines, "\n")
	}

	// Build scrollbar column.
	scrollCol := buildScrollIndicator(v.totalLines, v.ScrollOffset, height)

	// Merge content and scrollbar at fixed right edge.
	var rows []string
	for i := 0; i < height; i++ {
		rows = append(rows, visibleLines[i]+scrollCol[i])
	}
	return strings.Join(rows, "\n")
}

// HandleKey processes up/down/pgup/pgdown keys. Returns true if handled.
func (v *ScrollableContent) HandleKey(msg tea.KeyMsg, height int) bool {
	switch msg.String() {
	case "up", "k":
		v.clampScroll(height)
		if v.ScrollOffset > 0 {
			v.ScrollOffset--
			return true
		}
	case "down", "j":
		v.clampScroll(height)
		maxOffset := v.totalLines - height
		if maxOffset < 0 {
			maxOffset = 0
		}
		if v.ScrollOffset < maxOffset {
			v.ScrollOffset++
			return true
		}
	case "pgup", "ctrl+u":
		v.ScrollOffset -= height / 2
		v.clampScroll(height)
		return true
	case "pgdown", "ctrl+d":
		v.ScrollOffset += height / 2
		v.clampScroll(height)
		return true
	case "home", "g":
		v.ScrollOffset = 0
		return true
	case "end", "G":
		maxOffset := v.totalLines - height
		if maxOffset < 0 {
			maxOffset = 0
		}
		v.ScrollOffset = maxOffset
		return true
	}
	return false
}

// HandleMouse processes mouse wheel events. Returns true if handled.
func (v *ScrollableContent) HandleMouse(msg tea.MouseMsg, height int) bool {
	switch {
	case msg.Button == tea.MouseButtonWheelUp:
		if v.ScrollOffset > 0 {
			v.ScrollOffset -= 3
			v.clampScroll(height)
			return true
		}
	case msg.Button == tea.MouseButtonWheelDown:
		maxOffset := v.totalLines - height
		if maxOffset < 0 {
			maxOffset = 0
		}
		if v.ScrollOffset < maxOffset {
			v.ScrollOffset += 3
			v.clampScroll(height)
			return true
		}
	}
	return false
}

// EnsureLineVisible adjusts ScrollOffset so the given line index is visible
// within the viewport of the given height.
func (v *ScrollableContent) EnsureLineVisible(line, height int) {
	if line < v.ScrollOffset {
		v.ScrollOffset = line
	} else if line >= v.ScrollOffset+height {
		v.ScrollOffset = line - height + 1
	}
	v.clampScroll(height)
}

// clampScroll ensures ScrollOffset is within valid bounds.
func (v *ScrollableContent) clampScroll(height int) {
	if v.ScrollOffset < 0 {
		v.ScrollOffset = 0
	}
	maxOffset := v.totalLines - height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if v.ScrollOffset > maxOffset {
		v.ScrollOffset = maxOffset
	}
}
