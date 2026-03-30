package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// Modal is a centered dialog overlay. Key handling lives in the root model;
// the modal itself only renders.
type Modal struct {
	Title         string
	Body          string
	ConfirmLabel  string
	CancelLabel   string
	Active        bool
	ModalType     theme.ModalType
	FocusConfirm  bool // true = Confirm focused, false = Cancel focused
}

// NewModal creates a modal dialog of the given type.
func NewModal(modalType theme.ModalType, title, body, confirm, cancel string) Modal {
	return Modal{
		Title:        title,
		Body:         body,
		ConfirmLabel: confirm,
		CancelLabel:  cancel,
		Active:       true,
		ModalType:    modalType,
		FocusConfirm: true, // Default focus on Confirm.
	}
}

// View renders the modal centered within the given dimensions.
// When Active is false, View returns an empty string.
func (m Modal) View(width, height int) string {
	if !m.Active {
		return ""
	}

	// Build the inner content: title, divider, body, divider, buttons.
	title := theme.ModalTitleStyle.Render(m.Title)
	divider := theme.ModalDividerStyle.Render(strings.Repeat(theme.BorderH, 38))
	body := theme.ModalBodyStyle.Render(m.Body)

	arrow := lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render(theme.IconArrowRight + " ")
	var buttons string
	if m.FocusConfirm {
		buttons = arrow + theme.ModalConfirmStyle.Render("["+m.ConfirmLabel+"]") +
			"    " + theme.ModalCancelStyle.Render("  ["+m.CancelLabel+"]")
	} else {
		buttons = theme.ModalConfirmStyle.Render("  ["+m.ConfirmLabel+"]") +
			"    " + arrow + theme.ModalCancelStyle.Render("["+m.CancelLabel+"]")
	}

	// Center the button row within the modal inner width.
	buttonsRow := lipgloss.NewStyle().
		Width(44).
		Align(lipgloss.Center).
		Render(buttons)

	inner := lipgloss.JoinVertical(lipgloss.Center,
		"",
		title,
		"",
		divider,
		"",
		body,
		"",
		divider,
		"",
		buttonsRow,
		"",
	)

	// Apply the modal border style.
	box := theme.ModalBorderStyle.Render(inner)

	// Center the box within the terminal.
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// DimContent renders the given content string with dimmed (Faded) foreground,
// suitable for displaying behind an active modal overlay.
func DimContent(content string) string {
	return theme.DimBackdropStyle.Render(content)
}
