package vmguest

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/vmproto"
)

type delayedOutput struct {
	bytes.Buffer
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *delayedOutput) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.entered); <-w.release })
	return w.Buffer.Write(data)
}

func TestExecDrainsOutputAfterProcessExit(t *testing.T) {
	for _, mode := range []string{"stdout", "stderr", "terminal"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			marker := filepath.Join(t.TempDir(), "finished")
			script := `printf first; read -r line; printf final; printf done > "$1"`
			if mode == "stderr" {
				script = "exec 1>&2; " + script
			}
			if mode == "terminal" {
				script = "stty -echo -onlcr; " + script
			}
			command := exec.CommandContext(ctx, "sh", "-c", script, "sh", marker)
			input, send := io.Pipe()
			defer input.Close()
			defer send.Close()
			output := &delayedOutput{entered: make(chan struct{}), release: make(chan struct{})}
			var release sync.Once
			defer release.Do(func() { close(output.release) })
			writer := &synchronizedFrameWriter{w: output}
			done := make(chan struct{})
			go func() {
				defer close(done)
				if mode == "terminal" {
					runInteractiveExec(ctx, cancel, input, writer, command, vmproto.Header{Rows: 24, Columns: 80})
					return
				}
				runOneShotExec(ctx, cancel, input, writer, command)
			}()
			select {
			case <-output.entered:
			case <-ctx.Done():
				t.Fatal("command did not send initial output")
			}
			if err := vmproto.WriteFrame(send, vmproto.Frame{Type: vmproto.FrameStdin, Data: []byte("continue\n")}); err != nil {
				t.Fatal(err)
			}
			for {
				if _, err := os.Stat(marker); err == nil {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("command did not send final output")
				case <-time.After(5 * time.Millisecond):
				}
			}
			// Keep the first frame blocked while the process exits. Its final
			// bytes must remain readable until the consumer resumes.
			time.Sleep(100 * time.Millisecond)
			release.Do(func() { close(output.release) })
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("command did not finish")
			}
			var text bytes.Buffer
			var exited bool
			for output.Len() > 0 {
				frame, err := vmproto.ReadFrame(&output.Buffer)
				if err != nil {
					t.Fatal(err)
				}
				switch frame.Type {
				case vmproto.FrameStdout, vmproto.FrameStderr:
					if exited {
						t.Fatal("output arrived after exit status")
					}
					text.Write(frame.Data)
				case vmproto.FrameExit:
					if !bytes.Equal(frame.Data, vmproto.EncodeExit(0)) {
						t.Fatalf("command exit = %v", frame.Data)
					}
					exited = true
				default:
					t.Fatalf("unexpected frame: %+v", frame)
				}
			}
			if text.String() != "firstfinal" || !exited {
				t.Fatalf("command output = %q, exit received = %v", text.String(), exited)
			}
		})
	}
}
