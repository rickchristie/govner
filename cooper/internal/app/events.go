package app

import (
	"github.com/rickchristie/govner/cooper/internal/bridge"
	"github.com/rickchristie/govner/cooper/internal/clipboard"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/proxy"
)

// Re-export event types so the TUI can import from the app package instead
// of importing infrastructure packages (proxy, bridge, config, proof)
// directly. This keeps the TUI decoupled from implementation details.

// ACLRequest is an incoming ACL query from the Squid external ACL helper.
type ACLRequest = proxy.ACLRequest

// DecisionEvent is emitted when a request is resolved (approved, denied, or timed out).
type DecisionEvent = proxy.DecisionEvent

// ACLDecision represents the outcome of an ACL request.
type ACLDecision = proxy.ACLDecision

// PendingRequest tracks an in-flight ACL request awaiting user decision.
type PendingRequest = proxy.PendingRequest

// ExecutionLog captures the result of a single bridge script execution.
type ExecutionLog = bridge.ExecutionLog

// PortForwardRule describes a port forwarding rule.
type PortForwardRule = config.PortForwardRule

// BridgeRoute maps an API path to a host script for the execution bridge.
type BridgeRoute = config.BridgeRoute

// Re-export ACL decision constants so the TUI can reference them via app.DecisionAllow, etc.
const (
	DecisionPending = proxy.DecisionPending
	DecisionAllow   = proxy.DecisionAllow
	DecisionDeny    = proxy.DecisionDeny
	DecisionTimeout = proxy.DecisionTimeout
)

// Clipboard types re-exported for TUI consumption.
type ClipboardEvent = clipboard.ClipboardEvent
type ClipboardState = clipboard.ClipboardState
type StagedSnapshot = clipboard.StagedSnapshot
type ClipboardObject = clipboard.ClipboardObject
type ClipboardVariant = clipboard.ClipboardVariant

// Clipboard state constants.
const (
	ClipboardEmpty   = clipboard.ClipboardEmpty
	ClipboardStaged  = clipboard.ClipboardStaged
	ClipboardExpired = clipboard.ClipboardExpired
	ClipboardFailed  = clipboard.ClipboardFailed
)
