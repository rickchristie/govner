package tui

import (
	"fmt"
	"log"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/bridge"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/proxy"
	"github.com/rickchristie/govner/cooper/internal/tui/about"
	"github.com/rickchristie/govner/cooper/internal/tui/bridgeui"
	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/events"
	"github.com/rickchristie/govner/cooper/internal/tui/history"
	"github.com/rickchristie/govner/cooper/internal/tui/proxymon"
	"github.com/rickchristie/govner/cooper/internal/tui/settings"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// ----- Init -----

// Init starts background tickers and channel listeners.
func (m *Model) Init() tea.Cmd {
	var cmds []tea.Cmd

	// If we have a loading screen, let it initialise.
	if m.loadingModel != nil {
		cmds = append(cmds, m.loadingModel.Init())
	}

	// Start tick timers.
	cmds = append(cmds, m.tickCmd(), m.animTickCmd())

	// Subscribe to external event channels.
	if m.aclRequestCh != nil {
		cmds = append(cmds, listenACL(m.aclRequestCh))
	}
	if m.bridgeLogCh != nil {
		cmds = append(cmds, listenBridgeLogs(m.bridgeLogCh))
	}
	if m.aclDecisionCh != nil {
		cmds = append(cmds, listenACLDecisions(m.aclDecisionCh))
	}

	// Kick off the initial docker stats poll if enabled.
	if m.pollStats {
		cmds = append(cmds, pollStats(5*time.Second))
	}

	return tea.Batch(cmds...)
}

// ----- Update -----

// Update is the root BubbleTea update function.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	// ---- Window resize ----
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.tabBar.Width = msg.Width
		return m, nil

	// ---- Keyboard input ----
	case tea.KeyMsg:
		return m.handleKey(msg)

	// ---- Tick timers ----
	case events.TickMsg:
		// Forward to the active sub-model so it can refresh timestamps etc.
		cmd := m.forwardToActive(msg)
		return m, tea.Batch(cmd, m.tickCmd())

	case events.AnimTickMsg:
		cmd := m.forwardToActive(msg)
		return m, tea.Batch(cmd, m.animTickCmd())

	// ---- Channel events ----
	case events.ACLRequestMsg:
		// ACL events always go to the proxy monitor regardless of active tab.
		var cmd tea.Cmd
		if m.proxyMonModel != nil {
			var sm SubModel
			sm, cmd = m.proxyMonModel.Update(msg)
			m.proxyMonModel = sm
		}
		// Re-listen for the next event.
		listenCmd := listenACL(m.aclRequestCh)
		return m, tea.Batch(cmd, listenCmd)

	case events.ACLDecisionMsg:
		// Route decision to the appropriate history tab.
		entry := history.HistoryEntry{
			Request:   msg.Event.Request,
			Decision:  msg.Event.Reason, // "approved", "denied", "timeout"
			Timestamp: msg.Event.Request.Timestamp,
		}
		if msg.Event.Decision == proxy.DecisionAllow {
			if m.allowedModel != nil {
				if hm, ok := m.allowedModel.(*history.Model); ok {
					hm.AddEntry(entry)
				}
			}
		} else {
			if m.blockedModel != nil {
				if hm, ok := m.blockedModel.(*history.Model); ok {
					hm.AddEntry(entry)
				}
			}
		}
		// Re-listen for next decision.
		listenCmd := listenACLDecisions(m.aclDecisionCh)
		return m, listenCmd

	case events.BridgeLogMsg:
		var cmd tea.Cmd
		if m.bridgeLogsModel != nil {
			var sm SubModel
			sm, cmd = m.bridgeLogsModel.Update(msg)
			m.bridgeLogsModel = sm
		}
		listenCmd := listenBridgeLogs(m.bridgeLogCh)
		return m, tea.Batch(cmd, listenCmd)

	case events.ContainerStatsMsg:
		var cmd tea.Cmd
		if m.containersModel != nil {
			var sm SubModel
			sm, cmd = m.containersModel.Update(msg)
			m.containersModel = sm
		}
		// Schedule next poll.
		pollCmd := pollStats(theme.UITickInterval)
		return m, tea.Batch(cmd, pollCmd)

	case bridgeui.RoutesChangedMsg:
		// Persist route changes and hot-swap on the bridge server.
		if m.bridgeServer != nil {
			m.bridgeServer.UpdateRoutes(msg.Routes)
		}
		if m.cooperDir != "" {
			if err := bridge.SaveBridgeRoutes(m.cooperDir, msg.Routes); err != nil {
				log.Printf("cooper: failed to persist bridge routes: %v", err)
			}
		}
		return m, nil

	case settings.SettingsChangedMsg:
		// Apply changed settings to the runtime config.
		if m.cfg != nil {
			m.cfg.MonitorTimeoutSecs = msg.MonitorTimeoutSecs
			m.cfg.BlockedHistoryLimit = msg.BlockedHistoryLimit
			m.cfg.AllowedHistoryLimit = msg.AllowedHistoryLimit
			m.cfg.BridgeLogLimit = msg.BridgeLogLimit
		}
		// Propagate new values to live components.
		newTimeout := time.Duration(msg.MonitorTimeoutSecs) * time.Second
		if m.proxyMonModel != nil {
			if pm, ok := m.proxyMonModel.(*proxymon.Model); ok {
				pm.SetTimeout(newTimeout)
			}
		}
		// Update the live ACL listener so new requests use the updated timeout.
		if m.aclListener != nil {
			m.aclListener.SetTimeout(newTimeout)
		}
		if m.blockedModel != nil {
			if hm, ok := m.blockedModel.(*history.Model); ok {
				hm.SetMaxCapacity(msg.BlockedHistoryLimit)
			}
		}
		if m.allowedModel != nil {
			if hm, ok := m.allowedModel.(*history.Model); ok {
				hm.SetMaxCapacity(msg.AllowedHistoryLimit)
			}
		}
		if m.bridgeLogsModel != nil {
			if lm, ok := m.bridgeLogsModel.(*bridgeui.LogsModel); ok {
				lm.SetMaxCapacity(msg.BridgeLogLimit)
			}
		}
		return m, nil

	case settings.PortForwardChangedMsg:
		// Show a "Reloading..." modal and run the reload in the background.
		modal := components.NewModal(
			theme.ModalReloadSocat,
			"Reloading Port Forwarding...",
			"Writing rules and signaling containers.\nPlease wait.",
			"",
			"",
		)
		m.modal = &modal
		// Run validation + reload + persist as a background command.
		rules := msg.Rules
		return m, m.reloadPortForwardsCmd(rules)

	case portForwardReloadResultMsg:
		// Send result back to settings model so it can update its status.
		var resultCmd tea.Cmd
		if msg.Err != nil {
			modal := components.NewModal(
				theme.ModalReloadSocat,
				"Reload Failed",
				msg.Err.Error(),
				"OK",
				"Dismiss",
			)
			m.modal = &modal
			// Tell settings model the old (pre-change) rules so it can revert.
			oldRules := m.cfg.PortForwardRules
			resultCmd = func() tea.Msg {
				return settings.PFApplyResultMsg{OK: false, ErrMsg: msg.Err.Error(), Rules: oldRules}
			}
		} else {
			modal := components.NewModal(
				theme.ModalReloadSocat,
				theme.IconCheck+" Reload Successful",
				"Port forwarding rules updated.\nAll containers reloaded.",
				"OK",
				"Dismiss",
			)
			m.modal = &modal
			// Update local config state on success.
			if m.cfg != nil {
				m.cfg.PortForwardRules = msg.Rules
			}
			resultCmd = func() tea.Msg {
				return settings.PFApplyResultMsg{OK: true, Rules: msg.Rules}
			}
		}
		return m, resultCmd

	case about.RunUpdateMsg:
		modal := components.NewModal(
			theme.ModalUpdateInfo,
			theme.BarrelEmoji+" Update Required",
			"The TUI is currently running.\nOpen another terminal and run:\n\n  cooper update\n\nto rebuild the container image.",
			"OK",
			"Dismiss",
		)
		m.modal = &modal
		return m, nil

	case events.ShutdownCompleteMsg:
		m.shuttingDown = false
		return m, tea.Quit
	}

	// Default: forward unrecognized messages to active sub-model.
	cmd := m.forwardToActive(msg)
	return m, cmd
}

