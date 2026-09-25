package vmguest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hashicorp/yamux"

	"github.com/rickchristie/govner/cooper/internal/vmproto"
)

// Run loads the host manifest, proves the no-NIC boundary, and starts the
// selected agent in the guest Docker daemon.
func Run(ctx context.Context, manifestPath string, log io.Writer) (returnErr error) {
	if kernelLog, err := os.OpenFile("/dev/kmsg", os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
		defer kernelLog.Close()
		log = io.MultiWriter(log, kernelLog)
	}
	fmt.Fprintln(log, "Cooper VM guest: loading manifest")
	manifest, err := loadManifest(manifestPath)
	if err != nil {
		return err
	}
	fmt.Fprintln(log, "Cooper VM guest: verifying no-NIC boot")
	if err := assertBootNetwork(); err != nil {
		return err
	}
	fmt.Fprintln(log, "Cooper VM guest: verifying managed depth")
	if err := assertDepth(manifest.Depth); err != nil {
		return err
	}
	fmt.Fprintln(log, "Cooper VM guest: opening private control channel")
	device, err := os.OpenFile("/dev/virtio-ports/org.cooper.gateway", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open Cooper VM gateway: %w", err)
	}
	defer device.Close()
	session, err := yamux.Client(fileConnection{device}, muxConfig())
	if err != nil {
		return fmt.Errorf("start Cooper VM guest multiplexer: %w", err)
	}
	defer func() {
		if returnErr != nil {
			sendDiagnostic(session, manifest.Nonce, returnErr)
		}
		session.Close()
	}()
	fmt.Fprintln(log, "Cooper VM guest: growing the disposable root disk")
	if err := growRootDisk(ctx, log); err != nil {
		return err
	}
	fmt.Fprintln(log, "Cooper VM guest: mounting host exports")
	if err := mountExports(manifest, log); err != nil {
		return err
	}

	runtimeContext, cancel := context.WithCancel(ctx)
	defer cancel()
	docker := &dockerRuntime{manifest: manifest}
	fmt.Fprintln(log, "Cooper VM guest: starting private Docker daemon")
	if err := docker.start(runtimeContext, log); err != nil {
		return err
	}
	defer docker.stop()
	fmt.Fprintln(log, "Cooper VM guest: starting approved network relay")
	relay := newLocalRelay(session, manifest)
	relayDone := make(chan error, 1)
	go func() { relayDone <- relay.serve(runtimeContext) }()
	if err := waitForRelay(manifest.ControlGateway, manifest.ProxyPort); err != nil {
		return err
	}
	fmt.Fprintln(log, "Cooper VM guest: loading and starting agent image")
	if err := docker.loadAndStartAgent(runtimeContext, log); err != nil {
		return err
	}
	defer docker.stopAgent(context.Background())
	fmt.Fprintln(log, "Cooper VM guest: ready")
	if err := sendReady(session, manifest.Nonce); err != nil {
		return err
	}

	shutdown := make(chan struct{})
	acceptDone := make(chan error, 1)
	go func() { acceptDone <- acceptHostStreams(runtimeContext, session, manifest, relay, docker, shutdown) }()
	select {
	case <-ctx.Done():
		return nil
	case <-shutdown:
		docker.stopAgent(context.Background())
		docker.stop()
		go func() {
			time.Sleep(250 * time.Millisecond)
			_ = exec.Command("systemctl", "poweroff", "--no-block").Run()
		}()
		return nil
	case err := <-relayDone:
		return err
	case err := <-acceptDone:
		return err
	}
}

func sendDiagnostic(session *yamux.Session, nonce string, failure error) {
	stream, err := session.OpenStream()
	if err != nil {
		return
	}
	defer stream.Close()
	header := vmproto.NewHeader(vmproto.ServiceDiagnostic, "guest-failure")
	header.Nonce = nonce
	if vmproto.WriteHeader(stream, header) != nil {
		return
	}
	if vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameError, Data: []byte(failure.Error())}) != nil {
		return
	}
	// Wait until the host confirms that it saved the error. Without this
	// handshake, closing the yamux session can cancel the host handler before
	// guest-error.log reaches the host filesystem.
	_, _ = vmproto.ReadFrame(stream)
}

