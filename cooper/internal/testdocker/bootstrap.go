package testdocker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/templates"
)

// ImagePrefix isolates go-test Docker images from production images and from
// the shell-based test scripts that use test-mirror/test-latest/test-pinned.
const ImagePrefix = "cooper-gotest-"

// RuntimeNamespace isolates go-test Docker containers and networks from a
// user's live `cooper up` runtime.
const RuntimeNamespace = "cooper-gotest"

const (
	lockPath      = "/tmp/cooper-gotest.lock"
	buildDir      = "/tmp/cooper-gotest-build"
	buildStampRel = "build.stamp"
)

var (
	lockMu   sync.Mutex
	lockRefs int
	lockFile *os.File
)

// Lock is a re-entrant process-local handle backed by a cross-process flock.
// Multiple acquisitions in the same process share the same underlying lock.
type Lock struct {
	released bool
}

// AcquireLock serializes Docker-backed test packages across go test processes.
func AcquireLock() (*Lock, error) {
	lockMu.Lock()
	defer lockMu.Unlock()

	if lockRefs == 0 {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, fmt.Errorf("open docker test lock %s: %w", lockPath, err)
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
			f.Close()
			return nil, fmt.Errorf("acquire docker test lock %s: %w", lockPath, err)
		}
		lockFile = f
	}

	lockRefs++
	return &Lock{}, nil
}

// Release releases one reference to the shared package/process lock.
func (l *Lock) Release() error {
	if l == nil || l.released {
		return nil
	}

	lockMu.Lock()
	defer lockMu.Unlock()

	l.released = true
	if lockRefs == 0 {
		return nil
	}

	lockRefs--
	if lockRefs > 0 {
		return nil
	}

	if lockFile == nil {
		return nil
	}

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN); err != nil {
		lockFile.Close()
		lockFile = nil
		return fmt.Errorf("unlock docker test lock %s: %w", lockPath, err)
	}
	err := lockFile.Close()
	lockFile = nil
	if err != nil {
		return fmt.Errorf("close docker test lock %s: %w", lockPath, err)
	}
	return nil
}

// SetupPackage acquires the shared Docker test lock, verifies Docker access,
// sets the dedicated image prefix, and optionally rebuilds the shared test
// images for this test run.
func SetupPackage(ensureImages bool) (*Lock, error) {
	lock, err := AcquireLock()
	if err != nil {
		return nil, err
	}
	if err := requireDocker(); err != nil {
		lock.Release()
		return nil, err
	}

	docker.SetImagePrefix(ImagePrefix)
	docker.SetRuntimeNamespace(RuntimeNamespace)
	if err := docker.CleanupRuntime(); err != nil {
		lock.Release()
		return nil, err
	}
	if ensureImages {
		if err := ensureTestImagesLocked(); err != nil {
			lock.Release()
			return nil, err
		}
	}
	return lock, nil
}

// EnsureTestImages rebuilds the shared go-test images using Docker cache.
func EnsureTestImages() error {
	lock, err := SetupPackage(true)
	if err != nil {
		return err
	}
	return lock.Release()
}

// AssignDynamicPorts updates cfg with currently-free localhost ports for the
// proxy and bridge so tests do not collide with a live `cooper up`.
func AssignDynamicPorts(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("assign dynamic ports: nil config")
	}

	proxyPort, err := findFreeTCPPort()
	if err != nil {
		return fmt.Errorf("assign proxy port: %w", err)
	}
	bridgePort, err := findFreeTCPPort()
	if err != nil {
		return fmt.Errorf("assign bridge port: %w", err)
	}
	for bridgePort == proxyPort {
		bridgePort, err = findFreeTCPPort()
		if err != nil {
			return fmt.Errorf("reassign bridge port: %w", err)
		}
	}

	cfg.ProxyPort = proxyPort
	cfg.BridgePort = bridgePort
	return nil
}

func requireDocker() error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker not found on PATH: %w", err)
	}
	cmd := exec.Command("docker", "info")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker daemon is not available: %w\n%s", err, string(out))
	}
	return nil
}

