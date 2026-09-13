package containers

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/events"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// WorkloadManager is the subset of app.App used by the runtimes tab
// for stop/restart actions. Defining a local interface keeps this package
// decoupled from the full App interface.
type WorkloadManager interface {
	StopWorkload(name string) error
	RestartWorkload(name string) error
}

type actionState int

const (
	actionNone actionState = iota
	actionPending
	actionSuccess
	actionFailed
)

type workloadActionResultMsg struct {
	Action string
	Name   string
	Err    error
}

// workloadItem contains only presentation data for one runtime row.
type workloadItem struct {
	ID           string
	Kind         app.WorkloadKind
	Tool         string
	Workspace    string
	Profile      string
	Depth        int
	Status       string
	HealthReason string
	ShellCount   int
	CPUPercent   string
	MemUsage     string
	StorageUsage string
}

// Model is the sub-model for the Runtimes tab. It shows proxy, CLI, and VM
// workloads through one runtime-neutral application boundary.
type Model struct {
	list      components.ScrollableList
	workloads []workloadItem
	expanded  bool
	manager   WorkloadManager

	actionState actionState
	actionText  string
}

// New creates a new runtimes tab model.
func New(mgr WorkloadManager) *Model {
	return &Model{
		list:    components.NewScrollableList(0, 0),
		manager: mgr,
	}
}

// Init satisfies SubModel. No commands needed at init time; the root model
// drives container stat polling.
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update satisfies theme.SubModel.
func (m *Model) Update(msg tea.Msg) (theme.SubModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		m.list.HandleMouse(msg)
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case events.WorkloadStatsMsg:
		m.applyStats(msg.Stats)
		return m, nil
	case events.WorkloadActionConfirmMsg:
		return m.handleConfirmedAction(msg)
	case workloadActionResultMsg:
		m.handleActionResult(msg)
		return m, nil
	}
	return m, nil
}

// View satisfies SubModel. It renders the workload list or empty state.
func (m *Model) View(width, height int) string {
	if len(m.workloads) == 0 {
		return m.emptyState(width, height)
	}

	m.list.Width = width
	m.rebuildListItems()

	feedbackLines := 0
	if m.actionState != actionNone && m.actionText != "" {
		feedbackLines = 1
	}
	showDetail := m.expanded && height >= 14 && m.list.Selected() != nil
	detailLines := 0
	if showDetail {
		detailLines = 10
	}

	var sections []string
	header := renderHeader(width)
	sections = append(sections, header)
	divider := theme.DividerStyle.Render(strings.Repeat(theme.BorderH, width))
	sections = append(sections, divider)

	listHeight := height - 2 - feedbackLines - detailLines
	if listHeight < 1 {
		listHeight = 1
	}
	m.list.Height = listHeight
	sections = append(sections, m.list.View(renderRow))

	if m.actionState != actionNone && m.actionText != "" {
		sections = append(sections, renderActionStatus(m.actionState, m.actionText, width))
	}

	if showDetail {
		sel := m.list.Selected()
		if sel != nil {
			if ci, ok := sel.Data.(workloadItem); ok {
				sections = append(sections, renderDetail(ci, width))
			}
		}
	}

	return strings.Join(sections, "\n")
}

// handleKey processes key events for the runtimes tab.
func (m *Model) handleKey(msg tea.KeyMsg) (theme.SubModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.list.MoveUp()
		m.expanded = false
	case "down", "j":
		m.list.MoveDown()
		m.expanded = false
	case "s":
		if sel := m.list.Selected(); sel != nil {
			if ci, ok := sel.Data.(workloadItem); ok {
				return m, requestActionCmd("stop", ci.ID)
			}
		}
	case "r":
		if sel := m.list.Selected(); sel != nil {
			if ci, ok := sel.Data.(workloadItem); ok {
				return m, requestActionCmd("restart", ci.ID)
			}
		}
	case "enter":
		m.expanded = !m.expanded
	}
	return m, nil
}

func (m *Model) handleConfirmedAction(msg events.WorkloadActionConfirmMsg) (theme.SubModel, tea.Cmd) {
	switch msg.Action {
	case "stop":
		m.markActionPending(msg.Name, "Stopping")
		return m, m.stopWorkloadCmd(msg.Name)
	case "restart":
		m.markActionPending(msg.Name, "Restarting")
		return m, m.restartWorkloadCmd(msg.Name)
	default:
		return m, nil
	}
}

