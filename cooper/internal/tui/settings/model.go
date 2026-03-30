// Package settings implements the Configure tab sub-model.
// It shows runtime-editable settings as labeled number inputs,
// and a port forwarding rules editor with live-reload support.
package settings

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/tableutil"
	"github.com/rickchristie/govner/cooper/internal/tui/components"
	// theme.SubModel is the shared SubModel interface.
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// SettingsChangedMsg is emitted when a setting value changes.
// The root model should apply these values to the runtime config.
type SettingsChangedMsg struct {
	MonitorTimeoutSecs  int
	BlockedHistoryLimit int
	AllowedHistoryLimit int
	BridgeLogLimit      int
}

// PortForwardChangedMsg is emitted when port forwarding rules are modified.
// The root model should write socat config and trigger a live reload.
type PortForwardChangedMsg struct {
	Rules []config.PortForwardRule
}

// section identifies which sub-section of the Configure tab is active.
type section int

const (
	sectionSettings    section = iota
	sectionPortForward
)

// settingDef describes one editable setting.
type settingDef struct {
	Label       string
	Unit        string
	Description string
	Min         int
	Max         int
}

var settingDefs = []settingDef{
	{
		Label:       "Monitor approval timeout",
		Unit:        "seconds",
		Description: "How long to wait for approval before automatically denying a request. Lower values are more secure but require faster reactions.",
		Min:         1,
		Max:         60,
	},
	{
		Label:       "Blocked history limit",
		Unit:        "entries",
		Description: "Maximum number of blocked requests shown in the TUI. Full logs are always written to ~/.cooper/logs/ regardless of this display limit.",
		Min:         50,
		Max:         10000,
	},
	{
		Label:       "Allowed history limit",
		Unit:        "entries",
		Description: "Maximum number of allowed requests shown in the TUI. Full logs are always written to ~/.cooper/logs/ regardless of this display limit.",
		Min:         50,
		Max:         10000,
	},
	{
		Label:       "Bridge log limit",
		Unit:        "entries",
		Description: "Maximum number of bridge execution logs shown in the TUI. Full logs are always written to ~/.cooper/logs/ regardless of this display limit.",
		Min:         50,
		Max:         10000,
	},
}

// ----- Port forward editing state -----

// pfEditMode tracks the editing state for port forwarding rules.
type pfEditMode int

const (
	pfNone     pfEditMode = iota
	pfAdding              // Adding a new rule.
	pfEditing             // Editing an existing rule.
	pfDeleting            // Delete confirmation.
)

// pfStatus tracks the result of a port forward change.
type pfStatus int

const (
	pfClean   pfStatus = iota // No changes, or initial state.
	pfPending                 // Change emitted, waiting for root to confirm.
	pfApplied                 // Root confirmed success.
	pfFailed                  // Root reported failure.
)

// PFApplyResultMsg is sent from the root model back to the settings model
// to report whether a port forward change succeeded or failed.
type PFApplyResultMsg struct {
	OK      bool
	ErrMsg  string
	Rules   []config.PortForwardRule // The rules that were applied (or attempted).
}

// pfField identifies which field is active during port forward editing.
type pfField int

const (
	pfFieldContainerPort pfField = iota
	pfFieldHostPort
	pfFieldDesc
)

// Model is the sub-model for the Configure tab.
type Model struct {
	// Settings section.
	values   [4]int // One value per settingDef.
	selected int    // Currently highlighted setting (in settings section).
	editing  bool   // True when the settings edit modal is open.
	editBuf  string // Buffer for digit input in modal.
	editErr  string // Validation error shown in modal.
	dirty    bool   // True after a value has been changed (shows confirmation).

	// Active section.
	section section

	// Port forwarding section.
	pfRules    []config.PortForwardRule
	pfList     components.ScrollableList
	pfEditMode pfEditMode
	pfEditIdx  int // Index of rule being edited (-1 for new).
	pfField    pfField
	pfContBuf  string // Container port input buffer.
	pfHostBuf  string // Host port input buffer.
	pfDescBuf  string // Description input buffer.
	pfErr      string     // Validation error for port forward modal.
	pfStatus   pfStatus   // Status of the last port forward change.
	pfStatusMsg string    // Error message when pfStatus == pfFailed.

	// Layout.
	width  int // Cached width for modal rendering.
	height int // Cached height for modal rendering.
}

