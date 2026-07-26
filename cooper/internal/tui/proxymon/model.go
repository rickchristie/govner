package proxymon

import (
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/events"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// ACLApprover is the subset of app.App used by the proxy monitor for
// approve/deny actions and listing pending requests. Defining a local
// interface keeps this package decoupled from the full App interface.
type ACLApprover interface {
	ApproveRequest(id string)
	DenyRequest(id string)
	PendingRequests() []*app.PendingRequest
	AllowDomainForSession(domain string) (string, error)
	RevokeDomainForSession(domain string) bool
	IsDomainAllowedForSession(domain string) bool
	SessionAllowedDomains() []string
}

type sessionDialog uint8

const (
	sessionDialogNone sessionDialog = iota
	sessionDialogAllow
	sessionDialogManage
)

// Model is the sub-model for the Proxy Monitor tab. It shows a two-pane
// layout: left pane is a scrollable list of pending requests with countdown
// timer bars, right pane shows detail for the selected request.
type Model struct {
	list        components.ScrollableList
	pending     []*app.PendingRequest
	approver    ACLApprover
	timeout     time.Duration
	sessionList components.ScrollableList

	dialog               sessionDialog
	allowModal           *components.Modal
	pendingSessionDomain string
	sessionDomains       []string
	sessionMessage       string
	sessionMessageUntil  time.Time
	sessionError         string
}

// New creates a new proxy monitor tab model. The approver is used to
// approve/deny requests. timeout is the per-request approval window.
func New(approver ACLApprover, timeout time.Duration) *Model {
	m := &Model{
		list:        components.NewScrollableList(0, 0),
		approver:    approver,
		timeout:     timeout,
		sessionList: components.NewScrollableList(0, 0),
	}
	m.refreshSessionDomains()
	return m
}

// SetTimeout updates the per-request approval window at runtime.
func (m *Model) SetTimeout(d time.Duration) {
	m.timeout = d
}

// Init satisfies SubModel. No commands needed at init time.
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update satisfies theme.SubModel.
func (m *Model) Update(msg tea.Msg) (theme.SubModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if m.dialog == sessionDialogManage {
			m.sessionList.HandleMouse(msg)
		} else if m.dialog == sessionDialogNone {
			m.list.HandleMouse(msg)
		}
		return m, nil
	case tea.KeyMsg:
		if m.dialog != sessionDialogNone {
			return m.handleDialogKey(msg)
		}
		return m.handleKey(msg)
	case events.ACLRequestMsg:
		if m.approver != nil && m.approver.IsDomainAllowedForSession(msg.Request.Domain) {
			// A request can already be queued in Bubble Tea when the user
			// enables session access. The listener has resolved it; do not
			// resurrect a stale permission prompt.
			return m, nil
		}
		m.addRequest(msg.Request)
		return m, nil
	case events.ACLDecisionMsg:
		m.removeRequest(msg.Event.Request.ID)
		return m, nil
	case events.AnimTickMsg:
		m.pruneExpired()
		if !m.sessionMessageUntil.IsZero() && time.Now().After(m.sessionMessageUntil) {
			m.sessionMessage = ""
			m.sessionMessageUntil = time.Time{}
		}
		return m, nil
	}
	return m, nil
}

// ModalActive lets the root shell give this screen exclusive keyboard
// ownership while one of its dialogs is open.
func (m *Model) ModalActive() bool {
	return m.dialog != sessionDialogNone
}

