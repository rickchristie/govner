package clipboard

import (
	"time"
)

// ClipboardKind identifies the semantic type of a staged clipboard object.
// The core is generic; v1 capture policy accepts only image objects.
type ClipboardKind string

const (
	ClipboardKindImage   ClipboardKind = "image"
	ClipboardKindText    ClipboardKind = "text"
	ClipboardKindFile    ClipboardKind = "file"
	ClipboardKindUnknown ClipboardKind = "unknown"
)

// ClipboardVariant holds a derived representation of clipboard content
// keyed by MIME type. For v1, the primary variant is image/png.
type ClipboardVariant struct {
	MIME   string
	Bytes  []byte
	Size   int64
	Width  int
	Height int
}

// ClipboardObject is the generic staged clipboard envelope. It preserves
// original bytes and clipboard metadata alongside derived variants.
type ClipboardObject struct {
	Kind            ClipboardKind
	MIME            string
	Filename        string
	Extension       string
	Raw             []byte
	RawSize         int64
	OriginalTargets []string
	Variants        map[string]ClipboardVariant
}

// StagedSnapshot is an immutable staged clipboard grant. Once published,
// no field is mutated. Readers capture the pointer once and operate on it
// without races.
type StagedSnapshot struct {
	ID           string
	Object       ClipboardObject
	CreatedAt    time.Time
	ExpiresAt    time.Time
	LastAccessAt time.Time
	AccessCount  int
}

// IsExpired returns true if the snapshot has passed its expiry time.
func (s *StagedSnapshot) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// RemainingTTL returns the duration until expiry, or zero if already expired.
func (s *StagedSnapshot) RemainingTTL() time.Duration {
	remaining := time.Until(s.ExpiresAt)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// CaptureResult is returned by the clipboard Reader after reading the
// host clipboard. It contains the raw bytes and metadata before any
// normalization or conversion.
type CaptureResult struct {
	MIME            string
	Filename        string
	Extension       string
	Bytes           []byte
	OriginalTargets []string
}

// BarrelSession tracks a running barrel's clipboard eligibility and token.
type BarrelSession struct {
	Token         string
	ContainerName string
	ToolName      string
	ClipboardMode string // auto, shim, x11, off
	Eligible      bool
}

// ClipboardState represents the TUI-visible clipboard state.
type ClipboardState int

const (
	ClipboardEmpty   ClipboardState = iota
	ClipboardStaged
	ClipboardExpired
	ClipboardFailed
)

// String returns a human-readable name for the clipboard state.
func (s ClipboardState) String() string {
	switch s {
	case ClipboardEmpty:
		return "empty"
	case ClipboardStaged:
		return "staged"
	case ClipboardExpired:
		return "expired"
	case ClipboardFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// ClipboardEvent is emitted to the TUI when clipboard state changes.
type ClipboardEvent struct {
	State   ClipboardState
	Error   string // non-empty on ClipboardFailed
	Snapshot *StagedSnapshot // non-nil on ClipboardStaged
}
