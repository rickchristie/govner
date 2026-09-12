package clipboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadTokenMetadataRejectsTrailingJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path, err := WriteRuntimeToken(dir, "runtime", "token", RuntimeVM, "codex", "shim")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, []byte("{}\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTokenMetadata(path); err == nil || !strings.Contains(err.Error(), "unexpected data") {
		t.Fatalf("ReadTokenMetadata() error = %v, want trailing-data error", err)
	}
}

func TestTokenOperationsRejectPathIDs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, runtimeID := range []string{".", "..", "../outside", "path/name", "path\\name"} {
		if _, err := WriteRuntimeToken(dir, runtimeID, "token", RuntimeVM, "codex", "shim"); err == nil {
			t.Errorf("WriteRuntimeToken() accepted %q", runtimeID)
		}
		if err := RemoveTokenFile(dir, runtimeID); err == nil {
			t.Errorf("RemoveTokenFile() accepted %q", runtimeID)
		}
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "keep" {
		t.Fatalf("outside file changed: %q, %v", data, err)
	}
}