// New creates a new settings model with the given initial values.
func New(monitorTimeout, blockedLimit, allowedLimit, bridgeLogLimit int) *Model {
	return &Model{
		values:    [4]int{monitorTimeout, blockedLimit, allowedLimit, bridgeLogLimit},
		pfList:    components.NewScrollableList(10, 80),
		pfEditIdx: -1,
	}
}

// SetPortForwardRules sets the initial port forwarding rules.
func (m *Model) SetPortForwardRules(rules []config.PortForwardRule) {
	m.pfRules = make([]config.PortForwardRule, len(rules))
	copy(m.pfRules, rules)
	m.syncPFList()
}

// syncPFList rebuilds the ScrollableList items from m.pfRules.
func (m *Model) syncPFList() {
	items := make([]components.ListItem, len(m.pfRules))
	for i, r := range m.pfRules {
		items[i] = components.ListItem{
			ID:   fmt.Sprintf("pf-%d", i),
			Data: r,
		}
	}
	m.pfList.SetItems(items)
}

// Init satisfies the SubModel interface.
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update satisfies theme.SubModel.
func (m *Model) Update(msg tea.Msg) (theme.SubModel, tea.Cmd) {
	switch msg := msg.(type) {
	case PFApplyResultMsg:
		if msg.OK {
			m.pfStatus = pfApplied
			m.pfStatusMsg = ""
			// Rules are confirmed — keep the local state as-is.
		} else {
			m.pfStatus = pfFailed
			m.pfStatusMsg = msg.ErrMsg
			// Revert local rules to what was confirmed.
			m.pfRules = make([]config.PortForwardRule, len(msg.Rules))
			copy(m.pfRules, msg.Rules)
			m.syncPFList()
		}
		return m, nil
	case tea.MouseMsg:
		if m.section == sectionSettings && !m.editing {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				if m.selected > 0 {
					m.selected--
				}
			case tea.MouseButtonWheelDown:
				if m.selected < len(settingDefs)-1 {
					m.selected++
				}
			}
		} else if m.section == sectionPortForward && m.pfEditMode == pfNone {
			m.pfList.HandleMouse(msg)
		}
		return m, nil
	case tea.KeyMsg:
		// Tab key switches sections (unless modal is open).
		if !m.editing && m.pfEditMode == pfNone {
			switch msg.String() {
			case "tab":
				if m.section == sectionSettings {
					m.section = sectionPortForward
				} else {
					m.section = sectionSettings
				}
				return m, nil
			case "shift+tab":
				if m.section == sectionPortForward {
					m.section = sectionSettings
				} else {
					m.section = sectionPortForward
				}
				return m, nil
			}
		}

		if m.section == sectionSettings {
			if m.editing {
				return m.handleEditKey(msg)
			}
			return m.handleNormalKey(msg)
		}
		return m.handlePFKey(msg)
	}
	return m, nil
}

// ----- Settings section key handlers -----

