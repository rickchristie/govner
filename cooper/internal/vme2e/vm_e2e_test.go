package vme2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/clipboard"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/runtimefs"
	"github.com/rickchristie/govner/cooper/internal/testdocker"
	"github.com/rickchristie/govner/cooper/internal/testdriver"
	"github.com/rickchristie/govner/cooper/internal/usercontext"
	"github.com/rickchristie/govner/cooper/internal/vm"
)

const (
	vmE2ENamespace   = "cooper-vm-e2e"
	vmTestTool       = "vm-test-agent"
	selfNamespace    = "cooper-self-e2e"
	selfImagePrefix  = "cooper-self-e2e-"
	outerImagePrefix = "cooper-self-outer-e2e-"
)

func TestNestedCooperHarness(t *testing.T) {
	if os.Getenv("COOPER_NESTED_HARNESS") != "1" {
		t.Skip("nested Cooper harness runs only inside the outer VM")
	}
	cooperDir := requiredDirectory(t, "COOPER_SELF_HOST_CONFIG")
	cooperBinary := requiredFile(t, "COOPER_SELF_HOST_BINARY")
	// The outer test stages the rebuilt binary and its config together in the
	// shared workspace. The depth-2 guest has an intentionally private Cooper
	// directory, so its depth-3 rejection check must use this staged config.
	depthThreeConfigDir := filepath.Dir(cooperBinary)
	if _, err := os.Stat(filepath.Join(depthThreeConfigDir, "config.json")); err != nil {
		t.Fatalf("inspect staged depth-3 config: %v", err)
	}
	workspace := repositoryRoot(t)
	targetHost := strings.TrimSpace(os.Getenv("COOPER_SELF_HOST_TARGET"))
	if targetHost == "" {
		t.Fatal("COOPER_SELF_HOST_TARGET is required")
	}
	cfg, err := config.LoadConfig(filepath.Join(cooperDir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	docker.SetImagePrefix(selfImagePrefix)
	docker.SetRuntimeNamespace(selfNamespace)

	application := app.NewCooperApp(cfg, cooperDir)
	application.DisableClipboardReader()
	if err := application.Start(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	stopped := false
	defer func() {
		if !stopped {
			_ = application.Stop()
		}
	}()

	innerScript := fmt.Sprintf(`
set -eux
test -f cooper/go.mod
test "$(cat "$HOME/.codex/cooper-self-host-sentinel")" = outer-state
printf inner-state > "$HOME/.codex/cooper-inner-write"
test ! -e /dev/kvm
! grep -Eq '(^|[[:space:]])(svm|vmx)([[:space:]]|$)' /proc/cpuinfo
docker info >/dev/null
if env -u HTTP_PROXY -u HTTPS_PROXY -u http_proxy -u https_proxy curl -fsS --connect-timeout 2 --max-time 4 http://1.1.1.1/ >/dev/null 2>&1; then exit 41; fi
if curl -kfsS --connect-timeout 2 --max-time 4 --noproxy '*' https://%s/ >/dev/null 2>&1; then exit 42; fi
test "$(curl -kfsS --max-time 10 https://%s/)" = ok
if curl -fsS --max-time 4 https://vm-blocked.cooper.test/ >/dev/null 2>&1; then exit 43; fi
if %s --config %s --prefix %s --runtime-namespace %s vm codex -c true >/tmp/depth-three.out 2>&1; then exit 44; fi
if ! grep -Fq 'depth 3 exceeds the configured maximum 2' /tmp/depth-three.out; then
    cat /tmp/depth-three.out >&2
    exit 45
fi
printf INNER_SELF_HOST_OK
`, targetHost, targetHost, shellQuote(cooperBinary), shellQuote(depthThreeConfigDir), shellQuote(selfImagePrefix), shellQuote(selfNamespace))
	command := exec.Command(cooperBinary,
		"--config", cooperDir,
		"--prefix", selfImagePrefix,
		"--runtime-namespace", selfNamespace,
		"vm", "codex", "--cpus", "4", "--memory", "4096m", "--disk", "24g", "-c", innerScript,
	)
	// A Go package test runs from its package source directory. Start the inner
	// Cooper process at the repository root so this acceptance test gives the
	// inner agent the complete project, as a real self-hosting session does.
	command.Dir = workspace
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("start inner Cooper VM: %v\n%s\n%s", err, output, nestedVMLogs(cooperDir))
	}
	if !bytes.Contains(output, []byte("INNER_SELF_HOST_OK")) {
		t.Fatalf("inner Cooper output has no success marker:\n%s", output)
	}
	assertVirtioFSDLogsClean(t, filepath.Join(cooperDir, "vm", "run"))

	doctor := exec.Command(cooperBinary,
		"--config", cooperDir,
		"--prefix", selfImagePrefix,
		"--runtime-namespace", selfNamespace,
		"vm", "doctor",
	)
	doctorOutput, err := doctor.CombinedOutput()
	if err != nil {
		t.Fatalf("inner VM doctor: %v\n%s", err, doctorOutput)
	}
	if !bytes.Contains(doctorOutput, []byte("depth 2")) || !bytes.Contains(doctorOutput, []byte("external none, default routes none")) || !bytes.Contains(doctorOutput, []byte("KVM false")) {
		t.Fatalf("inner VM doctor did not prove the depth-two boundary:\n%s", doctorOutput)
	}
	accessLog := readFile(t, filepath.Join(cooperDir, "logs", "access.log"))
	if !strings.Contains(accessLog, targetHost) {
		t.Fatalf("inner Squid log has no request for %s", targetHost)
	}

	if err := application.Stop(); err != nil {
		t.Fatal(err)
	}
	stopped = true
	down := exec.Command(cooperBinary,
		"--config", cooperDir,
		"--prefix", selfImagePrefix,
		"--runtime-namespace", selfNamespace,
		"down",
	)
	downOutput, err := down.CombinedOutput()
	if err != nil {
		t.Fatalf("clean nested Cooper runtime: %v\n%s", err, downOutput)
	}
	assertNestedResourcesRemoved(t)
	fmt.Println("NESTED_COOPER_HARNESS_OK")
}

func nestedVMLogs(cooperDir string) string {
	matches, err := filepath.Glob(filepath.Join(cooperDir, "vm", "run", "*", "supervisor", "*.log"))
	if err != nil || len(matches) == 0 {
		return "No nested VM supervisor logs were found."
	}
	var report strings.Builder
	report.WriteString("Nested VM supervisor logs:\n")
	for _, path := range matches {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			fmt.Fprintf(&report, "\n--- %s ---\nread failed: %v\n", filepath.Base(path), readErr)
			continue
		}
		name := filepath.Base(path)
		if strings.HasPrefix(name, "virtiofsd-") && !bytes.Contains(data, []byte("ERROR")) {
			continue
		}
		tailLimit := 8 * 1024
		if name == "guest.log" {
			tailLimit = 32 * 1024
		}
		if len(data) > tailLimit {
			data = data[len(data)-tailLimit:]
		}
		fmt.Fprintf(&report, "\n--- %s ---\n%s\n", name, data)
	}
	return report.String()
}

// TestVMNestedDockerMountRefresh covers the filesystem path used when Cooper
// develops Cooper. It proves that a guest write and a nested Docker bind mount
// see the same bytes without changing a named zoneinfo file.
func TestVMNestedDockerMountRefresh(t *testing.T) {
	if os.Getenv("COOPER_RUN_VM_E2E") != "1" {
		t.Skip("set COOPER_RUN_VM_E2E=1 to run the Linux KVM gate")
	}
	preparedBase := requiredFile(t, "COOPER_VM_PREPARED_BASE")
	cooperBinary := requiredFile(t, "COOPER_VM_BINARY")

	lock, err := testdocker.SetupPackageNamed("vm-refresh-probe", true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := lock.Release(); err != nil {
			t.Errorf("release Docker test lock: %v", err)
		}
	}()
	driver, err := testdriver.New(testdriver.Options{
		ImagePrefix:          testdocker.ImagePrefix,
		DisableHostClipboard: true,
		ConfigMutator: func(cfg *config.Config) {
			cfg.VM = config.VMConfig{CPUs: 2, MemoryMiB: 3072, DiskGiB: 16, MaxDepth: 2, StartTimeoutS: 300, StopTimeoutS: 15}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	homeDir := driver.HomeDir()
	t.Setenv("HOME", homeDir)
	defer func() {
		docker.SetRuntimeNamespace(vmE2ENamespace)
		if err := driver.Close(); err != nil {
			t.Errorf("close refresh probe driver: %v", err)
		}
	}()
	docker.SetRuntimeNamespace(vmE2ENamespace)
	if err := (vm.InfrastructureImages{
		CooperDir: driver.CooperDir(), Prefix: testdocker.ImagePrefix,
		Executable: cooperBinary, Out: os.Stderr,
	}).Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := driver.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	manager, _, runtimeState, _ := startVM(t, driver, homeDir, preparedBase, cooperBinary, workspace, "codex", "x11")
	defer func() { _ = manager.Stop(context.Background(), runtimeState) }()

	imageRef := docker.GetImageCLI("codex")
	script := fmt.Sprintf(`
set -eu
root=/tmp/cooper-refresh-probe
rm -rf "$root"
mkdir -p "$root"
cp /usr/share/zoneinfo/Asia/Tokyo "$root/first"
docker run -d --name cooper-refresh-probe --mount type=bind,src="$root",dst=/probe,readonly --entrypoint sleep %s infinity >/dev/null
trap 'docker rm -f cooper-refresh-probe >/dev/null 2>&1 || true' EXIT
first_source=$(sha256sum "$root/first" | cut -d ' ' -f 1)
first_container=$(docker exec cooper-refresh-probe sha256sum /probe/first | cut -d ' ' -f 1)
test "$first_source" = "$first_container"
test "$(docker exec -e TZ=:/probe/first cooper-refresh-probe date +%%z)" = +0900
rm "$root/first"
cp /usr/share/zoneinfo/Etc/UTC "$root/second"
second_source=$(sha256sum "$root/second" | cut -d ' ' -f 1)
second_container=$(docker exec cooper-refresh-probe sha256sum /probe/second | cut -d ' ' -f 1)
test "$second_source" = "$second_container"
test "$(docker exec -e TZ=:/probe/second cooper-refresh-probe date +%%z)" = +0000
printf VM_NESTED_BIND_REFRESH_OK
`, imageRef)
	output := execVM(t, manager, runtimeState, script)
	if !strings.Contains(output, "VM_NESTED_BIND_REFRESH_OK") {
		t.Fatalf("nested Docker did not observe both timezone updates:\n%s", output)
	}
	assertVirtioFSDLogsClean(t, filepath.Join(runtimeState.RuntimeDir, "supervisor"))
}

func TestSecureVMFeatureMatrix(t *testing.T) {
	if os.Getenv("COOPER_RUN_VM_E2E") != "1" {
		t.Skip("set COOPER_RUN_VM_E2E=1 to run the Linux KVM gate")
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Fatalf("VM E2E requires Linux x86-64, got %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	preparedBase := requiredFile(t, "COOPER_VM_PREPARED_BASE")
	cooperBinary := requiredFile(t, "COOPER_VM_BINARY")

	firstHostPort, stopFirstServer := startHTTPServer(t, "first-port-ok")
	defer stopFirstServer()
	secondHostPort, stopSecondServer := startHTTPServer(t, "second-port-ok")
	defer stopSecondServer()
	bridgeScript := filepath.Join(t.TempDir(), "vm-bridge.sh")
	writeExecutable(t, bridgeScript, "#!/bin/sh\nprintf 'vm-bridge-ok\\n'\n")

	lock, err := testdocker.SetupPackageNamed("vm-e2e", true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := lock.Release(); err != nil {
			t.Errorf("release Docker test lock: %v", err)
		}
	}()

	driver, err := testdriver.New(testdriver.Options{
		ImagePrefix:          testdocker.ImagePrefix,
		DisableHostClipboard: true,
		ConfigMutator: func(cfg *config.Config) {
			cfg.MonitorTimeoutSecs = 1
			cfg.VM = config.VMConfig{CPUs: 4, MemoryMiB: 4096, DiskGiB: 16, MaxDepth: 2, StartTimeoutS: 300, StopTimeoutS: 15}
			cfg.WhitelistedDomains = append(cfg.WhitelistedDomains,
				config.DomainEntry{Domain: "vm-allowed.cooper.test", Source: "user"})
			cfg.PortForwardRules = []config.PortForwardRule{{Description: "VM first test port", ContainerPort: 48080, HostPort: firstHostPort}}
			cfg.BridgeRoutes = []config.BridgeRoute{{APIPath: "/vm-e2e", ScriptPath: bridgeScript}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	homeDir := driver.HomeDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("GROK_HOME", filepath.Join(homeDir, "grok-state"))
	writeFile(t, filepath.Join(homeDir, ".gitconfig"), "[user]\n\tname = Cooper VM Test\n")
	defer func() {
		docker.SetRuntimeNamespace(vmE2ENamespace)
		if err := driver.Close(); err != nil {
			t.Errorf("close VM test driver: %v", err)
		}
	}()
	docker.SetRuntimeNamespace(vmE2ENamespace)

	if err := driver.BuildCustomToolImage(vmTestTool, "FROM "+docker.GetImageBase()+"\nENV COOPER_CLI_TOOL="+vmTestTool+"\nENV COOPER_CLIPBOARD_MODE=shim\n"); err != nil {
		t.Fatal(err)
	}
	if err := (vm.InfrastructureImages{
		CooperDir: driver.CooperDir(), Prefix: testdocker.ImagePrefix,
		Executable: cooperBinary, Out: os.Stderr,
	}).Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := driver.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	// This local target has a self-signed certificate. Successful fixture
	// requests use -k; public Grok E2E checks verify the Cooper CA separately.
	target, err := testdocker.StartHTTPSTarget("vm-allowed.cooper.test", "vm-blocked.cooper.test")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Remove()

	workspace := t.TempDir()
	writeFile(t, filepath.Join(workspace, "workspace-sentinel"), "workspace-ok\n")
	if err := os.MkdirAll(filepath.Join(workspace, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(workspace, ".git", "hooks", "kept"), "hook-ok\n")

	manager, request, runtimeState, oldToken := startVM(t, driver, homeDir, preparedBase, cooperBinary, workspace, vmTestTool, "shim")
	defer func() { _ = manager.Stop(context.Background(), runtimeState) }()

	diagnostic, err := manager.GuestDiagnostic(context.Background(), runtimeState)
	if err != nil {
		t.Fatal(err)
	}
	if diagnostic.Depth != 1 || !contains(diagnostic.Interfaces, "lo") || len(diagnostic.ExternalInterfaces) != 0 || len(diagnostic.DefaultRoutes) != 0 || !diagnostic.KVMAvailable {
		t.Fatalf("guest diagnostic = %#v", diagnostic)
	}
	assertHostBoundary(t, runtimeState, workspace, homeDir)

	imageRef := docker.GetImageCLI(vmTestTool)
	baseChecks := fmt.Sprintf(`
set -eux
test "$(cat workspace-sentinel)" = workspace-ok
printf 'workspace-write-ok\n' > vm-write-result
docker run --rm --user 0 -v "$PWD:/mnt" --entrypoint sh %s -c 'printf root-write-ok > /mnt/vm-root-write-result'
test "$(cat .git/hooks/kept)" = hook-ok
if touch .git/hooks/direct-write 2>/dev/null; then exit 31; fi
if docker run --rm -v "$PWD:/mnt" --entrypoint sh %s -c 'touch /mnt/.git/hooks/docker-bypass' 2>/dev/null; then exit 32; fi
test -S /var/run/docker.sock
docker info >/dev/null
if docker ps --format '{{.Names}}' | grep -Fq '%s-proxy'; then exit 33; fi
test -c /dev/kvm
test "$(cat /sys/class/net/eth0/operstate)" = up
mkdir -p /tmp/vm-sibling-source
printf sibling-ok > /tmp/vm-sibling-source/value
test "$(docker run --rm -v /tmp/vm-sibling-source:/mnt --entrypoint sh %s -c 'cat /mnt/value')" = sibling-ok
test "$(curl -kfsS --max-time 10 https://vm-allowed.cooper.test/)" = ok
if curl -kfsS --connect-timeout 2 --max-time 4 --noproxy '*' https://%s/ >/dev/null 2>&1; then exit 34; fi
if env -u HTTP_PROXY -u HTTPS_PROXY -u http_proxy -u https_proxy curl -fsS --connect-timeout 2 --max-time 4 http://1.1.1.1/ >/dev/null 2>&1; then exit 35; fi
if env -u HTTP_PROXY -u HTTPS_PROXY -u http_proxy -u https_proxy HTTPS_PROXY=http://1.1.1.1:8888 curl -kfsS --connect-timeout 2 --max-time 4 https://vm-allowed.cooper.test/ >/dev/null 2>&1; then exit 36; fi
if curl -fsS --max-time 5 https://vm-blocked.cooper.test/ >/dev/null 2>&1; then exit 37; fi
test "$(curl -fsS --max-time 5 http://127.0.0.1:48080/)" = first-port-ok
test "$(curl -fsS -X POST --max-time 5 http://127.0.0.1:%d/vm-e2e | grep -o vm-bridge-ok)" = vm-bridge-ok
printf VM_BASE_MATRIX_OK
`, imageRef, imageRef, vmE2ENamespace, imageRef, target.IP, driver.Config().BridgePort)
	if output := execVM(t, manager, runtimeState, baseChecks); !strings.Contains(output, "VM_BASE_MATRIX_OK") {
		t.Fatalf("base VM checks output = %q", output)
	}
	if got := strings.TrimSpace(readFile(t, filepath.Join(workspace, "vm-write-result"))); got != "workspace-write-ok" {
		t.Fatalf("host workspace result = %q", got)
	}
	assertHostFileOwner(t, filepath.Join(workspace, "vm-root-write-result"), os.Getuid(), os.Getgid())
	if _, err := os.Stat(filepath.Join(workspace, ".git", "hooks", "docker-bypass")); !os.IsNotExist(err) {
		t.Fatalf("guest Docker changed host Git hooks: %v", err)
	}

	stageClipboard(t, driver)
	pngDigest := sha256.Sum256(minimalPNG(t))
	response, body, err := driver.ClipboardGet("/clipboard/image", oldToken)
	if err != nil || response.StatusCode != 200 || !bytes.Equal(body, minimalPNG(t)) {
		t.Fatalf("host clipboard check: status=%v bytes=%d error=%v", responseStatus(response), len(body), err)
	}
	directClipboard := execVM(t, manager, runtimeState, `
set -eu
token=$(jq -er '.token' "$COOPER_CLIPBOARD_TOKEN_FILE")
curl -fsS --max-time 5 -H "Authorization: Bearer ${token}" "$COOPER_CLIPBOARD_BRIDGE_URL/clipboard/image" | sha256sum
`)
	if !strings.Contains(directClipboard, hex.EncodeToString(pngDigest[:])) {
		t.Fatalf("direct guest clipboard output = %q", directClipboard)
	}
	hostShim, err := os.ReadFile(filepath.Join(driver.CooperDir(), "base", "shims", "xclip"))
	if err != nil {
		t.Fatal(err)
	}
	shimDigest := sha256.Sum256(hostShim)
	shimPreparation := execVM(t, manager, runtimeState, `
set -eu
test "$COOPER_CLIPBOARD_ENABLED" = 1
test "$COOPER_CLIPBOARD_MODE" = shim
test -r /etc/cooper/shims/xclip
token=$(jq -er '.token' "$COOPER_CLIPBOARD_TOKEN_FILE")
temporary=$(mktemp)
trap 'rm -f "$temporary"' EXIT
curl -fsS --max-time 5 -o "$temporary" -H "Authorization: Bearer ${token}" "$COOPER_CLIPBOARD_BRIDGE_URL/clipboard/image"
test -s "$temporary"
sha256sum /opt/cooper/bin/xclip "$temporary"
`)
	if !strings.Contains(shimPreparation, hex.EncodeToString(shimDigest[:])) || !strings.Contains(shimPreparation, hex.EncodeToString(pngDigest[:])) {
		t.Fatalf("guest shim preparation output = %q", shimPreparation)
	}
	clipboardOutput := execVM(t, manager, runtimeState, "set -o pipefail; test \"$(command -v xclip)\" = /opt/cooper/bin/xclip; xclip -selection clipboard -t image/png -o | sha256sum")
	if !strings.Contains(clipboardOutput, hex.EncodeToString(pngDigest[:])) {
		t.Fatalf("clipboard output = %q", clipboardOutput)
	}

	buildScript := fmt.Sprintf(`
set -eu
context=/tmp/vm-build-context
rm -rf "$context"
mkdir -p "$context"
cat > "$context/Dockerfile" <<'EOF'
FROM %s
RUN test "$(curl -kfsS --max-time 10 https://vm-allowed.cooper.test/)" = ok
EOF
docker build -t vm-e2e-build-result "$context" >/tmp/vm-build.log
docker image inspect vm-e2e-build-result >/dev/null
printf VM_DOCKER_BUILD_OK
`, imageRef)
	if output := execVM(t, manager, runtimeState, buildScript); !strings.Contains(output, "VM_DOCKER_BUILD_OK") {
		t.Fatalf("guest Docker build output = %q", output)
	}

	secondRules := []config.PortForwardRule{
		{Description: "VM first test port", ContainerPort: 48080, HostPort: firstHostPort},
		{Description: "VM second test port", ContainerPort: 48081, HostPort: secondHostPort},
	}
	if err := driver.App().UpdatePortForwards(secondRules); err != nil {
		t.Fatal(err)
	}
	if output := execVM(t, manager, runtimeState, "test \"$(curl -fsS --max-time 5 http://127.0.0.1:48081/)\" = second-port-ok && printf VM_LIVE_PORT_OK"); !strings.Contains(output, "VM_LIVE_PORT_OK") {
		t.Fatalf("live port output = %q", output)
	}
	if err := driver.App().UpdatePortForwards(secondRules[1:]); err != nil {
		t.Fatal(err)
	}
	execVM(t, manager, runtimeState, "! curl -fsS --connect-timeout 1 --max-time 3 http://127.0.0.1:48080/ >/dev/null 2>&1")

	assertConcurrentExec(t, manager, runtimeState)
	reuseStart := time.Now()
	reused, err := manager.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if reused.ID != runtimeState.ID || time.Since(reuseStart) > 3*time.Second {
		t.Fatalf("warm VM reuse took %s and returned %s", time.Since(reuseStart), reused.ID)
	}
	// A resource or image change recreates the stable VM identity. This path
	// must rotate the file-backed token after the old guest releases its bind
	// mount. Otherwise, the new guest can keep clipboard authority from the old
	// VM lifetime.
	recreatedRequest := request
	recreatedRequest.CPUs++
	recreated, err := manager.Start(context.Background(), recreatedRequest)
	if err != nil {
		t.Fatal(err)
	}
	runtimeState = recreated
	request = recreatedRequest
	recreatedTokenMetadata, err := clipboard.ReadTokenMetadata(clipboard.TokenFilePath(driver.CooperDir(), runtimeState.ID))
	if err != nil {
		t.Fatal(err)
	}
	if recreatedTokenMetadata.Token == oldToken {
		t.Fatal("implicit VM recreation did not rotate the clipboard token")
	}
	response, _, err = driver.ClipboardGet("/clipboard/type", oldToken)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 401 {
		t.Fatalf("old token after implicit VM recreation status = %d, want 401", response.StatusCode)
	}
	oldToken = recreatedTokenMetadata.Token

	if err := driver.App().RestartWorkload(runtimeState.ID); err != nil {
		t.Fatal(err)
	}
	newTokenMetadata, err := clipboard.ReadTokenMetadata(clipboard.TokenFilePath(driver.CooperDir(), runtimeState.ID))
	if err != nil {
		t.Fatal(err)
	}
	if newTokenMetadata.Token == oldToken {
		t.Fatal("VM restart did not rotate the clipboard token")
	}
	response, _, err = driver.ClipboardGet("/clipboard/type", oldToken)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 401 {
		t.Fatalf("old VM clipboard token status = %d, want 401", response.StatusCode)
	}
	if healthy, err := manager.Healthy(runtimeState); err != nil || !healthy {
		t.Fatalf("restarted VM health = %t, %v", healthy, err)
	}

	if err := exec.Command("docker", "rm", "-f", runtimeState.RelayName).Run(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, func() bool {
		healthy, _ := manager.Healthy(runtimeState)
		return !healthy
	}, "VM health did not detect the stopped relay")
	execVM(t, manager, runtimeState, "! curl -fsS --connect-timeout 1 --max-time 4 https://vm-allowed.cooper.test/ >/dev/null 2>&1")
	if _, err := manager.Start(context.Background(), request); err == nil || !strings.Contains(err.Error(), "cooper vm stop") {
		t.Fatalf("unhealthy VM start error = %v, want explicit recovery action", err)
	}
	if _, err := os.Stat(runtimeState.RuntimeDir); err != nil {
		t.Fatalf("unhealthy VM diagnostics were removed: %v", err)
	}
	if err := driver.App().RestartWorkload(runtimeState.ID); err != nil {
		t.Fatal(err)
	}

	_ = execVMError(manager, runtimeState, "docker stop -t 1 \"$HOSTNAME\"")
	waitFor(t, 10*time.Second, func() bool {
		healthy, _ := manager.Healthy(runtimeState)
		return !healthy
	}, "VM health did not detect the stopped agent container")
	if err := driver.App().RestartWorkload(runtimeState.ID); err != nil {
		t.Fatal(err)
	}
	assertVirtioFSDLogsClean(t, filepath.Join(runtimeState.RuntimeDir, "supervisor"))

	if err := driver.App().StopWorkload(runtimeState.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(clipboard.TokenFilePath(driver.CooperDir(), runtimeState.ID)); !os.IsNotExist(err) {
		t.Fatalf("VM stop did not revoke clipboard token: %v", err)
	}
	assertRuntimeRemoved(t, runtimeState)
	runBuiltInParityMatrix(t, driver, homeDir, preparedBase, cooperBinary)
}

func TestCooperVMSelfHosting(t *testing.T) {
	if os.Getenv("COOPER_RUN_VM_E2E") != "1" {
		t.Skip("set COOPER_RUN_VM_E2E=1 to run the Linux KVM gate")
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Fatalf("VM E2E requires Linux x86-64, got %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	preparedBase := requiredFile(t, "COOPER_VM_PREPARED_BASE")
	cooperBinary := requiredFile(t, "COOPER_VM_BINARY")
	repositoryRoot := repositoryRoot(t)

	lock, err := testdocker.SetupPackageNamed("vm-self-host", false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := lock.Release(); err != nil {
			t.Errorf("release Docker test lock: %v", err)
		}
	}()

	driver, err := testdriver.New(testdriver.Options{
		ImagePrefix:          outerImagePrefix,
		DisableHostClipboard: true,
		ConfigMutator: func(cfg *config.Config) {
			cfg.MonitorTimeoutSecs = 1
			cfg.ProgrammingTools = []config.ToolConfig{{Name: "go", Enabled: true, Mode: config.ModePin, PinnedVersion: "1.25.0", ContainerVersion: "1.25.0"}}
			cfg.AITools = []config.ToolConfig{{Name: "codex", Enabled: true, Mode: config.ModePin, PinnedVersion: "0.117.0", ContainerVersion: "0.117.0"}}
			cfg.VM = config.VMConfig{CPUs: 8, MemoryMiB: 12288, DiskGiB: 32, MaxDepth: 2, StartTimeoutS: 300, StopTimeoutS: 15}
			appendSelfHostDomains(cfg)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	homeDir := driver.HomeDir()
	t.Setenv("HOME", homeDir)
	writeFile(t, filepath.Join(homeDir, ".gitconfig"), "[user]\n\tname = Cooper Self Host Test\n")
	writeFile(t, filepath.Join(homeDir, ".codex", "cooper-self-host-sentinel"), "outer-state\n")
	defer func() {
		docker.SetRuntimeNamespace(vmE2ENamespace)
		if err := driver.Close(); err != nil {
			t.Errorf("close self-host test driver: %v", err)
		}
		docker.SetImagePrefix(testdocker.ImagePrefix)
	}()
	docker.SetRuntimeNamespace(vmE2ENamespace)
	if err := driver.BuildConfiguredImages(os.Stderr); err != nil {
		t.Fatal(err)
	}
	if err := (vm.InfrastructureImages{
		CooperDir: driver.CooperDir(), Prefix: outerImagePrefix,
		Executable: cooperBinary, Out: os.Stderr,
	}).Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	allowSelfSignedProxyFixture(t, driver.CooperDir())
	if err := driver.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The nested chain and the Docker-backed proxy package tests use this
	// self-signed, local-only target. A nested Squid must use the outer Squid as
	// its parent. Give the outer target the package fixture aliases so those
	// tests stay local and deterministic across both proxy levels.
	target, err := testdocker.StartHTTPSTarget(
		"vm-allowed.cooper.test",
		"vm-blocked.cooper.test",
		"api.anthropic.com",
		"example.com",
		"cli-chat-proxy.grok.com",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Remove()

	stageDir, err := os.MkdirTemp(repositoryRoot, ".cooper-vm-self-host-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.RemoveAll(stageDir); err != nil {
			t.Errorf("remove self-host staging directory: %v", err)
		}
	}()
	stagedBase := filepath.Join(stageDir, "cooper-guest-base.qcow2")
	stageFile(t, preparedBase, stagedBase)
	stageFile(t, preparedBase+".json", stagedBase+".json")
	nestedConfig := config.DefaultConfig()
	nestedConfig.ProxyPort = 41281
	nestedConfig.BridgePort = 41282
	nestedConfig.MonitorTimeoutSecs = 1
	// A depth-two VM imports the full Codex development image through nested
	// virtualization. A 3.01 GB archive did not finish loading in 15 minutes
	// with two CPUs, 3 GiB of memory, and a 16 GiB disk. Use the documented
	// self-host resource profile and keep a measured, bounded cold-start wait.
	nestedConfig.VM = config.VMConfig{CPUs: 4, MemoryMiB: 4096, DiskGiB: 24, MaxDepth: 2, StartTimeoutS: 1200, StopTimeoutS: 15}
	appendSelfHostDomains(nestedConfig)
	stagedConfig := filepath.Join(stageDir, "config.json")
	if err := config.SaveConfig(stagedConfig, nestedConfig); err != nil {
		t.Fatal(err)
	}
	stagedBinary := filepath.Join(stageDir, "cooper-self-host")

	manager, _, outerRuntime, _ := startVM(t, driver, homeDir, preparedBase, cooperBinary, repositoryRoot, "codex", "x11")
	defer manager.Stop(context.Background(), outerRuntime)
	diagnostic, err := manager.GuestDiagnostic(context.Background(), outerRuntime)
	if err != nil {
		t.Fatal(err)
	}
	if diagnostic.Depth != 1 || !diagnostic.KVMAvailable || len(diagnostic.ExternalInterfaces) != 0 || len(diagnostic.DefaultRoutes) != 0 {
		t.Fatalf("outer self-host diagnostic = %#v", diagnostic)
	}
	assertHostBoundary(t, outerRuntime, repositoryRoot, homeDir)

	selfHostScript := fmt.Sprintf(`
set -eu
self_host_binary=%s
test -c /dev/kvm
grep -Eq '(^|[[:space:]])(svm|vmx)([[:space:]]|$)' /proc/cpuinfo
test "$(cat "$HOME/.codex/cooper-self-host-sentinel")" = outer-state
docker info >/dev/null
if env -u HTTP_PROXY -u HTTPS_PROXY -u http_proxy -u https_proxy curl -fsS --connect-timeout 2 --max-time 4 http://1.1.1.1/ >/dev/null 2>&1; then exit 51; fi

if ! go build -C ./cooper -o "$self_host_binary" . >/tmp/self-host-build.log 2>&1; then
    cat /tmp/self-host-build.log >&2
    printf '%%s\n' '--- Cooper VM Go proxy diagnostics ---' >&2
    env | grep -Ei '^(HTTP|HTTPS|NO)_PROXY=|^(http|https|no)_proxy=' | sort >&2 || true
    go env GOENV GOPROXY GONOPROXY GOPRIVATE GOSUMDB >&2 || true
    getent ahostsv4 proxy.golang.org >&2 || true
    attempt=1
    while [ "$attempt" -le 3 ]; do
        printf '%%s\n' "--- curl attempt $attempt ---" >&2
        curl -sv --http1.1 --connect-timeout 5 --max-time 20 \
            https://proxy.golang.org/github.com/hashicorp/yamux/@v/v0.1.2.info \
            -o /tmp/cooper-go-proxy-response >&2 || true
        attempt=$((attempt + 1))
    done
    exit 52
fi
printf 'SELF_HOST_BUILD_OK\n'

# Docker-backed packages share one lock. Run packages in sequence so a package
# does not spend its timeout waiting for another cold image build. Keep an
# explicit per-package bound above Go's short default test timeout.
if ! go test -C ./cooper -p=1 ./... -count=1 -timeout=30m >/tmp/self-host-go-test.log 2>&1; then
    tail -n 2000 /tmp/self-host-go-test.log >&2
    exit 53
fi
printf 'SELF_HOST_GO_TEST_OK\n'

mkdir -p "${HOME}/.cooper/vm/assets/schema-1"
cp %s "${HOME}/.cooper/vm/assets/schema-1/cooper-guest-base.qcow2"
cp %s "${HOME}/.cooper/vm/assets/schema-1/cooper-guest-base.qcow2.json"
cp %s "${HOME}/.cooper/config.json"
chmod 0444 "${HOME}/.cooper/vm/assets/schema-1/cooper-guest-base.qcow2" "${HOME}/.cooper/vm/assets/schema-1/cooper-guest-base.qcow2.json"
docker tag %s %s

if ! "$self_host_binary" --config "${HOME}/.cooper" --prefix %s --runtime-namespace %s build >/tmp/self-host-docker-build.log 2>&1; then
    tail -n 300 /tmp/self-host-docker-build.log >&2
    exit 54
fi
docker image inspect %s >/dev/null
docker image inspect %s >/dev/null
printf 'SELF_HOST_DOCKER_BUILD_OK\n'

if ! go test -C ./cooper ./internal/docker -run '^TestEnsureNetworks$' -count=1 >/tmp/self-host-docker-test.log 2>&1; then
    cat /tmp/self-host-docker-test.log >&2
    exit 55
fi
printf 'SELF_HOST_DOCKER_TEST_OK\n'

if ! COOPER_NESTED_HARNESS=1 \
    COOPER_SELF_HOST_CONFIG="${HOME}/.cooper" \
    COOPER_SELF_HOST_BINARY="$self_host_binary" \
    COOPER_SELF_HOST_TARGET=vm-allowed.cooper.test \
    go test -C ./cooper -v ./internal/vme2e -run '^TestNestedCooperHarness$' -count=1 -timeout=30m >/tmp/self-host-nested.log 2>&1; then
    tail -n 1200 /tmp/self-host-nested.log >&2
    exit 56
fi
grep -Fq NESTED_COOPER_HARNESS_OK /tmp/self-host-nested.log
printf 'SELF_HOST_DEPTH_TWO_OK\n'
`, shellQuote(stagedBinary), shellQuote(stagedBase), shellQuote(stagedBase+".json"), shellQuote(stagedConfig),
		shellQuote(docker.GetImageCLI("codex")), shellQuote(selfImagePrefix+"cooper-cli-codex"),
		shellQuote(selfImagePrefix), shellQuote(selfNamespace),
		shellQuote(selfImagePrefix+"cooper-proxy"), shellQuote(selfImagePrefix+"cooper-base"))
	output := execVM(t, manager, outerRuntime, selfHostScript)
	for _, marker := range []string{
		"SELF_HOST_BUILD_OK", "SELF_HOST_GO_TEST_OK", "SELF_HOST_DOCKER_BUILD_OK",
		"SELF_HOST_DOCKER_TEST_OK", "SELF_HOST_DEPTH_TWO_OK",
	} {
		if !strings.Contains(output, marker) {
			t.Fatalf("self-host output has no %s marker:\n%s", marker, output)
		}
	}
	if strings.TrimSpace(readFile(t, filepath.Join(homeDir, ".codex", "cooper-inner-write"))) != "inner-state" {
		t.Fatal("depth-two state write did not cross both virtiofs boundaries")
	}
	assertHostFileOwner(t, filepath.Join(homeDir, ".codex", "cooper-inner-write"), os.Getuid(), os.Getgid())
	waitFor(t, 5*time.Second, func() bool {
		data, err := os.ReadFile(filepath.Join(driver.CooperDir(), "logs", "access.log"))
		return err == nil && bytes.Contains(data, []byte("vm-allowed.cooper.test"))
	}, "outer Squid log has no chained self-host request")

	if err := manager.Stop(context.Background(), outerRuntime); err != nil {
		t.Fatal(err)
	}
	assertRuntimeRemoved(t, outerRuntime)
}

func assertHostFileOwner(t *testing.T, path string, uid, gid int) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat host file %s: %v", path, err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("host file %s has no Unix owner information", path)
	}
	if int(stat.Uid) != uid || int(stat.Gid) != gid {
		t.Fatalf("host file %s owner is %d:%d; want %d:%d", path, stat.Uid, stat.Gid, uid, gid)
	}
}

func assertVirtioFSDLogsClean(t *testing.T, root string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, "*", "supervisor", "virtiofsd*.log"))
	if err != nil {
		t.Fatalf("find nested virtiofsd logs: %v", err)
	}
	if len(matches) == 0 {
		matches, err = filepath.Glob(filepath.Join(root, "virtiofsd*.log"))
		if err != nil {
			t.Fatalf("find virtiofsd logs: %v", err)
		}
	}
	if len(matches) == 0 {
		t.Fatalf("no virtiofsd logs found below %s", root)
	}
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read virtiofsd log %s: %v", path, err)
		}
		if bytes.Contains(data, []byte("ERROR")) || bytes.Contains(data, []byte("Operation not permitted")) {
			t.Fatalf("virtiofsd log %s contains an error:\n%s", path, data)
		}
	}
}

type stateTarget struct {
	hostPath  string
	guestPath string
	file      bool
}

type builtInAgent struct {
	name    string
	targets []stateTarget
}

func runBuiltInParityMatrix(t *testing.T, driver *testdriver.Driver, homeDir, preparedBase, cooperBinary string) {
	t.Helper()
	account, err := usercontext.Current()
	if err != nil {
		t.Fatal(err)
	}
	account.Home = homeDir
	agents := builtInAgents(homeDir)
	for _, agent := range agents {
		writeAgentStateSentinels(t, agent)
	}

	// A focused subtest run must check writes only for agents it started.
	var startedAgents []builtInAgent
	for _, agent := range agents {
		agent := agent
		t.Run("built-in-parity-"+agent.name, func(t *testing.T) {
			startedAgents = append(startedAgents, agent)
			workspace := t.TempDir()
			writeFile(t, filepath.Join(workspace, "parity-workspace"), agent.name+"\n")

			barrel, err := driver.StartBarrelInWorkspace(agent.name, workspace)
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(agent.targets[0].hostPath, ".cooper-vm-live-cli"), "cli-live\n")
			cliOutput, err := driver.ExecBarrel(barrel.Name, agentParityScript(agent, agents, workspace, "cli", account))
			if err != nil {
				t.Fatal(err)
			}
			if err := driver.StopBarrel(barrel.Name); err != nil {
				t.Fatal(err)
			}
			assertStateWrite(t, agent, "cli")
			// OpenCode can refresh its cache root during a normal version command.
			// Restore input sentinels so VM mode starts from the same host state.
			writeAgentStateSentinels(t, agent)

			clipboardMode, err := docker.ToolClipboardMode(agent.name)
			if err != nil {
				t.Fatal(err)
			}
			manager, _, runtimeState, _ := startVM(t, driver, homeDir, preparedBase, cooperBinary, workspace, agent.name, clipboardMode)
			writeFile(t, filepath.Join(agent.targets[0].hostPath, ".cooper-vm-live-vm"), "vm-live\n")
			vmOutput := execVM(t, manager, runtimeState, agentParityScript(agent, agents, workspace, "vm", account))
			if err := manager.Stop(context.Background(), runtimeState); err != nil {
				t.Fatal(err)
			}
			assertRuntimeRemoved(t, runtimeState)
			assertStateWrite(t, agent, "vm")

			if strings.TrimSpace(cliOutput) != strings.TrimSpace(vmOutput) {
				t.Fatalf("%s CLI and VM output differ:\nCLI: %q\nVM:  %q", agent.name, cliOutput, vmOutput)
			}
		})
	}

	for _, agent := range agents {
		writeAgentStateSentinels(t, agent)
	}
	if err := vm.RemoveCache(driver.CooperDir()); err != nil {
		t.Fatal(err)
	}
	for _, agent := range startedAgents {
		for _, target := range agent.targets {
			if target.file {
				if !strings.Contains(readFile(t, target.hostPath), "cooper-vm-state-"+agent.name) {
					t.Fatalf("cleanup changed %s state file %s", agent.name, target.hostPath)
				}
				continue
			}
			for _, runtimeKind := range []string{"cli", "vm"} {
				assertStateWrite(t, agent, runtimeKind)
			}
		}
	}
}

func builtInAgents(homeDir string) []builtInAgent {
	stateDir := func(relative string) stateTarget {
		return stateTarget{hostPath: filepath.Join(homeDir, relative), guestPath: filepath.Join(homeDir, relative)}
	}
	return []builtInAgent{
		{name: "claude", targets: []stateTarget{
			stateDir(".claude"),
			{hostPath: filepath.Join(homeDir, ".claude.json"), guestPath: filepath.Join(homeDir, ".claude.json"), file: true},
		}},
		{name: "copilot", targets: []stateTarget{stateDir(".copilot"), stateDir(".cache/copilot")}},
		{name: "codex", targets: []stateTarget{stateDir(".codex"), stateDir(".agents"), stateDir(".claude-plugin"), stateDir(".cursor-plugin")}},
		{name: "opencode", targets: []stateTarget{
			stateDir(".cache/opencode"),
			stateDir(".config/opencode"),
			stateDir(".local/share/opencode"),
			stateDir(".local/state/opencode"),
			stateDir(".opencode"),
		}},
		{name: "grok", targets: []stateTarget{stateDir("grok-state"), stateDir(".agents")}},
	}
}

func writeAgentStateSentinels(t *testing.T, agent builtInAgent) {
	t.Helper()
	for _, target := range agent.targets {
		if target.file {
			writeFile(t, target.hostPath, "{\"cooper\":\"cooper-vm-state-"+agent.name+"\"}\n")
			continue
		}
		writeFile(t, filepath.Join(target.hostPath, ".cooper-vm-state-"+agent.name), agent.name+"\n")
	}
}

func agentParityScript(selected builtInAgent, all []builtInAgent, workspace, runtimeKind string, account usercontext.Account) string {
	var checks strings.Builder
	fmt.Fprintf(&checks, "set -eu\ntest \"$PWD\" = %s\ntest \"$(cat parity-workspace)\" = %s\ntest \"$COOPER_CLI_TOOL\" = %s\n", shellQuote(workspace), shellQuote(selected.name), shellQuote(selected.name))
	fmt.Fprintf(&checks, "test \"$HOME\" = %s\ntest \"$USER\" = %s\ntest \"$LOGNAME\" = %s\ntest \"$(id -un)\" = %s\ntest \"$(id -u)\" = %d\ntest \"$(id -g)\" = %d\ntest \"$(getent passwd \"$(id -u)\" | cut -d: -f6)\" = %s\n", shellQuote(account.Home), shellQuote(account.Name), shellQuote(account.Name), shellQuote(account.Name), account.UID, account.GID, shellQuote(account.Home))
	for _, agent := range all {
		for _, target := range agent.targets {
			if target.file {
				if stateIsSelected(target, selected) {
					fmt.Fprintf(&checks, "grep -Fq %s %s\n", shellQuote("cooper-vm-state-"+agent.name), shellQuote(target.guestPath))
				} else {
					fmt.Fprintf(&checks, "! grep -Fq %s %s 2>/dev/null\n", shellQuote("cooper-vm-state-"+agent.name), shellQuote(target.guestPath))
				}
				continue
			}
			sentinel := filepath.Join(target.guestPath, ".cooper-vm-state-"+agent.name)
			if stateIsSelected(target, selected) {
				fmt.Fprintf(&checks, "test \"$(cat %s)\" = %s\n", shellQuote(sentinel), shellQuote(agent.name))
			} else {
				fmt.Fprintf(&checks, "! test -e %s\n", shellQuote(sentinel))
			}
		}
	}
	firstDir := selected.targets[0]
	if firstDir.file {
		panic("each built-in agent needs a directory state root first")
	}
	fmt.Fprintf(&checks, "test \"$(cat %s)\" = %s\n", shellQuote(filepath.Join(firstDir.guestPath, ".cooper-vm-live-"+runtimeKind)), shellQuote(runtimeKind+"-live"))
	if selected.name == "grok" {
		fmt.Fprintf(&checks, "test \"$GROK_HOME\" = %s\n", shellQuote(firstDir.guestPath))
	}
	fmt.Fprintf(&checks, "command -v %s >/dev/null\nprintf 'tool=%s version='\nNO_COLOR=1 %s --version 2>&1 | tr -d '\\r' | head -n 1\n", selected.name, selected.name, selected.name)
	// Some agents can refresh or replace a cache root while they start. Write
	// after the version command so the assertion measures the mounted root,
	// not the tool's valid cache cleanup policy.
	fmt.Fprintf(&checks, "printf %s > %s\n", shellQuote(runtimeKind+"\n"), shellQuote(filepath.Join(firstDir.guestPath, ".cooper-vm-write-"+runtimeKind)))
	return checks.String()
}

func stateIsSelected(target stateTarget, selected builtInAgent) bool {
	for _, entry := range selected.targets {
		if entry.hostPath == target.hostPath {
			return true
		}
	}
	return false
}

func assertStateWrite(t *testing.T, agent builtInAgent, runtimeKind string) {
	t.Helper()
	path := filepath.Join(agent.targets[0].hostPath, ".cooper-vm-write-"+runtimeKind)
	if strings.TrimSpace(readFile(t, path)) != runtimeKind {
		t.Fatalf("%s %s state write did not reach the host", agent.name, runtimeKind)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func responseStatus(response *http.Response) any {
	if response == nil {
		return nil
	}
	return response.StatusCode
}

func startVM(t *testing.T, driver *testdriver.Driver, homeDir, preparedBase, cooperBinary, workspace, tool, clipboardMode string) (vm.Manager, vm.StartRequest, vm.Runtime, string) {
	t.Helper()
	runtimeID, err := vm.RuntimeID(vmE2ENamespace, workspace, tool)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtimefs.SyncTimezoneFile(driver.CooperDir(), runtimeID); err != nil {
		t.Fatal(err)
	}
	token, err := clipboard.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clipboard.WriteRuntimeToken(driver.CooperDir(), runtimeID, token, clipboard.RuntimeVM, tool, clipboardMode); err != nil {
		t.Fatal(err)
	}
	manager := vm.Manager{
		CooperDir: driver.CooperDir(), HomeDir: homeDir, Namespace: vmE2ENamespace,
		ImagePrefix: driver.ImagePrefix(), ProxyName: docker.ProxyContainerName(), Config: driver.Config(),
		Out: os.Stderr, Executable: cooperBinary, SkipPrepare: true, PreparedBase: preparedBase,
	}
	driver.App().AdoptVMManager(manager)
	request := vm.StartRequest{
		WorkspaceDir: workspace, ToolName: tool, ImageRef: docker.GetImageCLI(tool), RuntimeID: runtimeID,
		CPUs: driver.Config().VM.CPUs, MemoryMiB: driver.Config().VM.MemoryMiB,
		DiskGiB: driver.Config().VM.DiskGiB, ClipboardMode: clipboardMode,
	}
	runtimeState, err := manager.Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	activeToken, err := clipboard.ReadTokenMetadata(clipboard.TokenFilePath(driver.CooperDir(), runtimeID))
	if err != nil {
		t.Fatal(err)
	}
	return manager, request, runtimeState, activeToken.Token
}

func execVM(t *testing.T, manager vm.Manager, runtimeState vm.Runtime, script string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := manager.ExecCommand(context.Background(), runtimeState, []string{"bash", "-lc", script}, nil, false, nil, &stdout, &stderr)
	if err != nil {
		t.Fatalf("VM command failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func execVMError(manager vm.Manager, runtimeState vm.Runtime, script string) error {
	return manager.ExecCommand(context.Background(), runtimeState, []string{"bash", "-lc", script}, nil, false, nil, io.Discard, io.Discard)
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func assertConcurrentExec(t *testing.T, manager vm.Manager, runtimeState vm.Runtime) {
	t.Helper()
	var wait sync.WaitGroup
	errors := make(chan error, 2)
	for index := 0; index < 2; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			var output bytes.Buffer
			err := manager.ExecCommand(context.Background(), runtimeState,
				[]string{"bash", "-lc", "sleep 0.2; printf concurrent-" + strconv.Itoa(index)}, nil, false, nil, &output, io.Discard)
			if err == nil && output.String() != "concurrent-"+strconv.Itoa(index) {
				err = fmt.Errorf("concurrent output = %q", output.String())
			}
			errors <- err
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func assertHostBoundary(t *testing.T, runtimeState vm.Runtime, workspace, homeDir string) {
	t.Helper()
	var inspection struct {
		Config     struct{ User string }
		HostConfig struct {
			NetworkMode    string
			Privileged     bool
			ReadonlyRootfs bool
			CapDrop        []string
			SecurityOpt    []string
			PidsLimit      int64
			Memory         int64
			NanoCpus       int64
			Init           *bool
			Devices        []struct{ PathOnHost string }
			Binds          []string
		}
		Mounts []struct {
			Source      string
			Destination string
			RW          bool
		}
	}
	data, err := exec.Command("docker", "inspect", runtimeState.ContainerName).Output()
	if err != nil {
		t.Fatal(err)
	}
	var values []json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil || len(values) != 1 {
		t.Fatalf("decode supervisor inspection: %v", err)
	}
	if err := json.Unmarshal(values[0], &inspection); err != nil {
		t.Fatal(err)
	}
	if inspection.HostConfig.NetworkMode != "none" || inspection.HostConfig.Privileged || !inspection.HostConfig.ReadonlyRootfs {
		t.Fatalf("unsafe supervisor host config: %#v", inspection.HostConfig)
	}
	if !contains(inspection.HostConfig.CapDrop, "ALL") || !hasSecurityOption(inspection.HostConfig.SecurityOpt, "no-new-privileges") ||
		inspection.HostConfig.PidsLimit != 1024 || inspection.HostConfig.Memory <= 0 || inspection.HostConfig.NanoCpus <= 0 ||
		inspection.HostConfig.Init == nil || !*inspection.HostConfig.Init {
		t.Fatalf("supervisor resource or process boundary is incomplete: %#v", inspection.HostConfig)
	}
	if inspection.Config.User == "" || strings.HasPrefix(inspection.Config.User, "0:") || len(inspection.HostConfig.Devices) != 1 || inspection.HostConfig.Devices[0].PathOnHost != "/dev/kvm" {
		t.Fatalf("unsafe supervisor user or devices: user=%q devices=%#v", inspection.Config.User, inspection.HostConfig.Devices)
	}
	runtimeMount := false
	readOnlyRelayMount := false
	for _, mount := range inspection.Mounts {
		if strings.Contains(mount.Source, "docker.sock") {
			t.Fatalf("physical host Docker socket entered supervisor: %#v", mount)
		}
		if mount.Destination == "/cooper/runtime" {
			runtimeMount = mount.Source == vm.SupervisorRuntimeDir(runtimeState.RuntimeDir) && mount.RW
		}
		if mount.Destination == "/cooper/relay" {
			readOnlyRelayMount = !mount.RW
		}
		if mount.Source == runtimeState.RuntimeDir || strings.HasSuffix(mount.Source, "/runtime.json") || strings.HasSuffix(mount.Source, "/relay-policy.json") {
			t.Fatalf("supervisor can change host-owned runtime policy: %#v", mount)
		}
	}
	if !runtimeMount || !readOnlyRelayMount {
		t.Fatalf("supervisor private or relay mounts are unsafe: %#v", inspection.Mounts)
	}

	relayData, err := exec.Command("docker", "inspect", runtimeState.RelayName).Output()
	if err != nil {
		t.Fatal(err)
	}
	values = nil
	if err := json.Unmarshal(relayData, &values); err != nil || len(values) != 1 {
		t.Fatalf("decode relay inspection: %v", err)
	}
	var relay struct {
		Config     struct{ User string }
		HostConfig struct {
			NetworkMode    string
			Privileged     bool
			ReadonlyRootfs bool
			CapDrop        []string
			SecurityOpt    []string
			PidsLimit      int64
			Memory         int64
			NanoCpus       int64
			Devices        []struct{ PathOnHost string }
		}
		Mounts []struct {
			Source, Destination string
			RW                  bool
		}
	}
	if err := json.Unmarshal(values[0], &relay); err != nil {
		t.Fatal(err)
	}
	if relay.Config.User == "" || strings.HasPrefix(relay.Config.User, "0:") || relay.HostConfig.Privileged ||
		!relay.HostConfig.ReadonlyRootfs || relay.HostConfig.NetworkMode != runtimeState.RelayNetwork ||
		!contains(relay.HostConfig.CapDrop, "ALL") || !hasSecurityOption(relay.HostConfig.SecurityOpt, "no-new-privileges") ||
		relay.HostConfig.PidsLimit != 300 || relay.HostConfig.Memory <= 0 || relay.HostConfig.NanoCpus <= 0 || len(relay.HostConfig.Devices) != 0 {
		t.Fatalf("unsafe relay config: %#v", relay)
	}
	relaySocketMount := false
	relayPolicyMount := false
	for _, mount := range relay.Mounts {
		if strings.HasPrefix(mount.Source, workspace) || strings.HasPrefix(mount.Source, homeDir) {
			t.Fatalf("relay received host workspace or home: %#v", mount)
		}
		if strings.Contains(mount.Source, "docker.sock") || strings.Contains(mount.Source, "/exports") || strings.Contains(mount.Source, "/control") {
			t.Fatalf("relay received a forbidden host path: %#v", mount)
		}
		switch mount.Destination {
		case "/cooper/relay":
			relaySocketMount = mount.RW
		case "/cooper/live":
			relayPolicyMount = !mount.RW
		default:
			t.Fatalf("relay received an unexpected mount: %#v", mount)
		}
	}
	if !relaySocketMount || !relayPolicyMount || len(relay.Mounts) != 2 {
		t.Fatalf("relay mounts are incomplete: %#v", relay.Mounts)
	}
}

func hasSecurityOption(options []string, want string) bool {
	for _, option := range options {
		if option == want || strings.HasPrefix(option, want+":") {
			return true
		}
	}
	return false
}

func assertRuntimeRemoved(t *testing.T, runtimeState vm.Runtime) {
	t.Helper()
	for _, name := range []string{runtimeState.ContainerName, runtimeState.RelayName} {
		if err := exec.Command("docker", "inspect", name).Run(); err == nil {
			t.Fatalf("container %s remains after VM stop", name)
		}
	}
	if err := exec.Command("docker", "network", "inspect", runtimeState.RelayNetwork).Run(); err == nil {
		t.Fatalf("network %s remains after VM stop", runtimeState.RelayNetwork)
	}
	for _, path := range []string{runtimeState.RuntimeDir, runtimeState.ControlDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("VM transient path %s remains: %v", path, err)
		}
	}
}

func assertNestedResourcesRemoved(t *testing.T) {
	t.Helper()
	containers, err := exec.Command("docker", "ps", "-aq", "--filter", "name=^/"+selfNamespace+"-").CombinedOutput()
	if err != nil {
		t.Fatalf("list nested Cooper containers: %v\n%s", err, containers)
	}
	if strings.TrimSpace(string(containers)) != "" {
		t.Fatalf("nested Cooper containers remain after shutdown: %s", containers)
	}
	networks, err := exec.Command("docker", "network", "ls", "--format", "{{.Name}}", "--filter", "name=^"+selfNamespace+"-").CombinedOutput()
	if err != nil {
		t.Fatalf("list nested Cooper networks: %v\n%s", err, networks)
	}
	if strings.TrimSpace(string(networks)) != "" {
		t.Fatalf("nested Cooper networks remain after shutdown: %s", networks)
	}
}

func appendSelfHostDomains(cfg *config.Config) {
	for _, domain := range []string{
		"vm-allowed.cooper.test",
		// The self-host suite starts one outer fixture for proxy integration
		// tests that run behind the required nested parent-proxy chain.
		"api.anthropic.com",
		"example.com",
		"cli-chat-proxy.grok.com",
		"registry.npmjs.org",
		"proxy.golang.org",
		"sum.golang.org",
		"go.dev",
		"pypi.org",
		// PyPI serves package metadata and package files from different hosts.
		// The self-host test needs both hosts to rebuild Cooper's Python tools.
		"files.pythonhosted.org",
		"auth.docker.io",
		"registry-1.docker.io",
		"production.cloudflare.docker.com",
		"production.cloudfront.docker.com",
		"docker.io",
		"dl-cdn.alpinelinux.org",
		"deb.debian.org",
		"nodejs.org",
		"www.squid-cache.org",
		"claude.ai",
		"downloads.claude.ai",
		"release-assets.githubusercontent.com",
		"x.ai",
	} {
		cfg.WhitelistedDomains = append(cfg.WhitelistedDomains, config.DomainEntry{Domain: domain, Source: "user"})
	}
}

func allowSelfSignedProxyFixture(t *testing.T, cooperDir string) {
	t.Helper()
	squidPath := filepath.Join(cooperDir, "proxy", "squid.conf")
	squidConfig, err := os.ReadFile(squidPath)
	if err != nil {
		t.Fatalf("read self-host Squid fixture config: %v", err)
	}
	squidConfig = append(squidConfig, []byte("\n# Local self-host fixture only.\nsslproxy_cert_error allow all\n")...)
	if err := os.WriteFile(squidPath, squidConfig, 0o644); err != nil {
		t.Fatalf("write self-host Squid fixture config: %v", err)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("find VM E2E source path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "cooper", "go.mod")); err != nil {
		t.Fatalf("find repository root %s: %v", root, err)
	}
	return root
}

func stageFile(t *testing.T, source, target string) {
	t.Helper()
	if err := os.Link(source, target); err == nil {
		return
	}
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}

func stageClipboard(t *testing.T, driver *testdriver.Driver) {
	t.Helper()
	png := minimalPNG(t)
	object := clipboard.ClipboardObject{
		Kind: clipboard.ClipboardKindImage, MIME: "image/png", Raw: png, RawSize: int64(len(png)),
		Variants: map[string]clipboard.ClipboardVariant{
			"image/png": {MIME: "image/png", Bytes: png, Size: int64(len(png)), Width: 1, Height: 1},
		},
	}
	if _, err := driver.StageClipboard(object, time.Minute); err != nil {
		t.Fatal(err)
	}
}

func minimalPNG(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func startHTTPServer(t *testing.T, body string) (int, func()) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				buffer := make([]byte, 4096)
				_, _ = connection.Read(buffer)
				_, _ = fmt.Fprintf(connection, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
			}()
		}
	}()
	return listener.Addr().(*net.TCPAddr).Port, func() { _ = listener.Close() }
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal(message)
}

func requiredFile(t *testing.T, name string) string {
	t.Helper()
	path := strings.TrimSpace(os.Getenv(name))
	if path == "" {
		t.Fatalf("%s is required", name)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("%s does not name a regular file: %v", name, err)
	}
	return absolute
}

func requiredDirectory(t *testing.T, name string) string {
	t.Helper()
	path := strings.TrimSpace(os.Getenv(name))
	if path == "" {
		t.Fatalf("%s is required", name)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		t.Fatalf("%s does not name a directory: %v", name, err)
	}
	return absolute
}

func writeFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeExecutable(t *testing.T, path, value string) {
	t.Helper()
	writeFile(t, path, value)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
