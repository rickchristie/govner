package configure

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/rickchristie/govner/cooper/internal/config"
)

// NewAIToolsPreviewModel uses fixed versions and cannot save or build. This
// lets the capture image show the real picker without host tools or Docker.
func NewAIToolsPreviewModel() tea.Model {
	tools := []config.ToolConfig{
		{Name: "claude", Enabled: true, Mode: config.ModeMirror, ContainerVersion: "2.1.87"},
		{Name: "codex", Enabled: true, Mode: config.ModeMirror, ContainerVersion: "0.117.0"},
		{Name: "chatgpt", Enabled: true, Mode: config.ModeMirror, ContainerVersion: "26.917.71314"},
	}
	model := &aiToolsPreview{tools: newAIToolsModel(tools, previewVersions{}), width: 100, height: 30}
	model.tools.cursor = len(model.tools.tools) - 1
	return model
}

type previewVersions struct{}

func (previewVersions) DetectHostVersion(name string) (string, error) {
	return map[string]string{"claude": "2.1.87", "codex": "0.117.0", "chatgpt": "26.917.71314"}[name], nil
}

type aiToolsPreview struct {
	tools         aicliModel
	width, height int
}

func (*aiToolsPreview) Init() tea.Cmd { return nil }
func (m *aiToolsPreview) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
		m.tools.update(msg)
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "ctrl+c" {
		return m, tea.Quit
	}
	result, cmd := m.tools.update(msg)
	if result == toolScreenBack {
		return m, tea.Quit
	}
	return m, cmd
}
func (m *aiToolsPreview) View() string { return m.tools.view(m.width, m.height) }

// NewSavePreviewModel cannot write files. Its actions close the test screen.
func NewSavePreviewModel() tea.Model {
	cfg := config.DefaultConfig()
	cfg.AITools = []config.ToolConfig{{Name: "chatgpt", Enabled: true, Mode: config.ModeMirror, HostVersion: "26.928.20755"}}
	return &savePreview{model: newSaveModel(cfg, "/home/example/.cooper", "/home/example/.cooper/config.json", nil)}
}

type savePreview struct {
	model         saveModel
	width, height int
}

func (*savePreview) Init() tea.Cmd { return nil }
func (m *savePreview) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.model.update(msg) != saveNone {
		return m, tea.Quit
	}
	return m, nil
}
func (m *savePreview) View() string { return m.model.view(m.width, m.height) }
