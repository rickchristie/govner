package vmguest

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"

	"github.com/rickchristie/govner/cooper/internal/vmproto"
)

type synchronizedFrameWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (w *synchronizedFrameWriter) write(frame vmproto.Frame) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return vmproto.WriteFrame(w.w, frame)
}

func runExecStream(parent context.Context, stream io.ReadWriteCloser, manifest vmproto.Manifest, header vmproto.Header) {
	defer stream.Close()
	if header.Nonce != manifest.Nonce || len(header.Command) == 0 {
		_ = vmproto.WriteFrame(stream, vmproto.Frame{Type: vmproto.FrameError, Data: []byte("invalid VM exec request")})
		return
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	args := []string{"--host=unix://" + dockerSocketPath, "exec"}
	if header.Interactive {
		args = append(args, "-it")
	} else {
		args = append(args, "-i")
	}
	for _, environment := range header.Environment {
		args = append(args, "-e", environment)
	}
	args = append(args, manifest.AgentContainer)
	args = append(args, header.Command...)
	command := exec.CommandContext(ctx, "/usr/local/bin/docker", args...)
	writer := &synchronizedFrameWriter{w: stream}
	if header.Interactive {
		runInteractiveExec(ctx, cancel, stream, writer, command, header)
		return
	}
	runOneShotExec(ctx, cancel, stream, writer, command)
}

func runInteractiveExec(ctx context.Context, cancel context.CancelFunc, stream io.Reader, writer *synchronizedFrameWriter, command *exec.Cmd, header vmproto.Header) {
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Rows: header.Rows, Cols: header.Columns})
	if err != nil {
		_ = writer.write(vmproto.Frame{Type: vmproto.FrameError, Data: []byte("cannot start interactive VM command")})
		return
	}
	defer terminal.Close()
	outputDone := make(chan struct{})
	go func() {
		copyFrames(terminal, writer, vmproto.FrameStdout)
		close(outputDone)
	}()
	go readClientFrames(ctx, cancel, stream, terminal, command.Process)
	err = command.Wait()
	terminal.Close()
	<-outputDone
	_ = writer.write(vmproto.Frame{Type: vmproto.FrameExit, Data: vmproto.EncodeExit(exitStatus(err))})
}

func runOneShotExec(ctx context.Context, cancel context.CancelFunc, stream io.Reader, writer *synchronizedFrameWriter, command *exec.Cmd) {
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = writer.write(vmproto.Frame{Type: vmproto.FrameError, Data: []byte("cannot open VM stdout")})
		return
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		_ = writer.write(vmproto.Frame{Type: vmproto.FrameError, Data: []byte("cannot open VM stderr")})
		return
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		_ = writer.write(vmproto.Frame{Type: vmproto.FrameError, Data: []byte("cannot open VM stdin")})
		return
	}
	if err := command.Start(); err != nil {
		_ = writer.write(vmproto.Frame{Type: vmproto.FrameError, Data: []byte("cannot start VM command")})
		return
	}
	var output sync.WaitGroup
	output.Add(2)
	go func() { defer output.Done(); copyFrames(stdout, writer, vmproto.FrameStdout) }()
	go func() { defer output.Done(); copyFrames(stderr, writer, vmproto.FrameStderr) }()
	go readClientFrames(ctx, cancel, stream, stdin, command.Process)
	err = command.Wait()
	stdin.Close()
	output.Wait()
	_ = writer.write(vmproto.Frame{Type: vmproto.FrameExit, Data: vmproto.EncodeExit(exitStatus(err))})
}

func copyFrames(reader io.Reader, writer *synchronizedFrameWriter, frameType vmproto.FrameType) {
	buffer := make([]byte, 32*1024)
	for {
		count, err := reader.Read(buffer)
		if count > 0 {
			if writeErr := writer.write(vmproto.Frame{Type: frameType, Data: append([]byte(nil), buffer[:count]...)}); writeErr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func readClientFrames(ctx context.Context, cancel context.CancelFunc, reader io.Reader, stdin io.WriteCloser, process *os.Process) {
	defer stdin.Close()
	for {
		frame, err := vmproto.ReadFrame(reader)
		if err != nil {
			cancel()
			return
		}
		switch frame.Type {
		case vmproto.FrameStdin:
			if len(frame.Data) == 0 {
				stdin.Close()
				continue
			}
			if _, err := stdin.Write(frame.Data); err != nil {
				return
			}
		case vmproto.FrameResize:
			if terminal, ok := stdin.(*os.File); ok {
				rows, columns, err := vmproto.DecodeResize(frame.Data)
				if err == nil {
					_ = pty.Setsize(terminal, &pty.Winsize{Rows: rows, Cols: columns})
				}
			}
		case vmproto.FrameSignal:
			signalProcess(process, string(frame.Data))
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func signalProcess(process *os.Process, signal string) {
	if process == nil {
		return
	}
	switch signal {
	case "INT":
		_ = process.Signal(os.Interrupt)
	case "TERM":
		_ = process.Signal(syscall.SIGTERM)
	case "HUP":
		_ = process.Signal(syscall.SIGHUP)
	}
}

func exitStatus(err error) int32 {
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return int32(exitError.ExitCode())
	}
	return 125
}
