package vm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"golang.org/x/term"

	"github.com/rickchristie/govner/cooper/internal/vmproto"
)

// ExitError reports a non-zero one-shot command status.
type ExitError struct{ Status int }

func (e ExitError) Error() string { return fmt.Sprintf("VM command exited with status %d", e.Status) }

// Exec runs one command in the selected agent container through the private
// host control socket.
func (m Manager) Exec(ctx context.Context, runtime Runtime, spec workloadExec, stdin io.Reader, stdout, stderr io.Writer) error {
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", runtime.ControlSocket)
	if err != nil {
		return fmt.Errorf("connect to VM %s: %w", runtime.ID, err)
	}
	defer connection.Close()
	// DialContext only covers connection setup. Closing the established stream
	// also interrupts a blocked read and notifies the guest of the disconnect.
	stopCancellation := context.AfterFunc(ctx, func() { connection.Close() })
	defer stopCancellation()
	requestID, err := randomHex(12)
	if err != nil {
		return err
	}
	header := vmproto.NewHeader(vmproto.ServiceExec, requestID)
	header.Command = append([]string(nil), spec.Command...)
	header.Environment = append([]string(nil), spec.Environment...)
	header.Interactive = spec.Interactive
	if spec.Interactive {
		header.Rows, header.Columns = terminalSize(stdin)
	}
	if err := vmproto.WriteHeader(connection, header); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	writer := &lockedWriter{writer: connection}
	if spec.Interactive {
		go copyInputFrames(ctx, stdin, writer)
		stopSignals := forwardTerminalSignals(writer, stdin)
		defer stopSignals()
	} else {
		_ = writer.write(vmproto.Frame{Type: vmproto.FrameStdin})
	}
	for {
		frame, err := vmproto.ReadFrame(connection)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return errors.New("VM command stream closed before an exit status")
			}
			return err
		}
		switch frame.Type {
		case vmproto.FrameStdout:
			_, _ = stdout.Write(frame.Data)
		case vmproto.FrameStderr:
			_, _ = stderr.Write(frame.Data)
		case vmproto.FrameError:
			return errors.New(string(frame.Data))
		case vmproto.FrameExit:
			status, err := vmproto.DecodeExit(frame.Data)
			if err != nil {
				return err
			}
			if spec.Interactive || status == 0 {
				return nil
			}
			return ExitError{Status: int(status)}
		}
	}
}

// workloadExec is intentionally small. The command adapter owns names and
// session files; the VM client owns only wire values.
type workloadExec struct {
	Command     []string
	Environment []string
	Interactive bool
}

// ExecCommand is the public transport-neutral entry point used by main.
func (m Manager) ExecCommand(ctx context.Context, runtime Runtime, command, environment []string, interactive bool, stdin io.Reader, stdout, stderr io.Writer) error {
	return m.Exec(ctx, runtime, workloadExec{Command: command, Environment: environment, Interactive: interactive}, stdin, stdout, stderr)
}

type lockedWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func (w *lockedWriter) write(frame vmproto.Frame) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return vmproto.WriteFrame(w.writer, frame)
}

func copyInputFrames(ctx context.Context, input io.Reader, writer *lockedWriter) {
	buffer := make([]byte, 32*1024)
	for {
		count, err := input.Read(buffer)
		if count > 0 {
			if writeErr := writer.write(vmproto.Frame{Type: vmproto.FrameStdin, Data: append([]byte(nil), buffer[:count]...)}); writeErr != nil {
				return
			}
		}
		if err != nil {
			_ = writer.write(vmproto.Frame{Type: vmproto.FrameStdin})
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func terminalSize(input io.Reader) (rows, columns uint16) {
	file, ok := input.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return 24, 80
	}
	width, height, err := term.GetSize(int(file.Fd()))
	if err != nil || width < 1 || height < 1 || width > 65535 || height > 65535 {
		return 24, 80
	}
	return uint16(height), uint16(width)
}

func forwardTerminalSignals(writer *lockedWriter, input io.Reader) func() {
	file, ok := input.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return func() {}
	}
	oldState, err := term.MakeRaw(int(file.Fd()))
	if err != nil {
		return func() {}
	}
	signals := make(chan os.Signal, 8)
	done := make(chan struct{})
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGWINCH)
	go func() {
		for {
			select {
			case received := <-signals:
				if received == syscall.SIGWINCH {
					rows, columns := terminalSize(file)
					_ = writer.write(vmproto.Frame{Type: vmproto.FrameResize, Data: vmproto.EncodeResize(rows, columns)})
					continue
				}
				name := "TERM"
				if received == os.Interrupt {
					name = "INT"
				} else if received == syscall.SIGHUP {
					name = "HUP"
				}
				_ = writer.write(vmproto.Frame{Type: vmproto.FrameSignal, Data: []byte(name)})
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(signals)
		close(done)
		_ = term.Restore(int(file.Fd()), oldState)
	}
}