// applyStats replaces the screen snapshot while it preserves list selection.
func (m *Model) applyStats(stats []app.WorkloadStat) {
	updated := make([]workloadItem, 0, len(stats))
	for _, s := range stats {
		updated = append(updated, workloadItem{
			ID: s.ID, Kind: s.Kind, Tool: s.Tool, Workspace: s.Workspace, Depth: s.Depth, Profile: s.Profile,
			Status: s.Status, HealthReason: s.HealthReason, ShellCount: s.ShellCount,
			CPUPercent: s.CPUPercent, MemUsage: s.MemUsage, StorageUsage: s.StorageUsage,
		})
	}

	sort.Slice(updated, func(i, j int) bool {
		if updated[i].Kind != updated[j].Kind {
			return workloadKindOrder(updated[i].Kind) < workloadKindOrder(updated[j].Kind)
		}
		return updated[i].ID < updated[j].ID
	})

	m.workloads = updated
	m.rebuildListItems()
	if len(updated) == 0 {
		m.actionState = actionNone
		m.actionText = ""
	}
}

func workloadKindOrder(kind app.WorkloadKind) int {
	switch kind {
	case app.WorkloadProxy:
		return 0
	case app.WorkloadCLI:
		return 1
	case app.WorkloadVM:
		return 2
	default:
		return 3
	}
}

// rebuildListItems syncs the list from the current workload snapshot.
func (m *Model) rebuildListItems() {
	items := make([]components.ListItem, len(m.workloads))
	for i, workload := range m.workloads {
		items[i] = components.ListItem{ID: workload.ID, Data: workload}
	}
	m.list.SetItems(items)
}

// emptyState renders the centered empty message.
func (m *Model) emptyState(width, height int) string {
	icon := theme.BarrelEmoji
	msg := theme.EmptyStateStyle.Render("No runtimes are running.")
	hint := theme.DimStyle.Render("Run ") +
		theme.BrandStyle.Render("cooper cli") +
		theme.DimStyle.Render(" or ") +
		theme.BrandStyle.Render("cooper vm") +
		theme.DimStyle.Render(" to start one.")

	content := lipgloss.JoinVertical(lipgloss.Center, icon, "", msg, "", hint)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

func (m *Model) markActionPending(name, verb string) {
	m.updateWorkloadStatus(name, verb+"...")
	m.actionState = actionPending
	m.actionText = verb + " " + name + "..."
}

func (m *Model) handleActionResult(msg workloadActionResultMsg) {
	verbPast := map[string]string{"stop": "Stopped", "restart": "Restarted"}
	verbPresent := map[string]string{"stop": "Running", "restart": "Running"}

	if msg.Err != nil {
		m.updateWorkloadStatus(msg.Name, "Running")
		m.actionState = actionFailed
		m.actionText = msg.Err.Error()
		return
	}

	if msg.Action == "stop" {
		m.removeWorkload(msg.Name)
	}
	if status, ok := verbPresent[msg.Action]; ok {
		m.updateWorkloadStatus(msg.Name, status)
	}
	m.actionState = actionSuccess
	if past, ok := verbPast[msg.Action]; ok {
		m.actionText = past + " " + msg.Name + "."
	}
}

func (m *Model) updateWorkloadStatus(name, status string) {
	for i := range m.workloads {
		if m.workloads[i].ID == name {
			m.workloads[i].Status = status
			return
		}
	}
}

func (m *Model) removeWorkload(name string) {
	filtered := m.workloads[:0]
	for _, item := range m.workloads {
		if item.ID != name {
			filtered = append(filtered, item)
		}
	}
	m.workloads = filtered
	m.rebuildListItems()
}

// stopWorkloadCmd returns a command that stops one workload by ID.
func (m *Model) stopWorkloadCmd(name string) tea.Cmd {
	mgr := m.manager
	return func() tea.Msg {
		if mgr == nil {
			return workloadActionResultMsg{Action: "stop", Name: name, Err: nil}
		}
		return workloadActionResultMsg{Action: "stop", Name: name, Err: mgr.StopWorkload(name)}
	}
}

// restartWorkloadCmd returns a command that restarts one workload.
func (m *Model) restartWorkloadCmd(name string) tea.Cmd {
	mgr := m.manager
	return func() tea.Msg {
		if mgr == nil {
			return workloadActionResultMsg{Action: "restart", Name: name, Err: nil}
		}
		return workloadActionResultMsg{Action: "restart", Name: name, Err: mgr.RestartWorkload(name)}
	}
}

func requestActionCmd(action, name string) tea.Cmd {
	return func() tea.Msg {
		return events.WorkloadActionRequestMsg{Action: action, Name: name}
	}
}
