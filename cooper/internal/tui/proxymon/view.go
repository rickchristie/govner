package proxymon

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

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

	// Render manually since each item is 2 rows.
	var rows []string
	end := m.list.ScrollOffset + visibleItems
	if end > len(m.pending) {
		end = len(m.pending)
	}

	for i := m.list.ScrollOffset; i < end; i++ {
		selected := i == m.list.SelectedIdx
		pr := m.pending[i]
		row := renderPendingItem(pr, selected, width, m.now())
		rows = append(rows, row)
	}

	return strings.Join(rows, "\n")
}

// renderPendingItem renders a single pending request as two lines:
// Line 1: [arrow] domain  [timer bar]
// Line 2: time remaining  source container  method badge
func renderPendingItem(pr *proxy.PendingRequest, selected bool, width int, now time.Time) string {
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

	timerView := tb.ViewAt(now)

	// Line 1: arrow + domain + padding + timer bar.
	domainPart := ansi.Truncate(arrow+domainStyled, max(3, width-lipgloss.Width(timerView)-1), "…")
	domainW := lipgloss.Width(domainPart)
	timerW := lipgloss.Width(timerView)
	gap := width - domainW - timerW
	if gap < 1 {
		gap = 1
	}
	line1 := domainPart + strings.Repeat(" ", gap) + timerView

	// Line 2: time remaining, source, method badge.
	remaining := pr.Deadline.Sub(now)
	if remaining < 0 {
		remaining = 0
	}
	progress := tb.ProgressAt(now)
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

	return renderRequestDetail(pr, width, height, m.now())
}

// renderRequestDetail renders the full detail for a pending request.
func renderRequestDetail(pr *proxy.PendingRequest, width, height int, now time.Time) string {
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
	remaining := pr.Deadline.Sub(now)
	if remaining < 0 {
		remaining = 0
	}
	tb := components.NewTimerBar(pr.Deadline, pr.Deadline.Sub(pr.Request.Timestamp), max(1, width-20))
	lines = append(lines, labelStyle.Render("  Timer   ")+tb.ViewAt(now))

	return strings.Join(lines, "\n")
}

// Session access remains visible while requests change. The manager replaces
// the body so screen clipping cannot hide its host list or controls.
func (m *Model) renderWithSessionAccess(base string, width, height int) string {
	return renderSessionAccessRail(m, width) + "\n" + theme.DividerStyle.Render(repeatToWidth(theme.BorderH, width)) + "\n" + base
}

func renderSessionAccessRail(m *Model, width int) string {
	title := theme.PaneLabelStyle.Render(" SESSION ACCESS  ") + theme.ProofStyle.Render(sessionHostCount(len(m.sessionDomains)))
	controls := theme.HelpDescStyle.Render("  w Allow host  s Manage  · cleared on exit")
	if m.sessionError != "" {
		return ansi.Truncate(title+"  "+theme.ErrorStyle.Render(m.sessionError), width, "")
	}
	return ansi.Truncate(title+controls, width, "")
}

func sessionHostCount(count int) string {
	if count == 1 {
		return "1 exact host"
	}
	return fmt.Sprintf("%d exact hosts", count)
}

func (m *Model) renderSessionManager(width, height int) string {
	footer := "↑↓ Select   r Remove host   Esc Close"
	status := m.sessionMessage
	if m.sessionError != "" {
		status = m.sessionError
	}
	frame := components.FixedFrame{Width: width, Height: height, Footer: theme.HelpDescStyle.Render(footer)}
	list := m.sessionList
	list.Width, list.Height = width, max(0, frame.BodyHeight()-4)
	list.ClampScroll()
	title := theme.PaneLabelStyle.Render(" Session Access · " + sessionHostCount(len(m.sessionDomains)))
	subtitle := theme.DimStyle.Render(" Hosts are allowed for every barrel until Cooper exits.")
	header := title + "\n" + subtitle + "\n" + theme.DimStyle.Render(status) + "\n" + theme.DividerStyle.Render(strings.Repeat(theme.BorderH, max(0, width)))
	body := "No hosts allowed for this session. Use w on a pending request."
	if len(m.sessionDomains) > 0 {
		body = list.View(func(item components.ListItem, selected bool, rowWidth int) string {
			marker, style := "  ", theme.RowNormalStyle
			if selected {
				marker, style = "▶ ", theme.RowSelectedStyle
			}
			return style.Width(rowWidth).Render(ansi.Truncate(marker+item.Data.(string), rowWidth, "…"))
		})
	}
	return frame.View(header + "\n" + body)
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
