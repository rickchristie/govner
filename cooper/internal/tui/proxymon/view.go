package proxymon

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/proxy"
	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// renderPendingList renders the left pane with the scrollable list of
// pending requests and their countdown timer bars.
func renderPendingList(m *Model, width, height int) string {
	if len(m.pending) == 0 {
		return pendingEmptyState(width, height)
	}

	// Each request takes 2 display rows, so the effective list height
	// in terms of items is height/2.
	visibleItems := height / 2
	if visibleItems < 1 {
		visibleItems = 1
	}
	m.list.Width = width
	m.list.Height = visibleItems

	// Render manually since each item is 2 rows.
	var rows []string
	end := m.list.ScrollOffset + visibleItems
	if end > len(m.pending) {
		end = len(m.pending)
	}

	for i := m.list.ScrollOffset; i < end; i++ {
		selected := i == m.list.SelectedIdx
		pr := m.pending[i]
		row := renderPendingItem(pr, selected, width)
		rows = append(rows, row)
	}

	return strings.Join(rows, "\n")
}

// renderPendingItem renders a single pending request as two lines:
// Line 1: [arrow] domain  [timer bar]
// Line 2: time remaining  source container  method badge
func renderPendingItem(pr *proxy.PendingRequest, selected bool, width int) string {
	// Timer bar: use roughly 40% of the width.
	timerWidth := width * 2 / 5
	if timerWidth < 8 {
		timerWidth = 8
	}
	tb := components.NewTimerBar(pr.Deadline, pr.Deadline.Sub(pr.Request.Timestamp), timerWidth)

	// Arrow prefix.
	var arrow string
	if selected {
		arrow = theme.SelectionArrowStyle.Render(theme.IconArrowRight) + " "
	} else {
		arrow = "  "
	}

	// Domain name styling.
	var domainStyled string
	if selected {
		domainStyled = theme.DomainStyle.Bold(true).Render(pr.Request.Domain)
	} else {
		domainStyled = lipgloss.NewStyle().Foreground(theme.ColorLinen).Render(pr.Request.Domain)
	}

	timerView := tb.View()

	// Line 1: arrow + domain + padding + timer bar.
	domainPart := arrow + domainStyled
	domainW := lipgloss.Width(domainPart)
	timerW := lipgloss.Width(timerView)
	gap := width - domainW - timerW
	if gap < 1 {
		gap = 1
	}
	line1 := domainPart + strings.Repeat(" ", gap) + timerView

	// Line 2: time remaining, source, method badge.
	remaining := time.Until(pr.Deadline)
	if remaining < 0 {
		remaining = 0
	}
	progress := tb.Progress()
	timeColor := theme.TimerColor(progress)
	timeStyle := lipgloss.NewStyle().Foreground(timeColor)
	timeStr := timeStyle.Render(fmt.Sprintf("%.1fs", remaining.Seconds()))

	sourceStr := theme.SourceStyle.Render(pr.Request.SourceIP)

	// Port hint as a pseudo-method: 443 = HTTPS, 80 = HTTP.
	portLabel := portToMethod(pr.Request.Port)
	methodStr := methodBadge(portLabel)

	line2Parts := "  " + timeStr + "  " + sourceStr
	methodW := lipgloss.Width(methodStr)
	line2LeftW := lipgloss.Width(line2Parts)
	gap2 := width - line2LeftW - methodW
	if gap2 < 1 {
		gap2 = 1
	}
	line2 := line2Parts + strings.Repeat(" ", gap2) + methodStr

	// Apply selected row background.
	if selected {
		line1 = theme.RowSelectedStyle.Width(width).Render(line1)
		line2 = theme.RowSelectedStyle.Width(width).Render(line2)
	}

	return line1 + "\n" + line2
}

// renderDetailPane renders the right pane with details of the selected request.
func renderDetailPane(m *Model, width, height int) string {
	sel := m.list.Selected()
	if sel == nil {
		return detailEmptyState(width, height)
	}

	pr, ok := sel.Data.(*proxy.PendingRequest)
	if !ok {
		return detailEmptyState(width, height)
	}

	return renderRequestDetail(pr, width, height)
}

