package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// SplitModel gives two screens one tab. Each screen keeps its own behavior;
// this component owns only geometry, focus, and message routing.
type SplitModel struct {
	First, Second           theme.SubModel
	FirstTitle, SecondTitle string
	Stacked                 bool
	focus                   int
	width, height           int
}

func NewSplit(firstTitle string, first theme.SubModel, secondTitle string, second theme.SubModel) *SplitModel {
	return &SplitModel{First: first, Second: second, FirstTitle: firstTitle, SecondTitle: secondTitle}
}

func (m *SplitModel) Init() tea.Cmd { return tea.Batch(m.First.Init(), m.Second.Init()) }

func (m *SplitModel) active() theme.SubModel {
	if m.focus == 0 {
		return m.First
	}
	return m.Second
}

func ownsKeys(screen theme.SubModel) bool {
	if modal, ok := screen.(interface{ ModalActive() bool }); ok && modal.ModalActive() {
		return true
	}
	if editor, ok := screen.(interface{ IsEditing() bool }); ok {
		return editor.IsEditing()
	}
	return false
}

func (m *SplitModel) ModalActive() bool { return ownsKeys(m.active()) }
func (m *SplitModel) IsEditing() bool   { return m.ModalActive() }

type paneRect struct{ x, y, width, height int }

func (m *SplitModel) geometry() (paneRect, paneRect) {
	if m.Stacked || m.width < 110 {
		first := max(1, (m.height-3)/2)
		return paneRect{0, 1, m.width, first}, paneRect{0, first + 3, m.width, max(0, m.height-first-3)}
	}
	first := (m.width - 1) * 2 / 5
	return paneRect{0, 1, first, max(0, m.height-1)}, paneRect{first + 1, 1, m.width - first - 1, max(0, m.height-1)}
}

func (m *SplitModel) resizeChildren() tea.Cmd {
	a, b := m.geometry()
	var first, second tea.Cmd
	m.First, first = m.First.Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	m.Second, second = m.Second.Update(tea.WindowSizeMsg{Width: b.width, Height: b.height})
	return tea.Batch(first, second)
}

func (m *SplitModel) Update(msg tea.Msg) (theme.SubModel, tea.Cmd) {
	switch event := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = event.Width, event.Height
		return m, m.resizeChildren()
	case tea.KeyMsg:
		if !m.ModalActive() && (event.String() == "[" || event.String() == "]") {
			m.focus = 1 - m.focus
			return m, nil
		}
		return m, m.forward(msg)
	case tea.MouseMsg:
		if m.ModalActive() {
			return m, m.forward(msg)
		}
		a, b := m.geometry()
		for index, r := range []paneRect{a, b} {
			if event.X >= r.x && event.X < r.x+r.width && event.Y >= r.y && event.Y < r.y+r.height {
				m.focus = index
				event.X -= r.x
				event.Y -= r.y
				return m, m.forward(event)
			}
		}
		return m, nil
	default:
		var first, second tea.Cmd
		m.First, first = m.First.Update(msg)
		m.Second, second = m.Second.Update(msg)
		return m, tea.Batch(first, second)
	}
}

func (m *SplitModel) forward(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	if m.focus == 0 {
		m.First, cmd = m.First.Update(msg)
	} else {
		m.Second, cmd = m.Second.Update(msg)
	}
	return cmd
}

func (m *SplitModel) View(width, height int) string {
	if m.ModalActive() {
		return m.active().View(width, height)
	}
	// Geometry is derived for rendering without changing interaction state.
	view := *m
	view.width, view.height = width, height
	a, b := view.geometry()
	header := func(title string, active bool, width int) string {
		style := theme.PaneLabelStyle
		mark := "  "
		if active {
			style = style.Foreground(theme.ColorAmber)
			mark = "▶ "
		}
		return FixedFrame{Width: width, Height: 1}.View(style.Render(mark + title + "  [ ] focus"))
	}
	first := header(m.FirstTitle, m.focus == 0, a.width) + "\n" + FixedFrame{Width: a.width, Height: a.height}.View(m.First.View(a.width, a.height))
	second := header(m.SecondTitle, m.focus == 1, b.width) + "\n" + FixedFrame{Width: b.width, Height: b.height}.View(m.Second.View(b.width, b.height))
	if m.Stacked || width < 110 {
		divider := theme.DividerStyle.Render(strings.Repeat(theme.BorderH, max(0, width)))
		return first + "\n" + divider + "\n" + second
	}
	divider := strings.TrimSuffix(strings.Repeat(theme.DividerStyle.Render(theme.BorderV)+"\n", max(0, height)), "\n")
	return lipgloss.JoinHorizontal(lipgloss.Top, first, divider, second)
}
