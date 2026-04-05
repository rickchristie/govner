// Package events defines shared message types used by both the root tui
// package and the tab sub-model packages (proxymon, containers, loading, etc.).
// Keeping them in a leaf package avoids import cycles and ensures that the
// root Update switch and each sub-model's Update switch match on the exact
// same Go type, preventing silent type-assertion failures.
package events

import (
	"github.com/rickchristie/govner/cooper/internal/app"
)

// ACLRequestMsg wraps a new pending ACL request arriving from the proxy.
type ACLRequestMsg struct {
	Request app.ACLRequest
}

// AnimTickMsg is sent every CountdownTickInterval (100 ms) for smooth
// animation updates (countdown bars, status pulses, barrel roll, etc.).
type AnimTickMsg struct{}

// ContainerStatsMsg carries a periodic snapshot of container resource usage.
type ContainerStatsMsg struct {
	Stats []app.ContainerStat
}

// TickMsg is sent every UITickInterval (1 s) for general UI refresh
// (timestamps, stats, etc.).
type TickMsg struct{}

// ShutdownCompleteMsg signals that graceful shutdown has finished.
type ShutdownCompleteMsg struct{}

// ShutdownStepCompleteMsg signals that a shutdown step completed.
type ShutdownStepCompleteMsg struct{ Index int }

// ShutdownStepErrorMsg signals that a shutdown step failed.
type ShutdownStepErrorMsg struct {
	Index int
	Err   error
}

// ACLDecisionMsg wraps a resolved ACL decision for the history tabs.
type ACLDecisionMsg struct {
	Event app.DecisionEvent
}

// BridgeLogMsg wraps a new execution log entry from the bridge server.
type BridgeLogMsg struct {
	Log app.ExecutionLog
}

// ClipboardCaptureMsg carries the result of a clipboard capture attempt.
type ClipboardCaptureMsg struct {
	Event *app.ClipboardEvent
	Err   error
}

// ClipboardClearMsg signals that the clipboard has been cleared by the user.
type ClipboardClearMsg struct{}

// ClipboardExpiredMsg signals that the staged clipboard content has expired.
type ClipboardExpiredMsg struct{}

// SquidLogLineMsg wraps a new line from the Squid access log.
type SquidLogLineMsg struct {
	Line string
}

// ClipboardTickMsg is sent on each UI tick to check clipboard TTL expiry.
type ClipboardTickMsg struct{}
