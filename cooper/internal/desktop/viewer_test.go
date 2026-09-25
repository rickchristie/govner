package desktop

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestViewerLifetimeAndReuse(t *testing.T) {
	directory := t.TempDir()
	var alive atomic.Bool
	alive.Store(true)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- ServeViewer(ctx, directory, func(context.Context) (net.Conn, error) {
			t.Error("health opened a guest stream")
			return nil, net.ErrClosed
		}, alive.Load)
	}()
	var record desktopViewerRecord
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		value, err := readDesktopViewer(directory)
		if err == nil && desktopViewerReady(ctx, value) {
			record = value
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if record.Token == "" {
		t.Fatal("viewer did not start")
	}
	url, err := OpenViewer(ctx, directory, "/no-such-executable", nil)
	if err != nil || url != (Server{Host: record.Host, Token: record.Token}).URL() {
		t.Fatalf("reuse: %s, %v", url, err)
	}
	alive.Store(false)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("viewer outlived workload")
	}
	if _, err := os.Stat(filepath.Join(directory, "desktop-viewer.json")); !os.IsNotExist(err) {
		t.Fatalf("viewer capability was retained: %v", err)
	}
	if desktopViewerReady(ctx, record) {
		t.Fatal("stopped viewer still accepts requests")
	}
}

func TestViewerRecordRejectsUnexpectedFilesAndHosts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "desktop-viewer.json")
	for _, host := range []string{"attacker.test:1234", "127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:http", "[::1]:1234"} {
		data, _ := json.Marshal(desktopViewerRecord{Host: host, Token: strings.Repeat("a", 64)})
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readDesktopViewer(dir); err == nil {
			t.Fatalf("accepted %s", host)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "elsewhere"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := readDesktopViewer(dir); err == nil {
		t.Fatal("accepted a symbolic link")
	}
}

func TestDisplayCommandConnectionAndDeadline(t *testing.T) {
	connection, err := DialCommand(t.Context(), "cat")
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(time.Second))
	if _, err := io.WriteString(connection, "display"); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, len("display"))
	if _, err := io.ReadFull(connection, data); err != nil || string(data) != "display" {
		t.Fatalf("display stream: %q, %v", data, err)
	}
	connection.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	_, err = connection.Read(data)
	if failure, ok := err.(net.Error); !ok || !failure.Timeout() {
		t.Fatalf("read deadline: %v", err)
	}
}
