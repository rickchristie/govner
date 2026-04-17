package tui

import (
	"fmt"
	"log"
	"math"
	"net/url"
	"os"
	"path/filepath"
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
	squidlogui "github.com/rickchristie/govner/cooper/internal/tui/squidlog"
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
		if ch := m.app.SquidLogs(); ch != nil {
			cmds = append(cmds, listenSquidLogs(ch))
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
		// Check clipboard TTL expiry on each tick.
		m.checkClipboardExpiry()
		// Forward to the active sub-model so it can refresh timestamps etc.
		cmd := m.forwardToActive(msg)
		return m, tea.Batch(cmd, m.tickCmd())

	case events.AnimTickMsg:
		cmd := m.forwardToActive(msg)
		return m, tea.Batch(cmd, m.animTickCmd())

	// ---- Channel events ----
	case events.ACLRequestMsg:
		// ACL events always go to the proxy monitor regardless of active tab.
		// The host-side alert also fires here in the root shell, not in the
		// proxymon sub-model, so every request that actually reached manual
		// approval can alert even while the user is on another tab.
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
		return m, tea.Batch(cmd, listenCmd, playProxyAlertCmd(m.alertPlayer))

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

	case events.SquidLogLineMsg:
		if m.squidLogModel != nil {
			if sm, ok := m.squidLogModel.(*squidlogui.Model); ok {
				sm.AddLine(msg.Line)
			}
		}
		var listenCmd tea.Cmd
		if m.app != nil {
			if ch := m.app.SquidLogs(); ch != nil {
				listenCmd = listenSquidLogs(ch)
			}
		}
		return m, listenCmd

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
				msg.ClipboardTTLSecs,
				msg.ClipboardMaxMB*1024*1024,
				msg.ProxyAlertSound,
			); err != nil {
				log.Printf("cooper: failed to update settings: %v", err)
			}
		}
		if m.alertPlayer != nil {
			if err := m.alertPlayer.SetEnabled(msg.ProxyAlertSound); err != nil {
				log.Printf("cooper: failed to update proxy alert sound: %v", err)
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

	case events.ClipboardCaptureMsg:
		if msg.Err != nil {
			m.clipboardState = app.ClipboardFailed
			m.clipboardError = msg.Err.Error()
			m.clipboardSnapshot = nil
			m.clipboardFailedAt = time.Now()
		} else if msg.Event != nil {
			m.clipboardState = msg.Event.State
			m.clipboardError = msg.Event.Error
			m.clipboardSnapshot = msg.Event.Snapshot
		}
		return m, nil

	case events.ClipboardClearMsg:
		if m.app != nil {
			m.app.ClearClipboard()
		}
		m.clipboardState = app.ClipboardEmpty
		m.clipboardSnapshot = nil
		m.clipboardError = ""
		return m, nil

	case events.ClipboardExpiredMsg:
		m.clipboardState = app.ClipboardExpired
		m.clipboardSnapshot = nil
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
		m.exitExpected = true
		return m, tea.Quit
	}

	// Default: forward unrecognized messages to active sub-model.
	cmd := m.forwardToActive(msg)
	return m, cmd
}

