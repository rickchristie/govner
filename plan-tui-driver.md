# TUI Driver Design

## Goal

Build a standalone Go library for driving terminal UIs in the same broad spirit
as Playwright drives browsers:

- start a target process in a PTY
- send input
- resize the terminal
- observe the rendered screen
- wait for conditions
- capture artifacts for assertions and debugging

The key difference from Playwright is that a terminal has no DOM, no selector
engine, and no standard accessibility tree. The driver therefore must be based
on:

- PTY process control
- ANSI/VT parsing
- a normalized screen model
- region-based assertions and waits

This library must be generic. It must not know Bubble Tea, Cooper, or any
application model. Application-specific tests can build thin wrappers on top of
the generic core.

## V1 Platform Scope

V1 is **Linux-only**.

This is now an explicit design constraint, not an implicit assumption. The
original draft was too generic about OS support. Local verification in this
workspace confirmed Linux PTY primitives and terminfo availability, but did not
verify macOS, BSD, or Windows PTY semantics.

So the implementation handoff must assume:

- Linux first
- `/dev/ptmx` or equivalent Linux PTY backend
- `TERM=xterm-256color` by default, with fallback/override support

Support for other operating systems is future work and must not be inferred as
part of V1.

## Core Position

This library is **not** a TUI framework adapter.

It is a **terminal runtime driver** with these responsibilities:

- spawn and manage processes in a PTY
- maintain a rendered screen model
- expose snapshots and region views
- provide waiting/capture primitives
- store failure artifacts

Anything that understands "focused tab", "selected row", or "active modal" is
app-specific logic built on top of the generic driver.

## Why Raw Screen Bytes Are Not Enough

We need to distinguish two different things:

1. `Transcript`
   - raw bytes read from the PTY
   - useful for debugging, replay, and low-level diagnosis
   - not stable for assertions because it contains escape sequences, cursor
     motion, and terminal control codes

2. `Snapshot`
   - normalized screen state after applying ANSI/VT behavior
   - stable for assertions
   - this is what users mean by "screenshot" for a TUI

The library must expose both.

## Requirements

### Functional Requirements

- Start a command in a PTY.
- Set an initial fixed terminal size.
- Resize the PTY later.
- Send key presses.
- Send text.
- Send control sequences.
- Optionally support mouse events later.
- Parse PTY output into a rendered screen buffer.
- Expose full-screen snapshots.
- Expose rectangular region views.
- Wait for arbitrary conditions over the full screen.
- Wait for arbitrary conditions over a region.
- Wait until the screen changes.
- Wait until a region changes.
- Wait until the screen stabilizes.
- Wait until a region stabilizes.
- Capture region changes as a sequence of `FullCapture` values.
- Save artifacts on failure.
- Work with alternate-screen TUIs.
- Work with Unicode and wide characters.

### Non-Functional Requirements

- Generic: no dependency on any app model.
- Deterministic: tests must run at fixed terminal sizes.
- Efficient: snapshot queries should not require reparsing the full transcript.
- Observable: failures must include enough artifacts to debug.
- Composable: app-specific wrappers should be easy to add.
- Safe for concurrent usage within one process.
- Safe from cross-process collisions when library-managed resources are global.

## Non-Goals

- Inferring application focus generically.
- Understanding framework-specific model state.
- OCR or raster image screenshots in V1.
- Full terminal emulator parity for every obscure control sequence in V1.
- Semantic selectors like CSS selectors.

## Explicit Design Constraints

The following items are **not** assumptions. They are explicit requirements for
the implementation. They are listed here because several of them could be
misread as "up for interpretation" if only implied by examples.

1. V1 is Linux-only.
2. All coordinate APIs are 0-based.
3. `Rect` values must be validated; invalid or out-of-bounds rects return an
   error. The driver must not silently clamp by default.
4. "Screenshot" means normalized rendered `Snapshot`, never raw PTY bytes.
5. The canonical comparison source for waits and change detection is normalized
   cell data, not raw transcript bytes and not naive string bytes.
6. Wait APIs are context-first. Timeout-only helpers may exist, but they are
   wrappers around context-based methods.
7. Fixed terminal size is mandatory for deterministic tests.
8. Generic focus inference is a non-goal. Any focus assertion belongs in an
   app-specific wrapper built on visible state.
9. The spec intentionally does not lock the implementation to a particular PTY,
   VT parser, or width-calculation dependency. Acceptance criteria matter more
   than dependency names.

## Important Design Decisions

### 1. Fixed Terminal Size Is Mandatory

All tests must choose a fixed width and height, for example `120x40`.

Without that:

- row-based assertions are usually fine
- column-based assertions become unstable
- wrapping changes test semantics
- region coordinates drift

The driver must default to a fixed size and encourage tests to resize
explicitly if needed.