// handleKey routes keyboard input. Modal keys take priority, then global
// keys (quit, tab switching), then delegation to the active sub-model.
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// --- Modal is active: arrow keys navigate, Enter/Esc confirm/cancel ---
	if m.modal != nil && m.modal.Active {
		switch msg.String() {
		case "enter":
			if m.modal.FocusConfirm {
				modal := m.modal
				m.modal = nil
				return m.executeModalConfirm(modal)
			}
			// Cancel focused — dismiss modal.
			m.modal = nil
			return m, nil
		case "esc":
			m.modal = nil
			return m, nil
		case "up", "down", "left", "right":
			m.modal.FocusConfirm = !m.modal.FocusConfirm
			return m, nil
		}
		// Swallow all other keys while modal is up.
		return m, nil
	}

	// --- Global keys ---
	switch msg.String() {
	case "q", "ctrl+c":
		m.showExitModal()
		return m, nil

	case "1":
		return m.switchTab(theme.TabContainers)
	case "2":
		return m.switchTab(theme.TabMonitor)
	case "3":
		return m.switchTab(theme.TabBlocked)
	case "4":
		return m.switchTab(theme.TabAllowed)
	case "5":
		return m.switchTab(theme.TabBridgeLogs)
	case "6":
		return m.switchTab(theme.TabBridgeRoutes)
	case "7":
		return m.switchTab(theme.TabConfigure)
	case "8":
		return m.switchTab(theme.TabAbout)

	case "tab":
		m.tabBar.Next()
		m.activeTab = m.tabBar.ActiveTab
		return m, nil
	case "shift+tab":
		m.tabBar.Prev()
		m.activeTab = m.tabBar.ActiveTab
		return m, nil
	}

	// --- Delegate to active sub-model ---
	cmd := m.forwardToActive(msg)
	return m, cmd
}

