package components

import (
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// TextEntryFromKeyMsg returns printable text from a Bubble Tea key message.
// It preserves multi-character paste payloads so text-entry modals can accept
// terminal paste directly instead of only single-key input.
func TextEntryFromKeyMsg(msg tea.KeyMsg, allow func(rune) bool) string {
	var text string

	switch msg.Type {
	case tea.KeyRunes:
		text = string(msg.Runes)
	case tea.KeySpace:
		text = " "
	default:
		return ""
	}

	return filterTextEntry(text, allow)
}

// TrimLastRune removes the last rune from s.
func TrimLastRune(s string) string {
	if s == "" {
		return ""
	}

	_, size := utf8.DecodeLastRuneInString(s)
	if size <= 0 {
		return ""
	}

	return s[:len(s)-size]
}

func filterTextEntry(text string, allow func(rune) bool) string {
	if text == "" {
		return ""
	}

	var b strings.Builder
	for _, r := range text {
		if unicode.IsControl(r) {
			continue
		}
		if allow != nil && !allow(r) {
			continue
		}
		b.WriteRune(r)
	}

	return b.String()
}
