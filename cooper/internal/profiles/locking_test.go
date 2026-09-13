package profiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/statelock"
)

func TestStartupLockPreventsProfilePublication(t *testing.T) {
	f := newFixture(t)
	f.write(".codex/account", "personal")
	startup, err := statelock.Acquire(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer startup.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Millisecond)
	defer cancel()
	if _, err := f.service.Save(ctx, SaveRequest{Harness: "codex"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("save crossed startup lock: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.service.storePath(), "index.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("blocked mutation published metadata")
	}
	if f.read(".codex/account") != "personal" {
		t.Fatal("blocked mutation changed host state")
	}
}
