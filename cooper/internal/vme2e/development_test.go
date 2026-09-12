package vme2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/buildflow"
	"github.com/rickchristie/govner/cooper/internal/clipboard"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/runtimefs"
	"github.com/rickchristie/govner/cooper/internal/templates"
	"github.com/rickchristie/govner/cooper/internal/testdocker"
	"github.com/rickchristie/govner/cooper/internal/testdriver"
	"github.com/rickchristie/govner/cooper/internal/usercontext"
	"github.com/rickchristie/govner/cooper/internal/vm"
	"github.com/rickchristie/govner/cooper/internal/vmdev"
)

var developmentAgentVersions = vmdev.AgentVersions

type developmentManifest struct {
	Schema                           int
	Source, Binary, Platform, Daemon string
	Account                          usercontext.Account
	Config                           *config.Config
	Images                           map[string]string
	Base                             vmdev.Stamp
	BaseMetadata                     string
	Tool                             string
}

type developmentRun struct {
	Schema                                                     int
	ID, Namespace, Daemon, CooperDir, Home, Workspace, DataDir string
	// IDs and types are recorded after creation. Cleanup never expands a name
	// prefix into a removal command, and never selects the host Docker daemon.
	Objects []developmentObject
}
type developmentObject = vmdev.Object

type developmentFixture struct {
	t                           *testing.T
	ctx                         context.Context
	root, cache, prefix         string
	binary, source, daemon      string
	mode, selection, tool, home string
	manifest                    developmentManifest
	run                         developmentRun
	driver                      *testdriver.Driver
	manager                     vm.Manager
	request                     vm.StartRequest
	state                       vm.Runtime
	report                      map[string]any
	starts                      int
	loads                       int
	lifetimes                   map[string]bool
}

