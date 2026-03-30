// Package bridgeui implements the Bridge Logs tab sub-model.
// It displays a scrollable list of bridge execution logs with a detail
// pane showing stdout/stderr for the selected entry.
package bridgeui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/bridge"
	"github.com/rickchristie/govner/cooper/internal/tableutil"
	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/events"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// LogsModel is the sub-model for the Bridge Logs tab.
type LogsModel struct {
	logs     []bridge.ExecutionLog
	list     components.ScrollableList
	capacity int

	// Detail pane toggle.
	showDetail bool
}

// NewLogsModel creates a new bridge logs sub-model with the given capacity.
func NewLogsModel(capacity int) *LogsModel {
	if capacity < 1 {
		capacity = 500
	}
	return &LogsModel{
		list:     components.NewScrollableList(10, 80),
		capacity: capacity,
	}
}

// SetMaxCapacity updates the maximum number of log entries kept at runtime.
func (m *LogsModel) SetMaxCapacity(n int) {
	if n < 1 {
		n = 500
	}
	m.capacity = n
	// Trim existing logs if needed.
	if len(m.logs) > m.capacity {
		m.logs = m.logs[:m.capacity]
		m.syncList()
	}
}

// AddLog prepends a log entry to the list and trims to capacity.
func (m *LogsModel) AddLog(log bridge.ExecutionLog) {
	m.logs = append([]bridge.ExecutionLog{log}, m.logs...)
	if len(m.logs) > m.capacity {
		m.logs = m.logs[:m.capacity]
	}
	m.syncList()
}

// syncList rebuilds the ScrollableList items from m.logs.
func (m *LogsModel) syncList() {
	items := make([]components.ListItem, len(m.logs))
	for i, l := range m.logs {
		items[i] = components.ListItem{
			ID:   fmt.Sprintf("log-%d", i),
			Data: l,
		}
	}
	m.list.SetItems(items)
}

// selectedLog returns the log entry at the current selection, or nil.
func (m *LogsModel) selectedLog() *bridge.ExecutionLog {
	sel := m.list.Selected()
	if sel == nil {
		return nil
	}
	if log, ok := sel.Data.(bridge.ExecutionLog); ok {
		return &log
	}
	return nil
}

// Init satisfies the SubModel interface.
func (m *LogsModel) Init() tea.Cmd {
	return nil
}

// Update satisfies theme.SubModel.
func (m *LogsModel) Update(msg tea.Msg) (theme.SubModel, tea.Cmd) {
	switch msg := msg.(type) {
	case events.BridgeLogMsg:
		m.AddLog(msg.Log)
		return m, nil
	case tea.MouseMsg:
		m.list.HandleMouse(msg)
		return m, nil
	case tea.KeyMsg:
		// When detail pane is open, esc closes it.
		if m.showDetail && msg.String() == "esc" {
			m.showDetail = false
			return m, nil
		}

		switch msg.String() {
		case "up", "k":
			m.list.MoveUp()
		case "down", "j":
			m.list.MoveDown()
		case "G":
			// Jump to bottom.
			if len(m.logs) > 0 {
				m.list.SelectedIdx = len(m.logs) - 1
				m.list.ClampScroll()
			}
		case "g":
			// Jump to top.
			m.list.SelectedIdx = 0
			m.list.ClampScroll()
		case "enter":
			if len(m.logs) > 0 {
				m.showDetail = !m.showDetail
			}
		}
	}
	return m, nil
}

// View satisfies the SubModel interface.
func (m *LogsModel) View(width, height int) string {
	if len(m.logs) == 0 {
		return renderLogsEmpty(width, height)
	}

	m.list.Width = width - 2
	// Reserve space for header (2 lines) and detail pane if visible.
	detailHeight := 0
	if m.showDetail {
		detailHeight = 12
	}
	listHeight := height - 3 - detailHeight
	if listHeight < 1 {
		listHeight = 1
	}
	m.list.Height = listHeight

	var b strings.Builder

	// Build a table to compute column widths for alignment.
	logTbl := buildLogTable(m.logs)

	// Column header.
	b.WriteString(renderLogHeader(logTbl, width))
	b.WriteString("\n")

	// Compute column widths for row rendering.
	logColWidths, _ := logTbl.RenderRows(0)

	// Log list.
	listView := m.list.View(func(item components.ListItem, selected bool, w int) string {
		log, ok := item.Data.(bridge.ExecutionLog)
		if !ok {
			return ""
		}
		return renderLogRow(log, selected, w, logColWidths)
	})
	b.WriteString(listView)

	// Detail pane.
	if m.showDetail {
		if sel := m.selectedLog(); sel != nil {
			b.WriteString("\n")
			b.WriteString(renderLogDetail(*sel, width-2))
		}
	}

	return b.String()
}

