package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// LockedAnimator handles the heartbeat animation for LOCKED status.
// It cycles through 5 frames with icons ◉→◈→◆→◈→◉ and colors coral→rose→orange→rose→coral.
type LockedAnimator struct {
	frame int

	// Pre-computed styles for each frame (avoid allocations in render loop)
	frameStyles []lipgloss.Style
	icons       []string
}

// NewLockedAnimator creates a new animator with pre-computed styles.
func NewLockedAnimator() *LockedAnimator {
	icons := LockedAnimationIcons()
	colors := LockedAnimationColors()

	// Pre-compute styles for each frame
	styles := make([]lipgloss.Style, len(icons))
	for i := range icons {
		styles[i] = lipgloss.NewStyle().
			Foreground(colors[i]).
			Bold(true)
	}

	return &LockedAnimator{
		frame:       0,
		frameStyles: styles,
		icons:       icons,
	}
}

// Tick advances the animation by one frame.
func (a *LockedAnimator) Tick() {
	a.frame = (a.frame + 1) % len(a.icons)
}

// Frame returns the current frame index.
func (a *LockedAnimator) Frame() int {
	return a.frame
}

// Icon returns the current animation icon.
func (a *LockedAnimator) Icon() string {
	return a.icons[a.frame]
}

// Style returns the pre-computed style for current frame.
func (a *LockedAnimator) Style() lipgloss.Style {
	return a.frameStyles[a.frame]
}

// Render returns the styled "◉ LOCKED" string for current frame.
func (a *LockedAnimator) Render() string {
	return a.frameStyles[a.frame].Render(a.icons[a.frame] + " LOCKED")
}

// RenderIcon returns just the styled icon.
func (a *LockedAnimator) RenderIcon() string {
	return a.frameStyles[a.frame].Render(a.icons[a.frame])
}

// CopyShimmer handles the shimmer animation for copy feedback.
// Displays "✓ Copied!" with a metallic sheen moving left to right.
type CopyShimmer struct {
	active bool
	frame  int

	// Pre-computed styles
	baseStyle      lipgloss.Style // Base text color
	highlightStyle lipgloss.Style // Bright highlight (sheen)
	glowStyle      lipgloss.Style // Glow around highlight

	// Text to animate
	text       string
	textRunes  []rune
	totalWidth int // Total animation width (text + buffer for sheen to exit)
}

// NewCopyShimmer creates a new shimmer animator with pre-computed styles.
func NewCopyShimmer() *CopyShimmer {
	text := IconCheckmark + " Copied!"
	runes := []rune(text)

	return &CopyShimmer{
		active:         false,
		frame:          0,
		baseStyle:      lipgloss.NewStyle().Foreground(ColorEmerald).Bold(true),
		highlightStyle: lipgloss.NewStyle().Foreground(ColorMint).Bold(true),
		glowStyle:      lipgloss.NewStyle().Foreground(ColorLime).Bold(true),
		text:           text,
		textRunes:      runes,
		totalWidth:     len(runes) + 3, // Extra frames for sheen to fully exit
	}
}

// Start activates the shimmer animation.
func (s *CopyShimmer) Start() {
	s.active = true
	s.frame = -2 // Start before the text so sheen enters from left
}

// Stop deactivates the shimmer animation.
func (s *CopyShimmer) Stop() {
	s.active = false
	s.frame = 0
}

// IsActive returns whether the shimmer is currently active.
func (s *CopyShimmer) IsActive() bool {
	return s.active
}

// Tick advances the shimmer animation by one frame.
func (s *CopyShimmer) Tick() {
	if s.active {
		s.frame++
		// Loop the animation
		if s.frame > s.totalWidth {
			s.frame = -2
		}
	}
}

// Render returns the styled "✓ Copied!" string with moving sheen.
func (s *CopyShimmer) Render() string {
	if !s.active {
		return ""
	}

	var result string
	sheenPos := s.frame

	for i, r := range s.textRunes {
		char := string(r)
		dist := i - sheenPos

		// Apply style based on distance from sheen position
		switch {
		case dist == 0:
			// Center of sheen - brightest
			result += s.highlightStyle.Render(char)
		case dist == -1 || dist == 1:
			// Adjacent to sheen - glow
			result += s.glowStyle.Render(char)
		default:
			// Base color
			result += s.baseStyle.Render(char)
		}
	}

	return result
}
