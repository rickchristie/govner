package chatgpt

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestHostInstanceLock(t *testing.T) {
	for _, test := range []struct {
		name, target        string
		processError        error
		wantError, wantLock bool
	}{
		{"live host", "host-123", nil, true, true},
		{"unreadable host", "host-123", syscall.EPERM, true, true},
		{"stale host", "host-123", syscall.ESRCH, false, false},
		{"another runtime", "cooper-desktop-other-123", syscall.ESRCH, false, true},
		{"invalid PID", "host-0", syscall.ESRCH, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "SingletonLock")
			if err := os.Symlink(test.target, path); err != nil {
				t.Fatal(err)
			}
			err := checkHostInstance(directory, "host", func(int) error { return test.processError })
			if (err != nil) != test.wantError {
				t.Fatalf("error: %v", err)
			}
			_, err = os.Lstat(path)
			if errors.Is(err, os.ErrNotExist) == test.wantLock {
				t.Fatalf("lock state: %v", err)
			}
		})
	}
}

func TestCookieKeyChecksOnlySelectedCredentialFiles(t *testing.T) {
	directory := t.TempDir()
	if needed, err := needsCookieKey(directory); err != nil || needed {
		t.Fatalf("empty state: %v, %v", needed, err)
	}
	if err := os.Mkdir(filepath.Join(directory, "Default"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "Default", "Cookies-wal")
	if err := os.WriteFile(path, []byte("synthetic v11 cookie"), 0600); err != nil {
		t.Fatal(err)
	}
	if needed, err := needsCookieKey(directory); err != nil || !needed {
		t.Fatalf("encrypted state: %v, %v", needed, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "other-account"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := needsCookieKey(directory); err == nil {
		t.Fatal("linked credentials were accepted")
	}
}
