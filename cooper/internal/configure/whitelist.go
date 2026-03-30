package configure

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/tableutil"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// whitelistResult is returned by whitelist update.
type whitelistResult int

const (
	whitelistNone whitelistResult = iota
	whitelistBack
)

// whitelistSubTab identifies the active sub-tab.
type whitelistSubTab int

const (
	subTabDomains whitelistSubTab = iota
	subTabPorts
)

// domainModal tracks the add/edit domain modal state.
type domainModal struct {
	active            bool
	editing           bool // false = add, true = edit
	editIndex         int
	domainInput       textInput
	includeSubdomains bool
	focusField        int // 0 = domain, 1 = subdomain toggle, 2 = confirm, 3 = cancel
}

// whitelistModel manages the Proxy Whitelist screen, including both
// domain whitelist and port forwarding sub-tabs.
type whitelistModel struct {
	subTab whitelistSubTab

	// Domain whitelist.
	defaultDomains []config.DomainEntry
	userDomains    []config.DomainEntry
	domainCursor   int
	modal          domainModal

	// Port forwarding.
	portRules  []config.PortForwardRule
	portCursor int
	portModal  portModal

	// Scroll state for layout.
	scrollOffset     int
	lastHeight       int // cached terminal height for scroll calculations in Update
	lastMaxScroll    int // cached max scroll offset from last render
}

func newWhitelistModel(domains []config.DomainEntry, rules []config.PortForwardRule) whitelistModel {
	var defaultDomains, userDomains []config.DomainEntry
	for _, d := range domains {
		if d.Source == "default" {
			defaultDomains = append(defaultDomains, d)
		} else {
			userDomains = append(userDomains, d)
		}
	}

	return whitelistModel{
		defaultDomains: defaultDomains,
		userDomains:    userDomains,
		portRules:      append([]config.PortForwardRule{}, rules...),
		modal: domainModal{
			domainInput: newTextInput("e.g., api.example.com", 40),
		},
		portModal: portModal{
			containerPortInput: newTextInput("e.g., 5432", 20),
			hostPortInput:      newTextInput("e.g., 5432", 20),
			descInput:          newTextInput("e.g., PostgreSQL", 30),
		},
	}
}

func (m *whitelistModel) update(msg tea.Msg) whitelistResult {
	// Handle modal first if active.
	if m.modal.active {
		return m.updateDomainModal(msg)
	}
	if m.portModal.active {
		return m.updatePortModal(msg)
	}

	switch m.subTab {
	case subTabDomains:
		return m.updateDomains(msg)
	case subTabPorts:
		return m.updatePorts(msg)
	}
	return whitelistNone
}

func (m *whitelistModel) updateDomains(msg tea.Msg) whitelistResult {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		handleMouseScroll(msg, &m.scrollOffset, m.lastMaxScroll)
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if len(m.userDomains) > 0 && m.domainCursor > 0 {
				m.domainCursor--
				ensureLineVisible(&m.scrollOffset, 5+len(m.defaultDomains)+m.domainCursor, m.lastHeight, 5, 1)
			} else if m.scrollOffset > 0 {
				m.scrollOffset--
			}
		case "down", "j":
			if len(m.userDomains) > 0 && m.domainCursor < len(m.userDomains)-1 {
				m.domainCursor++
				ensureLineVisible(&m.scrollOffset, 5+len(m.defaultDomains)+m.domainCursor, m.lastHeight, 5, 1)
			} else if m.scrollOffset < m.lastMaxScroll {
				m.scrollOffset++
			}
		case "n":
			m.modal.active = true
			m.modal.editing = false
			m.modal.domainInput.SetValue("")
			m.modal.includeSubdomains = false
			m.modal.focusField = 0
			m.modal.domainInput.Focus()
		case "e", "enter":
			if len(m.userDomains) > 0 {
				d := m.userDomains[m.domainCursor]
				m.modal.active = true
				m.modal.editing = true
				m.modal.editIndex = m.domainCursor
				m.modal.domainInput.SetValue(d.Domain)
				m.modal.includeSubdomains = d.IncludeSubdomains
				m.modal.focusField = 0
				m.modal.domainInput.Focus()
			}
		case "x":
			if len(m.userDomains) > 0 && m.domainCursor < len(m.userDomains) {
				m.userDomains = append(m.userDomains[:m.domainCursor], m.userDomains[m.domainCursor+1:]...)
				if m.domainCursor >= len(m.userDomains) && m.domainCursor > 0 {
					m.domainCursor--
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
		case "tab":
			m.subTab = subTabPorts
			m.scrollOffset = 0
		case "esc":
			return whitelistBack
		}
	}
	return whitelistNone
}

