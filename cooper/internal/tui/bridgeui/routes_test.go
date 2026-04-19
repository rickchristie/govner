package bridgeui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHandleEditInput_PasteAppendsScriptPath(t *testing.T) {
	m := NewRoutesModel()
	m.editMode = routeAdding
	m.editField = fieldScriptPath

	pasted := "/tmp/dev backend.sh"
	m.handleEditInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pasted), Paste: true})

	if m.editScript != pasted {
		t.Fatalf("editScript = %q, want %q", m.editScript, pasted)
	}
}

func TestHandleEditInput_TabSwitchesFields(t *testing.T) {
	m := NewRoutesModel()
	m.editMode = routeAdding
	m.editField = fieldAPIPath

	m.handleEditInput(tea.KeyMsg{Type: tea.KeyTab})
	if m.editField != fieldScriptPath {
		t.Fatalf("editField = %v, want %v", m.editField, fieldScriptPath)
	}

	m.handleEditInput(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.editField != fieldAPIPath {
		t.Fatalf("editField = %v, want %v", m.editField, fieldAPIPath)
	}
}