func (m *Model) handleNormalKey(msg tea.KeyMsg) (theme.SubModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.selected > 0 {
			m.selected--
		}
	case "down", "j":
		if m.selected < len(settingDefs)-1 {
			m.selected++
		}
	case "enter":
		m.editing = true
		m.editBuf = strconv.Itoa(m.values[m.selected])
	case "left", "h":
		m.adjustValue(-1)
		return m, m.emitChange()
	case "right", "l":
		m.adjustValue(1)
		return m, m.emitChange()
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
			m.editBuf = m.editBuf[:len(m.editBuf)-1]
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
		// Accept digits only.
		r := msg.String()
		if len(r) == 1 && r[0] >= '0' && r[0] <= '9' {
			m.editBuf += r
		}
	}
	return m, nil
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
	m.dirty = true
	msg := SettingsChangedMsg{
		MonitorTimeoutSecs:  m.values[0],
		BlockedHistoryLimit: m.values[1],
		AllowedHistoryLimit: m.values[2],
		BridgeLogLimit:      m.values[3],
	}
	return func() tea.Msg { return msg }
}

// ----- Port forwarding section key handlers -----

func (m *Model) handlePFKey(msg tea.KeyMsg) (theme.SubModel, tea.Cmd) {
	if m.pfEditMode == pfDeleting {
		return m.handlePFDeleteConfirm(msg)
	}
	if m.pfEditMode == pfAdding || m.pfEditMode == pfEditing {
		return m.handlePFEditInput(msg)
	}
	return m.handlePFNormalKey(msg)
}

func (m *Model) handlePFNormalKey(msg tea.KeyMsg) (theme.SubModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.pfList.MoveUp()
	case "down", "j":
		m.pfList.MoveDown()
	case "n":
		// Add new rule.
		m.pfEditMode = pfAdding
		m.pfEditIdx = -1
		m.pfField = pfFieldContainerPort
		m.pfContBuf = ""
		m.pfHostBuf = ""
		m.pfDescBuf = ""
		m.pfErr = ""
	case "enter", "e":
		// Edit selected rule (range rules are read-only).
		if sel := m.pfList.Selected(); sel != nil {
			if r, ok := sel.Data.(config.PortForwardRule); ok {
				if r.IsRange {
					// Range rules cannot be edited at runtime.
					break
				}
				m.pfEditMode = pfEditing
				m.pfEditIdx = m.pfList.SelectedIdx
				m.pfField = pfFieldContainerPort
				m.pfContBuf = strconv.Itoa(r.ContainerPort)
				m.pfHostBuf = strconv.Itoa(r.HostPort)
				m.pfDescBuf = r.Description
				m.pfErr = ""
			}
		}
	case "x":
		// Delete selected rule (range rules are read-only).
		if len(m.pfRules) > 0 && m.pfList.Selected() != nil {
			if sel := m.pfList.Selected(); sel != nil {
				if r, ok := sel.Data.(config.PortForwardRule); ok && r.IsRange {
					break
				}
			}
			m.pfEditMode = pfDeleting
		}
	}
	return m, nil
}

func (m *Model) handlePFEditInput(msg tea.KeyMsg) (theme.SubModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.pfEditMode = pfNone
		return m, nil
	case "up":
		if m.pfField > pfFieldContainerPort {
			m.pfField--
		}
		return m, nil
	case "down":
		if m.pfField < pfFieldDesc {
			m.pfField++
		}
		return m, nil
	case "enter":
		return m.savePFRule()
	case "backspace":
		switch m.pfField {
		case pfFieldContainerPort:
			if len(m.pfContBuf) > 0 {
				m.pfContBuf = m.pfContBuf[:len(m.pfContBuf)-1]
			}
		case pfFieldHostPort:
			if len(m.pfHostBuf) > 0 {
				m.pfHostBuf = m.pfHostBuf[:len(m.pfHostBuf)-1]
			}
		case pfFieldDesc:
			if len(m.pfDescBuf) > 0 {
				m.pfDescBuf = m.pfDescBuf[:len(m.pfDescBuf)-1]
			}
		}
		return m, nil
	default:
		r := msg.String()
		if len(r) == 1 {
			switch m.pfField {
			case pfFieldContainerPort:
				// Accept digits only.
				if r[0] >= '0' && r[0] <= '9' {
					m.pfContBuf += r
				}
			case pfFieldHostPort:
				if r[0] >= '0' && r[0] <= '9' {
					m.pfHostBuf += r
				}
			case pfFieldDesc:
				m.pfDescBuf += r
			}
		}
	}
	return m, nil
}