func (m *whitelistModel) updateDomainModal(msg tea.Msg) whitelistResult {
	const domainModalFields = 4 // 0=domain, 1=subdomain toggle, 2=confirm, 3=cancel

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.modal.active = false
			m.modal.domainInput.Blur()
			return whitelistNone
		case "down":
			m.modal.focusField = (m.modal.focusField + 1) % domainModalFields
			if m.modal.focusField == 0 {
				m.modal.domainInput.Focus()
			} else {
				m.modal.domainInput.Blur()
			}
			return whitelistNone
		case "up":
			m.modal.focusField = (m.modal.focusField - 1 + domainModalFields) % domainModalFields
			if m.modal.focusField == 0 {
				m.modal.domainInput.Focus()
			} else {
				m.modal.domainInput.Blur()
			}
			return whitelistNone
		case " ":
			if m.modal.focusField == 1 {
				m.modal.includeSubdomains = !m.modal.includeSubdomains
				return whitelistNone
			}
		case "enter":
			// Cancel button.
			if m.modal.focusField == 3 {
				m.modal.active = false
				m.modal.domainInput.Blur()
				return whitelistNone
			}
			// Confirm button or any other field.
			domain := m.modal.domainInput.Value()
			if domain == "" {
				return whitelistNone
			}
			entry := config.DomainEntry{
				Domain:            domain,
				IncludeSubdomains: m.modal.includeSubdomains,
				Source:            "user",
			}
			if m.modal.editing {
				m.userDomains[m.modal.editIndex] = entry
			} else {
				m.userDomains = append(m.userDomains, entry)
			}
			m.modal.active = false
			m.modal.domainInput.Blur()
			return whitelistNone
		}
		// Route other keys to domain input when focused.
		if m.modal.focusField == 0 {
			m.modal.domainInput.handleKey(msg.String())
		}
	}
	return whitelistNone
}

func (m *whitelistModel) updatePorts(msg tea.Msg) whitelistResult {
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
		case "tab":
			m.subTab = subTabDomains
			m.scrollOffset = 0
		case "esc":
			return whitelistBack
		}
	}
	return whitelistNone
}

func (m *whitelistModel) view(width, height int) string {
	m.lastHeight = height
	breadcrumb := breadcrumbStyle().Render(theme.BarrelEmoji+" Configure > ") +
		lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render("Proxy Whitelist")

	// Sub-tab bar.
	domainTab := lipgloss.NewStyle().Foreground(theme.ColorDusty).Render("Domains")
	portTab := lipgloss.NewStyle().Foreground(theme.ColorDusty).Render("Port Forwarding")
	if m.subTab == subTabDomains {
		domainTab = lipgloss.NewStyle().Foreground(theme.ColorVerdigris).Bold(true).Underline(true).Render("Domains")
	} else {
		portTab = lipgloss.NewStyle().Foreground(theme.ColorVerdigris).Bold(true).Underline(true).Render("Port Forwarding")
	}

	// Header: breadcrumb, separator, tabs (active tab has underline decoration — single line, no gap).
	headerSep := lipgloss.NewStyle().Foreground(theme.ColorOakLight).Render(strings.Repeat("─", width))
	header := breadcrumb + "\n" +
		headerSep + "\n" +
		domainTab + "  " + portTab + "\n"

	var content string
	var footer string

	switch m.subTab {
	case subTabDomains:
		content, footer = m.viewDomains(width)
	case subTabPorts:
		content, footer = m.viewPorts(width)
	}

	ly := newLayout(header, content, footer, width, height)
	ly.hideTopSep = true // tab underlines replace the top separator
	ly.scrollOffset = m.scrollOffset
	// Auto-scroll to keep cursor visible based on active sub-tab.
	switch m.subTab {
	case subTabDomains:
		if len(m.userDomains) > 0 {
			// User domain rows start after: default header (1) + default separator (1)
			// + default domain lines + blank line (1) + custom header (1) + custom separator (1).
			cursorLine := 5 + len(m.defaultDomains) + m.domainCursor
			ly.EnsureVisible(cursorLine)
		}
	case subTabPorts:
		if len(m.portRules) > 0 {
			// Port rows start after: description (1) + blank (1) + header (1) + separator (1).
			cursorLine := 4 + m.portCursor
			ly.EnsureVisible(cursorLine)
		}
	}
	s := ly.Render()
	m.scrollOffset = ly.scrollOffset
	m.lastMaxScroll = ly.MaxScrollOffset()

	// Overlay modal if active.
	if m.modal.active {
		s = overlayModal(s, m.viewDomainModal(width), width, height)
	}
	if m.portModal.active {
		s = overlayModal(s, m.portModal.view(width), width, height)
	}

	return s
}

