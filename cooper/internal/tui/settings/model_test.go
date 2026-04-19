package settings

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestToggleProxyAlertSoundWithEnter(t *testing.T) {
	m := New(30, 500, 500, 500, 300, 20, false)
	m.selected = len(settingDefs) - 1

	_, cmd := m.handleNormalKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected settings change command")
	}
	msg, ok := cmd().(SettingsChangedMsg)
	if !ok {
		t.Fatalf("command message type = %T, want SettingsChangedMsg", cmd())
	}
	if !msg.ProxyAlertSound {
		t.Fatal("ProxyAlertSound = false, want true")
	}
}

func TestToggleProxyAlertSoundWithArrowKeys(t *testing.T) {
	m := New(30, 500, 500, 500, 300, 20, true)
	m.selected = len(settingDefs) - 1

	_, cmd := m.handleNormalKey(tea.KeyMsg{Type: tea.KeyLeft})
	if cmd == nil {
		t.Fatal("expected settings change command")
	}
	msg := cmd().(SettingsChangedMsg)
	if msg.ProxyAlertSound {
		t.Fatal("ProxyAlertSound = true, want false")
	}

	_, cmd = m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRight})
	if cmd == nil {
		t.Fatal("expected settings change command")
	}
	msg = cmd().(SettingsChangedMsg)
	if !msg.ProxyAlertSound {
		t.Fatal("ProxyAlertSound = false, want true")
	}
}

func TestRenderBodyShowsCheckboxState(t *testing.T) {
	m := New(30, 500, 500, 500, 300, 20, true)
	body := m.renderBody(100)
	if !strings.Contains(body, "Enabled") {
		t.Fatalf("renderBody() missing enabled checkbox label:\n%s", body)
	}

	m.proxyAlertSound = false
	body = m.renderBody(100)
	if !strings.Contains(body, "Disabled") {
		t.Fatalf("renderBody() missing disabled checkbox label:\n%s", body)
	}
}

func TestHandleEditKey_PasteDigits(t *testing.T) {
	m := New(30, 500, 500, 500, 300, 20, false)
	m.editing = true
	m.editBuf = ""

	m.handleEditKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1234"), Paste: true})

	if m.editBuf != "1234" {
		t.Fatalf("editBuf = %q, want %q", m.editBuf, "1234")
	}
}
