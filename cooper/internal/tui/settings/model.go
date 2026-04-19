// Package settings implements the Runtime tab sub-model.
// It shows runtime-editable settings as labeled values with
// numeric edit modals and checkbox toggles in a scrollable viewport.
package settings

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/tableutil"
	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// SettingsChangedMsg is emitted when a setting value changes.
// The root model should apply these values to the runtime config.
type SettingsChangedMsg struct {
	MonitorTimeoutSecs  int
	BlockedHistoryLimit int
	AllowedHistoryLimit int
	BridgeLogLimit      int
	ClipboardTTLSecs    int
	ClipboardMaxMB      int
	ProxyAlertSound     bool
}

type settingKind int

const (
	settingKindNumber settingKind = iota
	settingKindToggle
)

// settingDef describes one editable setting.
type settingDef struct {
	Kind        settingKind
	Label       string
	Unit        string
	Description string
	Min         int
	Max         int
}

var settingDefs = []settingDef{
	{
		Kind:        settingKindNumber,
		Label:       "Monitor approval timeout",
		Unit:        "seconds",
		Description: "How long to wait for approval before automatically denying a request. Lower values are more secure but require faster reactions.",
		Min:         1,
		Max:         60,
	},
	{
		Kind:        settingKindNumber,
		Label:       "Blocked history limit",
		Unit:        "entries",
		Description: "Maximum number of blocked requests shown in the TUI. Full logs are always written to ~/.cooper/logs/ regardless of this display limit.",
		Min:         50,
		Max:         10000,
	},
	{
		Kind:        settingKindNumber,
		Label:       "Allowed history limit",
		Unit:        "entries",
		Description: "Maximum number of allowed requests shown in the TUI. Full logs are always written to ~/.cooper/logs/ regardless of this display limit.",
		Min:         50,
		Max:         10000,
	},
	{
		Kind:        settingKindNumber,
		Label:       "Bridge log limit",
		Unit:        "entries",
		Description: "Maximum number of bridge execution logs shown in the TUI. Full logs are always written to ~/.cooper/logs/ regardless of this display limit.",
		Min:         50,
		Max:         10000,
	},
	{
		Kind:        settingKindNumber,
		Label:       "Clipboard TTL",
		Unit:        "seconds",
		Description: "How long a staged clipboard image remains available to barrels before it expires. User can always delete early or replace with a new capture.",
		Min:         10,
		Max:         3600,
	},
	{
		Kind:        settingKindNumber,
		Label:       "Clipboard max size",
		Unit:        "MB",
		Description: "Maximum size of a clipboard image payload in megabytes. Oversized clipboard images are rejected at capture time.",
		Min:         1,
		Max:         100,
	},
	{
		Kind:        settingKindToggle,
		Label:       "Proxy approval alert sound",
		Description: "Play a short host-side sound when a new manual proxy approval request arrives. Disabled by default so Cooper stays quiet unless you want audible prompts.",
	},
}

// Model is the sub-model for the Runtime tab.
type Model struct {
	// Settings values.
	values          [6]int // Numeric values in display order.
	proxyAlertSound bool
	selected        int       // Currently highlighted setting.
	editing         bool      // True when the settings edit modal is open.
	editBuf         string    // Buffer for digit input in modal.
	editErr         string    // Validation error shown in modal.
	dirtyAt         time.Time // When the last value change was made (zero = no change this session).

	// Scrollable viewport for content.
	viewport components.ScrollableContent

	// Layout.
	width  int // Cached width for modal rendering.
	height int // Cached height for modal rendering.
}

// IsEditing returns true when the settings model has an edit modal open
// that consumes character key input.
func (m *Model) IsEditing() bool {
	return m.editing
}

// New creates a new settings model with the given initial values.
func New(monitorTimeout, blockedLimit, allowedLimit, bridgeLogLimit, clipboardTTL, clipboardMaxMB int, proxyAlertSound bool) *Model {
	return &Model{
		values:          [6]int{monitorTimeout, blockedLimit, allowedLimit, bridgeLogLimit, clipboardTTL, clipboardMaxMB},
		proxyAlertSound: proxyAlertSound,
	}
}

