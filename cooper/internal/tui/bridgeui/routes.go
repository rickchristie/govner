package bridgeui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/tableutil"
	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// RoutesChangedMsg is sent when routes are modified (add/edit/delete).
// The root model should use this to persist the change and update the
// bridge server.
type RoutesChangedMsg struct {
	Routes []config.BridgeRoute
}

// routeEditMode tracks the editing state for routes.
type routeEditMode int

const (
	routeNone    routeEditMode = iota
	routeAdding                // Adding a new route.
	routeEditing               // Editing an existing route.
	routeDeleting              // Delete confirmation.
)

// routeField identifies which field is active during editing.
type routeField int

const (
	fieldAPIPath routeField = iota
	fieldScriptPath
)

// RoutesModel is the sub-model for the Bridge Routes tab.
type RoutesModel struct {
	routes []config.BridgeRoute
	list   components.ScrollableList

	// Editing state.
	editMode    routeEditMode
	editField   routeField
	editIdx     int    // Index of route being edited (-1 for new).
	editAPI     string // Buffer for API path input.
	editScript  string // Buffer for script path input.
}

// NewRoutesModel creates a new bridge routes sub-model.
func NewRoutesModel() *RoutesModel {
	return &RoutesModel{
		list:    components.NewScrollableList(10, 80),
		editIdx: -1,
	}
}

// SetRoutes replaces the route list.
func (m *RoutesModel) SetRoutes(routes []config.BridgeRoute) {
	m.routes = make([]config.BridgeRoute, len(routes))
	copy(m.routes, routes)
	m.syncList()
}

// Routes returns a copy of the current routes.
func (m *RoutesModel) Routes() []config.BridgeRoute {
	out := make([]config.BridgeRoute, len(m.routes))
	copy(out, m.routes)
	return out
}

// syncList rebuilds the ScrollableList items from m.routes.
func (m *RoutesModel) syncList() {
	items := make([]components.ListItem, len(m.routes))
	for i, r := range m.routes {
		items[i] = components.ListItem{
			ID:   fmt.Sprintf("route-%d", i),
			Data: r,
		}
	}
	m.list.SetItems(items)
}

// Init satisfies the SubModel interface.
func (m *RoutesModel) Init() tea.Cmd {
	return nil
}

// Update satisfies SubModel.
func (m *RoutesModel) Update(msg tea.Msg) (theme.SubModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if m.editMode == routeNone {
			m.list.HandleMouse(msg)
		}
		return m, nil
	case tea.KeyMsg:
		if m.editMode == routeDeleting {
			return m.handleDeleteConfirm(msg)
		}
		if m.editMode == routeAdding || m.editMode == routeEditing {
			return m.handleEditInput(msg)
		}
		return m.handleNormalKey(msg)
	}
	return m, nil
}

// handleNormalKey processes keys when no edit/delete modal is active.
func (m *RoutesModel) handleNormalKey(msg tea.KeyMsg) (theme.SubModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.list.MoveUp()
	case "down", "j":
		m.list.MoveDown()
	case "n":
		// Add new route.
		m.editMode = routeAdding
		m.editIdx = -1
		m.editField = fieldAPIPath
		m.editAPI = "/"
		m.editScript = ""
	case "enter", "e":
		// Edit selected route.
		if sel := m.list.Selected(); sel != nil {
			if r, ok := sel.Data.(config.BridgeRoute); ok {
				m.editMode = routeEditing
				m.editIdx = m.list.SelectedIdx
				m.editField = fieldAPIPath
				m.editAPI = r.APIPath
				m.editScript = r.ScriptPath
			}
		}
	case "x":
		// Delete selected route.
		if len(m.routes) > 0 && m.list.Selected() != nil {
			m.editMode = routeDeleting
		}
	}
	return m, nil
}

// handleEditInput processes keys during route add/edit.
func (m *RoutesModel) handleEditInput(msg tea.KeyMsg) (theme.SubModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.editMode = routeNone
		return m, nil
	case "up", "down":
		// Toggle between fields.
		if m.editField == fieldAPIPath {
			m.editField = fieldScriptPath
		} else {
			m.editField = fieldAPIPath
		}
		return m, nil
	case "enter":
		// Save the route.
		return m.saveRoute()
	case "backspace":
		if m.editField == fieldAPIPath && len(m.editAPI) > 0 {
			m.editAPI = m.editAPI[:len(m.editAPI)-1]
		} else if m.editField == fieldScriptPath && len(m.editScript) > 0 {
			m.editScript = m.editScript[:len(m.editScript)-1]
		}
		return m, nil
	default:
		// Append character to active field.
		r := msg.String()
		if len(r) == 1 {
			if m.editField == fieldAPIPath {
				m.editAPI += r
			} else {
				m.editScript += r
			}
		}
	}
	return m, nil
}

