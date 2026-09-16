package components

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// TextCopier is the application boundary for an explicit copy action.
type TextCopier interface {
	CopyText(context.Context, string) error
}

// TextCopiedMsg returns a copy result to its log even after a tab change.
type TextCopiedMsg struct {
	Target string
	Lines  int
	Err    error
}

// TextLog is a bounded, selectable text viewport shared by both log screens.
// A click selects a whole logical line. Dragging selects adjacent lines.
// Selection pauses following so new records cannot move text under the mouse.
type TextLog struct {
	viewport           ScrollableContent
	Copier             TextCopier // Optional in fixtures; a copy reports an error if absent.
	Target             string
	Limit              int
	Follow             bool
	width, height      int
	anchor, cursor     int
	selected, dragging bool
	horizontal         int
	status             string
}

func NewTextLog(target string, limit int, copier TextCopier) TextLog {
	return TextLog{Target: target, Limit: limit, Copier: copier, Follow: true}
}

func (v *TextLog) SetContent(text string) {
	v.viewport.SetContent(text)
	v.selected, v.dragging = false, false
	v.status = ""
	v.horizontal = 0
	v.trim()
	if v.Follow {
		v.viewport.ScrollToBottom(v.bodyHeight())
	}
}

func (v *TextLog) AppendLine(line string) {
	v.viewport.AppendLine(line)
	v.trim()
	if v.Follow {
		v.viewport.ScrollToBottom(v.bodyHeight())
	}
}

func (v *TextLog) trim() {
	drop := v.viewport.totalLines - v.Limit
	if v.Limit <= 0 || drop <= 0 {
		return
	}
	v.viewport.lines = v.viewport.lines[drop:]
	v.viewport.totalLines = len(v.viewport.lines)
	v.viewport.ScrollOffset = max(0, v.viewport.ScrollOffset-drop)
	if v.selected {
		v.anchor -= drop
		v.cursor -= drop
		if v.anchor < 0 || v.cursor < 0 {
			v.selected, v.dragging = false, false
			v.status = "Selection expired from log history."
		}
	}
}

func (v *TextLog) bodyHeight() int { return max(0, v.height-2) }

func (v *TextLog) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		v.width, v.height = msg.Width, msg.Height
		v.viewport.clampScroll(v.bodyHeight())
		if v.Follow {
			v.viewport.ScrollToBottom(v.bodyHeight())
		}
	case TextCopiedMsg:
		if msg.Target != v.Target {
			return nil
		}
		if msg.Err != nil {
			v.status = "Copy failed: " + strings.Join(strings.Fields(msg.Err.Error()), " ")
		} else {
			v.status = fmt.Sprintf("Copied %d line(s).", msg.Lines)
		}
	case tea.MouseMsg:
		if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
			if msg.Y < 0 || msg.Y >= v.bodyHeight() || msg.X < 0 || msg.X >= v.width {
				return nil
			}
			index := v.viewport.ScrollOffset + msg.Y
			if index >= v.viewport.totalLines {
				return nil
			}
			v.anchor, v.cursor = index, index
			v.selected, v.dragging, v.Follow = true, true, false
			v.status = ""
		} else if msg.Action == tea.MouseActionMotion && v.dragging {
			v.cursor = min(max(0, v.viewport.ScrollOffset+msg.Y), v.viewport.totalLines-1)
		} else if msg.Action == tea.MouseActionRelease {
			v.dragging = false
		} else if v.viewport.HandleMouse(msg, v.bodyHeight()) {
			v.Follow = false
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "y":
			return v.copySelection()
		case "esc":
			v.selected, v.dragging, v.status = false, false, ""
		case "end", "G":
			v.Follow, v.selected = true, false
			v.viewport.ScrollToBottom(v.bodyHeight())
		case "left":
			v.horizontal = max(0, v.horizontal-8)
		case "right":
			v.horizontal += 8
		case "up", "k", "down", "j", "shift+up", "shift+down":
			if v.viewport.totalLines == 0 {
				return nil
			}
			index := v.cursor
			if !v.selected {
				index = min(v.viewport.totalLines-1, v.viewport.ScrollOffset+v.bodyHeight()-1)
			}
			if msg.String() == "up" || msg.String() == "k" || msg.String() == "shift+up" {
				index--
			} else {
				index++
			}
			index = min(max(0, index), v.viewport.totalLines-1)
			if !v.selected || !strings.HasPrefix(msg.String(), "shift+") {
				v.anchor = index
			}
			v.cursor, v.selected, v.Follow = index, true, false
			v.viewport.EnsureLineVisible(index, v.bodyHeight())
		default:
			if v.viewport.HandleKey(msg, v.bodyHeight()) {
				v.Follow = false
			}
		}
	}
	return nil
}

// SelectAll selects the full record, including lines outside the viewport.
func (v *TextLog) SelectAll() {
	if v.viewport.totalLines == 0 {
		return
	}
	v.anchor, v.cursor = 0, v.viewport.totalLines-1
	v.selected, v.Follow = true, false
}

func (v *TextLog) copySelection() tea.Cmd {
	if !v.selected {
		v.status = "Click a line or use arrows to select text."
		return nil
	}
	first, last := min(v.anchor, v.cursor), max(v.anchor, v.cursor)
	text := ansi.Strip(strings.Join(v.viewport.lines[first:last+1], "\n"))
	target, count, copier := v.Target, last-first+1, v.Copier
	return func() tea.Msg {
		if copier == nil {
			return TextCopiedMsg{Target: target, Err: fmt.Errorf("clipboard is unavailable")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return TextCopiedMsg{Target: target, Lines: count, Err: copier.CopyText(ctx, text)}
	}
}

func (v TextLog) View(width, height int) string {
	state := "Paused"
	if v.Follow {
		state = "Following"
	}
	if v.selected {
		state = fmt.Sprintf("%d selected", abs(v.anchor-v.cursor)+1)
	}
	footer := state + "  ·  drag/select  y Copy  G Follow  ←→ Pan"
	if v.status != "" {
		footer = v.status + "  ·  y Copy  G Follow"
	}
	frame := FixedFrame{Width: width, Height: height, Footer: theme.HelpDescStyle.Render(footer)}
	rows := make([]string, 0, frame.BodyHeight())
	if v.viewport.totalLines == 0 {
		rows = append(rows, theme.EmptyStateStyle.Render("No log lines yet."))
	}
	start := min(v.viewport.ScrollOffset, v.viewport.MaxScrollOffset(frame.BodyHeight()))
	for i := start; i < min(v.viewport.totalLines, start+frame.BodyHeight()); i++ {
		line := ansi.Cut(ansi.Strip(v.viewport.lines[i]), v.horizontal, v.horizontal+max(0, width-1))
		style := theme.RowNormalStyle
		if v.selected && i >= min(v.anchor, v.cursor) && i <= max(v.anchor, v.cursor) {
			style = theme.RowSelectedStyle
		}
		rows = append(rows, style.Width(max(0, width-1)).Render(line))
	}
	if v.viewport.totalLines > frame.BodyHeight() {
		bar := buildScrollIndicator(v.viewport.totalLines, start, frame.BodyHeight())
		for i := range rows {
			rows[i] += bar[i]
		}
	}
	return frame.View(strings.Join(rows, "\n"))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
