package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rickchristie/govner/cooper/internal/bridge"
	"github.com/rickchristie/govner/cooper/internal/clipboard"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/fontsync"
	"github.com/rickchristie/govner/cooper/internal/logging"

	"github.com/rickchristie/govner/cooper/internal/proxy"
)

// Compile-time check that CooperApp satisfies App.
var _ App = (*CooperApp)(nil)

// CooperApp is the concrete implementation of the App interface. It owns all
// infrastructure (Docker, proxy, bridge) and exposes a clean interface for
// the TUI or any other consumer.
type CooperApp struct {
	cfg        *config.Config
	cooperDir  string

	aclListener      *proxy.ACLListener
	bridgeServer     *bridge.BridgeServer
	clipboardManager *clipboard.Manager
	clipboardReader  *clipboard.LinuxReader

	aclLogger    *logging.Logger
	bridgeLogger *logging.Logger

	// Forwarding channels with logging taps. The TUI reads from these;
	// internal goroutines forward from the raw infrastructure channels.
	aclFwd      chan proxy.ACLRequest
	decisionFwd chan proxy.DecisionEvent
	bridgeFwd   chan bridge.ExecutionLog

	startupWarnings []string
}

// NewCooperApp creates a new CooperApp with the given configuration.
// The app is not started; call Start to begin the startup sequence.
func NewCooperApp(cfg *config.Config, cooperDir string) *CooperApp {
	logDir := filepath.Join(cooperDir, "logs")
	ttl := time.Duration(cfg.ClipboardTTLSecs) * time.Second
	mgr := clipboard.NewManager(ttl, cfg.ClipboardMaxBytes)
	mgr.SetCooperDir(cooperDir)
	return &CooperApp{
		cfg:              cfg,
		cooperDir:        cooperDir,
		clipboardManager: mgr,
		clipboardReader:  clipboard.NewLinuxReader(os.Getenv),
		aclLogger:        logging.NewLogger(logDir, "acl", 10*1024*1024, 10),
		bridgeLogger:     logging.NewLogger(logDir, "bridge", 10*1024*1024, 10),
		aclFwd:           make(chan proxy.ACLRequest, 256),
		decisionFwd:      make(chan proxy.DecisionEvent, 1024),
		bridgeFwd:        make(chan bridge.ExecutionLog, 256),
	}
}

// totalSteps is the number of startup steps reported via onProgress.
const totalSteps = 8

