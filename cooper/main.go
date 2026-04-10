package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/auth"
	"github.com/rickchristie/govner/cooper/internal/bridge"
	"github.com/rickchristie/govner/cooper/internal/clipboard"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/configure"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/fontsync"
	"github.com/rickchristie/govner/cooper/internal/logging"
	"github.com/rickchristie/govner/cooper/internal/names"
	"github.com/rickchristie/govner/cooper/internal/proof"
	"github.com/rickchristie/govner/cooper/internal/proxy"
	"github.com/rickchristie/govner/cooper/internal/templates"
	"github.com/rickchristie/govner/cooper/internal/tui"
	"github.com/rickchristie/govner/cooper/internal/tui/about"
	"github.com/rickchristie/govner/cooper/internal/tui/bridgeui"
	"github.com/rickchristie/govner/cooper/internal/tui/containers"
	"github.com/rickchristie/govner/cooper/internal/tui/events"
	"github.com/rickchristie/govner/cooper/internal/tui/history"
	"github.com/rickchristie/govner/cooper/internal/tui/loading"
	"github.com/rickchristie/govner/cooper/internal/tui/portfwd"
	"github.com/rickchristie/govner/cooper/internal/tui/proxymon"
	"github.com/rickchristie/govner/cooper/internal/tui/settings"
	squidlogui "github.com/rickchristie/govner/cooper/internal/tui/squidlog"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
	"github.com/rickchristie/govner/cooper/meta"
)

var configDir string
var imagePrefix string

var rootCmd = &cobra.Command{
	Use:   "cooper",
	Short: "Barrel-proof containers for undiluted AI",
	Long: `cooper - Barrel-proof containers for undiluted AI

Network-isolated Docker containers for AI coding assistants, with a Squid
SSL-bump proxy for network control and a real-time TUI for request approval.

Quick Start:
  cooper configure        Run interactive configuration wizard
  cooper build            Build proxy and CLI container images
  cooper up               Start proxy and open control panel TUI
  cooper cli              Open a CLI container for the current workspace

Management:
  cooper update           Regenerate Dockerfile and rebuild CLI container
  cooper proof            Run diagnostics inside a CLI container
  cooper cleanup          Remove all cooper containers and images`,
	Version: meta.Version,
}

var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Run interactive configuration wizard",
	Long:  `Runs an interactive wizard to configure cooper and generate necessary files in ~/.cooper.`,
	RunE:  runConfigure,
}

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build proxy and CLI container images",
	Long:  `Builds the proxy and CLI container Docker images from generated Dockerfiles.`,
	RunE:  runBuild,
}

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Start proxy and open control panel TUI",
	Long:  `Starts the proxy container, execution bridge, and opens the control panel TUI.`,
	RunE:  runUp,
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Regenerate Dockerfile and rebuild CLI container",
	Long:  `Regenerates the CLI Dockerfile and rebuilds only the layers that need updates.`,
	RunE:  runUpdate,
}

var cliOneShot string
var tuiTestScreen string
var regenerateCA bool

var cliCmd = &cobra.Command{
	Use:   "cli [tool-name]",
	Short: "Launch an AI tool in a network-isolated barrel",
	Long: `Launches an AI CLI tool inside a network-isolated barrel container.
The current directory is mounted as the workspace. The tool starts
automatically with auto-approve enabled.

  cooper cli claude       Launch Claude Code
  cooper cli codex        Launch Codex CLI
  cooper cli copilot      Launch Copilot CLI
  cooper cli opencode     Launch OpenCode
  cooper cli list         List available tool images

Use -c to run a one-shot command instead:
  cooper cli claude -c "go test ./..."`,
	Args: cobra.MaximumNArgs(1),
	RunE: runCLI,
}

var proofCmd = &cobra.Command{
	Use:   "proof",
	Short: "Full lifecycle integration test",
	Long: `Stands up the entire Cooper stack (networks, proxy, bridge, barrel),
runs comprehensive diagnostics (SSL, proxy, tools, AI CLIs), and tears
everything down. Output is designed to be copy-pasted into a GitHub issue.

Requires: cooper configure + cooper build completed first.
Refuses to run if cooper up is already running.`,
	RunE: runProof,
}

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove all cooper containers and images",
	Long:  `Stops and removes all cooper containers, removes Docker images, and optionally removes ~/.cooper.`,
	RunE:  runCleanup,
}

var tuiTestCmd = &cobra.Command{
	Use:   "tui-test",
	Short: "Launch TUI with mock data for visual QA",
	Long: `Launches the Cooper TUI control panel populated with mock data so you can
visually inspect every screen without needing Docker or a running proxy.

Use --screen to jump directly to a specific tab:
  cooper tui-test --screen monitor
  cooper tui-test --screen containers
  cooper tui-test --screen configure
  cooper tui-test --screen ports`,
	RunE: runTUITest,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configDir, "config", "~/.cooper",
		"Path to cooper configuration directory")
	rootCmd.PersistentFlags().StringVar(&imagePrefix, "prefix", "",
		"Prefix for Docker image/container names (for testing)")

	// Apply prefix early via PersistentPreRun so all commands see it.
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if imagePrefix != "" {
			docker.SetImagePrefix(imagePrefix)
		}
	}

	cliCmd.Flags().StringVarP(&cliOneShot, "command", "c", "",
		"Run a one-shot command in the CLI container")

	configureCmd.Flags().BoolVar(&regenerateCA, "regenerate-ca", false,
		"Regenerate the CA certificate and key (requires cooper build afterward)")

	buildCmd.Flags().BoolVar(&buildClean, "clean", false,
		"Force clean rebuild (ignore Docker cache)")

	rootCmd.AddCommand(configureCmd)
	rootCmd.AddCommand(buildCmd)
	rootCmd.AddCommand(upCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(cliCmd)
	rootCmd.AddCommand(proofCmd)
	rootCmd.AddCommand(cleanupCmd)
	rootCmd.AddCommand(tuiTestCmd)

	tuiTestCmd.Flags().StringVar(&tuiTestScreen, "screen", "",
		"Jump to a specific screen: containers, monitor, blocked, allowed, bridge-logs, bridge-routes, settings, ports, about, loading, configure")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// resolveCooperDir expands the ~ in configDir to the user's home directory.
func resolveCooperDir() (string, error) {
	dir := configDir
	if strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		dir = filepath.Join(home, dir[2:])
	}
	return dir, nil
}

