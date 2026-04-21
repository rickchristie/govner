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

// ContainerManager is the subset of app.App used by the containers tab
// for stop/restart actions. Defining a local interface keeps this package
// decoupled from the full App interface.
type ContainerManager interface {
	StopContainer(name string) error
	RestartContainer(name string) error
}

type actionState int

const (
	actionNone actionState = iota
	actionPending
	actionSuccess
	actionFailed
)

type containerActionResultMsg struct {
	Action string
	Name   string
	Err    error
}

// containerItem holds combined barrel info and optional resource stats for
// a single container row.
type containerItem struct {
	Name       string
	Status     string
	ShellCount int
	CPUPercent string
	MemUsage   string
	TmpUsage   string
}

// Model is the sub-model for the Containers tab. It shows a scrollable list of
// running containers with columns for status, shells, CPU, memory, and /tmp.
type Model struct {
	list       components.ScrollableList
	containers []containerItem
	expanded   bool
	manager    ContainerManager

	actionState actionState
	actionText  string
}

// New creates a new containers tab model.
func New(mgr ContainerManager) *Model {
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
	case events.ContainerStatsMsg:
		m.applyStats(msg.Stats)
		return m, nil
	case events.ContainerActionConfirmMsg:
		return m.handleConfirmedAction(msg)
	case containerActionResultMsg:
		m.handleActionResult(msg)
		return m, nil
	}
	return m, nil
}

// View satisfies SubModel. Renders the container list or empty state.
func (m *Model) View(width, height int) string {
	if len(m.containers) == 0 {
		return m.emptyState(width, height)
	}

	m.list.Width = width
	m.rebuildListItems()

	feedbackLines := 0
	if m.actionState != actionNone && m.actionText != "" {
		feedbackLines = 1
	}

	var sections []string
	header := renderHeader(width)
	sections = append(sections, header)
	divider := theme.DividerStyle.Render(strings.Repeat(theme.BorderH, width))
	sections = append(sections, divider)

	listHeight := height - 2 - feedbackLines
	if listHeight < 1 {
		listHeight = 1
	}
	m.list.Height = listHeight
	sections = append(sections, m.list.View(renderRow))

	if m.actionState != actionNone && m.actionText != "" {
		sections = append(sections, renderActionStatus(m.actionState, m.actionText, width))
	}

	if m.expanded {
		sel := m.list.Selected()
		if sel != nil {
			if ci, ok := sel.Data.(containerItem); ok {
				sections = append(sections, renderDetail(ci, width))
			}
		}
	}

	return strings.Join(sections, "\n")
}

// handleKey processes key events for the containers tab.
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
			if ci, ok := sel.Data.(containerItem); ok {
				return m, requestActionCmd("stop", ci.Name)
			}
		}
	case "r":
		if sel := m.list.Selected(); sel != nil {
			if ci, ok := sel.Data.(containerItem); ok {
				return m, requestActionCmd("restart", ci.Name)
			}
		}
	case "enter":
		// Detail pane disabled — info is already in the table columns.
	}
	return m, nil
}

func (m *Model) handleConfirmedAction(msg events.ContainerActionConfirmMsg) (theme.SubModel, tea.Cmd) {
	switch msg.Action {
	case "stop":
		m.markActionPending(msg.Name, "Stopping")
		return m, m.stopContainerCmd(msg.Name)
	case "restart":
		m.markActionPending(msg.Name, "Restarting")
		return m, m.restartContainerCmd(msg.Name)
	default:
		return m, nil
	}
}

// applyStats merges incoming container stats into the model.
func (m *Model) applyStats(stats []app.ContainerStat) {
	updated := make([]containerItem, 0, len(stats))
	for _, s := range stats {
		updated = append(updated, containerItem{
			Name:       s.Name,
			Status:     s.Status,
			ShellCount: s.ShellCount,
			CPUPercent: s.CPUPercent,
			MemUsage:   s.MemUsage,
			TmpUsage:   s.TmpUsage,
		})
	}

	sort.Slice(updated, func(i, j int) bool {
		if updated[i].Name == app.ContainerProxy {
			return true
		}
		if updated[j].Name == app.ContainerProxy {
			return false
		}
		return updated[i].Name < updated[j].Name
	})

	m.containers = updated
	m.rebuildListItems()
	if len(updated) == 0 {
		m.actionState = actionNone
		m.actionText = ""
	}
}

// rebuildListItems syncs the ScrollableList items from the containers slice.
func (m *Model) rebuildListItems() {
	items := make([]components.ListItem, len(m.containers))
	for i, c := range m.containers {
		items[i] = components.ListItem{ID: c.Name, Data: c}
	}
	m.list.SetItems(items)
}

// emptyState renders the centered empty message.
func (m *Model) emptyState(width, height int) string {
	icon := theme.BarrelEmoji
	msg := theme.EmptyStateStyle.Render("No containers running.")
	hint := theme.DimStyle.Render("Run ") +
		theme.BrandStyle.Render("cooper cli") +
		theme.DimStyle.Render(" to start a container.")

	content := lipgloss.JoinVertical(lipgloss.Center, icon, "", msg, "", hint)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

func (m *Model) markActionPending(name, verb string) {
	m.updateContainerStatus(name, verb+"...")
	m.actionState = actionPending
	m.actionText = verb + " " + name + "..."
}

func (m *Model) handleActionResult(msg containerActionResultMsg) {
	verbPast := map[string]string{"stop": "Stopped", "restart": "Restarted"}
	verbPresent := map[string]string{"stop": "Running", "restart": "Running"}

	if msg.Err != nil {
		m.updateContainerStatus(msg.Name, "Running")
		m.actionState = actionFailed
		m.actionText = msg.Err.Error()
		return
	}

	if msg.Action == "stop" {
		m.removeContainer(msg.Name)
	}
	if status, ok := verbPresent[msg.Action]; ok {
		m.updateContainerStatus(msg.Name, status)
	}
	m.actionState = actionSuccess
	if past, ok := verbPast[msg.Action]; ok {
		m.actionText = past + " " + msg.Name + "."
	}
}

func (m *Model) updateContainerStatus(name, status string) {
	for i := range m.containers {
		if m.containers[i].Name == name {
			m.containers[i].Status = status
			return
		}
	}
}

func (m *Model) removeContainer(name string) {
	filtered := m.containers[:0]
	for _, item := range m.containers {
		if item.Name != name {
			filtered = append(filtered, item)
		}
	}
	m.containers = filtered
	m.rebuildListItems()
}

// stopContainerCmd returns a tea.Cmd that stops a container by name.
func (m *Model) stopContainerCmd(name string) tea.Cmd {
	mgr := m.manager
	return func() tea.Msg {
		if mgr == nil {
			return containerActionResultMsg{Action: "stop", Name: name, Err: nil}
		}
		return containerActionResultMsg{Action: "stop", Name: name, Err: mgr.StopContainer(name)}
	}
}

// restartContainerCmd returns a tea.Cmd that restarts a container.
func (m *Model) restartContainerCmd(name string) tea.Cmd {
	mgr := m.manager
	return func() tea.Msg {
		if mgr == nil {
			return containerActionResultMsg{Action: "restart", Name: name, Err: nil}
		}
		return containerActionResultMsg{Action: "restart", Name: name, Err: mgr.RestartContainer(name)}
	}
}

func requestActionCmd(action, name string) tea.Cmd {
	return func() tea.Msg {
		return events.ContainerActionRequestMsg{Action: action, Name: name}
	}
}
