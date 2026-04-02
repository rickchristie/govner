package configure

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/tableutil"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// aicliModel manages the AI CLI Tools screen.
type aicliModel struct {
	tools  []toolEntry
	cursor int

	// Detail view state.
	inDetail     bool
	detailCursor int // 0=latest, 1=mirror, 2=pin
	pinInput     textInput
	pinError     string

	// Scroll state for layout.
	scrollOffset        int
	detailScrollOffset  int
	lastHeight          int // cached terminal height for scroll calculations in Update
	lastMaxScroll       int // cached max scroll offset from last render
	lastDetailMaxScroll int
}

var defaultAITools = []toolEntry{
	{name: "claude", displayName: "Claude Code"},
	{name: "copilot", displayName: "Copilot CLI"},
	{name: "codex", displayName: "Codex CLI"},
	{name: "opencode", displayName: "OpenCode"},
}

func newAICLIModel(existing []config.ToolConfig) aicliModel {
	tools := make([]toolEntry, len(defaultAITools))
	copy(tools, defaultAITools)

	// Detect host versions.
	for i := range tools {
		v, err := config.DetectHostVersion(tools[i].name)
		if err == nil {
			tools[i].hostVersion = v
		}
	}

	// Merge with existing config.
	// Note: hostVersion is NOT overwritten from config — the live-detected
	// value (from DetectHostVersion above) takes priority over the stale
	// value stored in config.json at last build time.
	for _, tc := range existing {
		for i := range tools {
			if tools[i].name == tc.Name {
				tools[i].enabled = tc.Enabled
				tools[i].mode = tc.Mode
				tools[i].containerVersion = tc.ContainerVersion
				if tc.PinnedVersion != "" {
					tools[i].pinVersion = tc.PinnedVersion
				}
				// Only use config's HostVersion if live detection failed.
				if tools[i].hostVersion == "" && tc.HostVersion != "" {
					tools[i].hostVersion = tc.HostVersion
				}
				break
			}
		}
	}

	// If no existing config, auto-enable all detected tools with mirror mode
	// (per REQUIREMENTS.md line 106: "AI CLI tools that are detected in the
	// host machine is on with versions detected at the host machine as starting point").
	if len(existing) == 0 {
		for i := range tools {
			if tools[i].hostVersion != "" {
				tools[i].enabled = true
				tools[i].mode = config.ModeMirror
			}
		}
	}

	return aicliModel{
		tools:    tools,
		pinInput: newTextInput("e.g., 1.0.5", 30),
	}
}

func (m *aicliModel) update(msg tea.Msg) toolScreenResult {
	if m.inDetail {
		return m.updateDetail(msg)
	}
	return m.updateList(msg)
}

func (m *aicliModel) updateList(msg tea.Msg) toolScreenResult {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		handleMouseScroll(msg, &m.scrollOffset, m.lastMaxScroll)
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				ensureLineVisible(&m.scrollOffset, 2+m.cursor, m.lastHeight, 4, 1)
			} else if m.scrollOffset > 0 {
				m.scrollOffset--
			}
		case "down", "j":
			if m.cursor < len(m.tools)-1 {
				m.cursor++
				ensureLineVisible(&m.scrollOffset, 2+m.cursor, m.lastHeight, 4, 1)
			} else if m.scrollOffset < m.lastMaxScroll {
				m.scrollOffset++
			}
		case " ":
			m.tools[m.cursor].enabled = !m.tools[m.cursor].enabled
		case "enter":
			m.inDetail = true
			m.detailScrollOffset = 0
			m.detailCursor = m.detailCursorForMode(m.tools[m.cursor].mode)
			m.pinInput.SetValue(m.tools[m.cursor].pinVersion)
			m.pinError = ""
		case "pgup", "ctrl+u":
			m.scrollOffset -= 10
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
		case "pgdown", "ctrl+d":
			m.scrollOffset += 10
			if m.scrollOffset > m.lastMaxScroll {
				m.scrollOffset = m.lastMaxScroll
			}
		case "esc":
			return toolScreenBack
		}
	}
	return toolScreenNone
}

