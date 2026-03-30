package aclsrc

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestEmbeddedSourceMatchesOriginal verifies that the embedded .src copies
// are identical to the actual source files. This test fails if someone edits
// the real source without updating the embedded copies.
//
// To fix: run from the cooper directory:
//   cp cmd/acl-helper/main.go internal/aclsrc/main.go.src
//   cp internal/proxy/helper.go internal/aclsrc/helper.go.src

func TestEmbeddedSourceMatchesOriginal(t *testing.T) {
	// Find the repo root relative to this test file.
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	tests := []struct {
		name     string
		embedded []byte
		original string
	}{
		{"main.go", MainGo, filepath.Join(repoRoot, "cmd", "acl-helper", "main.go")},
		{"helper.go", HelperGo, filepath.Join(repoRoot, "internal", "proxy", "helper.go")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig, err := os.ReadFile(tt.original)
			if err != nil {
				t.Fatalf("could not read original %s: %v", tt.original, err)
			}
			if string(tt.embedded) != string(orig) {
				t.Errorf("embedded %s differs from original.\n"+
					"Run: cp %s internal/aclsrc/%s.src",
					tt.name, tt.original, tt.name)
			}
		})
	}
}
