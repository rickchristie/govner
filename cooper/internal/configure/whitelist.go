package configure

import (
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

// domainModal tracks the add/edit domain modal state.
type domainModal struct {
	active            bool
	editing           bool // false = add, true = edit
	editIndex         int
	domainInput       textInput
	includeSubdomains bool
	focusField        int // 0 = domain, 1 = subdomain toggle, 2 = confirm, 3 = cancel
}

// whitelistModel manages the Proxy Whitelist screen (domains only).
type whitelistModel struct {
	// Domain whitelist.
	defaultDomains []config.DomainEntry
	userDomains    []config.DomainEntry
	domainCursor   int
	modal          domainModal

	// Scroll state for layout.
	scrollOffset  int
	lastHeight    int
	lastMaxScroll int
}

func newWhitelistModel(domains []config.DomainEntry) whitelistModel {
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
		modal: domainModal{
			domainInput: newTextInput("e.g., api.example.com", 40),
		},
	}
}

func (m *whitelistModel) update(msg tea.Msg) whitelistResult {
	if m.modal.active {
		return m.updateDomainModal(msg)
	}

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

func (m *whitelistModel) view(width, height int) string {
	m.lastHeight = height
	breadcrumb := breadcrumbStyle().Render(theme.BarrelEmoji+" Configure > ") +
		lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render("Proxy Whitelist")

	header := breadcrumb

	content, footer := m.viewDomains(width)

	ly := newLayout(header, content, footer, width, height)
	ly.scrollOffset = m.scrollOffset
	if len(m.userDomains) > 0 {
		cursorLine := 5 + len(m.defaultDomains) + m.domainCursor
		ly.EnsureVisible(cursorLine)
	}
	s := ly.Render()
	m.scrollOffset = ly.scrollOffset
	m.lastMaxScroll = ly.MaxScrollOffset()

	// Overlay modal if active.
	if m.modal.active {
		s = overlayModal(s, m.viewDomainModal(width), width, height)
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

	footer := " " + helpBar("[n New]", "[e Edit]", "[x Delete]", "[Esc Back]")
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

func (m *whitelistModel) toDomainEntries() []config.DomainEntry {
	result := make([]config.DomainEntry, 0, len(m.defaultDomains)+len(m.userDomains))
	result = append(result, m.defaultDomains...)
	result = append(result, m.userDomains...)
	return result
}
