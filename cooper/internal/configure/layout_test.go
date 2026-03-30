package configure

import (
	"strings"
	"testing"
)

func TestLayout_ContentFits(t *testing.T) {
	header := "Header"
	content := "Content A\nContent B"
	footer := "Footer"

	// header=1, footer=1, seps=2, total=10 → content area=6
	ly := newLayout(header, content, footer, 40, 10)

	if ly.NeedsScroll() {
		t.Error("expected NeedsScroll() = false when content fits")
	}
	if ly.ContentHeight() != 6 {
		t.Errorf("expected ContentHeight()=6, got %d", ly.ContentHeight())
	}

	rendered := ly.Render()
	lines := strings.Split(rendered, "\n")
	if len(lines) != 10 {
		t.Errorf("expected 10 rendered lines, got %d", len(lines))
	}

	// Header at top.
	if lines[0] != "Header" {
		t.Errorf("line 0: expected header, got %q", lines[0])
	}
	// Separator.
	if !strings.Contains(lines[1], "─") {
		t.Errorf("line 1: expected separator, got %q", lines[1])
	}
	// Content.
	if lines[2] != "Content A" {
		t.Errorf("line 2: expected content, got %q", lines[2])
	}
	// Footer separator.
	if !strings.Contains(lines[len(lines)-2], "─") {
		t.Errorf("second to last: expected separator, got %q", lines[len(lines)-2])
	}
	// Footer.
	if lines[len(lines)-1] != "Footer" {
		t.Errorf("last line: expected footer, got %q", lines[len(lines)-1])
	}
}

func TestLayout_ContentOverflows(t *testing.T) {
	header := "Header"
	var contentLines []string
	for i := range 10 {
		contentLines = append(contentLines, strings.Repeat("x", i+1))
	}
	content := strings.Join(contentLines, "\n")
	footer := " help keys"

	// header=1, footer=1, seps=2, height=8 → content area=4
	ly := newLayout(header, content, footer, 40, 8)

	if !ly.NeedsScroll() {
		t.Error("expected NeedsScroll() = true when content overflows")
	}
	if ly.ContentHeight() != 4 {
		t.Errorf("expected ContentHeight()=4, got %d", ly.ContentHeight())
	}

	rendered := ly.Render()
	lines := strings.Split(rendered, "\n")
	if len(lines) != 8 {
		t.Errorf("expected 8 rendered lines, got %d", len(lines))
	}

	lastLine := lines[len(lines)-1]
	if !strings.Contains(lastLine, "Top") {
		t.Errorf("expected footer to contain 'Top', got %q", lastLine)
	}
}

func TestLayout_ScrollDownClamp(t *testing.T) {
	header := "H"
	content := "L1\nL2\nL3\nL4\nL5\nL6\nL7\nL8\nL9\nL10"
	footer := "F"
	// header=1, footer=1, seps=2, height=7 → content area=3, max offset=7
	ly := newLayout(header, content, footer, 40, 7)

	ly.ScrollDown(100)
	if ly.scrollOffset != 7 {
		t.Errorf("expected scrollOffset clamped to 7, got %d", ly.scrollOffset)
	}
}

func TestLayout_ScrollUpClamp(t *testing.T) {
	header := "H"
	content := "L1\nL2\nL3\nL4\nL5"
	footer := "F"
	ly := newLayout(header, content, footer, 40, 7)
	ly.scrollOffset = 3

	ly.ScrollUp(100)
	if ly.scrollOffset != 0 {
		t.Errorf("expected scrollOffset clamped to 0, got %d", ly.scrollOffset)
	}
}

func TestLayout_ScrollToTopAndBottom(t *testing.T) {
	header := "H"
	content := "L1\nL2\nL3\nL4\nL5\nL6\nL7\nL8\nL9\nL10"
	footer := "F"
	// height=7, content area=3, max offset=7
	ly := newLayout(header, content, footer, 40, 7)

	ly.ScrollToBottom()
	if ly.scrollOffset != 7 {
		t.Errorf("expected scrollOffset=7 at bottom, got %d", ly.scrollOffset)
	}

	ly.ScrollToTop()
	if ly.scrollOffset != 0 {
		t.Errorf("expected scrollOffset=0 at top, got %d", ly.scrollOffset)
	}
}

func TestLayout_ScrollPercentage(t *testing.T) {
	header := "H"
	content := "L1\nL2\nL3\nL4\nL5\nL6\nL7\nL8\nL9\nL10"
	footer := "F"
	// height=7, content area=3, max offset=7
	ly := newLayout(header, content, footer, 40, 7)

	ly.scrollOffset = 0
	if text := ly.scrollIndicatorText(); text != "Top" {
		t.Errorf("expected 'Top', got %q", text)
	}

	ly.scrollOffset = 7
	if text := ly.scrollIndicatorText(); text != "Bot" {
		t.Errorf("expected 'Bot', got %q", text)
	}

	ly.scrollOffset = 3
	if text := ly.scrollIndicatorText(); text != "42%" {
		t.Errorf("expected '42%%', got %q", text)
	}
}

func TestLayout_EmptyContent(t *testing.T) {
	header := "H"
	footer := "F"
	ly := newLayout(header, "", footer, 40, 7)

	if ly.NeedsScroll() {
		t.Error("expected NeedsScroll() = false for empty content")
	}
}

func TestLayout_EnsureVisible(t *testing.T) {
	header := "H"
	content := "L1\nL2\nL3\nL4\nL5\nL6\nL7\nL8\nL9\nL10"
	footer := "F"
	// height=7, content area=3, max offset=7
	ly := newLayout(header, content, footer, 40, 7)
	ly.scrollOffset = 0

	ly.EnsureVisible(8)
	if ly.scrollOffset < 6 {
		t.Errorf("expected scrollOffset >= 6 to see line 8, got %d", ly.scrollOffset)
	}

	ly.EnsureVisible(0)
	if ly.scrollOffset != 0 {
		t.Errorf("expected scrollOffset=0 to see line 0, got %d", ly.scrollOffset)
	}
}

func TestLayout_NoScrollIndicatorWhenContentFits(t *testing.T) {
	header := "H"
	content := "A\nB"
	footer := "help bar"
	ly := newLayout(header, content, footer, 40, 10)

	rendered := ly.Render()
	lines := strings.Split(rendered, "\n")
	lastLine := lines[len(lines)-1]

	if strings.Contains(lastLine, "Top") || strings.Contains(lastLine, "Bot") || strings.Contains(lastLine, "%") {
		t.Errorf("expected no scroll indicator, got %q", lastLine)
	}
	if lastLine != "help bar" {
		t.Errorf("expected footer 'help bar', got %q", lastLine)
	}
}