### 2. The Generic Selector Is a Rectangle

The generic driver should not pretend it has semantic selectors.

The base selector is:

```go
type Rect struct {
	Row    int
	Col    int
	Width  int
	Height int
}
```

This is the correct primitive because the driver knows the screen, not the app.

Helper constructors can exist:

```go
func Row(n int, width int) Rect
func Rows(start, height, width int) Rect
func Cols(row, col, width, height int) Rect
func FromTop(rows int, screenWidth int) Rect
func FromBottom(rows int, screenWidth, screenHeight int) Rect
```

Application-specific wrappers can later define:

- `HeaderRect()`
- `FooterRect()`
- `LeftPaneRect()`
- `ModalRect()`

Normative `Rect` rules:

- `Row` and `Col` are 0-based and refer to the top-left cell of the rectangle.
- `Width` and `Height` must be positive.
- Methods that accept a `Rect` must return `ErrInvalidRect` if:
  - `Row < 0` or `Col < 0`
  - `Width <= 0` or `Height <= 0`
  - `Row+Height > Snapshot.Height`
  - `Col+Width > Snapshot.Width`
- The core library must not silently clamp invalid rectangles.
- If callers want best-effort clamping, that must be a separate helper such as
  `ClampRect`.

### 3. Focus Assertions Are Not Core

The generic library cannot know what "focus" means.

It can expose:

- rendered text
- cell styles
- cursor position
- region diffs

Then application-specific code may infer focus if the UI expresses it visually:

- inverse colors
- bold tab labels
- leading `>`
- cursor presence

The core library should not expose `AssertFocused()` or `WaitUntilFocused()`.

### 4. "Screenshot" Means Snapshot

The driver should use the word `Snapshot` in code, even if user-facing docs say
"screenshot".

`Snapshot` must represent the current rendered screen. It should be trivially
serializable to:

- plain text
- JSON
- region extracts

## Public Package Proposal

Module name:

```text
github.com/<org>/tuidriver
```

Public package layout:

```text
/driver.go
/session.go
/snapshot.go
/rect.go
/input.go
/wait.go
/capture.go
/artifact.go
/errors.go

/internal/ptyproc
/internal/vtparse
/internal/screenbuf
/internal/watch
/internal/transcript
```

Public API should remain in a single package:

```go
package tuidriver
```

This keeps the library easy to adopt.

## End-To-End Usage Example

The implementor should keep this usage shape in mind while building the API.

```go
ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
defer cancel()

sess, err := tuidriver.Start(ctx, tuidriver.ProcessSpec{
	Path: "my-tui-binary",
	Args: []string{"--mode", "test"},
	Dir:  "/tmp/work",
	Env:  []string{"NO_COLOR=1"},
}, tuidriver.Options{
	Width:  120,
	Height: 40,
	TERM:   "xterm-256color",
})
if err != nil {
	t.Fatal(err)
}
defer sess.Close()

header := tuidriver.Rect{Row: 0, Col: 0, Width: 120, Height: 1}
if err := sess.WaitUntilRegion(ctx, header, func(r tuidriver.RegionView) bool {
	return strings.Contains(r.Text(), "ready")
}); err != nil {
	var timeout *tuidriver.TimeoutError
	if errors.As(err, &timeout) {
		artifacts := tuidriver.ArtifactBundle{
			FinalSnapshot: timeout.Snapshot,
			Transcript:    sess.TranscriptTail(),
		}
		_ = artifacts.WriteDir("/tmp/tui-failure")
	}
	t.Fatal(err)
}

if err := sess.SendKey(tuidriver.KeyTab); err != nil {
	t.Fatal(err)
}
```

## Public Types

### ProcessSpec

```go
type ProcessSpec struct {
	Path string
	Args []string
	Dir  string
	Env  []string
}
```

### Options

```go
type Options struct {
	Width               int
	Height              int
	TERM                string
	Env                 []string
	TranscriptMaxBytes  int
	SnapshotHistorySize int
	PollInterval        time.Duration
	StableFor           time.Duration
}
```

Notes:

- `Width` and `Height` are required, with sane defaults.
- `TERM` defaults to `xterm-256color`.
- `TranscriptMaxBytes` bounds memory.
- `SnapshotHistorySize` keeps the last N snapshots for debugging.

### Session

`Session` is the main runtime object:

```go
type Session struct {
	// internal state hidden
}

func Start(ctx context.Context, spec ProcessSpec, opts Options) (*Session, error)
func (s *Session) Close() error
func (s *Session) PID() int
func (s *Session) Resize(width, height int) error
func (s *Session) SendKey(key Key) error
func (s *Session) SendText(text string) error
func (s *Session) SendBytes(data []byte) error
func (s *Session) Interrupt() error
func (s *Session) Wait() error
func (s *Session) TranscriptTail() Transcript
```

