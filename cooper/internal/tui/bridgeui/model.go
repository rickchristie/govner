// Package bridgeui implements the routes and execution log panes.
// Execution details show the full stdout and stderr for the selected record.
package bridgeui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/x/ansi"
	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/events"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// LogsModel keeps execution records and opens their full output in a
// selectable viewport. A record remains fixed while new executions arrive.
type LogsModel struct {
	logs       []app.ExecutionLog
	list       components.ScrollableList
	capacity   int
	showDetail bool
	detail     components.TextLog
}

func NewLogsModel(capacity int, copier ...components.TextCopier) *LogsModel {
	if capacity < 1 {
		capacity = 500
	}
	var copyText components.TextCopier
	if len(copier) > 0 {
		copyText = copier[0]
	}
	return &LogsModel{list: components.NewScrollableList(10, 80), capacity: capacity, detail: components.NewTextLog("bridge", 0, copyText)}
}

func (m *LogsModel) SetMaxCapacity(n int) {
	if n < 1 {
		n = 500
	}
	m.capacity = n
	if len(m.logs) > n {
		m.logs = m.logs[:n]
	}
	m.syncList()
}

func (m *LogsModel) AddLog(log app.ExecutionLog) {
	// Keep the selected execution in place when a later record arrives.
	if len(m.logs) > 0 {
		m.list.SelectedIdx++
		m.list.ScrollOffset++
	}
	m.logs = append([]app.ExecutionLog{log}, m.logs...)
	if len(m.logs) > m.capacity {
		m.logs = m.logs[:m.capacity]
	}
	m.syncList()
}

func (m *LogsModel) syncList() {
	items := make([]components.ListItem, len(m.logs))
	for i, log := range m.logs {
		items[i] = components.ListItem{ID: fmt.Sprintf("log-%d", i), Data: log}
	}
	m.list.SetItems(items)
}

func (m *LogsModel) Init() tea.Cmd { return nil }

func (m *LogsModel) Update(msg tea.Msg) (theme.SubModel, tea.Cmd) {
	switch event := msg.(type) {
	case events.BridgeLogMsg:
		m.AddLog(event.Log)
		return m, nil
	case events.RuntimeLimitsChangedMsg:
		m.SetMaxCapacity(event.BridgeLogLimit)
		return m, nil
	case tea.WindowSizeMsg:
		m.list.Width, m.list.Height = event.Width, max(1, event.Height-4)
		m.list.ClampScroll()
		event.Height = max(0, event.Height-1)
		return m, m.detail.Update(event)
	case components.TextCopiedMsg:
		return m, m.detail.Update(event)
	case tea.KeyMsg:
		if m.showDetail {
			if event.String() == "esc" {
				m.showDetail = false
				return m, nil
			}
			return m, m.detail.Update(event)
		}
		switch event.String() {
		case "up", "k":
			m.list.MoveUp()
		case "down", "j":
			m.list.MoveDown()
		case "g", "home":
			m.list.SelectedIdx = 0
			m.list.ClampScroll()
		case "G", "end":
			m.list.SelectedIdx = max(0, len(m.logs)-1)
			m.list.ClampScroll()
		case "enter", "y":
			if selected := m.list.Selected(); selected != nil {
				m.showDetail = true
				m.detail.SetContent(executionText(selected.Data.(app.ExecutionLog)))
				m.detail.Update(tea.KeyMsg{Type: tea.KeyHome})
				if event.String() == "y" {
					m.detail.SelectAll()
					return m, m.detail.Update(event)
				}
			}
		}
	case tea.MouseMsg:
		if m.showDetail {
			event.Y--
			return m, m.detail.Update(event)
		}
		if event.Button == tea.MouseButtonLeft && event.Action == tea.MouseActionPress && event.Y >= 2 && event.Y < m.list.Height+2 {
			index := event.Y - 2 + m.list.ScrollOffset
			if index < len(m.logs) {
				m.list.SelectedIdx = index
			}
		} else {
			m.list.HandleMouse(event)
		}
	}
	return m, nil
}

func (m *LogsModel) View(width, height int) string {
	if m.showDetail {
		return theme.PaneLabelStyle.Render(" Execution output · Esc Back") + "\n" + m.detail.View(width, max(0, height-1))
	}
	frame := components.FixedFrame{Width: width, Height: height, Header: theme.PaneLabelStyle.Render(" TIME      EXIT  ROUTE"), Footer: theme.HelpDescStyle.Render("Enter Output  y Copy record")}
	if len(m.logs) == 0 {
		return frame.View("No bridge executions yet. Configure a route to start.")
	}
	list := m.list
	list.Width, list.Height = width, frame.BodyHeight()
	list.ClampScroll()
	return frame.View(list.View(func(item components.ListItem, selected bool, w int) string {
		log := item.Data.(app.ExecutionLog)
		marker, style := "  ", theme.RowNormalStyle
		if selected {
			marker, style = "▶ ", theme.RowSelectedStyle
		}
		status := theme.ProofStyle.Render(fmt.Sprintf("%4d", log.ExitCode))
		if log.ExitCode != 0 {
			status = theme.ErrorStyle.Render(fmt.Sprintf("%4d", log.ExitCode))
		}
		row := marker + log.Timestamp.Format("15:04:05") + "  " + status + "  " + log.Route
		return style.Width(w).Render(ansi.Truncate(row, w, "…"))
	}))
}

func executionText(log app.ExecutionLog) string {
	return fmt.Sprintf("Route: %s\nScript: %s\nTime: %s\nExit: %d\nDuration: %s\nError: %s\n\nstdout:\n%s\n\nstderr:\n%s", log.Route, log.ScriptPath, log.Timestamp.Format(time.RFC3339), log.ExitCode, formatDuration(log.Duration), log.Error, strings.TrimRight(log.Stdout, "\n"), strings.TrimRight(log.Stderr, "\n"))
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