func (m *aicliModel) updateDetail(msg tea.Msg) toolScreenResult {
	tool := &m.tools[m.cursor]

	// If pin mode is selected and pin input is focused, route keys there.
	if m.detailCursor == 2 && m.pinInput.focused {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				m.pinInput.Blur()
				return toolScreenNone
			case "enter":
				v := m.pinInput.Value()
				if v == "" {
					m.pinError = "Version cannot be empty"
					return toolScreenNone
				}
				valid, err := config.ValidateVersion(tool.name, v)
				if err != nil {
					m.pinError = fmt.Sprintf("Validation error: %v", err)
					return toolScreenNone
				}
				if !valid {
					m.pinError = fmt.Sprintf("Invalid version: %s not found", v)
					return toolScreenNone
				}
				m.pinError = ""
				tool.pinVersion = v
				tool.mode = config.ModePin
				m.pinInput.Blur()
				return toolScreenNone
			default:
				m.pinInput.handleKey(msg.String())
			}
		}
		return toolScreenNone
	}

	switch msg := msg.(type) {
	case tea.MouseMsg:
		handleMouseScroll(msg, &m.detailScrollOffset, m.lastDetailMaxScroll)
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.detailCursor > 0 {
				m.detailCursor--
				ensureLineVisible(&m.detailScrollOffset, m.detailCursor, m.lastHeight, 2, 1)
			} else if m.detailScrollOffset > 0 {
				m.detailScrollOffset--
			}
		case "down", "j":
			maxCursor := m.detailModeCount() - 1
			if m.detailCursor < maxCursor {
				m.detailCursor++
				ensureLineVisible(&m.detailScrollOffset, m.detailCursor, m.lastHeight, 2, 1)
			} else if m.detailScrollOffset < m.lastDetailMaxScroll {
				m.detailScrollOffset++
			}
		case " ", "enter":
			selectedMode := m.detailModeAtCursor()
			tool.mode = selectedMode
			if selectedMode == config.ModePin {
				m.pinInput.Focus()
			}
		case "pgup", "ctrl+u":
			m.detailScrollOffset -= 10
			if m.detailScrollOffset < 0 {
				m.detailScrollOffset = 0
			}
		case "pgdown", "ctrl+d":
			m.detailScrollOffset += 10
			if m.detailScrollOffset > m.lastDetailMaxScroll {
				m.detailScrollOffset = m.lastDetailMaxScroll
			}
		case "esc":
			m.inDetail = false
		}
	}
	return toolScreenNone
}

func (m *aicliModel) view(width, height int) string {
	m.lastHeight = height
	if m.inDetail {
		return m.viewDetail(width, height)
	}
	return m.viewList(width, height)
}