`TranscriptTail()` returns a bounded recent slice of the raw PTY transcript for
debugging and artifact writing. It is not a normalized screen representation.

### Key

```go
type Key struct {
	Name string
}

var (
	KeyEnter  = Key{Name: "enter"}
	KeyTab    = Key{Name: "tab"}
	KeyEsc    = Key{Name: "esc"}
	KeyUp     = Key{Name: "up"}
	KeyDown   = Key{Name: "down"}
	KeyLeft   = Key{Name: "left"}
	KeyRight  = Key{Name: "right"}
	KeyCtrlC  = Key{Name: "ctrl+c"}
	KeyCtrlV  = Key{Name: "ctrl+v"}
)
```

The library can also provide helper constructors for rune keys.

### Snapshot

```go
type Snapshot struct {
	Width         int
	Height        int
	CursorVisible bool
	CursorRow     int
	CursorCol     int
	Timestamp     time.Time
	Revision      uint64
	Lines         []string
	Cells         [][]Cell
}

func (s Snapshot) Bytes() []byte
func (s Snapshot) String() string
func (s Snapshot) Region(rect Rect) RegionView
```

`Bytes()` should return deterministic UTF-8 bytes of the normalized rendered
screen, not raw ANSI output.

Normative `Snapshot` rules:

- `Lines` length must equal `Height`.
- `Cells` outer length must equal `Height`.
- Each `Cells[row]` length must equal `Width`.
- `CursorRow` and `CursorCol` must be `-1` if `CursorVisible == false`.
- `Bytes()` must join `Lines` with `\n` and must not append a trailing newline.
- `Bytes()` must contain no ANSI escape sequences.
- `Bytes()` must preserve trailing spaces because they are visible screen state.
- `String()` must be identical to `string(Bytes())`.
- `Lines[row]` must represent visible text for that row only, without a trailing
  newline.

Important note on wide characters:

- `Lines` are a human-readable projection, not the canonical comparison source.
- With wide characters, rune count is not equal to display-cell width.
- The canonical state is `Cells`, not `Lines`.

### Cell

```go
type Cell struct {
	Text         string
	Width        int
	Continuation bool
	FG           Color
	BG           Color
	Bold         bool
	Italic       bool
	Underline    bool
	Inverse      bool
}
```

In V1, style data may be partial, but the type should exist from the start.

Normative `Cell` rules:

- `Text` is the grapheme cluster that begins at this cell, or `" "` for a
  blank visible cell.
- `Width` is display-cell width for the leading cell:
  - `1` for normal-width visible cells
  - `2` for wide visible cells
  - `0` for continuation cells
- `Continuation == true` means this cell is the continuation half of a glyph
  that began in a previous column.
- Continuation cells must have `Width == 0`.
- Blank visible cells must have `Text == " "`, `Width == 1`, and
  `Continuation == false`.

### RegionView

```go
type RegionView struct {
	Rect      Rect
	Timestamp time.Time
	Revision  uint64
	Lines     []string
	Cells     [][]Cell
}

func (r RegionView) Text() string
func (r RegionView) Bytes() []byte
func (r RegionView) Hash(mode CompareMode) string
```

Normative `RegionView` rules:

- `Text()` must be identical to `string(Bytes())`.
- `Bytes()` follows the same normalization rules as `Snapshot.Bytes()`.
- `Hash(mode)` must hash normalized cell data, not raw `Bytes()`, because
  string bytes are not sufficient for wide-character-safe canonical comparison.

### Transcript

```go
type Transcript struct {
	StartOffset int64
	EndOffset   int64
	Bytes       []byte
}
```

Transcript slices should normally be **incremental**, not the full PTY history,
to avoid quadratic memory blowups.

Normative `Transcript` rules:

- `Bytes` are raw PTY bytes exactly as observed.
- No ANSI decoding, stripping, or normalization is allowed in `Transcript`.
- `StartOffset` and `EndOffset` refer to monotonically increasing raw-byte
  offsets in the session transcript stream.
- Unless a method explicitly says otherwise, transcript attached to a
  `FullCapture` is the delta since the previous relevant capture.

### FullCapture

This is the key composite type for debugging and change capture:

```go
type FullCapture struct {
	At         time.Time
	Snapshot   Snapshot
	Region     RegionView
	Transcript Transcript
}
```

Important note:

- `Snapshot` is the full rendered screen.
- `Region` is the extracted rect that triggered interest.
- `Transcript` is the transcript delta since the previous capture by default.

That keeps captures useful without exploding memory usage.

### Canonical Comparison Semantics

This section is intentionally explicit because it is easy for an implementation
to drift here.

