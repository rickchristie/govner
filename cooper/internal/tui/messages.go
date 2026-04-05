package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/tui/events"
)

// Re-export shared message types so callers (e.g. main.go) can reference
// them via the tui package for convenience.
type ACLRequestMsg = events.ACLRequestMsg
type AnimTickMsg = events.AnimTickMsg
type ContainerStatsMsg = events.ContainerStatsMsg
type TickMsg = events.TickMsg
type ShutdownCompleteMsg = events.ShutdownCompleteMsg

type BridgeLogMsg = events.BridgeLogMsg
type SquidLogLineMsg = events.SquidLogLineMsg

type ClipboardCaptureMsg = events.ClipboardCaptureMsg
type ClipboardClearMsg = events.ClipboardClearMsg
type ClipboardExpiredMsg = events.ClipboardExpiredMsg
type ClipboardTickMsg = events.ClipboardTickMsg

// ----- Channel listener commands -----

// listenACL returns a tea.Cmd that blocks until a value is received on ch,
// then wraps it as an ACLRequestMsg. The root model re-invokes this after
// each receive to keep listening.
func listenACL(ch <-chan app.ACLRequest) tea.Cmd {
	return func() tea.Msg {
		req, ok := <-ch
		if !ok {
			return nil
		}
		return events.ACLRequestMsg{Request: req}
	}
}

// listenACLDecisions returns a tea.Cmd that blocks until a decision event
// arrives on ch. Used to feed the Blocked/Allowed history tabs.
func listenACLDecisions(ch <-chan app.DecisionEvent) tea.Cmd {
	return func() tea.Msg {
		evt, ok := <-ch
		if !ok {
			return nil
		}
		return events.ACLDecisionMsg{Event: evt}
	}
}

// listenBridgeLogs returns a tea.Cmd that blocks until a log entry arrives
// on ch.
func listenBridgeLogs(ch <-chan app.ExecutionLog) tea.Cmd {
	return func() tea.Msg {
		log, ok := <-ch
		if !ok {
			return nil
		}
		return events.BridgeLogMsg{Log: log}
	}
}

// listenSquidLogs returns a tea.Cmd that blocks until a log line arrives
// on ch.
func listenSquidLogs(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return nil
		}
		return events.SquidLogLineMsg{Line: line}
	}
}

// pollStats returns a tea.Cmd that sleeps for interval, then collects
// container stats via the App and returns them as a ContainerStatsMsg.
func pollStats(a app.App, interval time.Duration) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(interval)
		stats, err := a.ContainerStats()
		if err != nil {
			// Swallow errors; the TUI will simply show stale data.
			return events.ContainerStatsMsg{}
		}
		return events.ContainerStatsMsg{Stats: stats}
	}
}
