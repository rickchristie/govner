package configure

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

type dockerBuildLineMsg struct {
	Line string
}

type dockerBuildStepFinishedMsg struct {
	Index int
	Err   error
}

type dockerBuildFinishedMsg struct {
	Err error
}

type dockerBuildCloseMsg struct{}

// buildFeedbackModel is presentation-only state for the long Docker phase.
// Docker execution stays in buildflow; this model receives typed facts and
// renders a fixed frame around a scrollable combined stdout/stderr viewport.
type buildFeedbackModel struct {
	steps     []string
	current   int
	completed int
	width     int
	height    int
	follow    bool
	done      bool
	err       error
	preview   bool
	rawLines  []string
	viewport  components.ScrollableContent
}

func newBuildFeedbackModel(steps []string) *buildFeedbackModel {
	return &buildFeedbackModel{
		steps:  append([]string(nil), steps...),
		follow: true,
	}
}

// NewBuildFeedbackPreviewModel returns a populated storybook model for
// `cooper tui-test --screen build`. It exercises the exact production view and
// scrolling behavior without running Docker.
func NewBuildFeedbackPreviewModel() tea.Model {
	m := newBuildFeedbackModel([]string{
		"Building proxy image...",
		"Building base image...",
		"Building claude image...",
		"Building codex image...",
	})
	m.preview = true
	m.completed = 1
	m.current = 1
	sample := []string{
		"#0 building with \"default\" instance using docker driver",
		"",
		"#1 [internal] load build definition from Dockerfile",
		"#1 transferring dockerfile: 4.31kB done",
		"#1 DONE 0.0s",
		"",
		"#2 [internal] load metadata for docker.io/library/ubuntu:24.04",
		"#2 DONE 1.2s",
	}
	for _, line := range sample {
		m.appendLine(line)
	}
	for i := 3; i <= 28; i++ {
		m.appendLine(fmt.Sprintf("#3 [base %d/28] installing Cooper runtime dependency", i))
	}
	return m
}

func (m *buildFeedbackModel) Init() tea.Cmd {
	return nil
}

func (m *buildFeedbackModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.rebuildViewport()
		if m.follow {
			m.viewport.ScrollToBottom(m.bodyHeight())
		}
		return m, nil

	case dockerBuildLineMsg:
		m.appendLine(sanitizeBuildLine(msg.Line))
		if m.follow {
			m.viewport.ScrollToBottom(m.bodyHeight())
		}
		return m, nil

	case dockerBuildStepFinishedMsg:
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		if msg.Index >= m.completed {
			m.completed = msg.Index + 1
		}
		if m.completed < len(m.steps) {
			m.current = m.completed
		}
		return m, nil

	case dockerBuildFinishedMsg:
		m.done = true
		m.err = msg.Err
		if msg.Err != nil {
			m.appendLine("")
			m.appendLine("Build failed: " + msg.Err.Error())
			if m.follow {
				m.viewport.ScrollToBottom(m.bodyHeight())
			}
			return m, nil
		}
		m.completed = len(m.steps)
		return m, tea.Tick(theme.LoadingHoldDuration, func(_ time.Time) tea.Msg {
			return dockerBuildCloseMsg{}
		})

	case dockerBuildCloseMsg:
		return m, tea.Quit

	case tea.MouseMsg:
		if m.viewport.HandleMouse(msg, m.bodyHeight()) {
			m.follow = false
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "end", "G":
			m.viewport.ScrollToBottom(m.bodyHeight())
			m.follow = true
			return m, nil
		case "enter":
			if m.done {
				return m, tea.Quit
			}
		case "q", "esc", "ctrl+c":
			if m.preview || (m.done && m.err != nil) {
				return m, tea.Quit
			}
		default:
			if m.viewport.HandleKey(msg, m.bodyHeight()) {
				m.follow = false
				return m, nil
			}
		}
	}
	return m, nil
}