`CompareText` must hash row-major normalized cells using only:

- `Cell.Text`
- `Cell.Width`
- `Cell.Continuation`

`CompareTextAndStyle` must hash everything from `CompareText` plus:

- `FG`
- `BG`
- `Bold`
- `Italic`
- `Underline`
- `Inverse`

`CompareCells` must hash the full normalized cell matrix for the region.

The implementation must **not** define `CompareText` as `sha256(region.Bytes())`
because:

- wide characters make rune bytes insufficient as a canonical cell encoding
- continuation cells matter
- visually significant blank cells and trailing spaces must be preserved

Reference logic:

```go
func hashRegionCells(cells [][]Cell, mode CompareMode) string {
	h := sha256.New()
	for _, row := range cells {
		for _, cell := range row {
			io.WriteString(h, cell.Text)
			io.WriteString(h, "\x1f")
			fmt.Fprintf(h, "%d|%t", cell.Width, cell.Continuation)
			if mode >= CompareTextAndStyle {
				fmt.Fprintf(h, "|%v|%v|%t|%t|%t|%t",
					cell.FG, cell.BG, cell.Bold, cell.Italic, cell.Underline, cell.Inverse,
				)
			}
			if mode >= CompareCells {
				io.WriteString(h, "|cell-end")
			}
			io.WriteString(h, "\x1e")
		}
		io.WriteString(h, "\n")
	}
	return hex.EncodeToString(h.Sum(nil))
}
```

## Wait API

The canonical API should be context-based.

Do not make timeout-only methods the primary form. Timeouts should be
expressible through `context.Context`.

### Core Waits

```go
func (s *Session) Snapshot() Snapshot
func (s *Session) WaitUntil(ctx context.Context, fn func(Snapshot) bool) error
func (s *Session) WaitUntilRegion(ctx context.Context, rect Rect, fn func(RegionView) bool) error
func (s *Session) WaitUntilScreenChanged(ctx context.Context) (Snapshot, error)
func (s *Session) WaitUntilRegionChanged(ctx context.Context, rect Rect, opts ChangeOptions) (FullCapture, error)
func (s *Session) WaitUntilScreenStable(ctx context.Context, quietFor time.Duration) (Snapshot, error)
func (s *Session) WaitUntilRegionStable(ctx context.Context, rect Rect, quietFor time.Duration, opts ChangeOptions) (FullCapture, error)
```

Wait contract:

- `WaitUntil...` methods must first evaluate the current snapshot before
  subscribing for future changes.
- Waits must never busy-loop.
- Waits must unblock on context cancellation even if the screen never changes.
- If a wait times out after collecting useful intermediate state, the returned
  error must contain the latest `Snapshot`.
- Change-based waits must deduplicate repeated redraws that do not change the
  region hash under the chosen `CompareMode`.

### CompareMode

```go
type CompareMode int

const (
	CompareText CompareMode = iota
	CompareTextAndStyle
	CompareCells
)
```

Why this matters:

- some waits only care that text changed
- some waits care that style changed
- some waits care about all cell-level changes

### ChangeOptions

```go
type ChangeOptions struct {
	Compare        CompareMode
	IncludeInitial bool
}
```

Semantics:

- baseline is the current region unless `IncludeInitial` is set
- changed means hash differs under the chosen compare mode

## Region Change Capture API

This requirement is good and should be in V1.

### High-Level Requirement

We need an API that captures each meaningful change in a region as a sequence of
`FullCapture` values, until either:

- the caller says stop
- the context times out or is canceled

This is especially useful for:

- progress bars
- countdown timers
- staged transitions
- loading spinners
- step-by-step screen flows

### Recommended API

The primary version should be context-based:

```go
type ContinueFunc func(FullCapture) bool

type CaptureOptions struct {
	Compare        CompareMode
	IncludeInitial bool
	MaxCaptures    int
}

func (s *Session) CaptureRegionChanges(
	ctx context.Context,
	rect Rect,
	shouldContinue ContinueFunc,
	opts CaptureOptions,
) ([]FullCapture, error)
```

Convenience wrapper if a timeout-only call is desired:

```go
func (s *Session) CaptureRegionChangesTimeout(
	rect Rect,
	shouldContinue ContinueFunc,
	timeout time.Duration,
	opts CaptureOptions,
) ([]FullCapture, error)
```

### Semantics

- Compute a baseline region hash at start.
- Optionally emit the initial capture if `IncludeInitial` is true.
- Subscribe to screen changes.
- When the region hash changes, create a `FullCapture`.
- Append it to the result slice.
- Call `shouldContinue(capture)`.
- If `shouldContinue` returns `false`, return the captures collected so far.
- If `MaxCaptures > 0` and limit is reached, return.
- If context expires, return partial captures plus timeout error.

