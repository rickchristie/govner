package configure

import (
	"fmt"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// proxyResult is returned by the proxy screen update.
type proxyResult int

const (
	proxyNone proxyResult = iota
	proxyBack
)

// proxyModel manages the Proxy Settings screen.
type proxyModel struct {
	proxyPort   int
	bridgePort  int
	shmSize     string
	proxyInput  textInput
	bridgeInput textInput
	shmInput    textInput
	focusField  int // 0 = proxy, 1 = bridge, 2 = shm size
	err         string

	// Scroll state for layout.
	scrollOffset  int
	lastHeight    int // cached terminal height for scroll calculations in Update
	lastMaxScroll int // cached max scroll offset from last render
}

func newProxyModel(proxyPort, bridgePort int, shmSize string) proxyModel {
	pi := newTextInput("3128", 20)
	pi.SetValue(fmt.Sprintf("%d", proxyPort))
	pi.Focus()

	bi := newTextInput("4343", 20)
	bi.SetValue(fmt.Sprintf("%d", bridgePort))

	si := newTextInput("1g", 20)
	si.SetValue(shmSize)

	return proxyModel{
		proxyPort:   proxyPort,
		bridgePort:  bridgePort,
		shmSize:     shmSize,
		proxyInput:  pi,
		bridgeInput: bi,
		shmInput:    si,
	}
}

func (m *proxyModel) update(msg tea.Msg) proxyResult {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		handleMouseScroll(msg, &m.scrollOffset, m.lastMaxScroll)
		return proxyNone
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return proxyBack
		case "down":
			if m.focusField < 2 {
				m.focusField++
				m.proxyInput.Blur()
				m.bridgeInput.Blur()
				m.shmInput.Blur()
				switch m.focusField {
				case 1:
					m.bridgeInput.Focus()
				case 2:
					m.shmInput.Focus()
				}
			} else {
				// At last field — scroll content down.
				m.scrollOffset++
				// Will be clamped by Render.
			}
			return proxyNone
		case "up":
			if m.focusField > 0 {
				m.focusField--
				m.proxyInput.Blur()
				m.bridgeInput.Blur()
				m.shmInput.Blur()
				switch m.focusField {
				case 0:
					m.proxyInput.Focus()
				case 1:
					m.bridgeInput.Focus()
				}
			} else if m.scrollOffset > 0 {
				m.scrollOffset--
			}
			return proxyNone
		case "enter":
			// Validate and save.
			pp, err := strconv.Atoi(m.proxyInput.Value())
			if err != nil || pp < 1 || pp > 65535 {
				m.err = "Proxy port must be a valid number (1-65535)"
				return proxyNone
			}
			bp, err := strconv.Atoi(m.bridgeInput.Value())
			if err != nil || bp < 1 || bp > 65535 {
				m.err = "Bridge port must be a valid number (1-65535)"
				return proxyNone
			}
			if pp == bp {
				m.err = fmt.Sprintf("Proxy port (%d) and bridge port (%d) must be different", pp, bp)
				return proxyNone
			}
			shmVal := m.shmInput.Value()
			if !config.SHMSizeValid(shmVal) {
				m.err = "SHM size must be a number with optional k/m/g suffix (e.g. 64m, 1g)"
				return proxyNone
			}
			m.proxyPort = pp
			m.bridgePort = bp
			m.shmSize = shmVal
			m.err = ""
			return proxyBack
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
		default:
			// Route to focused input.
			switch m.focusField {
			case 0:
				m.proxyInput.handleKeyMsg(msg)
			case 1:
				m.bridgeInput.handleKeyMsg(msg)
			case 2:
				m.shmInput.handleKeyMsg(msg)
			}
		}
	}
	return proxyNone
}

func (m *proxyModel) view(width, height int) string {
	m.lastHeight = height
	breadcrumb := breadcrumbStyle().Render(theme.BarrelEmoji+" Configure > ") +
		lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render("Proxy Settings")

	header := breadcrumb

	description := lipgloss.NewStyle().Foreground(theme.ColorDusty).Render(
		" Configure the ports used by Cooper's proxy and execution bridge.")

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(theme.ColorOakLight).
		Padding(1, 2).
		Width(min(70, width-4))

	labelBold := lipgloss.NewStyle().Foreground(theme.ColorLinen).Bold(true)
	hint := lipgloss.NewStyle().Foreground(theme.ColorDusty)

	var inner string

	// Proxy port field.
	proxyLabel := "    " + labelBold.Render("Squid Proxy Port:")
	if m.focusField == 0 {
		proxyLabel = "  " + lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render(theme.IconArrowRight) + " " + labelBold.Render("Squid Proxy Port:")
	}
	inner += proxyLabel + "\n"
	inner += m.proxyInput.viewWithMargin(4) + "\n"
	inner += "    " + hint.Render("Standard Squid port. Must not conflict with host services.") + "\n\n"

	// Bridge port field.
	bridgeLabel := "    " + labelBold.Render("Execution Bridge Port:")
	if m.focusField == 1 {
		bridgeLabel = "  " + lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render(theme.IconArrowRight) + " " + labelBold.Render("Execution Bridge Port:")
	}
	inner += bridgeLabel + "\n"
	inner += m.bridgeInput.viewWithMargin(4) + "\n"
	inner += "    " + hint.Render("HTTP API for AI-to-host script execution.") + "\n\n"

	// SHM size field.
	shmLabel := "    " + labelBold.Render("Barrel Shared Memory Size:")
	if m.focusField == 2 {
		shmLabel = "  " + lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render(theme.IconArrowRight) + " " + labelBold.Render("Barrel Shared Memory Size:")
	}
	inner += shmLabel + "\n"
	inner += m.shmInput.viewWithMargin(4) + "\n"
	inner += "    " + hint.Render("Size of /dev/shm for browser/Playwright workloads (e.g. 64m, 256m, 1g, 2g).") + "\n"

	if m.err != "" {
		inner += "\n  " + lipgloss.NewStyle().Foreground(theme.ColorFlame).Render(m.err)
	}

	var content string
	content += "\n" + description + "\n\n"
	content += boxStyle.Render(inner) + "\n\n"

	content += infoBox(" The execution bridge gives AI CLI tools a way to trigger host\n"+
		" scripts without direct machine access. For example:\n"+
		"   /deploy-staging  "+theme.IconArrowRight+"  ~/scripts/deploy-staging.sh\n"+
		"   /go-mod-tidy     "+theme.IconArrowRight+"  ~/scripts/go-mod-tidy.sh\n\n"+
		" Scripts should take NO input. The stdout/stderr is returned to the AI.\n"+
		" Configure routes in cooper up > Routes tab.", width)

	footer := " " + helpBar("["+theme.IconArrowUp+theme.IconArrowDown+" Nav]", "[Enter Save]", "[Esc Back]")

	ly := newLayout(header, content, footer, width, height)
	ly.scrollOffset = m.scrollOffset
	result := ly.Render()
	m.scrollOffset = ly.scrollOffset // Save AFTER Render, which clamps.
	m.lastMaxScroll = ly.MaxScrollOffset()
	return result
}
