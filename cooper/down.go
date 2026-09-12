package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/rickchristie/govner/cooper/internal/clipboard"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/logging"
	"github.com/rickchristie/govner/cooper/internal/vm"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

const (
	cooperUpShutdownGrace = 15 * time.Second
	cooperUpKillTimeout   = 10 * time.Second
)

func runDown(cmd *cobra.Command, args []string) error {
	cooperDir, err := resolveCooperDir()
	if err != nil {
		return err
	}

	logDir := filepath.Join(cooperDir, "logs")
	dl := logging.NewCmdLogger(logDir, "down")
	defer dl.Close()
	dl.LogStart()

	cfg, _, configErr := loadConfig()
	if configErr != nil {
		cfg = config.DefaultConfig()
	}
	shutdownTimeout := time.Duration(cfg.VM.StopTimeoutS)*time.Second + cooperUpShutdownGrace
	runtimeLock, err := stopRunningUpWithin(cooperDir, os.Stderr, shutdownTimeout)
	if err != nil {
		err = fmt.Errorf("stop cooper up process: %w", err)
		dl.LogDone(err)
		return err
	}
	defer func() {
		if err := runtimeLock.Release(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: release cooper runtime lock: %v\n", err)
		}
	}()

	var errs []string
	fmt.Fprintln(os.Stderr, "Stopping Cooper Docker runtime...")
	manager := vm.Manager{
		CooperDir: cooperDir,
		Namespace: docker.RuntimeNamespace(),
		ProxyName: docker.ProxyContainerName(),
		Config:    cfg,
	}
	vmsStopped := true
	if err := manager.StopAll(cmd.Context()); err != nil {
		vmsStopped = false
		errs = append(errs, fmt.Sprintf("stop Cooper VMs: %v", err))
	}
	if err := docker.CleanupRuntime(); err != nil {
		errs = append(errs, err.Error())
	}

	if err := cleanupRuntimeState(cooperDir, vmsStopped); err != nil {
		errs = append(errs, err.Error())
	}
	if !vmsStopped {
		errs = append(errs, "preserved VM-backed temporary, session, and runtime files because a VM did not stop")
	}

	if len(errs) > 0 {
		err := fmt.Errorf("cooper down: %s", strings.Join(errs, "; "))
		dl.LogDone(err)
		return err
	}

	fmt.Fprintln(os.Stderr, "Cooper runtime stopped.")
	dl.LogDone(nil)
	return nil
}

// stopRunningUp signals the process that owns the cooper up lock and returns
// with that same lock held. Holding it through Docker cleanup prevents a new
// cooper up from starting halfway through cooper down.
func stopRunningUpWithin(cooperDir string, out io.Writer, shutdownTimeout time.Duration) (*upLock, error) {
	path := upLockPath(cooperDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create cooper up lock directory: %w", err)
	}

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open cooper up lock %s: %w", path, err)
	}

	if err := tryLockUpFile(file); err == nil {
		if err := clearUpLockFile(file); err != nil {
			_ = unlockUpFile(file)
			_ = file.Close()
			return nil, err
		}
		fprintf(out, "No running cooper up process found.\n")
		return &upLock{file: file, path: path}, nil
	} else if !isLockBusy(err) {
		_ = file.Close()
		return nil, fmt.Errorf("check cooper up lock: %w", err)
	}

	pid := readPIDFromUpLock(file)
	if pid <= 0 {
		_ = file.Close()
		return nil, fmt.Errorf("cooper up lock is held but %s does not contain a valid pid", path)
	}

	fprintf(out, "Stopping cooper up process %d...\n", pid)
	if err := signalProcess(pid, syscall.SIGTERM); err != nil && !isProcessNotFound(err) {
		_ = file.Close()
		return nil, fmt.Errorf("send SIGTERM to cooper up process %d: %w", pid, err)
	}

	if err := waitForUpLockRelease(file, shutdownTimeout); err == nil {
		if err := clearUpLockFile(file); err != nil {
			_ = unlockUpFile(file)
			_ = file.Close()
			return nil, err
		}
		return &upLock{file: file, path: path}, nil
	} else if !errors.Is(err, errUpLockTimeout) {
		_ = file.Close()
		return nil, err
	}

	if err := tryLockUpFile(file); err == nil {
		if err := clearUpLockFile(file); err != nil {
			_ = unlockUpFile(file)
			_ = file.Close()
			return nil, err
		}
		return &upLock{file: file, path: path}, nil
	} else if !isLockBusy(err) {
		_ = file.Close()
		return nil, fmt.Errorf("check cooper up lock before kill: %w", err)
	}

	fprintf(out, "cooper up did not exit after %s; killing process %d...\n", shutdownTimeout, pid)
	if err := signalProcess(pid, syscall.SIGKILL); err != nil && !isProcessNotFound(err) {
		_ = file.Close()
		return nil, fmt.Errorf("send SIGKILL to cooper up process %d: %w", pid, err)
	}

	if err := waitForUpLockRelease(file, cooperUpKillTimeout); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := clearUpLockFile(file); err != nil {
		_ = unlockUpFile(file)
		_ = file.Close()
		return nil, err
	}
	return &upLock{file: file, path: path}, nil
}

