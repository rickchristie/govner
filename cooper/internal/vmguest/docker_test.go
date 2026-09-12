package vmguest

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestDeviceGIDFailsClosedForMissingPath(t *testing.T) {
	t.Parallel()
	if _, err := deviceGID(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("deviceGID() accepted a missing device as group 0")
	}
}

func TestDeviceGIDReadsUnixGroup(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "socket-placeholder")
	if err := os.WriteFile(path, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("test file has no Unix group information")
	}
	got, err := deviceGID(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != int(stat.Gid) {
		t.Fatalf("deviceGID() = %d, want %d", got, stat.Gid)
	}
}
