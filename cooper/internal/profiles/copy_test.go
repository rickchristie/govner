package profiles

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCopyPreservesCompleteStateWithoutFollowingLinks(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(root, "copy")
	if err := os.MkdirAll(filepath.Join(source, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(source, "auth.json"), "fake account\x00data", 0o600)
	writeFixture(t, filepath.Join(source, "sessions", "run"), "#!/bin/sh\nexit 0\n", 0o755)
	outside := filepath.Join(root, "outside")
	writeFixture(t, outside, "must stay outside", 0o600)
	for name, link := range map[string]string{"relative": "sessions/run", "external": outside, "broken": "absent"} {
		if err := os.Symlink(link, filepath.Join(source, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := syscall.Mkfifo(filepath.Join(source, "transport.pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", filepath.Join(source, "transport.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Chmod(filepath.Join(source, "sessions"), 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(filepath.Join(source, "sessions"), 0o700)
	defer os.Chmod(filepath.Join(target, "sessions"), 0o700)
	if err := copyTree(context.Background(), source, target); err != nil {
		t.Fatal(err)
	}
	before, err := treeDigest(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	after, err := treeDigest(context.Background(), target)
	if err != nil || before != after {
		t.Fatalf("complete state differs after copy: %v", err)
	}
	for _, name := range []string{"transport.pipe", "transport.sock"} {
		if _, err := os.Lstat(filepath.Join(target, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("copied process transport %s: %v", name, err)
		}
	}
	for name, link := range map[string]string{"relative": "sessions/run", "external": outside, "broken": "absent"} {
		actual, err := os.Readlink(filepath.Join(target, name))
		if err != nil || actual != link {
			t.Fatalf("link %s changed: %v", name, err)
		}
	}
	writeFixture(t, filepath.Join(target, "auth.json"), "new profile credentials", 0o600)
	unchanged, err := treeDigest(context.Background(), source)
	if err != nil || unchanged != before {
		t.Fatalf("writing profile state changed source: %v", err)
	}
}

func TestCopyCancellationAndSpecialFiles(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "source"), filepath.Join(root, "copy")
	writeFixture(t, source, "keep", 0o600)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := copyTree(ctx, source, target); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled copy: %v", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled copy created destination: %v", err)
	}
	if err := copyTree(context.Background(), "/dev/null", target); err == nil {
		t.Fatal("copied a device as profile state")
	}
}

func TestDigestDetectsFileModesAndMissingChildren(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "config")
	writeFixture(t, file, "same bytes", 0o600)
	before, err := treeDigest(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0o700); err != nil {
		t.Fatal(err)
	}
	after, err := treeDigest(context.Background(), root)
	if err != nil || before == after {
		t.Fatalf("digest ignored permission change: %v", err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	removed, err := treeDigest(context.Background(), root)
	if err != nil || removed == after {
		t.Fatalf("digest ignored removed file: %v", err)
	}
}

func TestDigestDistinguishesFileBytesFromTreeStructure(t *testing.T) {
	single, separate := t.TempDir(), t.TempDir()
	// These trees produced the same byte stream when file bytes shared the
	// tree hash directly. A binary file can contain a complete later record.
	writeFixture(t, filepath.Join(single, "a"), "first\x00b\x00384\x00second", 0o600)
	writeFixture(t, filepath.Join(separate, "a"), "first", 0o600)
	writeFixture(t, filepath.Join(separate, "b"), "second", 0o600)
	left, err := treeDigest(t.Context(), single)
	if err != nil {
		t.Fatal(err)
	}
	right, err := treeDigest(t.Context(), separate)
	if err != nil {
		t.Fatal(err)
	}
	if left == right {
		t.Fatal("file bytes hid a different directory structure")
	}
}

func writeFixture(t *testing.T, path, value string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), mode); err != nil {
		t.Fatal(err)
	}
}

func TestCopyRetainsSparseFileBytesAndIndependentInodes(t *testing.T) {
	source, target := filepath.Join(t.TempDir(), "sparse"), filepath.Join(t.TempDir(), "copy")
	file, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(4<<20, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("session-tail")); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(t.Context(), source, target); err != nil {
		t.Fatal(err)
	}
	before, err := treeDigest(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	after, err := treeDigest(t.Context(), target)
	if err != nil || before != after {
		t.Fatal("sparse file bytes changed")
	}
	left, _ := os.Stat(source)
	right, _ := os.Stat(target)
	if os.SameFile(left, right) {
		t.Fatal("copied profile shared a writable inode")
	}
}
