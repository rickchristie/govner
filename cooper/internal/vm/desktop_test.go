package vm

import (
	"context"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/vmproto"
)

func TestDialDesktopHandshake(t *testing.T) {
	for _, ready := range []bool{true, false} {
		t.Run(map[bool]string{true: "ready", false: "refused"}[ready], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "control.sock")
			listener, err := net.Listen("unix", path)
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
				connection.SetDeadline(time.Now().Add(3 * time.Second))
				header, err := vmproto.ReadHeader(connection)
				if err != nil {
					done <- err
					return
				}
				if header.Service != vmproto.ServiceDesktop || len(header.Command) != 0 || len(header.Environment) != 0 {
					t.Error("desktop request changed its fixed destination")
				}
				frame := vmproto.Frame{Type: vmproto.FrameError, Data: []byte("closed")}
				if ready {
					frame = vmproto.Frame{Type: vmproto.FrameStdout, Data: []byte("ready")}
				}
				err = vmproto.WriteFrame(connection, frame)
				if ready && err == nil {
					_, err = io.Copy(connection, connection)
				}
				done <- err
			}()
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			connection, err := DialDesktop(ctx, Runtime{ToolName: "chatgpt", ControlSocket: path})
			if (err == nil) != ready {
				t.Fatalf("ready = %v, error = %v", ready, err)
			}
			if ready {
				connection.SetDeadline(time.Now().Add(time.Second))
				if _, err := connection.Write([]byte("display")); err != nil {
					t.Fatal(err)
				}
				data := make([]byte, 7)
				if _, err := io.ReadFull(connection, data); err != nil || string(data) != "display" {
					t.Fatalf("display = %q, error = %v", data, err)
				}
				connection.Close()
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
	if _, err := DialDesktop(t.Context(), Runtime{ToolName: "codex"}); err == nil {
		t.Fatal("CLI runtime accepted a desktop connection")
	}
}