func (m *Model) savePFRule() (theme.SubModel, tea.Cmd) {
	// Validate container port.
	cp, err := strconv.Atoi(m.pfContBuf)
	if err != nil || cp < 1 || cp > 65535 {
		m.pfErr = "Container port must be 1-65535"
		return m, nil
	}
	hp, err := strconv.Atoi(m.pfHostBuf)
	if err != nil || hp < 1 || hp > 65535 {
		m.pfErr = "Host port must be 1-65535"
		return m, nil
	}

	rule := config.PortForwardRule{
		ContainerPort: cp,
		HostPort:      hp,
		Description:   m.pfDescBuf,
	}

	// Build candidate rule list for validation.
	candidate := make([]config.PortForwardRule, len(m.pfRules))
	copy(candidate, m.pfRules)
	if m.pfEditMode == pfAdding {
		candidate = append(candidate, rule)
	} else if m.pfEditMode == pfEditing && m.pfEditIdx >= 0 && m.pfEditIdx < len(candidate) {
		candidate[m.pfEditIdx] = rule
	}

	if errMsg := m.validatePFRules(candidate); errMsg != "" {
		m.pfErr = errMsg
		return m, nil
	}

	m.pfRules = candidate
	m.pfEditMode = pfNone
	m.pfStatus = pfPending
	m.syncPFList()
	return m, m.emitPFChange()
}

// validatePFRules checks a candidate rule set for duplicates and collisions
// with the proxy/bridge ports. Returns an error message string, or "" if valid.
func (m *Model) validatePFRules(rules []config.PortForwardRule) string {
	// Check for duplicate container ports.
	seen := make(map[int]bool)
	for _, r := range rules {
		if r.IsRange {
			for p := r.ContainerPort; p <= r.RangeEnd; p++ {
				if seen[p] {
					return fmt.Sprintf("Duplicate container port %d", p)
				}
				seen[p] = true
			}
		} else {
			if seen[r.ContainerPort] {
				return fmt.Sprintf("Duplicate container port %d", r.ContainerPort)
			}
			seen[r.ContainerPort] = true
		}
	}
	return ""
}

func (m *Model) handlePFDeleteConfirm(msg tea.KeyMsg) (theme.SubModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		idx := m.pfList.SelectedIdx
		if idx >= 0 && idx < len(m.pfRules) {
			m.pfRules = append(m.pfRules[:idx], m.pfRules[idx+1:]...)
			m.syncPFList()
		}
		m.pfEditMode = pfNone
		m.pfStatus = pfPending
		return m, m.emitPFChange()
	case "esc":
		m.pfEditMode = pfNone
	}
	return m, nil
}

// emitPFChange returns a command that sends a PortForwardChangedMsg.
func (m *Model) emitPFChange() tea.Cmd {
	rules := make([]config.PortForwardRule, len(m.pfRules))
	copy(rules, m.pfRules)
	return func() tea.Msg { return PortForwardChangedMsg{Rules: rules} }
}

// ----- View -----

// View satisfies the SubModel interface.
func (m *Model) View(width, height int) string {
	m.width = width
	m.height = height
	var b strings.Builder

	// Sub-tab bar.
	b.WriteString(m.renderSubTabs(width))
	b.WriteString("\n\n")

	// Content area (remaining height after sub-tabs).
	contentHeight := height - 3
	if contentHeight < 1 {
		contentHeight = 1
	}

	var content string
	if m.section == sectionSettings {
		content = m.renderSettings(width, contentHeight)
	} else {
		content = m.renderPortForward(width, contentHeight)
	}
	b.WriteString(content)

	return b.String()
}