// ----- Rendering helpers -----

// buildLogTable creates a tableRenderer populated with all log entries,
// used to compute consistent column widths.
func buildLogTable(logs []bridge.ExecutionLog) *tableutil.TableRenderer {
	tbl := tableutil.NewTable("TIME", "ROUTE", "SCRIPT", "STATUS", "DURATION")
	tbl.SetHeaderStyle(theme.ColorDusty, true)
	for _, log := range logs {
		ts := theme.TimestampStyle.Render(log.Timestamp.Format("15:04:05"))
		route := lipgloss.NewStyle().Foreground(theme.ColorParchment).Render(log.Route)
		script := lipgloss.NewStyle().Foreground(theme.ColorVerdigris).Render(truncate(log.ScriptPath, 28))

		var statusStr string
		if log.ExitCode == 0 {
			statusStr = lipgloss.NewStyle().Foreground(theme.ColorProof).Render(theme.IconCheck + " 0")
		} else {
			statusStr = lipgloss.NewStyle().Foreground(theme.ColorFlame).Render(
				fmt.Sprintf("%s %d", theme.IconCross, log.ExitCode),
			)
		}

		dur := theme.DimStyle.Render(formatDuration(log.Duration))
		tbl.AddRow(ts, route, script, statusStr, dur)
	}
	return tbl
}

func renderLogHeader(tbl *tableutil.TableRenderer, width int) string {
	header := " " + tbl.RenderHeader()
	divider := theme.DividerStyle.Render(" " + strings.Repeat(theme.BorderH, width-2))
	return header + "\n" + divider
}

func renderLogRow(log bridge.ExecutionLog, selected bool, width int, colWidths []int) string {
	tsW, routeW, scriptW, statusW, durW := 10, 20, 30, 8, 10
	if len(colWidths) >= 5 {
		tsW = colWidths[0]
		routeW = colWidths[1]
		scriptW = colWidths[2]
		statusW = colWidths[3]
		durW = colWidths[4]
	}

	ts := theme.TimestampStyle.Width(tsW).Render(log.Timestamp.Format("15:04:05"))
	route := lipgloss.NewStyle().Foreground(theme.ColorParchment).Width(routeW).Render(log.Route)
	script := lipgloss.NewStyle().Foreground(theme.ColorVerdigris).Width(scriptW).Render(truncate(log.ScriptPath, scriptW-2))

	var statusStr string
	if log.ExitCode == 0 {
		statusStr = lipgloss.NewStyle().Foreground(theme.ColorProof).Width(statusW).Render(theme.IconCheck + " 0")
	} else {
		statusStr = lipgloss.NewStyle().Foreground(theme.ColorFlame).Width(statusW).Render(
			fmt.Sprintf("%s %d", theme.IconCross, log.ExitCode),
		)
	}

	dur := theme.DimStyle.Width(durW).Render(formatDuration(log.Duration))

	row := ts + "  " + route + "  " + script + "  " + statusStr + "  " + dur

	if selected {
		arrow := theme.SelectionArrowStyle.Render(theme.IconArrowRight)
		return theme.RowSelectedStyle.Width(width).Render(arrow + " " + row)
	}
	return theme.RowNormalStyle.Width(width).Render("  " + row)
}

