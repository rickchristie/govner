package configure

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestAIToolsScrollChangesOnlyInUpdate(t *testing.T) {
	m := newAIToolsModel(nil, desktopVersions{version: "1.2.3"})
	m.update(tea.WindowSizeMsg{Width: 80, Height: 10})
	for i := 1; i < len(m.tools); i++ {
		m.update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.scrollOffset == 0 {
		t.Fatal("selection did not scroll into view")
	}
	before := m.scrollOffset
	view := m.view(80, 10)
	if m.scrollOffset != before || !strings.Contains(view, "ChatGPT") {
		t.Fatal("view changed scroll state or hid the selected tool")
	}
	m.update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	before = m.scrollOffset
	m.view(80, 10)
	if m.scrollOffset != before {
		t.Fatal("view reset manual scrolling")
	}
	m.update(tea.KeyMsg{Type: tea.KeyEnter})
	m.update(tea.KeyMsg{Type: tea.KeyPgDown})
	before = m.detailScrollOffset
	if before == 0 {
		t.Fatal("detail view did not scroll")
	}
	m.view(80, 10)
	if m.detailScrollOffset != before {
		t.Fatal("detail view changed scroll state")
	}
}

type desktopVersions struct{ version string }

func (v desktopVersions) DetectHostVersion(string) (string, error) {
	if v.version == "" {
		return "", errors.New("not installed")
	}
	return v.version, nil
}

func TestAIToolsUseOnlyLiveHostVersions(t *testing.T) {
	for _, mode := range []config.VersionMode{config.ModeLatest, config.ModePin, config.ModeMirror} {
		m := newAIToolsModel([]config.ToolConfig{{Name: "chatgpt", Enabled: true, Mode: mode, PinnedVersion: "old", HostVersion: "old", ContainerVersion: "old"}}, desktopVersions{version: "26.928.20755"})
		m.cursor = len(m.tools) - 1
		m.update(tea.KeyMsg{Type: tea.KeyEnter})
		m.update(tea.KeyMsg{Type: tea.KeyDown})
		m.update(tea.KeyMsg{Type: tea.KeyEnter})
		tool := m.toToolConfigs()[m.cursor]
		if tool.Mode != config.ModeMirror || tool.HostVersion != "26.928.20755" || tool.PinnedVersion != "" || tool.ContainerVersion != "old" {
			t.Fatalf("wrong tool state: %+v", tool)
		}
		view := m.viewDetail(100, 35)
		if strings.Contains(view, "Version Mode") || !strings.Contains(view, "official Linux desktop package") || !strings.Contains(view, "cooper vm chatgpt") {
			t.Fatal("wrong desktop instructions")
		}
	}
}

func TestMissingHostToolCannotUseSavedVersion(t *testing.T) {
	m := newAIToolsModel([]config.ToolConfig{{Name: "chatgpt", HostVersion: "26.928.20755"}}, desktopVersions{})
	m.cursor = len(m.tools) - 1
	m.update(tea.KeyMsg{Type: tea.KeySpace})
	tool := m.toToolConfigs()[m.cursor]
	if tool.Enabled || tool.HostVersion != "" {
		t.Fatalf("missing host tool accepted: %+v", tool)
	}
}
