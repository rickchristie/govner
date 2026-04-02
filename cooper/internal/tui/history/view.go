package history

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/tableutil"
	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// render produces the full tab content: column header, list, and optionally
// the detail pane.
func (m *Model) render(width, height int) string {
	var sections []string

	// Column header row.
	sections = append(sections, m.renderColumnHeader(width))

	// Scrollable list — takes full available height (no inline detail split).
	listView := m.renderList(width, m.list.Height)
	sections = append(sections, listView)

	result := strings.Join(sections, "\n")

	// Detail shown as modal overlay when open.
	if m.detailOpen {
		if entry := m.selectedEntry(); entry != nil {
			// Pad result to fill height so the overlay covers the full area.
			resultLines := strings.Split(result, "\n")
			for len(resultLines) < height {
				resultLines = append(resultLines, "")
			}
			bg := strings.Join(resultLines, "\n")

			modal := m.renderDetailModal(*entry, width, height)
			return lipgloss.Place(width, height, lipgloss.Left, lipgloss.Top,
				components.DimContent(bg)) + "\r" + modal
		}
	}

	return result
}

// renderDetailModal renders the detail as a centered modal overlay.
func (m *Model) renderDetailModal(entry HistoryEntry, width, height int) string {
	var titleStyle lipgloss.Style
	if m.mode == ModeBlocked {
		titleStyle = lipgloss.NewStyle().Foreground(theme.ColorFlame).Bold(true)
	} else {
		titleStyle = lipgloss.NewStyle().Foreground(theme.ColorProof).Bold(true)
	}

	// Build detail content.
	var lines []string

	url := "https://" + entry.Request.Domain
	if entry.Request.Port != "" && entry.Request.Port != "443" {
		url += ":" + entry.Request.Port
	}
	lines = append(lines, detailRow("URL", url))
	lines = append(lines, detailRow("Method", "CONNECT"))
	lines = append(lines, detailRow("Source", entry.Request.SourceIP))
	lines = append(lines, detailRow("Time", entry.Timestamp.Format("15:04:05")))

	if m.mode == ModeBlocked {
		var reason string
		switch entry.Decision {
		case "timeout":
			reason = "timeout (expired)"
		case "denied":
			reason = "denied by user"
		default:
			reason = entry.Decision
		}
		lines = append(lines, detailRow("Reason", reason))
	} else {
		lines = append(lines, detailRow("Type", entry.Decision))
		if entry.ResponseStatus > 0 {
			lines = append(lines, detailRow("Status", fmt.Sprintf("%d", entry.ResponseStatus)))
		}
	}

	inner := strings.Join(lines, "\n")

	var titleText string
	if m.mode == ModeBlocked {
		titleText = "Blocked Request Detail"
	} else {
		titleText = "Allowed Request Detail"
	}

	boxWidth := min(60, width-8)
	box := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(theme.ColorOakLight).
		Padding(1, 3).
		Width(boxWidth).
		Render(titleStyle.Render(titleText) + "\n\n" + inner + "\n\n" +
			lipgloss.NewStyle().Foreground(theme.ColorDusty).Render("[Esc/Enter] Close"))

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}

// renderColumnHeader renders the table column header.
func (m *Model) renderColumnHeader(width int) string {
	var tbl *tableutil.TableRenderer
	if m.mode == ModeAllowed {
		tbl = tableutil.NewTable("TIME", "DOMAIN", "SOURCE", "METHOD", "STATUS", "TYPE")
	} else {
		tbl = tableutil.NewTable("TIME", "DOMAIN", "SOURCE", "METHOD", "REASON")
	}
	tbl.SetHeaderStyle(theme.ColorDusty, true)
	tbl.SetMinWidth(0, 10)  // TIME
	tbl.SetMinWidth(1, 26)  // DOMAIN
	tbl.SetMinWidth(2, 16)  // SOURCE
	tbl.SetMinWidth(3, 7)   // METHOD
	return theme.ColumnHeaderStyle.Width(width).Render(" " + tbl.RenderHeader())
}