// renderSubTabs renders the Settings / Port Forwarding sub-tab bar.
func (m *Model) renderSubTabs(width int) string {
	activeStyle := lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true)
	inactiveStyle := lipgloss.NewStyle().Foreground(theme.ColorDusty)

	settingsLabel := " Settings "
	pfLabel := " Port Forwarding "

	var left, right string
	if m.section == sectionSettings {
		left = activeStyle.Render(theme.TabActive+theme.TabActive) + " " + activeStyle.Render(settingsLabel)
		right = inactiveStyle.Render(pfLabel) + " " + inactiveStyle.Render(theme.TabInactive+theme.TabInactive)
	} else {
		left = inactiveStyle.Render(theme.TabInactive+theme.TabInactive) + " " + inactiveStyle.Render(settingsLabel)
		right = activeStyle.Render(pfLabel) + " " + activeStyle.Render(theme.TabActive+theme.TabActive)
	}

	gap := width - lipgloss.Width(left) - lipgloss.Width(right) - 4
	if gap < 1 {
		gap = 1
	}
	hint := lipgloss.NewStyle().Foreground(theme.ColorFaded).Render("[Tab] switch")
	padding := gap - lipgloss.Width(hint)
	if padding < 1 {
		padding = 1
	}

	return " " + left + strings.Repeat(" ", padding) + hint + strings.Repeat(" ", 1) + right
}

// renderSettings renders the settings section content.
func (m *Model) renderSettings(width, height int) string {
	var b strings.Builder

	// Section title.
	titleStyle := lipgloss.NewStyle().Foreground(theme.ColorLinen).Bold(true)
	subtitleStyle := lipgloss.NewStyle().Foreground(theme.ColorDusty).Italic(true)
	b.WriteString(" " + titleStyle.Render("Runtime Settings"))
	// Right-align subtitle.
	subtitle := subtitleStyle.Render("Changes take effect immediately")
	gap := width - lipgloss.Width(" Runtime Settings") - lipgloss.Width(subtitle) - 2
	if gap < 1 {
		gap = 1
	}
	b.WriteString(strings.Repeat(" ", gap) + subtitle + "\n")
	b.WriteString("\n")

	// Column header using table renderer.
	settingsTbl := tableutil.NewTable("SETTING", "VALUE")
	settingsTbl.SetHeaderStyle(theme.ColorDusty, true)
	settingsTbl.SetMinWidth(0, 36)
	settingsTbl.SetMinWidth(1, 20)
	b.WriteString(" " + settingsTbl.RenderHeader() + "\n")
	b.WriteString(theme.DividerStyle.Render(" " + strings.Repeat(theme.BorderH, width-2)) + "\n")
	b.WriteString("\n")

	// Setting rows.
	for i, def := range settingDefs {
		selected := i == m.selected

		label := lipgloss.NewStyle().Foreground(theme.ColorParchment).Width(38)
		if selected {
			label = label.Bold(true)
		}

		bracketColor := theme.ColorOakLight
		if selected {
			bracketColor = theme.ColorAmber
		}
		valueStr := lipgloss.NewStyle().Foreground(bracketColor).Render("[") +
			lipgloss.NewStyle().Foreground(theme.ColorAmber).Render(fmt.Sprintf("%4d", m.values[i])) +
			lipgloss.NewStyle().Foreground(bracketColor).Render("]") +
			" " + lipgloss.NewStyle().Foreground(theme.ColorDusty).Render(def.Unit)

		prefix := "  "
		if selected {
			prefix = theme.SelectionArrowStyle.Render(theme.IconArrowRight) + " "
		}

		b.WriteString(prefix + label.Render(def.Label) + valueStr + "\n")
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

	// Show confirmation note when a value has been changed.
	if m.dirty {
		b.WriteString("\n")
		confirmStyle := lipgloss.NewStyle().Foreground(theme.ColorProof).Italic(true)
		b.WriteString(" " + confirmStyle.Render(theme.IconCheck+" Settings applied. Changes take effect immediately."))
	}

	result := b.String()

	// Overlay edit modal if active.
	if m.editing {
		result = m.overlayEditModal(result)
	}

	return result
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

// ----- Port forwarding view -----

func (m *Model) renderPortForward(width, height int) string {
	if m.pfEditMode == pfAdding || m.pfEditMode == pfEditing {
		bg := m.renderPFList(width, height)
		modal := m.renderPFEditModal(width, height)
		dimmed := components.DimContent(bg)
		return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top, dimmed) +
			"\r" + modal
	}
	if m.pfEditMode == pfDeleting {
		bg := m.renderPFList(width, height)
		modal := m.renderPFDeleteModal(width, height)
		dimmed := components.DimContent(bg)
		return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top, dimmed) +
			"\r" + modal
	}
	return m.renderPFList(width, height)
}

