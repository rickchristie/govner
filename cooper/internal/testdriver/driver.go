package testdriver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/clipboard"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/templates"
	"github.com/rickchristie/govner/cooper/internal/testdocker"
)

// DefaultImagePrefix keeps driver-created resources isolated from a user's
// normal Cooper containers and images.
const DefaultImagePrefix = testdocker.ImagePrefix

// Options control how the runtime driver prepares and starts Cooper.
type Options struct {
	ImagePrefix          string
	DisableHostClipboard bool
	KeepArtifactsOnClose bool
	ConfigMutator        func(*config.Config)
}

// Barrel describes a started test barrel managed through the driver.
type Barrel struct {
	Name           string
	ToolName       string
	WorkspaceDir   string
	ClipboardToken string
}

// Driver owns a temporary Cooper runtime and exposes helpers for tests and
// manual verification scenarios.
type Driver struct {
	cfg           *config.Config
	cooperDir     string
	app           *app.CooperApp
	imagePrefix   string
	keepArtifacts bool
	lock          *testdocker.Lock

	started     bool
	builtImages []string
	workspaces  []string
}

// New creates a runtime driver with a temporary Cooper directory rendered via
// the same template pipeline used by `cooper build` and `cooper up`.
func New(opts Options) (*Driver, error) {
	prefix := strings.TrimSpace(opts.ImagePrefix)
	if prefix == "" {
		prefix = DefaultImagePrefix
	}
	docker.SetImagePrefix(prefix)

	lock, err := testdocker.AcquireLock()
	if err != nil {
		return nil, err
	}

	cooperDir, cfg, err := setupCooperDir(opts.ConfigMutator)
	if err != nil {
		lock.Release()
		return nil, err
	}

	appInstance := app.NewCooperApp(cfg, cooperDir)
	if opts.DisableHostClipboard {
		appInstance.DisableClipboardReader()
	}

	return &Driver{
		cfg:           cfg,
		cooperDir:     cooperDir,
		app:           appInstance,
		imagePrefix:   prefix,
		keepArtifacts: opts.KeepArtifactsOnClose,
		lock:          lock,
	}, nil
}

func (d *Driver) logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[cooper testdriver][%s] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
}

// Config returns the live config snapshot used by the driver.
func (d *Driver) Config() *config.Config {
	return d.cfg
}

// CooperDir returns the temporary Cooper directory backing this runtime.
func (d *Driver) CooperDir() string {
	return d.cooperDir
}

// App exposes the real CooperApp for advanced scenarios that need the
// application boundary directly.
func (d *Driver) App() *app.CooperApp {
	return d.app
}

// Start starts the real Cooper runtime.
func (d *Driver) Start(ctx context.Context) error {
	if d.started {
		return nil
	}
	if err := d.app.Start(ctx, nil); err != nil {
		return err
	}
	d.started = true
	return nil
}

