# Plan: Drag-and-Drop File Staging to Clipboard Bridge

## Problem

Users want to share images with AI CLIs running inside Cooper barrels. The
clipboard bridge already supports pressing `c` to capture the host clipboard,
but there is no way to stage a **file from disk** directly. When a user drags
and drops a file onto the `cooper up` terminal, the terminal emits the file
path as pasted text — Cooper currently ignores it.

## How Terminal Drag-and-Drop Works

When a file is dragged onto a terminal emulator (iTerm2, Kitty, Windows
Terminal, Alacritty), the terminal **pastes the absolute file path as text**.
Modern terminals wrap it in bracketed paste escape sequences:

```
ESC[200~/home/user/screenshot.pngESC[201~
```

BubbleTea v1.3.10 already parses this. It delivers a `tea.KeyMsg` with
`Paste: true` and `Runes` containing the path string. Cooper does not
currently inspect the `Paste` flag.

## Scope

**Image files only.** The existing clipboard bridge serves PNG images. Staging
arbitrary non-image files would require new bridge endpoints, shim extensions,
and AI CLIs that understand file-type clipboard content. That is out of scope.

Supported formats: PNG, JPEG, GIF, BMP, TIFF, WebP — the same set that
`clipboard.Normalize()` already handles.

Non-image files produce a user-visible error in the TUI header ("Only image
files supported").

## Design

### Detection (TUI layer)

In `Model.Update()`, before delegating to `handleKey()`, check
`msg.Paste == true`. Extract the text, trim whitespace, validate it looks like
a file path (absolute path, single line, file exists on disk). If valid,
dispatch a `stageFileCmd` that calls `App.StageFile(path)`.

### Staging (App layer)

New method `StageFile(path string)` on the `App` interface:

1. Read file bytes from disk (`os.ReadFile`).
2. Detect MIME type from magic bytes via `clipboard.DetectMIME()`.
3. Reject non-image files early with a clear error.
4. Build a `CaptureResult` and feed it into the existing
   `clipboard.Normalize()` -> `Manager.Stage()` pipeline.

This reuses all existing normalization (format conversion, size enforcement,
PNG variant generation) and staging infrastructure.

### User Feedback (TUI layer)

After staging, the TUI shows the same "Staged" indicator with countdown timer
in the header bar. On error, it shows "Failed: <reason>" for 3 seconds, same
as a failed `c` capture.

## Files Changed

| File | Change |
|------|--------|
| `internal/clipboard/normalize.go` | Add `DetectMIME()` public function |
| `internal/app/app.go` | Add `StageFile(path string)` to interface |
| `internal/app/cooper.go` | Implement `StageFile()` |
| `internal/app/mock.go` | Add mock `StageFile()` |
| `internal/app/testapp.go` | Add stub `StageFile()` |
| `internal/tui/app.go` | Paste detection in `Update()`, `stageFileCmd()` |
| `internal/tui/events/events.go` | (no new type needed — reuse `ClipboardCaptureMsg`) |

## Testing

### Unit Tests

- `TestStageFile_ValidPNG` — stage a 1x1 PNG, verify snapshot created
- `TestStageFile_ValidJPEG` — stage JPEG, verify PNG conversion
- `TestStageFile_NonImage` — stage `.txt` file, verify error
- `TestStageFile_NotFound` — non-existent path, verify error
- `TestStageFile_TooLarge` — exceed max bytes, verify error
- `TestDetectMIME` — verify magic byte detection for all supported formats
- Paste detection tested via `tea.KeyMsg` construction in TUI model tests

### Integration (E2E)

Test at the `tea.Model.Update()` level by constructing `KeyMsg{Paste: true}`
directly, avoiding the need for real terminal PTY manipulation.
