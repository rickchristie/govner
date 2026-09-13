// Package app defines the App interface -- the boundary between the TUI
// (presentation) and business logic (infrastructure). The TUI depends ONLY
// on this interface; it knows nothing about Docker, Squid, socat, or any
// implementation detail.
package app

import (
	"context"

	"github.com/rickchristie/govner/cooper/internal/clipboard"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/profiles"
)

// App is the interface between the TUI (presentation) and the business logic
// (infrastructure). The TUI depends ONLY on this interface -- it knows nothing
// about Docker, Squid, socat, or any implementation detail.
type App interface {
	ProfileManager
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
	SquidLogs() <-chan string

	// ACL actions
	ApproveRequest(id string)
	DenyRequest(id string)
	PendingRequests() []*PendingRequest
	AllowDomainForSession(domain string) (string, error)
	RevokeDomainForSession(domain string) bool
	IsDomainAllowedForSession(domain string) bool
	SessionAllowedDomains() []string

	// Workload management
	WorkloadStats() ([]WorkloadStat, error)
	StopWorkload(id string) error
	RestartWorkload(id string) error
	ListWorkloads() ([]WorkloadInfo, error)
	IsProxyRunning() bool
	HeaderHealth() HeaderHealth

	// Port forwarding (live reload)
	UpdatePortForwards(rules []config.PortForwardRule) error

	// Bridge routes (live update)
	UpdateBridgeRoutes(routes []config.BridgeRoute) error

	// Settings (live update)
	UpdateSettings(timeoutSecs, blockedLimit, allowedLimit, bridgeLogLimit, clipboardTTLSecs, clipboardMaxBytes int, proxyAlertSound bool) error

	// Clipboard bridge
	CaptureClipboard() (*clipboard.ClipboardEvent, error)
	StageFile(path string) (*clipboard.ClipboardEvent, error)
	ClearClipboard()
	ClipboardSnapshot() *clipboard.StagedSnapshot

	// State
	Config() *config.Config
	CooperDir() string
	StartupWarnings() []string
}

// ProfileManager is the account-state boundary used by the Profiles screen.
// Inputs and results are shared with the CLI; the screen owns only UI state.
type ProfileManager interface {
	ListProfiles(context.Context) ([]profiles.Summary, error)
	SaveProfile(context.Context, profiles.SaveRequest) (profiles.Result, error)
	LoadProfile(context.Context, profiles.LoadRequest) (profiles.Result, error)
	DeleteProfile(context.Context, string, string) error
}

// WorkloadKind identifies the execution role without exposing Docker or QEMU
// details to the TUI.
type WorkloadKind string

const (
	WorkloadProxy WorkloadKind = "proxy"
	WorkloadCLI   WorkloadKind = "cli"
	WorkloadVM    WorkloadKind = "vm"
)

// WorkloadStat is one runtime-neutral resource and health snapshot.
type WorkloadStat struct {
	ID           string
	Kind         WorkloadKind
	Tool         string
	Profile      string
	Workspace    string
	Depth        int
	Status       string
	HealthReason string
	ShellCount   int
	CPUPercent   string
	MemUsage     string
	StorageUsage string
}

// WorkloadInfo holds stable identity and status without resource samples.
type WorkloadInfo struct {
	ID           string
	Kind         WorkloadKind
	Tool         string
	Profile      string
	Depth        int
	Status       string
	WorkspaceDir string
}

// HeaderHealth captures the lightweight runtime health badges shown in the TUI
// header.
type HeaderHealth struct {
	Proxy  bool
	Socat  bool
	Bridge bool
}
