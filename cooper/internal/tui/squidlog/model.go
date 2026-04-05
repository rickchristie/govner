// Package squidlog implements the Squid Logs tab sub-model.
// It displays a live tail of the Squid proxy access log using a
// ScrollableContent viewport with auto-scroll behavior.
package squidlog

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

const maxLines = 1000

// Model is the sub-model for the Squid Logs tab.
type Model struct {
	lines      []string
	viewport   components.ScrollableContent
	autoScroll bool
	lastHeight int
}

// New creates a new Squid Logs sub-model.
func New() *Model {
	return &Model{
		autoScroll: true,
	}
}

// AddLine appends a log line to the buffer and updates the viewport.
func (m *Model) AddLine(line string) {
	m.lines = append(m.lines, line)
	if len(m.lines) > maxLines {
		m.lines = m.lines[len(m.lines)-maxLines:]
	}
	m.syncViewport()
}

// syncViewport rebuilds the viewport content from the line buffer
// and auto-scrolls to the bottom if enabled.
func (m *Model) syncViewport() {
	m.viewport.SetContent(strings.Join(m.lines, "\n"))
	if m.autoScroll && m.lastHeight > 0 {
		m.scrollToBottom()
	}
}

func (m *Model) scrollToBottom() {
	maxOffset := len(m.lines) - m.lastHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	m.viewport.ScrollOffset = maxOffset
}

// Init satisfies the SubModel interface.
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update satisfies theme.SubModel.
func (m *Model) Update(msg tea.Msg) (theme.SubModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if m.viewport.HandleMouse(msg, m.lastHeight) {
			m.autoScroll = m.isAtBottom()
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.viewport.HandleKey(msg, m.lastHeight) {
				m.autoScroll = false
			}
		case "down", "j":
			if m.viewport.HandleKey(msg, m.lastHeight) {
				m.autoScroll = m.isAtBottom()
			}
		case "pgup", "ctrl+u":
			m.viewport.HandleKey(msg, m.lastHeight)
			m.autoScroll = false
		case "pgdown", "ctrl+d":
			m.viewport.HandleKey(msg, m.lastHeight)
			m.autoScroll = m.isAtBottom()
		case "home", "g":
			m.viewport.HandleKey(msg, m.lastHeight)
			m.autoScroll = false
		case "end", "G":
			m.viewport.HandleKey(msg, m.lastHeight)
			m.autoScroll = true
		}
	}
	return m, nil
}

// isAtBottom returns true when the viewport is scrolled to the bottom.
func (m *Model) isAtBottom() bool {
	maxOffset := len(m.lines) - m.lastHeight
	if maxOffset <= 0 {
		return true
	}
	return m.viewport.ScrollOffset >= maxOffset
}

// View satisfies the SubModel interface.
func (m *Model) View(width, height int) string {
	m.lastHeight = height
	if len(m.lines) == 0 {
		return renderEmpty(width, height)
	}

	// If auto-scroll is on, snap to bottom on each render (covers new
	// lines arriving between Update and View).
	if m.autoScroll {
		m.scrollToBottom()
	}

	return m.viewport.View(width, height)
}

func renderEmpty(width, height int) string {
	content := lipgloss.JoinVertical(lipgloss.Center,
		"",
		"",
		theme.EmptyStateStyle.Render("🦑"),
		"",
		theme.EmptyStateStyle.Render("No Squid log entries yet."),
		"",
		theme.EmptyStateStyle.Render("Proxy requests will appear here"),
		theme.EmptyStateStyle.Render("as they flow through Squid."),
	)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}
