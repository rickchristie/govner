package util

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// StripANSI removes all terminal control sequences from untrusted test output.
// Go tests may emit CSI color codes as well as OSC hyperlinks or title changes;
// using the ECMA-48 parser prevents those sequences from leaking into Gowt's UI.
func StripANSI(s string) string {
	stripped := ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < 0x20 || (r >= 0x7f && r <= 0x9f):
			return -1
		default:
			return r
		}
	}, stripped)
}