func ensureTestImagesLocked() error {
	root := cooperRoot()
	cfgPath := filepath.Join(root, ".testfiles", "config-pinned.json")
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("load pinned test config %s: %w", cfgPath, err)
	}
	fingerprint, err := buildFingerprint(root)
	if err != nil {
		return fmt.Errorf("compute shared image fingerprint: %w", err)
	}
	upToDate, err := sharedImagesUpToDate(cfg, fingerprint)
	if err != nil {
		return fmt.Errorf("check shared images: %w", err)
	}
	if upToDate {
		return nil
	}

	if err := os.RemoveAll(buildDir); err != nil {
		return fmt.Errorf("reset test build dir %s: %w", buildDir, err)
	}
	for _, dir := range []string{
		buildDir,
		filepath.Join(buildDir, "base"),
		filepath.Join(buildDir, "cli"),
		filepath.Join(buildDir, "proxy"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	buildStampPath := filepath.Join(buildDir, buildStampRel)

	cfgPathOut := filepath.Join(buildDir, "config.json")
	if err := config.SaveConfig(cfgPathOut, cfg); err != nil {
		return fmt.Errorf("save test config %s: %w", cfgPathOut, err)
	}

	if err := templates.WriteAllTemplates(filepath.Join(buildDir, "base"), filepath.Join(buildDir, "cli"), cfg); err != nil {
		return fmt.Errorf("write test cli templates: %w", err)
	}
	if err := templates.WriteProxyTemplates(filepath.Join(buildDir, "proxy"), cfg); err != nil {
		return fmt.Errorf("write test proxy templates: %w", err)
	}
	if err := templates.WriteACLHelperSource(filepath.Join(buildDir, "proxy")); err != nil {
		return fmt.Errorf("write test ACL helper source: %w", err)
	}

	caCertPath, caKeyPath, err := config.EnsureCA(buildDir)
	if err != nil {
		return fmt.Errorf("ensure test CA: %w", err)
	}
	if err := copyFile(caCertPath, filepath.Join(buildDir, "base", "cooper-ca.pem")); err != nil {
		return fmt.Errorf("stage test CA into base dir: %w", err)
	}
	if err := copyFile(caCertPath, filepath.Join(buildDir, "proxy", "cooper-ca.pem")); err != nil {
		return fmt.Errorf("stage test CA into proxy dir: %w", err)
	}
	if err := copyFile(caKeyPath, filepath.Join(buildDir, "proxy", "cooper-ca-key.pem")); err != nil {
		return fmt.Errorf("stage test CA key into proxy dir: %w", err)
	}

	uidGidArgs := map[string]string{
		"USER_UID": fmt.Sprintf("%d", os.Getuid()),
		"USER_GID": fmt.Sprintf("%d", os.Getgid()),
	}

	if err := docker.BuildImage(
		docker.GetImageProxy(),
		filepath.Join(buildDir, "proxy", "proxy.Dockerfile"),
		filepath.Join(buildDir, "proxy"),
		uidGidArgs,
		false,
	); err != nil {
		return fmt.Errorf("build shared proxy image: %w", err)
	}

	if err := docker.BuildImage(
		docker.GetImageBase(),
		filepath.Join(buildDir, "base", "Dockerfile"),
		filepath.Join(buildDir, "base"),
		uidGidArgs,
		false,
	); err != nil {
		return fmt.Errorf("build shared base image: %w", err)
	}

	for _, tool := range cfg.AITools {
		if !tool.Enabled {
			continue
		}
		toolDir := filepath.Join(buildDir, "cli", tool.Name)
		if err := docker.BuildImage(
			docker.GetImageCLI(tool.Name),
			filepath.Join(toolDir, "Dockerfile"),
			toolDir,
			nil,
			false,
		); err != nil {
			return fmt.Errorf("build shared %s image: %w", tool.Name, err)
		}
	}

	if err := os.WriteFile(buildStampPath, []byte(fingerprint+"\n"), 0o644); err != nil {
		return fmt.Errorf("write shared image build stamp %s: %w", buildStampPath, err)
	}

	return nil
}

func cooperRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

func findFreeTCPPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected listener address type %T", ln.Addr())
	}
	return addr.Port, nil
}

func sharedImagesUpToDate(cfg *config.Config, fingerprint string) (bool, error) {
	stampPath := filepath.Join(buildDir, buildStampRel)
	data, err := os.ReadFile(stampPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if strings.TrimSpace(string(data)) != fingerprint {
		return false, nil
	}

	requiredImages := []string{
		docker.GetImageProxy(),
		docker.GetImageBase(),
	}
	for _, tool := range cfg.AITools {
		if tool.Enabled {
			requiredImages = append(requiredImages, docker.GetImageCLI(tool.Name))
		}
	}
	for _, imageName := range requiredImages {
		exists, err := docker.ImageExists(imageName)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, nil
		}
	}
	return true, nil
}

func buildFingerprint(root string) (string, error) {
	h := sha256.New()
	paths := []string{
		filepath.Join(root, ".testfiles", "config-pinned.json"),
		filepath.Join(root, "internal", "templates"),
		filepath.Join(root, "internal", "aclsrc"),
		filepath.Join(root, "internal", "x11src"),
	}
	for _, path := range paths {
		if err := hashPath(h, root, path); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashPath(h hash.Hash, root, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return hashFile(h, root, path)
	}

	var files []string
	if err := filepath.Walk(path, func(candidate string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		files = append(files, candidate)
		return nil
	}); err != nil {
		return err
	}

	sort.Strings(files)
	for _, file := range files {
		if err := hashFile(h, root, file); err != nil {
			return err
		}
	}
	return nil
}

func hashFile(h hash.Hash, root, path string) error {
	relPath, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	if _, err := h.Write([]byte(relPath)); err != nil {
		return err
	}
	if _, err := h.Write([]byte{0}); err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if _, err := h.Write(data); err != nil {
		return err
	}
	if _, err := h.Write([]byte{0}); err != nil {
		return err
	}
	return nil
}