// Init satisfies the SubModel interface.
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update satisfies theme.SubModel.
func (m *Model) Update(msg tea.Msg) (theme.SubModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if !m.editing {
			m.viewport.HandleMouse(msg, m.bodyHeight())
		}
		return m, nil
	case tea.KeyMsg:
		if m.editing {
			return m.handleEditKey(msg)
		}
		return m.handleNormalKey(msg)
	}
	return m, nil
}

// ----- Settings key handlers -----

// settingRowOffset is the line number where setting rows begin in the scrollable body.
// The body starts directly with setting rows (line 0).
const settingRowOffset = 0

func (m *Model) handleNormalKey(msg tea.KeyMsg) (theme.SubModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.selected > 0 {
			m.selected--
			m.viewport.EnsureLineVisible(settingRowOffset+m.selected, m.bodyHeight())
		} else {
			m.viewport.HandleKey(msg, m.bodyHeight())
		}
	case "down", "j":
		if m.selected < len(settingDefs)-1 {
			m.selected++
			m.viewport.EnsureLineVisible(settingRowOffset+m.selected, m.bodyHeight())
		} else {
			m.viewport.HandleKey(msg, m.bodyHeight())
		}
	case "enter":
		if m.selectedDef().Kind == settingKindToggle {
			return m, m.setProxyAlertSound(!m.proxyAlertSound)
		}
		m.editing = true
		m.editBuf = strconv.Itoa(m.values[m.selected])
	case " ":
		if m.selectedDef().Kind == settingKindToggle {
			return m, m.setProxyAlertSound(!m.proxyAlertSound)
		}
	case "left", "h":
		if m.selectedDef().Kind == settingKindToggle {
			return m, m.setProxyAlertSound(false)
		}
		m.adjustValue(-1)
		return m, m.emitChange()
	case "right", "l":
		if m.selectedDef().Kind == settingKindToggle {
			return m, m.setProxyAlertSound(true)
		}
		m.adjustValue(1)
		return m, m.emitChange()
	default:
		// Let viewport handle scroll keys (pgup/pgdown/home/end).
		m.viewport.HandleKey(msg, m.bodyHeight())
	}
	return m, nil
}

func (m *Model) handleEditKey(msg tea.KeyMsg) (theme.SubModel, tea.Cmd) {
	switch msg.String() {
	case "esc", "enter":
		// Commit the edited value.
		val, err := strconv.Atoi(m.editBuf)
		if err != nil {
			// Revert to previous value on parse error.
			m.editing = false
			return m, nil
		}
		m.values[m.selected] = clamp(val, settingDefs[m.selected].Min, settingDefs[m.selected].Max)
		m.editing = false
		return m, m.emitChange()
	case "backspace":
		if len(m.editBuf) > 0 {
			m.editBuf = components.TrimLastRune(m.editBuf)
		}
	case "left", "h":
		m.adjustValueFromBuf(-1)
	case "right", "l":
		m.adjustValueFromBuf(1)
	case "shift+left", "H":
		m.adjustValueFromBuf(-10)
	case "shift+right", "L":
		m.adjustValueFromBuf(10)
	default:
		if text := components.TextEntryFromKeyMsg(msg, isDigitRune); text != "" {
			m.editBuf += text
		}
	}
	return m, nil
}

func isDigitRune(r rune) bool {
	return r >= '0' && r <= '9'
}

// adjustValue changes the currently selected value by delta and clamps it.
func (m *Model) adjustValue(delta int) {
	def := settingDefs[m.selected]
	m.values[m.selected] = clamp(m.values[m.selected]+delta, def.Min, def.Max)
}

// adjustValueFromBuf parses the edit buffer, adjusts by delta, and writes back.
func (m *Model) adjustValueFromBuf(delta int) {
	val, err := strconv.Atoi(m.editBuf)
	if err != nil {
		return
	}
	def := settingDefs[m.selected]
	val = clamp(val+delta, def.Min, def.Max)
	m.editBuf = strconv.Itoa(val)
}

// emitChange returns a command that sends a SettingsChangedMsg.
func (m *Model) emitChange() tea.Cmd {
	m.dirtyAt = time.Now()
	msg := SettingsChangedMsg{
		MonitorTimeoutSecs:  m.values[0],
		BlockedHistoryLimit: m.values[1],
		AllowedHistoryLimit: m.values[2],
		BridgeLogLimit:      m.values[3],
		ClipboardTTLSecs:    m.values[4],
		ClipboardMaxMB:      m.values[5],
		ProxyAlertSound:     m.proxyAlertSound,
	}
	return func() tea.Msg { return msg }
}

