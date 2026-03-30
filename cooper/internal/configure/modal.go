package configure

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// overlayModal renders a modal dialog centered on top of background content.
// The background is dimmed, and the modal box is drawn on top. Both bg and
// modal are split into lines and composited line-by-line so the modal appears
// to float over the existing screen.
func overlayModal(bg string, modal string, width, height int) string {
	bgLines := splitAndPad(bg, width, height)
	modalLines := strings.Split(modal, "\n")

	modalH := len(modalLines)
	modalW := 0
	for _, line := range modalLines {
		if w := lipgloss.Width(line); w > modalW {
			modalW = w
		}
	}

	// Center the modal within the terminal dimensions.
	startY := (height - modalH) / 2
	startX := (width - modalW) / 2
	if startY < 0 {
		startY = 0
	}
	if startX < 0 {
		startX = 0
	}

	dimStyle := lipgloss.NewStyle().Foreground(theme.ColorFaded)

	result := make([]string, height)
	for y := 0; y < height; y++ {
		bgLine := bgLines[y]

		// If this line is within the modal's vertical range, splice the
		// modal content into the background.
		if y >= startY && y < startY+modalH {
			mIdx := y - startY
			mLine := modalLines[mIdx]
			mLineW := lipgloss.Width(mLine)

			// Build: dimmed-left + modal-line + dimmed-right.
			left := truncateToWidth(bgLine, startX)
			left = dimStyle.Render(stripAnsi(left))

			rightStart := startX + mLineW
			right := ""
			if rightStart < width {
				right = skipToWidth(bgLine, rightStart)
				right = dimStyle.Render(stripAnsi(right))
			}

			// Pad the modal line if it's narrower than the measured max.
			if mLineW < modalW {
				mLine += strings.Repeat(" ", modalW-mLineW)
			}

			// Ensure left portion is exactly startX visible characters wide.
			leftW := lipgloss.Width(left)
			if leftW < startX {
				left += strings.Repeat(" ", startX-leftW)
			}

			result[y] = left + mLine + right
		} else {
			// Dim the entire background line.
			result[y] = dimStyle.Render(stripAnsi(bgLine))
		}
	}

	return strings.Join(result, "\n")
}

// splitAndPad splits s into lines, then pads or truncates to exactly `height`
// lines, each at least `width` visible characters wide.
func splitAndPad(s string, width, height int) []string {
	raw := strings.Split(s, "\n")
	lines := make([]string, height)
	for i := 0; i < height; i++ {
		if i < len(raw) {
			lines[i] = raw[i]
			// Pad to width if shorter.
			w := lipgloss.Width(lines[i])
			if w < width {
				lines[i] += strings.Repeat(" ", width-w)
			}
		} else {
			lines[i] = strings.Repeat(" ", width)
		}
	}
	return lines
}

// stripAnsi removes ANSI escape sequences from a string, returning only the
// visible text. This is used to dim background content by re-rendering the
// plain text with a dim style.
func stripAnsi(s string) string {
	var out strings.Builder
	inEsc := false
	for _, r := range s {
		if inEsc {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '~' {
				inEsc = false
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

// truncateToWidth returns the prefix of s whose visible width is at most w.
func truncateToWidth(s string, w int) string {
	if w <= 0 {
		return ""
	}
	plain := stripAnsi(s)
	runes := []rune(plain)
	out := make([]rune, 0, w)
	visW := 0
	for _, r := range runes {
		rw := lipgloss.Width(string(r))
		if visW+rw > w {
			break
		}
		out = append(out, r)
		visW += rw
	}
	return string(out)
}

// skipToWidth returns the substring of s starting at visible column `start`.
func skipToWidth(s string, start int) string {
	plain := stripAnsi(s)
	runes := []rune(plain)
	visW := 0
	for i, r := range runes {
		rw := lipgloss.Width(string(r))
		if visW+rw > start {
			return string(runes[i:])
		}
		visW += rw
	}
	return ""
}
