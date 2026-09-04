package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogBufferAppendAndSlice(t *testing.T) {
	buffer := NewLogBuffer()
	require.NotNil(t, buffer)
	assert.Zero(t, buffer.Len())

	first := buffer.Append("first")
	second := buffer.Append("\nsecond")

	assert.Equal(t, BufferRef{Start: 0, End: 5}, first)
	assert.Equal(t, BufferRef{Start: 5, End: 12}, second)
	assert.Equal(t, 12, buffer.Len())
	assert.Equal(t, "first", buffer.Slice(first))
	assert.Equal(t, []byte("\nsecond"), buffer.SliceBytes(second))
}

func TestLogBufferRejectsInvalidReferences(t *testing.T) {
	buffer := NewLogBuffer()
	valid := buffer.Append("content")

	tests := []struct {
		name string
		ref  BufferRef
	}{
		{name: "empty", ref: BufferRef{}},
		{name: "reversed", ref: BufferRef{Start: 4, End: 2}},
		{name: "negative start", ref: BufferRef{Start: -1, End: 2}},
		{name: "past end", ref: BufferRef{Start: 0, End: valid.End + 1}},
		{name: "starts at buffer end", ref: BufferRef{Start: valid.End, End: valid.End}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Empty(t, buffer.Slice(tt.ref))
			assert.Nil(t, buffer.SliceBytes(tt.ref))
		})
	}
}

func TestNodeLogTracksReferences(t *testing.T) {
	log := NewNodeLog()
	require.NotNil(t, log)
	assert.True(t, log.IsEmpty())
	assert.Zero(t, log.TotalSize())
	assert.Zero(t, log.LastEnd())

	log.Append(BufferRef{Start: 2, End: 5})
	log.Append(BufferRef{Start: 10, End: 14})

	assert.False(t, log.IsEmpty())
	assert.Equal(t, 7, log.TotalSize())
	assert.Equal(t, 14, log.LastEnd())
	assert.Equal(t, []BufferRef{{Start: 2, End: 5}, {Start: 10, End: 14}}, log.Refs)
}

func TestLogRendererBuildsAndAppendsIncrementally(t *testing.T) {
	buffer := NewLogBuffer()
	log := NewNodeLog()
	first := buffer.Append("first\n")
	log.Append(first)

	renderer := NewLogRenderer(buffer, log)
	assert.Equal(t, "first\n", renderer.String())
	assert.True(t, renderer.HasContent())
	assert.Equal(t, 2, renderer.LineCount())
	assert.False(t, renderer.AppendNew(), "no references were added")

	second := buffer.Append("second\n")
	log.Append(second)
	assert.True(t, renderer.AppendNew())
	assert.Equal(t, "first\nsecond\n", renderer.String())
	assert.False(t, renderer.AppendNew(), "the same reference must not be rendered twice")
}

func TestLogRendererHandlesNilAndEmptyLogs(t *testing.T) {
	buffer := NewLogBuffer()

	nilRenderer := NewLogRenderer(buffer, nil)
	assert.Empty(t, nilRenderer.String())
	assert.False(t, nilRenderer.HasContent())
	assert.Zero(t, nilRenderer.LineCount())
	assert.False(t, nilRenderer.AppendNew())

	emptyLog := NewNodeLog()
	emptyRenderer := NewLogRenderer(buffer, emptyLog)
	assert.Empty(t, emptyRenderer.String())

	ref := buffer.Append("later")
	emptyLog.Append(ref)
	assert.True(t, emptyRenderer.AppendNew())
	assert.Equal(t, "later", emptyRenderer.String())
	assert.Equal(t, 1, emptyRenderer.LineCount())
}

func TestLogRendererRebuildFullReflectsCurrentReferences(t *testing.T) {
	buffer := NewLogBuffer()
	first := buffer.Append("one")
	second := buffer.Append("two")
	log := &NodeLog{Refs: []BufferRef{first, second}}
	renderer := NewLogRenderer(buffer, log)
	assert.Equal(t, "onetwo", renderer.String())

	log.Refs = []BufferRef{second}
	renderer.RebuildFull()
	assert.Equal(t, "two", renderer.String())
	assert.False(t, renderer.AppendNew())
}

func TestLogRendererAppendNewHandlesOverlappingReference(t *testing.T) {
	buffer := NewLogBuffer()
	ref := buffer.Append("abcdef")
	log := &NodeLog{Refs: []BufferRef{{Start: ref.Start, End: 3}}}
	renderer := NewLogRenderer(buffer, log)
	assert.Equal(t, "abc", renderer.String())

	log.Refs = []BufferRef{ref}
	assert.True(t, renderer.AppendNew())
	assert.Equal(t, "abcdef", renderer.String())
}

func TestLogRendererRebuildsWhenBuildDiagnosticsArePrepended(t *testing.T) {
	buffer := NewLogBuffer()
	diagnostic := buffer.Append("compile diagnostic\n")
	result := buffer.Append("FAIL package [build failed]\n")
	log := &NodeLog{Refs: []BufferRef{result}}
	renderer := NewLogRenderer(buffer, log)
	assert.Equal(t, "FAIL package [build failed]\n", renderer.String())

	log.Refs = []BufferRef{diagnostic, result}
	assert.True(t, renderer.AppendNew())
	assert.Equal(t, "compile diagnostic\nFAIL package [build failed]\n", renderer.String())
}
