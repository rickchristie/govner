package fontsync

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testSources creates a single Source pointing at dir with the given prefix.
func testSources(dir, prefix string) []Source {
	return []Source{{Path: dir, Prefix: prefix}}
}

func TestSyncCopiesFontFiles(t *testing.T) {
	cooperDir := t.TempDir()
	srcDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(srcDir, "test.ttf"), []byte("fakefont"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := SyncFonts(testSources(srcDir, "src"), cooperDir)
	if err != nil {
		t.Fatalf("SyncFonts: %v", err)
	}

	if result.Copied != 1 {
		t.Errorf("expected 1 copied, got %d", result.Copied)
	}

	dst := filepath.Join(cooperDir, "fonts", "src", "test.ttf")
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("expected font file at %s", dst)
	}
}

func TestSyncIgnoresNonFontFiles(t *testing.T) {
	cooperDir := t.TempDir()
	srcDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(srcDir, "readme.txt"), []byte("not a font"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "real.otf"), []byte("otffont"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := SyncFonts(testSources(srcDir, "src"), cooperDir)
	if err != nil {
		t.Fatalf("SyncFonts: %v", err)
	}

	if result.Copied != 1 {
		t.Errorf("expected 1 copied, got %d", result.Copied)
	}

	notExist := filepath.Join(cooperDir, "fonts", "src", "readme.txt")
	if _, err := os.Stat(notExist); err == nil {
		t.Error("non-font file should not have been copied")
	}
}

func TestSyncPreservesSubdirectoryStructure(t *testing.T) {
	cooperDir := t.TempDir()
	srcDir := t.TempDir()

	subDir := filepath.Join(srcDir, "truetype", "dejavu")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "DejaVuSans.ttf"), []byte("dejavu"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := SyncFonts(testSources(srcDir, "src"), cooperDir)
	if err != nil {
		t.Fatalf("SyncFonts: %v", err)
	}

	if result.Copied != 1 {
		t.Errorf("expected 1 copied, got %d", result.Copied)
	}

	dst := filepath.Join(cooperDir, "fonts", "src", "truetype", "dejavu", "DejaVuSans.ttf")
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("expected font at %s", dst)
	}
}

func TestSyncPreservesUserAddedFiles(t *testing.T) {
	cooperDir := t.TempDir()
	srcDir := t.TempDir() // empty source

	// Create a user-added font in the destination.
	userFont := filepath.Join(cooperDir, "fonts", "custom", "MyFont.ttf")
	if err := os.MkdirAll(filepath.Dir(userFont), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userFont, []byte("userfont"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := SyncFonts(testSources(srcDir, "src"), cooperDir)
	if err != nil {
		t.Fatalf("SyncFonts: %v", err)
	}

	if _, err := os.Stat(userFont); err != nil {
		t.Error("user-added font should be preserved")
	}
}

func TestSyncSkipsUnchangedFiles(t *testing.T) {
	cooperDir := t.TempDir()
	srcDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(srcDir, "test.ttf"), []byte("fakefont"), 0o644); err != nil {
		t.Fatal(err)
	}

	// First sync.
	result1, err := SyncFonts(testSources(srcDir, "src"), cooperDir)
	if err != nil {
		t.Fatal(err)
	}
	if result1.Copied != 1 {
		t.Fatalf("first sync: expected 1 copied, got %d", result1.Copied)
	}

	// Second sync — file unchanged.
	result2, err := SyncFonts(testSources(srcDir, "src"), cooperDir)
	if err != nil {
		t.Fatal(err)
	}
	if result2.Skipped != 1 {
		t.Errorf("second sync: expected 1 skipped, got %d", result2.Skipped)
	}
	if result2.Copied != 0 {
		t.Errorf("second sync: expected 0 copied, got %d", result2.Copied)
	}
}

func TestSyncUpdatesChangedFiles(t *testing.T) {
	cooperDir := t.TempDir()
	srcDir := t.TempDir()

	srcPath := filepath.Join(srcDir, "test.ttf")
	if err := os.WriteFile(srcPath, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := SyncFonts(testSources(srcDir, "src"), cooperDir); err != nil {
		t.Fatal(err)
	}

	// Modify the source file (different size).
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(srcPath, []byte("v2-longer"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := SyncFonts(testSources(srcDir, "src"), cooperDir)
	if err != nil {
		t.Fatal(err)
	}
	if result.Copied != 1 {
		t.Errorf("expected 1 copied after change, got %d", result.Copied)
	}
}

func TestSyncWarnsOnMissingRoots(t *testing.T) {
	cooperDir := t.TempDir()

	sources := []Source{{Path: "/nonexistent/font/dir", Prefix: "missing"}}
	result, err := SyncFonts(sources, cooperDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Warnings) == 0 {
		t.Error("expected warnings for missing font source roots")
	}
}

func TestSyncMultipleSources(t *testing.T) {
	cooperDir := t.TempDir()
	src1 := t.TempDir()
	src2 := t.TempDir()

	if err := os.WriteFile(filepath.Join(src1, "a.ttf"), []byte("font-a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src2, "b.otf"), []byte("font-b"), 0o644); err != nil {
		t.Fatal(err)
	}

	sources := []Source{
		{Path: src1, Prefix: "src1"},
		{Path: src2, Prefix: "src2"},
	}
	result, err := SyncFonts(sources, cooperDir)
	if err != nil {
		t.Fatal(err)
	}
	if result.Copied != 2 {
		t.Errorf("expected 2 copied, got %d", result.Copied)
	}

	if _, err := os.Stat(filepath.Join(cooperDir, "fonts", "src1", "a.ttf")); err != nil {
		t.Error("missing font from first source")
	}
	if _, err := os.Stat(filepath.Join(cooperDir, "fonts", "src2", "b.otf")); err != nil {
		t.Error("missing font from second source")
	}
}

func TestLinuxSourcesReturnsExpectedPrefixes(t *testing.T) {
	sources := LinuxSources("/home/testuser")
	if len(sources) != 4 {
		t.Fatalf("expected 4 sources, got %d", len(sources))
	}

	expected := []struct {
		pathSuffix string
		prefix     string
	}{
		{".local/share/fonts", "user-local-share-fonts"},
		{".fonts", "user-dot-fonts"},
		{"/usr/local/share/fonts", "usr-local-share-fonts"},
		{"/usr/share/fonts", "usr-share-fonts"},
	}

	for i, e := range expected {
		if sources[i].Prefix != e.prefix {
			t.Errorf("source %d: expected prefix %q, got %q", i, e.prefix, sources[i].Prefix)
		}
	}
}
