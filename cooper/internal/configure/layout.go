package configure

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// mouseScrollLines is the number of lines scrolled per mouse wheel tick.
const mouseScrollLines = 3

// handleMouseScroll checks for mouse wheel events and adjusts scrollOffset.
// Returns true if the event was a wheel event that was handled.
func handleMouseScroll(msg tea.MouseMsg, scrollOffset *int, maxScroll int) bool {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		*scrollOffset -= mouseScrollLines
		if *scrollOffset < 0 {
			*scrollOffset = 0
		}
		return true
	case tea.MouseButtonWheelDown:
		*scrollOffset += mouseScrollLines
		if *scrollOffset > maxScroll {
			*scrollOffset = maxScroll
		}
		return true
	}
	return false
}

// layout renders a 3-row layout: fixed header, scrollable main content, fixed footer.
// The main content area takes all remaining height after header and footer.
// If content exceeds the available height, it is scrollable and the footer shows scroll percentage.
type layout struct {
	header       string // fixed top rows
	content      string // scrollable middle (may be taller than available)
	footer       string // fixed bottom rows (help bar)
	width        int
	height       int
	scrollOffset int
	hideTopSep   bool // skip the separator between header and content
}

// newLayout creates a layout with scrollOffset=0.
func newLayout(header, content, footer string, width, height int) *layout {
	return &layout{
		header:  header,
		content: content,
		footer:  footer,
		width:   width,
		height:  height,
	}
}

// ScrollDown scrolls content down by n lines, clamped to max.
func (l *layout) ScrollDown(n int) {
	l.scrollOffset += n
	maxOffset := l.maxScrollOffset()
	if l.scrollOffset > maxOffset {
		l.scrollOffset = maxOffset
	}
}

// ScrollUp scrolls content up by n lines, clamped to 0.
func (l *layout) ScrollUp(n int) {
	l.scrollOffset -= n
	if l.scrollOffset < 0 {
		l.scrollOffset = 0
	}
}

// ScrollToBottom scrolls to end.
func (l *layout) ScrollToBottom() {
	l.scrollOffset = l.maxScrollOffset()
}

// ScrollToTop scrolls to top.
func (l *layout) ScrollToTop() {
	l.scrollOffset = 0
}

// ContentHeight returns the available height for content.
func (l *layout) ContentHeight() int {
	headerLines := countLines(l.header)
	footerLines := countLines(l.footer)
	sepCount := 2
	if l.hideTopSep {
		sepCount = 1
	}
	avail := l.height - headerLines - footerLines - sepCount
	if avail < 0 {
		avail = 0
	}
	return avail
}

// TotalContentLines returns the total number of lines in the content.
func (l *layout) TotalContentLines() int {
	if l.content == "" {
		return 0
	}
	return countLines(l.content)
}

// NeedsScroll returns true if the content overflows the available height.
func (l *layout) NeedsScroll() bool {
	return l.TotalContentLines() > l.ContentHeight()
}

// MaxScrollOffset returns the maximum valid scroll offset for the current content/height.
func (l *layout) MaxScrollOffset() int {
	return l.maxScrollOffset()
}

// Render renders the 3-row layout.
func (l *layout) Render() string {
	headerLines := strings.Split(l.header, "\n")
	contentLines := splitContentLines(l.content)
	footerLines := strings.Split(l.footer, "\n")

	// Available height = total - header - footer - separator lines (1 or 2).
	sepCount := 2
	if l.hideTopSep {
		sepCount = 1
	}
	availHeight := l.height - len(headerLines) - len(footerLines) - sepCount
	if availHeight < 0 {
		availHeight = 0
	}

	totalContent := len(contentLines)

	// Clamp scroll offset.
	maxOffset := totalContent - availHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if l.scrollOffset > maxOffset {
		l.scrollOffset = maxOffset
	}
	if l.scrollOffset < 0 {
		l.scrollOffset = 0
	}

	var out []string

	// Header-content separator.
	sep := lipgloss.NewStyle().Foreground(theme.ColorOakLight).Render(strings.Repeat("─", l.width))

	// 1. Header, optionally followed by separator.
	out = append(out, headerLines...)
	if !l.hideTopSep {
		out = append(out, sep)
	}

	// 2. Content lines (scrollable region).
	if totalContent <= availHeight {
		// Content fits: render all, pad remaining space with empty lines.
		out = append(out, contentLines...)
		for i := totalContent; i < availHeight; i++ {
			out = append(out, "")
		}
	} else {
		// Content overflows: render only the visible window.
		end := l.scrollOffset + availHeight
		if end > totalContent {
			end = totalContent
		}
		out = append(out, contentLines[l.scrollOffset:end]...)
		// Pad if needed (shouldn't normally happen, but be safe).
		rendered := end - l.scrollOffset
		for i := rendered; i < availHeight; i++ {
			out = append(out, "")
		}
	}

	// Content-footer separator.
	out = append(out, sep)

	// 3. Footer with optional scroll indicator.
	if l.NeedsScroll() {
		footerWithScroll := l.appendScrollIndicator(footerLines)
		out = append(out, footerWithScroll...)
	} else {
		out = append(out, footerLines...)
	}

	return strings.Join(out, "\n")
}