// View satisfies SubModel. Renders the two-pane layout or empty state.
func (m *Model) View(width, height int) string {
	// Sort pending by time remaining (most urgent first).
	m.sortByUrgency()
	m.rebuildListItems()

	// Reserve a compact two-row session-access rail above the existing panes.
	paneHeight := height - 2
	if paneHeight < 3 {
		paneHeight = 3
	}

	// Two-pane split: 40% left, 60% right.
	leftWidth := int(float64(width) * 0.40)
	rightWidth := width - leftWidth - 1 // -1 for divider
	if leftWidth < 10 {
		leftWidth = 10
	}
	if rightWidth < 10 {
		rightWidth = 10
	}

	// Pane headers.
	leftHeader := lipgloss.NewStyle().
		Foreground(theme.ColorLinen).
		Bold(true).
		Width(leftWidth).
		Render(" Pending Requests")
	rightHeader := lipgloss.NewStyle().
		Foreground(theme.ColorLinen).
		Bold(true).
		Width(rightWidth).
		Render(" Request Detail")

	divChar := theme.BorderV
	headerDivider := theme.DividerStyle.Render(divChar)

	headerRow := leftHeader + headerDivider + rightHeader

	// Horizontal divider under headers.
	hDivLeft := theme.DividerStyle.Render(
		repeatToWidth(theme.BorderH, leftWidth))
	hDivRight := theme.DividerStyle.Render(
		repeatToWidth(theme.BorderH, rightWidth))
	hDivider := hDivLeft + theme.DividerStyle.Render(theme.BorderCross) + hDivRight

	// Content height: pane height minus header row and divider row.
	contentHeight := paneHeight - 2
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Left pane: pending list.
	leftPane := renderPendingList(m, leftWidth, contentHeight)

	// Right pane: detail of selected request.
	rightPane := renderDetailPane(m, rightWidth, contentHeight)

	// Build vertical divider for content area.
	leftLines := splitLines(leftPane, contentHeight)
	rightLines := splitLines(rightPane, contentHeight)

	var contentRows []string
	for i := 0; i < contentHeight; i++ {
		left := ""
		if i < len(leftLines) {
			left = leftLines[i]
		}
		right := ""
		if i < len(rightLines) {
			right = rightLines[i]
		}

		// Pad left pane to exact width.
		left = padToWidth(left, leftWidth)
		right = padToWidth(right, rightWidth)

		row := left + theme.DividerStyle.Render(divChar) + right
		contentRows = append(contentRows, row)
	}

	content := lipgloss.JoinVertical(lipgloss.Left, contentRows...)
	base := headerRow + "\n" + hDivider + "\n" + content
	return m.renderWithSessionAccess(base, width, height)
}

// handleKey processes key events for the proxy monitor tab.
func (m *Model) handleKey(msg tea.KeyMsg) (theme.SubModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.list.MoveUp()
	case "down", "j":
		m.list.MoveDown()
	case "a", "enter":
		// Approve selected request.
		if sel := m.list.Selected(); sel != nil {
			if pr, ok := sel.Data.(*app.PendingRequest); ok {
				if m.approver != nil {
					m.approver.ApproveRequest(pr.Request.ID)
				}
				m.removeRequest(pr.Request.ID)
			}
		}
	case "d":
		// Deny selected request.
		if sel := m.list.Selected(); sel != nil {
			if pr, ok := sel.Data.(*app.PendingRequest); ok {
				if m.approver != nil {
					m.approver.DenyRequest(pr.Request.ID)
				}
				m.removeRequest(pr.Request.ID)
			}
		}
	case "A":
		// Approve all pending requests.
		for _, pr := range m.pending {
			if m.approver != nil {
				m.approver.ApproveRequest(pr.Request.ID)
			}
		}
		m.pending = nil
		m.rebuildListItems()
	case "w":
		m.openSessionAllowDialog()
	case "s":
		m.openSessionManager()
	}
	return m, nil
}

func (m *Model) handleDialogKey(msg tea.KeyMsg) (theme.SubModel, tea.Cmd) {
	switch m.dialog {
	case sessionDialogAllow:
		switch msg.String() {
		case "esc":
			m.closeSessionDialog()
		case "up", "down", "left", "right", "tab", "shift+tab":
			if m.allowModal != nil {
				m.allowModal.FocusConfirm = !m.allowModal.FocusConfirm
			}
		case "enter":
			if m.allowModal == nil || !m.allowModal.FocusConfirm {
				m.closeSessionDialog()
				return m, nil
			}
			m.allowPendingDomainForSession()
		}

	case sessionDialogManage:
		switch msg.String() {
		case "esc", "s":
			m.closeSessionDialog()
		case "up", "k":
			m.sessionList.MoveUp()
		case "down", "j":
			m.sessionList.MoveDown()
		case "r":
			m.revokeSelectedSessionDomain()
		}
	}
	return m, nil
}

func (m *Model) openSessionAllowDialog() {
	selected := m.list.Selected()
	if selected == nil {
		m.sessionError = "Select a pending request before allowing session access."
		return
	}
	request, ok := selected.Data.(*app.PendingRequest)
	if !ok {
		return
	}

	m.pendingSessionDomain = request.Request.Domain
	body := "Automatically approve this exact hostname\n" +
		"for every barrel until Cooper exits?\n\n" +
		theme.DomainStyle.Bold(true).Render(request.Request.Domain) + "\n\n" +
		"No proxy settings will be changed."
	modal := components.NewModal(
		theme.ModalSessionAllowDomain,
		theme.IconShield+" Allow for This Session?",
		body,
		"Allow Exact Host",
		"Cancel",
	)
	m.allowModal = &modal
	m.dialog = sessionDialogAllow
	m.sessionError = ""
}