// renderRequestDetail renders the full detail for a pending request.
func renderRequestDetail(pr *proxy.PendingRequest, width, height int) string {
	labelStyle := theme.DetailLabelStyle
	valueStyle := theme.DetailValueStyle

	// Build URL from domain and port.
	scheme := "https"
	if pr.Request.Port == "80" {
		scheme = "http"
	}
	url := fmt.Sprintf("%s://%s", scheme, pr.Request.Domain)
	if pr.Request.Port != "443" && pr.Request.Port != "80" {
		url += ":" + pr.Request.Port
	}

	portLabel := portToMethod(pr.Request.Port)

	var lines []string
	lines = append(lines, "")
	lines = append(lines, labelStyle.Render("  URL     ")+valueStyle.Render(url))
	lines = append(lines, labelStyle.Render("  Method  ")+methodBadge(portLabel))
	lines = append(lines, labelStyle.Render("  Source  ")+theme.SourceStyle.Render(pr.Request.SourceIP))
	lines = append(lines, labelStyle.Render("  Domain  ")+theme.DomainStyle.Render(pr.Request.Domain))
	lines = append(lines, labelStyle.Render("  Port    ")+valueStyle.Render(pr.Request.Port))
	lines = append(lines, labelStyle.Render("  Time    ")+
		theme.TimestampStyle.Render(pr.Request.Timestamp.Format("15:04:05")))
	lines = append(lines, "")

	// Countdown.
	remaining := time.Until(pr.Deadline)
	if remaining < 0 {
		remaining = 0
	}
	tb := components.NewTimerBar(pr.Deadline, pr.Deadline.Sub(pr.Request.Timestamp), width-6)
	lines = append(lines, labelStyle.Render("  Timer   ")+tb.View())

	return strings.Join(lines, "\n")
}

// renderWithSessionAccess adds the session-only access rail and owns local
// modal presentation. The underlying monitor remains visible but dimmed so
// confirmation and management dialogs stay anchored to the current request.
func (m *Model) renderWithSessionAccess(base string, width, height int) string {
	rail := renderSessionAccessRail(m, width)
	divider := theme.DividerStyle.Render(repeatToWidth(theme.BorderH, width))
	content := rail + "\n" + divider + "\n" + base
	if m.dialog == sessionDialogNone {
		return content
	}

	lines := strings.Split(content, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	background := strings.Join(lines, "\n")

	var modal string
	switch m.dialog {
	case sessionDialogAllow:
		if m.allowModal != nil {
			modal = m.allowModal.View(width, height)
		}
	case sessionDialogManage:
		modal = m.renderSessionManagerModal(width, height)
	}
	return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top,
		components.DimContent(background)) + "\r" + modal
}

func renderSessionAccessRail(m *Model, width int) string {
	title := theme.PaneLabelStyle.Render(theme.IconShield + " SESSION ACCESS")
	var status string
	switch {
	case m.sessionError != "":
		status = theme.ErrorStyle.Render(theme.IconWarn + " " + m.sessionError)
	case m.sessionMessage != "":
		status = theme.ProofStyle.Render(theme.IconCheck+" ") +
			theme.DetailValueStyle.Render(m.sessionMessage)
	case len(m.sessionDomains) == 0:
		status = theme.DimStyle.Render("None") + "  " +
			theme.HelpDescStyle.Render("["+theme.HelpKeyStyle.Render("w")+" allow selected exact host]")
	default:
		count := fmt.Sprintf("%d exact host", len(m.sessionDomains))
		if len(m.sessionDomains) != 1 {
			count += "s"
		}
		preview := strings.Join(m.sessionDomains, "  ·  ")
		status = theme.StatusRunningStyle.Bold(true).Render(count) + "  " +
			theme.DomainStyle.Render(preview)
	}

	suffix := theme.HelpDescStyle.Render(
		"[" + theme.HelpKeyStyle.Render("s") + " manage]  ·  cleared on exit",
	)
	line := " " + title + "  " + status
	gap := width - lipgloss.Width(line) - lipgloss.Width(suffix) - 1
	if gap > 1 {
		line += strings.Repeat(" ", gap) + suffix
	} else {
		line += "  " + suffix
	}
	return lipgloss.NewStyle().MaxWidth(width).Width(width).Render(line)
}