// Start executes the startup sequence. It blocks until all steps complete
// or the context is cancelled. The onProgress callback is invoked after
// each step with (stepIndex, totalSteps, stepName, err). If err is non-nil,
// Start returns immediately with that error.
func (a *CooperApp) Start(ctx context.Context, onProgress func(step int, total int, name string, err error)) error {
	report := func(step int, name string, err error) {
		if onProgress != nil {
			onProgress(step, totalSteps, name, err)
		}
	}

	homeDir, _ := os.UserHomeDir()

	// Pre-check: verify clipboard prerequisites before anything else.
	// Refuse to start if clipboard host tools are missing.
	if a.clipboardReader != nil {
		if err := a.clipboardReader.CheckPrerequisites(ctx); err != nil {
			report(0, "Check clipboard prerequisites", err)
			return err
		}
	}

	// Step 0: Create Docker networks.
	if err := docker.EnsureNetworks(); err != nil {
		report(0, "Create networks", err)
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	report(0, "Create networks", nil)

	// Step 1: Start proxy container.
	if err := docker.StartProxy(a.cfg, a.cooperDir); err != nil {
		report(1, "Start proxy", err)
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	report(1, "Start proxy", nil)

	// Step 2: Verify CA certificate exists.
	if !config.CAExists(a.cooperDir) {
		err := fmt.Errorf("CA certificate not found, run 'cooper build' first")
		report(2, "Verify CA certificate", err)
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	report(2, "Verify CA certificate", nil)

	// Step 3: Start execution bridge.
	// Bind to both the cooper-external gateway and the default bridge gateway.
	// host.docker.internal resolves to the default bridge gateway, which may
	// differ from the cooper-external gateway.
	var gatewayIPs []string
	if ip, err := docker.GetGatewayIP(docker.NetworkExternal); err == nil {
		gatewayIPs = append(gatewayIPs, ip)
	}
	if ip, err := docker.GetGatewayIP("bridge"); err == nil {
		gatewayIPs = append(gatewayIPs, ip)
	}
	if len(gatewayIPs) == 0 {
		err := fmt.Errorf("could not discover any Docker gateway IP\n" +
			"Bridge won't be reachable from containers. Check that Docker networks exist")
		report(3, "Start bridge", err)
		return err
	}
	a.bridgeServer = bridge.NewBridgeServer(a.cfg.BridgeRoutes, a.cfg.BridgePort, gatewayIPs)
	// Install clipboard handler on the bridge before starting.
	if a.clipboardManager != nil {
		clipHandler := clipboard.NewHandler(a.clipboardManager)
		a.bridgeServer.SetClipboardHandler(clipHandler)
	}
	if err := a.bridgeServer.Start(); err != nil {
		report(3, "Start bridge", err)
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	report(3, "Start bridge", nil)

	// Step 4: Ensure Playwright support dirs and sync fonts (best-effort).
	if err := ensurePlaywrightSupportDirs(a.cooperDir); err != nil {
		report(4, "Playwright support ready", err)
		return err
	}
	fontResult, fontErr := fontsync.SyncLinuxFonts(homeDir, a.cooperDir)
	if fontErr != nil {
		// Font sync failure is non-fatal — add to warnings.
		a.startupWarnings = append(a.startupWarnings, fmt.Sprintf("Font sync failed: %v", fontErr))
	} else {
		for _, w := range fontResult.Warnings {
			a.startupWarnings = append(a.startupWarnings, fmt.Sprintf("Font sync: %s", w))
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	report(4, "Playwright support ready", nil)

	// Step 5: CLI image version check (informational, non-blocking).
	a.startupWarnings = append(a.startupWarnings, checkToolVersions(a.cfg)...)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	report(5, "Check tool versions", nil)

	// Step 6: Start ACL listener.
	socketPath := filepath.Join(a.cooperDir, "run", "acl.sock")
	if err := os.MkdirAll(filepath.Dir(socketPath), 0755); err != nil {
		err = fmt.Errorf("create run dir: %w", err)
		report(6, "Start ACL listener", err)
		return err
	}
	timeout := time.Duration(a.cfg.MonitorTimeoutSecs) * time.Second
	a.aclListener = proxy.NewACLListener(socketPath, timeout)
	if err := a.aclListener.Start(); err != nil {
		report(6, "Start ACL listener", err)
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	report(6, "Start ACL listener", nil)

	// Step 7: Wire forwarding channels and load persisted bridge routes.
	a.wireChannels()
	a.loadPersistedBridgeRoutes()
	report(7, "Ready", nil)

	return nil
}

// wireChannels starts goroutines that forward events from infrastructure
// channels to the TUI-facing channels, with logging taps.
func (a *CooperApp) wireChannels() {
	if a.aclListener != nil {
		// Forward ACL requests with logging tap.
		aclSrc := a.aclListener.RequestChan()
		go func() {
			for req := range aclSrc {
				a.aclLogger.Log(fmt.Sprintf("domain=%s port=%s src=%s", req.Domain, req.Port, req.SourceIP))
				a.aclFwd <- req
			}
			close(a.aclFwd)
		}()

		// Forward ACL decisions with logging tap.
		decisionSrc := a.aclListener.DecisionChan()
		go func() {
			for evt := range decisionSrc {
				a.aclLogger.Log(fmt.Sprintf("decision=%s domain=%s port=%s src=%s",
					evt.Reason, evt.Request.Domain, evt.Request.Port, evt.Request.SourceIP))
				a.decisionFwd <- evt
			}
			close(a.decisionFwd)
		}()
	}

	if a.bridgeServer != nil {
		// Forward bridge execution logs with logging tap.
		bridgeSrc := a.bridgeServer.LogChan()
		go func() {
			for entry := range bridgeSrc {
				a.bridgeLogger.Log(fmt.Sprintf("route=%s script=%s exit=%d duration=%s err=%s",
					entry.Route, entry.ScriptPath, entry.ExitCode, entry.Duration, entry.Error))
				a.bridgeFwd <- entry
			}
			close(a.bridgeFwd)
		}()
	}
}

// loadPersistedBridgeRoutes loads saved bridge routes from disk and merges
// them with the config defaults. If saved routes exist, they override the
// config and are applied to the running bridge server.
func (a *CooperApp) loadPersistedBridgeRoutes() {
	savedRoutes, err := bridge.LoadBridgeRoutes(a.cooperDir)
	if err != nil {
		// Non-fatal; log and continue with config defaults.
		return
	}
	if len(savedRoutes) > 0 {
		a.cfg.BridgeRoutes = savedRoutes
		if a.bridgeServer != nil {
			a.bridgeServer.UpdateRoutes(savedRoutes)
		}
	}
}

// Stop performs a graceful shutdown of all infrastructure components.
func (a *CooperApp) Stop() error {
	return a.StopWithProgress(nil)
}

// StopWithProgress performs a graceful shutdown, calling onStep(i) after
// each step completes so the caller can drive a progress UI.
// Steps: 0=ACL, 1=bridge, 2=containers, 3=proxy, 4=sealed.
func (a *CooperApp) StopWithProgress(onStep func(int)) error {
	if onStep == nil {
		onStep = func(int) {}
	}

	// Step 0: Stop ACL listener.
	if a.aclListener != nil {
		a.aclListener.Stop()
	}
	onStep(0)

	// Step 1: Stop bridge server.
	if a.bridgeServer != nil {
		a.bridgeServer.Stop()
	}
	onStep(1)

	// Step 2: Stop all barrel containers.
	barrels, _ := docker.ListBarrels()
	for _, b := range barrels {
		docker.StopBarrel(b.Name)
	}
	onStep(2)

	// Step 3: Stop proxy container.
	docker.StopProxy()
	onStep(3)

	// Step 4: Sealed — close loggers.
	a.bridgeLogger.Close()
	a.aclLogger.Close()
	onStep(4)

	return nil
}

// ----- Event channels -----

// ACLRequests returns the channel that emits incoming ACL requests.
func (a *CooperApp) ACLRequests() <-chan ACLRequest {
	return a.aclFwd
}

// ACLDecisions returns the channel that emits ACL decision events.
func (a *CooperApp) ACLDecisions() <-chan DecisionEvent {
	return a.decisionFwd
}

// BridgeLogs returns the channel that emits bridge execution logs.
func (a *CooperApp) BridgeLogs() <-chan ExecutionLog {
	return a.bridgeFwd
}

// ----- ACL actions -----

// ApproveRequest sets the decision for the given request ID to Allow.
func (a *CooperApp) ApproveRequest(id string) {
	if a.aclListener != nil {
		a.aclListener.Approve(id)
	}
}

// DenyRequest sets the decision for the given request ID to Deny.
func (a *CooperApp) DenyRequest(id string) {
	if a.aclListener != nil {
		a.aclListener.Deny(id)
	}
}

// PendingRequests returns a snapshot of all currently pending ACL requests.
func (a *CooperApp) PendingRequests() []*PendingRequest {
	if a.aclListener != nil {
		return a.aclListener.PendingRequests()
	}
	return nil
}

// ----- Container management -----

// ContainerStats returns CPU and memory statistics for all running
// cooper containers (proxy + barrels).
func (a *CooperApp) ContainerStats() ([]ContainerStat, error) {
	dockerStats, err := docker.AllContainerStats()
	if err != nil {
		return nil, err
	}
	stats := make([]ContainerStat, len(dockerStats))
	for i, ds := range dockerStats {
		stats[i] = ContainerStat{
			Name:       ds.Name,
			CPUPercent: ds.CPUPercent,
			MemUsage:   ds.MemUsage,
		}
	}
	return stats, nil
}

// StopContainer stops and removes a barrel container by name.
func (a *CooperApp) StopContainer(name string) error {
	return docker.StopBarrel(name)
}

// RestartContainer restarts a barrel container by name.
func (a *CooperApp) RestartContainer(name string) error {
	return docker.RestartBarrel(name)
}

// ListContainers returns information about all running barrel containers.
func (a *CooperApp) ListContainers() ([]ContainerInfo, error) {
	barrels, err := docker.ListBarrels()
	if err != nil {
		return nil, err
	}
	infos := make([]ContainerInfo, len(barrels))
	for i, b := range barrels {
		infos[i] = ContainerInfo{
			Name:         b.Name,
			Status:       b.Status,
			WorkspaceDir: b.WorkspaceDir,
		}
	}
	return infos, nil
}

// IsProxyRunning checks whether the proxy container is currently running.
func (a *CooperApp) IsProxyRunning() bool {
	running, err := docker.IsProxyRunning()
	if err != nil {
		return false
	}
	return running
}

// ----- Port forwarding -----

// UpdatePortForwards validates the new rules, reloads socat in all
// containers, and persists the updated configuration.
func (a *CooperApp) UpdatePortForwards(rules []config.PortForwardRule) error {
	// Validate.
	candidate := *a.cfg
	candidate.PortForwardRules = rules
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Reload socat (writes socat-rules.json + signals containers).
	if err := docker.ReloadSocat(a.cooperDir, a.cfg.BridgePort, rules); err != nil {
		return fmt.Errorf("reload failed: %w", err)
	}

	// Persist config.json.
	cfgCopy := *a.cfg
	cfgCopy.PortForwardRules = rules
	cfgPath := filepath.Join(a.cooperDir, "config.json")
	if err := config.SaveConfig(cfgPath, &cfgCopy); err != nil {
		return fmt.Errorf("config save failed: %w", err)
	}

	// Update in-memory config on success.
	a.cfg.PortForwardRules = rules
	return nil
}

// ----- Bridge routes -----

// UpdateBridgeRoutes hot-swaps bridge routes on the running server and
// persists them to disk.
func (a *CooperApp) UpdateBridgeRoutes(routes []config.BridgeRoute) error {
	if a.bridgeServer != nil {
		a.bridgeServer.UpdateRoutes(routes)
	}
	if err := bridge.SaveBridgeRoutes(a.cooperDir, routes); err != nil {
		return fmt.Errorf("persist bridge routes: %w", err)
	}
	a.cfg.BridgeRoutes = routes
	return nil
}

// ----- Settings -----

// UpdateSettings applies new timeout and limit settings to the running
// system and persists the updated configuration.
func (a *CooperApp) UpdateSettings(timeoutSecs, blockedLimit, allowedLimit, bridgeLogLimit int) error {
	a.cfg.MonitorTimeoutSecs = timeoutSecs
	a.cfg.BlockedHistoryLimit = blockedLimit
	a.cfg.AllowedHistoryLimit = allowedLimit
	a.cfg.BridgeLogLimit = bridgeLogLimit

	// Update live ACL listener timeout.
	if a.aclListener != nil {
		newTimeout := time.Duration(timeoutSecs) * time.Second
		a.aclListener.SetTimeout(newTimeout)
	}

	// Persist config.json.
	cfgPath := filepath.Join(a.cooperDir, "config.json")
	if err := config.SaveConfig(cfgPath, a.cfg); err != nil {
		return fmt.Errorf("config save failed: %w", err)
	}

	return nil
}

// ----- State accessors -----

// Config returns a pointer to the current configuration. Callers should
// treat this as read-only; mutations should go through the App methods.
func (a *CooperApp) Config() *config.Config {
	return a.cfg
}

// CooperDir returns the path to the cooper configuration directory.
func (a *CooperApp) CooperDir() string {
	return a.cooperDir
}

// StartupWarnings returns version mismatch warnings collected during startup.
func (a *CooperApp) StartupWarnings() []string {
	return a.startupWarnings
}

// Adopt injects pre-started infrastructure into the CooperApp and wires
// the forwarding channels. This is used when main.go runs its own startup
// sequence (e.g. with a loading screen) and then needs to hand off the
// already-running services to the App.
func (a *CooperApp) Adopt(aclListener *proxy.ACLListener, bridgeServer *bridge.BridgeServer, warnings []string) {
	a.aclListener = aclListener
	a.bridgeServer = bridgeServer
	a.startupWarnings = warnings

	// Re-install clipboard handler on the adopted bridge server so it uses
	// this app's clipboard manager (which may have been replaced by
	// AdoptClipboard before this call).
	if a.clipboardManager != nil && a.bridgeServer != nil {
		clipHandler := clipboard.NewHandler(a.clipboardManager)
		a.bridgeServer.SetClipboardHandler(clipHandler)
	}

	a.wireChannels()
	a.loadPersistedBridgeRoutes()
}

// AdoptClipboard replaces the internal clipboard manager and reader with
// pre-created instances. This is used by main.go's startup path which
// creates these early so the bridge can be wired before the app exists.
func (a *CooperApp) AdoptClipboard(mgr *clipboard.Manager, reader *clipboard.LinuxReader) {
	a.clipboardManager = mgr
	a.clipboardReader = reader
}

// ACLListener returns the underlying ACL listener for direct access by
// components that need the aclTimeoutUpdater interface (e.g., proxymon).
// This is a transitional accessor; eventually the TUI should go through
// App methods exclusively.
func (a *CooperApp) ACLListener() *proxy.ACLListener {
	return a.aclListener
}

// BridgeServer returns the underlying bridge server for direct access by
// components that need the bridgeServerUpdater interface. This is a
// transitional accessor.
func (a *CooperApp) BridgeServer() *bridge.BridgeServer {
	return a.bridgeServer
}

// ----- Clipboard bridge -----

// CaptureClipboard reads the host clipboard, normalizes the image to PNG,
// and stages it for authenticated barrel access.
func (a *CooperApp) CaptureClipboard() (*clipboard.ClipboardEvent, error) {
	if a.clipboardReader == nil || a.clipboardManager == nil {
		return &clipboard.ClipboardEvent{
			State: clipboard.ClipboardFailed,
			Error: "clipboard bridge not initialized",
		}, fmt.Errorf("clipboard bridge not initialized")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := a.clipboardReader.Read(ctx)
	if err != nil {
		return &clipboard.ClipboardEvent{
			State: clipboard.ClipboardFailed,
			Error: err.Error(),
		}, err
	}

	obj, err := clipboard.Normalize(result, a.cfg.ClipboardMaxBytes)
	if err != nil {
		return &clipboard.ClipboardEvent{
			State: clipboard.ClipboardFailed,
			Error: err.Error(),
		}, err
	}

	ttl := time.Duration(a.cfg.ClipboardTTLSecs) * time.Second
	snap, err := a.clipboardManager.Stage(*obj, ttl)
	if err != nil {
		return &clipboard.ClipboardEvent{
			State: clipboard.ClipboardFailed,
			Error: err.Error(),
		}, err
	}

	return &clipboard.ClipboardEvent{
		State:    clipboard.ClipboardStaged,
		Snapshot: snap,
	}, nil
}

// StageFile reads an image file from disk and stages it on the clipboard
// bridge. It reuses the same normalization pipeline as CaptureClipboard
// (format detection via magic bytes, PNG conversion, size enforcement).
// Non-image files are rejected with a clear error.
func (a *CooperApp) StageFile(path string) (*clipboard.ClipboardEvent, error) {
	if a.clipboardManager == nil {
		return &clipboard.ClipboardEvent{
			State: clipboard.ClipboardFailed,
			Error: "clipboard bridge not initialized",
		}, fmt.Errorf("clipboard bridge not initialized")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		msg := fmt.Sprintf("read file: %v", err)
		return &clipboard.ClipboardEvent{
			State: clipboard.ClipboardFailed,
			Error: msg,
		}, fmt.Errorf("%s", msg)
	}

	if !clipboard.IsImageData(data) {
		msg := "only image files can be staged (png, jpeg, gif, bmp, tiff, webp)"
		return &clipboard.ClipboardEvent{
			State: clipboard.ClipboardFailed,
			Error: msg,
		}, fmt.Errorf("%s", msg)
	}

	ext := filepath.Ext(path)
	result := &clipboard.CaptureResult{
		MIME:      clipboard.FormatToMIME(clipboard.DetectImageFormat(data)),
		Filename:  filepath.Base(path),
		Extension: ext,
		Bytes:     data,
	}

	obj, err := clipboard.Normalize(result, a.cfg.ClipboardMaxBytes)
	if err != nil {
		return &clipboard.ClipboardEvent{
			State: clipboard.ClipboardFailed,
			Error: err.Error(),
		}, err
	}

	ttl := time.Duration(a.cfg.ClipboardTTLSecs) * time.Second
	snap, err := a.clipboardManager.Stage(*obj, ttl)
	if err != nil {
		return &clipboard.ClipboardEvent{
			State: clipboard.ClipboardFailed,
			Error: err.Error(),
		}, err
	}

	return &clipboard.ClipboardEvent{
		State:    clipboard.ClipboardStaged,
		Snapshot: snap,
	}, nil
}

// ClearClipboard removes the currently staged clipboard image.
func (a *CooperApp) ClearClipboard() {
	if a.clipboardManager != nil {
		a.clipboardManager.Clear()
	}
}

// ClipboardSnapshot returns the current staged clipboard snapshot, or nil.
func (a *CooperApp) ClipboardSnapshot() *clipboard.StagedSnapshot {
	if a.clipboardManager == nil {
		return nil
	}
	return a.clipboardManager.Current()
}

// ClipboardManager returns the underlying clipboard manager for direct
// access by components that need token registration (barrel startup).
func (a *CooperApp) ClipboardManager() *clipboard.Manager {
	return a.clipboardManager
}

// ----- Internal helpers -----

// ensurePlaywrightSupportDirs creates the host directories for Playwright
// support before any barrel start, so Docker does not create them as root-owned.
func ensurePlaywrightSupportDirs(cooperDir string) error {
	dirs := []string{
		filepath.Join(cooperDir, "fonts"),
		filepath.Join(cooperDir, "cache", "ms-playwright"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create Playwright support dir %s: %w", dir, err)
		}
	}
	return nil
}

// checkToolVersions compares container tool versions against expected
// versions and returns warnings for any mismatches.
func checkToolVersions(cfg *config.Config) []string {
	var warnings []string
	allTools := append(cfg.ProgrammingTools, cfg.AITools...)
	for _, tool := range allTools {
		if !tool.Enabled || tool.Mode == config.ModeOff {
			continue
		}

		var expected string
		switch tool.Mode {
		case config.ModeMirror:
			hostVer, err := config.DetectHostVersion(tool.Name)
			if err != nil {
				continue
			}
			expected = hostVer
		case config.ModeLatest:
			type result struct {
				ver string
				err error
			}
			ch := make(chan result, 1)
			go func() {
				v, e := config.ResolveLatestVersion(tool.Name)
				ch <- result{v, e}
			}()
			select {
			case r := <-ch:
				if r.err != nil {
					continue
				}
				expected = r.ver
			case <-time.After(5 * time.Second):
				continue
			}
		case config.ModePin:
			expected = tool.PinnedVersion
		default:
			continue
		}

		status := config.CompareVersions(tool.ContainerVersion, expected, tool.Mode)
		if status == config.VersionMismatch {
			warnings = append(warnings, fmt.Sprintf(
				"%s: container=%s, expected=%s (%s mode)",
				tool.Name, tool.ContainerVersion, expected, tool.Mode,
			))
		}
	}
	return warnings
}
