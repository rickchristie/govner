// Package squidlog provides a file tailer for the Squid access log.
// It watches the access.log file written by the Squid proxy container
// (mounted from ~/.cooper/logs) and emits individual log lines through
// a channel for the TUI to display.
package squidlog

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Tailer watches the Squid access log file and sends new lines through
// a channel. It handles the file not existing yet (waits for creation)
// and polls for new data at a short interval.
type Tailer struct {
	path   string
	ch     chan string
	stopCh chan struct{}
}

// NewTailer creates a new Tailer for the access.log file in logDir.
// Call Start to begin tailing, and Lines to get the output channel.
func NewTailer(logDir string) *Tailer {
	return &Tailer{
		path:   filepath.Join(logDir, "access.log"),
		ch:     make(chan string, 1024),
		stopCh: make(chan struct{}),
	}
}

// Start begins tailing the log file in a background goroutine.
func (t *Tailer) Start() {
	go t.run()
}

// Lines returns the channel that emits new log lines.
func (t *Tailer) Lines() <-chan string {
	return t.ch
}

// Stop signals the tailer to shut down. The Lines channel is closed
// after the goroutine exits.
func (t *Tailer) Stop() {
	close(t.stopCh)
}

func (t *Tailer) run() {
	defer close(t.ch)

	// Wait for the file to appear. Squid may not have written it yet.
	var f *os.File
	for {
		var err error
		f, err = os.Open(t.path)
		if err == nil {
			break
		}
		select {
		case <-t.stopCh:
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				// Emit any partial line content before waiting.
				if trimmed := strings.TrimRight(line, "\n\r"); trimmed != "" {
					t.send(trimmed)
				}
				select {
				case <-t.stopCh:
					return
				case <-time.After(200 * time.Millisecond):
					continue
				}
			}
			// Unexpected error — stop.
			return
		}
		if trimmed := strings.TrimRight(line, "\n\r"); trimmed != "" {
			t.send(trimmed)
		}
	}
}

func (t *Tailer) send(line string) {
	select {
	case t.ch <- line:
	case <-t.stopCh:
	}
}