func (m *Model) renderPFList(width, height int) string {
	if len(m.pfRules) == 0 {
		return m.renderPFEmpty(width, height)
	}

	m.pfList.Width = width - 2
	listHeight := height - 8
	if listHeight < 1 {
		listHeight = 1
	}
	m.pfList.Height = listHeight

	// Build a table to compute column widths.
	tbl := tableutil.NewTable("CONTAINER PORT", "HOST PORT", "DESCRIPTION")
	tbl.SetHeaderStyle(theme.ColorDusty, true)
	for _, r := range m.pfRules {
		cpStr := strconv.Itoa(r.ContainerPort)
		hpStr := strconv.Itoa(r.HostPort)
		if r.IsRange {
			cpStr = fmt.Sprintf("%d-%d", r.ContainerPort, r.RangeEnd)
			hostEnd := r.HostPort + (r.RangeEnd - r.ContainerPort)
			hpStr = fmt.Sprintf("%d-%d", r.HostPort, hostEnd)
		}
		cp := lipgloss.NewStyle().Foreground(theme.ColorParchment).Render(cpStr)
		hp := lipgloss.NewStyle().Foreground(theme.ColorVerdigris).Render(hpStr)
		desc := lipgloss.NewStyle().Foreground(theme.ColorDusty).Render(r.Description)
		tbl.AddRow(cp, hp, desc)
	}

	var b strings.Builder

	// Section title.
	titleStyle := lipgloss.NewStyle().Foreground(theme.ColorLinen).Bold(true)
	subtitleStyle := lipgloss.NewStyle().Foreground(theme.ColorDusty).Italic(true)
	b.WriteString(" " + titleStyle.Render("Port Forwarding Rules"))
	subtitle := subtitleStyle.Render("Live-reload on save")
	gap := width - lipgloss.Width(" Port Forwarding Rules") - lipgloss.Width(subtitle) - 2
	if gap < 1 {
		gap = 1
	}
	b.WriteString(strings.Repeat(" ", gap) + subtitle + "\n\n")

	// Column header.
	b.WriteString(" " + tbl.RenderHeader() + "\n")
	b.WriteString(theme.DividerStyle.Render(" "+strings.Repeat(theme.BorderH, width-2)) + "\n")

	// Compute column widths for row rendering.
	colWidths, _ := tbl.RenderRows(0)

	// Rule list.
	listView := m.pfList.View(func(item components.ListItem, selected bool, w int) string {
		r, ok := item.Data.(config.PortForwardRule)
		if !ok {
			return ""
		}
		return renderPFRow(r, selected, w, colWidths)
	})
	b.WriteString(listView)

	// Info box.
	b.WriteString("\n\n")
	b.WriteString(renderPFInfoBox(width - 2))

	// Show status feedback.
	switch m.pfStatus {
	case pfPending:
		b.WriteString("\n")
		pendingStyle := lipgloss.NewStyle().Foreground(theme.ColorAmber).Italic(true)
		b.WriteString(" " + pendingStyle.Render("Reloading..."))
	case pfApplied:
		b.WriteString("\n")
		successStyle := lipgloss.NewStyle().Foreground(theme.ColorProof).Italic(true)
		b.WriteString(" " + successStyle.Render(theme.IconCheck+" Rules applied successfully."))
	case pfFailed:
		b.WriteString("\n")
		failStyle := lipgloss.NewStyle().Foreground(theme.ColorFlame).Italic(true)
		msg := "Reload failed."
		if m.pfStatusMsg != "" {
			msg = "Reload failed: " + m.pfStatusMsg
		}
		b.WriteString(" " + failStyle.Render(theme.IconCross+" "+msg))
	}

	return b.String()
}

