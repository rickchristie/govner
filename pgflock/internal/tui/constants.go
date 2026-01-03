package tui

import (
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Animation timing constants
const (
	// LOCKED status heartbeat - 500ms full cycle / 5 frames = 100ms per frame
	LockedAnimationInterval = 100 * time.Millisecond

	// Copy shimmer effect
	CopyShimmerInterval = 50 * time.Millisecond // shimmer speed (fast metallic sheen)
	CopyShimmerDuration = 2500 * time.Millisecond // total display time

	// Startup animation
	StartupFrameInterval = 100 * time.Millisecond  // 30 frames over 3s
	StartupTotalDuration = 3000 * time.Millisecond

	// UI refresh rate
	TickInterval = time.Second
)

// The Twilight Meadow color palette
var (
	// Base colors - The darkness of night
	ColorVoid      = lipgloss.Color("#0a0e14")
	ColorSurface   = lipgloss.Color("#131920")
	ColorSelection = lipgloss.Color("#1c2836") // Moonlit patch of grass (subtle)
	ColorHighlight = lipgloss.Color("#22d3ee") // Cyan selection (cool glow)
	ColorBorder    = lipgloss.Color("#2d3748") // Stone walls in shadow

	// Text colors
	ColorTextBright = lipgloss.Color("#e2e8f0") // Starlight
	ColorTextDim    = lipgloss.Color("#64748b") // Distant hills
	ColorTextFaint  = lipgloss.Color("#334155") // Mist (for dimmed backdrop)
	ColorTextMuted  = lipgloss.Color("#94a3b8") // Modal body text

	// Accent colors
	ColorLime   = lipgloss.Color("#4ade80") // Healthy grass, FREE status
	ColorAmber  = lipgloss.Color("#fbbf24") // Lantern light, selection
	ColorCyan   = lipgloss.Color("#22d3ee") // Headers, keys
	ColorViolet = lipgloss.Color("#a78bfa") // Test identifiers/markers

	// LOCKED animation colors - warm pulse
	ColorCoral  = lipgloss.Color("#f87171") // frame 0, 4 (base)
	ColorRose   = lipgloss.Color("#fb7185") // frame 1, 3
	ColorOrange = lipgloss.Color("#fb923c") // frame 2 (peak brightness)

	// Copy shimmer colors - light passing through
	ColorEmerald = lipgloss.Color("#34d399") // frame 0, 4
	ColorMint    = lipgloss.Color("#a7f3d0") // frame 2 (peak brightness)
)

// Unicode characters for the UI
const (
	// Brand
	SheepEmoji    = "🐑"
	SparklesEmoji = "✨"
	SleepingEmoji = "💤"

	// Status icons
	IconCheckmark      = "✓"
	IconCross          = "✗"
	IconWarning        = "⚠"
	IconFree           = "○"
	IconFarmer         = "🧑‍🌾"
	IconSelectionArrow = "▶"
	IconDatabase       = "🛢️"

	// LOCKED animation icons (5-frame cycle)
	IconLockedFrame0 = "◉" // filled circle
	IconLockedFrame1 = "◈" // diamond
	IconLockedFrame2 = "◆" // solid diamond (peak)
	IconLockedFrame3 = "◈" // diamond
	IconLockedFrame4 = "◉" // filled circle

	// Borders
	BorderHeavyH = "━"
	BorderLightH = "─"

	// Navigation hint
	NavArrows = "↑↓"
)

// LockedAnimationIcons returns the icon sequence for LOCKED animation
func LockedAnimationIcons() []string {
	return []string{
		IconLockedFrame0,
		IconLockedFrame1,
		IconLockedFrame2,
		IconLockedFrame3,
		IconLockedFrame4,
	}
}

// LockedAnimationColors returns the color sequence for LOCKED animation
func LockedAnimationColors() []lipgloss.Color {
	return []lipgloss.Color{
		ColorCoral,  // frame 0
		ColorRose,   // frame 1
		ColorOrange, // frame 2 (peak)
		ColorRose,   // frame 3
		ColorCoral,  // frame 4
	}
}

// CopyShimmerColors returns the color sequence for copy shimmer animation
func CopyShimmerColors() []lipgloss.Color {
	return []lipgloss.Color{
		ColorEmerald, // frame 0
		ColorLime,    // frame 1
		ColorMint,    // frame 2 (peak)
		ColorLime,    // frame 3
		ColorEmerald, // frame 4
	}
}
