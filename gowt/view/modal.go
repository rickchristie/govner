package view

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ModalButton represents a button in a modal
type ModalButton struct {
	Label    string
	Selected bool
}

// ModalConfig holds configuration for rendering a modal
type ModalConfig struct {
	Title      string
	Message    string // Optional message below title
	Buttons    []ModalButton
	Width      int // 0 = auto, minimum width
}

// ModalStyles holds all styles for modal rendering
type ModalStyles struct {
	// Container
	Container lipgloss.Style

	// Content styles
	Title          lipgloss.Style
	Message        lipgloss.Style
	Button         lipgloss.Style
	ButtonSelected lipgloss.Style

	// Effects
	Shadow lipgloss.Style
	Dim    lipgloss.Style
}

// DefaultModalStyles returns beautiful default modal styles
func DefaultModalStyles() ModalStyles {
	return ModalStyles{
		Container: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("99")).
			Background(lipgloss.Color("235")).
			Padding(1, 3),

		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("231")).
			Background(lipgloss.Color("235")).
			Align(lipgloss.Center),

		Message: lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Background(lipgloss.Color("235")).
			Align(lipgloss.Center),

		Button: lipgloss.NewStyle().
			Foreground(lipgloss.Color("250")).
			Background(lipgloss.Color("238")).
			Padding(0, 3).
			MarginRight(2),

		ButtonSelected: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("231")).
			Background(lipgloss.Color("99")).
			Padding(0, 3).
			MarginRight(2),

		Shadow: lipgloss.NewStyle().
			Foreground(lipgloss.Color("236")),

		Dim: lipgloss.NewStyle().
			Foreground(lipgloss.Color("239")),
	}
}

// RenderModal renders a modal dialog centered on the screen
// It overlays the modal on top of the existing content with dimming effect
func RenderModal(content string, config ModalConfig, styles ModalStyles, screenWidth, screenHeight int) string {
	if screenWidth == 0 || screenHeight == 0 {
		return content
	}

	// Build modal box
	modalBox := buildModalBox(config, styles)
	modalLines := strings.Split(modalBox, "\n")

	modalWidth := maxLineWidth(modalLines)
	modalHeight := len(modalLines)

	// Calculate center position
	startCol := (screenWidth - modalWidth) / 2
	startRow := (screenHeight - modalHeight) / 2

	if startCol < 1 {
		startCol = 1
	}
	if startRow < 1 {
		startRow = 1
	}

	// Split background content into lines
	bgLines := strings.Split(content, "\n")

	// Ensure we have enough lines
	for len(bgLines) < screenHeight {
		bgLines = append(bgLines, "")
	}

	// Dim the entire background
	for i := range bgLines {
		bgLines[i] = dimLineContent(bgLines[i], screenWidth)
	}

	// Draw shadow (offset by 1 down and 2 right)
	shadowChar := "░"
	for i := 0; i < modalHeight; i++ {
		row := startRow + i + 1
		if row >= 0 && row < len(bgLines) {
			shadowLine := strings.Repeat(shadowChar, modalWidth)
			bgLines[row] = insertAtPosition(bgLines[row], shadowLine, startCol+2, screenWidth, styles.Shadow)
		}
	}
	// Shadow bottom edge
	if startRow+modalHeight < len(bgLines) {
		shadowLine := strings.Repeat(shadowChar, modalWidth)
		bgLines[startRow+modalHeight] = insertAtPosition(bgLines[startRow+modalHeight], shadowLine, startCol+2, screenWidth, styles.Shadow)
	}

	// Draw modal on top
	for i, line := range modalLines {
		row := startRow + i
		if row >= 0 && row < len(bgLines) {
			bgLines[row] = insertAtPosition(bgLines[row], line, startCol, screenWidth, lipgloss.NewStyle())
		}
	}

	return strings.Join(bgLines, "\n")
}

