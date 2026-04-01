// Package app defines the App interface -- the boundary between the TUI
// (presentation) and business logic (infrastructure). The TUI depends ONLY
// on this interface; it knows nothing about Docker, Squid, socat, or any
// implementation detail.
package app

import (
	"context"

	"github.com/rickchristie/govner/cooper/internal/config"
)

// App is the interface between the TUI (presentation) and the business logic
// (infrastructure). The TUI depends ONLY on this interface -- it knows nothing
// about Docker, Squid, socat, or any implementation detail.
type App interface {
	// Lifecycle

	// Start executes the startup sequence (networks, proxy, CA verification,
	// bridge, version check, ACL listener). The onProgress callback is invoked
	// after each step completes (or fails). step is 0-based, total is the
	// number of steps, name is a human-readable label, and err is non-nil on
	// failure. Start blocks until all steps complete or an error occurs.
	Start(ctx context.Context, onProgress func(step int, total int, name string, err error)) error

	// Stop performs a graceful shutdown: stops the ACL listener, bridge
	// server, all barrel containers, proxy, and closes loggers.
	Stop() error

	// Event channels -- TUI subscribes to these for live updates.
	// All channels are closed when Stop is called.
	ACLRequests() <-chan ACLRequest
	ACLDecisions() <-chan DecisionEvent
	BridgeLogs() <-chan ExecutionLog

	// ACL actions
	ApproveRequest(id string)
	DenyRequest(id string)
	PendingRequests() []*PendingRequest

	// Container management
	ContainerStats() ([]ContainerStat, error)
	StopContainer(name string) error
	RestartContainer(name string) error
	ListContainers() ([]ContainerInfo, error)
	IsProxyRunning() bool

	// Port forwarding (live reload)
	UpdatePortForwards(rules []config.PortForwardRule) error

	// Bridge routes (live update)
	UpdateBridgeRoutes(routes []config.BridgeRoute) error

	// Settings (live update)
	UpdateSettings(timeoutSecs, blockedLimit, allowedLimit, bridgeLogLimit int) error

	// State
	Config() *config.Config
	CooperDir() string
	StartupWarnings() []string
}

// ContainerProxy is the well-known name for the proxy container.
// The TUI uses this for sorting (proxy always first) without importing docker.
const ContainerProxy = "cooper-proxy"

// ContainerStat holds resource usage statistics for a running container.
// This is the app-level type; it is mapped from docker.ContainerStat
// internally so the TUI never imports the docker package.
type ContainerStat struct {
	Name       string
	CPUPercent string
	MemUsage   string
}

// ContainerInfo holds identification and status for a container.
// Mapped from docker.BarrelInfo internally.
type ContainerInfo struct {
	Name         string
	Status       string
	WorkspaceDir string
}