### Important Memory Note

`CaptureRegionChanges(... ) []FullCapture` is convenient, but not always safe
for long-running or noisy regions. The library should therefore also expose a
streaming version:

```go
func (s *Session) ObserveRegionChanges(
	ctx context.Context,
	rect Rect,
	opts CaptureOptions,
	fn func(FullCapture) bool,
) error
```

`CaptureRegionChanges` can be implemented as a thin wrapper around
`ObserveRegionChanges`.

This is the right balance:

- easy API for common tests
- scalable API for long-running observations

Stream contract:

- `ObserveRegionChanges` stops when:
  - `fn(capture)` returns `false`
  - context is canceled or times out
  - the session closes
- `CaptureRegionChanges` collects the same captures into a slice by delegating
  to `ObserveRegionChanges`.
- Repeated redraws that produce the same region hash must not emit duplicate
  captures.

## Assertion Helpers

The generic package should provide basic helpers, but no app semantics:

```go
func AssertRegionContains(s Snapshot, rect Rect, substr string) error
func AssertRegionMatches(s Snapshot, rect Rect, re *regexp.Regexp) error
func AssertLineContains(s Snapshot, row int, substr string) error
```

Prefer keeping assertions as optional helpers, not required for the driver to
be useful.

## Artifact Model

When a wait fails, the caller should be able to save useful artifacts.

### ArtifactBundle

```go
type ArtifactBundle struct {
	FinalSnapshot Snapshot
	Transcript    Transcript
	Recent        []FullCapture
}

func (a ArtifactBundle) WriteDir(path string) error
```

Recommended output files:

- `final.txt`
  - normalized screen text
- `final.json`
  - structured snapshot
- `transcript.log`
  - PTY transcript bytes
- `captures/0001.txt`, `captures/0001.json`
  - recent captures for diffing

Recommended JSON shape:

```json
{
  "width": 120,
  "height": 40,
  "cursorVisible": false,
  "cursorRow": -1,
  "cursorCol": -1,
  "revision": 42,
  "timestamp": "2026-04-05T23:10:00Z",
  "lines": [
    "header line here ...",
    "..."
  ]
}
```

Recommended `FullCapture` JSON shape:

```json
{
  "at": "2026-04-05T23:10:01Z",
  "region": {
    "row": 10,
    "col": 2,
    "width": 60,
    "height": 1,
    "text": "Progress: 40%"
  },
  "transcript": {
    "startOffset": 1024,
    "endOffset": 1088
  },
  "snapshotRevision": 43
}
```

## Internal Architecture

### Main Components

1. PTY process manager
   - starts child process
   - owns PTY file descriptor
   - handles resize
   - handles input writes

2. Transcript recorder
   - appends raw bytes into bounded storage
   - serves incremental transcript slices

3. VT parser
   - interprets ANSI/VT sequences
   - updates the screen buffer

4. Screen buffer
   - current cell matrix
   - cursor position
   - revision number
   - dirty regions

5. Watch manager
   - receives dirty notifications
   - wakes waiters and region observers

6. Snapshot service
   - clones current buffer into immutable `Snapshot`
   - extracts `RegionView`

### Reference `Session` Shape

The exact private struct may differ, but an implementation in this family is
expected:

```go
type Session struct {
	proc   *os.Process
	pty    *os.File
	waitCh chan struct{}

	mu         sync.RWMutex
	closed     bool
	closeErr   error
	opts       Options
	screen     *screenbuf.Buffer
	parser     *vtparse.Parser
	transcript *transcript.Ring
	watchers   *watch.Hub
	revision   uint64
}
```

Important invariants:

- `mu` protects screen state snapshots and session lifecycle flags.
- parser application and screen mutation are serialized.
- snapshots are immutable copies.
- watcher notifications only occur after a full parser application step.

### Dependency Policy

The spec deliberately does not require a specific implementation dependency, but
it does require capability classes.

Required capability classes:

- PTY process control
- VT/ANSI parsing
- display-width calculation for grapheme clusters

Acceptable implementation options:

- a mature PTY package such as `creack/pty`, or direct syscall/x/sys code
- a mature VT parser, or a carefully scoped custom parser
- a width library such as `go-runewidth`, or equivalent correctness-tested code

What matters is not the package name. What matters is satisfying the acceptance
tests and normalization contracts in this document.

### Concurrency Model

Recommended goroutines:

- reader goroutine
  - reads bytes from PTY
  - appends to transcript
  - feeds parser
- state goroutine
  - applies parser mutations to screen buffer
  - increments revision
  - publishes dirty events
- waiter goroutines
  - block on dirty events or context cancellation

The public `Session` methods should be safe for concurrent use.

## Important Logic: PTY Reader Loop

Illustrative logic:

