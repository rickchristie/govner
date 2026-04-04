package tui

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	cooperapp "github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/clipboard"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
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

func TestExtractDroppedFilePath_QuotedPath(t *testing.T) {
	dir := t.TempDir()
	tmpFile := filepath.Join(dir, "quoted image.png")
	if err := os.WriteFile(tmpFile, []byte("fake image data"), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("'" + tmpFile + "'"), Paste: true}
	got := extractDroppedFilePath(msg)
	if got != tmpFile {
		t.Errorf("expected %q, got %q", tmpFile, got)
	}
}

func TestExtractDroppedFilePath_ShellEscapedPath(t *testing.T) {
	dir := t.TempDir()
	tmpFile := filepath.Join(dir, "shell escaped image.png")
	if err := os.WriteFile(tmpFile, []byte("fake image data"), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	escaped := strings.ReplaceAll(tmpFile, " ", "\\ ")
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(escaped), Paste: true}
	got := extractDroppedFilePath(msg)
	if got != tmpFile {
		t.Errorf("expected %q, got %q", tmpFile, got)
	}
}

func TestExtractDroppedFilePath_FileURI(t *testing.T) {
	dir := t.TempDir()
	tmpFile := filepath.Join(dir, "uri image.png")
	if err := os.WriteFile(tmpFile, []byte("fake image data"), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	uri := (&url.URL{Scheme: "file", Path: tmpFile}).String()
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(uri), Paste: true}
	got := extractDroppedFilePath(msg)
	if got != tmpFile {
		t.Errorf("expected %q, got %q", tmpFile, got)
	}
}

func TestExtractDroppedFilePath_GnomeCopiedFilesPayload(t *testing.T) {
	dir := t.TempDir()
	tmpFile := filepath.Join(dir, "copied image.png")
	if err := os.WriteFile(tmpFile, []byte("fake image data"), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	uri := (&url.URL{Scheme: "file", Path: tmpFile}).String()
	payload := "copy\n" + uri + "\n"
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(payload), Paste: true}
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
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

type recordingSubModel struct {
	messages []tea.Msg
}

func (m *recordingSubModel) Init() tea.Cmd { return nil }

func (m *recordingSubModel) Update(msg tea.Msg) (theme.SubModel, tea.Cmd) {
	m.messages = append(m.messages, msg)
	return m, nil
}

func (m *recordingSubModel) View(_, _ int) string { return "" }

func TestHandleKey_PasteStagesResolvedFilePath(t *testing.T) {
	dir := t.TempDir()
	tmpFile := filepath.Join(dir, "dragged image.png")
	if err := os.WriteFile(tmpFile, []byte("fake image data"), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	mockApp := cooperapp.NewMockApp(&config.Config{}, dir)
	mockApp.StageFileResult = &clipboard.ClipboardEvent{State: clipboard.ClipboardStaged}

	model := NewModel(mockApp)
	uri := (&url.URL{Scheme: "file", Path: tmpFile}).String()
	next, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(uri), Paste: true})
	if cmd == nil {
		t.Fatal("expected stage command")
	}

	msg := cmd()
	updated, _ := next.(*Model).Update(msg)
	finalModel := updated.(*Model)

	if len(mockApp.StagedFiles) != 1 || mockApp.StagedFiles[0] != tmpFile {
		t.Fatalf("expected staged file %q, got %#v", tmpFile, mockApp.StagedFiles)
	}
	if finalModel.clipboardState != cooperapp.ClipboardStaged {
		t.Fatalf("expected clipboard state %q, got %q", cooperapp.ClipboardStaged, finalModel.clipboardState)
	}
	if finalModel.ExitExpected() {
		t.Fatal("paste staging should not mark the model as exiting")
	}
}

func TestHandleKey_PasteFallsThroughWhenNotAFile(t *testing.T) {
	mockApp := cooperapp.NewMockApp(&config.Config{}, t.TempDir())
	recorder := &recordingSubModel{}

	model := NewModel(mockApp)
	model.SetContainersModel(recorder)

	_, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("not a file"), Paste: true})
	if cmd != nil {
		t.Fatal("expected no clipboard command for non-file paste")
	}
	if len(mockApp.StagedFiles) != 0 {
		t.Fatalf("expected no staged files, got %#v", mockApp.StagedFiles)
	}
	if len(recorder.messages) != 1 {
		t.Fatalf("expected paste to reach active model, got %d messages", len(recorder.messages))
	}
}

func TestHandleKey_CtrlVCapturesClipboard(t *testing.T) {
	mockApp := cooperapp.NewMockApp(&config.Config{}, t.TempDir())
	mockApp.CaptureClipboardResult = &clipboard.ClipboardEvent{State: clipboard.ClipboardStaged}

	model := NewModel(mockApp)
	next, cmd := model.handleKey(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd == nil {
		t.Fatal("expected capture command")
	}

	msg := cmd()
	updated, _ := next.(*Model).Update(msg)
	finalModel := updated.(*Model)

	if !mockApp.CapturedClipboard {
		t.Fatal("expected clipboard capture to be invoked")
	}
	if finalModel.clipboardState != cooperapp.ClipboardStaged {
		t.Fatalf("expected clipboard state %q, got %q", cooperapp.ClipboardStaged, finalModel.clipboardState)
	}
	if finalModel.ExitExpected() {
		t.Fatal("clipboard capture should not mark the model as exiting")
	}
}

func TestExecuteModalConfirm_ExitMarksExpectedExit(t *testing.T) {
	model := NewModel(nil)
	modal := components.NewModal(theme.ModalExit, "Exit", "Quit", "Confirm", "Cancel")

	_, cmd := model.executeModalConfirm(&modal)
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	if !model.ExitExpected() {
		t.Fatal("expected explicit exit flow to mark the model as exiting")
	}
}