func (m *whitelistModel) viewDomains(width int) (string, string) {
	sectionDefault := lipgloss.NewStyle().Foreground(theme.ColorDusty)
	sectionCustom := lipgloss.NewStyle().Foreground(theme.ColorLinen)
	defaultDomainStyle := lipgloss.NewStyle().Foreground(theme.ColorLinen)
	defaultSourceStyle := lipgloss.NewStyle().Foreground(theme.ColorDusty).Italic(true)
	customDomainStyle := lipgloss.NewStyle().Foreground(theme.ColorParchment)

	// Map domains to their AI tool for the "Used by" column.
	domainTool := map[string]string{
		".anthropic.com":                          "Claude",
		"platform.claude.com":                     "Claude",
		"statsig.anthropic.com":                   "Claude",
		".openai.com":                             "Codex",
		".chatgpt.com":                            "Codex",
		"github.com":                              "Copilot",
		"api.github.com":                          "Copilot",
		".githubcopilot.com":                      "Copilot",
		"copilot-proxy.githubusercontent.com":     "Copilot",
		"origin-tracker.githubusercontent.com":    "Copilot",
		"copilot-telemetry.githubusercontent.com": "Copilot",
		"collector.github.com":                    "Copilot",
		"default.exp-tas.com":                     "Copilot",
		"raw.githubusercontent.com":               "GitHub",
	}

	var content string

	// Build a single table with ALL domains (default + user) so columns align.
	tbl := tableutil.NewTable("", "", "")
	tbl.SetColGap(2)

	for _, d := range m.defaultDomains {
		scope := lipgloss.NewStyle().Foreground(theme.ColorMist).Render("exact")
		if d.IncludeSubdomains {
			scope = lipgloss.NewStyle().Foreground(theme.ColorMist).Render("*." + d.Domain)
		}
		tool := domainTool[d.Domain]
		if tool == "" {
			tool = "—"
		}
		tbl.AddRow(
			defaultDomainStyle.Render(d.Domain),
			scope,
			defaultSourceStyle.Render(tool),
		)
	}

	for _, d := range m.userDomains {
		scope := lipgloss.NewStyle().Foreground(theme.ColorSlateBlue).Render("exact only")
		if d.IncludeSubdomains {
			scope = lipgloss.NewStyle().Foreground(theme.ColorVerdigris).Render("with subdomains")
		}
		tbl.AddRow(
			customDomainStyle.Render(d.Domain),
			scope,
			"",
		)
	}

	_, allRows := tbl.RenderRows(0)

	// Default domains section.
	content += " " + sectionDefault.Render("Default whitelisted domains (auto-configured for enabled AI tools):") + "\n\n"
	content += " " + sectionDefault.Render(theme.BorderH+theme.BorderH+" Default "+repeatStr(theme.BorderH, 60)) + "\n"
	for i := 0; i < len(m.defaultDomains); i++ {
		content += "   " + allRows[i] + "\n"
	}

	content += "\n\n"

	// User domains section.
	content += " " + sectionCustom.Render("Your whitelisted domains:") + "\n\n"
	content += " " + sectionDefault.Render(theme.BorderH+theme.BorderH+" Custom "+repeatStr(theme.BorderH, 61)) + "\n"

	if len(m.userDomains) == 0 {
		content += "   " + lipgloss.NewStyle().Foreground(theme.ColorFaded).Italic(true).Render("(none)") + "\n"
	}
	userRowOffset := len(m.defaultDomains)
	for i := range m.userDomains {
		prefix := "   "
		if i == m.domainCursor {
			prefix = " " + lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render(theme.IconArrowRight) + " "
		}
		line := prefix + allRows[userRowOffset+i]
		if i == m.domainCursor {
			line = lipgloss.NewStyle().Background(theme.ColorOakMid).Render(line)
		}
		content += line + "\n"
	}

	content += "\n"
	content += infoBox(" Be strict: only whitelist domains you trust completely.\n"+
		" Package registries (npm, pypi, crates.io, gopkg) are blocked by\n"+
		" default to prevent supply-chain attacks. AI tool dependencies are\n"+
		" installed at cooper build time, not runtime.\n\n"+
		" For ad-hoc access, use the Monitor tab in cooper up to approve\n"+
		" individual requests in real-time.", width)

	footer := " " + helpBar("[n New]", "[e Edit]", "[x Delete]", "[Tab Ports]", "[Esc Back]")
	return content, footer
}

