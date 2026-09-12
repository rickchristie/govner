package vm

import (
	"bytes"
	"context"
	"net"
	"path/filepath"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/vmproto"
)

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
