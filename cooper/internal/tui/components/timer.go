package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// TimerBar renders a countdown progress bar that shrinks as the deadline
// approaches. Color shifts through the timer gradient defined in styles.
type TimerBar struct {
	Deadline time.Time
	// Duration is the total span from start to deadline. Stored so that
	// Progress() can compute a fraction even when only the deadline is
	// known at construction time.
	Duration time.Duration
	Width    int
}

// NewTimerBar creates a timer bar for a deadline. width is the character
// count of the bar (not including the time label).
func NewTimerBar(deadline time.Time, duration time.Duration, width int) TimerBar {
	if width < 1 {
		width = 20
	}
	return TimerBar{
		Deadline: deadline,
		Duration: duration,
		Width:    width,
	}
}

// Progress returns a value between 0.0 (expired) and 1.0 (full).
func (t TimerBar) Progress() float64 {
	remaining := time.Until(t.Deadline)
	if remaining <= 0 {
		return 0.0
	}
	if t.Duration <= 0 {
		return 0.0
	}
	p := float64(remaining) / float64(t.Duration)
	if p > 1.0 {
		p = 1.0
	}
	return p
}

// Expired returns true when the deadline has passed.
func (t TimerBar) Expired() bool {
	return time.Now().After(t.Deadline)
}

// View renders the timer bar with the format: [---] X.Xs
func (t TimerBar) View() string {
	progress := t.Progress()
	filled := int(float64(t.Width) * progress)
	if filled > t.Width {
		filled = t.Width
	}
	empty := t.Width - filled

	color := theme.TimerColor(progress)
	filledStyle := lipgloss.NewStyle().Foreground(color)

	bar := filledStyle.Render(strings.Repeat(theme.ProgressFull, filled)) +
		theme.TimerBarEmptyStyle.Render(strings.Repeat(theme.ProgressEmpty, empty))

	remaining := time.Until(t.Deadline)
	if remaining < 0 {
		remaining = 0
	}
	secs := remaining.Seconds()
	label := filledStyle.Render(fmt.Sprintf(" %.1fs", secs))

	return "[" + bar + "]" + label
}
