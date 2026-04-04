package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestExtractDroppedFilePath_ValidFile(t *testing.T) {
	dir := t.TempDir()
	tmpFile := filepath.Join(dir, "test.png")
	if err := os.WriteFile(tmpFile, []byte("fake image data"), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tmpFile), Paste: true}
	got := extractDroppedFilePath(msg)
	if got != tmpFile {
		t.Errorf("expected %q, got %q", tmpFile, got)
	}
}

func TestExtractDroppedFilePath_NonExistentPath(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/tmp/does-not-exist-ever-12345.png"), Paste: true}
	got := extractDroppedFilePath(msg)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestExtractDroppedFilePath_RelativePath(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("relative/path.png"), Paste: true}
	got := extractDroppedFilePath(msg)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestExtractDroppedFilePath_MultiLine(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/path/one\n/path/two"), Paste: true}
	got := extractDroppedFilePath(msg)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestExtractDroppedFilePath_EmptyPaste(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(""), Paste: true}
	got := extractDroppedFilePath(msg)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestExtractDroppedFilePath_WhitespaceOnly(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("   \t  "), Paste: true}
	got := extractDroppedFilePath(msg)
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestExtractDroppedFilePath_PathWithSpaces(t *testing.T) {
	dir := t.TempDir()
	spacedDir := filepath.Join(dir, "path with spaces")
	if err := os.MkdirAll(spacedDir, 0755); err != nil {
		t.Fatalf("failed to create directory with spaces: %v", err)
	}
	tmpFile := filepath.Join(spacedDir, "my file.png")
	if err := os.WriteFile(tmpFile, []byte("fake image data"), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tmpFile), Paste: true}
	got := extractDroppedFilePath(msg)
	if got != tmpFile {
		t.Errorf("expected %q, got %q", tmpFile, got)
	}
}

func TestExtractDroppedFilePath_Directory(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "somedir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create temp directory: %v", err)
	}

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(subDir), Paste: true}
	got := extractDroppedFilePath(msg)
	if got != subDir {
		t.Errorf("expected %q, got %q", subDir, got)
	}
}
