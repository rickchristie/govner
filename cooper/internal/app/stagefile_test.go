package app

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rickchristie/govner/cooper/internal/clipboard"
	"github.com/rickchristie/govner/cooper/internal/config"
)

// testStageApp creates a minimal CooperApp with only the clipboard manager
// initialized — enough to exercise StageFile without Docker or proxy.
func testStageApp(t *testing.T, maxBytes int) *CooperApp {
	t.Helper()
	mgr := clipboard.NewManager(5*time.Minute, maxBytes)
	cfg := &config.Config{
		ClipboardTTLSecs:  300,
		ClipboardMaxBytes: maxBytes,
	}
	return &CooperApp{
		cfg:              cfg,
		clipboardManager: mgr,
	}
}

// encodePNG returns a valid 1x1 PNG image as bytes.
func encodePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	return buf.Bytes()
}

// encodeJPEG returns a valid 1x1 JPEG image as bytes.
func encodeJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{G: 255, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode JPEG: %v", err)
	}
	return buf.Bytes()
}

func TestStageFile_ValidPNG(t *testing.T) {
	app := testStageApp(t, 10*1024*1024)

	dir := t.TempDir()
	path := filepath.Join(dir, "test.png")
	if err := os.WriteFile(path, encodePNG(t), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	ev, err := app.StageFile(path)
	if err != nil {
		t.Fatalf("StageFile returned error: %v", err)
	}
	if ev.State != clipboard.ClipboardStaged {
		t.Fatalf("expected state Staged, got %s", ev.State)
	}
	if ev.Snapshot == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if ev.Snapshot.Object.Kind != clipboard.ClipboardKindImage {
		t.Errorf("expected kind image, got %s", ev.Snapshot.Object.Kind)
	}
	if _, ok := ev.Snapshot.Object.Variants["image/png"]; !ok {
		t.Error("expected image/png variant in snapshot")
	}
}

func TestStageFile_ValidJPEG(t *testing.T) {
	app := testStageApp(t, 10*1024*1024)

	dir := t.TempDir()
	path := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(path, encodeJPEG(t), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	ev, err := app.StageFile(path)
	if err != nil {
		t.Fatalf("StageFile returned error: %v", err)
	}
	if ev.State != clipboard.ClipboardStaged {
		t.Fatalf("expected state Staged, got %s", ev.State)
	}
	if ev.Snapshot == nil {
		t.Fatal("expected non-nil snapshot")
	}
	// JPEG input must be converted to PNG variant.
	if _, ok := ev.Snapshot.Object.Variants["image/png"]; !ok {
		t.Error("expected image/png variant after JPEG conversion")
	}
}

func TestStageFile_NonImage(t *testing.T) {
	app := testStageApp(t, 10*1024*1024)

	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	ev, err := app.StageFile(path)
	if err == nil {
		t.Fatal("expected error for non-image file")
	}
	if ev.State != clipboard.ClipboardFailed {
		t.Fatalf("expected state Failed, got %s", ev.State)
	}
	if !strings.Contains(ev.Error, "only image files") {
		t.Errorf("expected error to mention 'only image files', got: %s", ev.Error)
	}
}

func TestStageFile_NotFound(t *testing.T) {
	app := testStageApp(t, 10*1024*1024)

	ev, err := app.StageFile("/tmp/does-not-exist-ever-12345.png")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
	if ev.State != clipboard.ClipboardFailed {
		t.Fatalf("expected state Failed, got %s", ev.State)
	}
	if !strings.Contains(ev.Error, "read file") {
		t.Errorf("expected error to mention 'read file', got: %s", ev.Error)
	}
}

func TestStageFile_TooLarge(t *testing.T) {
	// Use a very small maxBytes so even a tiny PNG exceeds the limit.
	app := testStageApp(t, 10)

	dir := t.TempDir()
	path := filepath.Join(dir, "big.png")
	if err := os.WriteFile(path, encodePNG(t), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	ev, err := app.StageFile(path)
	if err == nil {
		t.Fatal("expected error for oversized image")
	}
	if ev.State != clipboard.ClipboardFailed {
		t.Fatalf("expected state Failed, got %s", ev.State)
	}
	if !strings.Contains(ev.Error, "exceeds") {
		t.Errorf("expected error to mention 'exceeds', got: %s", ev.Error)
	}
}