// loadConfig loads the cooper configuration from the config directory.
func loadConfig() (*config.Config, string, error) {
	cooperDir, err := resolveCooperDir()
	if err != nil {
		return nil, "", err
	}
	configPath := filepath.Join(cooperDir, "config.json")
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, "", fmt.Errorf("load config from %s: %w", configPath, err)
	}
	return cfg, cooperDir, nil
}

// ---------- cooper configure ----------

func runConfigure(cmd *cobra.Command, args []string) error {
	cooperDir, err := resolveCooperDir()
	if err != nil {
		return err
	}

	logDir := filepath.Join(cooperDir, "logs")
	cl := logging.NewCmdLogger(logDir, "configure")
	defer cl.Close()
	cl.LogStart()

	if regenerateCA {
		fmt.Fprintln(os.Stderr, "Regenerating CA certificate...")
		if _, _, err := config.RegenerateCA(cooperDir); err != nil {
			err = fmt.Errorf("regenerate CA: %w", err)
			cl.LogStep(0, "Regenerate CA", err)
			cl.LogDone(err)
			return err
		}
		cl.LogStep(0, "Regenerate CA", nil)
		fmt.Fprintln(os.Stderr, "CA certificate regenerated. Run 'cooper build' to rebuild images with the new CA.")
	}

	ca, err := app.NewConfigureApp(cooperDir)
	if err != nil {
		err = fmt.Errorf("initialize configure: %w", err)
		cl.LogStep(1, "Initialize configure app", err)
		cl.LogDone(err)
		return err
	}
	cl.LogStep(1, "Initialize configure app", nil)

	result, err := configure.Run(ca)
	if err != nil {
		cl.LogStep(2, "Run configure wizard", err)
		cl.LogDone(err)
		return err
	}
	cl.LogStep(2, "Run configure wizard", nil)

	if result.BuildRequested {
		if result.CleanBuild {
			buildClean = true
		}
		cl.LogDone(nil)
		return runBuild(cmd, args)
	}
	cl.LogDone(nil)
	return nil
}

// ---------- cooper build ----------

var buildClean bool

func runBuild(cmd *cobra.Command, args []string) error {
	cfg, cooperDir, err := loadConfig()
	if err != nil {
		return err
	}

	// 1. Generate templates.
	baseDir := filepath.Join(cooperDir, "base")
	cliDir := filepath.Join(cooperDir, "cli")
	proxyDir := filepath.Join(cooperDir, "proxy")
	for _, d := range []string{baseDir, cliDir, proxyDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("create dir %s: %w", d, err)
		}
	}

	// Resolve latest versions for tools in ModeLatest so PinnedVersion is concrete.
	fmt.Fprintln(os.Stderr, "Resolving tool versions...")
	resolveLatestVersions(cfg)

	fmt.Fprintln(os.Stderr, "Generating templates...")
	if err := templates.WriteAllTemplates(baseDir, cliDir, cfg); err != nil {
		return fmt.Errorf("write templates: %w", err)
	}
	if err := templates.WriteProxyTemplates(proxyDir, cfg); err != nil {
		return fmt.Errorf("write proxy templates: %w", err)
	}

	// 2. Ensure CA certificate exists.
	fmt.Fprintln(os.Stderr, "Ensuring CA certificate...")
	caCertPath, caKeyPath, err := config.EnsureCA(cooperDir)
	if err != nil {
		return fmt.Errorf("ensure CA: %w", err)
	}

	// 3. Write ACL helper source into the proxy build context.
	fmt.Fprintln(os.Stderr, "Writing ACL helper source...")
	if err := templates.WriteACLHelperSource(proxyDir); err != nil {
		return fmt.Errorf("write acl helper source: %w", err)
	}

	// 4. Stage CA files into build contexts.
	fmt.Fprintln(os.Stderr, "Staging CA files into build contexts...")
	if err := copyFile(caCertPath, filepath.Join(baseDir, "cooper-ca.pem")); err != nil {
		return fmt.Errorf("stage CA cert into base dir: %w", err)
	}
	if err := copyFile(caCertPath, filepath.Join(proxyDir, "cooper-ca.pem")); err != nil {
		return fmt.Errorf("stage CA cert into proxy dir: %w", err)
	}
	if err := copyFile(caKeyPath, filepath.Join(proxyDir, "cooper-ca-key.pem")); err != nil {
		return fmt.Errorf("stage CA key into proxy dir: %w", err)
	}

	noCache := buildClean

	// 5. Build proxy image.
	fmt.Fprintln(os.Stderr, "Building proxy image...")
	proxyDockerfile := filepath.Join(proxyDir, "proxy.Dockerfile")
	uidGidArgs := map[string]string{
		"USER_UID": fmt.Sprintf("%d", os.Getuid()),
		"USER_GID": fmt.Sprintf("%d", os.Getgid()),
	}
	if err := docker.BuildImage(docker.GetImageProxy(), proxyDockerfile, proxyDir, uidGidArgs, noCache); err != nil {
		return fmt.Errorf("build proxy image: %w", err)
	}

	// 6. Build base image (no AI tools).
	fmt.Fprintln(os.Stderr, "Building base image...")
	baseDockerfile := filepath.Join(baseDir, "Dockerfile")
	if err := docker.BuildImage(docker.GetImageBase(), baseDockerfile, baseDir, uidGidArgs, noCache); err != nil {
		return fmt.Errorf("build base image: %w", err)
	}

	// 7. Build each enabled AI tool image.
	for _, tool := range cfg.AITools {
		if !tool.Enabled {
			continue
		}
		toolDir := filepath.Join(cliDir, tool.Name)
		imageName := docker.GetImageCLI(tool.Name)
		dockerfile := filepath.Join(toolDir, "Dockerfile")
		fmt.Fprintf(os.Stderr, "Building %s image...\n", tool.Name)
		if err := docker.BuildImage(imageName, dockerfile, toolDir, nil, noCache); err != nil {
			return fmt.Errorf("build %s image: %w", tool.Name, err)
		}
	}

	// 8. Build user-custom images (directories in cli/ not matching built-in tool names).
	builtinNames := map[string]bool{"claude": true, "copilot": true, "codex": true, "opencode": true}
	entries, _ := os.ReadDir(cliDir)
	for _, e := range entries {
		if !e.IsDir() || builtinNames[e.Name()] {
			continue
		}
		customDir := filepath.Join(cliDir, e.Name())
		customDockerfile := filepath.Join(customDir, "Dockerfile")
		if fileExists(customDockerfile) {
			imageName := docker.GetImageCLI(e.Name())
			fmt.Fprintf(os.Stderr, "Building custom image %s...\n", e.Name())
			if err := docker.BuildImage(imageName, customDockerfile, customDir, nil, noCache); err != nil {
				return fmt.Errorf("build custom image %s: %w", e.Name(), err)
			}
		}
	}

	// 9. Update ContainerVersion in config to reflect what was just built.
	updateContainerVersions(cfg)
	configPath := filepath.Join(cooperDir, "config.json")
	if err := config.SaveConfig(configPath, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not save updated config: %v\n", err)
	}

	fmt.Fprintln(os.Stderr, "Build complete.")
	return nil
}

