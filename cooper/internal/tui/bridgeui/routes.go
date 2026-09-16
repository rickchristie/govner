package bridgeui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/charmbracelet/x/ansi"
	"github.com/rickchristie/govner/cooper/internal/clipboard"
	"github.com/rickchristie/govner/cooper/internal/config"
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
	routeNone     routeEditMode = iota
	routeAdding                 // Adding a new route.
	routeEditing                // Editing an existing route.
	routeDeleting               // Delete confirmation.
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
	editMode   routeEditMode
	editField  routeField
	editIdx    int    // Index of route being edited (-1 for new).
	editAPI    string // Buffer for API path input.
	editScript string // Buffer for script path input.
	editErr    string // Validation error shown in edit modal.
}

// IsEditing returns true when the routes model is in an add/edit/delete modal
// that consumes character key input.
func (m *RoutesModel) IsEditing() bool {
	return m.editMode != routeNone
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
	case tea.WindowSizeMsg:
		m.list.Width, m.list.Height = msg.Width, max(1, msg.Height-4)
		m.list.ClampScroll()
	case tea.MouseMsg:
		if m.editMode == routeNone {
			if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress && msg.Y >= 2 && msg.Y < m.list.Height+2 {
				index := m.list.ScrollOffset + msg.Y - 2
				if index < len(m.routes) {
					m.list.SelectedIdx = index
				}
			} else {
				m.list.HandleMouse(msg)
			}
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
		m.editErr = ""
	case "enter", "e":
		// Edit selected route.
		if sel := m.list.Selected(); sel != nil {
			if r, ok := sel.Data.(config.BridgeRoute); ok {
				m.editMode = routeEditing
				m.editIdx = m.list.SelectedIdx
				m.editField = fieldAPIPath
				m.editAPI = r.APIPath
				m.editScript = r.ScriptPath
				m.editErr = ""
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
	case "up", "down", "tab", "shift+tab":
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
		m.editErr = ""
		if m.editField == fieldAPIPath && len(m.editAPI) > 0 {
			m.editAPI = components.TrimLastRune(m.editAPI)
		} else if m.editField == fieldScriptPath && len(m.editScript) > 0 {
			m.editScript = components.TrimLastRune(m.editScript)
		}
		return m, nil
	default:
		if text := components.TextEntryFromKeyMsg(msg, nil); text != "" {
			m.editErr = ""
			if m.editField == fieldAPIPath {
				m.editAPI += text
			} else {
				m.editScript += text
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
	// Reject paths under the reserved clipboard namespace.
	if clipboard.IsReservedPath(api) {
		m.editErr = "reserved path: /clipboard/* is used by clipboard-bridge"
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
		return m.renderEditModal(width, height)
	}
	if m.editMode == routeDeleting {
		return m.renderDeleteModal(width, height)
	}
	frame := components.FixedFrame{Width: width, Height: height, Header: theme.PaneLabelStyle.Render(" API PATH → HOST SCRIPT"), Footer: theme.HelpDescStyle.Render("n New  Enter Edit  x Delete")}
	if len(m.routes) == 0 {
		return frame.View("No routes. Press n to add a host script.")
	}
	list := m.list
	list.Width, list.Height = width, frame.BodyHeight()
	list.ClampScroll()
	return frame.View(list.View(func(item components.ListItem, selected bool, w int) string {
		route := item.Data.(config.BridgeRoute)
		marker, style := "  ", theme.RowNormalStyle
		if selected {
			marker, style = "▶ ", theme.RowSelectedStyle
		}
		return style.Width(w).Render(ansi.Truncate(marker+route.APIPath+" → "+route.ScriptPath, w, "…"))
	}))
}

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
		// Keep the end and the cursor visible without adding input rows.
		if inputWidth := ansi.StringWidth(display); inputWidth > 30 {
			display = ansi.TruncateLeft(display, inputWidth-29, "…")
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

	if m.editErr != "" {
		inner += theme.ErrorStyle.Render(m.editErr) + "\n\n"
	}

	inner += hintStyle.Render("Tab/Up/Down: switch fields") + "\n\n"
	inner += lipgloss.NewStyle().Foreground(theme.ColorProof).Bold(true).Render("[Enter "+theme.IconCheck+" Save]") +
		"    " + lipgloss.NewStyle().Foreground(theme.ColorDusty).Render("[Esc Cancel]")

	modal := boxStyle.Render(inner)
	if lipgloss.Height(modal) > height {
		// Drop blank rows so validation and Save fit on a small terminal.
		modal = boxStyle.Padding(0, 3).Render(strings.ReplaceAll(inner, "\n\n", "\n"))
	}
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