func (m *buildFeedbackModel) View() string {
	frame := m.frame()
	body := m.viewport.View(m.width, frame.BodyHeight())
	return frame.View(body)
}

func (m *buildFeedbackModel) frame() components.FixedFrame {
	return components.FixedFrame{
		Header: m.header(),
		Footer: m.footer(),
		Width:  m.width,
		Height: m.height,
	}
}

func (m *buildFeedbackModel) bodyHeight() int {
	return m.frame().BodyHeight()
}

func (m *buildFeedbackModel) header() string {
	breadcrumb := breadcrumbStyle().Render(theme.BarrelEmoji+" Configure > ") +
		lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render("Save & Build")

	total := len(m.steps)
	progress := fmt.Sprintf("%d/%d", m.completed, total)
	statusStyle := lipgloss.NewStyle().Foreground(theme.ColorAmber)
	status := "Preparing Docker build output..."
	switch {
	case m.err != nil:
		statusStyle = lipgloss.NewStyle().Foreground(theme.ColorFlame).Bold(true)
		status = theme.IconCross + " Docker build failed"
	case m.done:
		statusStyle = lipgloss.NewStyle().Foreground(theme.ColorProof).Bold(true)
		status = theme.IconCheck + " Docker build complete"
	case total > 0:
		idx := m.current
		if idx >= total {
			idx = total - 1
		}
		status = theme.IconArrowRight + " " + strings.TrimSuffix(m.steps[idx], "...")
	}

	meta := lipgloss.NewStyle().Foreground(theme.ColorDusty).Render(
		"  " + progress + "  •  combined stdout/stderr",
	)
	return breadcrumb + "\n " + statusStyle.Render(status) + meta
}

func (m *buildFeedbackModel) footer() string {
	switch {
	case m.err != nil:
		return " " + helpBar("["+theme.IconArrowUp+theme.IconArrowDown+" Scroll]", "[PgUp/PgDn]", "[q Close]") +
			"  " + lipgloss.NewStyle().Foreground(theme.ColorFlame).Render("Build failed")
	case m.done:
		return " " + lipgloss.NewStyle().Foreground(theme.ColorProof).Render(
			theme.IconCheck+" Build complete — returning to the terminal...",
		)
	case m.follow:
		hints := []string{"[" + theme.IconArrowUp + theme.IconArrowDown + " Scroll]", "[PgUp/PgDn]", "[End Follow]"}
		if m.preview {
			hints = append(hints, "[q Close]")
		}
		return " " + helpBar(hints...) +
			"  " + lipgloss.NewStyle().Foreground(theme.ColorVerdigris).Render("● LIVE")
	default:
		hints := []string{"[" + theme.IconArrowUp + theme.IconArrowDown + " Scroll]", "[PgUp/PgDn]", "[End Follow]"}
		if m.preview {
			hints = append(hints, "[q Close]")
		}
		return " " + helpBar(hints...) +
			"  " + lipgloss.NewStyle().Foreground(theme.ColorAmber).Render("Paused")
	}
}

func sanitizeBuildLine(line string) string {
	line = strings.ReplaceAll(line, "\r", "")
	line = ansi.Strip(line)
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, line)
}

func (m *buildFeedbackModel) appendLine(line string) {
	m.rawLines = append(m.rawLines, line)
	m.viewport.AppendLine(m.wrapLine(line))
}

func (m *buildFeedbackModel) rebuildViewport() {
	offset := m.viewport.ScrollOffset
	var wrapped []string
	for _, line := range m.rawLines {
		wrapped = append(wrapped, strings.Split(m.wrapLine(line), "\n")...)
	}
	m.viewport.SetContent(strings.Join(wrapped, "\n"))
	if !m.follow {
		m.viewport.ScrollOffset = offset
	}
}

func (m *buildFeedbackModel) wrapLine(line string) string {
	width := m.width - 1 // keep one stable column available for the scrollbar
	if width < 1 {
		return line
	}
	return ansi.Hardwrap(line, width, true)
}
