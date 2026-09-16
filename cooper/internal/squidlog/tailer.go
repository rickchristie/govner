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
	"sync"
	"time"
)

// Tailer watches the Squid access log file and sends new lines through
// a channel. It handles the file not existing yet (waits for creation)
// and polls for new data at a short interval.
type Tailer struct {
	path     string
	ch       chan string
	stopCh   chan struct{}
	stopOnce sync.Once
	poll     time.Duration
}

// InitialLines limits startup history so a large access log cannot replay
// old traffic through the live event stream.
const InitialLines = 200

// NewTailer creates a new Tailer for the access.log file in logDir.
// Call Start to begin tailing, and Lines to get the output channel.
func NewTailer(logDir string) *Tailer {
	return &Tailer{
		path:   filepath.Join(logDir, "access.log"),
		ch:     make(chan string, 1024),
		stopCh: make(chan struct{}),
		poll:   200 * time.Millisecond,
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
	t.stopOnce.Do(func() { close(t.stopCh) })
}

func (t *Tailer) run() {
	defer close(t.ch)

	var f *os.File
	defer func() {
		if f != nil {
			f.Close()
		}
	}()
	var reader *bufio.Reader
	var partial string
	for {
		select {
		case <-t.stopCh:
			return
		default:
		}
		if f == nil {
			var err error
			f, err = os.Open(t.path)
			if err != nil {
				if !t.wait() {
					return
				}
				continue
			}
			if err := seekLastLines(f, InitialLines); err != nil {
				return
			}
			reader = bufio.NewReader(f)
		}
		line, err := reader.ReadString('\n')
		partial += line
		if err == nil {
			if trimmed := strings.TrimRight(partial, "\n\r"); trimmed != "" {
				if !t.send(trimmed) {
					return
				}
			}
			partial = ""
			continue
		}
		if err != io.EOF || !t.wait() {
			return
		}

		// Squid rotates access.log by replacing its inode. Reopen the current
		// path instead of following the old file forever. Also handle truncation.
		current, statErr := os.Stat(t.path)
		opened, fileErr := f.Stat()
		if statErr == nil && fileErr == nil && !os.SameFile(current, opened) {
			f.Close()
			f = nil
			partial = ""
			continue
		}
		offset, seekErr := f.Seek(0, io.SeekCurrent)
		if statErr == nil && seekErr == nil && current.Size() < offset {
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return
			}
			reader.Reset(f)
			partial = ""
		}
	}
}

func (t *Tailer) wait() bool {
	select {
	case <-t.stopCh:
		return false
	case <-time.After(t.poll):
		return true
	}
}

func (t *Tailer) send(line string) bool {
	select {
	case t.ch <- line:
		return true
	case <-t.stopCh:
		return false
	}
}

func seekLastLines(f *os.File, limit int) error {
	end, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	buffer := make([]byte, 64*1024)
	remaining := limit
	for offset := end; offset > 0; {
		start := max(int64(0), offset-int64(len(buffer)))
		chunk := buffer[:offset-start]
		if _, err := f.ReadAt(chunk, start); err != nil {
			return err
		}
		for i := len(chunk) - 1; i >= 0; i-- {
			if chunk[i] != '\n' || start+int64(i) == end-1 {
				continue
			}
			remaining--
			if remaining == 0 {
				_, err := f.Seek(start+int64(i)+1, io.SeekStart)
				return err
			}
		}
		offset = start
	}
	_, err = f.Seek(0, io.SeekStart)
	return err
}
