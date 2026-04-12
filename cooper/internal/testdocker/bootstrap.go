package testdocker

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	"time"

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
	sharedStateDirName = ".test-tmp"
	lockFileName       = "cooper-gotest.lock"
	buildDirName       = "cooper-gotest-build"
	sharedCADirName    = "cooper-gotest-shared-ca"
	buildStampRel      = "build.stamp"

	// TestStopTimeoutSeconds keeps Docker-backed tests fail-fast. When a
	// container ignores SIGTERM or deadlocks during shutdown, we want test
	// teardown to surface that quickly instead of paying Docker's full
	// default grace period on every failing stop.
	TestStopTimeoutSeconds = 1
)

// SharedClipboardOffToolName is a prebuilt custom test barrel image that sets
// COOPER_CLIPBOARD_MODE=off. Docker-backed tests reuse it instead of building
// the same custom image repeatedly in multiple packages.
const SharedClipboardOffToolName = "test-clipboard-off"

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
	return acquireLock("testdocker")
}

func acquireLock(name string) (*Lock, error) {
	waitStart := time.Now()
	lockPath := sharedLockPath()
	logf(name, "waiting for shared docker test lock %s", lockPath)

	lockMu.Lock()
	defer lockMu.Unlock()

	if lockRefs == 0 {
		if err := os.MkdirAll(sharedStateDir(), 0o755); err != nil {
			return nil, fmt.Errorf("mkdir shared test state dir %s: %w", sharedStateDir(), err)
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

			logf(name, "shared docker test lock still busy on attempt %d after %s", attempt, time.Since(waitStart).Round(time.Millisecond))
			time.Sleep(1 * time.Second)
		}
		lockFile = f
	}

	lockRefs++
	logf(name, "acquired shared docker test lock after %s", time.Since(waitStart).Round(time.Millisecond))
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
		return fmt.Errorf("unlock docker test lock %s: %w", sharedLockPath(), err)
	}
	err := lockFile.Close()
	lockFile = nil
	if err != nil {
		return fmt.Errorf("close docker test lock %s: %w", sharedLockPath(), err)
	}
	return nil
}

// SetupPackage acquires the shared Docker test lock, verifies Docker access,
// sets the dedicated image prefix, and optionally rebuilds the shared test
// images for this test run.
func SetupPackage(ensureImages bool) (*Lock, error) {
	return SetupPackageNamed("testdocker", ensureImages)
}

// SetupPackageNamed is SetupPackage with a package label for progress logging.
func SetupPackageNamed(name string, ensureImages bool) (*Lock, error) {
	lock, err := acquireLock(name)
	if err != nil {
		return nil, err
	}
	logf(name, "checking Docker availability")
	if err := requireDocker(); err != nil {
		lock.Release()
		return nil, err
	}

	docker.SetImagePrefix(ImagePrefix)
	docker.SetRuntimeNamespace(RuntimeNamespace)
	docker.SetStopTimeoutSeconds(TestStopTimeoutSeconds)
	logf(name, "cleaning stale runtime resources in namespace %q", RuntimeNamespace)
	if err := docker.CleanupRuntime(); err != nil {
		lock.Release()
		return nil, err
	}
	if ensureImages {
		if err := ensureTestImagesLocked(name); err != nil {
			lock.Release()
			return nil, err
		}
	}
	logf(name, "package bootstrap complete")
	return lock, nil
}