// saveRoute commits the edit buffer to the route list.
func (m *RoutesModel) saveRoute() (theme.SubModel, tea.Cmd) {
	// Validate: API path must start with /, script path must be non-empty.
	api := m.editAPI
	if !strings.HasPrefix(api, "/") {
		api = "/" + api
	}
	if m.editScript == "" {
		// Do not save empty script.
		return m, nil
	}

	route := config.BridgeRoute{
		APIPath:    api,
		ScriptPath: m.editScript,
	}

	if m.editMode == routeAdding {
		m.routes = append(m.routes, route)
	} else if m.editMode == routeEditing && m.editIdx >= 0 && m.editIdx < len(m.routes) {
		m.routes[m.editIdx] = route
	}

	m.editMode = routeNone
	m.syncList()
	return m, func() tea.Msg { return RoutesChangedMsg{Routes: m.Routes()} }
}

// handleDeleteConfirm processes keys during delete confirmation.
func (m *RoutesModel) handleDeleteConfirm(msg tea.KeyMsg) (theme.SubModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		idx := m.list.SelectedIdx
		if idx >= 0 && idx < len(m.routes) {
			m.routes = append(m.routes[:idx], m.routes[idx+1:]...)
			m.syncList()
		}
		m.editMode = routeNone
		return m, func() tea.Msg { return RoutesChangedMsg{Routes: m.Routes()} }
	case "esc":
		m.editMode = routeNone
	}
	return m, nil
}

// View satisfies the SubModel interface.
func (m *RoutesModel) View(width, height int) string {
	if m.editMode == routeAdding || m.editMode == routeEditing {
		bg := m.renderRouteList(width, height)
		modal := m.renderEditModal(width, height)
		dimmed := components.DimContent(bg)
		return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top, dimmed) +
			"\r" + modal
	}
	if m.editMode == routeDeleting {
		bg := m.renderRouteList(width, height)
		modal := m.renderDeleteModal(width, height)
		dimmed := components.DimContent(bg)
		return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top, dimmed) +
			"\r" + modal
	}
	return m.renderRouteList(width, height)
}

// renderRouteList renders the main route table.
func (m *RoutesModel) renderRouteList(width, height int) string {
	if len(m.routes) == 0 {
		return renderRoutesEmpty(width, height)
	}

	m.list.Width = width - 2
	listHeight := height - 8 // Header, divider, info box.
	if listHeight < 1 {
		listHeight = 1
	}
	m.list.Height = listHeight

	// Build a table to compute column widths for alignment.
	tbl := tableutil.NewTable("API PATH", "SCRIPT")
	tbl.SetHeaderStyle(theme.ColorDusty, true)
	for _, r := range m.routes {
		api := lipgloss.NewStyle().Foreground(theme.ColorParchment).Render(r.APIPath)
		script := lipgloss.NewStyle().Foreground(theme.ColorVerdigris).Render(
			truncate(r.ScriptPath, 38),
		)
		tbl.AddRow(api, script)
	}

	var b strings.Builder

	// Column header using the table renderer for proper alignment.
	b.WriteString(" " + tbl.RenderHeader() + "\n")
	b.WriteString(theme.DividerStyle.Render(" "+strings.Repeat(theme.BorderH, width-2)) + "\n")

	// Compute column widths so the renderRouteRow callback can use them.
	colWidths, _ := tbl.RenderRows(0)

	// Route list.
	listView := m.list.View(func(item components.ListItem, selected bool, w int) string {
		r, ok := item.Data.(config.BridgeRoute)
		if !ok {
			return ""
		}
		return renderRouteRow(r, selected, w, colWidths)
	})
	b.WriteString(listView)

	// Info box at bottom.
	b.WriteString("\n\n")
	b.WriteString(renderRouteInfoBox(width - 2))

	return b.String()
}

func renderRouteRow(r config.BridgeRoute, selected bool, width int, colWidths []int) string {
	apiWidth := 22
	scriptWidth := 40
	if len(colWidths) >= 2 {
		apiWidth = colWidths[0]
		scriptWidth = colWidths[1]
	}

	api := lipgloss.NewStyle().Foreground(theme.ColorParchment).Width(apiWidth).Render(r.APIPath)
	script := lipgloss.NewStyle().Foreground(theme.ColorVerdigris).Width(scriptWidth).Render(
		truncate(r.ScriptPath, scriptWidth-2),
	)
	row := api + "  " + script

	if selected {
		arrow := theme.SelectionArrowStyle.Render(theme.IconArrowRight)
		return arrow + " " + row
	}
	return "  " + row
}

