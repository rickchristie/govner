package configure

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// portModalResult is returned by portModal.update.
type portModalResult int

const (
	portModalNone      portModalResult = iota
	portModalSaved                     // user confirmed — savedRule is populated
	portModalCancelled                 // user cancelled
)

// portModal manages the add/edit port forwarding modal.
type portModal struct {
	active             bool
	editing            bool
	editIndex          int
	containerPortInput textInput
	hostPortInput      textInput
	descInput          textInput
	isRange            bool
	focusField         int // 0=container, 1=host, 2=desc, 3=range toggle, 4=confirm, 5=cancel
	err                string
	savedRule          config.PortForwardRule // populated on portModalSaved
}

func (m *portModal) open(editing bool, index int, rule config.PortForwardRule) {
	m.active = true
	m.editing = editing
	m.editIndex = index
	m.err = ""
	m.focusField = 0
	m.containerPortInput.Focus()
	m.hostPortInput.Blur()
	m.descInput.Blur()

	if editing {
		if rule.IsRange {
			m.containerPortInput.SetValue(fmt.Sprintf("%d-%d", rule.ContainerPort, rule.RangeEnd))
			hostEnd := rule.HostPort + (rule.RangeEnd - rule.ContainerPort)
			m.hostPortInput.SetValue(fmt.Sprintf("%d-%d", rule.HostPort, hostEnd))
		} else {
			m.containerPortInput.SetValue(fmt.Sprintf("%d", rule.ContainerPort))
			m.hostPortInput.SetValue(fmt.Sprintf("%d", rule.HostPort))
		}
		m.descInput.SetValue(rule.Description)
		m.isRange = rule.IsRange
	} else {
		m.containerPortInput.SetValue("")
		m.hostPortInput.SetValue("")
		m.descInput.SetValue("")
		m.isRange = false
	}
}

func (pm *portModal) close() {
	pm.active = false
	pm.containerPortInput.Blur()
	pm.hostPortInput.Blur()
	pm.descInput.Blur()
}

func (pm *portModal) update(msg tea.Msg) portModalResult {
	const portModalFields = 6 // 0=container, 1=host, 2=desc, 3=range toggle, 4=confirm, 5=cancel

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			pm.close()
			return portModalCancelled

		case "down":
			pm.focusField = (pm.focusField + 1) % portModalFields
			pm.containerPortInput.Blur()
			pm.hostPortInput.Blur()
			pm.descInput.Blur()
			switch pm.focusField {
			case 0:
				pm.containerPortInput.Focus()
			case 1:
				pm.hostPortInput.Focus()
			case 2:
				pm.descInput.Focus()
			}
			return portModalNone

		case "up":
			pm.focusField = (pm.focusField - 1 + portModalFields) % portModalFields
			pm.containerPortInput.Blur()
			pm.hostPortInput.Blur()
			pm.descInput.Blur()
			switch pm.focusField {
			case 0:
				pm.containerPortInput.Focus()
			case 1:
				pm.hostPortInput.Focus()
			case 2:
				pm.descInput.Focus()
			}
			return portModalNone

		case " ":
			if pm.focusField == 3 {
				pm.isRange = !pm.isRange
				return portModalNone
			}

		case "enter":
			// Cancel button.
			if pm.focusField == 5 {
				pm.close()
				return portModalCancelled
			}
			// Confirm button or any other field.
			rule, err := pm.parseRule()
			if err != "" {
				pm.err = err
				return portModalNone
			}
			pm.savedRule = rule
			pm.close()
			return portModalSaved
		}

		// Route keys to the focused input.
		switch pm.focusField {
		case 0:
			pm.containerPortInput.handleKeyMsg(msg)
		case 1:
			pm.hostPortInput.handleKeyMsg(msg)
		case 2:
			pm.descInput.handleKeyMsg(msg)
		}
	}
	return portModalNone
}