// switchTab changes the active tab.
func (m *Model) switchTab(tab theme.TabID) (tea.Model, tea.Cmd) {
	m.activeTab = tab
	m.tabBar.SetActive(tab)
	return m, nil
}

// showExitModal displays the exit confirmation modal.
func (m *Model) showExitModal() {
	modal := components.NewModal(
		theme.ModalExit,
		theme.BarrelEmoji+" Exit Cooper?",
		"This will stop the proxy and all\ncontainers. AI sessions\nwill lose network access.",
		"Confirm",
		"Cancel",
	)
	m.modal = &modal
}

// executeModalConfirm runs the action for the confirmed modal.
func (m *Model) executeModalConfirm(modal *components.Modal) (tea.Model, tea.Cmd) {
	switch modal.ModalType {
	case theme.ModalExit:
		if m.onShutdown != nil {
			m.shuttingDown = true
			m.onShutdown()
			// The shutdown callback will eventually send shutdownCompleteMsg.
			return m, nil
		}
		if m.onQuit != nil {
			m.onQuit()
		}
		return m, tea.Quit
	}
	return m, nil
}

// forwardToActive sends a message to the currently active sub-model
// and returns any resulting command.
func (m *Model) forwardToActive(msg tea.Msg) tea.Cmd {
	sm := m.activeSubModel()
	if sm == nil {
		return nil
	}
	updated, cmd := sm.Update(msg)
	m.setActiveSubModel(updated)
	return cmd
}

// setActiveSubModel writes the updated sub-model back to the correct field.
func (m *Model) setActiveSubModel(sm SubModel) {
	switch m.activeTab {
	case theme.TabContainers:
		m.containersModel = sm
	case theme.TabMonitor:
		m.proxyMonModel = sm
	case theme.TabBlocked:
		m.blockedModel = sm
	case theme.TabAllowed:
		m.allowedModel = sm
	case theme.TabBridgeLogs:
		m.bridgeLogsModel = sm
	case theme.TabBridgeRoutes:
		m.bridgeRoutesModel = sm
	case theme.TabConfigure:
		m.settingsModel = sm
	case theme.TabAbout:
		m.aboutModel = sm
	}
}

