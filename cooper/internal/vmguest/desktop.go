package vmguest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/rickchristie/govner/cooper/internal/aitool"
	"github.com/rickchristie/govner/cooper/internal/vmproto"
)

func serveDesktop(ctx context.Context, stream net.Conn, manifest vmproto.Manifest) {
	defer stream.Close()
	connection, err := connectDesktop(ctx, manifest)
	if err != nil {
		_ = vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameError, Data: []byte(err.Error())})
		return
	}
	defer connection.Close()
	if err := vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameStdout, Data: []byte("ready")}); err != nil {
		return
	}
	stop := context.AfterFunc(ctx, func() { stream.Close(); connection.Close() })
	defer stop()
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(connection, stream)
		connection.Close()
		close(done)
	}()
	_, _ = io.Copy(stream, connection)
	stream.Close()
	<-done
}

func connectDesktop(ctx context.Context, manifest vmproto.Manifest) (net.Conn, error) {
	if !aitool.IsDesktop(manifest.ToolName) {
		return nil, errors.New("this VM does not have a desktop")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Only the selected container and fixed viewer port are reachable. The
	// host request cannot name an address or turn this into a guest proxy.
	output, err := dockerOutput(ctx, "inspect", "--format", `{{(index .NetworkSettings.Networks "cooper-control").IPAddress}}`, manifest.AgentContainer)
	if err != nil {
		return nil, fmt.Errorf("find guest desktop: %w", err)
	}
	address, err := desktopAddress(strings.TrimSpace(string(output)), manifest.ControlSubnet)
	if err != nil {
		return nil, err
	}
	connection, err := (&net.Dialer{}).DialContext(ctx, "tcp4", address)
	if err != nil {
		return nil, fmt.Errorf("connect guest desktop: %w", err)
	}
	return connection, nil
}

func desktopAddress(value, subnet string) (string, error) {
	ip := net.ParseIP(value)
	_, network, err := net.ParseCIDR(subnet)
	if err != nil || ip == nil || ip.To4() == nil || !network.Contains(ip) {
		return "", errors.New("guest desktop address is outside its control network")
	}
	return net.JoinHostPort(value, "6080"), nil
}
