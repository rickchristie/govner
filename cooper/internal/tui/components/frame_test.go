package components

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestFixedFrameKeepsHeaderAndFooterWhileBodyFillsRemainder(t *testing.T) {
	frame := FixedFrame{
		Header: "Header A\nHeader B",
		Footer: "Footer",
		Width:  20,
		Height: 8,
	}
	rendered := frame.View("one\ntwo\nthree\nfour\nfive")
	lines := strings.Split(rendered, "\n")

	if len(lines) != frame.Height {
		t.Fatalf("rendered lines = %d, want %d\n%s", len(lines), frame.Height, rendered)
	}
	if !strings.Contains(lines[0], "Header A") || !strings.Contains(lines[1], "Header B") {
		t.Fatalf("header moved or disappeared: %q", lines[:2])
	}
	if !strings.Contains(lines[len(lines)-1], "Footer") {
		t.Fatalf("footer moved or disappeared: %q", lines[len(lines)-1])
	}
	if frame.BodyHeight() != 3 {
		t.Fatalf("BodyHeight() = %d, want 3", frame.BodyHeight())
	}
	if strings.Contains(rendered, "four") || strings.Contains(rendered, "five") {
		t.Fatalf("oversized body escaped its viewport:\n%s", rendered)
	}
	for i, line := range lines {
		if got := lipgloss.Width(line); got > frame.Width {
			t.Fatalf("line %d width = %d, exceeds %d", i, got, frame.Width)
		}
	}
}

func TestScrollableContentSupportsKeyboardMouseAndFollow(t *testing.T) {
	var viewport ScrollableContent
	for i := range 12 {
		viewport.AppendLine(string(rune('a' + i)))
	}
	const height = 4

	viewport.ScrollToBottom(height)
	if !viewport.AtBottom(height) {
		t.Fatal("ScrollToBottom() did not reach bottom")
	}
	bottom := viewport.ScrollOffset

	if !viewport.HandleKey(tea.KeyMsg{Type: tea.KeyUp}, height) {
		t.Fatal("up key was not handled")
	}
	if viewport.ScrollOffset != bottom-1 {
		t.Fatalf("offset after up = %d, want %d", viewport.ScrollOffset, bottom-1)
	}

	if !viewport.HandleMouse(tea.MouseMsg{Button: tea.MouseButtonWheelUp}, height) {
		t.Fatal("mouse wheel up was not handled")
	}
	if viewport.ScrollOffset != bottom-4 {
		t.Fatalf("offset after mouse wheel = %d, want %d", viewport.ScrollOffset, bottom-4)
	}

	if !viewport.HandleKey(tea.KeyMsg{Type: tea.KeyEnd}, height) {
		t.Fatal("end key was not handled")
	}
	if !viewport.AtBottom(height) {
		t.Fatal("end key did not restore bottom position")
	}
}
