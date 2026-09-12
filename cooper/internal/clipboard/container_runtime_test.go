package clipboard

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/vmproto"
	"github.com/rickchristie/govner/cooper/internal/vmstate"
)

func TestVMControlHealthyRequiresGuestAgentReady(t *testing.T) {
	cooperDir := t.TempDir()
	runtimeID := "cooper-vm-project-codex-123456789abc"
	socket := vmstate.ControlSocketPath(cooperDir, runtimeID)
	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	done := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer connection.Close()
		header, err := vmproto.ReadHeader(connection)
		if err == nil && header.Service != vmproto.ServiceHealth {
			err = os.ErrInvalid
		}
		if err == nil {
			err = vmproto.WriteFrame(connection, vmproto.Frame{Type: vmproto.FrameStdout, Data: []byte("ready")})
		}
		done <- err
	}()

	if !vmControlHealthy(cooperDir, runtimeID) {
		t.Fatal("vmControlHealthy() rejected a ready guest agent")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("health test server did not finish")
	}
}

func TestVMControlHealthyRejectsMissingOrUnreadyGuest(t *testing.T) {
	cooperDir := t.TempDir()
	runtimeID := "cooper-vm-project-codex-123456789abc"
	if vmControlHealthy(cooperDir, runtimeID) {
		t.Fatal("vmControlHealthy() accepted a missing control socket")
	}

	socket := vmstate.ControlSocketPath(cooperDir, runtimeID)
	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		_, _ = vmproto.ReadHeader(connection)
		_ = vmproto.WriteFrame(connection, vmproto.Frame{Type: vmproto.FrameError, Data: []byte("starting")})
	}()
	if vmControlHealthy(cooperDir, runtimeID) {
		t.Fatal("vmControlHealthy() accepted an unready guest agent")
	}
}
