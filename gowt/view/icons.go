package view

import "github.com/charmbracelet/lipgloss"

// Pre-rendered icons and spinners to avoid repeated Style.Render() calls.
// These are computed once at package init and reused throughout rendering.

// Icon characters
const (
	IconCharPassed  = "✓"
	IconCharFailed  = "✗"
	IconCharSkipped = "⊘"
	IconCharPending = "○"
	IconCharCached  = "↯"
	IconCharGear    = "⚙"
)

// Spinner frames - Braille dot animation
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Color definitions (shared across views)
var (
	ColorPassed  = lipgloss.Color("82")  // Green
	ColorFailed  = lipgloss.Color("196") // Red
	ColorSkipped = lipgloss.Color("245") // Gray
	ColorPending = lipgloss.Color("241") // Dim gray
	ColorCached  = lipgloss.Color("220") // Yellow/gold
)

// Spinner gradient colors (cyan -> blue -> magenta -> pink cycle)
var SpinnerColors = []lipgloss.Color{
	lipgloss.Color("51"),  // Cyan
	lipgloss.Color("45"),  // Light blue
	lipgloss.Color("39"),  // Blue
	lipgloss.Color("33"),  // Darker blue
	lipgloss.Color("63"),  // Blue-purple
	lipgloss.Color("99"),  // Purple
	lipgloss.Color("135"), // Magenta
	lipgloss.Color("171"), // Pink
	lipgloss.Color("207"), // Light pink
	lipgloss.Color("213"), // Lighter pink
	lipgloss.Color("219"), // Very light pink
	lipgloss.Color("183"), // Lavender
}

// Pre-rendered icons (with trailing space for tree view alignment)
var (
	// Status icons - styled with color + trailing space
	IconPassed  string
	IconFailed  string
	IconSkipped string
	IconPending string
	IconCached  string

	// Status icons - styled with color, NO trailing space (for headers/compact use)
	IconPassedCompact  string
	IconFailedCompact  string
	IconSkippedCompact string
	IconPendingCompact string
	IconCachedCompact  string

	// Raw icons - no color styling (for use in inverted/selected rows)
	IconPassedRaw  = IconCharPassed + " "
	IconFailedRaw  = IconCharFailed + " "
	IconSkippedRaw = IconCharSkipped + " "
	IconPendingRaw = IconCharPending + " "
	IconCachedRaw  = IconCharCached + " "

	// Gear icons for header
	IconGearPassed string
	IconGearFailed string

	// Pre-rendered spinner frames: [frameIndex][colorIndex] = rendered string
	// Access: SpinnerRendered[frame % 10][color % 12]
	SpinnerRendered [10][12]string

	// Pre-rendered spinner frames without trailing space (for headers)
	SpinnerRenderedCompact [10][12]string

	// Raw spinner frames (no color, for selected rows)
	SpinnerRaw [10]string

	// Pre-rendered gear icons with spinner colors: [colorIndex] = rendered string
	SpinnerGearRendered [12]string
)

func init() {
	// Pre-render status icons
	passedStyle := lipgloss.NewStyle().Foreground(ColorPassed)
	failedStyle := lipgloss.NewStyle().Foreground(ColorFailed)
	skippedStyle := lipgloss.NewStyle().Foreground(ColorSkipped)
	pendingStyle := lipgloss.NewStyle().Foreground(ColorPending)
	cachedStyle := lipgloss.NewStyle().Foreground(ColorCached)

	// With trailing space (for tree view rows)
	IconPassed = passedStyle.Render(IconCharPassed) + " "
	IconFailed = failedStyle.Render(IconCharFailed) + " "
	IconSkipped = skippedStyle.Render(IconCharSkipped) + " "
	IconPending = pendingStyle.Render(IconCharPending) + " "
	IconCached = cachedStyle.Render(IconCharCached) + " "

	// Without trailing space (for headers/compact use)
	IconPassedCompact = passedStyle.Render(IconCharPassed)
	IconFailedCompact = failedStyle.Render(IconCharFailed)
	IconSkippedCompact = skippedStyle.Render(IconCharSkipped)
	IconPendingCompact = pendingStyle.Render(IconCharPending)
	IconCachedCompact = cachedStyle.Render(IconCharCached)

	// Pre-render gear icons for header
	IconGearPassed = passedStyle.Render(IconCharGear)
	IconGearFailed = failedStyle.Render(IconCharGear)

	// Pre-render all spinner frame + color combinations
	for frame := 0; frame < len(SpinnerFrames); frame++ {
		// Raw version (no color, with space)
		SpinnerRaw[frame] = SpinnerFrames[frame] + " "

		// Colored versions
		for colorIdx := 0; colorIdx < len(SpinnerColors); colorIdx++ {
			style := lipgloss.NewStyle().Foreground(SpinnerColors[colorIdx])
			SpinnerRendered[frame][colorIdx] = style.Render(SpinnerFrames[frame]) + " "
			SpinnerRenderedCompact[frame][colorIdx] = style.Render(SpinnerFrames[frame])
		}
	}

	// Pre-render gear icons with spinner colors
	for colorIdx := 0; colorIdx < len(SpinnerColors); colorIdx++ {
		style := lipgloss.NewStyle().Foreground(SpinnerColors[colorIdx])
		SpinnerGearRendered[colorIdx] = style.Render(IconCharGear)
	}
}

// GetSpinnerIcon returns the pre-rendered spinner icon for the given animation frame.
// Uses color cycling based on the frame number. Includes trailing space.
func GetSpinnerIcon(animFrame int) string {
	frame := animFrame % len(SpinnerFrames)
	colorIdx := animFrame % len(SpinnerColors)
	return SpinnerRendered[frame][colorIdx]
}

// GetSpinnerIconCompact returns the pre-rendered spinner icon without trailing space.
// Use this for headers or compact displays.
func GetSpinnerIconCompact(animFrame int) string {
	frame := animFrame % len(SpinnerFrames)
	colorIdx := animFrame % len(SpinnerColors)
	return SpinnerRenderedCompact[frame][colorIdx]
}

// GetSpinnerIconRaw returns the raw (unstyled) spinner icon for the given animation frame.
// Use this for selected/inverted rows where color would conflict.
func GetSpinnerIconRaw(animFrame int) string {
	frame := animFrame % len(SpinnerFrames)
	return SpinnerRaw[frame]
}

// GetSpinnerGear returns a pre-rendered gear icon with spinner color for the given frame.
// Used in the header during test runs.
func GetSpinnerGear(animFrame int) string {
	colorIdx := animFrame % len(SpinnerColors)
	return SpinnerGearRendered[colorIdx]
}
