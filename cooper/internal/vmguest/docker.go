package vmguest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rickchristie/govner/cooper/internal/vmcontext"
	"github.com/rickchristie/govner/cooper/internal/vmproto"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

const (
	controlNetworkName = "cooper-control"
	dockerSocketPath   = "/run/cooper/docker.sock"
	dockerWrapperPath  = "/usr/local/libexec/cooper-vm-docker-wrapper"
)

type dockerRuntime struct {
	manifest vmproto.Manifest
	dockerd  *exec.Cmd
	stopOnce sync.Once
}

func (d *dockerRuntime) start(ctx context.Context, log io.Writer) error {
	if err := os.MkdirAll("/run/cooper", 0o755); err != nil {
		return err
	}
	if err := installProxyCA(ctx, d.manifest.CADigest, log); err != nil {
		return err
	}
	proxyURL := fmt.Sprintf("http://%s:%d", d.manifest.ControlGateway, d.manifest.ProxyPort)
	// Keep the classic image store because Docker 29's fresh-install default
	// uses a manifest digest as the imported image ID. Cooper transfers a
	// Docker archive and verifies its config digest against the host image ID.
	d.dockerd = exec.CommandContext(ctx, "/usr/local/bin/dockerd",
		"--host=unix://"+dockerSocketPath,
		"--data-root=/var/lib/cooper-docker",
		"--exec-root=/run/cooper/docker-exec",
		"--pidfile=/run/cooper/dockerd.pid",
		"--feature=containerd-snapshotter=false",
		"--bip="+d.manifest.DefaultBridgeCIDR,
		"--log-level=error",
	)
	d.dockerd.Env = append(os.Environ(), "HTTP_PROXY="+proxyURL, "HTTPS_PROXY="+proxyURL, "NO_PROXY=localhost,127.0.0.1,"+d.manifest.ControlGateway)
	d.dockerd.Stdout = log
	d.dockerd.Stderr = log
	if err := d.dockerd.Start(); err != nil {
		return fmt.Errorf("start guest Docker: %w", err)
	}
	for attempt := 0; attempt < 600; attempt++ {
		if err := dockerCommand(ctx, nil, io.Discard, io.Discard, "info"); err == nil {
			break
		}
		if d.dockerd.Process.Signal(syscall.Signal(0)) != nil {
			return errors.New("guest Docker stopped before it became ready")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
		if attempt == 599 {
			return errors.New("guest Docker did not become ready in 60 seconds")
		}
	}
	if err := dockerCommand(ctx, nil, io.Discard, log,
		"network", "create", "--internal", "--subnet", d.manifest.ControlSubnet,
		"--gateway", d.manifest.ControlGateway, controlNetworkName); err != nil {
		return fmt.Errorf("create guest control network: %w", err)
	}
	return nil
}

func installProxyCA(ctx context.Context, wantDigest string, log io.Writer) error {
	const source = "/etc/cooper/cooper-ca.pem"
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read mounted Cooper CA: %w", err)
	}
	if err := verifyCADigest(data, wantDigest); err != nil {
		return err
	}
	const destination = "/usr/local/share/ca-certificates/cooper-host.crt"
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("create guest CA directory: %w", err)
	}
	if err := os.WriteFile(destination, data, 0o644); err != nil {
		return fmt.Errorf("install guest Cooper CA: %w", err)
	}
	command := exec.CommandContext(ctx, "update-ca-certificates")
	command.Stdout = log
	command.Stderr = log
	if err := command.Run(); err != nil {
		return fmt.Errorf("update guest CA certificates: %w", err)
	}
	return nil
}

func verifyCADigest(data []byte, want string) error {
	digest := sha256.Sum256(data)
	got := hex.EncodeToString(digest[:])
	if got != want {
		return fmt.Errorf("mounted Cooper CA digest is %s; want %s", got, want)
	}
	return nil
}

func (d *dockerRuntime) stop() {
	d.stopOnce.Do(func() {
		if d.dockerd == nil || d.dockerd.Process == nil {
			return
		}
		_ = d.dockerd.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() {
			_ = d.dockerd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			_ = d.dockerd.Process.Kill()
			<-done
		}
	})
}