func loadManifest(path string) (vmproto.Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return vmproto.Manifest{}, fmt.Errorf("read Cooper VM manifest: %w", err)
	}
	var manifest vmproto.Manifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return vmproto.Manifest{}, fmt.Errorf("decode Cooper VM manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("unexpected data after JSON object")
		}
		return vmproto.Manifest{}, fmt.Errorf("decode Cooper VM manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return vmproto.Manifest{}, err
	}
	return manifest, nil
}

func assertBootNetwork() error {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return fmt.Errorf("inspect guest network interfaces: %w", err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	if len(names) != 1 || names[0] != "lo" {
		return fmt.Errorf("cooper VM boot has unexpected network interfaces: %s", strings.Join(names, ", "))
	}
	return nil
}

func assertDepth(depth int) error {
	cpuInfo, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return err
	}
	flags := string(cpuInfo)
	if depth == 2 {
		if hasCPUFlag(flags, "svm") || hasCPUFlag(flags, "vmx") {
			return errors.New("depth-2 Cooper VM exposes nested virtualization")
		}
		if _, err := os.Stat("/dev/kvm"); err == nil {
			return errors.New("depth-2 Cooper VM exposes /dev/kvm")
		}
		return nil
	}
	module := "kvm_amd"
	if strings.Contains(flags, "GenuineIntel") {
		module = "kvm_intel"
	}
	_ = exec.Command("modprobe", module).Run()
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return fmt.Errorf("depth-1 Cooper VM cannot access nested KVM: %w", err)
	}
	return nil
}

func hasCPUFlag(cpuInfo, flag string) bool {
	for _, line := range strings.Split(cpuInfo, "\n") {
		if !strings.HasPrefix(line, "flags") {
			continue
		}
		for _, value := range strings.Fields(line) {
			if value == flag {
				return true
			}
		}
	}
	return false
}

func waitForRelay(host string, port int) error {
	address := net.JoinHostPort(host, strconv.Itoa(port))
	for attempt := 0; attempt < 100; attempt++ {
		connection, err := net.DialTimeout("tcp4", address, 100*time.Millisecond)
		if err == nil {
			connection.Close()
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("guest proxy relay %s did not become ready", address)
}

func sendReady(session *yamux.Session, nonce string) error {
	stream, err := session.OpenStream()
	if err != nil {
		return err
	}
	defer stream.Close()
	header := vmproto.NewHeader(vmproto.ServiceHello, "guest-ready")
	header.Nonce = nonce
	if err := vmproto.WriteHeader(stream, header); err != nil {
		return err
	}
	frame, err := vmproto.ReadFrame(stream)
	if err != nil {
		return err
	}
	if frame.Type != vmproto.FrameStdout || string(frame.Data) != "ready" {
		return errors.New("VM host rejected guest readiness")
	}
	return nil
}

func acceptHostStreams(ctx context.Context, session *yamux.Session, manifest vmproto.Manifest, relay *localRelay, docker *dockerRuntime, shutdown chan<- struct{}) error {
	var once sync.Once
	for {
		stream, err := session.AcceptStream()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept VM host stream: %w", err)
		}
		header, err := vmproto.ReadHeader(stream)
		if err != nil {
			stream.Close()
			continue
		}
		if header.Nonce != manifest.Nonce {
			stream.Close()
			continue
		}
		switch header.Service {
		case vmproto.ServiceHealth:
			if docker.agentHealthy(ctx) {
				_ = vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameStdout, Data: []byte("ready")})
				_ = vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameExit, Data: vmproto.EncodeExit(0)})
			} else {
				_ = vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameError, Data: []byte("VM agent container is not running")})
				_ = vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameExit, Data: vmproto.EncodeExit(1)})
			}
			stream.Close()
		case vmproto.ServiceExec:
			go runExecStream(ctx, stream, manifest, header)
		case vmproto.ServiceDesktop:
			go serveDesktop(ctx, stream, manifest)
		case vmproto.ServiceReload:
			if err := relay.reload(header.ForwardPorts); err != nil {
				_ = vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameError, Data: []byte(err.Error())})
				_ = vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameExit, Data: vmproto.EncodeExit(1)})
			} else if err := docker.reloadAgentEntrypoint(ctx); err != nil {
				_ = vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameError, Data: []byte(err.Error())})
				_ = vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameExit, Data: vmproto.EncodeExit(1)})
			} else {
				_ = vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameExit, Data: vmproto.EncodeExit(0)})
			}
			stream.Close()
		case vmproto.ServiceDoctor:
			diagnostic, err := guestDiagnostic(ctx, manifest)
			if err != nil {
				_ = vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameError, Data: []byte(err.Error())})
				_ = vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameExit, Data: vmproto.EncodeExit(1)})
			} else {
				data, _ := json.Marshal(diagnostic)
				_ = vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameStdout, Data: data})
				_ = vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameExit, Data: vmproto.EncodeExit(0)})
			}
			stream.Close()
		case vmproto.ServiceShutdown:
			_ = vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameExit, Data: vmproto.EncodeExit(0)})
			stream.Close()
			once.Do(func() { close(shutdown) })
		default:
			stream.Close()
		}
	}
}