func renderPFRow(r config.PortForwardRule, selected bool, width int, colWidths []int) string {
	cpW, hpW, descW := 16, 12, 30
	if len(colWidths) >= 3 {
		cpW = colWidths[0]
		hpW = colWidths[1]
		descW = colWidths[2]
	}

	cpStr := strconv.Itoa(r.ContainerPort)
	hpStr := strconv.Itoa(r.HostPort)
	if r.IsRange {
		cpStr = fmt.Sprintf("%d-%d", r.ContainerPort, r.RangeEnd)
		hostEnd := r.HostPort + (r.RangeEnd - r.ContainerPort)
		hpStr = fmt.Sprintf("%d-%d", r.HostPort, hostEnd)
	}

	cp := lipgloss.NewStyle().Foreground(theme.ColorParchment).Width(cpW).Render(cpStr)
	hp := lipgloss.NewStyle().Foreground(theme.ColorVerdigris).Width(hpW).Render(hpStr)

	descText := r.Description
	if r.IsRange {
		descText += " [range, read-only]"
	}
	desc := lipgloss.NewStyle().Foreground(theme.ColorDusty).Width(descW).Render(truncate(descText, descW-2))

	row := cp + "  " + hp + "  " + desc

	if selected {
		arrow := theme.SelectionArrowStyle.Render(theme.IconArrowRight)
		return arrow + " " + row
	}
	return "  " + row
}

func renderPFInfoBox(width int) string {
	infoStyle := theme.InfoTextStyle
	emphStyle := theme.InfoEmphasisStyle

	lines := []string{
		infoStyle.Render("  Port forwarding relays traffic from the CLI container to the host machine"),
		infoStyle.Render("  via the proxy container's socat relay. Changes are ") + emphStyle.Render("live-reloaded") + infoStyle.Render("."),
		infoStyle.Render("  Container port: port inside the barrel. Host port: port on your machine."),
		infoStyle.Render("  Host services must bind to ") + emphStyle.Render("0.0.0.0") + infoStyle.Render(" (not 127.0.0.1) to be reachable from containers."),
		infoStyle.Render("  Range rules are read-only here. Use ") + emphStyle.Render("cooper configure") + infoStyle.Render(" to edit them."),
	}

	box := theme.InfoBoxStyle.Width(width).Render(strings.Join(lines, "\n"))
	return box
}