func renderLogDetail(log bridge.ExecutionLog, width int) string {
	var b strings.Builder

	headerStyle := lipgloss.NewStyle().Foreground(theme.ColorDusty).Bold(true)
	b.WriteString(headerStyle.Render(
		" " + theme.BorderTL + strings.Repeat(theme.BorderH, 2) +
			" Execution Detail " +
			strings.Repeat(theme.BorderH, maxInt(width-24, 0)) +
			theme.BorderTR,
	))
	b.WriteString("\n")

	labelStyle := theme.DetailLabelStyle
	valueStyle := theme.DetailValueStyle

	b.WriteString(" " + theme.BorderV + "  " +
		labelStyle.Render("Route") + "  " + valueStyle.Render(log.Route) + "\n")
	b.WriteString(" " + theme.BorderV + "  " +
		labelStyle.Render("Script") + "  " + valueStyle.Render(log.ScriptPath) + "\n")
	b.WriteString(" " + theme.BorderV + "  " +
		labelStyle.Render("Time") + "  " + valueStyle.Render(log.Timestamp.Format("15:04:05")) + "\n")
	b.WriteString(" " + theme.BorderV + "  " +
		labelStyle.Render("Duration") + "  " + valueStyle.Render(formatDuration(log.Duration)) + "\n")

	var exitStyle lipgloss.Style
	if log.ExitCode == 0 {
		exitStyle = lipgloss.NewStyle().Foreground(theme.ColorProof)
	} else {
		exitStyle = lipgloss.NewStyle().Foreground(theme.ColorFlame)
	}
	b.WriteString(" " + theme.BorderV + "  " +
		labelStyle.Render("Exit") + "  " + exitStyle.Render(fmt.Sprintf("%d", log.ExitCode)) + "\n")

	// Stdout.
	stdoutHeader := lipgloss.NewStyle().Foreground(theme.ColorVerdigris).Render(
		" " + theme.BorderV + "  " + strings.Repeat(theme.BorderH, 2) + " stdout " +
			strings.Repeat(theme.BorderH, maxInt(width-16, 0)),
	)
	b.WriteString(stdoutHeader + "\n")
	if log.Stdout == "" {
		b.WriteString(" " + theme.BorderV + "  " +
			lipgloss.NewStyle().Foreground(theme.ColorFaded).Italic(true).Render("(empty)") + "\n")
	} else {
		for _, line := range strings.Split(strings.TrimRight(log.Stdout, "\n"), "\n") {
			b.WriteString(" " + theme.BorderV + "  " +
				lipgloss.NewStyle().Foreground(theme.ColorLinen).Render(truncate(line, width-6)) + "\n")
		}
	}

	// Stderr.
	stderrHeader := lipgloss.NewStyle().Foreground(theme.ColorCopper).Render(
		" " + theme.BorderV + "  " + strings.Repeat(theme.BorderH, 2) + " stderr " +
			strings.Repeat(theme.BorderH, maxInt(width-16, 0)),
	)
	b.WriteString(stderrHeader + "\n")
	if log.Stderr == "" {
		b.WriteString(" " + theme.BorderV + "  " +
			lipgloss.NewStyle().Foreground(theme.ColorFaded).Italic(true).Render("(empty)") + "\n")
	} else {
		for _, line := range strings.Split(strings.TrimRight(log.Stderr, "\n"), "\n") {
			b.WriteString(" " + theme.BorderV + "  " +
				lipgloss.NewStyle().Foreground(theme.ColorCopper).Render(truncate(line, width-6)) + "\n")
		}
	}

	b.WriteString(headerStyle.Render(
		" " + theme.BorderBL + strings.Repeat(theme.BorderH, maxInt(width-2, 0)) + theme.BorderBR,
	))

	return b.String()
}

func renderLogsEmpty(width, height int) string {
	content := lipgloss.JoinVertical(lipgloss.Center,
		"",
		"",
		theme.EmptyStateStyle.Render(theme.IconPlug),
		"",
		theme.EmptyStateStyle.Render("No bridge executions yet."),
		"",
		theme.EmptyStateStyle.Render("Configure routes in the Routes tab,"),
		theme.EmptyStateStyle.Render("then AI tools can call them"),
		theme.EmptyStateStyle.Render("via the bridge API."),
	)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func truncate(s string, maxLen int) string {
	if maxLen < 0 {
		maxLen = 0
	}
	if len(s) <= maxLen {
		return s
	}
	if maxLen < 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
