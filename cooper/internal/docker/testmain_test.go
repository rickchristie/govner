package docker

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
)

const dockerTestLockPath = "/tmp/cooper-gotest.lock"
const dockerTestRuntimeNamespace = "cooper-gotest"

var (
	dockerTestLockMu   sync.Mutex
	dockerTestLockRefs int
	dockerTestLockFile *os.File
)

type dockerTestLock struct {
	released bool
}

func TestMain(m *testing.M) {
	lock, err := setupDockerTests()
	if err != nil {
		fmt.Fprintf(os.Stderr, "docker test bootstrap failed: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	if err := CleanupRuntime(); err != nil {
		fmt.Fprintf(os.Stderr, "docker runtime cleanup failed: %v\n", err)
		if code == 0 {
			code = 1
		}
	}

	if err := lock.Release(); err != nil {
		fmt.Fprintf(os.Stderr, "docker test lock release failed: %v\n", err)
		if code == 0 {
			code = 1
		}
	}

	os.Exit(code)
}

func setupDockerTests() (*dockerTestLock, error) {
	lock, err := acquireDockerTestLock()
	if err != nil {
		return nil, err
	}
	if err := requireDockerForPackageTests(); err != nil {
		lock.Release()
		return nil, err
	}
	SetRuntimeNamespace(dockerTestRuntimeNamespace)
	if err := CleanupRuntime(); err != nil {
		lock.Release()
		return nil, err
	}
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
	dockerTestLockMu.Lock()
	defer dockerTestLockMu.Unlock()

	if dockerTestLockRefs == 0 {
		f, err := os.OpenFile(dockerTestLockPath, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, fmt.Errorf("open docker test lock %s: %w", dockerTestLockPath, err)
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
			f.Close()
			return nil, fmt.Errorf("acquire docker test lock %s: %w", dockerTestLockPath, err)
		}
		dockerTestLockFile = f
	}

	dockerTestLockRefs++
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
		return fmt.Errorf("unlock docker test lock %s: %w", dockerTestLockPath, err)
	}
	err := dockerTestLockFile.Close()
	dockerTestLockFile = nil
	if err != nil {
		return fmt.Errorf("close docker test lock %s: %w", dockerTestLockPath, err)
	}
	return nil
}
