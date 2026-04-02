// Package portfwd implements the Port Forwarding tab sub-model.
// It manages port forwarding rules with add/edit/delete modals
// and live-reload support.
package portfwd

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/tableutil"
	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// PortForwardChangedMsg is emitted when port forwarding rules are modified.
// The root model should write socat config and trigger a live reload.
type PortForwardChangedMsg struct {
	Rules []config.PortForwardRule
}

// PFApplyResultMsg is sent from the root model back to the port forward model
// to report whether a port forward change succeeded or failed.
type PFApplyResultMsg struct {
	OK     bool
	ErrMsg string
	Rules  []config.PortForwardRule // The rules that were applied (or attempted).
}

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

// pfField identifies which field is active during port forward editing.
type pfField int

const (
	pfFieldContainerPort pfField = iota
	pfFieldHostPort
	pfFieldDesc
	pfFieldRange // range mode toggle
)

// Model is the sub-model for the Port Forwarding tab.
type Model struct {
	// Port forwarding rules and list.
	pfRules    []config.PortForwardRule
	pfList     components.ScrollableList
	pfEditMode pfEditMode
	pfEditIdx  int // Index of rule being edited (-1 for new).
	pfField    pfField
	pfContBuf  string   // Container port input buffer.
	pfHostBuf  string   // Host port input buffer.
	pfDescBuf  string   // Description input buffer.
	pfIsRange  bool     // Range mode toggle.
	pfErr      string   // Validation error for port forward modal.
	pfStatus   pfStatus // Status of the last port forward change.
	pfStatusMsg string  // Error message when pfStatus == pfFailed.

	// Scrollable viewport for body content.
	viewport components.ScrollableContent

	// Layout.
	width  int
	height int
}

// New creates a new port forwarding model.
func New() *Model {
	return &Model{
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
		if m.pfEditMode == pfNone {
			m.viewport.HandleMouse(msg, m.pfBodyHeight())
		}
		return m, nil
	case tea.KeyMsg:
		return m.handlePFKey(msg)
	}
	return m, nil
}

