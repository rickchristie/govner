package model

import (
	"strings"
)

// LogBuffer is a shared append-only buffer for all test output.
// All output strings are stored in a single contiguous buffer to avoid duplication.
type LogBuffer struct {
	data []byte
}

// NewLogBuffer creates a new empty log buffer
func NewLogBuffer() *LogBuffer {
	return &LogBuffer{
		data: make([]byte, 0, 1024*1024), // Pre-allocate 1MB
	}
}

// Append adds output to the buffer and returns the BufferRef
func (b *LogBuffer) Append(output string) BufferRef {
	start := len(b.data)
	b.data = append(b.data, output...)
	return BufferRef{Start: start, End: len(b.data)}
}

// Slice returns the string for a BufferRef
func (b *LogBuffer) Slice(ref BufferRef) string {
	if ref.Start >= ref.End || ref.Start < 0 || ref.End > len(b.data) {
		return ""
	}
	return string(b.data[ref.Start:ref.End])
}

// SliceBytes returns the bytes for a BufferRef without allocation
func (b *LogBuffer) SliceBytes(ref BufferRef) []byte {
	if ref.Start >= ref.End || ref.Start < 0 || ref.End > len(b.data) {
		return nil
	}
	return b.data[ref.Start:ref.End]
}

// Len returns current buffer length
func (b *LogBuffer) Len() int {
	return len(b.data)
}

// BufferRef points to a slice of the shared buffer
type BufferRef struct {
	Start int // Inclusive
	End   int // Exclusive
}

// NodeLog manages log references for a single TestNode.
// Contains refs to the shared buffer in chronological order.
type NodeLog struct {
	Refs []BufferRef
}

// NewNodeLog creates a new empty NodeLog
func NewNodeLog() *NodeLog {
	return &NodeLog{
		Refs: make([]BufferRef, 0, 16), // Pre-allocate for typical output
	}
}

// Append adds a BufferRef to this node's log
func (nl *NodeLog) Append(ref BufferRef) {
	nl.Refs = append(nl.Refs, ref)
}

// TotalSize returns total bytes across all refs
func (nl *NodeLog) TotalSize() int {
	total := 0
	for _, ref := range nl.Refs {
		total += ref.End - ref.Start
	}
	return total
}

// LastEnd returns the end position of the last ref (0 if empty)
func (nl *NodeLog) LastEnd() int {
	if len(nl.Refs) == 0 {
		return 0
	}
	return nl.Refs[len(nl.Refs)-1].End
}

// IsEmpty returns true if there are no refs
func (nl *NodeLog) IsEmpty() bool {
	return len(nl.Refs) == 0
}

// LogRenderer efficiently renders logs from a NodeLog.
// It caches the rendered output and supports incremental updates.
type LogRenderer struct {
	buffer   *LogBuffer
	nodeLog  *NodeLog
	rendered strings.Builder // Cached rendered output
	lastEnd  int             // Last buffer position we've rendered up to
}

// NewLogRenderer creates a renderer for a node's log
func NewLogRenderer(buffer *LogBuffer, nodeLog *NodeLog) *LogRenderer {
	r := &LogRenderer{
		buffer:  buffer,
		nodeLog: nodeLog,
	}
	r.RebuildFull()
	return r
}

// RebuildFull rebuilds the entire rendered output from scratch
func (r *LogRenderer) RebuildFull() {
	r.rendered.Reset()
	if r.nodeLog == nil || r.nodeLog.IsEmpty() {
		r.lastEnd = 0
		return
	}

	r.rendered.Grow(r.nodeLog.TotalSize())

	for _, ref := range r.nodeLog.Refs {
		r.rendered.Write(r.buffer.SliceBytes(ref))
	}
	r.lastEnd = r.nodeLog.LastEnd()
}

// AppendNew appends only new content since last render.
// Returns true if new content was added.
func (r *LogRenderer) AppendNew() bool {
	if r.nodeLog == nil {
		return false
	}

	currentEnd := r.nodeLog.LastEnd()
	if currentEnd <= r.lastEnd {
		return false // No new content
	}

	// Find refs with content after lastEnd
	for _, ref := range r.nodeLog.Refs {
		if ref.Start >= r.lastEnd {
			// Entirely new ref
			r.rendered.Write(r.buffer.SliceBytes(ref))
		} else if ref.End > r.lastEnd {
			// Partially new ref (edge case: shouldn't happen with append-only)
			partial := BufferRef{Start: r.lastEnd, End: ref.End}
			r.rendered.Write(r.buffer.SliceBytes(partial))
		}
	}
	r.lastEnd = currentEnd
	return true
}

// String returns the current rendered output
func (r *LogRenderer) String() string {
	return r.rendered.String()
}

// LineCount returns the number of lines in the rendered output
func (r *LogRenderer) LineCount() int {
	s := r.rendered.String()
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// HasContent returns true if there is any rendered content
func (r *LogRenderer) HasContent() bool {
	return r.rendered.Len() > 0
}