// EnsureTestImages rebuilds the shared go-test images using Docker cache.
func EnsureTestImages() error {
	lock, err := SetupPackageNamed("ensure-images", true)
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

// FixOwnership resets a Cooper-managed directory back to the current user so
// t.TempDir cleanup can remove files created by containers. It uses the shared
// base image as a privileged helper, then falls back to a best-effort host
// chmod when Docker-based repair is unavailable.
func FixOwnership(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("fix ownership: empty path")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("fix ownership abs path %s: %w", path, err)
	}

	imageName := docker.GetImageBase()
	cmd := exec.Command(
		"docker", "run", "--rm",
		"--user", "root",
		"-v", absPath+":/target",
		"--entrypoint", "sh",
		imageName,
		"-c",
		fmt.Sprintf("chown -R %d:%d /target >/dev/null 2>&1 || true; chmod -R u+rwX /target >/dev/null 2>&1 || true", os.Getuid(), os.Getgid()),
	)
	if out, err := cmd.CombinedOutput(); err == nil {
		return nil
	} else if chmodErr := exec.Command("chmod", "-R", "u+rwX", absPath).Run(); chmodErr == nil {
		return nil
	} else {
		return fmt.Errorf("fix ownership for %s with %s failed: %w\n%s", absPath, imageName, err, string(out))
	}
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

func ensureTestImagesLocked(name string) error {
	root := cooperRoot()
	buildDir := sharedBuildDir()
	sharedCADir := sharedTestCADir()
	cfgPath := filepath.Join(root, ".testfiles", "config-pinned.json")
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("load pinned test config %s: %w", cfgPath, err)
	}
	fingerprint, err := buildFingerprint(root)
	if err != nil {
		return fmt.Errorf("compute shared image fingerprint: %w", err)
	}
	upToDate, reason, err := sharedImagesUpToDate(fingerprint)
	if err != nil {
		return fmt.Errorf("check shared images: %w", err)
	}
	if upToDate {
		logf(name, "shared Docker test images already up to date (%s)", reason)
		return nil
	}
	logf(name, "shared Docker test images need rebuild (%s)", reason)

	if _, err := os.Stat(buildDir); err == nil {
		if err := FixOwnership(buildDir); err != nil {
			return fmt.Errorf("repair shared build dir %s: %w", buildDir, err)
		}
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

	implicit, err := config.ResolveImplicitTools(cfg)
	if err != nil {
		return fmt.Errorf("resolve implicit tools for shared test images: %w", err)
	}
	if err := templates.WriteAllTemplates(filepath.Join(buildDir, "base"), filepath.Join(buildDir, "cli"), cfg, implicit); err != nil {
		return fmt.Errorf("write test cli templates: %w", err)
	}
	if err := templates.WriteProxyTemplates(filepath.Join(buildDir, "proxy"), cfg); err != nil {
		return fmt.Errorf("write test proxy templates: %w", err)
	}
	if err := templates.WriteACLHelperSource(filepath.Join(buildDir, "proxy")); err != nil {
		return fmt.Errorf("write test ACL helper source: %w", err)
	}

	if err := stageSharedTestCA(sharedCADir, buildDir); err != nil {
		return err
	}

	uidGidArgs := map[string]string{
		"USER_UID": fmt.Sprintf("%d", os.Getuid()),
		"USER_GID": fmt.Sprintf("%d", os.Getgid()),
	}

	logf(name, "building shared proxy image %q", docker.GetImageProxy())
	if err := docker.BuildImage(
		docker.GetImageProxy(),
		filepath.Join(buildDir, "proxy", "proxy.Dockerfile"),
		filepath.Join(buildDir, "proxy"),
		uidGidArgs,
		false,
	); err != nil {
		return fmt.Errorf("build shared proxy image: %w", err)
	}

	logf(name, "building shared base image %q", docker.GetImageBase())
	if err := docker.BuildImage(
		docker.GetImageBase(),
		filepath.Join(buildDir, "base", "Dockerfile"),
		filepath.Join(buildDir, "base"),
		uidGidArgs,
		false,
	); err != nil {
		return fmt.Errorf("build shared base image: %w", err)
	}

	for _, tool := range sharedBuiltToolNames() {
		toolDir := filepath.Join(buildDir, "cli", tool.Name)
		logf(name, "building shared CLI image %q", docker.GetImageCLI(tool.Name))
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

	for _, spec := range sharedCustomImageSpecs() {
		toolDir := filepath.Join(buildDir, "cli", spec.ToolName)
		if err := os.MkdirAll(toolDir, 0o755); err != nil {
			return fmt.Errorf("mkdir shared custom tool dir %s: %w", toolDir, err)
		}
		dockerfilePath := filepath.Join(toolDir, "Dockerfile")
		if err := os.WriteFile(dockerfilePath, []byte(spec.Dockerfile), 0o644); err != nil {
			return fmt.Errorf("write shared custom Dockerfile %s: %w", dockerfilePath, err)
		}
		logf(name, "building shared custom CLI image %q", docker.GetImageCLI(spec.ToolName))
		if err := docker.BuildImage(
			docker.GetImageCLI(spec.ToolName),
			dockerfilePath,
			toolDir,
			nil,
			false,
		); err != nil {
			return fmt.Errorf("build shared custom %s image: %w", spec.ToolName, err)
		}
	}

	if err := os.WriteFile(buildStampPath, []byte(fingerprint+"\n"), 0o644); err != nil {
		return fmt.Errorf("write shared image build stamp %s: %w", buildStampPath, err)
	}

	logf(name, "shared Docker test images ready (%s)", shortFingerprint(fingerprint))
	return nil
}

func cooperRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func sharedStateDir() string {
	return filepath.Join(cooperRoot(), sharedStateDirName)
}

func sharedLockPath() string {
	return filepath.Join(sharedStateDir(), lockFileName)
}

func sharedBuildDir() string {
	return filepath.Join(sharedStateDir(), buildDirName)
}

func sharedTestCADir() string {
	return filepath.Join(sharedStateDir(), sharedCADirName)
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// stageSharedTestCA copies a stable shared test CA into the ephemeral build
// context. The build dir is wiped on every rebuild, so generating the CA there
// would rotate it every time and invalidate Docker cache for downstream COPY
// and image layers. Keeping the source CA outside the wiped build dir makes
// rebuilds materially cheaper after the first seed.
func stageSharedTestCA(caRootDir, buildDir string) error {
	caCertPath, caKeyPath, err := config.EnsureCA(caRootDir)
	if err != nil {
		return fmt.Errorf("ensure shared test CA in %s: %w", caRootDir, err)
	}
	if err := copyFile(caCertPath, filepath.Join(buildDir, "base", "cooper-ca.pem")); err != nil {
		return fmt.Errorf("stage shared test CA into base dir: %w", err)
	}
	if err := copyFile(caCertPath, filepath.Join(buildDir, "proxy", "cooper-ca.pem")); err != nil {
		return fmt.Errorf("stage shared test CA into proxy dir: %w", err)
	}
	if err := copyFile(caKeyPath, filepath.Join(buildDir, "proxy", "cooper-ca-key.pem")); err != nil {
		return fmt.Errorf("stage shared test CA key into proxy dir: %w", err)
	}
	return nil
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

func sharedImagesUpToDate(fingerprint string) (bool, string, error) {
	buildDir := sharedBuildDir()
	stampPath := filepath.Join(buildDir, buildStampRel)
	data, err := os.ReadFile(stampPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, fmt.Sprintf("build stamp %s is missing", stampPath), nil
		}
		return false, "", err
	}
	stamp := strings.TrimSpace(string(data))
	if stamp != fingerprint {
		return false, fmt.Sprintf("fingerprint changed (%s -> %s)", shortFingerprint(stamp), shortFingerprint(fingerprint)), nil
	}

	requiredImages := []string{
		docker.GetImageProxy(),
		docker.GetImageBase(),
	}
	for _, tool := range sharedBuiltToolNames() {
		requiredImages = append(requiredImages, docker.GetImageCLI(tool.Name))
	}
	for _, spec := range sharedCustomImageSpecs() {
		requiredImages = append(requiredImages, docker.GetImageCLI(spec.ToolName))
	}
	for _, imageName := range requiredImages {
		exists, err := docker.ImageExists(imageName)
		if err != nil {
			return false, "", err
		}
		if !exists {
			return false, fmt.Sprintf("missing image %q", imageName), nil
		}
	}
	return true, fmt.Sprintf("fingerprint %s", shortFingerprint(fingerprint)), nil
}

func buildFingerprint(root string) (string, error) {
	h := sha256.New()
	paths := []string{
		filepath.Join(root, ".testfiles", "config-pinned.json"),
		filepath.Join(root, "internal", "config"),
		filepath.Join(root, "internal", "templates"),
		filepath.Join(root, "internal", "aclsrc"),
		filepath.Join(root, "internal", "x11src"),
	}
	for _, path := range paths {
		if err := hashPath(h, root, path); err != nil {
			return "", err
		}
	}
	for _, spec := range sharedCustomImageSpecs() {
		if _, err := h.Write([]byte(spec.ToolName)); err != nil {
			return "", err
		}
		if _, err := h.Write([]byte{0}); err != nil {
			return "", err
		}
		if _, err := h.Write([]byte(spec.Dockerfile)); err != nil {
			return "", err
		}
		if _, err := h.Write([]byte{0}); err != nil {
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

func shortFingerprint(fingerprint string) string {
	if len(fingerprint) <= 12 {
		return fingerprint
	}
	return fingerprint[:12]
}

func logf(name, format string, args ...any) {
	if name == "" {
		name = "testdocker"
	}
	fmt.Fprintf(os.Stderr, "[cooper test bootstrap][%s][%s] %s\n",
		name,
		time.Now().Format("15:04:05"),
		fmt.Sprintf(format, args...),
	)
}

func isLockBusy(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}

type sharedToolSpec struct {
	Name string
}

type sharedCustomImageSpec struct {
	ToolName   string
	Dockerfile string
}

func sharedBuiltToolNames() []sharedToolSpec {
	// Keep the default shared image set to the minimal barrels that the
	// untagged Docker-backed tests actually start.
	return []sharedToolSpec{
		{Name: "claude"},
		{Name: "codex"},
	}
}

func sharedCustomImageSpecs() []sharedCustomImageSpec {
	return []sharedCustomImageSpec{
		{
			ToolName: SharedClipboardOffToolName,
			Dockerfile: fmt.Sprintf(
				"FROM %s\nENV COOPER_CLI_TOOL=%s\nENV COOPER_CLIPBOARD_MODE=off\n",
				docker.GetImageBase(),
				SharedClipboardOffToolName,
			),
		},
	}
}
