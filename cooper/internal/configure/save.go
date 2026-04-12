package configure

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// saveResult is returned by the save screen update.
type saveResult int

const (
	saveNone saveResult = iota
	saveBack
	saveQuit
)

// saveModel manages the Save & Build screen.
type saveModel struct {
	cfg                 *config.Config
	cooperDir           string
	configPath          string
	configureApp        *app.ConfigureApp
	saved               bool
	buildRequested      bool
	cleanBuildRequested bool
	saveErr             string
	doneMsgs            []string
	focusBtn            int // 0=save&build, 1=save only

	// Scroll state for layout.
	scrollOffset  int
	lastMaxScroll int // cached max scroll offset from last render
}

func newSaveModel(cfg *config.Config, cooperDir, configPath string, ca *app.ConfigureApp) saveModel {
	return saveModel{
		cfg:          cfg,
		cooperDir:    cooperDir,
		configPath:   configPath,
		configureApp: ca,
	}
}

func (m *saveModel) update(msg tea.Msg) saveResult {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		handleMouseScroll(msg, &m.scrollOffset, m.lastMaxScroll)
		return saveNone
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			if m.saved {
				return saveQuit
			}
			return saveBack
		case "enter":
			// Save & Build: save config + request build.
			if err := m.doSave(); err != nil {
				m.saveErr = err.Error()
				return saveNone
			}
			m.buildRequested = true
			m.saved = true
			return saveQuit
		case "s":
			// Save only.
			if err := m.doSave(); err != nil {
				m.saveErr = err.Error()
				return saveNone
			}
			m.saved = true
			return saveQuit
		case "c":
			// Save & Clean Build (no Docker cache).
			if err := m.doSave(); err != nil {
				m.saveErr = err.Error()
				return saveNone
			}
			m.buildRequested = true
			m.cleanBuildRequested = true
			m.saved = true
			return saveQuit
		case "left", "h":
			if m.focusBtn > 0 {
				m.focusBtn--
			}
		case "right", "l":
			if m.focusBtn < 1 {
				m.focusBtn++
			}
		case "up", "k":
			if m.scrollOffset > 0 {
				m.scrollOffset--
			}
		case "down", "j":
			if m.scrollOffset < m.lastMaxScroll {
				m.scrollOffset++
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
		}
	}
	return saveNone
}

func (m *saveModel) doSave() error {
	// Sync the TUI-mutated config back into the ConfigureApp before saving.
	if m.configureApp != nil {
		m.configureApp.SetProgrammingTools(m.cfg.ProgrammingTools)
		m.configureApp.SetAITools(m.cfg.AITools)
		m.configureApp.SetWhitelistedDomains(m.cfg.WhitelistedDomains)
		m.configureApp.SetPortForwardRules(m.cfg.PortForwardRules)
		m.configureApp.SetProxyPort(m.cfg.ProxyPort)
		m.configureApp.SetBridgePort(m.cfg.BridgePort)
		m.configureApp.SetBarrelSHMSize(m.cfg.BarrelSHMSize)

		warnings, err := m.configureApp.Save()
		if err != nil {
			return err
		}
		m.doneMsgs = append(m.doneMsgs, warnings...)
	}

	m.doneMsgs = append(m.doneMsgs, fmt.Sprintf("Saved %s", m.configPath))
	m.doneMsgs = append(m.doneMsgs, fmt.Sprintf("Generated templates in %s", m.cooperDir))
	m.doneMsgs = append(m.doneMsgs, "CA certificate ensured")
	m.doneMsgs = append(m.doneMsgs, "")
	m.doneMsgs = append(m.doneMsgs, "Configuration saved. Run 'cooper build' to rebuild images.")
	m.doneMsgs = append(m.doneMsgs, "  Base image: rebuilds if programming tool or implicit language-server versions changed.")
	m.doneMsgs = append(m.doneMsgs, "  AI tool images: each tool rebuilds independently.")

	return nil
}

