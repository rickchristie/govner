package vm

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type imageArchiveRunner struct {
	imageID   string
	payload   string
	saveCalls int
	saveArgs  []string
}

func (r *imageArchiveRunner) Output(_ context.Context, _ string, _ ...string) ([]byte, error) {
	return []byte(r.imageID + "\n"), nil
}

func (r *imageArchiveRunner) Run(_ context.Context, _ io.Reader, stdout, _ io.Writer, _ string, args ...string) error {
	r.saveCalls++
	r.saveArgs = append([]string(nil), args...)
	_, err := io.WriteString(stdout, r.payload)
	return err
}

func TestEnsureImageArchiveExportsImmutableIDAndReusesCache(t *testing.T) {
	t.Parallel()
	runner := &imageArchiveRunner{imageID: "sha256:" + strings.Repeat("a", 64), payload: "docker archive"}
	cooperDir := t.TempDir()
	first, err := EnsureImageArchive(context.Background(), cooperDir, "cooper-cli-codex:latest", runner, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EnsureImageArchive(context.Background(), cooperDir, "cooper-cli-codex:latest", runner, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || runner.saveCalls != 1 {
		t.Fatalf("archives = %#v and %#v; save calls = %d", first, second, runner.saveCalls)
	}
	if got := strings.Join(runner.saveArgs, " "); got != "save "+runner.imageID {
		t.Fatalf("docker arguments = %q, want immutable image ID", got)
	}
	if _, err := os.Stat(imageArchiveMetadataPath(first.Path)); err != nil {
		t.Fatalf("archive metadata is missing: %v", err)
	}
}

func TestEnsureImageArchiveRepairsTruncatedCache(t *testing.T) {
	t.Parallel()
	runner := &imageArchiveRunner{imageID: "sha256:" + strings.Repeat("b", 64), payload: "complete docker archive"}
	cooperDir := t.TempDir()
	archive, err := EnsureImageArchive(context.Background(), cooperDir, "agent:tag", runner, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(archive.Path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive.Path, []byte("short"), 0o444); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureImageArchive(context.Background(), cooperDir, "agent:tag", runner, io.Discard); err != nil {
		t.Fatal(err)
	}
	if runner.saveCalls != 2 {
		t.Fatalf("save calls = %d, want 2", runner.saveCalls)
	}
	data, err := os.ReadFile(archive.Path)
	if err != nil || string(data) != runner.payload {
		t.Fatalf("repaired archive = %q, %v", data, err)
	}
}

func TestEnsureImageArchiveRepairsMalformedMetadata(t *testing.T) {
	t.Parallel()
	runner := &imageArchiveRunner{imageID: "sha256:" + strings.Repeat("c", 64), payload: "docker archive"}
	cooperDir := t.TempDir()
	archive, err := EnsureImageArchive(context.Background(), cooperDir, "agent:tag", runner, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	metadataPath := imageArchiveMetadataPath(archive.Path)
	if err := os.Chmod(metadataPath, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadataPath, []byte("{} {}"), 0o444); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureImageArchive(context.Background(), cooperDir, "agent:tag", runner, io.Discard); err != nil {
		t.Fatal(err)
	}
	if runner.saveCalls != 2 {
		t.Fatalf("save calls = %d, want 2", runner.saveCalls)
	}
}

func TestEnsureImageArchiveRejectsInvalidImageID(t *testing.T) {
	t.Parallel()
	runner := &imageArchiveRunner{imageID: "sha256:" + strings.Repeat("/", 64), payload: "archive"}
	if _, err := EnsureImageArchive(context.Background(), t.TempDir(), "agent:tag", runner, io.Discard); err == nil {
		t.Fatal("EnsureImageArchive() accepted a non-hex image ID")
	}
}

func TestImageArchiveReusableRejectsWrongType(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), strings.Repeat("d", 64)+".tar")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	reusable, err := imageArchiveReusable(path, "sha256:"+strings.Repeat("d", 64))
	if err != nil {
		t.Fatal(err)
	}
	if reusable {
		t.Fatal("directory was accepted as an image archive")
	}
}