// updateContainerVersions sets ContainerVersion for all enabled tools
// to reflect what was actually built into the image.
func updateContainerVersions(cfg *config.Config) {
	for i := range cfg.ProgrammingTools {
		cfg.ProgrammingTools[i].RefreshContainerVersion()
	}
	for i := range cfg.AITools {
		cfg.AITools[i].RefreshContainerVersion()
	}
}

// resolveLatestVersions resolves the latest upstream version for all enabled
// tools in ModeLatest and stores it in PinnedVersion. This ensures the
// Dockerfile uses a concrete version and ContainerVersion is set correctly.
func resolveLatestVersions(cfg *config.Config) {
	for i := range cfg.ProgrammingTools {
		t := &cfg.ProgrammingTools[i]
		if t.Enabled && t.Mode == config.ModeLatest {
			v, err := config.ResolveLatestVersion(t.Name)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: could not resolve latest %s: %v\n", t.Name, err)
			} else {
				t.PinnedVersion = v
				fmt.Fprintf(os.Stderr, "  %s: latest = %s\n", t.Name, v)
			}
		}
	}
	for i := range cfg.AITools {
		t := &cfg.AITools[i]
		if t.Enabled && t.Mode == config.ModeLatest {
			v, err := config.ResolveLatestVersion(t.Name)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: could not resolve latest %s: %v\n", t.Name, err)
			} else {
				t.PinnedVersion = v
				fmt.Fprintf(os.Stderr, "  %s: latest = %s\n", t.Name, v)
			}
		}
	}
}

// ---------- cooper up ----------