func (m *Model) renderSessionManagerModal(width, height int) string {
	boxWidth := min(68, width-8)
	if boxWidth < 36 {
		boxWidth = 36
	}
	innerWidth := boxWidth - 6

	title := lipgloss.NewStyle().
		Foreground(theme.ColorParchment).
		Bold(true).
		Width(innerWidth).
		Align(lipgloss.Center).
		Render(theme.IconShield + " Session Access")
	subtitle := lipgloss.NewStyle().
		Foreground(theme.ColorDusty).
		Width(innerWidth).
		Align(lipgloss.Center).
		Render("Exact hostnames automatically approved for every barrel\nuntil this Cooper instance exits.")
	divider := theme.ModalDividerStyle.Width(innerWidth).
		Render(strings.Repeat(theme.BorderH, max(1, innerWidth-4)))

	listHeight := min(8, max(3, height-14))
	list := m.sessionList
	list.Width = innerWidth
	list.Height = listHeight
	list.ClampScroll()

	var listView string
	if len(m.sessionDomains) == 0 {
		empty := theme.EmptyStateStyle.Render(
			"No active session access.\n\nApprove a pending request with [w].",
		)
		listView = lipgloss.Place(innerWidth, listHeight,
			lipgloss.Center, lipgloss.Center, empty)
	} else {
		listView = list.View(func(item components.ListItem, selected bool, rowWidth int) string {
			domain, _ := item.Data.(string)
			arrow := "  "
			style := theme.RowNormalStyle
			if selected {
				arrow = theme.SelectionArrowStyle.Render(theme.IconArrowRight) + " "
				style = theme.RowSelectedStyle
			}
			badge := theme.ProofStyle.Render("exact")
			left := arrow + theme.DomainStyle.Render(domain)
			gap := rowWidth - lipgloss.Width(left) - lipgloss.Width(badge)
			if gap < 1 {
				gap = 1
			}
			return style.Width(rowWidth).Render(left + strings.Repeat(" ", gap) + badge)
		})
	}

	help := lipgloss.NewStyle().
		Foreground(theme.ColorDusty).
		Width(innerWidth).
		Align(lipgloss.Center).
		Render("[" + theme.HelpKeyStyle.Render("↑↓") + " Navigate]   [" +
			theme.HelpKeyStyle.Render("r") + " Revoke]   [" +
			theme.HelpKeyStyle.Render("Esc") + " Close]")

	inner := lipgloss.JoinVertical(lipgloss.Center,
		"",
		title,
		"",
		subtitle,
		"",
		divider,
		"",
		listView,
		"",
		divider,
		"",
		help,
		"",
	)
	box := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(theme.ColorAmber).
		Background(theme.ColorOakDark).
		Padding(0, 2).
		Width(boxWidth).
		Render(inner)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// pendingEmptyState renders the empty state for the left pane.
func pendingEmptyState(width, height int) string {
	icon := lipgloss.NewStyle().Foreground(theme.ColorProof).Render(theme.IconCheck)
	msg := theme.EmptyStateStyle.Render("All clear. No pending\nrequests to review.")
	content := lipgloss.JoinVertical(lipgloss.Center, "", "", icon, "", msg)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

// detailEmptyState renders the empty state for the right detail pane.
func detailEmptyState(width, height int) string {
	msg := theme.EmptyStateStyle.Render("No request selected.")
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, msg)
}

// methodBadge returns a styled method badge string.
func methodBadge(method string) string {
	switch strings.ToUpper(method) {
	case "GET":
		return theme.MethodGetStyle.Render("GET")
	case "POST":
		return theme.MethodPostStyle.Render("POST")
	case "PUT":
		return theme.MethodPutStyle.Render("PUT")
	case "DELETE":
		return theme.MethodDeleteStyle.Render("DELETE")
	case "PATCH":
		return theme.MethodPatchStyle.Render("PATCH")
	default:
		return theme.MethodGetStyle.Render(method)
	}
}

// portToMethod returns a pseudo-HTTP method hint based on port.
// Since the ACL request only provides domain/port/source, we use
// CONNECT as the default since these are proxy CONNECT requests.
func portToMethod(port string) string {
	return "CONNECT"
}

// splitLines splits a rendered string into individual lines, padding
// to exactly count lines.
func splitLines(s string, count int) []string {
	lines := strings.Split(s, "\n")
	for len(lines) < count {
		lines = append(lines, "")
	}
	if len(lines) > count {
		lines = lines[:count]
	}
	return lines
}

// padToWidth pads a string with spaces to reach the target display width.
func padToWidth(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// repeatToWidth repeats a string until it fills the given display width.
func repeatToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	sw := lipgloss.Width(s)
	if sw == 0 {
		return strings.Repeat(" ", width)
	}
	n := width / sw
	if n < 1 {
		n = 1
	}
	result := strings.Repeat(s, n)
	// Trim or pad to exact width.
	for lipgloss.Width(result) > width && len(result) > 0 {
		result = result[:len(result)-1]
	}
	for lipgloss.Width(result) < width {
		result += " "
	}
	return result
}
