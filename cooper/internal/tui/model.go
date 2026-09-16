package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/events"
	"github.com/rickchristie/govner/cooper/internal/tui/loading"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// SubModel re-exports theme.SubModel for use within the tui package.
type SubModel = theme.SubModel

// HelpBinding is a single key-hint pair shown in the help bar.
type HelpBinding struct {
	Key  string
	Desc string
}

// AlertPlayer plays host-side alerts for new proxy approvals.
type AlertPlayer interface {
	PlayProxyApprovalNeeded() error
	SetEnabled(bool) error
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

	// Pending confirmed workload action while the shared root modal is open.
	pendingWorkloadAction string
	pendingWorkloadName   string

	// Sub-models are optional until the caller wires each screen.
	runtimesModel    SubModel
	proxyMonModel    SubModel
	historyModel     SubModel
	squidLogModel    SubModel
	bridgeModel      SubModel
	runtimeModel     SubModel
	portForwardModel SubModel
	profilesModel    SubModel

	// Loading screen (nil after startup completes).
	loadingModel SubModel

	// Clipboard bridge state.
	clipboardState     app.ClipboardState
	clipboardSnapshot  *app.StagedSnapshot
	clipboardError     string
	clipboardFailedAt  time.Time
	clipboardExpiredAt time.Time
	headerHealth       app.HeaderHealth
	alertPlayer        AlertPlayer

	// ACL requests and decisions arrive on separate channels. Track their
	// pairing so a session-resolved decision that wins the scheduling race
	// cannot be followed by a stale permission prompt.
	seenPromptedACLRequests     map[string]struct{}
	resolvedPromptedACLRequests map[string]struct{}

	// Shutdown state.
	shuttingDown  bool
	shutdownModel *loading.Model
	exitExpected  bool
	exitReason    string

	// Callbacks.
	onShutdown func()
	onQuit     func()
}

// NewModel creates the root model. Sub-models are nil by default;
// call the Set* methods to wire them up before running the program.
func NewModel(a app.App) *Model {
	tb := components.NewTabBar(theme.AllTabs, theme.TabRuntimes)
	health := app.HeaderHealth{}
	if a != nil {
		health = a.HeaderHealth()
	}
	return &Model{
		app:                         a,
		activeTab:                   theme.TabRuntimes,
		tabBar:                      tb,
		headerHealth:                health,
		seenPromptedACLRequests:     make(map[string]struct{}),
		resolvedPromptedACLRequests: make(map[string]struct{}),
	}
}

// ----- Setter methods -----

// SetApp sets the app interface. This is useful when the Model is created
// before the App is fully initialised.
func (m *Model) SetApp(a app.App) {
	m.app = a
	clear(m.seenPromptedACLRequests)
	clear(m.resolvedPromptedACLRequests)
	if a != nil {
		m.headerHealth = a.HeaderHealth()
	}
}

// SetSize updates the terminal dimensions.
func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h
	m.tabBar.Width = w
	for _, screen := range m.screens() {
		routeMessage(screen, tea.WindowSizeMsg{Width: w, Height: m.contentHeight()})
	}

}

// SetRuntimesModel wires the runtimes tab.
func (m *Model) SetRuntimesModel(sm SubModel) { m.runtimesModel = sm }

// SetProxyMonModel wires the proxy monitor tab.
func (m *Model) SetProxyMonModel(sm SubModel) { m.proxyMonModel = sm }

// SetHistoryModel wires the combined request history.
func (m *Model) SetHistoryModel(sm SubModel) { m.historyModel = sm }

// SetSquidLogModel wires the squid logs tab.
func (m *Model) SetSquidLogModel(sm SubModel) { m.squidLogModel = sm }

// SetBridgeModel wires routes and execution logs.
func (m *Model) SetBridgeModel(sm SubModel) { m.bridgeModel = sm }

// SetRuntimeModel wires the runtime settings tab.
func (m *Model) SetRuntimeModel(sm SubModel) { m.runtimeModel = sm }

// SetPortForwardModel wires the port forwarding tab.
func (m *Model) SetPortForwardModel(sm SubModel) { m.portForwardModel = sm }

func (m *Model) SetProfilesModel(sm SubModel) {
	m.profilesModel, _ = sm.Update(tea.WindowSizeMsg{Width: m.width, Height: m.contentHeight()})
}

// SetLoadingModel wires the loading/startup screen sub-model.
func (m *Model) SetLoadingModel(sm SubModel) { m.loadingModel = sm }

// SetAlertPlayer wires the global proxy alert dependency.
func (m *Model) SetAlertPlayer(p AlertPlayer) { m.alertPlayer = p }

// SetActiveTab switches to the given tab (for tui-test --screen).
func (m *Model) SetActiveTab(tab theme.TabID) {
	m.activeTab = tab
	m.tabBar.SetActive(tab)
	m.forwardToActive(events.TabActivatedMsg{})
}

// SetOnShutdown sets the callback invoked when the user confirms exit.
func (m *Model) SetOnShutdown(fn func()) { m.onShutdown = fn }

// SetOnQuit sets the callback invoked for an immediate quit.
func (m *Model) SetOnQuit(fn func()) { m.onQuit = fn }

// ExitExpected reports whether the model has entered an explicit user-initiated
// quit or shutdown path. Callers can use this to distinguish a normal TUI exit
// from an unexpected program termination.
func (m *Model) ExitExpected() bool { return m.exitExpected }

// ExitReason describes why the TUI asked Bubble Tea to quit when the exit was
// not user-initiated. It is intentionally kept in the root model so main.go can
// log the reason without the TUI importing logging or OS-signal packages.
func (m *Model) ExitReason() string { return m.exitReason }

// activeSubModel returns the SubModel for the currently active tab,
// or nil if the tab has not been wired yet.
func (m *Model) activeSubModel() SubModel {
	switch m.activeTab {
	case theme.TabRuntimes:
		return m.runtimesModel
	case theme.TabMonitor:
		return m.proxyMonModel
	case theme.TabHistory:
		return m.historyModel
	case theme.TabSquidLogs:
		return m.squidLogModel
	case theme.TabBridge:
		return m.bridgeModel
	case theme.TabRuntime:
		return m.runtimeModel
	case theme.TabPortForward:
		return m.portForwardModel
	case theme.TabProfiles:
		return m.profilesModel
	}
	return nil
}

func (m *Model) screens() []*SubModel {
	return []*SubModel{&m.runtimesModel, &m.profilesModel, &m.proxyMonModel, &m.historyModel, &m.squidLogModel, &m.bridgeModel, &m.portForwardModel, &m.runtimeModel}
}

func routeMessage(screen *SubModel, msg tea.Msg) tea.Cmd {
	if *screen == nil {
		return nil
	}
	var cmd tea.Cmd
	*screen, cmd = (*screen).Update(msg)
	return cmd
}
