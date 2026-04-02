package configure

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/tableutil"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// toolScreenResult is returned by programming/aicli update methods.
type toolScreenResult int

const (
	toolScreenNone toolScreenResult = iota
	toolScreenBack
)

// toolEntry represents a single tool in the list view.
type toolEntry struct {
	name             string // internal name (e.g., "go", "node")
	displayName      string // display name (e.g., "Go", "Node.js")
	enabled          bool
	mode             config.VersionMode
	hostVersion      string // live-detected host version
	pinVersion       string // user-pinned version
	containerVersion string // version currently built into the container image
}

// programmingModel manages the Programming Tools screen.
type programmingModel struct {
	tools  []toolEntry
	cursor int

	// Detail view state.
	inDetail     bool
	detailCursor int // 0=mirror, 1=latest, 2=pin
	pinInput     textInput
	pinError     string

	// Scroll state for layout.
	scrollOffset         int
	detailScrollOffset   int
	lastHeight           int // cached terminal height for scroll calculations in Update
	lastMaxScroll        int // cached max scroll offset from last render
	lastDetailMaxScroll  int
}

var defaultProgrammingTools = []toolEntry{
	{name: "go", displayName: "Go"},
	{name: "node", displayName: "Node.js"},
	{name: "python", displayName: "Python"},
}

func newProgrammingModel(existing []config.ToolConfig) programmingModel {
	tools := make([]toolEntry, len(defaultProgrammingTools))
	copy(tools, defaultProgrammingTools)

	// Detect host versions for all tools.
	for i := range tools {
		v, err := config.DetectHostVersion(tools[i].name)
		if err == nil {
			tools[i].hostVersion = v
		}
	}

	// Merge with existing config.
	// Note: hostVersion is NOT overwritten from config — the live-detected
	// value takes priority over the stale value from last build.
	for _, tc := range existing {
		for i := range tools {
			if tools[i].name == tc.Name {
				tools[i].enabled = tc.Enabled
				tools[i].mode = tc.Mode
				tools[i].containerVersion = tc.ContainerVersion
				if tc.PinnedVersion != "" {
					tools[i].pinVersion = tc.PinnedVersion
				}
				if tools[i].hostVersion == "" && tc.HostVersion != "" {
					tools[i].hostVersion = tc.HostVersion
				}
				break
			}
		}
	}

	// If no existing config, auto-enable detected tools.
	// Mirror mode only if host version detected; otherwise Latest.
	if len(existing) == 0 {
		for i := range tools {
			if tools[i].hostVersion != "" {
				tools[i].enabled = true
				tools[i].mode = config.ModeMirror
			}
		}
	}

	return programmingModel{
		tools:    tools,
		pinInput: newTextInput("e.g., 1.22.5", 30),
	}
}

func (m *programmingModel) update(msg tea.Msg) toolScreenResult {
	if m.inDetail {
		return m.updateDetail(msg)
	}
	return m.updateList(msg)
}