// ----- View -----

// View renders the full TUI screen.
func (m *Model) View() string {
	// During loading, delegate entirely to the loading model.
	if m.loadingModel != nil && !m.shuttingDown {
		return m.loadingModel.View(m.width, m.height)
	}

	var sections []string

	sep := lipgloss.NewStyle().Foreground(theme.ColorOakLight).Render(strings.Repeat("─", m.width))

	// Header bar.
	sections = append(sections, m.headerBar(m.width))

	// Header-tab separator.
	sections = append(sections, sep)

	// Tab bar (single line, active tab underlined).
	sections = append(sections, m.tabBar.View())

	// Tab bottom padding (fixed blank line).
	sections = append(sections, "")

	// Active tab content — pad to fill contentHeight so footer stays at bottom.
	contentHeight := m.contentHeight()
	var tabContent string
	if sm := m.activeSubModel(); sm != nil {
		tabContent = sm.View(m.width, contentHeight)
	} else {
		tabContent = theme.EmptyStateStyle.Width(m.width).Render(
			fmt.Sprintf("Tab %d content coming soon", int(m.activeTab)+1),
		)
	}

	// Pad content to fill available height.
	contentLines := strings.Split(tabContent, "\n")
	for len(contentLines) < contentHeight {
		contentLines = append(contentLines, "")
	}
	sections = append(sections, strings.Join(contentLines, "\n"))

	// Content-footer separator.
	sections = append(sections, sep)

	// Help bar (footer).
	sections = append(sections, m.helpBar(m.width))

	screen := strings.Join(sections, "\n")

	// If a modal is active, dim the background and overlay the modal.
	if m.modal != nil && m.modal.Active {
		dimmed := components.DimContent(screen)
		overlay := m.modal.View(m.width, m.height)
		return lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top, dimmed) +
			"\r" + overlay
	}

	return screen
}

// contentHeight returns the available height for tab content after
// subtracting: header(1) + sep(1) + tabs(1) + padding(1) + footer sep(1) + footer(1) = 6.
func (m *Model) contentHeight() int {
	h := m.height - 6
	if h < 1 {
		h = 1
	}
	return h
}

// headerBar renders: 🥃 Cooper  barrel-proof  🛡️ Proxy ✓  📦 N containers  ⏱ N pending
func (m *Model) headerBar(width int) string {
	brand := theme.BrandStyle.Render(theme.BarrelEmoji + " Cooper")
	tagline := theme.TaglineStyle.Render("barrel-proof")

	// Proxy status -- we use the presence of the ACL channel as a proxy for
	// "proxy is running". A proper check would call docker.IsProxyRunning(),
	// but that is a blocking Docker API call unsuitable for a synchronous
	// View render. The channel is non-nil only when startup succeeded, so
	// this is an acceptable heuristic for v1.
	var proxyStatus string
	if m.aclRequestCh != nil {
		proxyStatus = theme.StatusRunningStyle.Render(theme.IconShield + "  Proxy " + theme.IconCheck)
	} else {
		proxyStatus = theme.StatusStoppedStyle.Render(theme.IconShield + "  Proxy " + theme.IconCross)
	}

	parts := []string{brand, tagline, proxyStatus}

	row := strings.Join(parts, "  ")

	// Pad or truncate to terminal width.
	rendered := theme.HeaderBarStyle.Width(width).Render(row)
	return rendered
}

