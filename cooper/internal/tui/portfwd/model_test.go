package portfwd

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHandlePFEditInput_PastePortRange(t *testing.T) {
	m := New()
	m.pfEditMode = pfAdding
	m.pfField = pfFieldContainerPort

	pasted := "8000-8004"
	m.handlePFEditInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true})

	if m.pfContBuf != pasted {
		t.Fatalf("pfContBuf = %q, want %q", m.pfContBuf, pasted)
	}
}

func TestHandlePFEditInput_PasteDescription(t *testing.T) {
	m := New()
	m.pfEditMode = pfAdding
	m.pfField = pfFieldDesc

	pasted := "web ui local"
	m.handlePFEditInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true})

	if m.pfDescBuf != pasted {
		t.Fatalf("pfDescBuf = %q, want %q", m.pfDescBuf, pasted)
	}
}
