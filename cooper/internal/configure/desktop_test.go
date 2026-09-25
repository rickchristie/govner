package configure

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rickchristie/govner/cooper/internal/config"
)

type desktopVersions struct {
	err   error
	calls int
}

func (*desktopVersions) DetectHostVersion(string) (string, error) {
	return "", errors.New("not installed")
}
func (v *desktopVersions) ValidateVersion(name, version string) (bool, error) {
	v.calls++
	return name == "chatgpt" && version == "26.917.71314", v.err
}

func TestDesktopPinChecksVersionWithoutBlockingInput(t *testing.T) {
	versions := &desktopVersions{}
	m := newAIToolsModel([]config.ToolConfig{{Name: "chatgpt", Enabled: true, Mode: config.ModeLatest}}, versions)
	m.cursor = len(m.tools) - 1
	m.update(tea.KeyMsg{Type: tea.KeyEnter})
	m.update(tea.KeyMsg{Type: tea.KeyDown}) // No host version: Pin follows Latest.
	m.update(tea.KeyMsg{Type: tea.KeyEnter})
	m.pinInput.SetValue("26.917.71314")
	_, command := m.update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil || !m.checkingVersion || versions.calls != 0 {
		t.Fatal("version request was not deferred")
	}
	m.update(command())
	if m.pinInput.focused || m.checkingVersion || m.tools[m.cursor].pinVersion != "26.917.71314" {
		t.Fatalf("pin result was not applied: %+v", m.tools[m.cursor])
	}
	if view := m.viewDetail(100, 35); !strings.Contains(view, "official Linux desktop package") || !strings.Contains(view, "cooper vm chatgpt") {
		t.Fatal("desktop setup instructions are missing")
	}
}

func TestDesktopPinCancelIgnoresLateResult(t *testing.T) {
	m := newAIToolsModel(nil, &desktopVersions{})
	m.cursor = len(m.tools) - 1
	m.inDetail = true
	m.detailCursor = 1
	m.pinInput.Focus()
	m.pinInput.SetValue("26.917.71314")
	_, command := m.update(tea.KeyMsg{Type: tea.KeyEnter})
	m.update(tea.KeyMsg{Type: tea.KeyEsc})
	m.update(command())
	if m.tools[m.cursor].pinVersion != "" || m.checkingVersion {
		t.Fatal("canceled result changed the selected version")
	}
}