func runUp(cmd *cobra.Command, args []string) error {
	cfg, cooperDir, err := loadConfig()
	if err != nil {
		return err
	}

	logDir := filepath.Join(cooperDir, "logs")
	ul := logging.NewCmdLogger(logDir, "up")
	defer ul.Close()
	ul.LogStart()

	// Create the loading screen model.
	loadModel := loading.New(false)

	// Create a BubbleTea program for the loading screen.
	p := tea.NewProgram(&loadingAdapter{model: loadModel}, tea.WithAltScreen(), tea.WithMouseCellMotion())

	// Services that need cleanup on exit.
	var aclListener *proxy.ACLListener
	var bridgeServer *bridge.BridgeServer
	var hostRelay *docker.HostRelay

	// Startup warnings collected by the version check step.
	var startupWarnings []string

	// Create clipboard manager and reader early so the bridge can be wired
	// with the clipboard handler before it starts.
	ttl := time.Duration(cfg.ClipboardTTLSecs) * time.Second
	clipMgr := clipboard.NewManager(ttl, cfg.ClipboardMaxBytes)
	clipMgr.SetCooperDir(cooperDir)
	clipReader := clipboard.NewLinuxReader(os.Getenv)

	// Pre-check: verify clipboard prerequisites (xclip/wl-paste on host).
	// Refuse to start if missing — matches CooperApp.Start() contract.
	if err := clipReader.CheckPrerequisites(context.Background()); err != nil {
		err = fmt.Errorf("clipboard prerequisites not met: %w\nInstall xclip or wl-paste and try again", err)
		ul.LogStep(0, "Check clipboard prerequisites", err)
		ul.LogDone(err)
		return err
	}

	// Context for the startup goroutine so it can be cancelled if the user
	// quits during loading.
	startupCtx, startupCancel := context.WithCancel(context.Background())
	defer startupCancel()

	// Run startup steps in a background goroutine.
	go func() {
		// Step 0: Create networks.
		if err := docker.EnsureNetworks(); err != nil {
			ul.LogStep(0, "Create networks", err)
			p.Send(loading.StepErrorMsg{Index: 0, Err: err})
			return
		}
		if startupCtx.Err() != nil {
			return
		}
		ul.LogStep(0, "Create networks", nil)
		p.Send(loading.StepCompleteMsg{Index: 0})

		// Step 1: Start proxy.
		if err := docker.StartProxy(cfg, cooperDir); err != nil {
			ul.LogStep(1, "Start proxy", err)
			p.Send(loading.StepErrorMsg{Index: 1, Err: err})
			return
		}
		if startupCtx.Err() != nil {
			return
		}
		ul.LogStep(1, "Start proxy", nil)
		p.Send(loading.StepCompleteMsg{Index: 1})

		// Step 2: SSL certificates (already ensured during configure/build,
		// but verify they exist).
		if !config.CAExists(cooperDir) {
			err := fmt.Errorf("CA certificate not found, run 'cooper build' first")
			ul.LogStep(2, "Verify CA certificate", err)
			p.Send(loading.StepErrorMsg{Index: 2, Err: err})
			return
		}
		if startupCtx.Err() != nil {
			return
		}
		ul.LogStep(2, "Verify CA certificate", nil)
		p.Send(loading.StepCompleteMsg{Index: 2})

		// Step 3: Start execution bridge.
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
			ul.LogStep(3, "Start bridge", err)
			p.Send(loading.StepErrorMsg{Index: 3, Err: err})
			return
		}
		bridgeServer = bridge.NewBridgeServer(cfg.BridgeRoutes, cfg.BridgePort, gatewayIPs)
		// Install clipboard handler so /clipboard/* endpoints work.
		clipHandler := clipboard.NewHandler(clipMgr)
		bridgeServer.SetClipboardHandler(clipHandler)
		if err := bridgeServer.Start(); err != nil {
			ul.LogStep(3, "Start bridge", err)
			p.Send(loading.StepErrorMsg{Index: 3, Err: err})
			return
		}
		if startupCtx.Err() != nil {
			return
		}

		// Start host-side lazy TCP relays so services bound to 127.0.0.1 are
		// reachable from containers via host.docker.internal (gateway IP).
		// The relay periodically scans ports and only activates when a
		// loopback-only service is detected. Relays are torn down when
		// the service stops (via scan or failed connection).
		hostRelay = docker.NewHostRelay(gatewayIPs, nil)
		hostRelay.Start(cfg.PortForwardRules)

		ul.LogStep(3, "Start bridge", nil)
		p.Send(loading.StepCompleteMsg{Index: 3})

		// Step 4: Ensure Playwright support dirs and sync fonts (best-effort).
		playwrightDirs := []string{
			filepath.Join(cooperDir, "fonts"),
			filepath.Join(cooperDir, "cache", "ms-playwright"),
		}
		for _, d := range playwrightDirs {
			if err := os.MkdirAll(d, 0o755); err != nil {
				err = fmt.Errorf("create Playwright support dir %s: %w", d, err)
				ul.LogStep(4, "Playwright support ready", err)
				p.Send(loading.StepErrorMsg{Index: 4, Err: err})
				return
			}
		}
		homeDir, _ := os.UserHomeDir()
		fontResult, fontErr := fontsync.SyncLinuxFonts(homeDir, cooperDir)
		if fontErr != nil {
			startupWarnings = append(startupWarnings, fmt.Sprintf("Font sync failed: %v", fontErr))
		} else {
			for _, w := range fontResult.Warnings {
				if !strings.Contains(w, "not accessible") {
					startupWarnings = append(startupWarnings, fmt.Sprintf("Font sync: %s", w))
				}
			}
		}
		if startupCtx.Err() != nil {
			return
		}
		ul.LogStep(4, "Playwright support ready", nil)
		p.Send(loading.StepCompleteMsg{Index: 4})

		// Step 5: CLI image version check (informational, non-blocking).
		startupWarnings = append(startupWarnings, checkToolVersions(cfg)...)
		if startupCtx.Err() != nil {
			return
		}
		ul.LogStep(5, "Check tool versions", nil)
		p.Send(loading.StepCompleteMsg{Index: 5})

		// Step 6: Start ACL listener.
		socketPath := filepath.Join(cooperDir, "run", "acl.sock")
		if err := os.MkdirAll(filepath.Dir(socketPath), 0755); err != nil {
			err = fmt.Errorf("create run dir: %w", err)
			ul.LogStep(6, "Start ACL listener", err)
			p.Send(loading.StepErrorMsg{Index: 6, Err: err})
			return
		}
		timeout := time.Duration(cfg.MonitorTimeoutSecs) * time.Second
		aclListener = proxy.NewACLListener(socketPath, timeout)
		if err := aclListener.Start(); err != nil {
			ul.LogStep(6, "Start ACL listener", err)
			p.Send(loading.StepErrorMsg{Index: 6, Err: err})
			return
		}
		if startupCtx.Err() != nil {
			return
		}
		ul.LogStep(6, "Start ACL listener", nil)
		p.Send(loading.StepCompleteMsg{Index: 6})

		// Step 7: Ready.
		ul.LogStep(7, "Ready", nil)
		ul.LogDone(nil)
		p.Send(loading.StepCompleteMsg{Index: 7})
	}()

	// Run the loading screen. This blocks until it finishes.
	loadingResult, err := p.Run()
	if err != nil {
		ul.LogDone(fmt.Errorf("loading screen: %w", err))
		return fmt.Errorf("loading screen: %w", err)
	}

	// Cancel startup context so the goroutine stops if it's still running.
	startupCancel()

	// Check if loading completed successfully or was cancelled/errored.
	adapter, ok := loadingResult.(*loadingAdapter)
	if !ok || adapter.model.HasError || !adapter.model.Done {
		// User cancelled or error occurred. Clean up what was started.
		cleanupServices(aclListener, bridgeServer, hostRelay)
		if adapter != nil && adapter.model.HasError {
			ul.LogDone(fmt.Errorf("startup failed"))
			return fmt.Errorf("startup failed")
		}
		ul.LogDone(nil)
		return nil
	}

	// Build the App, adopting the pre-started infrastructure.
	cooperApp := app.NewCooperApp(cfg, cooperDir)
	cooperApp.AdoptClipboard(clipMgr, clipReader)
	cooperApp.Adopt(aclListener, bridgeServer, hostRelay, startupWarnings)

	// Transition to the main TUI.
	mainModel := tui.NewModel(cooperApp)

	// Wire all tab sub-models.
	containersModel := containers.New(cooperApp)
	mainModel.SetContainersModel(containersModel)

	timeout := time.Duration(cfg.MonitorTimeoutSecs) * time.Second
	proxyMonModel := proxymon.New(cooperApp, timeout)
	mainModel.SetProxyMonModel(proxyMonModel)

	blockedModel := history.NewWithCapacity(history.ModeBlocked, cfg.BlockedHistoryLimit)
	mainModel.SetBlockedModel(blockedModel)

	allowedModel := history.NewWithCapacity(history.ModeAllowed, cfg.AllowedHistoryLimit)
	mainModel.SetAllowedModel(allowedModel)

	squidLogModel := squidlogui.New()
	mainModel.SetSquidLogModel(squidLogModel)

	bridgeLogsModel := bridgeui.NewLogsModel(cfg.BridgeLogLimit)
	mainModel.SetBridgeLogsModel(bridgeLogsModel)

	bridgeRoutesModel := bridgeui.NewRoutesModel()
	bridgeRoutesModel.SetRoutes(cfg.BridgeRoutes)
	mainModel.SetBridgeRoutesModel(bridgeRoutesModel)

	runtimeModel := settings.New(
		cfg.MonitorTimeoutSecs,
		cfg.BlockedHistoryLimit,
		cfg.AllowedHistoryLimit,
		cfg.BridgeLogLimit,
		cfg.ClipboardTTLSecs,
		cfg.ClipboardMaxBytes/(1024*1024), // Convert bytes to MB for display.
	)
	mainModel.SetRuntimeModel(runtimeModel)

	portForwardModel := portfwd.New()
	portForwardModel.SetPortForwardRules(cfg.PortForwardRules)
	mainModel.SetPortForwardModel(portForwardModel)

	aboutModel := about.New(cfg)
	// Send startup version warnings collected during loading.
	if warnings := cooperApp.StartupWarnings(); len(warnings) > 0 {
		aboutModel.Update(about.StartupWarningsMsg{Warnings: warnings})
	}
	mainModel.SetAboutModel(aboutModel)

	// Create the main TUI program so we can reference it in the shutdown
	// callback for sending ShutdownCompleteMsg.
	mainProgram := tui.NewProgram(mainModel)

	// Wire the shutdown callback: when the user confirms exit, run shutdown
	// steps in a goroutine, sending progress messages to drive the loading screen.
	mainModel.SetOnShutdown(func() {
		go func() {
			cooperApp.StopWithProgress(func(step int) {
				mainProgram.Send(events.ShutdownStepCompleteMsg{Index: step})
			})
		}()
	})

	mainModel.SetOnQuit(func() {
		cooperApp.Stop()
	})

	// Run the main TUI.
	if _, err := mainProgram.Run(); err != nil {
		return fmt.Errorf("TUI: %w", err)
	}
	if !mainModel.ExitExpected() {
		return fmt.Errorf("TUI exited unexpectedly without a user quit request; terminal input may have been closed or reset")
	}

	return nil
}