// Close shuts down Cooper, removes test containers/networks, removes any
// custom images built by the driver, and deletes the temporary Cooper dir.
func (d *Driver) Close() error {
	var errs []string

	if d.started {
		if err := d.app.Stop(); err != nil {
			errs = append(errs, fmt.Sprintf("stop app: %v", err))
		}
		d.started = false
	}

	for _, imageName := range d.builtImages {
		if err := docker.RemoveImage(imageName); err != nil {
			errs = append(errs, fmt.Sprintf("remove image %s: %v", imageName, err))
		}
	}
	d.builtImages = nil

	cleanupDocker()
	fixCooperDirPermissions(d.cooperDir)

	if !d.keepArtifacts {
		for _, workspaceDir := range d.workspaces {
			if err := os.RemoveAll(workspaceDir); err != nil {
				errs = append(errs, fmt.Sprintf("remove workspace %s: %v", workspaceDir, err))
			}
		}
	}
	d.workspaces = nil

	if !d.keepArtifacts {
		if err := os.RemoveAll(d.cooperDir); err != nil {
			errs = append(errs, fmt.Sprintf("remove cooper dir: %v", err))
		}
	}

	if d.lock != nil {
		if err := d.lock.Release(); err != nil {
			errs = append(errs, fmt.Sprintf("release driver lock: %v", err))
		}
	}
	d.lock = nil

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// PersistedConfig reloads config.json from disk so callers can verify that a
// runtime mutation was persisted, not just applied in memory.
func (d *Driver) PersistedConfig() (*config.Config, error) {
	return config.LoadConfig(filepath.Join(d.cooperDir, "config.json"))
}

// RequireProxyImage ensures the proxy image for the driver's prefix exists.
func (d *Driver) RequireProxyImage() error {
	exists, err := docker.ImageExists(docker.GetImageProxy())
	if err != nil {
		return fmt.Errorf("check proxy image %s: %w", docker.GetImageProxy(), err)
	}
	if !exists {
		return fmt.Errorf("proxy image %s not found; build shared Cooper test images first", docker.GetImageProxy())
	}
	return nil
}

// RequireBaseImage ensures the base CLI image exists.
func (d *Driver) RequireBaseImage() error {
	exists, err := docker.ImageExists(docker.GetImageBase())
	if err != nil {
		return fmt.Errorf("check base image %s: %w", docker.GetImageBase(), err)
	}
	if !exists {
		return fmt.Errorf("base image %s not found; build shared Cooper test images first", docker.GetImageBase())
	}
	return nil
}

// RegisterBarrelSession registers an in-memory barrel session and returns the
// generated bearer token. This is useful for exercising the clipboard bridge
// without starting a real container.
func (d *Driver) RegisterBarrelSession(session clipboard.BarrelSession) (string, error) {
	if err := d.app.ClipboardManager().RegisterBarrel(session); err != nil {
		return "", fmt.Errorf("register barrel session: %w", err)
	}
	for _, candidate := range d.app.ClipboardManager().ActiveSessions() {
		if candidate.ContainerName == session.ContainerName {
			return candidate.Token, nil
		}
	}
	return "", fmt.Errorf("registered barrel session %s not found", session.ContainerName)
}

// StageClipboard stages a clipboard object through the real clipboard manager.
func (d *Driver) StageClipboard(obj clipboard.ClipboardObject, ttl time.Duration) (*clipboard.StagedSnapshot, error) {
	return d.app.ClipboardManager().Stage(obj, ttl)
}

// ClipboardGet performs an HTTP GET against a live clipboard bridge endpoint.
func (d *Driver) ClipboardGet(path, token string) (*http.Response, []byte, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d%s", d.cfg.BridgePort, path)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("new request %s: %w", url, err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("get %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read response body %s: %w", url, err)
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp, body, nil
}

// StartBarrel creates a temporary workspace, writes a clipboard token file,
// starts a real barrel container, and waits for it to report running.
func (d *Driver) StartBarrel(toolName string) (*Barrel, error) {
	workspaceDir, err := os.MkdirTemp("", "cooper-driver-ws-*")
	if err != nil {
		return nil, fmt.Errorf("create temp workspace: %w", err)
	}
	d.workspaces = append(d.workspaces, workspaceDir)
	return d.StartBarrelInWorkspace(toolName, workspaceDir)
}

// StartBarrelInWorkspace starts a barrel in the provided workspace.
func (d *Driver) StartBarrelInWorkspace(toolName, workspaceDir string) (*Barrel, error) {
	name := docker.BarrelContainerName(workspaceDir, toolName)
	token, err := d.WriteClipboardToken(name)
	if err != nil {
		return nil, err
	}
	if err := docker.StartBarrel(d.cfg, workspaceDir, d.cooperDir, toolName); err != nil {
		return nil, fmt.Errorf("start barrel %s: %w", name, err)
	}
	if err := d.WaitForContainer(name, 15*time.Second); err != nil {
		return nil, err
	}
	return &Barrel{
		Name:           name,
		ToolName:       toolName,
		WorkspaceDir:   workspaceDir,
		ClipboardToken: token,
	}, nil
}

// StopBarrel stops a barrel through the app boundary so token revocation is
// exercised as part of the runtime behavior.
func (d *Driver) StopBarrel(name string) error {
	return d.app.StopContainer(name)
}

// RestartBarrel restarts a barrel through the app boundary so token rotation
// is exercised as part of the runtime behavior.
func (d *Driver) RestartBarrel(name string) error {
	return d.app.RestartContainer(name)
}

// WriteClipboardToken writes a clipboard token file for a barrel.
func (d *Driver) WriteClipboardToken(containerName string) (string, error) {
	token, err := clipboard.GenerateToken()
	if err != nil {
		return "", fmt.Errorf("generate clipboard token: %w", err)
	}
	if _, err := clipboard.WriteTokenFile(d.cooperDir, containerName, token); err != nil {
		return "", fmt.Errorf("write clipboard token for %s: %w", containerName, err)
	}
	return token, nil
}

// ReadClipboardToken reloads a barrel's token file from disk.
func (d *Driver) ReadClipboardToken(containerName string) (string, error) {
	data, err := os.ReadFile(clipboard.TokenFilePath(d.cooperDir, containerName))
	if err != nil {
		return "", fmt.Errorf("read clipboard token for %s: %w", containerName, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// WaitForContainer polls docker inspect until the container reports running.
func (d *Driver) WaitForContainer(containerName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	attempt := 0
	for time.Now().Before(deadline) {
		attempt++
		out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", containerName).CombinedOutput()
		d.logf("waitForContainer attempt=%d container=%s running=%q err=%v", attempt, containerName, strings.TrimSpace(string(out)), err)
		if err == nil && strings.TrimSpace(string(out)) == "true" {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("container %s did not become ready", containerName)
}

// ExecBarrel runs a shell command inside a barrel and returns stdout/stderr.
func (d *Driver) ExecBarrel(containerName, shellCmd string) (string, error) {
	out, err := exec.Command("docker", "exec", containerName, "sh", "-lc", shellCmd).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("exec in %s failed: %w\n%s", containerName, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// BuildCustomToolImage writes a Dockerfile under the driver's temporary
// cli/<tool>/ directory and builds a custom image for that tool name.
func (d *Driver) BuildCustomToolImage(toolName, dockerfile string) error {
	customDir := filepath.Join(d.cooperDir, "cli", toolName)
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		return fmt.Errorf("mkdir custom dir %s: %w", customDir, err)
	}
	if err := os.WriteFile(filepath.Join(customDir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		return fmt.Errorf("write custom Dockerfile for %s: %w", toolName, err)
	}

	imageName := docker.GetImageCLI(toolName)
	if err := docker.BuildImage(imageName, filepath.Join(customDir, "Dockerfile"), customDir, nil, false); err != nil {
		return fmt.Errorf("build custom image %s: %w", imageName, err)
	}
	if !containsString(d.builtImages, imageName) {
		d.builtImages = append(d.builtImages, imageName)
	}
	return nil
}

func setupCooperDir(configMutator func(*config.Config)) (string, *config.Config, error) {
	cooperDir, err := os.MkdirTemp("", "cooper-driver-*")
	if err != nil {
		return "", nil, fmt.Errorf("mkdir temp cooper dir: %w", err)
	}

	cfg := config.DefaultConfig()
	if err := testdocker.AssignDynamicPorts(cfg); err != nil {
		return "", nil, fmt.Errorf("assign dynamic test ports: %w", err)
	}
	if configMutator != nil {
		configMutator(cfg)
	}

	cfgPath := filepath.Join(cooperDir, "config.json")
	if err := config.SaveConfig(cfgPath, cfg); err != nil {
		return "", nil, fmt.Errorf("save config: %w", err)
	}

	if _, _, err := config.EnsureCA(cooperDir); err != nil {
		return "", nil, fmt.Errorf("ensure CA: %w", err)
	}

	baseDir := filepath.Join(cooperDir, "base")
	cliDir := filepath.Join(cooperDir, "cli")
	proxyDir := filepath.Join(cooperDir, "proxy")
	for _, dir := range []string{baseDir, cliDir, proxyDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", nil, fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}

	if err := templates.WriteAllTemplates(baseDir, cliDir, cfg); err != nil {
		return "", nil, fmt.Errorf("write cli templates: %w", err)
	}
	if err := templates.WriteProxyTemplates(proxyDir, cfg); err != nil {
		return "", nil, fmt.Errorf("write proxy templates: %w", err)
	}
	if err := docker.WritePortForwardConfig(cooperDir, cfg.BridgePort, cfg.PortForwardRules); err != nil {
		return "", nil, fmt.Errorf("write port forward config: %w", err)
	}

	for _, dir := range []string{
		filepath.Join(cooperDir, "run"),
		filepath.Join(cooperDir, "logs"),
	} {
		if err := os.MkdirAll(dir, 0o777); err != nil {
			return "", nil, fmt.Errorf("mkdir %s: %w", dir, err)
		}
		_ = os.Chmod(dir, 0o777)
	}

	if err := copyFile(filepath.Join(cooperDir, "ca", "cooper-ca.pem"), filepath.Join(proxyDir, "cooper-ca.pem")); err != nil {
		return "", nil, fmt.Errorf("copy CA cert: %w", err)
	}
	if err := copyFile(filepath.Join(cooperDir, "ca", "cooper-ca-key.pem"), filepath.Join(proxyDir, "cooper-ca-key.pem")); err != nil {
		return "", nil, fmt.Errorf("copy CA key: %w", err)
	}

	return cooperDir, cfg, nil
}

func cleanupDocker() {
	_ = docker.CleanupRuntime()
}

func fixCooperDirPermissions(cooperDir string) {
	_ = testdocker.FixOwnership(cooperDir)
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// DecodeJSON unmarshals a bridge response body into a map for quick scenario
// assertions.
func DecodeJSON(body []byte) (map[string]any, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}
