package view

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

func TestDefaultModalStylesAndBuildModalBox(t *testing.T) {
	styles := DefaultModalStyles()
	assert.True(t, styles.Title.GetBold())
	assert.True(t, styles.ButtonSelected.GetBold())

	box := buildModalBox(ModalConfig{
		Title:   "Confirm",
		Message: "Continue?",
		Buttons: []ModalButton{
			{Label: "Yes", Selected: true},
			{Label: "No"},
		},
	}, styles)
	plain := stripAnsi(box)
	assert.Contains(t, plain, "Confirm")
	assert.Contains(t, plain, "Continue?")
	assert.Contains(t, plain, "Yes")
	assert.Contains(t, plain, "No")
	assert.GreaterOrEqual(t, lipgloss.Width(box), 30)
}

func TestBuildModalBoxCustomMinimumWidth(t *testing.T) {
	box := buildModalBox(ModalConfig{
		Title: "Wide",
		Width: 50,
	}, DefaultModalStyles())
	assert.GreaterOrEqual(t, lipgloss.Width(box), 50)
}

func TestRenderModalZeroDimensionsReturnsOriginal(t *testing.T) {
	content := "original"
	config := ModalConfig{Title: "Ignored"}
	styles := DefaultModalStyles()

	assert.Equal(t, content, RenderModal(content, config, styles, 0, 10))
	assert.Equal(t, content, RenderModal(content, config, styles, 10, 0))
}

func TestRenderModalOverlaysAndPadsBackground(t *testing.T) {
	rendered := RenderModal(
		"background",
		ModalConfig{
			Title: "Confirm",
			Buttons: []ModalButton{
				{Label: "OK", Selected: true},
			},
		},
		DefaultModalStyles(),
		80,
		20,
	)
	plain := stripAnsi(rendered)
	assert.Contains(t, plain, "Confirm")
	assert.Contains(t, plain, "OK")
	assert.Contains(t, plain, "background")
	assert.GreaterOrEqual(t, len(strings.Split(plain, "\n")), 20)
	assert.Contains(t, plain, "░")
}

func TestConfirmAndInfoModalConvenienceFunctions(t *testing.T) {
	confirm := stripAnsi(RenderConfirmModal("background", "Rerun?", true, 80, 20))
	assert.Contains(t, confirm, "Rerun?")
	assert.Contains(t, confirm, "Yes")
	assert.Contains(t, confirm, "No")

	info := stripAnsi(RenderInfoModal("background", "Notice", "All done", 80, 20))
	assert.Contains(t, info, "Notice")
	assert.Contains(t, info, "All done")
	assert.Contains(t, info, "OK")
}

func TestModalLayoutHelpers(t *testing.T) {
	dimmed := dimLineContent("abc", 6)
	assert.Equal(t, "abc   ", stripAnsi(dimmed))

	inserted := insertAtPosition("abcdefghij", "XX", 3, 10, lipgloss.NewStyle())
	assert.Equal(t, "abcXXfghij", stripAnsi(inserted))

	styled := insertAtPosition(
		"abcdefghij",
		"XX",
		3,
		10,
		lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
	)
	assert.Equal(t, "abcXXfghij", stripAnsi(styled))

	assert.Equal(t, 5, maxLineWidth([]string{"a", "12345", "abc"}))
	assert.Equal(t, 2, maxLineWidth([]string{"界"}))
}

func TestModalLayoutUsesTerminalCellWidthsForWideUnicode(t *testing.T) {
	dimmed := stripAnsi(dimLineContent("界a", 4))
	assert.Equal(t, "界a ", dimmed)
	assert.Equal(t, 4, lipgloss.Width(dimmed))

	inserted := stripAnsi(insertAtPosition("界abcdef", "🙂", 2, 8, lipgloss.NewStyle()))
	assert.Equal(t, "界🙂cdef", inserted)
	assert.Equal(t, 8, lipgloss.Width(inserted))

	insideWideRune := stripAnsi(insertAtPosition("界abcdef", "X", 1, 8, lipgloss.NewStyle()))
	assert.Equal(t, 8, lipgloss.Width(insideWideRune))
}

func TestStripANSICommonSequences(t *testing.T) {
	assert.Equal(t, "red plain", stripAnsi("\x1b[31mred\x1b[0m plain"))
	assert.Equal(t, "text", stripAnsi("\x1b[2Jtext\x1b[H"))
	assert.Equal(t, "before", stripAnsi("before\x1b[31"))
	assert.Equal(t, "link", stripAnsi("\x1b]8;;https://example.com\x07link\x1b]8;;\x07"))
}