func (m *aicliModel) viewList(width, height int) string {
	breadcrumb := breadcrumbStyle().Render(theme.BarrelEmoji+" Configure > ") +
		lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render("AI CLI Tools")

	header := breadcrumb

	description := lipgloss.NewStyle().Foreground(theme.ColorDusty).Render(
		" Select AI CLI tools to install in containers. Toggle on/off, select to configure.")

	onStyle := lipgloss.NewStyle().Foreground(theme.ColorProof)
	offStyle := lipgloss.NewStyle().Foreground(theme.ColorFaded)
	modeStyles := map[config.VersionMode]lipgloss.Style{
		config.ModeMirror: lipgloss.NewStyle().Foreground(theme.ColorSlateBlue),
		config.ModeLatest: lipgloss.NewStyle().Foreground(theme.ColorVerdigris),
		config.ModePin:    lipgloss.NewStyle().Foreground(theme.ColorAmber),
	}

	// Build table with all columns: PREFIX, TOOL, STATUS, BUILT, HOST, NEW, MODE.
	tbl := tableutil.NewTable("", "TOOL", "STATUS", "BUILT", "HOST", "NEW", "MODE")
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

		newVer := lipgloss.NewStyle().Foreground(theme.ColorFaded).Render(theme.BorderH)
		if t.enabled {
			resolved := resolvedVersion(t)
			if t.containerVersion != "" && resolved != t.containerVersion && resolved != "latest" && resolved != theme.BorderH {
				newVer = lipgloss.NewStyle().Foreground(theme.ColorCopper).Bold(true).Render(resolved)
			} else {
				newVer = lipgloss.NewStyle().Foreground(theme.ColorProof).Render(resolved)
			}
		}

		modeStr := lipgloss.NewStyle().Foreground(theme.ColorFaded).Render(theme.BorderH)
		if t.enabled {
			if ms, ok := modeStyles[t.mode]; ok {
				modeStr = ms.Render(t.mode.String())
			}
		}

		tbl.AddRow(toggle, t.displayName, status, builtVer, hostVer, newVer, modeStr)
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
	content += infoBox(" Enabled AI tools will have their API provider domains automatically\n"+
		" added to the proxy whitelist (e.g., api.anthropic.com for Claude).\n\n"+
		" Additional tools can be added in ~/.cooper/cli/Dockerfile.user.", width)

	footer := " " + helpBar("[Space Toggle]", "[Enter Configure]", "["+theme.IconArrowUp+theme.IconArrowDown+" Nav]", "[Esc Back]")

	ly := newLayout(header, content, footer, width, height)
	ly.scrollOffset = m.scrollOffset
	ly.EnsureVisible(2 + m.cursor)
	result := ly.Render()
	m.scrollOffset = ly.scrollOffset
	m.lastMaxScroll = ly.MaxScrollOffset()
	return result
}

func (m *aicliModel) viewDetail(width, height int) string {
	t := m.tools[m.cursor]
	breadcrumb := breadcrumbStyle().Render(theme.BarrelEmoji+" Configure > AI CLI Tools > ") +
		lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render(t.displayName)

	header := breadcrumb

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(theme.ColorOakLight).
		Padding(1, 2).
		Width(min(70, width-4))

	var inner string

	// Status.
	if t.enabled {
		inner += " Status: " + lipgloss.NewStyle().Foreground(theme.ColorAmber).Render("["+theme.IconDot+"]") +
			lipgloss.NewStyle().Foreground(theme.ColorProof).Render(" Enabled") + "\n\n"
	} else {
		inner += " Status: " + lipgloss.NewStyle().Foreground(theme.ColorDusty).Render("["+theme.IconDotEmpty+"]") +
			lipgloss.NewStyle().Foreground(theme.ColorFaded).Render(" Disabled") + "\n\n"
	}

	inner += " Version Mode:\n\n"

	// Radio buttons: conditionally include Mirror only if host version detected.
	type modeOption struct {
		name string
		desc string
		mode config.VersionMode
	}
	var modes []modeOption
	modes = append(modes, modeOption{"Latest", "Install latest from npm", config.ModeLatest})
	if t.hostVersion != "" {
		modes = append(modes, modeOption{"Mirror", fmt.Sprintf("Install same version as host: %s", t.hostVersion), config.ModeMirror})
	}
	modes = append(modes, modeOption{"Pin", "Specify exact version", config.ModePin})

	for i, mode := range modes {
		radio := lipgloss.NewStyle().Foreground(theme.ColorDusty).Render(theme.IconDotEmpty)
		if t.mode == mode.mode {
			radio = lipgloss.NewStyle().Foreground(theme.ColorAmber).Render(theme.IconDot)
		}
		prefix := "     "
		if i == m.detailCursor {
			prefix = "   " + lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render(theme.IconArrowRight) + " "
		}
		inner += fmt.Sprintf("%s%s %s   %s",
			prefix, radio,
			lipgloss.NewStyle().Foreground(theme.ColorParchment).Render(mode.name),
			lipgloss.NewStyle().Foreground(theme.ColorDusty).Render(mode.desc))

		if mode.mode == config.ModePin {
			pinMargin := 11 // Align with radio label text (past "   ▸ ● Pin").
			inner += "\n" + m.pinInput.viewWithMargin(pinMargin)
			if m.pinError != "" {
				errIndent := lipgloss.NewStyle().MarginLeft(pinMargin)
				inner += "\n" + errIndent.Render(lipgloss.NewStyle().Foreground(theme.ColorFlame).Render(m.pinError))
			}
		}
		inner += "\n"
	}

	inner += "\n"
	inner += lipgloss.NewStyle().Foreground(theme.ColorDusty).Render(
		" "+theme.BorderH+theme.BorderH+" Tool Info "+repeatStr(theme.BorderH, 44)) + "\n\n"

	inner += fmt.Sprintf("  Host version:      %s\n",
		lipgloss.NewStyle().Foreground(theme.ColorLinen).Render(displayOrDash(t.hostVersion)))

	inner += "\n"
	inner += lipgloss.NewStyle().Foreground(theme.ColorDusty).Render(
		"  Mirror and Latest modes will update when you run ") +
		lipgloss.NewStyle().Foreground(theme.ColorAmber).Render("cooper update") +
		lipgloss.NewStyle().Foreground(theme.ColorDusty).Render(".")

	content := boxStyle.Render(inner)

	footer := " " + helpBar("["+theme.IconArrowUp+theme.IconArrowDown+" Nav]", "[Space Select]", "[Esc Back]")

	ly := newLayout(header, content, footer, width, height)
	ly.scrollOffset = m.detailScrollOffset
	// Auto-scroll to keep the selected radio button visible within the box.
	ly.EnsureVisible(m.detailCursor)
	result := ly.Render()
	m.detailScrollOffset = ly.scrollOffset
	m.lastDetailMaxScroll = ly.MaxScrollOffset()
	return result
}