var errUpLockTimeout = errors.New("timed out waiting for cooper up lock")

func waitForUpLockRelease(file *os.File, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := tryLockUpFile(file); err == nil {
			return nil
		} else if !isLockBusy(err) {
			return fmt.Errorf("wait for cooper up lock release: %w", err)
		}

		if time.Now().After(deadline) {
			return errUpLockTimeout
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func signalProcess(pid int, signal syscall.Signal) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Signal(signal)
}

func isProcessNotFound(err error) bool {
	return errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH)
}

func cleanupRuntimeState(cooperDir string, removeVMBackedState bool) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home directory before runtime cleanup: %w", err)
	}
	if err := workload.ValidateAllHostAgentStateRoots(homeDir, cooperDir); err != nil {
		return fmt.Errorf("refuse runtime cleanup: %w", err)
	}

	var errs []string
	if err := removeRuntimeTokenFiles(cooperDir); err != nil {
		errs = append(errs, err.Error())
	}
	if err := os.RemoveAll(filepath.Join(cooperDir, "run")); err != nil {
		errs = append(errs, fmt.Sprintf("remove run directory: %v", err))
	}
	if removeVMBackedState {
		if err := docker.ResetRuntimeTempRoot(cooperDir); err != nil {
			errs = append(errs, err.Error())
		}
		if err := docker.ResetRuntimeSessionRoot(cooperDir); err != nil {
			errs = append(errs, err.Error())
		}
		if err := os.RemoveAll(filepath.Join(cooperDir, "vm", "run")); err != nil {
			errs = append(errs, fmt.Sprintf("remove VM runtime directory: %v", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("cleanup runtime state: %s", strings.Join(errs, "; "))
	}
	return nil
}

// ensureNoRunningRuntimeBeforeUp catches stale or foreign Cooper runtime
// containers that do not own this config directory's lock. Users should run
// cooper down first so startup never races old containers or host services.
func ensureNoRunningRuntimeBeforeUp() error {
	var active []string
	proxyRunning, err := docker.IsProxyRunning()
	if err != nil {
		return fmt.Errorf("check proxy runtime: %w", err)
	}
	if proxyRunning {
		active = append(active, docker.ProxyContainerName())
	}

	barrels, err := docker.ListBarrels()
	if err != nil {
		return fmt.Errorf("check barrel runtime: %w", err)
	}
	for _, barrel := range barrels {
		active = append(active, barrel.Name)
	}
	vms, err := vm.List(context.Background(), docker.RuntimeNamespace(), nil)
	if err != nil {
		return fmt.Errorf("check VM runtime: %w", err)
	}
	active = append(active, vms...)

	if len(active) > 0 {
		return fmt.Errorf("cooper runtime is already active (%s); run 'cooper down' before starting it again", strings.Join(active, ", "))
	}
	return nil
}

func removeRuntimeTokenFiles(cooperDir string) error {
	tokenDir := filepath.Join(cooperDir, "tokens")
	entries, err := os.ReadDir(tokenDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read token directory %s: %w", tokenDir, err)
	}

	barrelPrefix := docker.BarrelNamePrefix()
	vmPrefix := strings.TrimSuffix(docker.RuntimeNamespace(), "-") + "-vm-"
	for _, entry := range entries {
		if entry.IsDir() || (!strings.HasPrefix(entry.Name(), barrelPrefix) && !strings.HasPrefix(entry.Name(), vmPrefix)) {
			continue
		}
		if err := clipboard.RemoveTokenFile(cooperDir, entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func fprintf(out io.Writer, format string, args ...any) {
	if out == nil {
		return
	}
	fmt.Fprintf(out, format, args...)
}
