package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/launch"
	"github.com/rickchristie/govner/cooper/internal/runtimefs"
	"github.com/rickchristie/govner/cooper/internal/statelock"
	"github.com/rickchristie/govner/cooper/internal/vm"
	"github.com/rickchristie/govner/cooper/internal/vmguest"
	"github.com/rickchristie/govner/cooper/internal/vmhost"
	"github.com/rickchristie/govner/cooper/internal/vmrelay"
)

var (
	vmOneShot string
	vmCPUs    int
	vmMemory  string
	vmDisk    string
)

var vmCmd = &cobra.Command{
	Use:   "vm [tool-name] [profile]",
	Short: "Launch an AI tool in a secure VM with its own Docker daemon",
	Long: `Launches the selected AI CLI in a KVM virtual machine. It uses the same
workspace, selected-agent state, tools, proxy, clipboard, and settings as
cooper cli. The guest has its own Docker daemon and has no network device.

  cooper vm codex
  cooper vm codex Work
  cooper vm claude -c "go test ./..."
  cooper vm list
  cooper vm stop <runtime-id>
  cooper vm restart <runtime-id>
  cooper vm doctor
  cooper vm prepare`,
	Args: cobra.MaximumNArgs(2),
	RunE: runVM,
}

var vmSupervisorCmd = &cobra.Command{
	Use:    "__vm-supervisor",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, _ := cmd.Flags().GetString("config-file")
		config, err := vmhost.LoadSupervisorConfig(path)
		if err != nil {
			return err
		}
		ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		defer cancel()
		return vmhost.RunSupervisor(ctx, config, os.Stderr)
	},
}

var vmGuestCmd = &cobra.Command{
	Use:    "__vm-guest",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, _ := cmd.Flags().GetString("manifest")
		ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		defer cancel()
		err := vmguest.Run(ctx, path, os.Stderr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Cooper VM guest failed: %v\n", err)
		}
		return err
	},
}

var vmRelayCmd = &cobra.Command{
	Use:    "__vm-relay",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		socket, _ := cmd.Flags().GetString("socket")
		policy, _ := cmd.Flags().GetString("policy")
		logPath, _ := cmd.Flags().GetString("log")
		ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		defer cancel()
		return (vmrelay.Server{SocketPath: socket, PolicyPath: policy, LogPath: logPath}).Serve(ctx)
	},
}

func initVMCommands() {
	vmCmd.Flags().StringVarP(&vmOneShot, "command", "c", "", "Run a one-shot command in the VM agent")
	vmCmd.Flags().IntVar(&vmCPUs, "cpus", 0, "Override the VM CPU count")
	vmCmd.Flags().StringVar(&vmMemory, "memory", "", "Override VM memory, for example 6g or 4096m")
	vmCmd.Flags().StringVar(&vmDisk, "disk", "", "Override VM disk size, for example 24g")
	vmSupervisorCmd.Flags().String("config-file", "/cooper/runtime/supervisor.json", "Supervisor config path")
	vmGuestCmd.Flags().String("manifest", "/run/cooper/host/control/manifest.json", "Guest manifest path")
	vmRelayCmd.Flags().String("socket", "/cooper/relay/relay.sock", "Relay Unix socket")
	vmRelayCmd.Flags().String("policy", "/cooper/policy.json", "Relay policy path")
	vmRelayCmd.Flags().String("log", "", "Relay lifecycle log path")
	rootCmd.AddCommand(vmCmd, vmSupervisorCmd, vmGuestCmd, vmRelayCmd)
}