func renderRouteInfoBox(width int) string {
	infoStyle := theme.InfoTextStyle
	emphStyle := theme.InfoEmphasisStyle

	lines := []string{
		infoStyle.Render("  Best practice: Bridge scripts should take ") + emphStyle.Render("NO input") + infoStyle.Render("."),
		infoStyle.Render("  If scripts take input, they must validate religiously."),
		infoStyle.Render("  Scripts run on the ") + emphStyle.Render("HOST") + infoStyle.Render(" machine with your user's permissions."),
	}

	box := theme.InfoBoxStyle.Width(width).Render(strings.Join(lines, "\n"))
	return box
}

func renderRoutesEmpty(width, height int) string {
	content := lipgloss.JoinVertical(lipgloss.Center,
		"",
		"",
		theme.EmptyStateStyle.Render(theme.IconPlug),
		"",
		theme.EmptyStateStyle.Render("No bridge routes configured."),
		"",
		theme.EmptyStateStyle.Render("Press  n  to add your first route."),
		theme.EmptyStateStyle.Render("Routes let AI tools execute host scripts"),
		theme.EmptyStateStyle.Render("via the bridge API."),
	)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

// renderEditModal renders the add/edit route modal overlay.
func (m *RoutesModel) renderEditModal(width, height int) string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(theme.ColorAmber).
		Padding(1, 3).
		Width(50)

	titleStyle := lipgloss.NewStyle().Foreground(theme.ColorParchment).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(theme.ColorLinen)
	hintStyle := lipgloss.NewStyle().Foreground(theme.ColorDusty)
	cursor := lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render("_")

	var titleText string
	if m.editMode == routeAdding {
		titleText = theme.IconPlug + " Add Bridge Route"
	} else {
		titleText = theme.IconPlug + " Edit Bridge Route"
	}

	makeInput := func(value string, active bool) string {
		borderColor := theme.ColorOakLight
		if active {
			borderColor = theme.ColorAmber
		}
		display := value
		if active {
			display += cursor
		}
		return lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(borderColor).
			Foreground(theme.ColorParchment).
			Width(30).
			Render(display)
	}

	var inner string
	inner += titleStyle.Render(titleText) + "\n\n"

	inner += labelStyle.Render("API Path:") + "\n"
	inner += makeInput(m.editAPI, m.editField == fieldAPIPath) + "\n\n"

	inner += labelStyle.Render("Script Path:") + "\n"
	inner += makeInput(m.editScript, m.editField == fieldScriptPath) + "\n\n"

	inner += hintStyle.Render("Tab/Up/Down: switch fields") + "\n\n"
	inner += lipgloss.NewStyle().Foreground(theme.ColorProof).Bold(true).Render("[Enter "+theme.IconCheck+" Save]") +
		"    " + lipgloss.NewStyle().Foreground(theme.ColorDusty).Render("[Esc Cancel]")

	modal := boxStyle.Render(inner)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

// renderDeleteModal renders the delete confirmation modal.
func (m *RoutesModel) renderDeleteModal(width, height int) string {
	idx := m.list.SelectedIdx
	var routePath, scriptPath string
	if idx >= 0 && idx < len(m.routes) {
		routePath = m.routes[idx].APIPath
		scriptPath = m.routes[idx].ScriptPath
	}

	title := theme.ModalTitleStyle.Render(theme.IconPlug + " Delete Route?")
	divider := theme.ModalDividerStyle.Render(strings.Repeat(theme.BorderH, 38))

	bodyLines := []string{
		lipgloss.NewStyle().Foreground(theme.ColorLinen).Render("Route: " + routePath),
		lipgloss.NewStyle().Foreground(theme.ColorLinen).Render("Script: " + scriptPath),
	}
	body := theme.ModalBodyStyle.Render(strings.Join(bodyLines, "\n"))

	confirm := theme.ModalConfirmStyle.Render("[Enter " + theme.IconCheck + " Delete]")
	cancel := theme.ModalCancelStyle.Render("[Esc Cancel]")
	buttons := lipgloss.NewStyle().Width(44).Align(lipgloss.Center).Render(confirm + "    " + cancel)

	inner := lipgloss.JoinVertical(lipgloss.Center,
		"",
		title,
		"",
		divider,
		"",
		body,
		"",
		divider,
		"",
		buttons,
		"",
	)

	box := theme.ModalBorderStyle.Render(inner)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

func padRight(s string, length int) string {
	if len(s) >= length {
		return s[:length]
	}
	return s + strings.Repeat(" ", length-len(s))
}