func (m *Model) renderPFEmpty(width, height int) string {
	content := lipgloss.JoinVertical(lipgloss.Center,
		"",
		"",
		theme.EmptyStateStyle.Render(theme.IconPlug),
		"",
		theme.EmptyStateStyle.Render("No port forwarding rules configured."),
		"",
		theme.EmptyStateStyle.Render("Press  n  to add a port forward."),
		theme.EmptyStateStyle.Render("Forwards traffic from the CLI container"),
		theme.EmptyStateStyle.Render("to a port on your host machine."),
	)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

// renderPFEditModal renders the add/edit port forward modal.
func (m *Model) renderPFEditModal(width, height int) string {
	var titleText string
	if m.pfEditMode == pfAdding {
		titleText = theme.IconPlug + " Add Port Forward"
	} else {
		titleText = theme.IconPlug + " Edit Port Forward"
	}

	title := theme.ModalTitleStyle.Render(titleText)
	divider := theme.ModalDividerStyle.Render(strings.Repeat(theme.BorderH, 38))

	// Field styles.
	contStyle := theme.InputInactiveStyle
	hostStyle := theme.InputInactiveStyle
	descStyle := theme.InputInactiveStyle
	switch m.pfField {
	case pfFieldContainerPort:
		contStyle = theme.InputActiveStyle
	case pfFieldHostPort:
		hostStyle = theme.InputActiveStyle
	case pfFieldDesc:
		descStyle = theme.InputActiveStyle
	}

	contLabel := lipgloss.NewStyle().Foreground(theme.ColorLinen).Render("Container Port:")
	contValue := contStyle.Render("[") +
		theme.InputTextStyle.Render(padRight(m.pfContBuf, 20)) +
		contStyle.Render("]")

	hostLabel := lipgloss.NewStyle().Foreground(theme.ColorLinen).Render("Host Port:")
	hostValue := hostStyle.Render("[") +
		theme.InputTextStyle.Render(padRight(m.pfHostBuf, 20)) +
		hostStyle.Render("]")

	descLabel := lipgloss.NewStyle().Foreground(theme.ColorLinen).Render("Description (optional):")
	descValue := descStyle.Render("[") +
		theme.InputTextStyle.Render(padRight(m.pfDescBuf, 20)) +
		descStyle.Render("]")

	var errLine string
	if m.pfErr != "" {
		errLine = lipgloss.NewStyle().Foreground(theme.ColorFlame).Render(m.pfErr)
	}

	confirm := theme.ModalConfirmStyle.Render("[Enter " + theme.IconCheck + " Save]")
	cancel := theme.ModalCancelStyle.Render("[Esc Cancel]")
	buttons := lipgloss.NewStyle().Width(44).Align(lipgloss.Center).Render(confirm + "    " + cancel)

	parts := []string{
		"",
		title,
		"",
		divider,
		"",
		contLabel,
		contValue,
		"",
		hostLabel,
		hostValue,
		"",
		descLabel,
		descValue,
	}
	if errLine != "" {
		parts = append(parts, "", errLine)
	}
	parts = append(parts, "", divider, "", buttons, "")

	inner := lipgloss.JoinVertical(lipgloss.Center, parts...)
	box := theme.ModalBorderStyle.Render(inner)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// renderPFDeleteModal renders the delete confirmation modal.
func (m *Model) renderPFDeleteModal(width, height int) string {
	idx := m.pfList.SelectedIdx
	var desc string
	if idx >= 0 && idx < len(m.pfRules) {
		r := m.pfRules[idx]
		desc = fmt.Sprintf("Port %d -> %d", r.ContainerPort, r.HostPort)
		if r.Description != "" {
			desc += " (" + r.Description + ")"
		}
	}

	title := theme.ModalTitleStyle.Render(theme.IconPlug + " Delete Port Forward?")
	divider := theme.ModalDividerStyle.Render(strings.Repeat(theme.BorderH, 38))
	body := theme.ModalBodyStyle.Render(desc)

	confirm := theme.ModalConfirmStyle.Render("[Enter " + theme.IconCheck + " Delete]")
	cancel := theme.ModalCancelStyle.Render("[Esc Cancel]")
	buttons := lipgloss.NewStyle().Width(44).Align(lipgloss.Center).Render(confirm + "    " + cancel)

	inner := lipgloss.JoinVertical(lipgloss.Center,
		"",
		title,
		"",
		divider,
		"",
		body,
		"",
		divider,
		"",
		buttons,
		"",
	)

	box := theme.ModalBorderStyle.Render(inner)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
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

func truncate(s string, maxLen int) string {
	if maxLen < 0 {
		maxLen = 0
	}
	if len(s) <= maxLen {
		return s
	}
	if maxLen < 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func padRight(s string, length int) string {
	if len(s) >= length {
		return s[:length]
	}
	return s + strings.Repeat(" ", length-len(s))
}
