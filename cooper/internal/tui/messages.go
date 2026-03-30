package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rickchristie/govner/cooper/internal/bridge"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/proxy"
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

// ----- Channel listener commands -----

// listenACL returns a tea.Cmd that blocks until a value is received on ch,
// then wraps it as an ACLRequestMsg. The root model re-invokes this after
// each receive to keep listening.
func listenACL(ch <-chan proxy.ACLRequest) tea.Cmd {
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
func listenACLDecisions(ch <-chan proxy.DecisionEvent) tea.Cmd {
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
func listenBridgeLogs(ch <-chan bridge.ExecutionLog) tea.Cmd {
	return func() tea.Msg {
		log, ok := <-ch
		if !ok {
			return nil
		}
		return events.BridgeLogMsg{Log: log}
	}
}

// pollStats returns a tea.Cmd that sleeps for interval, then collects
// container stats and returns them as a ContainerStatsMsg.
func pollStats(interval time.Duration) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(interval)
		stats, err := docker.AllContainerStats()
		if err != nil {
			// Swallow errors; the TUI will simply show stale data.
			return events.ContainerStatsMsg{}
		}
		return events.ContainerStatsMsg{Stats: stats}
	}
}
