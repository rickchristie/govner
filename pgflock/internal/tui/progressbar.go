package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ProgressBar is a reusable progress bar component.
// Pre-computes styles at creation time for performance.
type ProgressBar struct {
	width       int
	filledChar  string
	emptyChar   string
	filledStyle lipgloss.Style
	emptyStyle  lipgloss.Style
}

// ProgressBarOption configures a ProgressBar.
type ProgressBarOption func(*ProgressBar)

// WithWidth sets the progress bar width (number of characters).
func WithWidth(width int) ProgressBarOption {
	return func(pb *ProgressBar) {
		pb.width = width
	}
}

// WithChars sets the filled and empty characters.
func WithChars(filled, empty string) ProgressBarOption {
	return func(pb *ProgressBar) {
		pb.filledChar = filled
		pb.emptyChar = empty
	}
}

// WithColors sets the filled and empty colors.
func WithColors(filled, empty lipgloss.Color) ProgressBarOption {
	return func(pb *ProgressBar) {
		pb.filledStyle = lipgloss.NewStyle().Foreground(filled)
		pb.emptyStyle = lipgloss.NewStyle().Foreground(empty)
	}
}

// NewProgressBar creates a new progress bar with the given options.
// Default: 20 chars wide, uses ━ for filled and ─ for empty, lime/border colors.
func NewProgressBar(opts ...ProgressBarOption) *ProgressBar {
	pb := &ProgressBar{
		width:       20,
		filledChar:  "━",
		emptyChar:   "─",
		filledStyle: lipgloss.NewStyle().Foreground(ColorLime),
		emptyStyle:  lipgloss.NewStyle().Foreground(ColorBorder),
	}

	for _, opt := range opts {
		opt(pb)
	}

	return pb
}

// Render renders the progress bar at the given progress (0.0 to 1.0).
func (pb *ProgressBar) Render(progress float64) string {
	if pb.width <= 0 {
		return ""
	}

	// Clamp progress to [0, 1]
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}

	filled := int(progress * float64(pb.width))
	if filled > pb.width {
		filled = pb.width
	}
	empty := pb.width - filled

	var b strings.Builder
	if filled > 0 {
		b.WriteString(pb.filledStyle.Render(strings.Repeat(pb.filledChar, filled)))
	}
	if empty > 0 {
		b.WriteString(pb.emptyStyle.Render(strings.Repeat(pb.emptyChar, empty)))
	}

	return b.String()
}

// RenderWithPercent renders the progress bar with percentage label.
func (pb *ProgressBar) RenderWithPercent(progress float64) string {
	bar := pb.Render(progress)
	percent := int(progress * 100)
	if percent > 100 {
		percent = 100
	}
	return bar + pb.emptyStyle.Render(" "+itoa(percent)+"%")
}

// Width returns the configured width.
func (pb *ProgressBar) Width() int {
	return pb.width
}