// ----- Port forwarding key handlers -----

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
		prevIdx := m.pfList.SelectedIdx
		m.pfList.MoveUp()
		if m.pfList.SelectedIdx != prevIdx {
			m.viewport.EnsureLineVisible(m.pfList.SelectedIdx, m.pfBodyHeight())
		} else {
			// At top of list — scroll viewport up to reveal content above.
			m.viewport.HandleKey(msg, m.pfBodyHeight())
		}
	case "down", "j":
		prevIdx := m.pfList.SelectedIdx
		m.pfList.MoveDown()
		if m.pfList.SelectedIdx != prevIdx {
			m.viewport.EnsureLineVisible(m.pfList.SelectedIdx, m.pfBodyHeight())
		} else {
			// At bottom of list — scroll viewport down to reveal info box.
			m.viewport.HandleKey(msg, m.pfBodyHeight())
		}
	case "pgup", "pgdown":
		m.viewport.HandleKey(msg, m.pfBodyHeight())
	case "n":
		// Add new rule.
		m.pfEditMode = pfAdding
		m.pfEditIdx = -1
		m.pfField = pfFieldContainerPort
		m.pfContBuf = ""
		m.pfHostBuf = ""
		m.pfDescBuf = ""
		m.pfIsRange = false
		m.pfErr = ""
	case "enter", "e":
		// Edit selected rule.
		if sel := m.pfList.Selected(); sel != nil {
			if r, ok := sel.Data.(config.PortForwardRule); ok {
				m.pfEditMode = pfEditing
				m.pfEditIdx = m.pfList.SelectedIdx
				m.pfField = pfFieldContainerPort
				m.pfIsRange = r.IsRange
				if r.IsRange {
					m.pfContBuf = fmt.Sprintf("%d-%d", r.ContainerPort, r.RangeEnd)
					hostEnd := r.HostPort + (r.RangeEnd - r.ContainerPort)
					m.pfHostBuf = fmt.Sprintf("%d-%d", r.HostPort, hostEnd)
				} else {
					m.pfContBuf = strconv.Itoa(r.ContainerPort)
					m.pfHostBuf = strconv.Itoa(r.HostPort)
				}
				m.pfDescBuf = r.Description
				m.pfErr = ""
			}
		}
	case "x":
		// Delete selected rule.
		if len(m.pfRules) > 0 && m.pfList.Selected() != nil {
			if sel := m.pfList.Selected(); sel != nil {
				if _, ok := sel.Data.(config.PortForwardRule); !ok {
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
		if m.pfField < pfFieldRange {
			m.pfField++
		}
		return m, nil
	case "tab":
		m.pfField = (m.pfField + 1) % (pfFieldRange + 1)
		return m, nil
	case " ":
		// Toggle range mode when on the range field.
		if m.pfField == pfFieldRange {
			m.pfIsRange = !m.pfIsRange
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
				// Accept digits and '-' for range mode.
				if (r[0] >= '0' && r[0] <= '9') || r[0] == '-' {
					m.pfContBuf += r
				}
			case pfFieldHostPort:
				if (r[0] >= '0' && r[0] <= '9') || r[0] == '-' {
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
	var rule config.PortForwardRule
	rule.Description = m.pfDescBuf
	rule.IsRange = m.pfIsRange

	if m.pfIsRange {
		// Parse range format: "8000-9000"
		cParts := strings.SplitN(m.pfContBuf, "-", 2)
		hParts := strings.SplitN(m.pfHostBuf, "-", 2)
		if len(cParts) != 2 || len(hParts) < 1 {
			m.pfErr = "Range format: start-end (e.g. 8000-9000)"
			return m, nil
		}
		cStart, err1 := strconv.Atoi(strings.TrimSpace(cParts[0]))
		cEnd, err2 := strconv.Atoi(strings.TrimSpace(cParts[1]))
		hStart, err3 := strconv.Atoi(strings.TrimSpace(hParts[0]))
		if err1 != nil || err2 != nil || err3 != nil {
			m.pfErr = "Invalid port numbers"
			return m, nil
		}
		if cStart < 1 || cEnd < 1 || hStart < 1 || cStart > 65535 || cEnd > 65535 || hStart > 65535 {
			m.pfErr = "Ports must be 1-65535"
			return m, nil
		}
		if cEnd <= cStart {
			m.pfErr = "End port must be greater than start"
			return m, nil
		}
		rule.ContainerPort = cStart
		rule.RangeEnd = cEnd
		rule.HostPort = hStart
	} else {
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
		rule.ContainerPort = cp
		rule.HostPort = hp
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

// pfHeaderLines is the number of fixed header lines (title + blank + column header + separator).
const pfHeaderLines = 4

func (m *Model) pfBodyHeight() int {
	h := m.height - pfHeaderLines
	if h < 1 {
		h = 1
	}
	return h
}

func (m *Model) renderPFList(width, height int) string {
	bodyHeight := m.pfBodyHeight()

	// Build the title line (fixed header part 1).
	titleStyle := lipgloss.NewStyle().Foreground(theme.ColorLinen).Bold(true)
	subtitleStyle := lipgloss.NewStyle().Foreground(theme.ColorDusty).Italic(true)
	var titleLine strings.Builder
	titleLine.WriteString(" " + titleStyle.Render("Port Forwarding Rules"))
	subtitle := subtitleStyle.Render("Live-reload on save")
	gap := width - lipgloss.Width(" Port Forwarding Rules") - lipgloss.Width(subtitle) - 2
	if gap < 1 {
		gap = 1
	}
	titleLine.WriteString(strings.Repeat(" ", gap) + subtitle + "\n\n")

	if len(m.pfRules) == 0 {
		// Render header with empty table columns.
		tbl := tableutil.NewTable("CONTAINER PORT", "HOST PORT", "DESCRIPTION")
		tbl.SetHeaderStyle(theme.ColorDusty, true)
		header := titleLine.String() +
			" " + tbl.RenderHeader() + "\n" +
			theme.DividerStyle.Render(" "+strings.Repeat(theme.BorderH, width-2)) + "\n"
		return header + m.renderPFEmpty(width, bodyHeight)
	}

	// Build a single table with header + all data rows for consistent column widths.
	tbl := tableutil.NewTable("CONTAINER PORT", "HOST PORT", "DESCRIPTION")
	tbl.SetHeaderStyle(theme.ColorDusty, true)
	tbl.SetMinWidth(2, 30) // Give DESCRIPTION reasonable width.
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

	// Fixed header: title + column header from the SAME table (aligned widths).
	header := titleLine.String() +
		" " + tbl.RenderHeader() + "\n" +
		theme.DividerStyle.Render(" "+strings.Repeat(theme.BorderH, width-2)) + "\n"

	// Get column widths and rendered rows from the table.
	_, renderedRows := tbl.RenderRows(0)

	// Render body: data rows with selection indicator + info box + status.
	var body strings.Builder

	selected := m.pfList.SelectedIdx
	for i, row := range renderedRows {
		prefix := "  "
		if i == selected {
			prefix = lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render(theme.IconArrowRight) + " "
		}
		line := prefix + row
		if i == selected {
			line = lipgloss.NewStyle().Background(theme.ColorOakMid).Render(line)
		}
		body.WriteString(line + "\n")
	}

	// Info box.
	body.WriteString("\n")
	body.WriteString(renderPFInfoBox(width - 2))

	// Status feedback.
	switch m.pfStatus {
	case pfPending:
		body.WriteString("\n")
		body.WriteString(" " + lipgloss.NewStyle().Foreground(theme.ColorAmber).Italic(true).Render("Reloading..."))
	case pfApplied:
		body.WriteString("\n")
		body.WriteString(" " + lipgloss.NewStyle().Foreground(theme.ColorProof).Italic(true).Render(theme.IconCheck+" Rules applied successfully."))
	case pfFailed:
		body.WriteString("\n")
		msg := "Reload failed."
		if m.pfStatusMsg != "" {
			msg = "Reload failed: " + m.pfStatusMsg
		}
		body.WriteString(" " + lipgloss.NewStyle().Foreground(theme.ColorFlame).Italic(true).Render(theme.IconCross+" "+msg))
	}

	// Use ScrollableContent viewport for the body.
	m.viewport.SetContent(body.String())
	return header + m.viewport.View(width, bodyHeight)
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

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(theme.ColorAmber).
		Padding(1, 3).
		Width(50)

	titleStyle := lipgloss.NewStyle().Foreground(theme.ColorParchment).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(theme.ColorLinen)
	hintStyle := lipgloss.NewStyle().Foreground(theme.ColorDusty)
	cursor := lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render("_")

	// Bordered input box — active field gets amber border, inactive gets dim.
	makeInput := func(value string, active bool) string {
		borderColor := theme.ColorOakLight
		if active {
			borderColor = theme.ColorAmber
		}
		display := value
		if active {
			display += cursor
		}
		return lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(borderColor).
			Foreground(theme.ColorParchment).
			Width(24).
			Render(display)
	}

	var inner string
	inner += titleStyle.Render(titleText) + "\n\n"

	inner += labelStyle.Render("Container Port:") + "\n"
	inner += makeInput(m.pfContBuf, m.pfField == pfFieldContainerPort) + "\n\n"

	inner += labelStyle.Render("Host Port:") + "\n"
	inner += makeInput(m.pfHostBuf, m.pfField == pfFieldHostPort) + "\n\n"

	inner += labelStyle.Render("Description (optional):") + "\n"
	inner += makeInput(m.pfDescBuf, m.pfField == pfFieldDesc) + "\n\n"

	// Range mode toggle.
	toggleIcon := lipgloss.NewStyle().Foreground(theme.ColorDusty).Render("[ ]")
	if m.pfIsRange {
		toggleIcon = lipgloss.NewStyle().Foreground(theme.ColorAmber).Render("[" + theme.IconDot + "]")
	}
	rangeColor := theme.ColorDusty
	if m.pfField == pfFieldRange {
		rangeColor = theme.ColorAmber
	}
	inner += lipgloss.NewStyle().Foreground(rangeColor).Render(toggleIcon+" Range mode (e.g. 8000-9000)") + "\n"

	if m.pfErr != "" {
		inner += "\n" + lipgloss.NewStyle().Foreground(theme.ColorFlame).Render(m.pfErr) + "\n"
	}

	inner += "\n" + hintStyle.Render("Tab/Up/Down: switch fields, Space: toggle range") + "\n\n"
	inner += lipgloss.NewStyle().Foreground(theme.ColorProof).Bold(true).Render("[Enter "+theme.IconCheck+" Save]") +
		"    " + lipgloss.NewStyle().Foreground(theme.ColorDusty).Render("[Esc Cancel]")

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, boxStyle.Render(inner))
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


