package tui

import (
	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// SubModel re-exports theme.SubModel for use within the tui package.
type SubModel = theme.SubModel

// HelpBinding is a single key-hint pair shown in the help bar.
type HelpBinding struct {
	Key  string
	Desc string
}

// Model is the root BubbleTea model for the Cooper TUI. It owns the
// tab bar, modal overlay, and delegates to per-tab SubModels for content.
type Model struct {
	// App interface -- the single dependency for all business logic.
	app app.App

	// Terminal dimensions.
	width  int
	height int

	// Tab state.
	activeTab theme.TabID
	tabBar    components.TabBar

	// Modal overlay (nil when no modal is active).
	modal *components.Modal

	// Sub-models, one per tab. These are nil until the corresponding tab
	// package supplies a concrete implementation (Work Packages 4C-4I).
	containersModel   SubModel
	proxyMonModel     SubModel
	blockedModel      SubModel
	allowedModel      SubModel
	bridgeLogsModel   SubModel
	bridgeRoutesModel SubModel
	settingsModel     SubModel
	aboutModel        SubModel

	// Loading screen (nil after startup completes).
	loadingModel SubModel

	// Shutdown state.
	shuttingDown bool

	// Callbacks.
	onShutdown func()
	onQuit     func()
}

// NewModel creates the root model. Sub-models are nil by default;
// call the Set* methods to wire them up before running the program.
func NewModel(a app.App) *Model {
	tb := components.NewTabBar(theme.AllTabs, theme.TabContainers)
	return &Model{
		app:       a,
		activeTab: theme.TabContainers,
		tabBar:    tb,
	}
}

// ----- Setter methods -----

// SetApp sets the app interface. This is useful when the Model is created
// before the App is fully initialised.
func (m *Model) SetApp(a app.App) { m.app = a }

// SetSize updates the terminal dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.tabBar.Width = w
}

// SetContainersModel wires the containers tab.
func (m *Model) SetContainersModel(sm SubModel) { m.containersModel = sm }

// SetProxyMonModel wires the proxy monitor tab.
func (m *Model) SetProxyMonModel(sm SubModel) { m.proxyMonModel = sm }

// SetBlockedModel wires the blocked-history tab.
func (m *Model) SetBlockedModel(sm SubModel) { m.blockedModel = sm }

// SetAllowedModel wires the allowed-history tab.
func (m *Model) SetAllowedModel(sm SubModel) { m.allowedModel = sm }

// SetBridgeLogsModel wires the bridge logs tab.
func (m *Model) SetBridgeLogsModel(sm SubModel) { m.bridgeLogsModel = sm }

// SetBridgeRoutesModel wires the bridge routes tab.
func (m *Model) SetBridgeRoutesModel(sm SubModel) { m.bridgeRoutesModel = sm }

// SetSettingsModel wires the settings tab.
func (m *Model) SetSettingsModel(sm SubModel) { m.settingsModel = sm }

// SetAboutModel wires the about tab.
func (m *Model) SetAboutModel(sm SubModel) { m.aboutModel = sm }

// SetLoadingModel wires the loading/startup screen sub-model.
func (m *Model) SetLoadingModel(sm SubModel) { m.loadingModel = sm }

// SetActiveTab switches to the given tab (for tui-test --screen).
func (m *Model) SetActiveTab(tab theme.TabID) {
	m.activeTab = tab
	m.tabBar.SetActive(tab)
}

// SetOnShutdown sets the callback invoked when the user confirms exit.
func (m *Model) SetOnShutdown(fn func()) { m.onShutdown = fn }

// SetOnQuit sets the callback invoked for an immediate quit.
func (m *Model) SetOnQuit(fn func()) { m.onQuit = fn }

// activeSubModel returns the SubModel for the currently active tab,
// or nil if the tab has not been wired yet.
func (m *Model) activeSubModel() SubModel {
	switch m.activeTab {
	case theme.TabContainers:
		return m.containersModel
	case theme.TabMonitor:
		return m.proxyMonModel
	case theme.TabBlocked:
		return m.blockedModel
	case theme.TabAllowed:
		return m.allowedModel
	case theme.TabBridgeLogs:
		return m.bridgeLogsModel
	case theme.TabBridgeRoutes:
		return m.bridgeRoutesModel
	case theme.TabConfigure:
		return m.settingsModel
	case theme.TabAbout:
		return m.aboutModel
	}
	return nil
}
