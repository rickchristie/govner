package docker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareProxyMountDirsPreservesCommandLogs(t *testing.T) {
	cooperDir := t.TempDir()
	aclSocketDir := filepath.Join(cooperDir, "run")
	logDir := filepath.Join(cooperDir, "logs")

	staleSocket := filepath.Join(aclSocketDir, "acl.sock")
	if err := os.MkdirAll(filepath.Dir(staleSocket), 0o755); err != nil {
		t.Fatalf("mkdir stale socket dir: %v", err)
	}
	if err := os.WriteFile(staleSocket, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale socket: %v", err)
	}

	upLog := filepath.Join(logDir, "up.log")
	if err := os.MkdirAll(filepath.Dir(upLog), 0o755); err != nil {
		t.Fatalf("mkdir log dir: %v", err)
	}
	if err := os.WriteFile(upLog, []byte("command=up status=started\n"), 0o644); err != nil {
		t.Fatalf("write up log: %v", err)
	}

	if err := prepareProxyMountDirs(aclSocketDir, logDir); err != nil {
		t.Fatalf("prepareProxyMountDirs() failed: %v", err)
	}

	if _, err := os.Stat(staleSocket); !os.IsNotExist(err) {
		t.Fatalf("stale socket still exists or stat failed unexpectedly: %v", err)
	}
	if info, err := os.Stat(aclSocketDir); err != nil || !info.IsDir() {
		t.Fatalf("acl socket dir missing after prepare: info=%v err=%v", info, err)
	}

	data, err := os.ReadFile(upLog)
	if err != nil {
		t.Fatalf("read preserved up log: %v", err)
	}
	if string(data) != "command=up status=started\n" {
		t.Fatalf("up log content changed: %q", string(data))
	}
}