func (m *Model) selectedDef() settingDef {
	return settingDefs[m.selected]
}

func (m *Model) setProxyAlertSound(enabled bool) tea.Cmd {
	if m.proxyAlertSound == enabled {
		return nil
	}
	m.proxyAlertSound = enabled
	return m.emitChange()
}

// ----- View -----

// headerLines is the number of fixed header lines (title + blank + column header + separator).
const headerLines = 4

// bodyHeight returns the available height for the scrollable body area.
func (m *Model) bodyHeight() int {
	h := m.height - headerLines
	if h < 1 {
		h = 1
	}
	return h
}

// View satisfies the SubModel interface.
func (m *Model) View(width, height int) string {
	if width < 2 {
		width = 2
	}
	m.width = width
	m.height = height

	// Fixed header: title + column header (not scrollable).
	header := m.renderHeader(width)

	// Scrollable body: setting rows + info box.
	body := m.renderBody(width)
	m.viewport.SetContent(body)
	scrollableBody := m.viewport.View(width, m.bodyHeight())

	result := header + scrollableBody

	// Overlay edit modal if active.
	if m.editing {
		result = m.overlayEditModal(result)
	}

	return result
}

// renderHeader renders the fixed header (title row + column headers).
func (m *Model) renderHeader(width int) string {
	var b strings.Builder

	titleStyle := lipgloss.NewStyle().Foreground(theme.ColorLinen).Bold(true)
	subtitleStyle := lipgloss.NewStyle().Foreground(theme.ColorDusty).Italic(true)
	b.WriteString(" " + titleStyle.Render("Runtime Settings"))
	subtitle := subtitleStyle.Render("Changes take effect immediately")
	gap := width - lipgloss.Width(" Runtime Settings") - lipgloss.Width(subtitle) - 2
	if gap < 1 {
		gap = 1
	}
	b.WriteString(strings.Repeat(" ", gap) + subtitle + "\n")
	b.WriteString("\n")

	// Build a single table with header and all rows for consistent alignment.
	settingsTbl := tableutil.NewTable("SETTING", "VALUE")
	settingsTbl.SetHeaderStyle(theme.ColorDusty, true)
	settingsTbl.SetMinWidth(0, 36)
	settingsTbl.SetMinWidth(1, 30)

	for i, def := range settingDefs {
		labelStyle := lipgloss.NewStyle().Foreground(theme.ColorParchment)
		if i == m.selected {
			labelStyle = labelStyle.Bold(true)
		}
		settingsTbl.AddRow(labelStyle.Render(def.Label), m.renderValue(i))
	}

	b.WriteString(" " + settingsTbl.RenderHeader() + "\n")
	b.WriteString(theme.DividerStyle.Render(" "+strings.Repeat(theme.BorderH, width-2)) + "\n")

	return b.String()
}

// renderBody renders the scrollable body (setting rows + info box).
func (m *Model) renderBody(width int) string {
	var b strings.Builder

	// Build table again for data rows (same structure as header).
	settingsTbl := tableutil.NewTable("SETTING", "VALUE")
	settingsTbl.SetMinWidth(0, 36)
	settingsTbl.SetMinWidth(1, 30)

	for i, def := range settingDefs {
		labelStyle := lipgloss.NewStyle().Foreground(theme.ColorParchment)
		if i == m.selected {
			labelStyle = labelStyle.Bold(true)
		}
		settingsTbl.AddRow(labelStyle.Render(def.Label), m.renderValue(i))
	}

	_, rows := settingsTbl.RenderRows(0)
	for i, row := range rows {
		prefix := "  "
		if i == m.selected {
			prefix = theme.SelectionArrowStyle.Render(theme.IconArrowRight) + " "
		}
		line := prefix + row
		if i == m.selected {
			line = lipgloss.NewStyle().Background(theme.ColorOakMid).Render(line)
		}
		b.WriteString(line + "\n")
	}

	// Info box with description for selected setting.
	b.WriteString("\n\n")
	desc := settingDefs[m.selected].Description
	infoLines := wrapText(desc, width-8)
	var infoContent []string
	for _, line := range infoLines {
		infoContent = append(infoContent, theme.InfoTextStyle.Render("  "+line))
	}
	infoContent = append(infoContent, "")
	infoContent = append(infoContent,
		theme.InfoTextStyle.Render("  Full logs are always written to ~/.cooper/logs/ regardless of these"),
	)
	infoContent = append(infoContent,
		theme.InfoTextStyle.Render("  display limits."),
	)

	infoBox := theme.InfoBoxStyle.Width(width - 2).Render(strings.Join(infoContent, "\n"))
	b.WriteString(infoBox)

	// Show confirmation note briefly (3 seconds) after a value change.
	if !m.dirtyAt.IsZero() && time.Since(m.dirtyAt) < 3*time.Second {
		b.WriteString("\n")
		confirmStyle := lipgloss.NewStyle().Foreground(theme.ColorProof).Italic(true)
		b.WriteString(" " + confirmStyle.Render(theme.IconCheck+" Settings applied. Changes take effect immediately."))
	}

	return b.String()
}

