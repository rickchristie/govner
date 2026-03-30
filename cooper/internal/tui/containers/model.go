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

// containerItem holds combined barrel info and optional resource stats for
// a single container row.
type containerItem struct {
	Name       string
	Status     string
	CPUPercent string
	MemUsage   string
}

// Model is the sub-model for the Containers tab. It shows a scrollable
// list of running containers with columns: Name, Status, CPU%, Memory.
type Model struct {
	list       components.ScrollableList
	containers []containerItem
	expanded   bool // whether the detail pane is shown for selected
	manager    ContainerManager
}

// New creates a new containers tab model.
func New(mgr ContainerManager) *Model {
	return &Model{
		list:    components.NewScrollableList(0, 0),
		manager: mgr,
	}
}

// Init satisfies SubModel. No commands needed at init time; the root
// model drives container stat polling.
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
	}
	return m, nil
}

// View satisfies SubModel. Renders the container list or empty state.
func (m *Model) View(width, height int) string {
	if len(m.containers) == 0 {
		return m.emptyState(width, height)
	}

	m.list.Width = width
	m.list.Height = height
	m.rebuildListItems()

	var sections []string

	// Column header.
	header := renderHeader(width)
	sections = append(sections, header)

	// Divider under header.
	divider := theme.DividerStyle.Render(strings.Repeat(theme.BorderH, width))
	sections = append(sections, divider)

	// List content (height minus header and divider).
	listHeight := height - 2
	if listHeight < 1 {
		listHeight = 1
	}
	m.list.Height = listHeight
	listView := m.list.View(renderRow)
	sections = append(sections, listView)

	// If expanded, show detail for selected container below the list.
	if m.expanded {
		sel := m.list.Selected()
		if sel != nil {
			if ci, ok := sel.Data.(containerItem); ok {
				detail := renderDetail(ci, width)
				sections = append(sections, detail)
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
		// Stop selected container.
		if sel := m.list.Selected(); sel != nil {
			if ci, ok := sel.Data.(containerItem); ok {
				return m, m.stopContainerCmd(ci.Name)
			}
		}
	case "r":
		// Restart selected container.
		if sel := m.list.Selected(); sel != nil {
			if ci, ok := sel.Data.(containerItem); ok {
				return m, m.restartContainerCmd(ci.Name)
			}
		}
	case "enter":
		m.expanded = !m.expanded
	}
	return m, nil
}

// applyStats merges incoming container stats into the model. Containers
// that appear in stats but not in the current list are added; containers
// no longer present are removed.
func (m *Model) applyStats(stats []app.ContainerStat) {
	// Rebuild container list from stats.
	var updated []containerItem
	for _, s := range stats {
		ci := containerItem{
			Name:       s.Name,
			Status:     "running",
			CPUPercent: s.CPUPercent,
			MemUsage:   s.MemUsage,
		}
		updated = append(updated, ci)
	}

	// Sort: proxy first, then alphabetically.
	sort.Slice(updated, func(i, j int) bool {
		// cooper-proxy always first.
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
}

// rebuildListItems syncs the ScrollableList items from the containers slice.
func (m *Model) rebuildListItems() {
	items := make([]components.ListItem, len(m.containers))
	for i, c := range m.containers {
		items[i] = components.ListItem{
			ID:   c.Name,
			Data: c,
		}
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

// ----- Commands -----

// stopContainerCmd returns a tea.Cmd that stops a container by name.
func (m *Model) stopContainerCmd(name string) tea.Cmd {
	mgr := m.manager
	return func() tea.Msg {
		if mgr != nil {
			_ = mgr.StopContainer(name)
		}
		return nil
	}
}

// restartContainerCmd returns a tea.Cmd that restarts a container.
// Uses docker restart to preserve the container; the next stats poll
// will pick up the updated state.
func (m *Model) restartContainerCmd(name string) tea.Cmd {
	mgr := m.manager
	return func() tea.Msg {
		if mgr != nil {
			_ = mgr.RestartContainer(name)
		}
		return nil
	}
}