func (m *Model) allowPendingDomainForSession() {
	domain := m.pendingSessionDomain
	m.closeSessionDialog()
	if m.approver == nil {
		m.sessionError = "Session access is unavailable."
		return
	}

	normalized, err := m.approver.AllowDomainForSession(domain)
	if err != nil {
		m.sessionError = err.Error()
		return
	}
	m.sessionError = ""
	m.sessionMessage = normalized + " is allowed until Cooper exits."
	m.sessionMessageUntil = time.Now().Add(3 * time.Second)
	m.refreshSessionDomains()
	m.removeRequestsForDomain(normalized)
}

func (m *Model) openSessionManager() {
	m.refreshSessionDomains()
	m.dialog = sessionDialogManage
	m.allowModal = nil
	m.pendingSessionDomain = ""
	m.sessionError = ""
}

func (m *Model) revokeSelectedSessionDomain() {
	selected := m.sessionList.Selected()
	if selected == nil || m.approver == nil {
		return
	}
	domain, ok := selected.Data.(string)
	if !ok {
		return
	}
	if !m.approver.RevokeDomainForSession(domain) {
		m.sessionError = "Could not revoke session access for " + domain + "."
		return
	}
	m.sessionError = ""
	m.sessionMessage = domain + " will require approval again."
	m.sessionMessageUntil = time.Now().Add(3 * time.Second)
	m.refreshSessionDomains()
}

func (m *Model) closeSessionDialog() {
	m.dialog = sessionDialogNone
	m.allowModal = nil
	m.pendingSessionDomain = ""
}

func (m *Model) refreshSessionDomains() {
	if m.approver == nil {
		m.sessionDomains = nil
		m.sessionList.SetItems(nil)
		return
	}

	m.sessionDomains = append([]string(nil), m.approver.SessionAllowedDomains()...)
	items := make([]components.ListItem, len(m.sessionDomains))
	for i, domain := range m.sessionDomains {
		items[i] = components.ListItem{ID: domain, Data: domain}
	}
	m.sessionList.SetItems(items)
}

func (m *Model) removeRequestsForDomain(domain string) {
	normalized := canonicalDomain(domain)
	kept := m.pending[:0]
	for _, request := range m.pending {
		if canonicalDomain(request.Request.Domain) != normalized {
			kept = append(kept, request)
		}
	}
	m.pending = kept
	m.rebuildListItems()
}

func canonicalDomain(domain string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
}

// addRequest inserts a new pending request into the list.
func (m *Model) addRequest(req app.ACLRequest) {
	pr := &app.PendingRequest{
		Request:  req,
		Deadline: time.Now().Add(m.timeout),
	}
	// decision defaults to zero value (DecisionPending).
	m.pending = append(m.pending, pr)
	m.sortByUrgency()
	m.rebuildListItems()
}

// removeRequest removes a request by ID.
func (m *Model) removeRequest(id string) {
	for i, pr := range m.pending {
		if pr.Request.ID == id {
			m.pending = append(m.pending[:i], m.pending[i+1:]...)
			break
		}
	}
	m.rebuildListItems()
}

// pruneExpired removes requests whose deadline has passed.
func (m *Model) pruneExpired() {
	now := time.Now()
	var kept []*app.PendingRequest
	for _, pr := range m.pending {
		if now.Before(pr.Deadline) && pr.GetDecision() == app.DecisionPending {
			kept = append(kept, pr)
		}
	}
	m.pending = kept
	m.rebuildListItems()
}

// sortByUrgency sorts pending requests by time remaining ascending
// (most urgent at top).
func (m *Model) sortByUrgency() {
	sort.Slice(m.pending, func(i, j int) bool {
		return m.pending[i].Deadline.Before(m.pending[j].Deadline)
	})
}

// rebuildListItems syncs the ScrollableList items from the pending slice.
func (m *Model) rebuildListItems() {
	items := make([]components.ListItem, len(m.pending))
	for i, pr := range m.pending {
		items[i] = components.ListItem{
			ID:   pr.Request.ID,
			Data: pr,
		}
	}
	m.list.SetItems(items)
}
