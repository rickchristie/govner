package vm

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/vmproto"
)

func TestExecCommandCancellationAfterConnection(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "control.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- (Manager{}).ExecCommand(ctx, Runtime{ID: "test", ControlSocket: path}, []string{"sleep", "30"}, nil, false, nil, io.Discard, io.Discard)
	}()
	connection, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := vmproto.ReadHeader(connection); err != nil {
		t.Fatal(err)
	}
	if _, err := vmproto.ReadFrame(connection); err != nil {
		t.Fatal(err)
	}
	// Cancel after the request reaches the guest. DialContext alone cannot
	// interrupt the read that waits for command output or an exit status.
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled exec returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled exec kept waiting on the guest connection")
	}
}

func TestExecCommandCarriesOutputAndExitStatus(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "control.sock")
	listener, err := net.Listen("unix", path)
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
		header, _ := vmproto.ReadHeader(connection)
		if len(header.Command) != 1 || header.Command[0] != "test" {
			return
		}
		_, _ = vmproto.ReadFrame(connection)
		_ = vmproto.WriteFrame(connection, vmproto.Frame{Type: vmproto.FrameStdout, Data: []byte("out")})
		_ = vmproto.WriteFrame(connection, vmproto.Frame{Type: vmproto.FrameStderr, Data: []byte("err")})
		_ = vmproto.WriteFrame(connection, vmproto.Frame{Type: vmproto.FrameExit, Data: vmproto.EncodeExit(7)})
	}()
	manager := Manager{}
	var stdout, stderr bytes.Buffer
	err = manager.ExecCommand(context.Background(), Runtime{ID: "test", ControlSocket: path}, []string{"test"}, nil, false, bytes.NewReader(nil), &stdout, &stderr)
	if stdout.String() != "out" || stderr.String() != "err" {
		t.Fatalf("output = %q, %q", stdout.String(), stderr.String())
	}
	exit, ok := err.(ExitError)
	if !ok || exit.Status != 7 {
		t.Fatalf("error = %#v", err)
	}
}