func (m *programmingModel) updateList(msg tea.Msg) toolScreenResult {
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

func (m *programmingModel) updateDetail(msg tea.Msg) toolScreenResult {
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
				// Validate the pinned version.
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

func (m *programmingModel) view(width, height int) string {
	m.lastHeight = height
	if m.inDetail {
		return m.viewDetail(width, height)
	}
	return m.viewList(width, height)
}

func (m *programmingModel) viewList(width, height int) string {
	breadcrumb := breadcrumbStyle().Render(theme.BarrelEmoji+" Configure > ") +
		lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render("Programming Tools")

	header := breadcrumb

	description := lipgloss.NewStyle().Foreground(theme.ColorDusty).Render(
		" Detected host tools are shown. Toggle tools on/off, select to configure version.")

	onStyle := lipgloss.NewStyle().Foreground(theme.ColorProof)
	offStyle := lipgloss.NewStyle().Foreground(theme.ColorFaded)
	modeStyles := map[config.VersionMode]lipgloss.Style{
		config.ModeMirror: lipgloss.NewStyle().Foreground(theme.ColorSlateBlue),
		config.ModeLatest: lipgloss.NewStyle().Foreground(theme.ColorVerdigris),
		config.ModePin:    lipgloss.NewStyle().Foreground(theme.ColorAmber),
	}

	// Build table with all columns: PREFIX, TOOL, STATUS, VERSION, HOST VERSION, MODE.
	// PREFIX contains the arrow and toggle indicator; it is part of the table
	// so that all columns align regardless of content width.
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
			// Highlight if new version differs from built version.
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
	content += infoBox(" Tools not in this list can be added manually in\n"+
		" ~/.cooper/cli/Dockerfile.user which layers on top of the\n"+
		" generated Dockerfile. Cooper never modifies Dockerfile.user.", width)

	footer := " " + helpBar("[Space Toggle]", "[Enter Configure]", "["+theme.IconArrowUp+theme.IconArrowDown+" Nav]", "[Esc Back]")

	ly := newLayout(header, content, footer, width, height)
	ly.scrollOffset = m.scrollOffset
	// Auto-scroll to keep cursor visible: the cursor row is at line offset
	// 2 (header + separator) + cursor index in the content.
	ly.EnsureVisible(2 + m.cursor)
	result := ly.Render()
	m.scrollOffset = ly.scrollOffset
	m.lastMaxScroll = ly.MaxScrollOffset()
	return result
}

func (m *programmingModel) viewDetail(width, height int) string {
	t := m.tools[m.cursor]
	breadcrumb := breadcrumbStyle().Render(theme.BarrelEmoji+" Configure > Programming Tools > ") +
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

	// Radio buttons: conditionally include Mirror only if host version is detected.
	type modeOption struct {
		name string
		desc string
		mode config.VersionMode
	}
	var modes []modeOption
	if t.hostVersion != "" {
		modes = append(modes, modeOption{"Mirror", fmt.Sprintf("Install same version as host: %s", t.hostVersion), config.ModeMirror})
	}
	modes = append(modes,
		modeOption{"Latest", "Install latest available", config.ModeLatest},
		modeOption{"Pin", "Specify exact version", config.ModePin},
	)

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

		if mode.mode == config.ModePin { // Pin - show input.
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
		" "+theme.BorderH+theme.BorderH+" Version Info "+repeatStr(theme.BorderH, 40)) + "\n\n"

	inner += fmt.Sprintf("  Host version:      %s\n",
		lipgloss.NewStyle().Foreground(theme.ColorLinen).Render(displayOrDash(t.hostVersion)))
	inner += fmt.Sprintf("  Container version: %s\n",
		lipgloss.NewStyle().Foreground(theme.ColorLinen).Render(displayOrDash(resolvedVersion(t))))
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
	// The box content starts at the top of the content string; each radio
	// option occupies roughly 1 rendered line within the box. We estimate
	// the cursor's line position as a small offset into the content.
	ly.EnsureVisible(m.detailCursor)
	result := ly.Render()
	m.detailScrollOffset = ly.scrollOffset
	m.lastDetailMaxScroll = ly.MaxScrollOffset()
	return result
}

func (m *programmingModel) toToolConfigs() []config.ToolConfig {
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

// resolvedVersion returns the version string that would be used for the tool.
func resolvedVersion(t toolEntry) string {
	switch t.mode {
	case config.ModeMirror:
		return displayOrDash(t.hostVersion)
	case config.ModePin:
		return displayOrDash(t.pinVersion)
	case config.ModeLatest:
		return "latest"
	default:
		return theme.BorderH
	}
}

func displayOrDash(s string) string {
	if s == "" {
		return theme.BorderH
	}
	return s
}

// detailModes returns the available version modes for the currently selected tool.
// Mirror is only available when the host version is detected.
func (m *programmingModel) detailModes() []config.VersionMode {
	t := m.tools[m.cursor]
	var modes []config.VersionMode
	if t.hostVersion != "" {
		modes = append(modes, config.ModeMirror)
	}
	modes = append(modes, config.ModeLatest, config.ModePin)
	return modes
}

// detailModeCount returns how many mode options are shown in the detail view.
func (m *programmingModel) detailModeCount() int {
	return len(m.detailModes())
}

// detailModeAtCursor returns the VersionMode at the current detailCursor position.
func (m *programmingModel) detailModeAtCursor() config.VersionMode {
	modes := m.detailModes()
	if m.detailCursor >= 0 && m.detailCursor < len(modes) {
		return modes[m.detailCursor]
	}
	return config.ModeLatest
}

// detailCursorForMode returns the cursor index for a given mode in the dynamic list.
func (m *programmingModel) detailCursorForMode(mode config.VersionMode) int {
	for i, md := range m.detailModes() {
		if md == mode {
			return i
		}
	}
	return 0
}

func modeToIndex(m config.VersionMode) int {
	switch m {
	case config.ModeMirror:
		return 0
	case config.ModeLatest:
		return 1
	case config.ModePin:
		return 2
	default:
		return 0
	}
}

func modeMatchesIndex(m config.VersionMode, idx int) bool {
	return modeToIndex(m) == idx
}

// --- Shared view helpers ---

func breadcrumbStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.ColorDusty)
}

func infoBox(text string, width int) string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(theme.ColorOakLight).
		Foreground(theme.ColorDusty).
		Padding(0, 1).
		Width(min(72, width-4))
	return boxStyle.Render(text)
}

func helpBar(items ...string) string {
	var s string
	for i, item := range items {
		if i > 0 {
			s += "  "
		}
		s += theme.HelpKeyStyle.Render(item)
	}
	return s
}

func repeatStr(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
