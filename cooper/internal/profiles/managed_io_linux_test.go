package profiles

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// Watch the actual history inode. A copy hook alone would not detect a full
// digest pass, which would still make switching depend on history size.
func TestManagedSaveLoadDoesNotOpenHistory(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	f.save("codex")
	f.migrate()
	f.load("codex", "Work")
	f.write(".codex/account", "work")
	f.write(".codex/sessions/history", "a native session")
	f.save("codex")
	path := filepath.Join(f.home, ".codex/sessions/history")
	watch, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(watch)
	if _, err := unix.InotifyAddWatch(watch, path, unix.IN_OPEN|unix.IN_ACCESS); err != nil {
		t.Fatal(err)
	}
	// First prove that this filesystem reports a read through the host alias.
	if _, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	}
	events := make([]byte, 4096)
	if count, err := unix.Read(watch, events); err != nil || count == 0 {
		t.Fatalf("history read was not observed: %d, %v", count, err)
	}
	f.save("codex")
	f.load("codex", "Default")
	f.load("codex", "Work")
	f.load("codex", "Work")
	f.save("codex")
	if count, err := unix.Read(watch, events); !errors.Is(err, unix.EAGAIN) {
		t.Fatalf("save/load opened session history: event bytes=%d, error=%v", count, err)
	}
}