func (m *whitelistModel) viewDomainModal(width int) string {
	title := "Add Whitelisted Domain"
	if m.modal.editing {
		title = "Edit Whitelisted Domain"
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(theme.ColorAmber).
		Padding(1, 3).
		Width(min(50, width-10))

	titleStyle := lipgloss.NewStyle().Foreground(theme.ColorParchment).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(theme.ColorLinen)

	var inner string
	inner += "  " + titleStyle.Render(title) + "\n\n"
	inner += "  " + lipgloss.NewStyle().Foreground(theme.ColorOakLight).Render(repeatStr(theme.BorderH, 38)) + "\n\n"

	inner += "  " + labelStyle.Render("Domain:") + "\n"
	inner += m.modal.domainInput.viewWithMargin(2) + "\n\n"

	toggle := lipgloss.NewStyle().Foreground(theme.ColorDusty).Render("[" + theme.IconDotEmpty + "]")
	if m.modal.includeSubdomains {
		toggle = lipgloss.NewStyle().Foreground(theme.ColorAmber).Render("[" + theme.IconDot + "]")
	}
	if m.modal.focusField == 1 {
		inner += "  " + lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render(theme.IconArrowRight) + " "
	} else {
		inner += "    "
	}
	inner += toggle + " " + labelStyle.Render("Include subdomains") + "\n\n"

	inner += "  " + lipgloss.NewStyle().Foreground(theme.ColorOakLight).Render(repeatStr(theme.BorderH, 38)) + "\n\n"

	// Confirm button.
	if m.modal.focusField == 2 {
		inner += "  " + lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render(theme.IconArrowRight) + " " +
			lipgloss.NewStyle().Background(theme.ColorOakMid).Foreground(theme.ColorProof).Bold(true).Render("["+theme.IconCheck+" Save]")
	} else {
		inner += "    " + lipgloss.NewStyle().Foreground(theme.ColorProof).Bold(true).Render("["+theme.IconCheck+" Save]")
	}

	inner += "    "

	// Cancel button.
	if m.modal.focusField == 3 {
		inner += lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render(theme.IconArrowRight) + " " +
			lipgloss.NewStyle().Background(theme.ColorOakMid).Foreground(theme.ColorDusty).Render("[Cancel]")
	} else {
		inner += lipgloss.NewStyle().Foreground(theme.ColorDusty).Render("[Cancel]")
	}

	return boxStyle.Render(inner)
}

func (m *whitelistModel) viewPorts(width int) (string, string) {
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

	// Render header and separator with same indent as data rows.
	rowIndent := "   " // 3 spaces — matches non-selected row prefix.
	content += rowIndent + tbl.RenderHeader() + "\n"
	content += rowIndent + tbl.RenderSeparator(0) + "\n"

	if len(m.portRules) == 0 {
		content += rowIndent + lipgloss.NewStyle().Foreground(theme.ColorFaded).Italic(true).Render("(no rules configured)") + "\n"
	}

	// Render each row with selection arrow.
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
	content += infoBox(" "+theme.IconWarn+" Host services must bind to 0.0.0.0 or the Docker gateway IP to\n"+
		" be reachable from containers. Services bound to 127.0.0.1 only will\n"+
		" NOT be accessible through port forwarding.\n\n"+
		" Forwarding uses a two-hop relay:\n"+
		" CLI container "+theme.IconArrowRight+" cooper-proxy "+theme.IconArrowRight+" host machine", width)

	footer := " " + helpBar("[n New]", "[e Edit]", "[x Delete]", "[Tab Domains]", "[Esc Back]")
	return content, footer
}

func (m *whitelistModel) toDomainEntries() []config.DomainEntry {
	result := make([]config.DomainEntry, 0, len(m.defaultDomains)+len(m.userDomains))
	result = append(result, m.defaultDomains...)
	result = append(result, m.userDomains...)
	return result
}

func (m *whitelistModel) toPortForwardRules() []config.PortForwardRule {
	result := make([]config.PortForwardRule, len(m.portRules))
	copy(result, m.portRules)
	return result
}
