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
	ValidateVersion(string, string) (bool, error)
}

type toolVersionCheckedMsg struct {
	request int
	version string
	valid   bool
	err     error
}

// aicliModel manages CLI and desktop version choices through one tool table.
type aicliModel struct {
	tools           []toolEntry
	cursor          int
	versions        toolVersions
	versionRequest  int
	checkingVersion bool

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

func defaultAITools() []toolEntry {
	defs := aitool.Definitions()
	tools := make([]toolEntry, len(defs))
	for i, def := range defs {
		tools[i] = toolEntry{name: def.Name, displayName: def.DisplayName, mode: config.ModeLatest}
	}
	return tools
}

func newAICLIModel(existing []config.ToolConfig) aicliModel {
	return newAIToolsModel(existing, app.ToolVersions{})
}

func newAIToolsModel(existing []config.ToolConfig, versions toolVersions) aicliModel {
	tools := defaultAITools()

	// Detect host versions.
	for i := range tools {
		v, err := versions.DetectHostVersion(tools[i].name)
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
	// Off is not a selectable AI version mode. Older configs and rows added by
	// a catalog migration can contain its zero value. Keep the tool disabled,
	// but give it a usable version mode for a later Space toggle.
	for i := range tools {
		if tools[i].mode == config.ModeOff {
			tools[i].mode = config.ModeLatest
		}
	}

	// Start a new configuration with detected tools in Mirror mode so the
	// initial image versions match the tools that the user already runs.
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
		versions: versions,
		pinInput: newTextInput("e.g., 1.0.5", 30),
	}
}

func (m *aicliModel) update(msg tea.Msg) (toolScreenResult, tea.Cmd) {
	if result, ok := msg.(toolVersionCheckedMsg); ok {
		if result.request != m.versionRequest || !m.checkingVersion {
			return toolScreenNone, nil
		}
		m.checkingVersion = false
		switch {
		case result.err != nil:
			m.pinError = fmt.Sprintf("Version check failed: %v", result.err)
		case !result.valid:
			m.pinError = fmt.Sprintf("Version %s was not found", result.version)
		default:
			m.tools[m.cursor].pinVersion = result.version
			m.tools[m.cursor].mode = config.ModePin
			m.pinInput.Blur()
			m.pinError = ""
		}
		return toolScreenNone, nil
	}
	if m.checkingVersion {
		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "esc" {
			m.checkingVersion = false
			m.versionRequest++
			m.pinError = ""
		}
		return toolScreenNone, nil
	}
	if m.inDetail {
		return m.updateDetail(msg)
	}
	return m.updateList(msg), nil
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
			if m.tools[m.cursor].enabled && m.tools[m.cursor].mode == config.ModeOff {
				m.tools[m.cursor].mode = config.ModeLatest
			}
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

func (m *aicliModel) updateDetail(msg tea.Msg) (toolScreenResult, tea.Cmd) {
	tool := &m.tools[m.cursor]

	// If pin mode is selected and pin input is focused, route keys there.
	if m.detailModeAtCursor() == config.ModePin && m.pinInput.focused {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				m.pinInput.Blur()
				return toolScreenNone, nil
			case "enter":
				v := m.pinInput.Value()
				if v == "" {
					m.pinError = "Version cannot be empty"
					return toolScreenNone, nil
				}
				m.pinError = "Checking version... (Esc cancels)"
				m.checkingVersion = true
				m.versionRequest++
				request, name, versions := m.versionRequest, tool.name, m.versions
				return toolScreenNone, func() tea.Msg {
					valid, err := versions.ValidateVersion(name, v)
					return toolVersionCheckedMsg{request: request, version: v, valid: valid, err: err}
				}
			default:
				m.pinInput.handleKeyMsg(msg)
			}
		}
		return toolScreenNone, nil
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
	return toolScreenNone, nil
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
		lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render("AI Tools")

	header := breadcrumb

	description := lipgloss.NewStyle().Foreground(theme.ColorDusty).Render(
		" Select CLI and desktop apps to install with cooper build.")

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

		name := t.displayName
		if aitool.IsDesktop(t.name) {
			name += " (desktop)"
		}
		tbl.AddRow(toggle, name, status, builtVer, hostVer, newVer, modeStr)
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
	breadcrumb := breadcrumbStyle().Render(theme.BarrelEmoji+" Configure > AI Tools > ") +
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
	latestDesc := "Install latest from npm"
	if t.name == "grok" {
		latestDesc = "Install latest from xAI CLI channel"
	}
	if t.name == "chatgpt" {
		latestDesc = "Install the official Linux desktop package"
	}
	modes = append(modes, modeOption{"Latest", latestDesc, config.ModeLatest})
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
	if aitool.IsDesktop(t.name) {
		inner += "\n  cooper vm chatgpt opens the app in a local viewer.\n" +
			"  The desktop has a terminal (Ctrl+Alt+T).\n" +
			"  Tasks have full access to the workspace and mounted state.\n"
	}

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
