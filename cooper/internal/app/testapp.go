package app

import (
	"context"
	"sort"
	"sync"

	"github.com/rickchristie/govner/cooper/internal/clipboard"
	"github.com/rickchristie/govner/cooper/internal/config"
)

// Compile-time check that TestApp satisfies App.
var _ App = (*TestApp)(nil)

// TestApp is a minimal implementation of App for use in tui-test mode.
// It provides mock channels and no-op infrastructure methods, allowing the
// TUI to render without Docker or any real services.
type TestApp struct {
	cfg        *config.Config
	aclCh      chan ACLRequest
	decisionCh chan DecisionEvent
	bridgeCh   chan ExecutionLog
	squidLogCh chan string
	proxyUp    bool
	socatUp    bool
	bridgeUp   bool
	workloads  []WorkloadStat

	sessionMu      sync.Mutex
	sessionDomains map[string]struct{}
}

// NewTestApp creates a TestApp with the given config and mock channels.
// The caller populates the channels with test data as needed.
func NewTestApp(cfg *config.Config, aclCh chan ACLRequest, bridgeCh chan ExecutionLog) *TestApp {
	return &TestApp{
		cfg:        cfg,
		aclCh:      aclCh,
		decisionCh: make(chan DecisionEvent),
		bridgeCh:   bridgeCh,
		squidLogCh: make(chan string, 1024),
		proxyUp:    true,
		socatUp:    true,
		bridgeUp:   true,
		workloads: []WorkloadStat{
			{ID: "cooper-proxy", Kind: WorkloadProxy, Status: "Running", CPUPercent: "0.4%", MemUsage: "42MiB / 256MiB", StorageUsage: "--"},
			{ID: "barrel-demo-claude", Kind: WorkloadCLI, Tool: "claude", Workspace: "/work/demo", Status: "Running", ShellCount: 2, CPUPercent: "1.2%", MemUsage: "380MiB / 4GiB", StorageUsage: "18MiB"},
			{ID: "cooper-vm-govner-codex-aabbccddeeff", Kind: WorkloadVM, Tool: "codex", Workspace: "/work/govner", Depth: 1, Status: "Running", ShellCount: 1, CPUPercent: "7.8%", MemUsage: "4.2GiB / 12.8GiB", StorageUsage: "3.1GiB"},
		},
		sessionDomains: make(map[string]struct{}),
	}
}

func (t *TestApp) Start(_ context.Context, _ func(int, int, string, error)) error { return nil }
func (t *TestApp) Stop() error                                                    { return nil }

func (t *TestApp) ACLRequests() <-chan ACLRequest     { return t.aclCh }
func (t *TestApp) ACLDecisions() <-chan DecisionEvent { return t.decisionCh }
func (t *TestApp) BridgeLogs() <-chan ExecutionLog    { return t.bridgeCh }
func (t *TestApp) SquidLogs() <-chan string           { return t.squidLogCh }

func (t *TestApp) ApproveRequest(_ string)            {}
func (t *TestApp) DenyRequest(_ string)               {}
func (t *TestApp) PendingRequests() []*PendingRequest { return nil }
func (t *TestApp) AllowDomainForSession(domain string) (string, error) {
	t.sessionMu.Lock()
	defer t.sessionMu.Unlock()
	normalized := normalizeSessionDomainForTestApp(domain)
	t.sessionDomains[normalized] = struct{}{}
	return normalized, nil
}
func (t *TestApp) RevokeDomainForSession(domain string) bool {
	t.sessionMu.Lock()
	defer t.sessionMu.Unlock()
	normalized := normalizeSessionDomainForTestApp(domain)
	if _, ok := t.sessionDomains[normalized]; !ok {
		return false
	}
	delete(t.sessionDomains, normalized)
	return true
}
func (t *TestApp) IsDomainAllowedForSession(domain string) bool {
	t.sessionMu.Lock()
	defer t.sessionMu.Unlock()
	_, ok := t.sessionDomains[normalizeSessionDomainForTestApp(domain)]
	return ok
}
func (t *TestApp) SessionAllowedDomains() []string {
	t.sessionMu.Lock()
	defer t.sessionMu.Unlock()
	domains := make([]string, 0, len(t.sessionDomains))
	for domain := range t.sessionDomains {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	return domains
}

func (t *TestApp) WorkloadStats() ([]WorkloadStat, error) {
	t.sessionMu.Lock()
	defer t.sessionMu.Unlock()
	return append([]WorkloadStat(nil), t.workloads...), nil
}
func (t *TestApp) StopWorkload(id string) error {
	t.sessionMu.Lock()
	defer t.sessionMu.Unlock()
	filtered := t.workloads[:0]
	for _, workload := range t.workloads {
		if workload.ID != id {
			filtered = append(filtered, workload)
		}
	}
	t.workloads = filtered
	return nil
}
func (t *TestApp) RestartWorkload(_ string) error { return nil }
func (t *TestApp) ListWorkloads() ([]WorkloadInfo, error) {
	t.sessionMu.Lock()
	defer t.sessionMu.Unlock()
	result := make([]WorkloadInfo, 0, len(t.workloads))
	for _, workload := range t.workloads {
		result = append(result, WorkloadInfo{
			ID: workload.ID, Kind: workload.Kind, Tool: workload.Tool,
			Depth: workload.Depth, Status: workload.Status, WorkspaceDir: workload.Workspace,
		})
	}
	return result, nil
}
func (t *TestApp) IsProxyRunning() bool { return t.proxyUp }
func (t *TestApp) HeaderHealth() HeaderHealth {
	return HeaderHealth{Proxy: t.proxyUp, Socat: t.socatUp, Bridge: t.bridgeUp}
}

func (t *TestApp) UpdatePortForwards(_ []config.PortForwardRule) error { return nil }
func (t *TestApp) UpdateBridgeRoutes(_ []config.BridgeRoute) error     { return nil }
func (t *TestApp) UpdateSettings(_, _, _, _, _, _ int, _ bool) error   { return nil }

func (t *TestApp) CaptureClipboard() (*clipboard.ClipboardEvent, error)  { return nil, nil }
func (t *TestApp) StageFile(_ string) (*clipboard.ClipboardEvent, error) { return nil, nil }
func (t *TestApp) ClearClipboard()                                       {}
func (t *TestApp) ClipboardSnapshot() *clipboard.StagedSnapshot          { return nil }

func (t *TestApp) Config() *config.Config    { return t.cfg }
func (t *TestApp) CooperDir() string         { return "/tmp/cooper-test" }
func (t *TestApp) StartupWarnings() []string { return nil }
