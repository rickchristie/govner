package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/auth"
	"github.com/rickchristie/govner/cooper/internal/bridge"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/configure"
	"github.com/rickchristie/govner/cooper/internal/docker"
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
	"github.com/rickchristie/govner/cooper/internal/tui/proxymon"
	"github.com/rickchristie/govner/cooper/internal/tui/settings"
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
	Use:   "cli",
	Short: "Open a CLI container for the current workspace",
	Long: `Opens an interactive shell inside a network-isolated CLI container.
The current directory is mounted as the workspace.

Use -c to run a one-shot command:
  cooper cli -c "go test ./..."`,
	RunE: runCLI,
}

var proofCmd = &cobra.Command{
	Use:   "proof",
	Short: "Run diagnostics inside a CLI container",
	Long:  `Runs diagnostic checks to verify proxy connectivity, SSL bump, whitelists, and tool installations.`,
	RunE:  runProof,
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
  cooper tui-test --screen configure`,
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

	rootCmd.AddCommand(configureCmd)
	rootCmd.AddCommand(buildCmd)
	rootCmd.AddCommand(upCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(cliCmd)
	rootCmd.AddCommand(proofCmd)
	rootCmd.AddCommand(cleanupCmd)
	rootCmd.AddCommand(tuiTestCmd)

	tuiTestCmd.Flags().StringVar(&tuiTestScreen, "screen", "",
		"Jump to a specific screen: containers, monitor, blocked, allowed, bridge-logs, bridge-routes, settings, about, loading, configure")
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

	if regenerateCA {
		fmt.Fprintln(os.Stderr, "Regenerating CA certificate...")
		if _, _, err := config.RegenerateCA(cooperDir); err != nil {
			return fmt.Errorf("regenerate CA: %w", err)
		}
		fmt.Fprintln(os.Stderr, "CA certificate regenerated. Run 'cooper build' to rebuild images with the new CA.")
	}

	result, err := configure.Run(cooperDir)
	if err != nil {
		return err
	}

	if result.BuildRequested {
		return runBuild(cmd, args)
	}
	return nil
}

// ---------- cooper build ----------

func runBuild(cmd *cobra.Command, args []string) error {
	cfg, cooperDir, err := loadConfig()
	if err != nil {
		return err
	}

	// 1. Generate templates.
	cliDir := filepath.Join(cooperDir, "cli")
	proxyDir := filepath.Join(cooperDir, "proxy")
	if err := os.MkdirAll(cliDir, 0755); err != nil {
		return fmt.Errorf("create cli dir: %w", err)
	}
	if err := os.MkdirAll(proxyDir, 0755); err != nil {
		return fmt.Errorf("create proxy dir: %w", err)
	}

	// Resolve latest versions for tools in ModeLatest so PinnedVersion is concrete.
	fmt.Fprintln(os.Stderr, "Resolving tool versions...")
	resolveLatestVersions(cfg)

	fmt.Fprintln(os.Stderr, "Generating templates...")
	if err := templates.WriteAllTemplates(cliDir, cfg); err != nil {
		return fmt.Errorf("write templates: %w", err)
	}
	// Write proxy-specific templates into the proxy directory.
	if err := writeProxyTemplates(proxyDir, cfg); err != nil {
		return fmt.Errorf("write proxy templates: %w", err)
	}

	// 2. Ensure CA certificate exists.
	fmt.Fprintln(os.Stderr, "Ensuring CA certificate...")
	caCertPath, caKeyPath, err := config.EnsureCA(cooperDir)
	if err != nil {
		return fmt.Errorf("ensure CA: %w", err)
	}

	// 3. Remove existing images before building to ensure a clean slate.
	fmt.Fprintln(os.Stderr, "Removing existing images...")
	for _, img := range []string{docker.GetImageProxy(), docker.GetImageBarrelBase(), docker.GetImageBarrel()} {
		if err := removeImageIfExists(img); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not remove %s: %v\n", img, err)
		}
	}

	// 4. Write ACL helper source into the proxy build context.
	// The proxy Dockerfile compiles it inside Docker (multi-stage build),
	// so `cooper build` is self-contained — no host Go installation required.
	fmt.Fprintln(os.Stderr, "Writing ACL helper source...")
	if err := templates.WriteACLHelperSource(proxyDir); err != nil {
		return fmt.Errorf("write acl helper source: %w", err)
	}

	// 5. Stage CA files into each build context subdirectory.
	// The Dockerfiles use COPY with root-relative paths (e.g. COPY cooper-ca.pem),
	// so these files must be present in each image's build context directory.
	fmt.Fprintln(os.Stderr, "Staging CA files into build contexts...")
	// CLI image needs the CA cert.
	if err := copyFile(caCertPath, filepath.Join(cliDir, "cooper-ca.pem")); err != nil {
		return fmt.Errorf("stage CA cert into cli dir: %w", err)
	}
	// Proxy image needs both the CA cert and key.
	if err := copyFile(caCertPath, filepath.Join(proxyDir, "cooper-ca.pem")); err != nil {
		return fmt.Errorf("stage CA cert into proxy dir: %w", err)
	}
	if err := copyFile(caKeyPath, filepath.Join(proxyDir, "cooper-ca-key.pem")); err != nil {
		return fmt.Errorf("stage CA key into proxy dir: %w", err)
	}

	// 6. Build proxy image (no-cache). Context = proxyDir.
	// Pass host UID/GID so squid and all processes run as the host user.
	// This prevents mounted volumes from getting files owned by a different UID.
	fmt.Fprintln(os.Stderr, "Building proxy image...")
	proxyDockerfile := filepath.Join(proxyDir, "proxy.Dockerfile")
	proxyBuildArgs := map[string]string{
		"USER_UID": fmt.Sprintf("%d", os.Getuid()),
		"USER_GID": fmt.Sprintf("%d", os.Getgid()),
	}
	if err := docker.BuildImage(docker.GetImageProxy(), proxyDockerfile, proxyDir, proxyBuildArgs, true); err != nil {
		return fmt.Errorf("build proxy image: %w", err)
	}

	// 7. Build barrel-base image (no-cache). Context = cliDir.
	// Same UID/GID args so the user inside the container matches the host user.
	fmt.Fprintln(os.Stderr, "Building barrel-base image...")
	cliDockerfile := filepath.Join(cliDir, "Dockerfile")
	cliBuildArgs := map[string]string{
		"USER_UID": fmt.Sprintf("%d", os.Getuid()),
		"USER_GID": fmt.Sprintf("%d", os.Getgid()),
	}
	if err := docker.BuildImage(docker.GetImageBarrelBase(), cliDockerfile, cliDir, cliBuildArgs, true); err != nil {
		return fmt.Errorf("build barrel-base image: %w", err)
	}

	// 8. If Dockerfile.user exists, build barrel from it; else tag barrel-base.
	userDockerfile := filepath.Join(cliDir, "Dockerfile.user")
	if fileExists(userDockerfile) {
		fmt.Fprintln(os.Stderr, "Building barrel image from Dockerfile.user...")
		if err := docker.BuildImage(docker.GetImageBarrel(), userDockerfile, cliDir, nil, true); err != nil {
			return fmt.Errorf("build barrel image: %w", err)
		}
	} else {
		fmt.Fprintln(os.Stderr, "Tagging barrel-base as barrel...")
		if err := docker.TagImage(docker.GetImageBarrelBase(), docker.GetImageBarrel()); err != nil {
			return fmt.Errorf("tag barrel image: %w", err)
		}
	}

	// 9. Update ContainerVersion in config to reflect what was just built.
	// This ensures version comparisons in `cooper up` and `cooper update`
	// work correctly instead of comparing against empty/stale values.
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

// writeProxyTemplates writes the proxy-specific templates (proxy.Dockerfile,
// squid.conf, proxy-entrypoint.sh) into the given directory.
func writeProxyTemplates(dir string, cfg *config.Config) error {
	proxyDockerfile, err := templates.RenderProxyDockerfile(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "proxy.Dockerfile"), []byte(proxyDockerfile), 0644); err != nil {
		return fmt.Errorf("write proxy.Dockerfile: %w", err)
	}

	squidConf, err := templates.RenderSquidConf(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "squid.conf"), []byte(squidConf), 0644); err != nil {
		return fmt.Errorf("write squid.conf: %w", err)
	}

	proxyEntrypoint, err := templates.RenderProxyEntrypoint(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "proxy-entrypoint.sh"), []byte(proxyEntrypoint), 0755); err != nil {
		return fmt.Errorf("write proxy-entrypoint.sh: %w", err)
	}

	return nil
}

// ---------- cooper up ----------

// loadingProgram is the BubbleTea program used during startup and shutdown.
// It is stored at package level so the background goroutine can send messages.
type loadingProgram struct {
	program *tea.Program
}

func runUp(cmd *cobra.Command, args []string) error {
	cfg, cooperDir, err := loadConfig()
	if err != nil {
		return err
	}

	// Create the loading screen model.
	loadModel := loading.New(false)

	// Create a BubbleTea program for the loading screen.
	p := tea.NewProgram(&loadingAdapter{model: loadModel}, tea.WithAltScreen(), tea.WithMouseCellMotion())

	// Services that need cleanup on exit.
	var aclListener *proxy.ACLListener
	var bridgeServer *bridge.BridgeServer

	// Startup warnings collected by the version check step.
	var startupWarnings []string

	// Context for the startup goroutine so it can be cancelled if the user
	// quits during loading.
	startupCtx, startupCancel := context.WithCancel(context.Background())
	defer startupCancel()

	// Run startup steps in a background goroutine.
	go func() {
		// Step 0: Create networks.
		if err := docker.EnsureNetworks(); err != nil {
			p.Send(loading.StepErrorMsg{Index: 0, Err: err})
			return
		}
		if startupCtx.Err() != nil {
			return
		}
		p.Send(loading.StepCompleteMsg{Index: 0})

		// Step 1: Start proxy.
		if err := docker.StartProxy(cfg, cooperDir); err != nil {
			p.Send(loading.StepErrorMsg{Index: 1, Err: err})
			return
		}
		if startupCtx.Err() != nil {
			return
		}
		p.Send(loading.StepCompleteMsg{Index: 1})

		// Step 2: SSL certificates (already ensured during configure/build,
		// but verify they exist).
		if !config.CAExists(cooperDir) {
			p.Send(loading.StepErrorMsg{Index: 2, Err: fmt.Errorf("CA certificate not found, run 'cooper build' first")})
			return
		}
		if startupCtx.Err() != nil {
			return
		}
		p.Send(loading.StepCompleteMsg{Index: 2})

		// Step 3: Start execution bridge.
		gatewayIP, gwErr := docker.GetGatewayIP(docker.NetworkExternal)
		if gwErr != nil {
			p.Send(loading.StepErrorMsg{Index: 3,
				Err: fmt.Errorf("could not discover Docker gateway IP: %w\n"+
					"Bridge won't be reachable from containers. Check that cooper-external network exists", gwErr)})
			return
		}
		bridgeServer = bridge.NewBridgeServer(cfg.BridgeRoutes, cfg.BridgePort, gatewayIP, cfg.BridgePort)
		if err := bridgeServer.Start(); err != nil {
			p.Send(loading.StepErrorMsg{Index: 3, Err: err})
			return
		}
		if startupCtx.Err() != nil {
			return
		}
		p.Send(loading.StepCompleteMsg{Index: 3})

		// Step 4: CLI image version check (informational, non-blocking).
		startupWarnings = checkToolVersions(cfg)
		if startupCtx.Err() != nil {
			return
		}
		p.Send(loading.StepCompleteMsg{Index: 4})

		// Step 5: Start ACL listener.
		socketPath := filepath.Join(cooperDir, "run", "acl.sock")
		if err := os.MkdirAll(filepath.Dir(socketPath), 0755); err != nil {
			p.Send(loading.StepErrorMsg{Index: 5, Err: fmt.Errorf("create run dir: %w", err)})
			return
		}
		timeout := time.Duration(cfg.MonitorTimeoutSecs) * time.Second
		aclListener = proxy.NewACLListener(socketPath, timeout)
		if err := aclListener.Start(); err != nil {
			p.Send(loading.StepErrorMsg{Index: 5, Err: err})
			return
		}
		if startupCtx.Err() != nil {
			return
		}
		p.Send(loading.StepCompleteMsg{Index: 5})

		// Step 6: Ready.
		p.Send(loading.StepCompleteMsg{Index: 6})
	}()

	// Run the loading screen. This blocks until it finishes.
	loadingResult, err := p.Run()
	if err != nil {
		return fmt.Errorf("loading screen: %w", err)
	}

	// Cancel startup context so the goroutine stops if it's still running.
	startupCancel()

	// Check if loading completed successfully or was cancelled/errored.
	adapter, ok := loadingResult.(*loadingAdapter)
	if !ok || adapter.model.HasError || !adapter.model.Done {
		// User cancelled or error occurred. Clean up what was started.
		cleanupServices(aclListener, bridgeServer)
		if adapter != nil && adapter.model.HasError {
			return fmt.Errorf("startup failed")
		}
		return nil
	}

	// Build the App, adopting the pre-started infrastructure.
	cooperApp := app.NewCooperApp(cfg, cooperDir)
	cooperApp.Adopt(aclListener, bridgeServer, startupWarnings)

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

	bridgeLogsModel := bridgeui.NewLogsModel(cfg.BridgeLogLimit)
	mainModel.SetBridgeLogsModel(bridgeLogsModel)

	bridgeRoutesModel := bridgeui.NewRoutesModel()
	bridgeRoutesModel.SetRoutes(cfg.BridgeRoutes)
	mainModel.SetBridgeRoutesModel(bridgeRoutesModel)

	settingsModel := settings.New(
		cfg.MonitorTimeoutSecs,
		cfg.BlockedHistoryLimit,
		cfg.AllowedHistoryLimit,
		cfg.BridgeLogLimit,
	)
	settingsModel.SetPortForwardRules(cfg.PortForwardRules)
	mainModel.SetSettingsModel(settingsModel)

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
	// in a goroutine and send ShutdownCompleteMsg when done.
	mainModel.SetOnShutdown(func() {
		go func() {
			cooperApp.Stop()
			// Signal the TUI that shutdown is complete so it can quit.
			mainProgram.Send(events.ShutdownCompleteMsg{})
		}()
	})

	mainModel.SetOnQuit(func() {
		cooperApp.Stop()
	})

	// Run the main TUI.
	if _, err := mainProgram.Run(); err != nil {
		return fmt.Errorf("TUI: %w", err)
	}

	return nil
}

// cleanupServices stops the ACL listener and bridge server if they were started.
func cleanupServices(acl *proxy.ACLListener, br *bridge.BridgeServer) {
	if acl != nil {
		acl.Stop()
	}
	if br != nil {
		br.Stop()
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

func runCLI(cmd *cobra.Command, args []string) error {
	// 1. Check proxy is running.
	running, err := docker.IsProxyRunning()
	if err != nil {
		return fmt.Errorf("check proxy: %w", err)
	}
	if !running {
		return fmt.Errorf("proxy is not running. Start it with 'cooper up' first")
	}

	// 2. Load config.
	cfg, cooperDir, err := loadConfig()
	if err != nil {
		return err
	}

	// 3. Resolve tokens.
	workspaceDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	var enabledTools []string
	for _, t := range cfg.AITools {
		if t.Enabled {
			enabledTools = append(enabledTools, t.Name)
		}
	}

	tokens, err := auth.ResolveTokens(workspaceDir, cooperDir, enabledTools)
	if err != nil {
		return fmt.Errorf("resolve tokens: %w", err)
	}

	// 4. Determine barrel container name.
	containerName := docker.BarrelContainerName(workspaceDir)

	// 5. If barrel not running, start it.
	barrelRunning, err := docker.IsBarrelRunning(containerName)
	if err != nil {
		return fmt.Errorf("check barrel: %w", err)
	}
	if !barrelRunning {
		fmt.Fprintf(os.Stderr, "Starting barrel container %s...\n", containerName)
		if err := docker.StartBarrel(cfg, workspaceDir, cooperDir); err != nil {
			return fmt.Errorf("start barrel: %w", err)
		}
	}

	// 6. Generate random name.
	sessionName := names.Generate(workspaceDir)
	defer names.Release(sessionName)

	// 7. Set terminal title.
	dirName := filepath.Base(workspaceDir)
	termTitle := fmt.Sprintf("%s-%s", dirName, sessionName)
	fmt.Fprintf(os.Stdout, "\033]0;%s\007", termTitle)

	// 8. Build environment variables for the exec.
	var envArgs []string
	for _, t := range tokens {
		envArgs = append(envArgs, fmt.Sprintf("%s=%s", t.Name, t.Value))
	}

	// 9. Execute shell or one-shot command.
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

	return nil
}

// ---------- cooper proof ----------

func runProof(cmd *cobra.Command, args []string) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}

	// Determine the barrel container for the current workspace.
	workspaceDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	containerName := docker.BarrelContainerName(workspaceDir)

	// Check that the barrel is running.
	running, err := docker.IsBarrelRunning(containerName)
	if err != nil {
		return fmt.Errorf("check barrel: %w", err)
	}
	if !running {
		return fmt.Errorf("no barrel running for this workspace (%s). Start one with 'cooper cli' first", containerName)
	}

	// Run all checks.
	results, err := proof.RunAllChecks(containerName, cfg)
	if err != nil {
		return fmt.Errorf("run diagnostics: %w", err)
	}

	// Format and print results.
	fmt.Print(proof.FormatResults(results))
	return nil
}

// ---------- cooper cleanup ----------

func runCleanup(cmd *cobra.Command, args []string) error {
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
	}

	// 2. Stop proxy.
	fmt.Fprintln(os.Stderr, "Stopping proxy...")
	if err := docker.StopProxy(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not stop proxy: %v\n", err)
	}

	// 3. Remove images.
	fmt.Fprintln(os.Stderr, "Removing Docker images...")
	for _, img := range []string{docker.GetImageBarrel(), docker.GetImageBarrelBase(), docker.GetImageProxy()} {
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
	cooperDir, err := resolveCooperDir()
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

func runUpdate(cmd *cobra.Command, args []string) error {
	cfg, cooperDir, err := loadConfig()
	if err != nil {
		return err
	}

	langChanged := false
	aiChanged := false

	// 1. Check each tool in latest/mirror mode for mismatches.
	allTools := append(cfg.ProgrammingTools, cfg.AITools...)
	for i, tool := range allTools {
		if !tool.Enabled {
			continue
		}
		isProg := i < len(cfg.ProgrammingTools)

		switch tool.Mode {
		case config.ModeLatest:
			fmt.Fprintf(os.Stderr, "Checking latest version for %s...\n", tool.Name)
			latest, err := config.ResolveLatestVersion(tool.Name)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: could not resolve latest %s: %v\n", tool.Name, err)
				continue
			}
			if latest != tool.ContainerVersion {
				fmt.Fprintf(os.Stderr, "  %s: container=%s, latest=%s (mismatch)\n",
					tool.Name, tool.ContainerVersion, latest)
				if isProg {
					langChanged = true
					cfg.ProgrammingTools[i].PinnedVersion = latest
				} else {
					aiChanged = true
					cfg.AITools[i-len(cfg.ProgrammingTools)].PinnedVersion = latest
				}
			}

		case config.ModeMirror:
			fmt.Fprintf(os.Stderr, "Checking host version for %s...\n", tool.Name)
			hostVer, err := config.DetectHostVersion(tool.Name)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: could not detect host %s: %v\n", tool.Name, err)
				continue
			}
			if hostVer != tool.ContainerVersion {
				fmt.Fprintf(os.Stderr, "  %s: container=%s, host=%s (mismatch)\n",
					tool.Name, tool.ContainerVersion, hostVer)
				if isProg {
					langChanged = true
					cfg.ProgrammingTools[i].HostVersion = hostVer
				} else {
					aiChanged = true
					cfg.AITools[i-len(cfg.ProgrammingTools)].HostVersion = hostVer
				}
			}
		}
	}

	if !langChanged && !aiChanged {
		fmt.Fprintln(os.Stderr, "All tool versions match. No rebuild needed.")
		return nil
	}

	// Only reload Squid if an AI tool changed (AI tools affect the domain whitelist).
	needsSquidReload := aiChanged

	// 2. Regenerate templates.
	fmt.Fprintln(os.Stderr, "Regenerating templates...")
	cliDir := filepath.Join(cooperDir, "cli")
	if err := templates.WriteAllTemplates(cliDir, cfg); err != nil {
		return fmt.Errorf("write templates: %w", err)
	}

	// Regenerate squid.conf only if AI tools changed.
	if aiChanged {
		proxyDir := filepath.Join(cooperDir, "proxy")
		if err := writeProxyTemplates(proxyDir, cfg); err != nil {
			return fmt.Errorf("write proxy templates: %w", err)
		}
	}

	// 3. Stage CA cert into CLI build context (same fix as runBuild).
	caCert := filepath.Join(cooperDir, "ca", "cooper-ca.pem")
	if fileExists(caCert) {
		if err := copyFile(caCert, filepath.Join(cliDir, "cooper-ca.pem")); err != nil {
			return fmt.Errorf("stage CA cert for CLI: %w", err)
		}
	}

	// 4. Rebuild CLI image with selective cache bust: only bust the layer
	// for the tool category that actually changed.
	fmt.Fprintln(os.Stderr, "Rebuilding barrel-base image...")
	cliDockerfile := filepath.Join(cliDir, "Dockerfile")
	cacheBust := fmt.Sprintf("%d", time.Now().Unix())
	buildArgs := map[string]string{
		"USER_UID": fmt.Sprintf("%d", os.Getuid()),
		"USER_GID": fmt.Sprintf("%d", os.Getgid()),
	}
	if langChanged {
		buildArgs["CACHE_BUST_LANG"] = cacheBust
	}
	if aiChanged {
		buildArgs["CACHE_BUST_AI"] = cacheBust
	}
	if err := docker.BuildImage(docker.GetImageBarrelBase(), cliDockerfile, cliDir, buildArgs, false); err != nil {
		return fmt.Errorf("rebuild barrel-base: %w", err)
	}

	// Rebuild or re-tag barrel.
	userDockerfile := filepath.Join(cliDir, "Dockerfile.user")
	if fileExists(userDockerfile) {
		fmt.Fprintln(os.Stderr, "Rebuilding barrel from Dockerfile.user...")
		if err := docker.BuildImage(docker.GetImageBarrel(), userDockerfile, cliDir, nil, false); err != nil {
			return fmt.Errorf("rebuild barrel: %w", err)
		}
	} else {
		if err := docker.TagImage(docker.GetImageBarrelBase(), docker.GetImageBarrel()); err != nil {
			return fmt.Errorf("re-tag barrel: %w", err)
		}
	}

	// 4. Hot-reload squid if needed.
	if needsSquidReload {
		proxyRunning, _ := docker.IsProxyRunning()
		if proxyRunning {
			fmt.Fprintln(os.Stderr, "Hot-reloading Squid configuration...")
			if err := docker.ReconfigureSquid(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: squid reconfigure failed: %v\n", err)
			}
		}
	}

	// 5. Update ContainerVersion and save config.
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

	logsModel := bridgeui.NewLogsModel(cfg.BridgeLogLimit)
	mainModel.SetBridgeLogsModel(logsModel)

	routesModel := bridgeui.NewRoutesModel()
	routesModel.SetRoutes(cfg.BridgeRoutes)
	mainModel.SetBridgeRoutesModel(routesModel)

	tuiSettingsModel := settings.New(
		cfg.MonitorTimeoutSecs,
		cfg.BlockedHistoryLimit,
		cfg.AllowedHistoryLimit,
		cfg.BridgeLogLimit,
	)
	tuiSettingsModel.SetPortForwardRules(cfg.PortForwardRules)
	mainModel.SetSettingsModel(tuiSettingsModel)
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
		case "bridge-logs":
			mainModel.SetActiveTab(theme.TabBridgeLogs)
		case "bridge-routes":
			mainModel.SetActiveTab(theme.TabBridgeRoutes)
		case "settings":
			mainModel.SetActiveTab(theme.TabConfigure)
		case "about":
			mainModel.SetActiveTab(theme.TabAbout)
		case "loading":
			// The loading screen uses a non-standard tea.Model (returns Model, not tea.Model).
			// It's tested via the real `cooper up` startup flow. For tui-test, print a note.
			fmt.Println("Loading screen is a transient state tested via `cooper up`.")
			fmt.Println("To see it, run: cooper up (with Docker running).")
			return nil
		case "configure":
			_, err := configure.Run("/tmp/cooper-test")
			return err
		default:
			return fmt.Errorf("unknown screen: %s\nAvailable: containers, monitor, blocked, allowed, bridge-logs, bridge-routes, settings, about, loading, configure", tuiTestScreen)
		}
	}

	// Run the TUI.
	p := tea.NewProgram(mainModel, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

// removeImageIfExists removes a Docker image if it exists.
// Returns nil when the image does not exist (tolerates "not found").
func removeImageIfExists(name string) error {
	exists, err := docker.ImageExists(name)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	return docker.RemoveImage(name)
}

// checkToolVersions inspects each enabled tool for version mismatches.
// For Mirror mode it detects the host version; for Latest mode it resolves
// the latest upstream version (with a short timeout so startup is not blocked
// forever). Returns a slice of human-readable warning strings.
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
				// Cannot detect host version; skip silently.
				continue
			}
			expected = hostVer
		case config.ModeLatest:
			// Use a short timeout so we don't block startup.
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
				// Timed out resolving latest version; skip.
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