// maxScrollOffset returns the maximum scroll offset.
func (l *layout) maxScrollOffset() int {
	max := l.TotalContentLines() - l.ContentHeight()
	if max < 0 {
		return 0
	}
	return max
}

// appendScrollIndicator adds scroll percentage to the right side of the last footer line.
func (l *layout) appendScrollIndicator(footerLines []string) []string {
	if len(footerLines) == 0 {
		return footerLines
	}

	result := make([]string, len(footerLines))
	copy(result, footerLines)

	// Calculate scroll indicator text.
	indicator := l.scrollIndicatorText()
	styledIndicator := lipgloss.NewStyle().Foreground(theme.ColorDusty).Render(indicator)

	// Append to the last footer line, right-aligned.
	lastIdx := len(result) - 1
	lastLine := result[lastIdx]
	lastLineWidth := lipgloss.Width(lastLine)
	indicatorWidth := lipgloss.Width(styledIndicator)

	// Use width-1 to account for terminal scrollbar/margin in some terminals (e.g., VS Code).
	gap := l.width - lastLineWidth - indicatorWidth - 1
	if gap < 1 {
		gap = 1
	}

	result[lastIdx] = lastLine + strings.Repeat(" ", gap) + styledIndicator
	return result
}

// scrollIndicatorText returns "Top", "Bot", or a percentage string.
func (l *layout) scrollIndicatorText() string {
	maxOffset := l.maxScrollOffset()
	if maxOffset <= 0 {
		return "Top"
	}
	if l.scrollOffset <= 0 {
		return "Top"
	}
	if l.scrollOffset >= maxOffset {
		return "Bot"
	}
	pct := (l.scrollOffset * 100) / maxOffset
	return fmt.Sprintf("%d%%", pct)
}

// EnsureVisible adjusts the scroll offset so that the given line index is visible
// within the content area. Used to auto-scroll to keep cursor/selection visible.
func (l *layout) EnsureVisible(lineIndex int) {
	avail := l.ContentHeight()
	if avail <= 0 {
		return
	}
	// If above the visible window, scroll up.
	if lineIndex < l.scrollOffset {
		l.scrollOffset = lineIndex
	}
	// If below the visible window, scroll down.
	if lineIndex >= l.scrollOffset+avail {
		l.scrollOffset = lineIndex - avail + 1
	}
	// Clamp.
	maxOffset := l.maxScrollOffset()
	if l.scrollOffset > maxOffset {
		l.scrollOffset = maxOffset
	}
	if l.scrollOffset < 0 {
		l.scrollOffset = 0
	}
}

// ensureLineVisible adjusts scrollOffset so that lineIndex is visible within availableHeight.
// This is a standalone function callable from Update() without needing a full layout.
// headerLines is the number of fixed header lines (to estimate available space from total height).
func ensureLineVisible(scrollOffset *int, lineIndex, totalHeight, headerLines, footerLines int) {
	avail := totalHeight - headerLines - footerLines
	if avail <= 0 {
		return
	}
	if lineIndex < *scrollOffset {
		*scrollOffset = lineIndex
	}
	if lineIndex >= *scrollOffset+avail {
		*scrollOffset = lineIndex - avail + 1
	}
	if *scrollOffset < 0 {
		*scrollOffset = 0
	}
}

// countLines returns the number of lines in a string (splitting by newline).
func countLines(s string) int {
	if s == "" {
		return 0
	}
	return len(strings.Split(s, "\n"))
}

// splitContentLines splits content into lines, handling the empty string case.
func splitContentLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