func (d *dockerRuntime) loadAndStartAgent(ctx context.Context, log io.Writer) error {
	loadStarted := time.Now()
	output, err := dockerOutput(ctx, "load", "--input", d.manifest.ImageArchive)
	_, _ = log.Write(output)
	if err != nil {
		return fmt.Errorf("load exact agent image: %w: %s", err, lastOutputLine(output))
	}
	fmt.Fprintf(log, "Cooper VM guest: agent image load finished in %s\n", time.Since(loadStarted).Round(time.Millisecond))
	output, err = dockerOutput(ctx, "image", "inspect", "--format", "{{.Id}}", d.manifest.ImageID)
	if err != nil {
		return fmt.Errorf("inspect loaded agent image: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if strings.TrimSpace(string(output)) != d.manifest.ImageID {
		return fmt.Errorf("loaded agent image ID does not match manifest")
	}
	// The host exports the immutable image ID so a moved tag cannot poison the
	// archive cache. Docker therefore loads the image without its host tag. Add
	// the reviewed reference only after the ID check so Docker development in
	// the VM sees the same local image name as CLI mode.
	if err := dockerCommand(ctx, nil, log, log,
		"image", "tag", d.manifest.ImageID, d.manifest.ImageRef); err != nil {
		return fmt.Errorf("tag verified agent image: %w", err)
	}
	if err := d.writeNestedFiles(); err != nil {
		return err
	}
	dockerGID, err := deviceGID(dockerSocketPath)
	if err != nil {
		return fmt.Errorf("inspect guest Docker socket group: %w", err)
	}
	_ = os.Remove(workload.EntrypointReadyPath)
	_ = dockerCommand(ctx, nil, io.Discard, io.Discard, "rm", "-f", d.manifest.AgentContainer)
	args := []string{
		"run", "-d", "--name", d.manifest.AgentContainer,
		"--network", controlNetworkName,
		"--cap-drop=ALL",
		"--security-opt=no-new-privileges",
		"--security-opt", "seccomp=" + d.manifest.SeccompProfile,
		"--init", "--shm-size", d.manifest.SHMSize,
		"--group-add", strconv.Itoa(dockerGID),
		"--label", "cooper.kind=vm-agent",
		"--label", "cooper.runtime-id=" + d.manifest.RuntimeID,
		"--label", "cooper.tool=" + d.manifest.ToolName,
		"--mount", "type=bind,src=" + dockerSocketPath + ",dst=/var/run/docker.sock",
		"--mount", "type=bind,src=" + dockerWrapperPath + ",dst=/usr/local/bin/docker,readonly",
		"--mount", "type=bind,src=/usr/local/bin/docker,dst=/usr/local/libexec/cooper-vm-docker,readonly",
		"--mount", workload.DockerBindMount(d.manifest.CooperDir, d.manifest.CooperDir, false),
		"--mount", workload.DockerBindMount(filepath.Join(d.manifest.HomeDir, ".docker"), filepath.Join(d.manifest.HomeDir, ".docker"), false),
		"--mount", "type=bind,src=/run/cooper/vm-context.json,dst=/run/cooper/vm-context.json,readonly",
	}
	if d.manifest.Depth == 1 {
		kvmGID, err := deviceGID("/dev/kvm")
		if err != nil {
			return fmt.Errorf("inspect guest KVM device group: %w", err)
		}
		args = append(args,
			"--device=/dev/kvm",
			"--group-add", strconv.Itoa(kvmGID),
		)
	}
	for _, mount := range d.manifest.Mounts {
		value := workload.DockerBindMount(mount.Target, mount.Target, mount.ReadOnly)
		args = append(args, "--mount", value)
	}
	for _, environment := range d.manifest.Environment {
		args = append(args, "-e", environment)
	}
	args = append(args,
		"-e", "COOPER_VM_CONTEXT=/run/cooper/vm-context.json",
		"-e", "COOPER_VM_DEPTH="+strconv.Itoa(d.manifest.Depth),
		"-w", d.manifest.WorkspaceDir,
		d.manifest.ImageID, "sleep", "infinity",
	)
	if err := dockerCommand(ctx, nil, log, log, args...); err != nil {
		return fmt.Errorf("start VM agent container: %w", err)
	}
	fmt.Fprintln(log, "Cooper VM guest: agent container started")
	for attempt := 0; attempt < 600; attempt++ {
		if err := dockerCommand(ctx, nil, io.Discard, io.Discard, "exec", d.manifest.AgentContainer, "test", "-f", workload.EntrypointReadyPath); err == nil {
			return nil
		}
		if !d.agentRunning(ctx) {
			return errors.New("VM agent stopped before its entrypoint became ready")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return errors.New("VM agent entrypoint did not become ready in 60 seconds")
}

func lastOutputLine(output []byte) string {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[len(lines)-1]) == "" {
		return "no Docker error output"
	}
	return strings.TrimSpace(lines[len(lines)-1])
}

func (d *dockerRuntime) writeNestedFiles() error {
	for _, dir := range []string{d.manifest.CooperDir, filepath.Join(d.manifest.HomeDir, ".docker")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create guest-local user directory: %w", err)
		}
		if err := os.Chown(dir, d.manifest.UID, d.manifest.GID); err != nil {
			return fmt.Errorf("set guest-local user directory owner: %w", err)
		}
	}
	proxyURL := fmt.Sprintf("http://%s:%d", d.manifest.ControlGateway, d.manifest.ProxyPort)
	dockerConfig := map[string]any{"proxies": map[string]any{"default": map[string]string{
		"httpProxy": proxyURL, "httpsProxy": proxyURL,
		"noProxy": "localhost,127.0.0.1," + d.manifest.ControlGateway,
	}}}
	data, err := json.MarshalIndent(dockerConfig, "", "  ")
	if err != nil {
		return err
	}
	configPath := filepath.Join(d.manifest.HomeDir, ".docker", "config.json")
	if err := os.WriteFile(configPath, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write nested Docker proxy config: %w", err)
	}
	if err := os.Chown(configPath, d.manifest.UID, d.manifest.GID); err != nil {
		return fmt.Errorf("set nested Docker proxy config owner: %w", err)
	}
	if err := d.writeDockerWrapper(proxyURL); err != nil {
		return err
	}
	contextData := vmcontext.Context{
		Schema: vmcontext.Schema, Depth: d.manifest.Depth,
		ParentNetwork: controlNetworkName, ParentProxy: d.manifest.ControlGateway,
		AgentContainer: d.manifest.AgentContainer,
		ProxyPort:      d.manifest.ProxyPort, BridgePort: d.manifest.BridgePort,
		WorkspaceDir: d.manifest.WorkspaceDir, TempDir: "/tmp", CooperDir: d.manifest.CooperDir, HomeDir: d.manifest.HomeDir,
	}
	if err := vmcontext.Write("/run/cooper/vm-context.json", contextData); err != nil {
		return fmt.Errorf("write nested Cooper context: %w", err)
	}
	return nil
}

func (d *dockerRuntime) writeDockerWrapper(proxyURL string) error {
	script := dockerWrapperScript(proxyURL, d.manifest.ControlGateway)
	if err := os.MkdirAll(filepath.Dir(dockerWrapperPath), 0o755); err != nil {
		return fmt.Errorf("create nested Docker wrapper directory: %w", err)
	}
	if err := os.WriteFile(dockerWrapperPath, []byte(script), 0o555); err != nil {
		return fmt.Errorf("write nested Docker wrapper: %w", err)
	}
	return nil
}

func dockerWrapperScript(proxyURL, gateway string) string {
	noProxy := "localhost,127.0.0.1," + gateway
	buildArgs := fmt.Sprintf(`--build-arg HTTP_PROXY=%s --build-arg HTTPS_PROXY=%s --build-arg NO_PROXY=%s --build-arg http_proxy=%s --build-arg https_proxy=%s --build-arg no_proxy=%s`,
		proxyURL, proxyURL, noProxy, proxyURL, proxyURL, noProxy)
	return `#!/bin/sh
set -eu
real=/usr/local/libexec/cooper-vm-docker
if [ "${1-}" = build ]; then
    shift
    exec "$real" build ` + buildArgs + ` "$@"
fi
if [ "${1-}" = image ] && [ "${2-}" = build ]; then
    shift 2
    exec "$real" image build ` + buildArgs + ` "$@"
fi
exec "$real" "$@"
`
}

func (d *dockerRuntime) stopAgent(ctx context.Context) {
	_ = dockerCommand(ctx, nil, io.Discard, io.Discard, "rm", "-f", d.manifest.AgentContainer)
}

func (d *dockerRuntime) agentHealthy(ctx context.Context) bool {
	if !d.agentRunning(ctx) {
		return false
	}
	return dockerCommand(ctx, nil, io.Discard, io.Discard, "exec", d.manifest.AgentContainer, "test", "-f", workload.EntrypointReadyPath) == nil
}

func (d *dockerRuntime) reloadAgentEntrypoint(ctx context.Context) error {
	if err := dockerCommand(ctx, nil, io.Discard, io.Discard, "exec", d.manifest.AgentContainer, "rm", "-f", workload.EntrypointReadyPath); err != nil {
		return fmt.Errorf("clear VM agent readiness before reload: %w", err)
	}
	if err := dockerCommand(ctx, nil, io.Discard, io.Discard, "exec", d.manifest.AgentContainer, "kill", "-HUP", "1"); err != nil {
		return fmt.Errorf("signal VM agent port reload: %w", err)
	}
	for attempt := 0; attempt < 100; attempt++ {
		if d.agentHealthy(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return errors.New("VM agent did not finish its port reload in 10 seconds")
}

func (d *dockerRuntime) agentRunning(ctx context.Context) bool {
	output, err := dockerOutput(ctx, "inspect", "--format", "{{.State.Running}}", d.manifest.AgentContainer)
	return err == nil && strings.TrimSpace(string(output)) == "true"
}

func deviceGID(path string) (int, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(stat.Gid), nil
	}
	return 0, fmt.Errorf("path %s has no Unix group information", path)
}

func dockerCommand(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	command := exec.CommandContext(ctx, "/usr/local/bin/docker", append([]string{"--host=unix://" + dockerSocketPath}, args...)...)
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
}

func dockerOutput(ctx context.Context, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "/usr/local/bin/docker", append([]string{"--host=unix://" + dockerSocketPath}, args...)...)
	return command.CombinedOutput()
}
