package bridge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestSaveAndLoadBridgeRoutes_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	routes := []config.BridgeRoute{
		{APIPath: "/api/test", ScriptPath: "/usr/local/bin/test.sh"},
		{APIPath: "/api/build", ScriptPath: "/usr/local/bin/build.sh"},
	}

	if err := SaveBridgeRoutes(dir, routes); err != nil {
		t.Fatalf("SaveBridgeRoutes: %v", err)
	}

	loaded, err := LoadBridgeRoutes(dir)
	if err != nil {
		t.Fatalf("LoadBridgeRoutes: %v", err)
	}

	if len(loaded) != len(routes) {
		t.Fatalf("expected %d routes, got %d", len(routes), len(loaded))
	}
	for i, r := range loaded {
		if r.APIPath != routes[i].APIPath || r.ScriptPath != routes[i].ScriptPath {
			t.Errorf("route[%d] mismatch: got %+v, want %+v", i, r, routes[i])
		}
	}
}

func TestLoadBridgeRoutes_MissingFile(t *testing.T) {
	dir := t.TempDir()

	routes, err := LoadBridgeRoutes(dir)
	if err != nil {
		t.Fatalf("LoadBridgeRoutes: unexpected error: %v", err)
	}
	if len(routes) != 0 {
		t.Fatalf("expected empty slice, got %d routes", len(routes))
	}
}

func TestSaveBridgeRoutes_CreatesDirectory(t *testing.T) {
	base := t.TempDir()
	nested := filepath.Join(base, "sub", "dir")

	routes := []config.BridgeRoute{
		{APIPath: "/api/test", ScriptPath: "/usr/local/bin/test.sh"},
	}

	if err := SaveBridgeRoutes(nested, routes); err != nil {
		t.Fatalf("SaveBridgeRoutes: %v", err)
	}

	// Verify the directory was created.
	info, err := os.Stat(nested)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected directory, got file")
	}

	// Verify the file exists and is readable.
	loaded, err := LoadBridgeRoutes(nested)
	if err != nil {
		t.Fatalf("LoadBridgeRoutes after create: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 route, got %d", len(loaded))
	}
}
