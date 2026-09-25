package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type desktopViewerRecord struct {
	Host  string `json:"host"`
	Token string `json:"token"`
}

// OpenViewer starts one local viewer process and reuses it on later launches.
func OpenViewer(ctx context.Context, directory, executable string, arguments []string) (string, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	if record, err := readDesktopViewer(directory); err == nil && desktopViewerReady(ctx, record) {
		return (Server{Host: record.Host, Token: record.Token}).URL(), nil
	}
	log, err := os.OpenFile(filepath.Join(directory, "desktop-viewer.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer log.Close()
	command := exec.Command(executable, arguments...)
	command.Stdin = nil
	command.Stdout, command.Stderr = log, log
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return "", fmt.Errorf("start local desktop viewer: %w", err)
	}
	// Reap this child while the launch process lives. Setsid lets the viewer
	// remain available after that process and its terminal exit.
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(15 * time.Second)
	defer timeout.Stop()
	for {
		if record, err := readDesktopViewer(directory); err == nil && desktopViewerReady(ctx, record) {
			return (Server{Host: record.Host, Token: record.Token}).URL(), nil
		}
		select {
		case err := <-done:
			if err != nil {
				return "", fmt.Errorf("local desktop viewer stopped: %w; see %s", err, log.Name())
			}
			// Another launch can own the viewer lock. Its record will appear
			// during the same bounded wait.
			done = nil
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timeout.C:
			return "", fmt.Errorf("local desktop viewer did not become ready; see %s", log.Name())
		case <-ticker.C:
		}
	}
}

// ServeViewer owns every HTTP and WebSocket connection until the workload
// stops. Each back end supplies only its fixed display dialer and lifetime.
func ServeViewer(ctx context.Context, directory string, dial Dial, alive func() bool) error {
	lock, err := os.OpenFile(filepath.Join(directory, "desktop-viewer.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil
		}
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if !alive() {
		return errors.New("desktop workload is not running")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	token, err := NewToken()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	viewer := Server{Host: listener.Addr().String(), Token: token, Dial: func(request context.Context) (net.Conn, error) {
		connection, err := dial(request)
		if err != nil {
			return nil, err
		}
		stop := context.AfterFunc(ctx, func() { connection.Close() })
		return &viewerConnection{Conn: connection, stop: stop}, nil
	}}
	server := &http.Server{Handler: viewer.Handler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 32 << 10}
	data, err := json.Marshal(desktopViewerRecord{Host: viewer.Host, Token: token})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(directory, "desktop-viewer.json"), data, 0600); err != nil {
		return err
	}
	defer removeDesktopViewer(directory, token)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer server.Close()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-done:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		case <-ticker.C:
			if !alive() {
				return nil
			}
		}
	}
}

type viewerConnection struct {
	net.Conn
	stop func() bool
}

func (c *viewerConnection) Close() error {
	c.stop()
	return c.Conn.Close()
}

func readDesktopViewer(directory string) (desktopViewerRecord, error) {
	var record desktopViewerRecord
	path := filepath.Join(directory, "desktop-viewer.json")
	info, err := os.Lstat(path)
	if err != nil {
		return record, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return record, errors.New("desktop viewer record must be a private regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return record, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return record, err
	}
	if len(data) > 4096 {
		return record, errors.New("desktop viewer record is too large")
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return record, err
	}
	host, port, err := net.SplitHostPort(record.Host)
	portNumber, portErr := strconv.Atoi(port)
	if err != nil || host != "127.0.0.1" || portErr != nil || portNumber < 1 || portNumber > 65535 || len(record.Token) != 64 || strings.Trim(record.Token, "0123456789abcdef") != "" {
		return record, errors.New("desktop viewer record is invalid")
	}
	return record, nil
}

func removeDesktopViewer(directory, token string) {
	if record, err := readDesktopViewer(directory); err == nil && record.Token == token {
		_ = os.Remove(filepath.Join(directory, "desktop-viewer.json"))
	}
}

func desktopViewerReady(ctx context.Context, record desktopViewerRecord) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+record.Host+"/health", nil)
	if err != nil {
		return false
	}
	request.Header.Set("Authorization", "Bearer "+record.Token)
	client := &http.Client{Timeout: 500 * time.Millisecond, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	response.Body.Close()
	return response.StatusCode == http.StatusOK
}