// handleKey routes keyboard input. Modal keys take priority, then global
// keys (quit, tab switching), then delegation to the active sub-model.
func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// --- Bracketed paste: detect file drag-and-drop ---
	// Terminal emulators send dragged file paths as bracketed paste text.
	// BubbleTea sets Paste=true on the resulting KeyMsg.
	if msg.Paste {
		if path := extractDroppedFilePath(msg); path != "" {
			return m, m.stageFileCmd(path)
		}
	}

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

	// --- Clipboard shortcuts (only when not editing a text field) ---
	if !m.isTextInputActive() {
		switch msg.String() {
		case "c", "ctrl+v":
			return m, m.captureClipboardCmd()
		case "x":
			if m.clipboardState == app.ClipboardStaged {
				return m, func() tea.Msg { return events.ClipboardClearMsg{} }
			}
		}
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

// isTextInputActive returns true when the active sub-model is in a text
// editing mode. Clipboard shortcuts (c/x) are suppressed in this state
// so keystrokes reach the sub-model's input buffer instead.
func (m *Model) isTextInputActive() bool {
	switch m.activeTab {
	case theme.TabBridgeRoutes:
		if rm, ok := m.bridgeRoutesModel.(*bridgeui.RoutesModel); ok {
			return rm.IsEditing()
		}
	case theme.TabRuntime:
		if sm, ok := m.runtimeModel.(*settings.Model); ok {
			return sm.IsEditing()
		}
	case theme.TabPortForward:
		if pm, ok := m.portForwardModel.(*portfwd.Model); ok {
			return pm.IsEditing()
		}
	}
	return false
}

// captureClipboardCmd returns a tea.Cmd that captures the host clipboard
// and emits a ClipboardCaptureMsg with the result.
func (m *Model) captureClipboardCmd() tea.Cmd {
	a := m.app
	return func() tea.Msg {
		if a == nil {
			return events.ClipboardCaptureMsg{Err: fmt.Errorf("app not available")}
		}
		event, err := a.CaptureClipboard()
		return events.ClipboardCaptureMsg{Event: event, Err: err}
	}
}

// stageFileCmd returns a tea.Cmd that stages a file from disk onto the
// clipboard bridge. It reuses ClipboardCaptureMsg so the TUI handles the
// result identically to a clipboard capture.
func (m *Model) stageFileCmd(path string) tea.Cmd {
	a := m.app
	return func() tea.Msg {
		if a == nil {
			return events.ClipboardCaptureMsg{Err: fmt.Errorf("app not available")}
		}
		event, err := a.StageFile(path)
		return events.ClipboardCaptureMsg{Event: event, Err: err}
	}
}

// extractDroppedFilePath checks whether a paste KeyMsg contains a single
// absolute file path that exists on disk. Returns the path if valid, or
// "" if the paste is not a file drop.
func extractDroppedFilePath(msg tea.KeyMsg) string {
	text := strings.TrimSpace(string(msg.Runes))
	if text == "" {
		return ""
	}

	candidate := singlePastedPathCandidate(text)
	if candidate == "" {
		return ""
	}

	for _, path := range pastedPathVariants(candidate) {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}

	return ""
}

func singlePastedPathCandidate(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	candidates := make([]string, 0, len(lines))
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i == 0 && (line == "copy" || line == "cut") {
			continue
		}
		candidates = append(candidates, line)
	}
	if len(candidates) != 1 {
		return ""
	}
	return candidates[0]
}

func pastedPathVariants(text string) []string {
	variants := make([]string, 0, 6)
	seen := make(map[string]struct{}, 6)
	add := func(value string) {
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		variants = append(variants, value)
	}

	add(text)
	if stripped := trimPastedQuotes(text); stripped != text {
		add(stripped)
	}

	snapshot := append([]string(nil), variants...)
	for _, variant := range snapshot {
		if unescaped := unescapePastedPath(variant); unescaped != variant {
			add(unescaped)
		}
	}

	snapshot = append([]string(nil), variants...)
	for _, variant := range snapshot {
		add(parseFileURIPath(variant))
	}

	filtered := variants[:0]
	for _, variant := range variants {
		if filepath.IsAbs(variant) {
			filtered = append(filtered, variant)
		}
	}
	return filtered
}

func trimPastedQuotes(text string) string {
	if len(text) < 2 {
		return text
	}
	if text[0] == '\'' && text[len(text)-1] == '\'' {
		return text[1 : len(text)-1]
	}
	if text[0] == '"' && text[len(text)-1] == '"' {
		return text[1 : len(text)-1]
	}
	return text
}