// cleanupServices stops the ACL listener and bridge server if they were started.
func cleanupServices(acl *proxy.ACLListener, br *bridge.BridgeServer, hr *docker.HostRelay) {
	if acl != nil {
		acl.Stop()
	}
	if br != nil {
		br.Stop()
	}
	if hr != nil {
		hr.Stop()
	}
	docker.StopProxy()
}

// loadingAdapter wraps a loading.Model to satisfy tea.Model.
type loadingAdapter struct {
	model loading.Model
}

func (a *loadingAdapter) Init() tea.Cmd {
	return a.model.Init()
}

func (a *loadingAdapter) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := a.model.Update(msg)
	a.model = m

	// If the loading screen is done (100% hold complete), quit to
	// transition to the main TUI.
	if a.model.Done && !a.model.HasError {
		return a, tea.Quit
	}

	return a, cmd
}

func (a *loadingAdapter) View() string {
	return a.model.View(a.model.Width, a.model.Height)
}

// ---------- cooper cli ----------

func listCLITools() {
	images, _ := docker.ListCLIImages()
	if len(images) == 0 {
		fmt.Fprintln(os.Stderr, "No tool images found. Run 'cooper build' first.")
		return
	}
	fmt.Fprintln(os.Stderr, "Available CLI tool images:")
	for _, img := range images {
		parts := strings.SplitN(img, "cooper-cli-", 2)
		if len(parts) == 2 {
			fmt.Fprintf(os.Stderr, "  %s\n", parts[1])
		}
	}
}

func runCLI(cmd *cobra.Command, args []string) error {
	// 1. Determine which tool.
	if len(args) == 0 {
		listCLITools()
		return fmt.Errorf("specify a tool: cooper cli <tool-name>")
	}

	// Handle "cooper cli list" subcommand.
	if args[0] == "list" {
		listCLITools()
		return nil
	}

	toolName := args[0]

	// 2. Validate tool image exists.
	imageName := docker.GetImageCLI(toolName)
	exists, _ := docker.ImageExists(imageName)
	if !exists {
		return fmt.Errorf("no image found for '%s'. Run 'cooper build' first", toolName)
	}

	// 3. Check proxy is running.
	running, err := docker.IsProxyRunning()
	if err != nil {
		return fmt.Errorf("check proxy: %w", err)
	}
	if !running {
		return fmt.Errorf("proxy is not running. Start it with 'cooper up' first")
	}

	// 4. Load config.
	cfg, cooperDir, err := loadConfig()
	if err != nil {
		return err
	}

	// 5. Resolve tokens (only for the specified tool).
	workspaceDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	tokens, err := auth.ResolveTokens(workspaceDir, cooperDir, []string{toolName})
	if err != nil {
		return fmt.Errorf("resolve tokens: %w", err)
	}

	// 6. Determine barrel container name (includes tool name).
	containerName := docker.BarrelContainerName(workspaceDir, toolName)

	// 7. If barrel not running, start it and wait for entrypoint readiness.
	barrelRunning, err := docker.IsBarrelRunning(containerName)
	if err != nil {
		return fmt.Errorf("check barrel: %w", err)
	}
	if !barrelRunning {
		// Generate and write clipboard token before starting the barrel.
		// The token file is mounted read-only into the container. The running
		// cooper up process validates tokens by scanning the tokens directory.
		clipToken, err := clipboard.GenerateToken()
		if err != nil {
			return fmt.Errorf("generate clipboard token: %w", err)
		}
		if _, err := clipboard.WriteTokenFile(cooperDir, containerName, clipToken); err != nil {
			return fmt.Errorf("write clipboard token: %w", err)
		}

		fmt.Fprintf(os.Stderr, "Starting barrel container %s...\n", containerName)
		if err := docker.StartBarrel(cfg, workspaceDir, cooperDir, toolName); err != nil {
			// Clean up token file on failed start.
			clipboard.RemoveTokenFile(cooperDir, containerName)
			return fmt.Errorf("start barrel: %w", err)
		}
		// Wait for entrypoint to finish writing .bashrc (welcome banner).
		// The entrypoint writes "Cooper: Welcome" to .bashrc as one of its last steps.
		for i := 0; i < 50; i++ {
			out, err := exec.Command("docker", "exec", containerName, "grep", "-q", "Cooper: Welcome", "/home/user/.bashrc").CombinedOutput()
			_ = out
			if err == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	// 8. Generate random name.
	sessionName := names.Generate(workspaceDir)
	defer names.Release(sessionName)

	// 9. Set terminal title.
	dirName := filepath.Base(workspaceDir)
	termTitle := fmt.Sprintf("%s-%s-%s", dirName, toolName, sessionName)
	fmt.Fprintf(os.Stdout, "\033]0;%s\007", termTitle)

	// 10. Build environment variables for the exec.
	var envArgs []string
	for _, t := range tokens {
		envArgs = append(envArgs, fmt.Sprintf("%s=%s", t.Name, t.Value))
	}

	// 11. Execute: one-shot command or interactive shell.
	var execCmd []string
	if cliOneShot != "" {
		execCmd = []string{"bash", "-c", cliOneShot}
	} else {
		execCmd = []string{"bash", "-l"}
	}

	interactive := cliOneShot == ""
	if err := docker.ExecBarrel(containerName, execCmd, envArgs, interactive); err != nil {
		return fmt.Errorf("exec barrel: %w", err)
	}

	if interactive {
		// Reset terminal title and print exit message.
		fmt.Fprint(os.Stdout, "\033]0;\007")
		fmt.Print("\n  \033[38;5;130m🥃 Barrel sealed. Back on host.\033[0m\n\n")
	}

	return nil
}

// ---------- cooper proof ----------

func runProof(cmd *cobra.Command, args []string) error {
	cfg, cooperDir, err := loadConfig()
	if err != nil {
		return err
	}
	return proof.Run(cfg, cooperDir)
}

// ---------- cooper cleanup ----------

func runCleanup(cmd *cobra.Command, args []string) error {
	// Load cooperDir for token cleanup. Non-fatal if it fails.
	_, cooperDir, _ := loadConfig()

	// 1. List and stop all barrels.
	fmt.Fprintln(os.Stderr, "Stopping barrel containers...")
	barrels, err := docker.ListBarrels()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not list barrels: %v\n", err)
	}
	for _, b := range barrels {
		fmt.Fprintf(os.Stderr, "  Stopping %s...\n", b.Name)
		if err := docker.StopBarrel(b.Name); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: %v\n", err)
		}
		// Clean up clipboard token file.
		if cooperDir != "" {
			clipboard.RemoveTokenFile(cooperDir, b.Name)
		}
	}

	// 2. Stop proxy.
	fmt.Fprintln(os.Stderr, "Stopping proxy...")
	if err := docker.StopProxy(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not stop proxy: %v\n", err)
	}

	// 3. Remove all CLI images.
	fmt.Fprintln(os.Stderr, "Removing Docker images...")
	cliImages, _ := docker.ListCLIImages()
	for _, img := range cliImages {
		fmt.Fprintf(os.Stderr, "  Removing %s...\n", img)
		if err := docker.RemoveImage(img); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: %v\n", err)
		}
	}
	// Remove base and proxy images.
	for _, img := range []string{docker.GetImageBase(), docker.GetImageProxy()} {
		exists, err := docker.ImageExists(img)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not check image %s: %v\n", img, err)
			continue
		}
		if exists {
			fmt.Fprintf(os.Stderr, "  Removing %s...\n", img)
			if err := docker.RemoveImage(img); err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: %v\n", err)
			}
		}
	}

	// 4. Remove networks.
	fmt.Fprintln(os.Stderr, "Removing networks...")
	if err := docker.RemoveNetworks(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not remove networks: %v\n", err)
	}

	// 5. Optionally remove ~/.cooper.
	cooperDir, err = resolveCooperDir()
	if err == nil {
		fmt.Fprintf(os.Stderr, "\nRemove configuration directory %s? [y/N] ", cooperDir)
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "y" || answer == "yes" {
			if err := os.RemoveAll(cooperDir); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not remove %s: %v\n", cooperDir, err)
			} else {
				fmt.Fprintf(os.Stderr, "Removed %s\n", cooperDir)
			}
		} else {
			fmt.Fprintln(os.Stderr, "Keeping configuration directory.")
		}
	}

	fmt.Fprintln(os.Stderr, "Cleanup complete.")
	return nil
}

