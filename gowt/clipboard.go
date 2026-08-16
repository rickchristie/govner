package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Clipboard is the application boundary used by the TUI. The model requests a
// write and receives a typed result; it never probes the OS or starts a process
// directly from Update or View.
type Clipboard interface {
	Write(text string) error
	Hint() string
}

type systemClipboard struct{ timeout time.Duration }

const defaultClipboardTimeout = 5 * time.Second

func newSystemClipboard() Clipboard { return systemClipboard{timeout: defaultClipboardTimeout} }

func (systemClipboard) command(ctx context.Context) (*exec.Cmd, error) {
	commands := []struct {
		name string
		args []string
	}{
		{name: "wl-copy"},
		{name: "xclip", args: []string{"-selection", "clipboard"}},
		{name: "xsel", args: []string{"--clipboard", "--input"}},
		{name: "pbcopy"},
		{name: "clip.exe"},
	}
	for _, candidate := range commands {
		if _, err := exec.LookPath(candidate.name); err == nil {
			return exec.CommandContext(ctx, candidate.name, candidate.args...), nil
		}
	}
	return nil, fmt.Errorf("no clipboard command found (install wl-copy, xclip, or xsel)")
}

func (c systemClipboard) Write(text string) error {
	timeout := c.timeout
	if timeout <= 0 {
		timeout = defaultClipboardTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd, err := c.command(ctx)
	if err != nil {
		return err
	}
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("clipboard command timed out: %w", ctx.Err())
		}
		return err
	}
	return nil
}

func (c systemClipboard) Hint() string {
	if _, err := c.command(context.Background()); err == nil {
		return ""
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return "\n             (install: sudo apt install wl-clipboard)"
	}
	if os.Getenv("DISPLAY") != "" {
		return "\n             (install: sudo apt install xclip)"
	}
	return "\n             (no clipboard tool found)"
}

// copyToClipboard remains a small compatibility helper for callers outside the
// model; production UI code uses Clipboard asynchronously through copyLogs.
func copyToClipboard(text string) error {
	return systemClipboard{timeout: defaultClipboardTimeout}.Write(text)
}