```go
func (s *Session) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			s.transcript.Append(chunk)
			dirty := s.parser.Apply(chunk, s.screen)
			if dirty.Any() {
				s.screen.Revision++
				s.watchers.Notify(dirty, s.screen.Revision, s.transcript.EndOffset())
			}
		}
		if err != nil {
			s.watchers.Close(err)
			return
		}
	}
}
```

## Important Logic: WaitUntilRegion

Illustrative logic:

```go
func (s *Session) WaitUntilRegion(
	ctx context.Context,
	rect Rect,
	fn func(RegionView) bool,
) error {
	region := s.Snapshot().Region(rect)
	if fn(region) {
		return nil
	}

	sub := s.watchers.Subscribe(rect)
	defer sub.Close()

	for {
		select {
		case <-ctx.Done():
			return &TimeoutError{
				Kind:     "wait-until-region",
				Rect:     rect,
				Snapshot: s.Snapshot(),
				Err:      ctx.Err(),
			}
		case <-sub.C():
			region = s.Snapshot().Region(rect)
			if fn(region) {
				return nil
			}
		}
	}
}
```

## Important Logic: WaitUntilRegionChanged

```go
func (s *Session) WaitUntilRegionChanged(
	ctx context.Context,
	rect Rect,
	opts ChangeOptions,
) (FullCapture, error) {
	base := s.Snapshot()
	baseRegion := base.Region(rect)
	baseHash := baseRegion.Hash(opts.Compare)
	lastOffset := s.transcript.EndOffset()

	if opts.IncludeInitial {
		return FullCapture{
			At:         time.Now(),
			Snapshot:   base,
			Region:     baseRegion,
			Transcript: s.transcript.Slice(lastOffset, lastOffset),
		}, nil
	}

	sub := s.watchers.Subscribe(rect)
	defer sub.Close()

	for {
		select {
		case <-ctx.Done():
			return FullCapture{}, &TimeoutError{
				Kind:     "wait-until-region-changed",
				Rect:     rect,
				Snapshot: s.Snapshot(),
				Err:      ctx.Err(),
			}
		case <-sub.C():
			snap := s.Snapshot()
			region := snap.Region(rect)
			hash := region.Hash(opts.Compare)
			if hash == baseHash {
				continue
			}
			capture := FullCapture{
				At:         time.Now(),
				Snapshot:   snap,
				Region:     region,
				Transcript: s.transcript.Slice(lastOffset, s.transcript.EndOffset()),
			}
			return capture, nil
		}
	}
}
```

## Important Logic: CaptureRegionChanges

```go
func (s *Session) CaptureRegionChanges(
	ctx context.Context,
	rect Rect,
	shouldContinue ContinueFunc,
	opts CaptureOptions,
) ([]FullCapture, error) {
	var captures []FullCapture

	snap := s.Snapshot()
	region := snap.Region(rect)
	lastHash := region.Hash(opts.Compare)
	lastOffset := s.transcript.EndOffset()

	if opts.IncludeInitial {
		initial := FullCapture{
			At:         time.Now(),
			Snapshot:   snap,
			Region:     region,
			Transcript: s.transcript.Slice(lastOffset, lastOffset),
		}
		captures = append(captures, initial)
		if !shouldContinue(initial) {
			return captures, nil
		}
	}

	sub := s.watchers.Subscribe(rect)
	defer sub.Close()

	for {
		select {
		case <-ctx.Done():
			return captures, &TimeoutError{
				Kind:     "capture-region-changes",
				Rect:     rect,
				Snapshot: s.Snapshot(),
				Err:      ctx.Err(),
			}
		case <-sub.C():
			snap = s.Snapshot()
			region = snap.Region(rect)
			hash := region.Hash(opts.Compare)
			if hash == lastHash {
				continue
			}

			capture := FullCapture{
				At:         time.Now(),
				Snapshot:   snap,
				Region:     region,
				Transcript: s.transcript.Slice(lastOffset, s.transcript.EndOffset()),
			}
			captures = append(captures, capture)
			lastHash = hash
			lastOffset = s.transcript.EndOffset()

			if opts.MaxCaptures > 0 && len(captures) >= opts.MaxCaptures {
				return captures, nil
			}
			if !shouldContinue(capture) {
				return captures, nil
			}
		}
	}
}
```

## TimeoutError

Timeouts must be debuggable.

```go
type TimeoutError struct {
	Kind     string
	Rect     Rect
	Snapshot Snapshot
	Err      error
}

func (e *TimeoutError) Error() string
```

Every timeout must return the latest snapshot so callers can write artifacts.

Reference error values:

```go
var (
	ErrClosed      = errors.New("session closed")
	ErrInvalidRect = errors.New("invalid rect")
	ErrNoSnapshot  = errors.New("snapshot unavailable")
)
```