// renderList renders the scrollable list portion using the shared
// ScrollableList component.
func (m *Model) renderList(width, height int) string {
	m.list.Width = width
	m.list.Height = height

	return m.list.View(func(item components.ListItem, selected bool, w int) string {
		entry, ok := item.Data.(HistoryEntry)
		if !ok {
			return ""
		}
		return m.renderRow(entry, selected, w)
	})
}

// renderRow renders a single list row.
func (m *Model) renderRow(entry HistoryEntry, selected bool, width int) string {
	// Indicator arrow for selected row.
	arrow := "  "
	if selected {
		arrow = theme.SelectionArrowStyle.Render(theme.IconArrowRight) + " "
	}

	// Timestamp.
	ts := theme.TimestampStyle.Render(entry.Timestamp.Format("15:04:05"))

	// Domain -- different style for selected vs unselected.
	domain := entry.Request.Domain
	if len(domain) > 26 {
		domain = domain[:23] + "..."
	}
	var domainStr string
	if selected {
		domainStr = theme.DomainStyle.Render(fmt.Sprintf("%-26s", domain))
	} else {
		domainStr = lipgloss.NewStyle().Foreground(theme.ColorLinen).
			Render(fmt.Sprintf("%-26s", domain))
	}

	// Source container.
	source := entry.Request.SourceIP
	if len(source) > 16 {
		source = source[:13] + "..."
	}
	sourceStr := theme.SourceStyle.Render(fmt.Sprintf("%-16s", source))

	// Method badge (empty if not available -- ACL is domain-only).
	methodStr := fmt.Sprintf("%-7s", "")

	// Decision / reason / type column.
	var trailingCol string
	if m.mode == ModeBlocked {
		trailingCol = m.renderReason(entry.Decision)
	} else {
		statusStr := m.renderStatusCode(entry.ResponseStatus)
		typeStr := m.renderType(entry.Decision)
		trailingCol = statusStr + "  " + typeStr
	}

	row := arrow + ts + "  " + domainStr + "  " + sourceStr + "  " + methodStr + "  " + trailingCol

	// Apply row-level styling.
	if selected {
		return theme.RowSelectedStyle.Width(width).Render(row)
	}
	return theme.RowNormalStyle.Width(width).Render(row)
}

// renderReason renders the reason badge for blocked entries.
func (m *Model) renderReason(decision string) string {
	switch decision {
	case "timeout":
		return theme.CopperStyle.Render("timeout")
	case "denied":
		return theme.FlameStyle.Render("denied")
	default:
		return theme.DimStyle.Render(decision)
	}
}

// renderStatusCode renders an HTTP status code with the appropriate color.
func (m *Model) renderStatusCode(status int) string {
	if status == 0 {
		return fmt.Sprintf("%-6s", "")
	}

	label := fmt.Sprintf("%d", status)
	switch {
	case status >= 200 && status < 300:
		return theme.StatusCode2xxStyle.Render(fmt.Sprintf("%-6s", label))
	case status >= 300 && status < 400:
		return theme.StatusCode3xxStyle.Render(fmt.Sprintf("%-6s", label))
	case status >= 400 && status < 500:
		return theme.StatusCode4xxStyle.Render(fmt.Sprintf("%-6s", label))
	case status >= 500:
		return theme.StatusCode5xxStyle.Render(fmt.Sprintf("%-6s", label))
	default:
		return theme.DimStyle.Render(fmt.Sprintf("%-6s", label))
	}
}

// renderType renders the type badge for allowed entries.
func (m *Model) renderType(decision string) string {
	switch decision {
	case "whitelist":
		return theme.ProofStyle.Render("whitelist")
	case "approved":
		return lipgloss.NewStyle().Foreground(theme.ColorAmber).Render("approved")
	default:
		return theme.DimStyle.Render(decision)
	}
}

