package configure

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/aitool"
	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/tableutil"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

type toolVersions interface {
	DetectHostVersion(string) (string, error)
}

// aicliModel selects tools. AI versions always come from the live host.
type aicliModel struct {
	tools              []toolEntry
	cursor             int
	inDetail           bool
	migrated           bool
	scrollOffset       int
	detailScrollOffset int
	width              int
	height             int
}

func defaultAITools() []toolEntry {
	defs := aitool.Definitions()
	tools := make([]toolEntry, len(defs))
	for i, def := range defs {
		tools[i] = toolEntry{name: def.Name, displayName: def.DisplayName, mode: config.ModeMirror}
	}
	return tools
}

func newAICLIModel(existing []config.ToolConfig) aicliModel {
	return newAIToolsModel(existing, app.ToolVersions{})
}

func newAIToolsModel(existing []config.ToolConfig, versions toolVersions) aicliModel {
	m := aicliModel{tools: defaultAITools(), width: 80, height: 24}
	for i := range m.tools {
		tool := &m.tools[i]
		version, err := versions.DetectHostVersion(tool.name)
		if err == nil {
			tool.hostVersion = version
		}
		tool.enabled = len(existing) == 0 && tool.hostVersion != ""
		for _, saved := range existing {
			if saved.Name != tool.name {
				continue
			}
			tool.enabled = saved.Enabled
			tool.containerVersion = saved.ContainerVersion
			m.migrated = m.migrated || saved.Mode == config.ModePin || saved.Mode == config.ModeLatest
		}
	}
	return m
}

func (m *aicliModel) toggle() {
	tool := &m.tools[m.cursor]
	if tool.enabled {
		tool.enabled = false
		return
	}
	if tool.hostVersion != "" {
		tool.enabled = true
	}
}

func (m *aicliModel) update(msg tea.Msg) (toolScreenResult, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
		m.keepSelectionVisible()
		m.detailScrollOffset = min(m.detailScrollOffset, m.detailLayout(m.width, m.height).MaxScrollOffset())
		return toolScreenNone, nil
	}
	if !m.inDetail {
		return m.updateList(msg), nil
	}
	maxScroll := m.detailLayout(m.width, m.height).MaxScrollOffset()
	switch msg := msg.(type) {
	case tea.MouseMsg:
		handleMouseScroll(msg, &m.detailScrollOffset, maxScroll)
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.inDetail = false
		case " ":
			m.toggle()
		case "down", "j":
			m.detailScrollOffset = min(maxScroll, m.detailScrollOffset+1)
		case "up", "k":
			if m.detailScrollOffset > 0 {
				m.detailScrollOffset--
			}
		case "pgup", "ctrl+u":
			m.detailScrollOffset = max(0, m.detailScrollOffset-10)
		case "pgdown", "ctrl+d":
			m.detailScrollOffset = min(maxScroll, m.detailScrollOffset+10)
		}
	}
	return toolScreenNone, nil
}

func (m *aicliModel) updateList(msg tea.Msg) toolScreenResult {
	maxScroll := m.listLayout(m.width, m.height).MaxScrollOffset()
	switch msg := msg.(type) {
	case tea.MouseMsg:
		handleMouseScroll(msg, &m.scrollOffset, maxScroll)
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.keepSelectionVisible()
			}
		case "down", "j":
			if m.cursor < len(m.tools)-1 {
				m.cursor++
				m.keepSelectionVisible()
			}
		case " ":
			m.toggle()
		case "enter":
			m.inDetail = true
			m.detailScrollOffset = 0
		case "pgup", "ctrl+u":
			m.scrollOffset = max(0, m.scrollOffset-10)
		case "pgdown", "ctrl+d":
			m.scrollOffset = min(maxScroll, m.scrollOffset+10)
		case "esc":
			return toolScreenBack
		}
	}
	return toolScreenNone
}

func (m *aicliModel) keepSelectionVisible() {
	ly := m.listLayout(m.width, m.height)
	ly.EnsureVisible(5 + m.cursor)
	m.scrollOffset = ly.scrollOffset
}

func (m aicliModel) view(width, height int) string {
	if m.inDetail {
		return m.viewDetail(width, height)
	}
	return m.viewList(width, height)
}

func (m aicliModel) viewList(width, height int) string {
	return m.listLayout(width, height).Render()
}