## Unicode and Cell Width

The screen model must handle:

- ASCII
- UTF-8
- wide characters
- combining marks

At minimum, V1 must use display-cell width correctly. A region is a rectangle in
display cells, not raw bytes and not raw rune count.

This is mandatory for robust assertions.

Implementation note:

- If the chosen width library cannot correctly model grapheme clusters and East
  Asian wide characters, the implementation is not acceptable for V1.
- Width correctness must be enforced by tests, not just dependency choice.

## Alternate Screen and Scrollback

The driver must work with alternate-screen applications, because most TUIs use
it. V1 does **not** need full scrollback modeling for assertions.

Recommended behavior:

- `Snapshot` always represents the current visible screen
- transcript preserves the raw PTY history
- scrollback support can be added later if needed

## Stability Waits

Many TUIs redraw in bursts. A single change is often not enough to assert.

So the library should support "stable for N milliseconds":

```go
func (s *Session) WaitUntilRegionStable(
	ctx context.Context,
	rect Rect,
	quietFor time.Duration,
	opts ChangeOptions,
) (FullCapture, error)
```

Semantics:

- observe changes in the region
- once the region has remained unchanged for `quietFor`, return the latest
  capture

This is more reliable than sleeping after input.

## Acceptance Tests

The implementation session should treat the following as mandatory acceptance
tests, not optional polish.

1. `Snapshot.Bytes()` contains no ANSI sequences and preserves trailing spaces.
2. `Rect` validation rejects negative and out-of-bounds rectangles.
3. `WaitUntilRegion` evaluates the current state before waiting for changes.
4. `WaitUntilRegionChanged` ignores redraws that do not change the selected
   region hash.
5. `CaptureRegionChanges` returns partial captures on timeout together with an
   error.
6. `ObserveRegionChanges` stops when callback returns `false`.
7. `Transcript` preserves raw `\r` and ESC bytes exactly.
8. Alternate-screen enter/leave sequences do not break snapshot collection.
9. Resizing changes `Snapshot.Width` and `Snapshot.Height`.
10. Wide-character regions compare correctly under `CompareText`.
11. `CompareTextAndStyle` distinguishes style-only changes.
12. Artifact writing produces `final.txt`, `final.json`, and transcript output.
13. Multiple concurrent waiters on different regions do not race.
14. Session cancellation unblocks waits even if the process stops drawing.

## Example: Header Assertion

Goal:

- header is row 0
- assert it contains `clipboard staged`
- assert it contains a seconds value

```go
header := tuidriver.Rect{Row: 0, Col: 0, Width: 120, Height: 1}
ttlRe := regexp.MustCompile(`\b[0-9]+s\b`)

err := sess.WaitUntilRegion(ctx, header, func(r tuidriver.RegionView) bool {
	line := r.Text()
	return strings.Contains(line, "clipboard staged") && ttlRe.MatchString(line)
})
```

## Example: Progress Bar Capture

Goal:

- keep captures until progress reaches 100%
- later assert the sequence changed over time

```go
progress := tuidriver.Rect{Row: 10, Col: 2, Width: 60, Height: 1}

captures, err := sess.CaptureRegionChanges(ctx, progress, func(fc tuidriver.FullCapture) bool {
	return !strings.Contains(fc.Region.Text(), "100%")
}, tuidriver.CaptureOptions{
	Compare:     tuidriver.CompareText,
	MaxCaptures: 100,
})
```

Then tests can assert:

- at least 2 captures
- first and last differ
- final region contains `100%`

## App-Specific Layering

This library should remain generic. Application-specific wrappers can be built
like this:

```go
type CooperScreen struct {
	S *tuidriver.Session
}

func (c CooperScreen) Header() tuidriver.Rect {
	return tuidriver.Rect{Row: 0, Col: 0, Width: 120, Height: 1}
}

func (c CooperScreen) WaitForClipboardStaged(ctx context.Context) error {
	return c.S.WaitUntilRegion(ctx, c.Header(), func(r tuidriver.RegionView) bool {
		return strings.Contains(r.Text(), "clipboard staged")
	})
}
```

This is the correct place for semantics like:

- "header"
- "settings panel"
- "blocked list"
- "active tab"

## Recommended V1 Scope

V1 should include:

- PTY start/stop
- resize
- send key/text/bytes
- transcript recording
- snapshot model
- region extraction
- wait until predicate
- wait until region changed
- wait until region stable
- capture region changes
- artifact writing
- timeout errors with last snapshot

V1 should explicitly exclude:

- mouse support
- semantic selectors
- built-in focus assertions
- scrollback assertions
- framework-specific adapters

## Future Work

Potential V2 additions:

- streaming capture APIs with channels/iterators
- mouse support
- scrollback model
- style-aware diff tools
- golden snapshot tooling
- framework adapters
- semantic accessibility hints if some apps opt into them

## Assumptions And Verification Log

This section is part of the handoff on purpose. It separates:

- verified environment/runtime assumptions
- assumptions that were found to be too loose
- plan adjustments made as a result

### Verified Assumptions

1. **Linux PTY primitives are available in the current implementation
   environment.**
   - Evidence:
     - `go env GOOS GOARCH GOVERSION` returned:
       - `linux`
       - `amd64`
       - `go1.24.10`
     - `stat /dev/ptmx` succeeded and showed `/dev/ptmx -> pts/ptmx`
   - Result:
     - Verified.
   - Impact on plan:
     - V1 explicitly targets Linux.

2. **A default TERM of `xterm-256color` is reasonable in the current
   environment.**
   - Evidence:
     - `infocmp xterm-256color` succeeded.
     - `infocmp xterm` also succeeded.
   - Result:
     - Verified locally.
   - Impact on plan:
     - Keep `xterm-256color` as the default.
     - Require override/fallback support because terminfo availability may vary
       across hosts.

3. **Raw PTY transcript bytes are not suitable for direct assertions because
   control bytes are preserved.**
   - Evidence:
     - `script -qfec "printf 'hello\\rworld\\n'" ...` produced transcript bytes
       containing `0d` carriage return in the raw stream.
     - Human-visible command output was `hello\rworld`, while the final visible
       line after terminal interpretation would be `world`.
   - Result:
     - Verified.
   - Impact on plan:
     - `Transcript` and `Snapshot` remain separate first-class concepts.

4. **Alternate-screen enter/leave sequences appear in the raw output stream and
   must be handled by the parser.**
   - Evidence:
     - `script -qfec "printf '\\033[?1049hALT\\033[?1049l'" ...` captured raw
       bytes:
       - `1b 5b 3f 31 30 34 39 68` (`ESC[?1049h`)
       - `1b 5b 3f 31 30 34 39 6c` (`ESC[?1049l`)
   - Result:
     - Verified.
   - Impact on plan:
     - Alternate-screen handling is mandatory in V1.

5. **Terminal size can be controlled inside a PTY session and therefore fixed
   session sizing is a real runtime capability, not just a testing convention.**
   - Evidence:
     - `script -qfec "stty cols 91 rows 17; stty size" ...` printed `17 91`.
   - Result:
     - Verified.
   - Impact on plan:
     - Fixed width/height remains a hard requirement for deterministic tests.

6. **The current workspace already carries a width-related dependency, but not a
   clear PTY/parser choice.**
   - Evidence:
     - `cooper/go.mod` includes `github.com/mattn/go-runewidth` indirectly.
     - The workspace scan did not establish an existing PTY or VT parser
       dependency that should be treated as mandatory for the standalone
       library.
   - Result:
     - Partially verified.
   - Impact on plan:
     - Keep dependency policy open.
     - Require capability classes and acceptance tests instead of a package-name
       mandate.

### Assumptions That Were Incorrect Or Too Loose

1. **Initial assumption: the library spec could remain cross-platform in V1.**
   - Status:
     - Incorrect / too loose.
   - Reason:
     - Only Linux PTY behavior was actually verified.
   - Plan adjustment:
     - V1 is now explicitly Linux-only.

2. **Initial assumption: string bytes could serve as the canonical comparison
   basis for region change detection.**
   - Status:
     - Incorrect / too loose.
   - Reason:
     - Wide characters and continuation cells make naive string hashing
       ambiguous.
   - Plan adjustment:
     - Canonical compare/hash semantics are now cell-based.

3. **Initial assumption: a specific PTY or VT parser dependency might be safe to
   imply.**
   - Status:
     - Incorrect / too loose.
   - Reason:
     - No existing mandatory dependency choice was verified in this workspace,
       and the library is meant to be standalone.
   - Plan adjustment:
     - Dependency choice remains open, constrained by capability requirements
       and acceptance tests.

4. **Initial assumption: generic focus assertions might belong in the core
   driver.**
   - Status:
     - Incorrect.
   - Reason:
     - The driver cannot know app-level focus semantics from PTY state alone.
   - Plan adjustment:
     - Focus inference is an explicit non-goal for the generic core.

## Final Recommendation

Build this as a generic package around **rendered screen state**, not around raw
output bytes.

The winning abstractions are:

- `Session`
- `Snapshot`
- `RegionView`
- `Transcript`
- `FullCapture`
- `Rect`
- `WaitUntil...`
- `CaptureRegionChanges(...)`

That gives us a generic TUI runtime driver that is useful on its own, and also
gives future app-specific test suites a solid base to build real end-to-end TUI
tests.
