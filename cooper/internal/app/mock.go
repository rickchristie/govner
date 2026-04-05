package app

import (
	"context"
	"sync"

	"github.com/rickchristie/govner/cooper/internal/clipboard"
	"github.com/rickchristie/govner/cooper/internal/config"
)

// Compile-time check that MockApp satisfies App.
var _ App = (*MockApp)(nil)

// MockApp implements the App interface with controllable behavior for unit
// testing the TUI without Docker or any real infrastructure. Tests inject
// events via the public fields and verify actions through recorded calls.
type MockApp struct {
	mu sync.Mutex

	cfg       *config.Config
	cooperDir string

	aclRequests  chan ACLRequest
	aclDecisions chan DecisionEvent
	bridgeLogs   chan ExecutionLog
	squidLogs    chan string

	// Controllable return values.
	StartErr            error
	StopErr             error
	ContainerStatsVal   []ContainerStat
	ContainerStatsErr   error
	ListContainersVal   []ContainerInfo
	ListContainersErr   error
	StopContainerErr    error
	RestartContainerErr error
	ProxyRunning        bool

	// Clipboard controllable return values.
	CaptureClipboardResult *clipboard.ClipboardEvent
	CaptureClipboardErr    error
	StageFileResult        *clipboard.ClipboardEvent
	StageFileErr           error
	ClipboardSnapshotVal   *clipboard.StagedSnapshot

	// Recorded calls for assertions.
	ApprovedIDs          []string
	DeniedIDs            []string
	StoppedContainers    []string
	RestartedContainers  []string
	UpdatedPortForwards  []config.PortForwardRule
	UpdatedBridgeRoutes  []config.BridgeRoute
	UpdatedSettings      []SettingsUpdate
	StartCalled          bool
	StopCalled           bool
	CapturedClipboard    bool
	StagedFiles          []string
	ClearedClipboard     bool

	startupWarnings []string
	pendingRequests []*PendingRequest
}

// SettingsUpdate records a call to UpdateSettings.
type SettingsUpdate struct {
	TimeoutSecs       int
	BlockedLimit      int
	AllowedLimit      int
	BridgeLogLimit    int
	ClipboardTTLSecs  int
	ClipboardMaxBytes int
}

// NewMockApp creates a MockApp with buffered channels and sensible defaults.
func NewMockApp(cfg *config.Config, cooperDir string) *MockApp {
	return &MockApp{
		cfg:          cfg,
		cooperDir:    cooperDir,
		aclRequests:  make(chan ACLRequest, 256),
		aclDecisions: make(chan DecisionEvent, 256),
		bridgeLogs:   make(chan ExecutionLog, 256),
		squidLogs:    make(chan string, 1024),
		ProxyRunning: true,
	}
}

// ----- Lifecycle -----

func (m *MockApp) Start(_ context.Context, onProgress func(step int, total int, name string, err error)) error {
	m.mu.Lock()
	m.StartCalled = true
	m.mu.Unlock()

	if m.StartErr != nil {
		if onProgress != nil {
			onProgress(0, 1, "Mock start", m.StartErr)
		}
		return m.StartErr
	}
	if onProgress != nil {
		onProgress(0, 1, "Mock start", nil)
	}
	return nil
}

func (m *MockApp) Stop() error {
	m.mu.Lock()
	m.StopCalled = true
	m.mu.Unlock()
	return m.StopErr
}

// ----- Event channels -----

func (m *MockApp) ACLRequests() <-chan ACLRequest     { return m.aclRequests }
func (m *MockApp) ACLDecisions() <-chan DecisionEvent { return m.aclDecisions }
func (m *MockApp) BridgeLogs() <-chan ExecutionLog    { return m.bridgeLogs }
func (m *MockApp) SquidLogs() <-chan string            { return m.squidLogs }

// InjectACLRequest sends a request on the ACL requests channel.
func (m *MockApp) InjectACLRequest(req ACLRequest) {
	m.aclRequests <- req
}