func runVM(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("specify a tool: cooper vm <tool-name>")
	}
	if args[0] == "list" {
		if len(args) != 1 {
			return fmt.Errorf("usage: cooper vm list")
		}
		runtimes, err := vm.List(cmd.Context(), docker.RuntimeNamespace(), nil)
		if err != nil {
			return err
		}
		if len(runtimes) == 0 {
			fmt.Fprintln(os.Stderr, "No Cooper VMs are running.")
			return nil
		}
		for _, runtime := range runtimes {
			fmt.Fprintln(os.Stdout, runtime)
		}
		return nil
	}
	cfg, cooperDir, err := loadConfig()
	if err != nil {
		return err
	}
	if args[0] == "prepare" {
		if len(args) != 1 {
			return fmt.Errorf("usage: cooper vm prepare")
		}
		fmt.Fprintln(os.Stderr, "Preparing the Cooper VM guest...")
		path, err := (vm.Preparer{CooperDir: cooperDir, Prefix: docker.ImagePrefix(), Out: os.Stderr}).Prepare(cmd.Context())
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Cooper VM guest is ready: %s\n", path)
		return nil
	}
	if args[0] == "doctor" {
		if len(args) != 1 {
			return fmt.Errorf("usage: cooper vm doctor")
		}
		return runVMDoctor(cmd.Context(), cfg, cooperDir)
	}
	depth, err := vm.ManagedDepth()
	if err != nil {
		return err
	}
	if depth > cfg.VM.MaxDepth {
		return fmt.Errorf("cooper VM depth %d exceeds the configured maximum %d", depth, cfg.VM.MaxDepth)
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	manager := vm.Manager{
		CooperDir: cooperDir, HomeDir: homeDir, Namespace: docker.RuntimeNamespace(),
		ImagePrefix: docker.ImagePrefix(), ProxyName: docker.ProxyContainerName(), Config: cfg, Out: os.Stderr,
	}
	if args[0] == "stop" || args[0] == "restart" {
		if len(args) != 2 {
			return fmt.Errorf("usage: cooper vm %s <runtime-id>", args[0])
		}
		if args[0] == "stop" {
			return manager.StopID(cmd.Context(), args[1])
		}
		_, err := manager.Restart(cmd.Context(), args[1])
		return err
	}
	toolName := strings.ToLower(strings.TrimSpace(args[0]))
	imageRef := docker.GetImageCLI(toolName)
	exists, err := docker.ImageExists(imageRef)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("no image found for %q. Run 'cooper build' first", toolName)
	}
	clipboardMode, err := docker.ToolClipboardMode(toolName)
	if err != nil {
		return err
	}
	proxyRunning, err := docker.IsProxyRunning()
	if err != nil {
		return fmt.Errorf("check proxy: %w", err)
	}
	if !proxyRunning {
		return fmt.Errorf("cooper is not running; start 'cooper up' in another terminal")
	}
	workspaceDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get workspace: %w", err)
	}
	workspaceDir, err = filepath.Abs(workspaceDir)
	if err != nil {
		return err
	}
	stateLock, err := statelock.Acquire(cmd.Context(), false)
	if err != nil {
		return err
	}
	defer stateLock.Close()
	selection, err := selectLaunchProfile(cmd.Context(), cooperDir, workspaceDir, homeDir, toolName, args[1:])
	if err != nil {
		return err
	}
	runtimeID, err := vm.ProfileRuntimeID(docker.RuntimeNamespace(), workspaceDir, toolName, selection.ID)
	if err != nil {
		return err
	}
	if _, err := runtimefs.SyncTimezoneFile(cooperDir, runtimeID); err != nil {
		return fmt.Errorf("sync VM timezone: %w", err)
	}
	preparedSession, warnings, err := launch.PrepareSession(launch.SessionRequest{
		Config: cfg, CooperDir: cooperDir, RuntimeID: runtimeID,
		ToolName: toolName, WorkspaceDir: workspaceDir, OneShot: vmOneShot,
		State: &selection,
	})
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := preparedSession.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: clean session files: %v\n", closeErr)
		}
	}()
	for _, warning := range warnings {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", warning)
	}
	memoryMiB, err := parseVMSize(vmMemory, "memory", 1)
	if err != nil {
		return err
	}
	diskGiB, err := parseVMSize(vmDisk, "disk", 1024)
	if err != nil {
		return err
	}
	runtime, err := manager.Start(cmd.Context(), vm.StartRequest{
		WorkspaceDir: workspaceDir, ToolName: toolName, ImageRef: imageRef,
		RuntimeID: runtimeID, CPUs: vmCPUs, MemoryMiB: memoryMiB, DiskGiB: diskGiB,
		ClipboardMode: clipboardMode,
		ProfileID:     selection.ID,
	})
	if err != nil {
		return err
	}
	if err := stateLock.Close(); err != nil {
		return err
	}
	return executeVMSession(cmd.Context(), manager, runtime, preparedSession)
}

