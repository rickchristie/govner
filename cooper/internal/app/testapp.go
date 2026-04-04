package app

import (
	"context"

	"github.com/rickchristie/govner/cooper/internal/clipboard"
	"github.com/rickchristie/govner/cooper/internal/config"
)

// Compile-time check that TestApp satisfies App.
var _ App = (*TestApp)(nil)

// TestApp is a minimal implementation of App for use in tui-test mode.
// It provides mock channels and no-op infrastructure methods, allowing the
// TUI to render without Docker or any real services.
type TestApp struct {
	cfg       *config.Config
	aclCh     chan ACLRequest
	decisionCh chan DecisionEvent
	bridgeCh  chan ExecutionLog
	proxyUp   bool
}

// NewTestApp creates a TestApp with the given config and mock channels.
// The caller populates the channels with test data as needed.
func NewTestApp(cfg *config.Config, aclCh chan ACLRequest, bridgeCh chan ExecutionLog) *TestApp {
	return &TestApp{
		cfg:        cfg,
		aclCh:      aclCh,
		decisionCh: make(chan DecisionEvent),
		bridgeCh:   bridgeCh,
		proxyUp:    true,
	}
}

func (t *TestApp) Start(_ context.Context, _ func(int, int, string, error)) error { return nil }
func (t *TestApp) Stop() error                                                     { return nil }

func (t *TestApp) ACLRequests() <-chan ACLRequest    { return t.aclCh }
func (t *TestApp) ACLDecisions() <-chan DecisionEvent { return t.decisionCh }
func (t *TestApp) BridgeLogs() <-chan ExecutionLog   { return t.bridgeCh }

func (t *TestApp) ApproveRequest(_ string)         {}
func (t *TestApp) DenyRequest(_ string)             {}
func (t *TestApp) PendingRequests() []*PendingRequest { return nil }

func (t *TestApp) ContainerStats() ([]ContainerStat, error)   { return nil, nil }
func (t *TestApp) StopContainer(_ string) error                { return nil }
func (t *TestApp) RestartContainer(_ string) error             { return nil }
func (t *TestApp) ListContainers() ([]ContainerInfo, error)    { return nil, nil }
func (t *TestApp) IsProxyRunning() bool                        { return t.proxyUp }

func (t *TestApp) UpdatePortForwards(_ []config.PortForwardRule) error { return nil }
func (t *TestApp) UpdateBridgeRoutes(_ []config.BridgeRoute) error     { return nil }
func (t *TestApp) UpdateSettings(_, _, _, _ int) error                 { return nil }

func (t *TestApp) CaptureClipboard() (*clipboard.ClipboardEvent, error)  { return nil, nil }
func (t *TestApp) StageFile(_ string) (*clipboard.ClipboardEvent, error) { return nil, nil }
func (t *TestApp) ClearClipboard()                                       {}
func (t *TestApp) ClipboardSnapshot() *clipboard.StagedSnapshot          { return nil }

func (t *TestApp) Config() *config.Config    { return t.cfg }
func (t *TestApp) CooperDir() string          { return "/tmp/cooper-test" }
func (t *TestApp) StartupWarnings() []string  { return nil }
