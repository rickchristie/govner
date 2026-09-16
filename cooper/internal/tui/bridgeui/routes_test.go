package bridgeui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

func TestRouteEditorKeepsBorderAndActionsInsideSmallBody(t *testing.T) {
	for _, message := range []string{"", "reserved path: /clipboard/* is used by clipboard-bridge"} {
		m := NewRoutesModel()
		m.editMode = routeEditing
		m.editAPI = "/run"
		m.editScript = "/scripts/" + strings.Repeat("long-directory/", 8) + "script.sh"
		m.editField = fieldScriptPath
		m.editErr = message
		view := m.View(80, 18)
		if lipgloss.Height(view) > 18 || lipgloss.Width(view) > 80 {
			t.Fatalf("editor exceeds its body with error %q:\n%s", message, view)
		}
		for _, text := range []string{"/run", "script.sh_", "Save", "Cancel", "╚", "╝"} {
			if !strings.Contains(ansi.Strip(view), text) {
				t.Fatalf("editor hides %q with error %q:\n%s", text, message, view)
			}
		}
		if message != "" && !strings.Contains(ansi.Strip(view), "reserved path") {
			t.Fatal("editor hides the validation error")
		}
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