// ---------- cooper update ----------

type updatePlan struct {
	baseChanged  bool
	toolsChanged map[string]bool
}

// collectUpdatePlan compares the persisted container versions against either
// the current host versions or the latest upstream versions, mutating cfg with
// the newly selected versions when a rebuild is needed.
func collectUpdatePlan(
	cfg *config.Config,
	latestResolver func(string) (string, error),
	hostDetector func(string) (string, error),
	out io.Writer,
) updatePlan {
	if out == nil {
		out = io.Discard
	}

	plan := updatePlan{toolsChanged: map[string]bool{}}

	// Check programming tools. Any mismatch here requires rebuilding the base
	// image, which in turn forces all enabled tool images to rebuild.
	for i, tool := range cfg.ProgrammingTools {
		if !tool.Enabled {
			continue
		}
		switch tool.Mode {
		case config.ModeLatest:
			fmt.Fprintf(out, "Checking latest version for %s...\n", tool.Name)
			latest, err := latestResolver(tool.Name)
			if err != nil {
				fmt.Fprintf(out, "  Warning: could not resolve latest %s: %v\n", tool.Name, err)
				continue
			}
			if latest != tool.ContainerVersion {
				fmt.Fprintf(out, "  %s: container=%s, latest=%s (mismatch)\n",
					tool.Name, tool.ContainerVersion, latest)
				plan.baseChanged = true
				cfg.ProgrammingTools[i].PinnedVersion = latest
			}
		case config.ModeMirror:
			fmt.Fprintf(out, "Checking host version for %s...\n", tool.Name)
			hostVer, err := hostDetector(tool.Name)
			if err != nil {
				fmt.Fprintf(out, "  Warning: could not detect host %s: %v\n", tool.Name, err)
				continue
			}
			if hostVer != tool.ContainerVersion {
				fmt.Fprintf(out, "  %s: container=%s, host=%s (mismatch)\n",
					tool.Name, tool.ContainerVersion, hostVer)
				plan.baseChanged = true
				cfg.ProgrammingTools[i].HostVersion = hostVer
			}
		}
	}

	// Check AI tool images. These can rebuild independently unless the base
	// image changed above.
	for i, tool := range cfg.AITools {
		if !tool.Enabled {
			continue
		}
		switch tool.Mode {
		case config.ModeLatest:
			fmt.Fprintf(out, "Checking latest version for %s...\n", tool.Name)
			latest, err := latestResolver(tool.Name)
			if err != nil {
				fmt.Fprintf(out, "  Warning: could not resolve latest %s: %v\n", tool.Name, err)
				continue
			}
			if latest != tool.ContainerVersion {
				fmt.Fprintf(out, "  %s: container=%s, latest=%s (mismatch)\n",
					tool.Name, tool.ContainerVersion, latest)
				plan.toolsChanged[tool.Name] = true
				cfg.AITools[i].PinnedVersion = latest
			}
		case config.ModeMirror:
			fmt.Fprintf(out, "Checking host version for %s...\n", tool.Name)
			hostVer, err := hostDetector(tool.Name)
			if err != nil {
				fmt.Fprintf(out, "  Warning: could not detect host %s: %v\n", tool.Name, err)
				continue
			}
			if hostVer != tool.ContainerVersion {
				fmt.Fprintf(out, "  %s: container=%s, host=%s (mismatch)\n",
					tool.Name, tool.ContainerVersion, hostVer)
				plan.toolsChanged[tool.Name] = true
				cfg.AITools[i].HostVersion = hostVer
			}
		}
	}

	return plan
}

