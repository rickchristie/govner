package main

import (
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
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/logging"
)

const (
	cooperUpShutdownTimeout = 30 * time.Second
	cooperUpKillTimeout     = 10 * time.Second
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

	runtimeLock, err := stopRunningUp(cooperDir, os.Stderr)
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
	if err := docker.CleanupRuntime(); err != nil {
		errs = append(errs, err.Error())
	}

	if err := cleanupRuntimeState(cooperDir); err != nil {
		errs = append(errs, err.Error())
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
func stopRunningUp(cooperDir string, out io.Writer) (*upLock, error) {
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

	if err := waitForUpLockRelease(file, cooperUpShutdownTimeout); err == nil {
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

	fprintf(out, "cooper up did not exit after %s; killing process %d...\n", cooperUpShutdownTimeout, pid)
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

func cleanupRuntimeState(cooperDir string) error {
	var errs []string
	if err := removeRuntimeTokenFiles(cooperDir); err != nil {
		errs = append(errs, err.Error())
	}
	if err := os.RemoveAll(filepath.Join(cooperDir, "run")); err != nil {
		errs = append(errs, fmt.Sprintf("remove run directory: %v", err))
	}
	if err := docker.ResetBarrelTmpRoot(cooperDir); err != nil {
		errs = append(errs, err.Error())
	}
	if err := docker.ResetBarrelSessionRoot(cooperDir); err != nil {
		errs = append(errs, err.Error())
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

	prefix := docker.BarrelNamePrefix()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
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