func TestVMDevelopment(t *testing.T) {
	mode := os.Getenv("COOPER_VM_DEV_MODE")
	if mode == "" {
		t.Skip("run cooper/test-vm-dev.sh for development VM checks")
	}
	selection := os.Getenv("COOPER_VM_DEV_SELECTION")
	valid := mode == "unit" || mode == "prepare" || mode == "smoke" || mode == "mounts" || mode == "clean" || mode == "clean-cache"
	if mode == "prepare-agent" || mode == "parity" {
		_, valid = developmentAgentVersions[selection]
	}
	if mode == "lifecycle" {
		valid = contains([]string{"restart", "resources", "relay", "agent"}, selection)
	}
	if !valid || mode == "unit" {
		t.Fatal("invalid VM development mode; use cooper/test-vm-dev.sh")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	f := &developmentFixture{t: t, ctx: ctx, root: filepath.Join(repositoryRoot(t), "cooper"), mode: mode, selection: selection, tool: vmTestTool, report: map[string]any{}, lifetimes: map[string]bool{}}
	if mode == "prepare-agent" || mode == "parity" {
		f.tool = selection
	}
	f.binary = filepath.Join(f.root, "cooper")
	f.source = f.digest()
	if f.source != os.Getenv("COOPER_VM_DEV_SOURCE") {
		t.Fatal("source changed while the test was compiled; retry the development command")
	}
	f.daemon = strings.TrimSpace(f.command("docker", "info", "--format", "{{.ID}}"))
	if f.daemon == "" {
		t.Fatal("Docker daemon identity is absent")
	}
	key := vmdev.CacheKey(f.daemon, os.Getuid())
	f.cache = filepath.Join(f.root, ".test-tmp", "vm-dev-cache", key)
	f.prefix = "cooper-vd-" + key + "-"
	lockContext, lockCancel := context.WithTimeout(ctx, 5*time.Second)
	started := time.Now()
	lease, err := vmdev.Acquire(lockContext, f.cache+".lock")
	lockCancel()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	f.report["lock_wait_seconds"] = time.Since(started).Seconds()
	defer func() {
		f.report["mode"], f.report["selection"], f.report["source_digest"] = mode, selection, f.source
		f.report["daemon"], f.report["cache"], f.report["vm_starts"], f.report["image_loads"] = f.daemon, f.cache, f.starts, f.loads
		f.report["supervisor_ids"] = f.lifetimes
		f.report["passed"] = !t.Failed()
		if err := vmdev.WriteJSON(os.Getenv("COOPER_VM_DEV_REPORT"), f.report); err != nil {
			t.Logf("optional report: %v", err)
		}
	}()
	f.claimCache()
	if mode != "clean" && mode != "clean-cache" {
		f.prefix += f.tool + "-"
	}
	f.home = filepath.Join(f.cache, "home")
	f.clearHostPaths()
	if mode == "clean" || mode == "clean-cache" {
		f.cleanRuns()
		if mode == "clean-cache" {
			f.cleanCache()
		}
		return
	}
	depth, err := vm.ManagedDepth()
	if err != nil {
		t.Fatal(err)
	}
	if depth > 2 {
		t.Fatal("development VM checks require a physical host or a depth-one Cooper VM")
	}
	if err := vm.HostRequirements(depth == 1); err != nil {
		t.Fatal(err)
	}
	f.report["invocation_depth"] = depth - 1
	if mode == "prepare" || mode == "prepare-agent" {
		f.prepare()
		return
	}
	f.requirePrepared()
	defer f.stopFixture()
	f.startFixture()
	switch mode {
	case "smoke":
		f.smoke()
	case "mounts":
		f.mounts()
	case "lifecycle":
		f.lifecycle(selection)
	case "parity":
		f.parity()
	}
	if err := ctx.Err(); err != nil {
		t.Fatal(err)
	}
	if f.digest() != f.source {
		t.Fatal("source changed during the VM test; result is stale")
	}
	binary, err := vmdev.FileDigest(f.binary)
	if err != nil || binary != f.manifest.Binary {
		t.Fatal("Cooper binary changed during the VM test; result is stale")
	}
}

func (f *developmentFixture) digest() string {
	f.t.Helper()
	digest, err := vmdev.SourceDigest(f.root)
	if err != nil {
		f.t.Fatal(err)
	}
	return digest
}
func (f *developmentFixture) command(name string, args ...string) string {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		f.t.Fatalf("%s %v: %v\n%s", name, args, err, output)
	}
	return string(output)
}
func (f *developmentFixture) claimCache() {
	owner := filepath.Join(f.cache, "owner.json")
	expected := struct {
		Schema       int
		Daemon, Root string
		UID          int
	}{1, f.daemon, f.root, os.Getuid()}
	if err := os.Mkdir(f.cache, 0700); err == nil {
		// An existing image prefix without our owner record is not ours to reuse.
		if names := strings.TrimSpace(f.command("docker", "image", "ls", "--filter", "reference="+f.prefix+"*", "--format", "{{.Repository}}")); names != "" {
			f.t.Fatalf("unowned images use development prefix %s", f.prefix)
		}
		if err := vmdev.WriteJSON(owner, expected); err != nil {
			f.t.Fatal(err)
		}
		return
	} else if !os.IsExist(err) {
		f.t.Fatal(err)
	}
	var got struct {
		Schema       int
		Daemon, Root string
		UID          int
	}
	f.readJSON(owner, &got)
	if got != expected {
		f.t.Fatal("VM development cache owner differs from this account, workspace, or Docker daemon")
	}
}
func (f *developmentFixture) clearHostPaths() {
	// HOME selects the test account path. Keep the host Go caches in place so
	// each preparation does not download and compile a second Go toolchain.
	for _, name := range []string{"GOPATH", "GOCACHE"} {
		if os.Getenv(name) == "" {
			f.t.Setenv(name, strings.TrimSpace(f.command("go", "env", name)))
		}
	}
	// State fixtures must never resolve a developer's credentials or configuration.
	clearAgentStatePaths(f.t)
	f.t.Setenv("HOME", f.home)
	f.t.Setenv("GROK_HOME", filepath.Join(f.home, "grok-state"))
	docker.SetImagePrefix(f.prefix)
}
func (f *developmentFixture) readJSON(path string, value any) {
	f.t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		f.t.Fatal(err)
	}
}
func (f *developmentFixture) manifestPath() string { return filepath.Join(f.cache, f.tool+".json") }
func (f *developmentFixture) buildDir() string     { return filepath.Join(f.cache, "build", f.tool) }
func (f *developmentFixture) store() string        { return filepath.Join(f.cache, "store") }
func (f *developmentFixture) imageID(name string) string {
	return strings.TrimSpace(f.command("docker", "image", "inspect", "--format", "{{.Id}}", name))
}
func (f *developmentFixture) prepare() {
	// Prepare and clean use the same lease as tests. A failed previous run must
	// be cleaned explicitly before its stable test home can be used again.
	f.requireNoRuns()
	started := time.Now()
	cmd := exec.CommandContext(f.ctx, "go", "build", "-C", f.root, "-o", "./cooper", ".")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		f.t.Fatal(err)
	}
	f.report["binary_build_seconds"] = time.Since(started).Seconds()
	cfg, err := vmdev.ConfigFor(f.tool)
	if err != nil {
		f.t.Fatal(err)
	}
	buildDir := f.buildDir()
	cert, key, err := config.EnsureCA(filepath.Join(f.cache, "build"))
	if err != nil {
		f.t.Fatal(err)
	}
	for _, source := range []string{cert, key} {
		writeFile(f.t, filepath.Join(buildDir, "ca", filepath.Base(source)), readFile(f.t, source))
	}
	if f.tool == vmTestTool {
		writeFile(f.t, filepath.Join(buildDir, "cli", vmTestTool, "Dockerfile"), "FROM "+docker.GetImageBase()+"\nENV COOPER_CLI_TOOL="+vmTestTool+"\nENV COOPER_CLIPBOARD_MODE=shim\n")
	}
	started = time.Now()
	if err := buildflow.Run(cfg, buildDir, buildflow.Options{Out: os.Stderr}); err != nil {
		f.t.Fatal(err)
	}
	f.report["agent_build_seconds"] = time.Since(started).Seconds()
	store := f.store()
	// An explicitly supplied base is read-only input. Its production metadata
	// and full digest must pass before a hard link or copy enters the cache.
	baseReady := vm.PreparedGuestValid(store)
	if !baseReady {
		source := os.Getenv("COOPER_VM_PREPARED_BASE")
		if source != "" {
			sourceRoot := filepath.Clean(filepath.Join(filepath.Dir(source), "../../.."))
			if source != vm.PreparedGuestPath(sourceRoot) || !vm.PreparedGuestValid(sourceRoot) {
				f.t.Fatal("supplied prepared VM base failed production validation")
			}
			target := vm.PreparedGuestPath(store)
			for _, suffix := range []string{"", ".json"} {
				if err := os.Remove(target + suffix); err != nil && !os.IsNotExist(err) {
					f.t.Fatal(err)
				}
				method, err := vmdev.Stage(source+suffix, target+suffix)
				if err != nil {
					f.t.Fatal(err)
				}
				f.report["base_stage"] = method
			}
			baseReady = true
		}
	}
	// Runtime counts track workload VMs. A cold base needs a separate guest;
	// keep that cost visible and leave it unknown if preparation fails.
	f.report["base_cache_hit"] = baseReady
	f.report["preparation_vm_starts"] = 0
	if !baseReady {
		f.report["preparation_vm_starts"] = nil
	}
	started = time.Now()
	base, err := (vm.Preparer{CooperDir: store, Prefix: f.prefix, Executable: f.binary, Out: os.Stderr}).Prepare(f.ctx)
	if err != nil {
		f.t.Fatal(err)
	}
	if !baseReady {
		f.report["preparation_vm_starts"] = 1
	}
	f.report["base_and_helpers_seconds"] = time.Since(started).Seconds()
	imageRef := docker.GetImageCLI(f.tool)
	id := f.imageID(imageRef)
	_, cacheErr := vm.ReadImageArchive(store, id)
	archive, err := vm.EnsureImageArchive(f.ctx, store, imageRef, nil, os.Stderr)
	if err != nil {
		f.t.Fatal(err)
	}
	f.report["image_exports"] = 0
	if cacheErr != nil {
		f.report["image_exports"] = 1
	}
	info, err := os.Stat(archive.Path)
	if err != nil {
		f.t.Fatal(err)
	}
	f.report["archive_bytes"] = info.Size()
	account, err := usercontext.Current()
	if err != nil {
		f.t.Fatal(err)
	}
	manifest := developmentManifest{Schema: 1, Source: f.source, Platform: runtime.GOOS + "/" + runtime.GOARCH, Daemon: f.daemon, Account: account, Config: cfg, Images: map[string]string{}, Tool: f.tool}
	manifest.Binary, err = vmdev.FileDigest(f.binary)
	if err != nil {
		f.t.Fatal(err)
	}
	manifest.Base, err = vmdev.FileStamp(base)
	if err != nil {
		f.t.Fatal(err)
	}
	manifest.BaseMetadata, err = vmdev.FileDigest(base + ".json")
	if err != nil {
		f.t.Fatal(err)
	}
	for _, image := range []string{imageRef, docker.GetImageProxy(), docker.GetImageBase(), vm.SupervisorImageName(f.prefix), vm.RelayImageName(f.prefix)} {
		manifest.Images[image] = f.imageID(image)
	}
	if err := account.CheckLabel(strings.TrimSpace(f.command("docker", "image", "inspect", "--format", `{{index .Config.Labels "cooper.account"}}`, id))); err != nil {
		f.t.Fatal(err)
	}
	f.report["tool_inventory"] = f.command("docker", "run", "--rm", "--network", "none", "--entrypoint", "sh", id, "-ec", `for tool in node curl jq socat xclip; do command -v "$tool"; done; cat /etc/cooper/account.json`)
	if f.digest() != f.source {
		f.t.Fatal("source changed during preparation; cache was not published")
	}
	if err := vmdev.WriteJSON(f.manifestPath(), manifest); err != nil {
		f.t.Fatal(err)
	}
	f.report["binary_digest"], f.report["images"] = manifest.Binary, manifest.Images
	f.t.Logf("prepared %s; normal VM test commands will not build or export", f.tool)
}
func (f *developmentFixture) prepareHint() string {
	if f.tool == vmTestTool {
		return "./cooper/test-vm-dev.sh prepare"
	}
	return "./cooper/test-vm-dev.sh prepare-agent " + f.tool
}
func (f *developmentFixture) requirePrepared() {
	data, err := os.ReadFile(f.manifestPath())
	if err != nil {
		f.t.Fatalf("prepared inputs are absent; run %s", f.prepareHint())
	}
	if err := json.Unmarshal(data, &f.manifest); err != nil {
		f.t.Fatalf("invalid prepared inputs; run %s: %v", f.prepareHint(), err)
	}
	m := f.manifest
	account, err := usercontext.Current()
	if err != nil {
		f.t.Fatal(err)
	}
	binary, err := vmdev.FileDigest(f.binary)
	if err != nil || m.Schema != 1 || m.Source != f.source || m.Binary != binary || m.Platform != runtime.GOOS+"/"+runtime.GOARCH || m.Daemon != f.daemon || m.Account != account || m.Tool != f.tool {
		f.t.Fatalf("prepared inputs are stale; run %s", f.prepareHint())
	}
	base := vm.PreparedGuestPath(f.store())
	stamp, err := vmdev.FileStamp(base)
	if err != nil || stamp != m.Base {
		f.t.Fatalf("prepared base changed; run %s", f.prepareHint())
	}
	metadata, err := vmdev.FileDigest(base + ".json")
	if err != nil || metadata != m.BaseMetadata {
		f.t.Fatalf("prepared base metadata changed; run %s", f.prepareHint())
	}
	for image, id := range m.Images {
		if got := f.imageID(image); got != id {
			f.t.Fatalf("prepared image %s changed; run %s", image, f.prepareHint())
		}
	}
	for _, image := range []string{vm.SupervisorImageName(f.prefix), vm.RelayImageName(f.prefix)} {
		got := strings.TrimSpace(f.command("docker", "image", "inspect", "--format", `{{index .Config.Labels "cooper.vm.binary-sha"}}`, image))
		if got != m.Binary {
			f.t.Fatalf("helper image does not contain the current binary; run %s", f.prepareHint())
		}
	}
	archive, err := vm.ReadImageArchive(f.store(), m.Images[docker.GetImageCLI(f.tool)])
	if err != nil {
		f.t.Fatalf("%v; run %s", err, f.prepareHint())
	}
	info, err := os.Stat(archive.Path)
	if err != nil {
		f.t.Fatal(err)
	}
	f.report["archive_bytes"], f.report["image_exports"], f.report["cache_hit"] = info.Size(), 0, true
	f.report["binary_digest"], f.report["images"] = m.Binary, m.Images
	f.manager.PreparedArchive = &archive
}
func (f *developmentFixture) requireNoRuns() {
	entries, err := os.ReadDir(filepath.Join(f.cache, "runs"))
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		f.t.Fatal(err)
	}
	if len(entries) > 0 {
		f.t.Fatal("an unfinished VM development run exists; run ./cooper/test-vm-dev.sh clean")
	}
}
func (f *developmentFixture) startFixture() {
	f.requireNoRuns()
	id := os.Getenv("COOPER_VM_DEV_RUN")
	if len(id) != 12 || strings.Trim(id, "0123456789abcdef") != "" {
		f.t.Fatal("invalid development run identity")
	}
	root, err := os.MkdirTemp("/tmp", "cvd-"+id+"-")
	if err != nil {
		f.t.Fatal(err)
	}
	dataDir := filepath.Join(f.root, ".test-tmp", "vm-dev-runs", id)
	if err := os.MkdirAll(filepath.Dir(dataDir), 0700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Mkdir(dataDir, 0700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dataDir, "c"), 0700); err != nil {
		f.t.Fatal(err)
	}
	// Keep archives and runtime files on the same filesystem so archive staging
	// uses a hard link. Only the socket path needs a short /tmp alias.
	if err := os.Symlink(filepath.Join(dataDir, "c"), filepath.Join(root, "c")); err != nil {
		f.t.Fatal(err)
	}
	f.run = developmentRun{Schema: 1, ID: id, Namespace: "cvd-" + id, Daemon: f.daemon, CooperDir: filepath.Join(root, "c"), Home: f.home, Workspace: filepath.Join(dataDir, "w"), DataDir: dataDir}
	// Register cleanup before the first Docker resource. Errors keep this record
	// and the logs; the explicit clean command verifies it against this cache.
	if err := os.MkdirAll(filepath.Join(f.cache, "runs"), 0700); err != nil {
		f.t.Fatal(err)
	}
	f.saveRun()
	docker.SetRuntimeNamespace(f.run.Namespace)
	for _, name := range []string{docker.ProxyContainerName(), docker.ExternalNetworkName(), docker.InternalNetworkName()} {
		out, err := exec.CommandContext(f.ctx, "docker", "inspect", name).CombinedOutput()
		if err == nil || !vmdev.ObjectMissing(out) {
			f.t.Fatalf("development runtime name is not free: %s: %s", name, out)
		}
	}
	if err := os.MkdirAll(f.home, 0700); err != nil {
		f.t.Fatal(err)
	}
	writeFile(f.t, filepath.Join(f.home, ".gitconfig"), "[user]\n name = Cooper VM Development\n")
	writeFile(f.t, filepath.Join(f.run.Workspace, "workspace-sentinel"), "workspace-ok\n")
	writeFile(f.t, filepath.Join(f.run.Workspace, ".git/hooks/kept"), "hook-ok\n")
	cfg := f.manifest.Config
	if err := testdocker.AssignDynamicPorts(cfg); err != nil {
		f.t.Fatal(err)
	}
	for _, relative := range []string{"ca/cooper-ca.pem", "ca/cooper-ca-key.pem"} {
		// CA keys belong to this fixture, not to the host account.
		source := filepath.Join(f.buildDir(), relative)
		data, err := os.ReadFile(source)
		if err != nil {
			f.t.Fatal(err)
		}
		writeFile(f.t, filepath.Join(f.run.CooperDir, relative), string(data))
	}
	if err := templates.WriteAllTemplates(filepath.Join(f.run.CooperDir, "base"), filepath.Join(f.run.CooperDir, "cli"), cfg, nil); err != nil {
		f.t.Fatal(err)
	}
	if err := templates.WriteProxyTemplates(filepath.Join(f.run.CooperDir, "proxy"), cfg); err != nil {
		f.t.Fatal(err)
	}
	// These two origins live in this fixture's private Docker network. A parent
	// proxy outside the current VM cannot resolve that network's DNS aliases.
	// Keep only these test origins local; all other traffic keeps the normal
	// parent route. The full parent chain is tested by the release gate.
	squidPath := filepath.Join(f.run.CooperDir, "proxy", "squid.conf")
	squid := readFile(f.t, squidPath)
	if strings.Contains(squid, "never_direct allow all") {
		squid = "acl vm_development_origin dstdomain vm-allowed.cooper.test vm-blocked.cooper.test\nalways_direct allow vm_development_origin\n" + strings.Replace(squid, "never_direct allow all", "never_direct deny vm_development_origin\nnever_direct allow all", 1)
		writeFile(f.t, squidPath, squid)
		f.report["local_fixture_route"] = "two private Docker DNS aliases; other traffic uses the parent proxy"
	}
	if err := config.SaveConfig(filepath.Join(f.run.CooperDir, "config.json"), cfg); err != nil {
		f.t.Fatal(err)
	}
	f.driver = testdriver.AttachPrepared(cfg, f.run.CooperDir, f.home)
	prepared := f.manager.PreparedArchive
	f.manager = vm.Manager{CooperDir: f.run.CooperDir, HomeDir: f.home, Namespace: f.run.Namespace, ImagePrefix: f.prefix, ProxyName: docker.ProxyContainerName(), Config: cfg, Out: os.Stderr, Executable: f.binary, SkipPrepare: true, PreparedBase: vm.PreparedGuestPath(f.store()), PreparedArchive: prepared}
	f.driver.App().AdoptVMManager(f.manager)
	if err := f.driver.Start(f.ctx); err != nil {
		f.recordInfrastructure()
		f.t.Fatal(err)
	}
	f.recordInfrastructure()
	f.report["run_directory"], f.report["resources"] = f.run.DataDir, cfg.VM
}
func (f *developmentFixture) saveRun() {
	if err := vmdev.WriteJSON(filepath.Join(f.cache, "runs", f.run.ID+".json"), f.run); err != nil {
		f.t.Fatal(err)
	}
}
func (f *developmentFixture) recordInfrastructure() {
	for _, object := range []developmentObject{{Type: "container", Name: docker.ProxyContainerName()}, {Type: "network", Name: docker.ExternalNetworkName()}, {Type: "network", Name: docker.InternalNetworkName()}} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, err := exec.CommandContext(ctx, "docker", object.Type, "inspect", "--format", "{{.Id}}", object.Name).CombinedOutput()
		cancel()
		if err != nil {
			continue
		}
		object.ID = strings.TrimSpace(string(out))
		f.run.Objects = append(f.run.Objects, object)
	}
	f.saveRun()
}
func (f *developmentFixture) startVM() {
	started := time.Now()
	id, err := vm.RuntimeID(f.run.Namespace, f.run.Workspace, f.tool)
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := runtimefs.SyncTimezoneFile(f.run.CooperDir, id); err != nil {
		f.t.Fatal(err)
	}
	mode := "shim"
	if f.tool != vmTestTool {
		mode, err = docker.ToolClipboardMode(f.tool)
		if err != nil {
			f.t.Fatal(err)
		}
	}
	f.request = vm.StartRequest{RuntimeID: id, WorkspaceDir: f.run.Workspace, ToolName: f.tool, ImageRef: docker.GetImageCLI(f.tool), ClipboardMode: mode, CPUs: f.manifest.Config.VM.CPUs, MemoryMiB: f.manifest.Config.VM.MemoryMiB, DiskGiB: f.manifest.Config.VM.DiskGiB}
	f.state, err = f.manager.Start(f.ctx, f.request)
	if err != nil {
		// A failed boot can still create a VM and import an image. Keep its
		// diagnostics and observed work before the fixture removes the runtime.
		f.state = vm.Runtime{ID: id, ContainerName: id, RuntimeDir: vm.RuntimeDir(f.run.CooperDir, id), ControlDir: vm.ControlDir(f.run.CooperDir, id)}
		if _, inspectErr := exec.CommandContext(f.ctx, "docker", "inspect", id).Output(); inspectErr == nil {
			f.observeLifetime()
		}
		f.captureRuntime()
		f.t.Fatalf("start VM: %v; logs: %s", err, vm.RuntimeDir(f.run.CooperDir, id))
	}
	f.observeLifetime()
	f.report["first_start_seconds"] = time.Since(started).Seconds()
}
func (f *developmentFixture) observeLifetime() {
	identity := strings.TrimSpace(f.command("docker", "inspect", "--format", "{{.Id}}", f.state.ContainerName))
	if f.lifetimes[identity] {
		return
	}
	f.lifetimes[identity] = true
	f.starts++
	data, err := os.ReadFile(filepath.Join(vm.SupervisorRuntimeDir(f.state.RuntimeDir), "guest.log"))
	if err == nil {
		f.loads += strings.Count(string(data), "agent image load finished")
	}
	// Keep the complete logs before a restart deletes this disk and log tree.
	f.captureRuntime()
}
func (f *developmentFixture) captureRuntime() {
	if f.state.ID == "" {
		return
	}
	destination := filepath.Join(f.run.DataDir, fmt.Sprintf("lifetime-%d", f.starts))
	os.MkdirAll(destination, 0700)
	for _, root := range []string{f.state.ControlDir, vm.SupervisorRuntimeDir(f.state.RuntimeDir)} {
		filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".log") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err == nil {
				os.WriteFile(filepath.Join(destination, entry.Name()), data, 0600)
			}
			return nil
		})
	}
	disk := vm.RuntimeDiskPath(f.run.CooperDir, f.state.ID)
	if info, err := os.Stat(disk); err == nil {
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			f.report[fmt.Sprintf("disk_%d", f.starts)] = map[string]int64{"logical_bytes": info.Size(), "allocated_bytes": stat.Blocks * 512}
		}
	}
	archive := filepath.Join(f.state.RuntimeDir, "exports", "image", "agent.tar")
	if left, err := os.Stat(archive); err == nil {
		if right, err := os.Stat(f.manager.PreparedArchive.Path); err == nil {
			f.report["archive_stage_hardlink"] = os.SameFile(left, right)
		}
	}
}
func (f *developmentFixture) stopFixture() {
	if f.run.ID == "" {
		return
	}
	f.captureRuntime()
	for _, object := range f.run.Objects {
		if object.Type != "container" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		data, err := exec.CommandContext(ctx, "docker", "logs", "--tail", "1000", object.ID).CombinedOutput()
		cancel()
		if err == nil {
			_ = os.WriteFile(filepath.Join(f.run.DataDir, object.Name+".log"), data, 0600)
		}
	}
	started := time.Now()
	if err := f.checkRecordedObjects(f.run); err != nil {
		f.t.Errorf("cleanup ownership: %v", err)
		return
	}
	if f.driver != nil {
		// Do not run broad cleanup or recursive ownership repair over an archive
		// cache. The app and VM manager stop their own exact runtime identities.
		if err := f.driver.App().Stop(); err != nil {
			f.t.Errorf("stop development app: %v", err)
			return
		}
		f.driver = nil
	}
	if err := f.removeRecordedObjects(f.run); err != nil {
		f.t.Errorf("cleanup: %v", err)
		return
	}
	if err := os.Remove(filepath.Join(f.cache, "runs", f.run.ID+".json")); err != nil {
		f.t.Error(err)
		return
	}
	// Keep logs, reports, and the small fixture after success. Each next run gets
	// a new directory and the old writable VM disk has already been removed.
	f.report["cleanup_seconds"] = time.Since(started).Seconds()
	f.report["runtime_removed"] = true
	f.run.ID = ""
	if err := os.RemoveAll(f.home); err != nil {
		f.t.Errorf("remove test home: %v", err)
	}
}
func developmentDockerOutput(ctx context.Context, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, "docker", args...).CombinedOutput()
}
func (f *developmentFixture) removeRecordedObjects(run developmentRun) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return vmdev.RemoveObjects(ctx, run.Namespace, run.Objects, developmentDockerOutput)
}
func (f *developmentFixture) checkRecordedObjects(run developmentRun) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return vmdev.CheckObjects(ctx, run.Namespace, run.Objects, developmentDockerOutput)
}
func (f *developmentFixture) cleanRuns() {
	entries, err := os.ReadDir(filepath.Join(f.cache, "runs"))
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		f.t.Fatal(err)
	}
	for _, entry := range entries {
		var run developmentRun
		f.readJSON(filepath.Join(f.cache, "runs", entry.Name()), &run)
		root := filepath.Dir(run.CooperDir)
		if run.Schema != 1 || len(run.ID) != 12 || strings.Trim(run.ID, "0123456789abcdef") != "" || entry.Name() != run.ID+".json" || run.Daemon != f.daemon || run.Namespace != "cvd-"+run.ID || run.Home != f.home || filepath.Dir(root) != "/tmp" || !strings.HasPrefix(filepath.Base(root), "cvd-"+run.ID+"-") || filepath.Base(run.CooperDir) != "c" || run.DataDir != filepath.Join(f.root, ".test-tmp", "vm-dev-runs", run.ID) || run.Workspace != filepath.Join(run.DataDir, "w") {
			f.t.Fatal("refuse cleanup of invalid run record")
		}
		link, err := os.Readlink(run.CooperDir)
		if err != nil || link != filepath.Join(run.DataDir, "c") {
			f.t.Fatal("refuse cleanup of a replaced runtime alias")
		}
		cfg := config.DefaultConfig()
		cfg.VM.StopTimeoutS = 1
		if err := f.checkRecordedObjects(run); err != nil {
			f.t.Fatal(err)
		}
		manager := vm.Manager{CooperDir: run.CooperDir, Namespace: run.Namespace, Config: cfg, ProxyName: run.Namespace + "-proxy"}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = manager.StopAll(ctx)
		cancel()
		if err != nil {
			f.t.Fatal(err)
		}
		if err := f.removeRecordedObjects(run); err != nil {
			f.t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(f.cache, "runs", entry.Name())); err != nil {
			f.t.Fatal(err)
		}
		f.t.Logf("cleaned interrupted run %s; logs remain in %s", run.ID, root)
	}
	if err := os.RemoveAll(f.home); err != nil {
		f.t.Fatal(err)
	}
}
func (f *developmentFixture) cleanCache() {
	// Remove only references and files recorded by this owner. Docker refuses an
	// image removal if a container still uses it; never force image deletion.
	entries, err := os.ReadDir(f.cache)
	if err != nil {
		f.t.Fatal(err)
	}
	images := map[string]string{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") || entry.Name() == "owner.json" {
			continue
		}
		var m developmentManifest
		f.readJSON(filepath.Join(f.cache, entry.Name()), &m)
		if m.Schema != 1 || m.Daemon != f.daemon {
			f.t.Fatal("invalid cache manifest")
		}
		for name, id := range m.Images {
			if !strings.HasPrefix(name, f.prefix) {
				f.t.Fatal("unowned image in cache manifest")
			}
			images[name] = id
		}
	}
	for name, id := range images {
		if f.imageID(name) != id {
			f.t.Fatalf("refuse cleanup of changed image %s", name)
		}
	}
	for name := range images {
		f.command("docker", "image", "rm", name)
	}
	for _, entry := range entries {
		if entry.Name() == "owner.json" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(f.cache, entry.Name())); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *developmentFixture) token() string {
	metadata, err := clipboard.ReadTokenMetadata(clipboard.TokenFilePath(f.run.CooperDir, f.state.ID))
	if err != nil {
		f.t.Fatal(err)
	}
	return metadata.Token
}
