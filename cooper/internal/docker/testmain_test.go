package docker

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
)

const dockerTestRuntimeNamespace = "cooper-gotest"
const dockerTestStateDirName = ".test-tmp"
const dockerTestLockFileName = "cooper-gotest.lock"

var (
	dockerTestLockMu   sync.Mutex
	dockerTestLockRefs int
	dockerTestLockFile *os.File
)

type dockerTestLock struct {
	released bool
}

func TestMain(m *testing.M) {
	logTestMain("starting package bootstrap")
	lock, err := setupDockerTests()
	if err != nil {
		fmt.Fprintf(os.Stderr, "docker test bootstrap failed: %v\n", err)
		os.Exit(1)
	}
	logTestMain("starting test execution")

	code := m.Run()

	logTestMain("cleaning package runtime resources")
	if err := CleanupRuntime(); err != nil {
		fmt.Fprintf(os.Stderr, "docker runtime cleanup failed: %v\n", err)
		if code == 0 {
			code = 1
		}
	}

	logTestMain("releasing shared docker test lock")
	if err := lock.Release(); err != nil {
		fmt.Fprintf(os.Stderr, "docker test lock release failed: %v\n", err)
		if code == 0 {
			code = 1
		}
	}

	os.Exit(code)
}

func setupDockerTests() (*dockerTestLock, error) {
	logTestMain("waiting for shared docker test lock")
	lock, err := acquireDockerTestLock()
	if err != nil {
		return nil, err
	}
	logTestMain("checking Docker availability")
	if err := requireDockerForPackageTests(); err != nil {
		lock.Release()
		return nil, err
	}
	SetRuntimeNamespace(dockerTestRuntimeNamespace)
	SetStopTimeoutSeconds(1)
	logTestMain(fmt.Sprintf("cleaning stale runtime resources in namespace %q", dockerTestRuntimeNamespace))
	if err := CleanupRuntime(); err != nil {
		lock.Release()
		return nil, err
	}
	logTestMain("package bootstrap complete")
	return lock, nil
}

func requireDockerForPackageTests() error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker not found on PATH: %w", err)
	}
	cmd := exec.Command("docker", "info")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker daemon is not available: %w\n%s", err, string(out))
	}
	return nil
}

func acquireDockerTestLock() (*dockerTestLock, error) {
	waitStart := time.Now()
	lockPath := dockerTestLockPath()
	dockerTestLockMu.Lock()
	defer dockerTestLockMu.Unlock()

	if dockerTestLockRefs == 0 {
		if err := os.MkdirAll(dockerTestStateDir(), 0o755); err != nil {
			return nil, fmt.Errorf("mkdir docker test state dir %s: %w", dockerTestStateDir(), err)
		}
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, fmt.Errorf("open docker test lock %s: %w", lockPath, err)
		}

		attempt := 0
		for {
			attempt++
			if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
				break
			} else if !isLockBusy(err) {
				f.Close()
				return nil, fmt.Errorf("acquire docker test lock %s: %w", lockPath, err)
			}

			logTestMain(fmt.Sprintf("shared docker test lock still busy on attempt %d after %s", attempt, time.Since(waitStart).Round(time.Millisecond)))
			time.Sleep(1 * time.Second)
		}
		dockerTestLockFile = f
	}

	dockerTestLockRefs++
	logTestMain(fmt.Sprintf("acquired shared docker test lock after %s", time.Since(waitStart).Round(time.Millisecond)))
	return &dockerTestLock{}, nil
}

func (l *dockerTestLock) Release() error {
	if l == nil || l.released {
		return nil
	}

	dockerTestLockMu.Lock()
	defer dockerTestLockMu.Unlock()

	l.released = true
	if dockerTestLockRefs == 0 {
		return nil
	}

	dockerTestLockRefs--
	if dockerTestLockRefs > 0 {
		return nil
	}

	if dockerTestLockFile == nil {
		return nil
	}

	if err := syscall.Flock(int(dockerTestLockFile.Fd()), syscall.LOCK_UN); err != nil {
		dockerTestLockFile.Close()
		dockerTestLockFile = nil
		return fmt.Errorf("unlock docker test lock %s: %w", dockerTestLockPath(), err)
	}
	err := dockerTestLockFile.Close()
	dockerTestLockFile = nil
	if err != nil {
		return fmt.Errorf("close docker test lock %s: %w", dockerTestLockPath(), err)
	}
	return nil
}

func dockerTestRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func dockerTestStateDir() string {
	return filepath.Join(dockerTestRoot(), dockerTestStateDirName)
}

func dockerTestLockPath() string {
	return filepath.Join(dockerTestStateDir(), dockerTestLockFileName)
}

func logTestMain(msg string) {
	fmt.Fprintf(os.Stderr, "[cooper test bootstrap][internal/docker][%s] %s\n", time.Now().Format("15:04:05"), msg)
}

func isLockBusy(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}
