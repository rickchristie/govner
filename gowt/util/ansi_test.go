package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStripANSIRemovesEscapeAndUnsafeControlSequences(t *testing.T) {
	input := "\x1b]8;;https://example.com\x07link\x1b]8;;\x07 " +
		"\x1b[31mred\x1b[0m\x00\x07\r\n\tkept"
	assert.Equal(t, "link red\n\tkept", StripANSI(input))
}
