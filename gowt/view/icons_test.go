package view

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPreRenderedIcons(t *testing.T) {
	assert.Equal(t, IconCharPassed, strings.TrimSpace(stripAnsi(IconPassed)))
	assert.Equal(t, IconCharFailed, strings.TrimSpace(stripAnsi(IconFailed)))
	assert.Equal(t, IconCharSkipped, strings.TrimSpace(stripAnsi(IconSkipped)))
	assert.Equal(t, IconCharPending, strings.TrimSpace(stripAnsi(IconPending)))
	assert.Equal(t, IconCharCached, strings.TrimSpace(stripAnsi(IconCached)))
	assert.Equal(t, IconCharGear, stripAnsi(IconGearPassed))
	assert.Equal(t, IconCharGear, stripAnsi(IconGearFailed))
}

func TestSpinnerAccessorsCycle(t *testing.T) {
	assert.Len(t, SpinnerFrames, 10)
	assert.Len(t, SpinnerColors, 12)

	for frame := 0; frame < len(SpinnerFrames)*len(SpinnerColors); frame++ {
		assert.Equal(t, SpinnerFrames[frame%len(SpinnerFrames)],
			strings.TrimSpace(stripAnsi(GetSpinnerIcon(frame))))
		assert.Equal(t, SpinnerFrames[frame%len(SpinnerFrames)],
			stripAnsi(GetSpinnerIconCompact(frame)))
		assert.Equal(t, SpinnerFrames[frame%len(SpinnerFrames)],
			strings.TrimSpace(GetSpinnerIconRaw(frame)))
		assert.Equal(t, IconCharGear, stripAnsi(GetSpinnerGear(frame)))
	}

	assert.Equal(t, GetSpinnerIcon(0), GetSpinnerIcon(len(SpinnerFrames)*len(SpinnerColors)))
	assert.Equal(t, GetSpinnerGear(0), GetSpinnerGear(len(SpinnerColors)))
}
