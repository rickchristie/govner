package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// FixedFrame reserves stable header and footer rows and gives every remaining
// row to the body. Callers own the body's interaction state (a viewport, list,
// table, or form); the frame guarantees that oversized content cannot push the
// footer off-screen.
type FixedFrame struct {
	Header           string
	Footer           string
	Width            int
	Height           int
	HideTopSeparator bool
}

// BodyHeight returns the exact number of terminal rows available to content.
func (f FixedFrame) BodyHeight() int {
	height := f.Height - len(frameLines(f.Header)) - len(frameLines(f.Footer))
	if f.Header != "" && !f.HideTopSeparator {
		height--
	}
	if f.Footer != "" {
		height--
	}
	if height < 0 {
		return 0
	}
	return height
}

// View renders a body inside the fixed frame, truncating or padding it to the
// exact available height. Every line is ANSI-width-aware and constrained to
// the terminal width so wrapping cannot move the footer.
func (f FixedFrame) View(body string) string {
	width := f.Width
	if width < 0 {
		width = 0
	}

	var rows []string
	header := frameLines(f.Header)
	footer := frameLines(f.Footer)
	for _, line := range header {
		rows = append(rows, fitFrameLine(line, width))
	}

	separator := lipgloss.NewStyle().
		Foreground(theme.ColorOakLight).
		Render(strings.Repeat(theme.BorderH, width))
	if len(header) > 0 && !f.HideTopSeparator {
		rows = append(rows, separator)
	}

	bodyHeight := f.BodyHeight()
	bodyLines := frameLines(body)
	if len(bodyLines) > bodyHeight {
		bodyLines = bodyLines[:bodyHeight]
	}
	for _, line := range bodyLines {
		rows = append(rows, fitFrameLine(line, width))
	}
	for len(bodyLines) < bodyHeight {
		rows = append(rows, "")
		bodyLines = append(bodyLines, "")
	}

	if len(footer) > 0 {
		rows = append(rows, separator)
		for _, line := range footer {
			rows = append(rows, fitFrameLine(line, width))
		}
	}

	if len(rows) > f.Height && f.Height >= 0 {
		rows = rows[:f.Height]
	}
	return strings.Join(rows, "\n")
}

func frameLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func fitFrameLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(line) > width {
		return lipgloss.NewStyle().MaxWidth(width).Render(line)
	}
	return line
}
