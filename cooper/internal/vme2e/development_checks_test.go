package vme2e

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/testdocker"
	"github.com/rickchristie/govner/cooper/internal/usercontext"
	"github.com/rickchristie/govner/cooper/internal/vm"
)

func (f *developmentFixture) smoke() {
	firstPort, closeFirst := startHTTPServer(f.t, "first-port-ok")
	defer closeFirst()
	secondPort, closeSecond := startHTTPServer(f.t, "second-port-ok")
	defer closeSecond()
	script := filepath.Join(filepath.Dir(f.run.CooperDir), "bridge.sh")
	writeExecutable(f.t, script, "#!/bin/sh\nprintf 'vm-bridge-ok\\n'\n")
	if err := f.driver.App().UpdateBridgeRoutes([]config.BridgeRoute{{APIPath: "/vm-e2e", ScriptPath: script}}); err != nil {
		f.t.Fatal(err)
	}
	firstGuestPort := 48080
	depth, err := vm.ManagedDepth()
	if err != nil {
		f.t.Fatal(err)
	}
	if depth == 2 {
		firstGuestPort = firstPort
	}
	if err := f.driver.App().UpdatePortForwards([]config.PortForwardRule{{ContainerPort: firstGuestPort, HostPort: firstPort}}); err != nil {
		f.t.Fatal(err)
	}
	target, err := testdocker.StartHTTPSTarget("vm-allowed.cooper.test", "vm-blocked.cooper.test")
	if err != nil {
		f.t.Fatal(err)
	}
	defer target.Remove()
	f.run.Objects = append(f.run.Objects, developmentObject{Type: "container", Name: target.ContainerName, ID: strings.TrimSpace(f.command("docker", "inspect", "--format", "{{.Id}}", target.ContainerName))})
	f.saveRun()
	// Both aliases resolve to a working endpoint. A denied request must fail
	// because of the proxy policy, not a missing DNS record or a dead server.
	for _, domain := range target.Domains {
		f.command("docker", "exec", target.ContainerName, "bash", "-ec", httpsTargetProbe(domain, ""))
	}
	// Check the fixture path before paying for a guest image import.
	f.command("docker", "exec", target.ContainerName, "bash", "-ec", httpsTargetProbe(target.Domains[0], fmt.Sprintf("%s:%d", docker.ProxyContainerName(), f.manifest.Config.ProxyPort)))
	if got := strings.TrimSpace(f.command("docker", "run", "--rm", "--pull=never", "--network", docker.ExternalNetworkName(), "--entrypoint", "curl", docker.GetImageBase(), "--noproxy", "*", "-fsS", "--max-time", "5", fmt.Sprintf("http://%s:%d/", docker.ProxyContainerName(), firstGuestPort))); got != "first-port-ok" {
		f.t.Fatalf("port fixture returned %q", got)
	}
	f.startVM()
	started := time.Now()
	runVMSmokeChecks(f.t, f.driver, f.manager, f.request, f.state, f.token(), f.run.Workspace, f.home, target, firstPort, secondPort)
	err = f.manager.ExecCommand(f.ctx, f.state, []string{"sh", "-c", "exit 23"}, nil, false, nil, io.Discard, io.Discard)
	var exit vm.ExitError
	if !errors.As(err, &exit) || exit.Status != 23 {
		f.t.Fatalf("VM exit status was lost: %v", err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 250*time.Millisecond)
	cancelledAt := time.Now()
	err = f.manager.ExecCommand(ctx, f.state, []string{"sh", "-c", "sleep 30"}, nil, false, nil, io.Discard, io.Discard)
	cancel()
	if err == nil || time.Since(cancelledAt) > 3*time.Second {
		f.t.Fatalf("VM exec cancellation was not bounded: %v", err)
	}
	execVM(f.t, f.manager, f.state, `openssl verify -CAfile /etc/ssl/certs/ca-certificates.crt /usr/local/share/ca-certificates/cooper-ca.crt`)
	f.observeLifetime()
	if f.starts != 1 || f.loads != 1 {
		f.t.Fatalf("smoke performed %d starts and %d image loads; want one each", f.starts, f.loads)
	}
	f.report["assertion_seconds"] = time.Since(started).Seconds()
	f.report["checks"] = []string{"host-boundary", "no-guest-NIC-or-route", "depth-and-KVM", "workspace-and-root-owner", "read-only-hooks", "private-Docker", "sibling-bind", "trusted-local-HTTPS", "reachable-denied-alias", "direct-and-proxy-bypass", "bridge", "live-ports", "clipboard-bytes-and-auth", "shim", "Docker-build-allow-and-deny", "concurrent-exec", "exit-status", "exec-cancel", "warm-reuse", "installed-Cooper-CA"}
	f.stopVM()
}

func (f *developmentFixture) mounts() {
	f.startVM()
	output := execVM(f.t, f.manager, f.state, mountRefreshScript(docker.GetImageCLI(f.tool)))
	if !strings.Contains(output, "VM_NESTED_BIND_REFRESH_OK") {
		f.t.Fatalf("mount refresh: %s", output)
	}
	// A root write from a sibling Docker container must retain host ownership.
	execVM(f.t, f.manager, f.state, fmt.Sprintf(`docker run --rm --user 0 -v "$PWD:/mnt" --entrypoint sh %s -c 'echo mount-owner > /mnt/mount-owner'`, docker.GetImageCLI(f.tool)))
	assertHostFileOwner(f.t, filepath.Join(f.run.Workspace, "mount-owner"), os.Getuid(), os.Getgid())
	assertVirtioFSDLogsClean(f.t, vm.SupervisorRuntimeDir(f.state.RuntimeDir))
	f.report["checks"] = []string{"Docker-bind-create-remove-replace", "timezone-content", "host-ownership", "virtiofs-logs"}
	f.stopVM()
}

func (f *developmentFixture) lifecycle(scenario string) {
	f.startVM()
	oldToken := f.token()
	before := strings.TrimSpace(f.command("docker", "inspect", "--format", "{{.Id}}", f.state.ContainerName))
	execVM(f.t, f.manager, f.state, "printf keep > lifecycle-host; printf private > /var/tmp/cooper-dev-private")
	f.captureRuntime()
	started := time.Now()
	var err error
	switch scenario {
	case "resources":
		// Depth-two workloads cap CPUs at four. Reduce the request so the
		// effective resource setting changes at either supported depth.
		f.request.CPUs--
		f.state, err = f.manager.Start(f.ctx, f.request)
	case "relay":
		f.command("docker", "rm", "-f", f.state.RelayName)
		waitFor(f.t, 5*time.Second, func() bool { healthy, _ := f.manager.Healthy(f.state); return !healthy }, "relay loss was not detected")
		if _, err := f.manager.Start(f.ctx, f.request); err == nil || !strings.Contains(err.Error(), "cooper vm stop") {
			f.t.Fatalf("unhealthy start did not require recovery: %v", err)
		}
		if _, err := os.Stat(f.state.RuntimeDir); err != nil {
			f.t.Fatal("unhealthy start removed diagnostics")
		}
		f.state, err = f.manager.Restart(f.ctx, f.state.ID)
	case "agent":
		_ = execVMError(f.manager, f.state, `docker stop -t 1 "$HOSTNAME"`)
		waitFor(f.t, 10*time.Second, func() bool { healthy, _ := f.manager.Healthy(f.state); return !healthy }, "agent loss was not detected")
		f.state, err = f.manager.Restart(f.ctx, f.state.ID)
	case "restart":
		f.state, err = f.manager.Restart(f.ctx, f.state.ID)
	}
	if err != nil {
		f.t.Fatal(err)
	}
	f.observeLifetime()
	after := strings.TrimSpace(f.command("docker", "inspect", "--format", "{{.Id}}", f.state.ContainerName))
	if before == after || oldToken == f.token() {
		f.t.Fatal("recovery reused the old VM or clipboard token")
	}
	response, _, err := f.driver.ClipboardGet("/clipboard/type", oldToken)
	if err != nil || response.StatusCode != 401 {
		f.t.Fatalf("old clipboard token remains valid: %v %v", responseStatus(response), err)
	}
	if healthy, err := f.manager.Healthy(f.state); err != nil || !healthy {
		f.t.Fatalf("recovered health: %t %v", healthy, err)
	}
	execVM(f.t, f.manager, f.state, `test "$(cat lifecycle-host)" = keep; test ! -e /var/tmp/cooper-dev-private`)
	if f.starts != 2 || f.loads != 2 {
		f.t.Fatalf("lifecycle performed %d starts and %d image loads; want two each", f.starts, f.loads)
	}
	f.report["recovery_seconds"] = time.Since(started).Seconds()
	f.report["checks"] = []string{scenario, "new-VM-identity", "token-rotation-and-revocation", "new-private-disk", "workspace-survives", "recovered-health"}
	f.stopVM()
}

// Selected parity also runs inside a depth-one Cooper VM. It checks account
// and complete host state in both boundaries without a third VM level or
// provider credentials. The release matrix calls the same state assertions.
func (f *developmentFixture) parity() {
	account, err := usercontext.Current()
	if err != nil {
		f.t.Fatal(err)
	}
	agents := builtInAgents(f.home)
	var selected builtInAgent
	for _, agent := range agents {
		writeAgentStateSentinels(f.t, agent)
		if agent.name == f.tool {
			selected = agent
		}
	}
	writeFile(f.t, filepath.Join(f.run.Workspace, "parity-workspace"), selected.name+"\n")
	barrel, err := f.driver.StartBarrelInWorkspace(selected.name, f.run.Workspace)
	if err != nil {
		f.t.Fatal(err)
	}
	f.run.Objects = append(f.run.Objects, developmentObject{Type: "container", Name: barrel.Name, ID: strings.TrimSpace(f.command("docker", "inspect", "--format", "{{.Id}}", barrel.Name))})
	f.saveRun()
	writeFile(f.t, filepath.Join(selected.targets[0].hostPath, ".cooper-vm-live-cli"), "cli-live\n")
	cliOutput, err := f.driver.ExecBarrel(barrel.Name, agentParityScript(selected, agents, f.run.Workspace, "cli", account))
	if err != nil {
		f.t.Fatal(err)
	}
	if !strings.Contains(cliOutput, developmentAgentVersions[f.tool]) {
		f.t.Fatalf("prepared %s version differs from pin %s: %s", f.tool, developmentAgentVersions[f.tool], cliOutput)
	}
	if err := f.driver.StopBarrel(barrel.Name); err != nil {
		f.t.Fatal(err)
	}
	assertStateWrite(f.t, selected, "cli")
	writeAgentStateSentinels(f.t, selected)
	f.startVM()
	writeFile(f.t, filepath.Join(selected.targets[0].hostPath, ".cooper-vm-live-vm"), "vm-live\n")
	vmOutput := execVM(f.t, f.manager, f.state, agentParityScript(selected, agents, f.run.Workspace, "vm", account))
	if strings.TrimSpace(cliOutput) != strings.TrimSpace(vmOutput) {
		f.t.Fatalf("CLI/VM parity differs:\nCLI: %q\nVM: %q", cliOutput, vmOutput)
	}
	f.stopVM()
	assertStateWrite(f.t, selected, "vm")
	// Runtime cleanup sees only the private Cooper directory. The archive lease
	// remains held and every host-owned state root must survive cache cleanup.
	if err := vm.RemoveCache(f.run.CooperDir); err != nil {
		f.t.Fatal(err)
	}
	assertStateWrite(f.t, selected, "cli")
	assertStateWrite(f.t, selected, "vm")
	f.report["checks"] = []string{"same-account-and-home", "same-workspace", "same-image-version", "all-state-roots", "unselected-state-hidden", "live-host-state", "read-write-state", "cleanup-preserves-state"}
	f.report["agent_version"] = strings.TrimSpace(vmOutput)
}

func (f *developmentFixture) stopVM() {
	f.captureRuntime()
	oldToken := f.token()
	if err := f.driver.App().StopWorkload(f.state.ID); err != nil {
		f.t.Fatal(err)
	}
	assertRuntimeRemoved(f.t, f.state)
	response, _, err := f.driver.ClipboardGet("/clipboard/type", oldToken)
	if err != nil || response.StatusCode != 401 {
		f.t.Fatalf("stop did not revoke token: status=%v error=%v", responseStatus(response), err)
	}
	f.report["stop_revoked_token"] = true
}

func httpsTargetProbe(domain, proxy string) string {
	proxyArg := ""
	if proxy != "" {
		proxyArg = " -proxy " + shellQuote(proxy)
	}
	return fmt.Sprintf(`set -eu
printf 'GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n' | timeout 10s openssl s_client -quiet -ignore_unexpected_eof -verify_return_error -CAfile /tmp/target.crt -verify_hostname %s -servername %s -connect %s:443%s >/tmp/cooper-positive.out
test "$(tail -c 2 /tmp/cooper-positive.out)" = ok
`, domain, shellQuote(domain), shellQuote(domain), shellQuote(domain), proxyArg)
}