func (m *saveModel) view(width, height int) string {
	breadcrumb := breadcrumbStyle().Render(theme.BarrelEmoji+" Configure > ") +
		lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render("Save & Build")

	header := breadcrumb

	// Configuration summary.
	sectionStyle := lipgloss.NewStyle().Foreground(theme.ColorDusty)
	labelStyle := lipgloss.NewStyle().Foreground(theme.ColorLinen)
	valueStyle := lipgloss.NewStyle().Foreground(theme.ColorParchment)

	var content string
	content += " " + sectionStyle.Render(theme.BorderH+theme.BorderH+" Configuration Summary "+repeatStr(theme.BorderH, 48)) + "\n\n"

	// Programming tools summary.
	progSummary := toolSummary(m.cfg.ProgrammingTools)
	content += " " + labelStyle.Render("Programming Tools:  ") + valueStyle.Render(progSummary) + "\n"

	// AI tools summary.
	aiSummary := toolSummary(m.cfg.AITools)
	content += " " + labelStyle.Render("AI CLI Tools:       ") + valueStyle.Render(aiSummary) + "\n"

	// Whitelist summary.
	defaultCount, userCount := 0, 0
	for _, d := range m.cfg.WhitelistedDomains {
		if d.Source == "default" {
			defaultCount++
		} else {
			userCount++
		}
	}
	content += " " + labelStyle.Render("Whitelisted:        ") +
		valueStyle.Render(fmt.Sprintf("%d domains (%d default + %d custom)",
			defaultCount+userCount, defaultCount, userCount)) + "\n"

	// Port forwarding.
	content += " " + labelStyle.Render("Port Forwarding:    ") +
		valueStyle.Render(fmt.Sprintf("%d rules", len(m.cfg.PortForwardRules))) + "\n"

	// Ports.
	content += " " + labelStyle.Render("Proxy Port:         ") +
		lipgloss.NewStyle().Foreground(theme.ColorAmber).Render(fmt.Sprintf("%d", m.cfg.ProxyPort)) + "\n"
	content += " " + labelStyle.Render("Bridge Port:        ") +
		lipgloss.NewStyle().Foreground(theme.ColorAmber).Render(fmt.Sprintf("%d", m.cfg.BridgePort)) + "\n"
	content += " " + labelStyle.Render("Barrel SHM Size:    ") +
		lipgloss.NewStyle().Foreground(theme.ColorAmber).Render(m.cfg.BarrelSHMSize) + "\n"

	content += "\n"

	// Files to write.
	content += " " + sectionStyle.Render(theme.BorderH+theme.BorderH+" Files to Write "+repeatStr(theme.BorderH, 53)) + "\n\n"

	fileStyle := lipgloss.NewStyle().Foreground(theme.ColorVerdigris)
	fileDesc := lipgloss.NewStyle().Foreground(theme.ColorDusty)
	cooperPath := "~/.cooper"

	files := []struct{ path, desc string }{
		{cooperPath + "/config.json", "configuration"},
		{cooperPath + "/proxy/proxy.Dockerfile", "proxy image"},
		{cooperPath + "/proxy/squid.conf", "proxy config"},
		{cooperPath + "/base/Dockerfile", "base image"},
		{cooperPath + "/base/entrypoint.sh", "base entrypoint"},
		{cooperPath + "/cli/<tool>/Dockerfile", "per-tool images"},
	}
	for _, f := range files {
		content += fmt.Sprintf("   %s  %s\n",
			fileStyle.Render(fmt.Sprintf("%-45s", f.path)),
			fileDesc.Render(f.desc))
	}

	content += "\n"

	// Action box.
	actionBox := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(theme.ColorOakLight).
		Padding(1, 2).
		Width(min(72, width-4))

	saveBuildBtn := lipgloss.NewStyle().Foreground(theme.ColorProof).Bold(true).
		Render("[Enter " + theme.IconCheck + " Save & Build]")
	cleanBuildBtn := lipgloss.NewStyle().Foreground(theme.ColorFlame).
		Render("[c Clean Build]")
	saveOnlyBtn := lipgloss.NewStyle().Foreground(theme.ColorAmber).
		Render("[s Save Only]")
	cancelBtn := lipgloss.NewStyle().Foreground(theme.ColorDusty).
		Render("[Esc Cancel]")

	boxWidth := min(66, width-12)
	inner := center("Save configuration and build images?", boxWidth) + "\n\n" +
		center(saveBuildBtn+"   "+cleanBuildBtn, boxWidth) + "\n" +
		center(saveOnlyBtn+"   "+cancelBtn, boxWidth)

	content += actionBox.Render(inner) + "\n"

	if m.saveErr != "" {
		content += "\n " + lipgloss.NewStyle().Foreground(theme.ColorFlame).Bold(true).Render("Error: "+m.saveErr) + "\n"
	}

	for _, dm := range m.doneMsgs {
		content += " " + lipgloss.NewStyle().Foreground(theme.ColorProof).Render(theme.IconCheck+" "+dm) + "\n"
	}

	footer := " " + helpBar("[Enter Build]", "[c Clean Build]", "[s Save]", "[Esc Cancel]")

	ly := newLayout(header, content, footer, width, height)
	ly.scrollOffset = m.scrollOffset
	result := ly.Render()
	m.scrollOffset = ly.scrollOffset
	m.lastMaxScroll = ly.MaxScrollOffset()
	return result
}

// toolSummary returns a comma-separated summary of enabled tools.
func toolSummary(tools []config.ToolConfig) string {
	modeStyle := map[config.VersionMode]lipgloss.Style{
		config.ModeMirror: lipgloss.NewStyle().Foreground(theme.ColorSlateBlue),
		config.ModeLatest: lipgloss.NewStyle().Foreground(theme.ColorVerdigris),
		config.ModePin:    lipgloss.NewStyle().Foreground(theme.ColorAmber),
	}

	var parts []string
	for _, t := range tools {
		if !t.Enabled {
			continue
		}
		ms := lipgloss.NewStyle().Foreground(theme.ColorDusty)
		if s, ok := modeStyle[t.Mode]; ok {
			ms = s
		}
		parts = append(parts, t.Name+" "+ms.Render("("+t.Mode.String()+")"))
	}
	if len(parts) == 0 {
		return lipgloss.NewStyle().Foreground(theme.ColorFaded).Render("(none)")
	}

	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ", "
		}
		result += p
	}
	return result
}
