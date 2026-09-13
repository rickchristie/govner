package vm

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/vmproto"
)

// Health checks share Docker inspection results, but must have independent
// control requests when a status update overlaps a launch.
type healthRunner struct{}

func (healthRunner) Run(context.Context, io.Reader, io.Writer, io.Writer, string, ...string) error {
	return errors.New("unexpected command")
}

func (healthRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	if name != "docker" || !strings.Contains(strings.Join(args, " "), ".State.Running") {
		return nil, errors.New("unexpected inspection")
	}
	return []byte("true\n"), nil
}

func healthSocket(t *testing.T) (net.Listener, Runtime) {
	t.Helper()
	// Unix socket names have a small limit. Test names must not consume it.
	dir, err := os.MkdirTemp("", "cooper-health-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	runtime := Runtime{ID: "test", ContainerName: "test-vm", RelayName: "test-relay", ControlDir: dir, RuntimeDir: dir, ControlSocket: filepath.Join(dir, "control.sock")}
	listener, err := net.Listen("unix", runtime.ControlSocket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	return listener, runtime
}

func TestHealthyConcurrentRequests(t *testing.T) {
	listener, runtime := healthSocket(t)
	manager := Manager{Runner: healthRunner{}, ProxyName: "test-proxy"}
	var active vmproto.ActiveRequests
	// Apply the same active-request rule as the supervisor. Hold both requests
	// until their headers arrive so the checks must overlap.
	go func() {
		var connections []net.Conn
		var accepted []bool
		for range 2 {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			defer connection.Close()
			_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
			header, err := vmproto.ReadHeader(connection)
			if err != nil {
				return
			}
			connections = append(connections, connection)
			accepted = append(accepted, active.Acquire(header.RequestID))
		}
		for index, connection := range connections {
			frame := vmproto.Frame{Type: vmproto.FrameStdout, Data: []byte("ready")}
			if !accepted[index] {
				frame = vmproto.Frame{Type: vmproto.FrameError, Data: []byte("duplicate request")}
			}
			_ = vmproto.WriteFrame(connection, frame)
		}
	}()
	results := make(chan bool, 2)
	for range 2 {
		go func() {
			healthy, err := manager.Healthy(runtime)
			results <- healthy && err == nil
		}()
	}
	for range 2 {
		select {
		case healthy := <-results:
			if !healthy {
				t.Error("overlapping health request reported a healthy guest as unavailable")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("health request did not finish")
		}
	}
}

func TestHealthReadAndStartupHaveDeadlines(t *testing.T) {
	for _, check := range []string{"status", "startup", "cancel"} {
		t.Run(check, func(t *testing.T) {
			listener, runtime := healthSocket(t)
			manager := Manager{Runner: healthRunner{}, ProxyName: "test-proxy"}
			accepted := make(chan struct{})
			go func() {
				connection, err := listener.Accept()
				if err != nil {
					return
				}
				defer connection.Close()
				// Bound the reproduction too: old code must fail, not hang tests.
				_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
				if _, err := vmproto.ReadHeader(connection); err != nil {
					return
				}
				close(accepted)
				_, _ = io.Copy(io.Discard, connection)
			}()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if check == "cancel" {
				go func() {
					select {
					case <-accepted:
						cancel()
					case <-ctx.Done():
					}
				}()
			}
			started := time.Now()
			limit := 500 * time.Millisecond
			if check == "status" {
				limit = 1500 * time.Millisecond
				if healthy, _ := manager.Healthy(runtime); healthy {
					t.Fatal("silent guest reported ready")
				}
			} else {
				timeout := 100 * time.Millisecond
				if check == "cancel" {
					timeout = time.Minute
				}
				err := manager.waitHealthy(ctx, runtime, timeout)
				if err == nil || (check == "cancel" && !errors.Is(err, context.Canceled)) {
					t.Fatalf("startup failure = %v", err)
				}
			}
			if elapsed := time.Since(started); elapsed > limit {
				t.Fatalf("silent health stream took %s, limit %s", elapsed, limit)
			}
		})
	}
}