func guestDiagnostic(ctx context.Context, manifest vmproto.Manifest) (vmproto.GuestDiagnostic, error) {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return vmproto.GuestDiagnostic{}, fmt.Errorf("inspect guest network interfaces: %w", err)
	}
	interfaces := make([]string, 0, len(entries))
	externalInterfaces := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		interfaces = append(interfaces, name)
		if _, err := os.Stat(filepath.Join("/sys/class/net", name, "device")); err == nil {
			externalInterfaces = append(externalInterfaces, name)
		} else if !errors.Is(err, os.ErrNotExist) {
			return vmproto.GuestDiagnostic{}, fmt.Errorf("inspect guest network interface %s: %w", name, err)
		}
	}
	sort.Strings(interfaces)
	sort.Strings(externalInterfaces)
	defaultRoutes, err := guestDefaultRoutes()
	if err != nil {
		return vmproto.GuestDiagnostic{}, err
	}
	version, err := dockerOutput(ctx, "version", "--format", "{{.Server.Version}}")
	if err != nil {
		return vmproto.GuestDiagnostic{}, fmt.Errorf("inspect guest Docker: %w", err)
	}
	_, kvmErr := os.Stat("/dev/kvm")
	return vmproto.GuestDiagnostic{
		Schema: vmproto.GuestDiagnosticSchema, Depth: manifest.Depth,
		Interfaces: interfaces, ExternalInterfaces: externalInterfaces, DefaultRoutes: defaultRoutes,
		DockerVersion: strings.TrimSpace(string(version)),
		KVMAvailable:  kvmErr == nil,
	}, nil
}

func guestDefaultRoutes() ([]string, error) {
	ipv4, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return nil, fmt.Errorf("inspect guest IPv4 routes: %w", err)
	}
	ipv6, err := os.ReadFile("/proc/net/ipv6_route")
	if err != nil {
		return nil, fmt.Errorf("inspect guest IPv6 routes: %w", err)
	}
	return defaultRouteInterfaces(ipv4, ipv6), nil
}

func defaultRouteInterfaces(ipv4, ipv6 []byte) []string {
	var routes []string
	for index, line := range strings.Split(string(ipv4), "\n") {
		fields := strings.Fields(line)
		if index == 0 || len(fields) < 2 || fields[0] == "lo" || fields[1] != "00000000" {
			continue
		}
		routes = append(routes, fields[0])
	}
	for _, line := range strings.Split(string(ipv6), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 || fields[len(fields)-1] == "lo" || fields[0] != strings.Repeat("0", 32) || fields[1] != "00" {
			continue
		}
		routes = append(routes, fields[len(fields)-1])
	}
	sort.Strings(routes)
	return routes
}

func muxConfig() *yamux.Config {
	config := yamux.DefaultConfig()
	config.AcceptBacklog = 64
	config.EnableKeepAlive = false
	config.MaxStreamWindowSize = 256 * 1024
	config.KeepAliveInterval = 15 * time.Second
	config.ConnectionWriteTimeout = 15 * time.Second
	config.StreamOpenTimeout = 15 * time.Second
	config.StreamCloseTimeout = 30 * time.Second
	config.LogOutput = io.Discard
	return config
}
