package docker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirSizeBytes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("1234"), 0o644); err != nil {
		t.Fatalf("write a.txt: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "b.txt"), []byte("123456"), 0o644); err != nil {
		t.Fatalf("write b.txt: %v", err)
	}

	size, err := DirSizeBytes(dir)
	if err != nil {
		t.Fatalf("DirSizeBytes() failed: %v", err)
	}
	if size != 10 {
		t.Fatalf("DirSizeBytes() = %d, want 10", size)
	}
}