// helpBar renders context-sensitive keybindings at the bottom of the screen.
func (m *Model) helpBar(width int) string {
	bindings := []HelpBinding{
		{Key: "q", Desc: "Quit"},
		{Key: "1-8", Desc: "Tabs"},
		{Key: "\u2191\u2193", Desc: "Navigate"},
	}

	// Add tab-specific bindings.
	switch m.activeTab {
	case theme.TabMonitor:
		bindings = append(bindings,
			HelpBinding{Key: "a", Desc: "Approve"},
			HelpBinding{Key: "d", Desc: "Deny"},
		)
	case theme.TabContainers:
		bindings = append(bindings,
			HelpBinding{Key: "s", Desc: "Stop"},
			HelpBinding{Key: "r", Desc: "Restart"},
		)
	case theme.TabBridgeRoutes:
		bindings = append(bindings,
			HelpBinding{Key: "n", Desc: "New"},
			HelpBinding{Key: "x", Desc: "Delete"},
		)
	case theme.TabConfigure:
		bindings = append(bindings,
			HelpBinding{Key: "Tab", Desc: "Section"},
			HelpBinding{Key: "n", Desc: "New"},
			HelpBinding{Key: "x", Desc: "Delete"},
		)
	}

	var parts []string
	for _, b := range bindings {
		parts = append(parts,
			"["+theme.HelpKeyStyle.Render(b.Key)+" "+theme.HelpDescStyle.Render(b.Desc)+"]",
		)
	}

	left := strings.Join(parts, "  ")
	right := theme.BarrelEmoji

	// Fill the gap between left and right with spaces.
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}

	return theme.HelpBarStyle.Render(left + strings.Repeat(" ", gap) + right)
}

// ----- Tick commands -----

func (m *Model) tickCmd() tea.Cmd {
	return tea.Tick(theme.UITickInterval, func(time.Time) tea.Msg {
		return events.TickMsg{}
	})
}

func (m *Model) animTickCmd() tea.Cmd {
	return tea.Tick(theme.CountdownTickInterval, func(time.Time) tea.Msg {
		return events.AnimTickMsg{}
	})
}

// ----- Public entry point -----

// NewProgram creates a BubbleTea program with the alternate screen enabled.
// The caller should invoke p.Run() to start the event loop. Having access
// to the *tea.Program before Run blocks allows external goroutines (e.g.
// the shutdown callback) to send messages via p.Send().
func NewProgram(m *Model) *tea.Program {
	return tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
}

// Run creates and runs a BubbleTea program. This is a convenience wrapper
// around NewProgram + p.Run() for callers that do not need the program
// reference.
func Run(m *Model) error {
	p := NewProgram(m)
	_, err := p.Run()
	return err
}

// ----- Port forward reload -----

// portForwardReloadResultMsg carries the result of an async port forward reload.
type portForwardReloadResultMsg struct {
	Rules []config.PortForwardRule
	Err   error
}

// reloadPortForwardsCmd returns a tea.Cmd that validates, reloads socat, and
// persists config in the background.
func (m *Model) reloadPortForwardsCmd(rules []config.PortForwardRule) tea.Cmd {
	cooperDir := m.cooperDir
	cfg := m.cfg
	return func() tea.Msg {
		// Validate.
		if cfg != nil {
			candidate := *cfg
			candidate.PortForwardRules = rules
			if err := candidate.Validate(); err != nil {
				return portForwardReloadResultMsg{Rules: rules, Err: fmt.Errorf("validation failed: %w", err)}
			}
		}

		// Reload socat (writes socat-rules.json + signals containers).
		if cooperDir != "" && cfg != nil {
			if err := docker.ReloadSocat(cooperDir, cfg.BridgePort, rules); err != nil {
				return portForwardReloadResultMsg{Rules: rules, Err: fmt.Errorf("reload failed: %w", err)}
			}
		}

		// Persist config.json.
		if cooperDir != "" && cfg != nil {
			cfgCopy := *cfg
			cfgCopy.PortForwardRules = rules
			cfgPath := cooperDir + "/config.json"
			if err := config.SaveConfig(cfgPath, &cfgCopy); err != nil {
				return portForwardReloadResultMsg{Rules: rules, Err: fmt.Errorf("config save failed: %w", err)}
			}
		}

		return portForwardReloadResultMsg{Rules: rules, Err: nil}
	}
}
