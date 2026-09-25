package desktop

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestViewerAssetsUseModulePaths(t *testing.T) {
	// Go removes vendor directories from module archives. A checkout build
	// can work while go install produces a viewer with missing dependencies.
	err := fs.WalkDir(viewerAssets, "assets", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == "vendor" {
			t.Errorf("Go module archives exclude %s", name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestViewerServesAllUpstreamAssets(t *testing.T) {
	manifest, err := viewerAssets.ReadFile("assets/novnc/SHA256SUMS")
	if err != nil {
		t.Fatal(err)
	}
	handler := assetHandler()
	for _, line := range strings.Split(strings.TrimSpace(string(manifest)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("invalid asset hash entry: %q", line)
		}
		name := fields[1]
		t.Run(name, func(t *testing.T) {
			// Preserve upstream import URLs while storing dependencies at a
			// path that is included in the Go module archive.
			path := strings.Replace(name, "dependencies/", "vendor/", 1)
			request := httptest.NewRequest("GET", "/assets/novnc/"+path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("asset returned HTTP %d", response.Code)
			}
			if got := fmt.Sprintf("%x", sha256.Sum256(response.Body.Bytes())); got != fields[0] {
				t.Fatalf("asset hash = %s, want %s", got, fields[0])
			}
			if strings.HasSuffix(name, ".js") && !strings.Contains(response.Header().Get("Content-Type"), "javascript") {
				t.Fatalf("module content type = %q", response.Header().Get("Content-Type"))
			}
		})
	}
}
