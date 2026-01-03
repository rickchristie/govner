package tui

import "github.com/charmbracelet/lipgloss"

// Pre-computed styles for performance.
// All styles are initialized at package load time to avoid allocations in render loop.
var (
	// === Header Styles ===

	// Title style for "🐑 pgflock"
	TitleStyle = lipgloss.NewStyle().
			Foreground(ColorCyan).
			Bold(true)

	// Instance count "✓ 2 instances"
	InstancesStyle = lipgloss.NewStyle().
			Foreground(ColorLime)

	// Locked count indicator style (for the number)
	LockedCountStyle = lipgloss.NewStyle().
				Bold(true)

	// Free count "○ 22 free"
	FreeCountStyle = lipgloss.NewStyle().
			Foreground(ColorLime)

	// Waiting count "⏳ 4 waiting"
	WaitingCountStyle = lipgloss.NewStyle().
				Foreground(ColorAmber).
				Bold(true)

	// Dim text for separators and secondary info
	DimStyle = lipgloss.NewStyle().
			Foreground(ColorTextDim)

	// Dimmed backdrop for modal overlay (simulates 40% opacity)
	DimBackdropStyle = lipgloss.NewStyle().
				Foreground(ColorTextFaint)

	// === Section Header ===

	SectionHeaderStyle = lipgloss.NewStyle().
				Foreground(ColorSelection)

	// === Database List Styles ===

	// Normal row (not selected)
	RowNormalStyle = lipgloss.NewStyle().
			Foreground(ColorTextBright)

	// Selected row with lantern glow background
	RowSelectedStyle = lipgloss.NewStyle().
				Foreground(ColorVoid).
				Background(ColorHighlight).
				Bold(true).
				PaddingLeft(1).
				PaddingRight(1)

	// Selection arrow style
	SelectionArrowStyle = lipgloss.NewStyle().
				Foreground(ColorAmber).
				Bold(true)

	// Port display ":9090"
	PortStyle = lipgloss.NewStyle().
			Foreground(ColorAmber)

	// Marker/test identifier "[api-tests]"
	MarkerStyle = lipgloss.NewStyle().
			Foreground(ColorViolet)

	// Duration "2m 34s"
	DurationStyle = lipgloss.NewStyle().
			Foreground(ColorTextDim)

	// FREE status "○ FREE"
	FreeStatusStyle = lipgloss.NewStyle().
			Foreground(ColorLime)

	// === Empty State ===

	EmptyStateStyle = lipgloss.NewStyle().
			Foreground(ColorTextDim).
			Italic(true).
			Align(lipgloss.Center)

	// === Help Bar Styles ===

	HelpBarStyle = lipgloss.NewStyle().
			Foreground(ColorTextDim)

	HelpKeyStyle = lipgloss.NewStyle().
			Foreground(ColorCyan)

	HelpDescStyle = lipgloss.NewStyle().
			Foreground(ColorTextDim)

	// === Error Style ===

	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorCoral).
			Bold(true)

	// === Health Status Styles ===

	// Healthy status (green checkmark)
	HealthyStyle = lipgloss.NewStyle().
			Foreground(ColorLime)

	// Unhealthy status (red cross)
	UnhealthyStyle = lipgloss.NewStyle().
			Foreground(ColorCoral)

	// Partial health status (amber warning)
	PartialHealthStyle = lipgloss.NewStyle().
				Foreground(ColorAmber)

	// Status label style (dim text for "locker", "pg")
	StatusLabelStyle = lipgloss.NewStyle().
				Foreground(ColorTextDim)
)

// GetLockedCountStyle returns the appropriate style for locked count based on animation frame.
// This is called per-render but uses pre-computed colors.
func GetLockedCountStyle(animFrame int) lipgloss.Style {
	colors := LockedAnimationColors()
	return LockedCountStyle.Copy().Foreground(colors[animFrame%len(colors)])
}