// overlayEditModal renders an edit modal centered on top of the content.
func (m *Model) overlayEditModal(bg string) string {
	def := settingDefs[m.selected]

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(theme.ColorAmber).
		Padding(1, 3).
		Width(44)

	titleStyle := lipgloss.NewStyle().Foreground(theme.ColorParchment).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(theme.ColorLinen)
	hintStyle := lipgloss.NewStyle().Foreground(theme.ColorDusty)

	var inner string
	inner += titleStyle.Render("Edit: "+def.Label) + "\n\n"
	inner += labelStyle.Render("Value ("+def.Unit+"):") + "\n"

	// Input display.
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(theme.ColorAmber).
		Foreground(theme.ColorParchment).
		Width(20)
	cursor := lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render("_")
	inner += inputStyle.Render(m.editBuf+cursor) + "\n"

	if m.editErr != "" {
		inner += lipgloss.NewStyle().Foreground(theme.ColorFlame).Render(m.editErr) + "\n"
	}

	inner += "\n" + hintStyle.Render(fmt.Sprintf("Range: %d -- %d %s", def.Min, def.Max, def.Unit)) + "\n\n"

	// Buttons.
	inner += lipgloss.NewStyle().Foreground(theme.ColorProof).Bold(true).Render("[Enter Save]") +
		"    " + lipgloss.NewStyle().Foreground(theme.ColorDusty).Render("[Esc Cancel]")

	modal := boxStyle.Render(inner)

	// Center the modal on the background.
	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		modal,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(theme.ColorCharcoal),
	)
}

// ----- Helpers -----

func clamp(val, min, max int) int {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

func (m *Model) renderValue(index int) string {
	def := settingDefs[index]
	bracketColor := theme.ColorOakLight
	if index == m.selected {
		bracketColor = theme.ColorAmber
	}

	if def.Kind == settingKindToggle {
		checkMark := " "
		status := "Disabled"
		statusColor := theme.ColorDusty
		if m.proxyAlertSound {
			checkMark = "x"
			status = "Enabled"
			statusColor = theme.ColorProof
		}
		return lipgloss.NewStyle().Foreground(bracketColor).Render("[") +
			lipgloss.NewStyle().Foreground(theme.ColorAmber).Render(checkMark) +
			lipgloss.NewStyle().Foreground(bracketColor).Render("]") +
			" " + lipgloss.NewStyle().Foreground(statusColor).Render(status)
	}

	return lipgloss.NewStyle().Foreground(bracketColor).Render("[") +
		lipgloss.NewStyle().Foreground(theme.ColorAmber).Render(fmt.Sprintf("%4d", m.values[index])) +
		lipgloss.NewStyle().Foreground(bracketColor).Render("]") +
		" " + lipgloss.NewStyle().Foreground(theme.ColorDusty).Render(def.Unit)
}

// wrapText splits text into lines that fit within maxWidth characters.
func wrapText(text string, maxWidth int) []string {
	if maxWidth < 1 {
		maxWidth = 1
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}

	var lines []string
	current := words[0]
	for _, w := range words[1:] {
		if len(current)+1+len(w) > maxWidth {
			lines = append(lines, current)
			current = w
		} else {
			current += " " + w
		}
	}
	lines = append(lines, current)
	return lines
}
