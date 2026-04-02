package tui

import (
	"fmt"
	"log"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/tui/about"
	"github.com/rickchristie/govner/cooper/internal/tui/bridgeui"
	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/events"
	"github.com/rickchristie/govner/cooper/internal/tui/history"
	"github.com/rickchristie/govner/cooper/internal/tui/loading"
	"github.com/rickchristie/govner/cooper/internal/tui/portfwd"
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

	// Subscribe to external event channels via the App.
	if m.app != nil {
		if ch := m.app.ACLRequests(); ch != nil {
			cmds = append(cmds, listenACL(ch))
		}
		if ch := m.app.BridgeLogs(); ch != nil {
			cmds = append(cmds, listenBridgeLogs(ch))
		}
		if ch := m.app.ACLDecisions(); ch != nil {
			cmds = append(cmds, listenACLDecisions(ch))
		}

		// Kick off the initial docker stats poll.
		cmds = append(cmds, pollStats(m.app, 5*time.Second))
	}

	return tea.Batch(cmds...)
}

// ----- Update -----

// Update is the root BubbleTea update function.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// During shutdown, route all messages through the shutdown loading screen.
	if m.shuttingDown && m.shutdownModel != nil {
		return m.updateShutdown(msg)
	}

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
		var listenCmd tea.Cmd
		if m.app != nil {
			if ch := m.app.ACLRequests(); ch != nil {
				listenCmd = listenACL(ch)
			}
		}
		return m, tea.Batch(cmd, listenCmd)

	case events.ACLDecisionMsg:
		// Route decision to the appropriate history tab.
		entry := history.HistoryEntry{
			Request:   msg.Event.Request,
			Decision:  msg.Event.Reason, // "approved", "denied", "timeout"
			Timestamp: msg.Event.Request.Timestamp,
		}
		if msg.Event.Decision == app.DecisionAllow {
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
		var listenCmd tea.Cmd
		if m.app != nil {
			if ch := m.app.ACLDecisions(); ch != nil {
				listenCmd = listenACLDecisions(ch)
			}
		}
		return m, listenCmd

	case events.BridgeLogMsg:
		var cmd tea.Cmd
		if m.bridgeLogsModel != nil {
			var sm SubModel
			sm, cmd = m.bridgeLogsModel.Update(msg)
			m.bridgeLogsModel = sm
		}
		var listenCmd tea.Cmd
		if m.app != nil {
			if ch := m.app.BridgeLogs(); ch != nil {
				listenCmd = listenBridgeLogs(ch)
			}
		}
		return m, tea.Batch(cmd, listenCmd)

	case events.ContainerStatsMsg:
		var cmd tea.Cmd
		if m.containersModel != nil {
			var sm SubModel
			sm, cmd = m.containersModel.Update(msg)
			m.containersModel = sm
		}
		// Schedule next poll.
		var pollCmd tea.Cmd
		if m.app != nil {
			pollCmd = pollStats(m.app, theme.UITickInterval)
		}
		return m, tea.Batch(cmd, pollCmd)

	case bridgeui.RoutesChangedMsg:
		// Persist route changes and hot-swap on the bridge server via the App.
		if m.app != nil {
			if err := m.app.UpdateBridgeRoutes(msg.Routes); err != nil {
				log.Printf("cooper: failed to update bridge routes: %v", err)
			}
		}
		return m, nil

	case settings.SettingsChangedMsg:
		// Apply changed settings via the App.
		if m.app != nil {
			if err := m.app.UpdateSettings(
				msg.MonitorTimeoutSecs,
				msg.BlockedHistoryLimit,
				msg.AllowedHistoryLimit,
				msg.BridgeLogLimit,
			); err != nil {
				log.Printf("cooper: failed to update settings: %v", err)
			}
		}
		// Propagate new values to live TUI components.
		newTimeout := time.Duration(msg.MonitorTimeoutSecs) * time.Second
		if m.proxyMonModel != nil {
			if pm, ok := m.proxyMonModel.(*proxymon.Model); ok {
				pm.SetTimeout(newTimeout)
			}
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

	case portfwd.PortForwardChangedMsg:
		// Show a "Reloading..." modal and run the reload in the background.
		modal := components.NewModal(
			theme.ModalReloadSocat,
			"Reloading Port Forwarding...",
			"Writing rules and signaling containers.\nPlease wait.",
			"",
			"",
		)
		m.modal = &modal
		// Run reload via the App as a background command.
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
			var oldRules []app.PortForwardRule
			if m.app != nil {
				oldRules = m.app.Config().PortForwardRules
			}
			resultCmd = func() tea.Msg {
				return portfwd.PFApplyResultMsg{OK: false, ErrMsg: msg.Err.Error(), Rules: oldRules}
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
			resultCmd = func() tea.Msg {
				return portfwd.PFApplyResultMsg{OK: true, Rules: msg.Rules}
			}
		}
		return m, resultCmd

	case portfwd.PFApplyResultMsg:
		// Always route to the port forward model regardless of active tab.
		if m.portForwardModel != nil {
			var sm SubModel
			sm, _ = m.portForwardModel.Update(msg)
			m.portForwardModel = sm
		}
		return m, nil

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
		// Legacy fallback — new shutdown flow uses the loading screen.
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
			m.modal = nil
			sdModel := loading.New(true)
			m.shutdownModel = &sdModel
			m.onShutdown()
			return m, m.shutdownModel.Init()
		}
		if m.onQuit != nil {
			m.onQuit()
		}
		return m, tea.Quit
	}
	return m, nil
}