func (m *aicliModel) toToolConfigs() []config.ToolConfig {
	result := make([]config.ToolConfig, len(m.tools))
	for i, t := range m.tools {
		tc := config.ToolConfig{
			Name:             t.name,
			Enabled:          t.enabled,
			Mode:             t.mode,
			HostVersion:      t.hostVersion,
			ContainerVersion: t.containerVersion,
		}
		if t.mode == config.ModePin {
			tc.PinnedVersion = t.pinVersion
		}
		result[i] = tc
	}
	return result
}

// detailModes returns the available version modes for the currently selected AI tool.
// Latest is first (AI tools default to latest), Mirror only if host version detected.
func (m *aicliModel) detailModes() []config.VersionMode {
	t := m.tools[m.cursor]
	var modes []config.VersionMode
	modes = append(modes, config.ModeLatest)
	if t.hostVersion != "" {
		modes = append(modes, config.ModeMirror)
	}
	modes = append(modes, config.ModePin)
	return modes
}

func (m *aicliModel) detailModeCount() int {
	return len(m.detailModes())
}

func (m *aicliModel) detailModeAtCursor() config.VersionMode {
	modes := m.detailModes()
	if m.detailCursor >= 0 && m.detailCursor < len(modes) {
		return modes[m.detailCursor]
	}
	return config.ModeLatest
}

func (m *aicliModel) detailCursorForMode(mode config.VersionMode) int {
	for i, md := range m.detailModes() {
		if md == mode {
			return i
		}
	}
	return 0
}

// aiModeToIndex maps a VersionMode to the AI detail radio index.
// AI tools show Latest first (0), then Mirror (1), then Pin (2).
func aiModeToIndex(m config.VersionMode) int {
	switch m {
	case config.ModeLatest:
		return 0
	case config.ModeMirror:
		return 1
	case config.ModePin:
		return 2
	default:
		return 0
	}
}

func aiModeMatchesIndex(m config.VersionMode, idx int) bool {
	return aiModeToIndex(m) == idx
}