// renderDetail renders the detail pane for the selected entry.
func (m *Model) renderDetail(entry HistoryEntry, width, height int) string {
	// Build the detail pane title.
	var titleStyle lipgloss.Style
	var titleText string
	if m.mode == ModeBlocked {
		titleStyle = lipgloss.NewStyle().Foreground(theme.ColorFlame).Bold(true)
		titleText = " Blocked Detail "
	} else {
		titleStyle = lipgloss.NewStyle().Foreground(theme.ColorProof).Bold(true)
		titleText = " Allowed Detail "
	}

	// Construct inner content lines.
	var lines []string

	// URL: domain + port.
	url := "https://" + entry.Request.Domain
	if entry.Request.Port != "" && entry.Request.Port != "443" {
		url += ":" + entry.Request.Port
	}
	lines = append(lines, detailRow("URL", url))

	// Method.
	lines = append(lines, detailRow("Method", "CONNECT"))

	// Source.
	lines = append(lines, detailRow("Source", entry.Request.SourceIP))

	// Time.
	lines = append(lines, detailRow("Time", entry.Timestamp.Format("15:04:05")))

	if m.mode == ModeBlocked {
		// Reason.
		var reason string
		switch entry.Decision {
		case "timeout":
			reason = "timeout (expired)"
		case "denied":
			reason = "denied by user"
		default:
			reason = entry.Decision
		}
		lines = append(lines, detailRow("Reason", reason))
	} else {
		// Type.
		lines = append(lines, detailRow("Type", entry.Decision))
	}

	lines = append(lines, "")

	// Request headers section.
	lines = append(lines, sectionHeader("Request Headers", width-6))
	lines = append(lines, detailRow("Host", entry.Request.Domain))
	lines = append(lines, "")

	// For Allowed mode, add response section.
	if m.mode == ModeAllowed {
		lines = append(lines, sectionHeader("Response", width-6))

		statusLabel := fmt.Sprintf("%d", entry.ResponseStatus)
		if entry.ResponseStatus == 0 {
			statusLabel = "pending"
		}
		lines = append(lines, detailRow("Status", statusLabel))

		if entry.ResponseHeaders != "" {
			lines = append(lines, "")
			lines = append(lines, sectionHeader("Response Headers", width-6))
			for _, hdr := range strings.Split(entry.ResponseHeaders, "\n") {
				hdr = strings.TrimSpace(hdr)
				if hdr == "" {
					continue
				}
				parts := strings.SplitN(hdr, ":", 2)
				if len(parts) == 2 {
					lines = append(lines, detailRow(
						strings.TrimSpace(parts[0]),
						strings.TrimSpace(parts[1]),
					))
				} else {
					lines = append(lines, "  "+theme.DetailValueStyle.Render(hdr))
				}
			}
		}
	}

	inner := strings.Join(lines, "\n")

	// Wrap in the detail pane border.
	paneWidth := width - 2
	if paneWidth < 10 {
		paneWidth = 10
	}

	pane := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(theme.ColorOakLight).
		Padding(0, 1).
		Width(paneWidth).
		MaxHeight(height).
		Render(inner)

	// Embed the title into the top border.
	renderedTitle := titleStyle.Render(titleText)
	topBorderPrefix := theme.DividerStyle.Render(theme.BorderTL + theme.BorderH)
	pane = topBorderPrefix + renderedTitle + pane[lipgloss.Width(topBorderPrefix)+lipgloss.Width(renderedTitle):]

	return pane
}

// renderEmpty renders the centered empty state message.
func (m *Model) renderEmpty(width, height int) string {
	var msg string
	if m.mode == ModeBlocked {
		msg = theme.IconShield + "\n\n" +
			"No blocked requests yet.\n\n" +
			"All network requests have been approved\n" +
			"or no requests have been made."
	} else {
		msg = theme.BarrelEmoji + "\n\n" +
			"No requests recorded yet.\n\n" +
			"Requests will appear here once AI tools\n" +
			"start making network calls."
	}

	styled := theme.EmptyStateStyle.Render(msg)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, styled)
}

// ----- Rendering helpers -----

// detailRow renders a label: value pair using the standard detail styles.
func detailRow(label, value string) string {
	return "  " +
		theme.DetailLabelStyle.Render(label) + "  " +
		theme.DetailValueStyle.Render(value)
}

// sectionHeader renders a section divider like "-- Headers -----------".
func sectionHeader(title string, width int) string {
	prefix := theme.BorderH + theme.BorderH + " " + title + " "
	fillLen := width - lipgloss.Width(prefix)
	if fillLen < 0 {
		fillLen = 0
	}
	fill := strings.Repeat(theme.BorderH, fillLen)
	return "  " + theme.DetailSectionStyle.Render(prefix+fill)
}