func runUpdate(cmd *cobra.Command, args []string) error {
	cfg, cooperDir, err := loadConfig()
	if err != nil {
		return err
	}

	plan := collectUpdatePlan(cfg, config.ResolveLatestVersion, config.DetectHostVersion, os.Stderr)
	baseChanged := plan.baseChanged
	toolsChanged := plan.toolsChanged

	if !baseChanged && len(toolsChanged) == 0 {
		fmt.Fprintln(os.Stderr, "All tool versions match. No rebuild needed.")
		return nil
	}

	// Only reload Squid if an AI tool changed (AI tools affect the domain whitelist).
	needsSquidReload := len(toolsChanged) > 0

	// 3. Regenerate templates.
	fmt.Fprintln(os.Stderr, "Regenerating templates...")
	baseDir := filepath.Join(cooperDir, "base")
	cliDir := filepath.Join(cooperDir, "cli")
	if err := templates.WriteAllTemplates(baseDir, cliDir, cfg); err != nil {
		return fmt.Errorf("write templates: %w", err)
	}

	if needsSquidReload {
		proxyDir := filepath.Join(cooperDir, "proxy")
		if err := templates.WriteProxyTemplates(proxyDir, cfg); err != nil {
			return fmt.Errorf("write proxy templates: %w", err)
		}
	}

	// 4. Stage CA cert into base build context.
	caCert := filepath.Join(cooperDir, "ca", "cooper-ca.pem")
	if fileExists(caCert) {
		if err := copyFile(caCert, filepath.Join(baseDir, "cooper-ca.pem")); err != nil {
			return fmt.Errorf("stage CA cert: %w", err)
		}
	}

	// 5. Rebuild base if programming tools changed.
	if baseChanged {
		fmt.Fprintln(os.Stderr, "Rebuilding base image...")
		baseDockerfile := filepath.Join(baseDir, "Dockerfile")
		buildArgs := map[string]string{
			"USER_UID": fmt.Sprintf("%d", os.Getuid()),
			"USER_GID": fmt.Sprintf("%d", os.Getgid()),
		}
		if err := docker.BuildImage(docker.GetImageBase(), baseDockerfile, baseDir, buildArgs, false); err != nil {
			return fmt.Errorf("rebuild base image: %w", err)
		}
	}

	// 6. Rebuild tool images.
	// If base changed, ALL tool images need rebuilding (FROM changed).
	// If only specific tools changed, only those images rebuild.
	if baseChanged {
		for _, tool := range cfg.AITools {
			if !tool.Enabled {
				continue
			}
			toolDir := filepath.Join(cliDir, tool.Name)
			imageName := docker.GetImageCLI(tool.Name)
			dockerfile := filepath.Join(toolDir, "Dockerfile")
			fmt.Fprintf(os.Stderr, "Rebuilding %s image...\n", tool.Name)
			if err := docker.BuildImage(imageName, dockerfile, toolDir, nil, false); err != nil {
				return fmt.Errorf("rebuild %s: %w", tool.Name, err)
			}
		}
	} else {
		for name := range toolsChanged {
			toolDir := filepath.Join(cliDir, name)
			imageName := docker.GetImageCLI(name)
			dockerfile := filepath.Join(toolDir, "Dockerfile")
			fmt.Fprintf(os.Stderr, "Rebuilding %s image...\n", name)
			if err := docker.BuildImage(imageName, dockerfile, toolDir, nil, false); err != nil {
				return fmt.Errorf("rebuild %s: %w", name, err)
			}
		}
	}

	// 7. Hot-reload squid if needed.
	if needsSquidReload {
		proxyRunning, _ := docker.IsProxyRunning()
		if proxyRunning {
			fmt.Fprintln(os.Stderr, "Hot-reloading Squid configuration...")
			if err := docker.ReconfigureSquid(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: squid reconfigure failed: %v\n", err)
			}
		}
	}

	// 8. Update ContainerVersion and save config.
	updateContainerVersions(cfg)
	configPath := filepath.Join(cooperDir, "config.json")
	if err := config.SaveConfig(configPath, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Update complete.")
	return nil
}

// runTUITest launches the Cooper TUI with mock data for visual QA.
func runTUITest(cmd *cobra.Command, args []string) error {
	cfg := config.DefaultConfig()

	// Populate mock data for visual testing.
	cfg.ProgrammingTools = []config.ToolConfig{
		{Name: "go", Enabled: true, Mode: config.ModeMirror, PinnedVersion: "", HostVersion: "1.24.10", ContainerVersion: "1.24.10"},
		{Name: "node", Enabled: true, Mode: config.ModeLatest, PinnedVersion: "", HostVersion: "22.12.0", ContainerVersion: "22.11.0"},
		{Name: "python", Enabled: false, Mode: config.ModeOff},
		{Name: "rust", Enabled: false, Mode: config.ModeOff},
	}
	cfg.AITools = []config.ToolConfig{
		{Name: "claude", Enabled: true, Mode: config.ModeLatest, ContainerVersion: "1.0.18", HostVersion: "1.0.18"},
		{Name: "copilot", Enabled: true, Mode: config.ModeLatest, ContainerVersion: "0.7.2", HostVersion: "0.7.2"},
		{Name: "codex", Enabled: false, Mode: config.ModeOff},
		{Name: "opencode", Enabled: false, Mode: config.ModeOff},
	}
	cfg.PortForwardRules = []config.PortForwardRule{
		{ContainerPort: 5432, HostPort: 5432, Description: "PostgreSQL"},
		{ContainerPort: 6379, HostPort: 6379, Description: "Redis"},
	}
	cfg.BridgeRoutes = []config.BridgeRoute{
		{APIPath: "/deploy-staging", ScriptPath: "~/scripts/deploy-staging.sh"},
		{APIPath: "/go-mod-tidy", ScriptPath: "~/scripts/go-mod-tidy.sh"},
	}

	// Create mock ACL request channel with sample pending requests.
	aclCh := make(chan app.ACLRequest, 10)
	go func() {
		time.Sleep(500 * time.Millisecond)
		mockRequests := []app.ACLRequest{
			{ID: "req-1", Domain: "stackoverflow.com", Port: "443", SourceIP: "172.20.0.3", Timestamp: time.Now()},
			{ID: "req-2", Domain: "docs.python.org", Port: "443", SourceIP: "172.20.0.3", Timestamp: time.Now()},
			{ID: "req-3", Domain: "pkg.go.dev", Port: "443", SourceIP: "172.20.0.4", Timestamp: time.Now()},
		}
		for _, r := range mockRequests {
			aclCh <- r
		}
	}()

	// Create mock bridge log channel.
	bridgeLogCh := make(chan app.ExecutionLog, 10)
	go func() {
		time.Sleep(800 * time.Millisecond)
		bridgeLogCh <- app.ExecutionLog{
			Timestamp:  time.Now().Add(-2 * time.Minute),
			Route:      "/deploy-staging",
			ScriptPath: "~/scripts/deploy-staging.sh",
			ExitCode:   0,
			Stdout:     "Deploying to staging...\nDone.",
			Stderr:     "",
			Duration:   3200 * time.Millisecond,
		}
		bridgeLogCh <- app.ExecutionLog{
			Timestamp:  time.Now().Add(-30 * time.Second),
			Route:      "/go-mod-tidy",
			ScriptPath: "~/scripts/go-mod-tidy.sh",
			ExitCode:   1,
			Stdout:     "",
			Stderr:     "go: module not found",
			Duration:   450 * time.Millisecond,
			Error:      "exit status 1",
		}
	}()

	// Build the test app and TUI model.
	testApp := app.NewTestApp(cfg, aclCh, bridgeLogCh)
	mainModel := tui.NewModel(testApp)

	// Wire sub-models.
	mainModel.SetContainersModel(containers.New(testApp))
	mainModel.SetProxyMonModel(proxymon.New(testApp, time.Duration(cfg.MonitorTimeoutSecs)*time.Second))
	mainModel.SetBlockedModel(history.NewWithCapacity(history.ModeBlocked, cfg.BlockedHistoryLimit))
	mainModel.SetAllowedModel(history.NewWithCapacity(history.ModeAllowed, cfg.AllowedHistoryLimit))
	mainModel.SetSquidLogModel(squidlogui.New())

	logsModel := bridgeui.NewLogsModel(cfg.BridgeLogLimit)
	mainModel.SetBridgeLogsModel(logsModel)

	routesModel := bridgeui.NewRoutesModel()
	routesModel.SetRoutes(cfg.BridgeRoutes)
	mainModel.SetBridgeRoutesModel(routesModel)

	tuiRuntimeModel := settings.New(
		cfg.MonitorTimeoutSecs,
		cfg.BlockedHistoryLimit,
		cfg.AllowedHistoryLimit,
		cfg.BridgeLogLimit,
		cfg.ClipboardTTLSecs,
		cfg.ClipboardMaxBytes/(1024*1024),
	)
	mainModel.SetRuntimeModel(tuiRuntimeModel)

	tuiPortFwdModel := portfwd.New()
	tuiPortFwdModel.SetPortForwardRules(cfg.PortForwardRules)
	mainModel.SetPortForwardModel(tuiPortFwdModel)

	mainModel.SetAboutModel(about.New(cfg))

	// Jump to requested screen if --screen flag is set.
	if tuiTestScreen != "" {
		switch strings.ToLower(tuiTestScreen) {
		case "containers":
			mainModel.SetActiveTab(theme.TabContainers)
		case "monitor":
			mainModel.SetActiveTab(theme.TabMonitor)
		case "blocked":
			mainModel.SetActiveTab(theme.TabBlocked)
		case "allowed":
			mainModel.SetActiveTab(theme.TabAllowed)
		case "squid-logs", "squid":
			mainModel.SetActiveTab(theme.TabSquidLogs)
		case "bridge-logs":
			mainModel.SetActiveTab(theme.TabBridgeLogs)
		case "bridge-routes":
			mainModel.SetActiveTab(theme.TabBridgeRoutes)
		case "settings", "runtime":
			mainModel.SetActiveTab(theme.TabRuntime)
		case "ports", "portforward", "port-forward":
			mainModel.SetActiveTab(theme.TabPortForward)
		case "about":
			mainModel.SetActiveTab(theme.TabAbout)
		case "loading":
			// The loading screen uses a non-standard tea.Model (returns Model, not tea.Model).
			// It's tested via the real `cooper up` startup flow. For tui-test, print a note.
			fmt.Println("Loading screen is a transient state tested via `cooper up`.")
			fmt.Println("To see it, run: cooper up (with Docker running).")
			return nil
		case "configure":
			testCA, caErr := app.NewConfigureApp("/tmp/cooper-test")
			if caErr != nil {
				return caErr
			}
			_, err := configure.Run(testCA)
			return err
		default:
			return fmt.Errorf("unknown screen: %s\nAvailable: containers, monitor, blocked, allowed, squid-logs, bridge-logs, bridge-routes, settings, ports, about, loading, configure", tuiTestScreen)
		}
	}

	// Run the TUI.
	p := tea.NewProgram(mainModel, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

// checkToolVersions inspects each enabled tool for version mismatches.
// For Mirror mode it detects the live host version and updates cfg.HostVersion
// so the About screen displays correct values. For Latest mode it resolves
// the latest upstream version (with a short timeout so startup is not blocked
// forever). Returns a slice of human-readable warning strings.
func checkToolVersions(cfg *config.Config) []string {
	var warnings []string

	// Helper: check a single tool, update its HostVersion in the config.
	check := func(tool *config.ToolConfig) {
		if !tool.Enabled || tool.Mode == config.ModeOff {
			return
		}

		var expected string
		switch tool.Mode {
		case config.ModeMirror:
			hostVer, err := config.DetectHostVersion(tool.Name)
			if err != nil {
				return
			}
			// Update the config's HostVersion so the About screen shows the live value.
			tool.HostVersion = hostVer
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
					return
				}
				expected = r.ver
			case <-time.After(5 * time.Second):
				return
			}
		case config.ModePin:
			expected = tool.PinnedVersion
		default:
			return
		}

		status := config.CompareVersions(tool.ContainerVersion, expected, tool.Mode)
		if status == config.VersionMismatch {
			warnings = append(warnings, fmt.Sprintf(
				"%s: container=%s, expected=%s (%s mode)",
				tool.Name, tool.ContainerVersion, expected, tool.Mode,
			))
		}
	}

	for i := range cfg.ProgrammingTools {
		check(&cfg.ProgrammingTools[i])
	}
	for i := range cfg.AITools {
		check(&cfg.AITools[i])
	}
	return warnings
}

// fileExists returns true if the path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// copyFile copies src to dst, creating or truncating dst. It preserves the
// source file's permission bits.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