// parseRule validates and parses the modal inputs into a PortForwardRule.
func (pm *portModal) parseRule() (config.PortForwardRule, string) {
	var rule config.PortForwardRule
	rule.Description = pm.descInput.Value()
	rule.IsRange = pm.isRange

	containerVal := strings.TrimSpace(pm.containerPortInput.Value())
	hostVal := strings.TrimSpace(pm.hostPortInput.Value())

	if containerVal == "" {
		return rule, "CLI port is required"
	}
	if hostVal == "" {
		return rule, "Host port is required"
	}

	if pm.isRange {
		cStart, cEnd, err := parsePortRange(containerVal)
		if err != "" {
			return rule, "CLI port: " + err
		}
		hStart, _, herr := parsePortRange(hostVal)
		if herr != "" {
			return rule, "Host port: " + herr
		}
		rule.ContainerPort = cStart
		rule.RangeEnd = cEnd
		rule.HostPort = hStart
	} else {
		cp, err := strconv.Atoi(containerVal)
		if err != nil {
			return rule, "CLI port must be a number"
		}
		hp, errh := strconv.Atoi(hostVal)
		if errh != nil {
			return rule, "Host port must be a number"
		}
		if cp < 1 || cp > 65535 {
			return rule, "CLI port must be 1-65535"
		}
		if hp < 1 || hp > 65535 {
			return rule, "Host port must be 1-65535"
		}
		rule.ContainerPort = cp
		rule.HostPort = hp
	}

	return rule, ""
}

// parsePortRange parses "8000-8100" into (8000, 8100). Returns start, end, error.
func parsePortRange(s string) (int, int, string) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return 0, 0, "expected range format like 8000-8100"
	}
	start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, "invalid start port"
	}
	end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, "invalid end port"
	}
	if start < 1 || start > 65535 || end < 1 || end > 65535 {
		return 0, 0, "ports must be 1-65535"
	}
	if end <= start {
		return 0, 0, "end must be greater than start"
	}
	return start, end, ""
}

func (pm *portModal) view(width int) string {
	title := "Add Port Forward"
	if pm.editing {
		title = "Edit Port Forward"
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(theme.ColorAmber).
		Padding(1, 3).
		Width(min(50, width-10))

	titleStyle := lipgloss.NewStyle().Foreground(theme.ColorParchment).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(theme.ColorLinen)

	var inner string
	inner += "  " + theme.IconPlug + " " + titleStyle.Render(title) + "\n\n"
	inner += "  " + lipgloss.NewStyle().Foreground(theme.ColorOakLight).Render(repeatStr(theme.BorderH, 38)) + "\n\n"

	inner += "  " + labelStyle.Render("CLI Port:") + "\n"
	inner += pm.containerPortInput.viewWithMargin(2) + "\n\n"

	inner += "  " + labelStyle.Render("Host Port:") + "\n"
	inner += pm.hostPortInput.viewWithMargin(2) + "\n\n"

	inner += "  " + labelStyle.Render("Description (optional):") + "\n"
	inner += pm.descInput.viewWithMargin(2) + "\n\n"

	toggle := lipgloss.NewStyle().Foreground(theme.ColorDusty).Render("[" + theme.IconDotEmpty + "]")
	if pm.isRange {
		toggle = lipgloss.NewStyle().Foreground(theme.ColorAmber).Render("[" + theme.IconDot + "]")
	}
	prefix := "    "
	if pm.focusField == 3 {
		prefix = "  " + lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render(theme.IconArrowRight) + " "
	}
	inner += prefix + toggle + " " + labelStyle.Render("Range mode (e.g. 8000-8100)") + "\n\n"

	if pm.err != "" {
		inner += "  " + lipgloss.NewStyle().Foreground(theme.ColorFlame).Render(pm.err) + "\n\n"
	}

	inner += "  " + lipgloss.NewStyle().Foreground(theme.ColorOakLight).Render(repeatStr(theme.BorderH, 38)) + "\n\n"

	// Confirm button.
	if pm.focusField == 4 {
		inner += "  " + lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render(theme.IconArrowRight) + " " +
			lipgloss.NewStyle().Background(theme.ColorOakMid).Foreground(theme.ColorProof).Bold(true).Render("["+theme.IconCheck+" Save]")
	} else {
		inner += "    " + lipgloss.NewStyle().Foreground(theme.ColorProof).Bold(true).Render("["+theme.IconCheck+" Save]")
	}

	inner += "    "

	// Cancel button.
	if pm.focusField == 5 {
		inner += lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render(theme.IconArrowRight) + " " +
			lipgloss.NewStyle().Background(theme.ColorOakMid).Foreground(theme.ColorDusty).Render("[Cancel]")
	} else {
		inner += lipgloss.NewStyle().Foreground(theme.ColorDusty).Render("[Cancel]")
	}

	return boxStyle.Render(inner)
}