// buildModalBox creates the styled modal box content
func buildModalBox(config ModalConfig, styles ModalStyles) string {
	var parts []string

	// Title
	parts = append(parts, styles.Title.Render(config.Title))

	// Message (if provided)
	if config.Message != "" {
		parts = append(parts, "")
		parts = append(parts, styles.Message.Render(config.Message))
	}

	// Spacer before buttons
	parts = append(parts, "")

	// Buttons row
	var buttonParts []string
	for _, btn := range config.Buttons {
		if btn.Selected {
			buttonParts = append(buttonParts, styles.ButtonSelected.Render(btn.Label))
		} else {
			buttonParts = append(buttonParts, styles.Button.Render(btn.Label))
		}
	}
	buttonsRow := lipgloss.JoinHorizontal(lipgloss.Center, buttonParts...)

	// Center buttons
	parts = append(parts, buttonsRow)

	// Join all content
	innerContent := strings.Join(parts, "\n")

	// Calculate width
	contentWidth := lipgloss.Width(innerContent)
	minWidth := config.Width
	if minWidth == 0 {
		minWidth = 30
	}
	if contentWidth < minWidth {
		contentWidth = minWidth
	}

	// Apply container with centered content
	container := styles.Container.
		Width(contentWidth).
		Align(lipgloss.Center)

	return container.Render(innerContent)
}

// dimLineContent dims a line of text to create the overlay effect
func dimLineContent(line string, width int) string {
	// Strip existing ANSI codes and apply dim styling
	stripped := stripAnsi(line)

	// Pad to full width
	if len(stripped) < width {
		stripped += strings.Repeat(" ", width-len(stripped))
	}

	// Apply dim color
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("239"))
	return dimStyle.Render(stripped)
}

// insertAtPosition inserts overlay text at a specific position in a line
func insertAtPosition(baseLine, overlay string, col, screenWidth int, style lipgloss.Style) string {
	// Get the base line as runes (handle unicode properly)
	baseStripped := stripAnsi(baseLine)
	baseRunes := []rune(baseStripped)

	// Pad base to screen width if needed
	for len(baseRunes) < screenWidth {
		baseRunes = append(baseRunes, ' ')
	}

	// Get overlay visual width
	overlayStripped := stripAnsi(overlay)
	overlayWidth := len([]rune(overlayStripped))

	// Build result
	var result strings.Builder

	// Part before overlay
	if col > 0 {
		before := string(baseRunes[:min(col, len(baseRunes))])
		result.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("239")).Render(before))
	}

	// Overlay content (with optional style)
	if style.Value() != "" {
		result.WriteString(style.Render(overlay))
	} else {
		result.WriteString(overlay)
	}

	// Part after overlay
	afterStart := col + overlayWidth
	if afterStart < len(baseRunes) {
		after := string(baseRunes[afterStart:])
		result.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("239")).Render(after))
	}

	return result.String()
}

// maxLineWidth returns the maximum visual width of lines
func maxLineWidth(lines []string) int {
	maxWidth := 0
	for _, line := range lines {
		w := lipgloss.Width(line)
		if w > maxWidth {
			maxWidth = w
		}
	}
	return maxWidth
}

// stripAnsi removes ANSI escape sequences from a string
func stripAnsi(s string) string {
	var result strings.Builder
	inEscape := false

	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		result.WriteRune(r)
	}

	return result.String()
}

// RenderConfirmModal is a convenience function for yes/no confirmation dialogs
func RenderConfirmModal(content string, title string, yesSelected bool, screenWidth, screenHeight int) string {
	styles := DefaultModalStyles()
	config := ModalConfig{
		Title: title,
		Buttons: []ModalButton{
			{Label: "Yes", Selected: yesSelected},
			{Label: "No", Selected: !yesSelected},
		},
	}
	return RenderModal(content, config, styles, screenWidth, screenHeight)
}

// RenderInfoModal renders an info modal with just an OK button
func RenderInfoModal(content string, title, message string, screenWidth, screenHeight int) string {
	styles := DefaultModalStyles()
	config := ModalConfig{
		Title:   title,
		Message: message,
		Buttons: []ModalButton{
			{Label: "OK", Selected: true},
		},
	}
	return RenderModal(content, config, styles, screenWidth, screenHeight)
}