func runVMDoctor(ctx context.Context, cfg *config.Config, cooperDir string) error {
	failures := 0
	fmt.Fprintln(os.Stdout, "Cooper VM doctor")
	if err := vm.HostRequirements(true); err != nil {
		fmt.Fprintf(os.Stdout, "[FAIL] Host: unavailable: %v\n", err)
		failures++
	} else {
		fmt.Fprintln(os.Stdout, "[OK]   Host: Linux x86-64, Docker, KVM, and nested KVM are ready")
	}
	fmt.Fprintf(os.Stdout, "[INFO] Defaults: %d CPUs, %d MiB memory, %d GiB disk, depth %d\n",
		cfg.VM.CPUs, cfg.VM.MemoryMiB, cfg.VM.DiskGiB, cfg.VM.MaxDepth)
	for _, status := range vm.InfrastructureStatuses(ctx, docker.ImagePrefix(), nil) {
		if !status.Ready {
			fmt.Fprintf(os.Stdout, "[INFO] Image %s: %s\n", status.Name, status.Detail)
			continue
		}
		fmt.Fprintf(os.Stdout, "[OK]   Image %s: %s\n", status.Name, status.ImageID)
	}
	prepared, detail := vm.PreparedStatus(cooperDir)
	if prepared {
		fmt.Fprintf(os.Stdout, "[OK]   Guest base: %s\n", detail)
	} else {
		fmt.Fprintf(os.Stdout, "[INFO] Guest base: %s; run 'cooper vm prepare'\n", detail)
	}
	infos, err := vm.ListInfo(ctx, docker.RuntimeNamespace(), nil)
	if err != nil {
		fmt.Fprintf(os.Stdout, "[FAIL] Running VMs: unavailable: %v\n", err)
		failures++
	} else if len(infos) == 0 {
		fmt.Fprintln(os.Stdout, "[INFO] Running VMs: none")
	} else {
		homeDir, _ := os.UserHomeDir()
		manager := vm.Manager{
			CooperDir: cooperDir, HomeDir: homeDir, Namespace: docker.RuntimeNamespace(),
			ImagePrefix: docker.ImagePrefix(), ProxyName: docker.ProxyContainerName(), Config: cfg,
		}
		for _, info := range infos {
			runtimeDir := vm.RuntimeDir(cooperDir, info.ID)
			controlDir := vm.ControlDir(cooperDir, info.ID)
			runtime := vm.Runtime{
				ID: info.ID, ToolName: info.ToolName, WorkspaceDir: info.WorkspaceDir,
				ContainerName: info.ID, RelayName: vm.RelayContainerName(info.ID),
				RelayNetwork: vm.RelayNetworkName(info.ID), RuntimeDir: runtimeDir,
				ControlDir: controlDir, ControlSocket: vm.ControlSocketPath(cooperDir, info.ID), Depth: info.Depth,
			}
			diagnostic, err := manager.GuestDiagnostic(ctx, runtime)
			if err != nil {
				fmt.Fprintf(os.Stdout, "[FAIL] VM %s: unhealthy: %v\n", info.ID, err)
				failures++
				continue
			}
			fmt.Fprintf(os.Stdout, "[OK]   VM %s: depth %d, interfaces %s, external none, default routes none, Docker %s, KVM %t\n",
				info.ID, diagnostic.Depth, strings.Join(diagnostic.Interfaces, ","), diagnostic.DockerVersion, diagnostic.KVMAvailable)
		}
	}
	if failures > 0 {
		return fmt.Errorf("cooper VM doctor found %d failure(s)", failures)
	}
	return nil
}

func executeVMSession(ctx context.Context, manager vm.Manager, runtime vm.Runtime, session *launch.Session) error {
	if session.Interactive {
		fmt.Fprintf(os.Stdout, "\033]0;%s\007", session.Title)
		defer func() {
			fmt.Fprint(os.Stdout, "\033]0;\007")
			fmt.Print("\n  \033[38;5;130m🥃 VM sealed. Back on host.\033[0m\n\n")
		}()
	}
	return manager.ExecCommand(ctx, runtime, session.Command, session.Environment, session.Interactive, os.Stdin, os.Stdout, os.Stderr)
}

// parseVMSize returns MiB. divisor converts the MiB result for disk GiB.
func parseVMSize(value, name string, divisor int) (int, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return 0, nil
	}
	multiplier := 1
	suffix := value[len(value)-1]
	if suffix != 'g' && suffix != 'm' && divisor == 1024 {
		multiplier = 1024
	}
	if suffix == 'g' || suffix == 'm' {
		value = strings.TrimSpace(value[:len(value)-1])
		if suffix == 'g' {
			multiplier = 1024
		}
	}
	number, err := strconv.Atoi(value)
	if err != nil || number <= 0 {
		return 0, fmt.Errorf("invalid VM %s size %q", name, value)
	}
	mebibytes := number * multiplier
	if mebibytes%divisor != 0 {
		return 0, fmt.Errorf("VM %s must use whole GiB", name)
	}
	return mebibytes / divisor, nil
}