// InjectACLDecision sends a decision on the ACL decisions channel.
func (m *MockApp) InjectACLDecision(evt DecisionEvent) {
	m.aclDecisions <- evt
}

// InjectBridgeLog sends a log entry on the bridge logs channel.
func (m *MockApp) InjectBridgeLog(entry ExecutionLog) {
	m.bridgeLogs <- entry
}

// ----- ACL actions -----

func (m *MockApp) ApproveRequest(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ApprovedIDs = append(m.ApprovedIDs, id)
}

func (m *MockApp) DenyRequest(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DeniedIDs = append(m.DeniedIDs, id)
}

func (m *MockApp) PendingRequests() []*PendingRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pendingRequests
}

// SetPendingRequests sets the list returned by PendingRequests.
func (m *MockApp) SetPendingRequests(prs []*PendingRequest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pendingRequests = prs
}

// ----- Container management -----

func (m *MockApp) ContainerStats() ([]ContainerStat, error) {
	return m.ContainerStatsVal, m.ContainerStatsErr
}

func (m *MockApp) StopContainer(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.StoppedContainers = append(m.StoppedContainers, name)
	return m.StopContainerErr
}

func (m *MockApp) RestartContainer(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RestartedContainers = append(m.RestartedContainers, name)
	return m.RestartContainerErr
}

func (m *MockApp) ListContainers() ([]ContainerInfo, error) {
	return m.ListContainersVal, m.ListContainersErr
}

func (m *MockApp) IsProxyRunning() bool {
	return m.ProxyRunning
}

// ----- Port forwarding -----

func (m *MockApp) UpdatePortForwards(rules []config.PortForwardRule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.UpdatedPortForwards = rules
	m.cfg.PortForwardRules = rules
	return nil
}

// ----- Bridge routes -----

func (m *MockApp) UpdateBridgeRoutes(routes []config.BridgeRoute) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.UpdatedBridgeRoutes = routes
	m.cfg.BridgeRoutes = routes
	return nil
}

// ----- Settings -----

func (m *MockApp) UpdateSettings(timeoutSecs, blockedLimit, allowedLimit, bridgeLogLimit, clipboardTTLSecs, clipboardMaxBytes int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.UpdatedSettings = append(m.UpdatedSettings, SettingsUpdate{
		TimeoutSecs:       timeoutSecs,
		BlockedLimit:      blockedLimit,
		AllowedLimit:      allowedLimit,
		BridgeLogLimit:    bridgeLogLimit,
		ClipboardTTLSecs:  clipboardTTLSecs,
		ClipboardMaxBytes: clipboardMaxBytes,
	})
	m.cfg.MonitorTimeoutSecs = timeoutSecs
	m.cfg.BlockedHistoryLimit = blockedLimit
	m.cfg.AllowedHistoryLimit = allowedLimit
	m.cfg.BridgeLogLimit = bridgeLogLimit
	m.cfg.ClipboardTTLSecs = clipboardTTLSecs
	m.cfg.ClipboardMaxBytes = clipboardMaxBytes
	return nil
}

// ----- Clipboard -----

func (m *MockApp) CaptureClipboard() (*clipboard.ClipboardEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CapturedClipboard = true
	return m.CaptureClipboardResult, m.CaptureClipboardErr
}

func (m *MockApp) StageFile(path string) (*clipboard.ClipboardEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.StagedFiles = append(m.StagedFiles, path)
	return m.StageFileResult, m.StageFileErr
}

func (m *MockApp) ClearClipboard() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ClearedClipboard = true
}

func (m *MockApp) ClipboardSnapshot() *clipboard.StagedSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ClipboardSnapshotVal
}

// ----- State -----

func (m *MockApp) Config() *config.Config { return m.cfg }
func (m *MockApp) CooperDir() string      { return m.cooperDir }

func (m *MockApp) StartupWarnings() []string {
	return m.startupWarnings
}

// SetStartupWarnings sets the warnings returned by StartupWarnings.
func (m *MockApp) SetStartupWarnings(warnings []string) {
	m.startupWarnings = warnings
}
