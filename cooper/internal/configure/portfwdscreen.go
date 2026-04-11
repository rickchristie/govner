package configure

import (
	"fmt"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/tableutil"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

func portForwardInfoText(goos string) string {
	if goos == "darwin" {
		return " " + theme.IconWarn + " On macOS with Docker Desktop, services on any bind address\n" +
			" (including 127.0.0.1) are reachable from barrels via\n" +
			" host.docker.internal. No HostRelay is needed.\n\n" +
			" Forwarding uses a two-hop relay:\n" +
			" CLI container " + theme.IconArrowRight + " cooper-proxy " + theme.IconArrowRight + " host machine"
	}

	return " " + theme.IconWarn + " Host services must bind to 0.0.0.0 or the Docker gateway IP to\n" +
		" be reachable from containers. Services bound to 127.0.0.1 only are\n" +
		" handled by Cooper's HostRelay when needed.\n\n" +
		" Forwarding uses a two-hop relay:\n" +
		" CLI container " + theme.IconArrowRight + " cooper-proxy " + theme.IconArrowRight + " host machine"
}

// portFwdResult is returned by the port forwarding screen update.
type portFwdResult int

const (
	portFwdNone portFwdResult = iota
	portFwdBack
)

// portFwdModel manages the Port Forwarding to Host screen.
type portFwdModel struct {
	portRules  []config.PortForwardRule
	portCursor int
	portModal  portModal

	// Scroll state for layout.
	scrollOffset  int
	lastHeight    int
	lastMaxScroll int
}

func newPortFwdModel(rules []config.PortForwardRule) portFwdModel {
	return portFwdModel{
		portRules: append([]config.PortForwardRule{}, rules...),
		portModal: portModal{
			containerPortInput: newTextInput("e.g., 5432", 20),
			hostPortInput:      newTextInput("e.g., 5432", 20),
			descInput:          newTextInput("e.g., PostgreSQL", 30),
		},
	}
}

func (m *portFwdModel) update(msg tea.Msg) portFwdResult {
	if m.portModal.active {
		return m.updateModal(msg)
	}

	switch msg := msg.(type) {
	case tea.MouseMsg:
		handleMouseScroll(msg, &m.scrollOffset, m.lastMaxScroll)
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if len(m.portRules) > 0 && m.portCursor > 0 {
				m.portCursor--
				ensureLineVisible(&m.scrollOffset, 4+m.portCursor, m.lastHeight, 5, 1)
			} else if m.scrollOffset > 0 {
				m.scrollOffset--
			}
		case "down", "j":
			if len(m.portRules) > 0 && m.portCursor < len(m.portRules)-1 {
				m.portCursor++
				ensureLineVisible(&m.scrollOffset, 4+m.portCursor, m.lastHeight, 5, 1)
			} else if m.scrollOffset < m.lastMaxScroll {
				m.scrollOffset++
			}
		case "n":
			m.portModal.open(false, -1, config.PortForwardRule{})
		case "e", "enter":
			if len(m.portRules) > 0 {
				m.portModal.open(true, m.portCursor, m.portRules[m.portCursor])
			}
		case "x":
			if len(m.portRules) > 0 && m.portCursor < len(m.portRules) {
				m.portRules = append(m.portRules[:m.portCursor], m.portRules[m.portCursor+1:]...)
				if m.portCursor >= len(m.portRules) && m.portCursor > 0 {
					m.portCursor--
				}
			}
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
			return portFwdBack
		}
	}
	return portFwdNone
}

func (m *portFwdModel) updateModal(msg tea.Msg) portFwdResult {
	result := m.portModal.update(msg)
	switch result {
	case portModalSaved:
		rule := m.portModal.savedRule
		if m.portModal.editing {
			m.portRules[m.portModal.editIndex] = rule
		} else {
			m.portRules = append(m.portRules, rule)
		}
	case portModalCancelled:
		// nothing
	}
	return portFwdNone
}

func (m *portFwdModel) view(width, height int) string {
	m.lastHeight = height
	breadcrumb := breadcrumbStyle().Render(theme.BarrelEmoji+" Configure > ") +
		lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render("Port Forwarding to Host")

	header := breadcrumb

	var content string
	content += lipgloss.NewStyle().Foreground(theme.ColorDusty).Render(
		" Port forwarding routes traffic from CLI container to host services.") + "\n\n"

	// Build table for port forwarding rules.
	tbl := tableutil.NewTable("CLI PORT", "HOST PORT", "DESCRIPTION")
	tbl.SetHeaderStyle(theme.ColorDusty, true)
	sepColor := theme.ColorOakLight
	tbl.SetSeparator(theme.BorderH, &sepColor)

	for _, r := range m.portRules {
		cliPort := fmt.Sprintf("%d", r.ContainerPort)
		hostPort := fmt.Sprintf("%d", r.HostPort)
		if r.IsRange {
			cliPort = fmt.Sprintf("%d-%d", r.ContainerPort, r.RangeEnd)
			hostPort = fmt.Sprintf("%d-%d", r.HostPort, r.HostPort+(r.RangeEnd-r.ContainerPort))
		}

		tbl.AddRow(
			lipgloss.NewStyle().Foreground(theme.ColorParchment).Render(cliPort),
			lipgloss.NewStyle().Foreground(theme.ColorParchment).Render(hostPort),
			lipgloss.NewStyle().Foreground(theme.ColorDusty).Render(r.Description))
	}

	rowIndent := "   "
	content += rowIndent + tbl.RenderHeader() + "\n"
	content += rowIndent + tbl.RenderSeparator(0) + "\n"

	if len(m.portRules) == 0 {
		content += rowIndent + lipgloss.NewStyle().Foreground(theme.ColorFaded).Italic(true).Render("(no rules configured)") + "\n"
	}

	_, rows := tbl.RenderRows(0)
	for i, row := range rows {
		prefix := rowIndent
		if i == m.portCursor {
			prefix = " " + lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render(theme.IconArrowRight) + " "
		}

		line := prefix + row
		if i == m.portCursor {
			line = lipgloss.NewStyle().Background(theme.ColorOakMid).Render(line)
		}
		content += line + "\n"
	}

	content += "\n"
	content += infoBox(portForwardInfoText(runtime.GOOS), width)

	footer := " " + helpBar("[n New]", "[e Edit]", "[x Delete]", "[Esc Back]")

	ly := newLayout(header, content, footer, width, height)
	ly.scrollOffset = m.scrollOffset
	if len(m.portRules) > 0 {
		cursorLine := 4 + m.portCursor
		ly.EnsureVisible(cursorLine)
	}
	s := ly.Render()
	m.scrollOffset = ly.scrollOffset
	m.lastMaxScroll = ly.MaxScrollOffset()

	if m.portModal.active {
		s = overlayModal(s, m.portModal.view(width), width, height)
	}

	return s
}

func (m *portFwdModel) toPortForwardRules() []config.PortForwardRule {
	result := make([]config.PortForwardRule, len(m.portRules))
	copy(result, m.portRules)
	return result
}
