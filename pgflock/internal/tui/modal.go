package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Pre-computed modal styles for performance
var (
	// Modal container with amber border
	modalBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(ColorAmber).
			Padding(1, 3).
			Width(50)

	// Modal title - bright with sheep
	modalTitleStyle = lipgloss.NewStyle().
			Foreground(ColorTextBright).
			Bold(true).
			Align(lipgloss.Center).
			Width(44)

	// Modal divider
	modalDividerStyle = lipgloss.NewStyle().
				Foreground(ColorBorder).
				Align(lipgloss.Center).
				Width(44)

	// Modal body text
	modalBodyStyle = lipgloss.NewStyle().
			Foreground(ColorTextMuted).
			Align(lipgloss.Center).
			Width(44)

	// Modal button styles
	modalConfirmStyle = lipgloss.NewStyle().
				Foreground(ColorLime).
				Bold(true)

	modalCancelStyle = lipgloss.NewStyle().
				Foreground(ColorTextDim)

	// Modal button container
	modalButtonsStyle = lipgloss.NewStyle().
				Align(lipgloss.Center).
				Width(44).
				MarginTop(1)
)

// ModalConfig holds configuration for rendering a modal dialog.
type ModalConfig struct {
	Title       string
	Body        []string // Multiple lines of body text
	ConfirmText string
	CancelText  string
}

// RenderModal renders a modal dialog box.
func RenderModal(cfg ModalConfig) string {
	var b strings.Builder

	// Title with sheep emoji
	title := SheepEmoji + " " + cfg.Title
	b.WriteString(modalTitleStyle.Render(title))
	b.WriteString("\n\n")

	// Divider
	divider := strings.Repeat(BorderLightH, 40)
	b.WriteString(modalDividerStyle.Render(divider))
	b.WriteString("\n\n")

	// Body lines
	for _, line := range cfg.Body {
		b.WriteString(modalBodyStyle.Render(line))
		b.WriteString("\n")
	}

	// Divider
	b.WriteString("\n")
	b.WriteString(modalDividerStyle.Render(divider))
	b.WriteString("\n")

	// Buttons
	confirm := modalConfirmStyle.Render("[Enter " + IconCheckmark + " " + cfg.ConfirmText + "]")
	cancel := modalCancelStyle.Render("[Esc " + cfg.CancelText + "]")
	buttons := confirm + "    " + cancel
	b.WriteString(modalButtonsStyle.Render(buttons))

	return modalBoxStyle.Render(b.String())
}

// QuitModal returns the quit confirmation modal.
func QuitModal(lockedCount int) string {
	body := []string{
		"This will stop all PostgreSQL containers",
	}
	if lockedCount > 0 {
		body = append(body, "and release "+pluralize(lockedCount, "database lock", "database locks")+".")
	}

	return RenderModal(ModalConfig{
		Title:       "Quit pgflock?",
		Body:        body,
		ConfirmText: "Confirm",
		CancelText:  "Cancel",
	})
}

// RestartModal returns the restart confirmation modal.
func RestartModal(lockedCount int) string {
	body := []string{}
	if lockedCount > 0 {
		body = append(body, "All "+pluralize(lockedCount, "locked database", "locked databases")+" will be")
		body = append(body, "forcefully released. Running tests may fail.")
	} else {
		body = append(body, "All containers will be restarted.")
	}

	return RenderModal(ModalConfig{
		Title:       "Restart containers?",
		Body:        body,
		ConfirmText: "Confirm",
		CancelText:  "Cancel",
	})
}

// UnlockModal returns the unlock confirmation modal.
func UnlockModal(dbName, marker string, duration string) string {
	body := []string{
		"Currently held by: [" + marker + "]",
		"Locked for: " + duration,
	}

	return RenderModal(ModalConfig{
		Title:       "Force unlock " + dbName + "?",
		Body:        body,
		ConfirmText: "Confirm",
		CancelText:  "Cancel",
	})
}

// pluralize returns singular or plural form based on count.
func pluralize(count int, singular, plural string) string {
	if count == 1 {
		return "1 " + singular
	}
	return itoa(count) + " " + plural
}

// itoa converts an int to string without fmt.Sprintf
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

