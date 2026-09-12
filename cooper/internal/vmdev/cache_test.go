package vmdev

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSourceDigestIncludesDirtyAndNewInputs(t *testing.T) {
	root := t.TempDir()
	write := func(name, value string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	digest := func() string {
		t.Helper()
		got, err := SourceDigest(root)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	write("go.mod", "module test")
	previous := digest()
	for _, path := range []string{"go.mod", "go.sum", "internal/new.go", "internal/templates/new.sh", "internal/vmpayload/data.bin", "cmd/tool/main.go", "dev/vm-test.py", "test-vm-dev.sh"} {
		write(path, "changed")
		next := digest()
		if next == previous {
			t.Fatalf("digest ignores %s", path)
		}
		previous = next
	}
	write(".test-tmp/cache/value", "ignored")
	write("README.md", "ignored")
	write("cooper", "generated binary")
	if digest() != previous {
		t.Fatal("generated state changed source digest")
	}
	if err := os.Remove(filepath.Join(root, "internal/new.go")); err != nil {
		t.Fatal(err)
	}
	if digest() == previous {
		t.Fatal("deletion did not change source digest")
	}
}

func TestLeaseBlocksCleanupAndSeparatesDaemons(t *testing.T) {
	root := t.TempDir()
	first, err := Acquire(context.Background(), filepath.Join(root, CacheKey("host", 1000), "lease"))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if other, err := Acquire(ctx, filepath.Join(root, CacheKey("host", 1000), "lease")); err == nil {
		other.Close()
		t.Fatal("active cache lease was bypassed")
	}
	second, err := Acquire(context.Background(), filepath.Join(root, CacheKey("guest", 1000), "lease"))
	if err != nil {
		t.Fatal(err)
	}
	second.Close()
	if CacheKey("host", 1000) == CacheKey("host", 1001) {
		t.Fatal("account cache collision")
	}
}

func TestStagePreservesSharedInodeAndRejectsReplacement(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "archive")
	if err := os.WriteFile(source, []byte("immutable test archive"), 0444); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "run", "archive")
	mode, err := Stage(source, target)
	if err != nil || mode != "hardlink" {
		t.Fatalf("stage: %s %v", mode, err)
	}
	left, _ := os.Stat(source)
	right, _ := os.Stat(target)
	if !os.SameFile(left, right) {
		t.Fatal("archive was copied on the same filesystem")
	}
	if _, err := Stage(source, target); err == nil {
		t.Fatal("stage replaced a live file")
	}
	if err := os.RemoveAll(filepath.Dir(target)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(source)
	if err != nil || string(data) != "immutable test archive" {
		t.Fatalf("cleanup damaged cache: %q %v", data, err)
	}
	if info, _ := os.Stat(source); info.Mode().Perm() != 0444 {
		t.Fatal("cache mode changed")
	}
}