func (m aicliModel) listLayout(width, height int) *layout {
	breadcrumb := breadcrumbStyle().Render(theme.BarrelEmoji+" Configure > ") +
		lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render("AI Tools")

	header := breadcrumb

	description := lipgloss.NewStyle().Foreground(theme.ColorDusty).Render(
		" AI tools always use the host version. Install each tool on the host first.")

	onStyle := lipgloss.NewStyle().Foreground(theme.ColorProof)
	offStyle := lipgloss.NewStyle().Foreground(theme.ColorFaded)
	// Show the two versions that must match before shared state can be used.
	tbl := tableutil.NewTable("", "TOOL", "STATUS", "BUILT", "HOST")
	tbl.SetHeaderStyle(theme.ColorDusty, true)
	sepColor := theme.ColorOakLight
	tbl.SetSeparator(theme.BorderH, &sepColor)

	for _, t := range m.tools {
		toggle := lipgloss.NewStyle().Foreground(theme.ColorDusty).Render("[" + theme.IconDotEmpty + "]")
		status := offStyle.Render("off")
		if t.enabled {
			toggle = lipgloss.NewStyle().Foreground(theme.ColorAmber).Render("[" + theme.IconDot + "]")
			status = onStyle.Render("on")
		}

		builtVer := lipgloss.NewStyle().Foreground(theme.ColorFaded).Render(theme.BorderH)
		if t.containerVersion != "" {
			builtVer = lipgloss.NewStyle().Foreground(theme.ColorDusty).Render(t.containerVersion)
		}

		hostVer := lipgloss.NewStyle().Foreground(theme.ColorFaded).Italic(true).Render("(not detected)")
		if t.hostVersion != "" {
			hostVer = lipgloss.NewStyle().Foreground(theme.ColorLinen).Render(t.hostVersion)
		}

		name := t.displayName
		if aitool.IsDesktop(t.name) {
			name += " (desktop)"
		}
		tbl.AddRow(toggle, name, status, builtVer, hostVer)
	}

	// Render header and separator with the same left margin as data rows.
	rowIndent := "   " // 3 spaces — matches non-selected row prefix.
	var content string
	content += "\n" + description + "\n\n"
	content += rowIndent + tbl.RenderHeader() + "\n"
	content += rowIndent + tbl.RenderSeparator(0) + "\n"

	// Render each row individually so we can add the selection arrow and
	// highlight the selected row.
	_, rows := tbl.RenderRows(0)
	for i, row := range rows {
		prefix := rowIndent
		if i == m.cursor {
			prefix = " " + lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render(theme.IconArrowRight) + " "
		}

		line := prefix + row
		if i == m.cursor {
			line = lipgloss.NewStyle().Background(theme.ColorOakMid).Render(line)
		}
		content += line + "\n"
	}

	content += "\n"
	if m.migrated {
		content += " Previous Latest and Pin settings will be replaced with Mirror.\n\n"
	}
	content += infoBox(" Enabled AI tools will have their API provider domains automatically\n"+
		" added to the proxy whitelist (e.g., api.anthropic.com for Claude).\n\n"+
		" Additional tools can be added in ~/.cooper/cli/Dockerfile.user.", width)

	footer := " " + helpBar("[Space Toggle]", "[Enter Details]", "["+theme.IconArrowUp+theme.IconArrowDown+" Nav]", "[Esc Back]")

	ly := newLayout(header, content, footer, width, height)
	ly.scrollOffset = m.scrollOffset
	return ly
}

func (m aicliModel) viewDetail(width, height int) string {
	return m.detailLayout(width, height).Render()
}

func (m aicliModel) detailLayout(width, height int) *layout {
	t := m.tools[m.cursor]
	header := breadcrumbStyle().Render(theme.BarrelEmoji+" Configure > AI Tools > ") +
		lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render(t.displayName)
	status := "Disabled"
	if t.enabled {
		status = "Enabled"
	}
	content := fmt.Sprintf("\n Status: %s\n\n Host version: %s\n Built version: %s\n\n", status, displayOrDash(t.hostVersion), displayOrDash(t.containerVersion))
	content += " Cooper installs the exact host version.\n Shared settings and history need matching tool versions.\n"
	if t.hostVersion == "" {
		content += "\n Install this tool on the host, then open Configure again.\n"
	} else {
		content += "\n After a host update, run cooper build before starting a session.\n"
	}
	if aitool.IsDesktop(t.name) {
		content += "\n Cooper installs the official Linux desktop package.\n cooper vm chatgpt opens the app in a local viewer.\n The desktop has a terminal (Ctrl+Alt+T).\n Tasks have full access to the workspace and mounted state.\n"
	}
	footer := " " + helpBar("[Space Toggle]", "[Esc Back]")
	ly := newLayout(header, content, footer, width, height)
	ly.scrollOffset = m.detailScrollOffset
	return ly
}

func (m *aicliModel) toToolConfigs() []config.ToolConfig {
	result := make([]config.ToolConfig, len(m.tools))
	for i, t := range m.tools {
		result[i] = config.ToolConfig{Name: t.name, Enabled: t.enabled, Mode: config.ModeMirror,
			HostVersion: t.hostVersion, ContainerVersion: t.containerVersion}
	}
	return result
}
