package configure

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// textInput is a simple single-line text input widget.
// It does not depend on charmbracelet/bubbles.
type textInput struct {
	value       string
	placeholder string
	focused     bool
	cursorPos   int
	width       int
}

func newTextInput(placeholder string, width int) textInput {
	return textInput{
		placeholder: placeholder,
		width:       width,
	}
}

func (t *textInput) SetValue(v string) {
	t.value = v
	t.cursorPos = len(v)
}

func (t *textInput) Value() string {
	return t.value
}

func (t *textInput) Focus() {
	t.focused = true
}

func (t *textInput) Blur() {
	t.focused = false
}

// handleKey processes a single key press or pasted text. Returns true if the
// input was handled.
func (t *textInput) handleKey(key string) bool {
	switch key {
	case "backspace":
		if t.cursorPos > 0 && len(t.value) > 0 {
			t.value = t.value[:t.cursorPos-1] + t.value[t.cursorPos:]
			t.cursorPos--
		}
		return true
	case "delete":
		if t.cursorPos < len(t.value) {
			t.value = t.value[:t.cursorPos] + t.value[t.cursorPos+1:]
		}
		return true
	case "left":
		if t.cursorPos > 0 {
			t.cursorPos--
		}
		return true
	case "right":
		if t.cursorPos < len(t.value) {
			t.cursorPos++
		}
		return true
	case "home", "ctrl+a":
		t.cursorPos = 0
		return true
	case "end", "ctrl+e":
		t.cursorPos = len(t.value)
		return true
	default:
		// Insert printable ASCII text, including multi-character paste payloads.
		if isPrintableASCIIText(key) {
			t.value = t.value[:t.cursorPos] + key + t.value[t.cursorPos:]
			t.cursorPos += len(key)
			return true
		}
	}
	return false
}

// handleKeyMsg processes Bubble Tea key messages so pasted text can be handled
// as literal input without confusing named keys like ctrl+z for text.
func (t *textInput) handleKeyMsg(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "backspace", "delete", "left", "right", "home", "ctrl+a", "end", "ctrl+e":
		return t.handleKey(msg.String())
	}

	switch msg.Type {
	case tea.KeyRunes:
		return t.handleKey(string(msg.Runes))
	case tea.KeySpace:
		return t.handleKey(" ")
	default:
		return false
	}
}

func isPrintableASCIIText(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 32 || s[i] >= 127 {
			return false
		}
	}
	return true
}

// viewWithMargin renders the input with a left margin for alignment within indented layouts.
// The margin is applied to the entire box (all lines), not just the first line.
func (t *textInput) viewWithMargin(marginLeft int) string {
	return t.renderBox(marginLeft)
}

func (t *textInput) view() string {
	return t.renderBox(0)
}

func (t *textInput) renderBox(marginLeft int) string {
	borderColor := theme.ColorOakLight
	if t.focused {
		borderColor = theme.ColorAmber
	}
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		Foreground(theme.ColorParchment).
		Width(t.width).
		MarginLeft(marginLeft)

	display := t.value
	if display == "" && !t.focused {
		display = lipgloss.NewStyle().
			Foreground(theme.ColorFaded).
			Italic(true).
			Render(t.placeholder)
		return borderStyle.Render(display)
	}

	if t.focused {
		// Show cursor as underscore character.
		before := t.value[:t.cursorPos]
		after := ""
		if t.cursorPos < len(t.value) {
			after = t.value[t.cursorPos:]
		}
		cursor := lipgloss.NewStyle().
			Foreground(theme.ColorAmber).
			Bold(true).
			Render("_")
		display = before + cursor + after
	}

	return borderStyle.Render(display)
}