// updateShutdown routes messages to the shutdown loading model. It converts
// ShutdownStepCompleteMsg/ErrorMsg into the loading model's own step messages,
// and forwards everything else (animation ticks, hold timer, window resize)
// directly. When the loading model reports Done, it quits.
func (m *Model) updateShutdown(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case events.ShutdownStepCompleteMsg:
		updated, cmd := m.shutdownModel.Update(loading.StepCompleteMsg{Index: msg.Index})
		m.shutdownModel = &updated
		if updated.Done {
			return m, tea.Quit
		}
		return m, cmd

	case events.ShutdownStepErrorMsg:
		updated, cmd := m.shutdownModel.Update(loading.StepErrorMsg{Index: msg.Index, Err: msg.Err})
		m.shutdownModel = &updated
		return m, cmd
	}

	// Forward all other messages (animTickMsg, holdDoneMsg, KeyMsg, etc.)
	updated, cmd := m.shutdownModel.Update(msg)
	m.shutdownModel = &updated
	if updated.Done {
		return m, tea.Quit
	}
	return m, cmd
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
	case theme.TabRuntime:
		m.runtimeModel = sm
	case theme.TabPortForward:
		m.portForwardModel = sm
	case theme.TabAbout:
		m.aboutModel = sm
	}
}

// ----- View -----

// View renders the full TUI screen.
func (m *Model) View() string {
	// During shutdown, delegate to the shutdown loading model.
	if m.shuttingDown && m.shutdownModel != nil {
		return m.shutdownModel.View(m.width, m.height)
	}

	// During startup loading, delegate to the loading model.
	if m.loadingModel != nil {
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

	// If a modal is active, show only the modal (full screen, centered).
	if m.modal != nil && m.modal.Active {
		return m.modal.View(m.width, m.height)
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

	// Proxy status -- check via the App interface.
	var proxyStatus string
	if m.app != nil && m.app.IsProxyRunning() {
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
		{Key: "Tab", Desc: "Switch"},
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
	case theme.TabRuntime:
		bindings = append(bindings,
			HelpBinding{Key: "Enter", Desc: "Edit"},
		)
	case theme.TabPortForward:
		bindings = append(bindings,
			HelpBinding{Key: "n", Desc: "New"},
			HelpBinding{Key: "x", Desc: "Delete"},
			HelpBinding{Key: "Enter", Desc: "Edit"},
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
	Rules []app.PortForwardRule
	Err   error
}

// reloadPortForwardsCmd returns a tea.Cmd that validates and reloads
// port forwarding via the App.
func (m *Model) reloadPortForwardsCmd(rules []app.PortForwardRule) tea.Cmd {
	a := m.app
	return func() tea.Msg {
		if a == nil {
			return portForwardReloadResultMsg{Rules: rules, Err: fmt.Errorf("app not available")}
		}
		if err := a.UpdatePortForwards(rules); err != nil {
			return portForwardReloadResultMsg{Rules: rules, Err: err}
		}
		return portForwardReloadResultMsg{Rules: rules, Err: nil}
	}
}