func unescapePastedPath(text string) string {
	if !strings.ContainsRune(text, '\\') {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	escaped := false
	for _, r := range text {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		b.WriteRune(r)
	}
	if escaped {
		b.WriteRune('\\')
	}
	return b.String()
}

func parseFileURIPath(text string) string {
	if !strings.HasPrefix(text, "file://") {
		return ""
	}
	u, err := url.Parse(text)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	if u.Host != "" && u.Host != "localhost" {
		return ""
	}
	path, err := url.PathUnescape(u.Path)
	if err != nil {
		return ""
	}
	return path
}

// checkClipboardExpiry checks clipboard state transitions on each tick:
//   - Staged → Expired when TTL elapses (also clears the manager)
//   - Failed → Empty after 3 seconds (auto-clear error display)
//   - Expired → Empty after 3 seconds (auto-clear expired display)
func (m *Model) checkClipboardExpiry() {
	switch m.clipboardState {
	case app.ClipboardStaged:
		if m.clipboardSnapshot != nil && m.clipboardSnapshot.IsExpired() {
			m.clipboardState = app.ClipboardExpired
			m.clipboardSnapshot = nil
			m.clipboardExpiredAt = time.Now()
			// Actively clear the manager so the bridge stops serving the image.
			if m.app != nil {
				m.app.ClearClipboard()
			}
		}
	case app.ClipboardFailed:
		if !m.clipboardFailedAt.IsZero() && time.Since(m.clipboardFailedAt) > 3*time.Second {
			m.clipboardState = app.ClipboardEmpty
			m.clipboardError = ""
		}
	case app.ClipboardExpired:
		if !m.clipboardExpiredAt.IsZero() && time.Since(m.clipboardExpiredAt) > 3*time.Second {
			m.clipboardState = app.ClipboardEmpty
		}
	}
}

// executeModalConfirm runs the action for the confirmed modal.
func (m *Model) executeModalConfirm(modal *components.Modal) (tea.Model, tea.Cmd) {
	switch modal.ModalType {
	case theme.ModalExit:
		m.exitExpected = true
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
			m.exitExpected = true
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
		m.exitExpected = true
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
	case theme.TabSquidLogs:
		m.squidLogModel = sm
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

// headerBar renders: 🥃 Cooper  barrel-proof  🛡️ Proxy ✓  <clipboard status>
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

	leftParts := []string{brand, tagline, proxyStatus}
	left := strings.Join(leftParts, "  ")

	// Clipboard status segment on the right side.
	right := m.clipboardHeaderSegment()

	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}

	row := left + strings.Repeat(" ", gap) + right

	// Pad or truncate to terminal width.
	rendered := theme.HeaderBarStyle.Width(width).Render(row)
	return rendered
}

// clipboardHeaderSegment renders the clipboard status for the header bar.
func (m *Model) clipboardHeaderSegment() string {
	label := theme.DimStyle.Render("Clipboard")
	switch m.clipboardState {
	case app.ClipboardStaged:
		if m.clipboardSnapshot == nil {
			return label + " " + theme.StatusRunningStyle.Render("Staged") +
				"  [" + theme.HelpKeyStyle.Render("c") + " Replace]" +
				"  [" + theme.HelpKeyStyle.Render("x") + " Delete]"
		}
		snap := m.clipboardSnapshot
		remaining := time.Until(snap.ExpiresAt)
		if remaining < 0 {
			remaining = 0
		}
		total := snap.ExpiresAt.Sub(snap.CreatedAt)
		if total <= 0 {
			total = 1
		}
		progress := float64(remaining) / float64(total)
		if progress > 1.0 {
			progress = 1.0
		}

		// Timer bar (compact, 10 chars wide).
		barWidth := 10
		filled := int(float64(barWidth) * progress)
		if filled > barWidth {
			filled = barWidth
		}
		empty := barWidth - filled

		color := theme.TimerColor(progress)
		filledStyle := lipgloss.NewStyle().Foreground(color)
		bar := filledStyle.Render(strings.Repeat(theme.IconBlock, filled)) +
			theme.TimerBarEmptyStyle.Render(strings.Repeat(theme.IconShade, empty))

		secs := int(math.Ceil(remaining.Seconds()))
		timeLabel := filledStyle.Render(fmt.Sprintf("%ds", secs))

		return label + " " + theme.StatusRunningStyle.Render("Staged") +
			" [" + bar + "] " + timeLabel +
			"  [" + theme.HelpKeyStyle.Render("c") + " Replace]" +
			"  [" + theme.HelpKeyStyle.Render("x") + " Delete]"

	case app.ClipboardFailed:
		errText := m.clipboardError
		if len(errText) > 30 {
			errText = errText[:27] + "..."
		}
		return label + " " + theme.ErrorStyle.Render("Failed: "+errText) +
			"  [" + theme.HelpKeyStyle.Render("c") + " Retry]"

	case app.ClipboardExpired:
		return label + " " + theme.DimStyle.Render("Expired") +
			"  [" + theme.HelpKeyStyle.Render("c") + " Copy]"

	default: // ClipboardEmpty
		return label + " " + theme.DimStyle.Render("Empty") +
			"  [" + theme.HelpKeyStyle.Render("c") + " Copy]"
	}
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
			HelpBinding{Key: "Enter", Desc: "Edit/Toggle"},
			HelpBinding{Key: "Space", Desc: "Toggle"},
		)
	case theme.TabPortForward:
		bindings = append(bindings,
			HelpBinding{Key: "n", Desc: "New"},
			HelpBinding{Key: "x", Desc: "Delete"},
			HelpBinding{Key: "Enter", Desc: "Edit"},
		)
	case theme.TabSquidLogs:
		bindings = append(bindings,
			HelpBinding{Key: "G", Desc: "Bottom"},
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
