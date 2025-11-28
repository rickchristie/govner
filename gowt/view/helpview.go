package view

import (
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// HelpViewRequest represents a request from HelpView to the controller
type HelpViewRequest interface {
	isHelpViewRequest()
}

// CloseHelpRequest is emitted when user wants to close help
type CloseHelpRequest struct{}

func (CloseHelpRequest) isHelpViewRequest() {}

// HelpSource indicates which screen the help was opened from
type HelpSource int

const (
	HelpSourceTree HelpSource = iota
	HelpSourceLog
)

// HelpView displays keyboard shortcuts
type HelpView struct {
	width    int
	height   int
	styles   helpStyles
	viewport viewport.Model
	ready    bool
	source   HelpSource // Which screen opened the help
}

type helpStyles struct {
	title   lipgloss.Style
	section lipgloss.Style
	key     lipgloss.Style
	desc    lipgloss.Style
	hint    lipgloss.Style
}

func defaultHelpStyles() helpStyles {
	return helpStyles{
		title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("212")),
		section: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("248")),
		key: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("82")),
		desc: lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")),
		hint: lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")),
	}
}

// NewHelpView creates a new HelpView
func NewHelpView() HelpView {
	return HelpView{
		styles: defaultHelpStyles(),
	}
}

// SetSource sets the help source (Tree or Log) and refreshes content
func (v HelpView) SetSource(source HelpSource) HelpView {
	v.source = source
	if v.ready {
		v.viewport.SetContent(v.renderContent())
		v.viewport.GotoTop()
	}
	return v
}

// Init implements tea.Model
func (v HelpView) Init() tea.Cmd {
	return nil
}

// helpKeys defines keybindings for HelpView
type helpKeyMap struct {
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Close    key.Binding
}

var helpKeys = helpKeyMap{
	Up:       key.NewBinding(key.WithKeys("up", "k", "K")),
	Down:     key.NewBinding(key.WithKeys("down", "j", "J")),
	PageUp:   key.NewBinding(key.WithKeys("pgup", "ctrl+u", "ctrl+U")),
	PageDown: key.NewBinding(key.WithKeys("pgdown", "ctrl+d", "ctrl+D")),
	Close:    key.NewBinding(key.WithKeys("q", "Q", "esc", "?")),
}

// Update implements tea.Model
func (v HelpView) Update(msg tea.Msg) (HelpView, tea.Cmd, HelpViewRequest) {
	var request HelpViewRequest
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
		headerHeight := 1 // Title line
		if !v.ready {
			v.viewport = viewport.New(msg.Width, msg.Height-headerHeight)
			v.viewport.SetContent(v.renderContent())
			v.ready = true
		} else {
			v.viewport.Width = msg.Width
			v.viewport.Height = msg.Height - headerHeight
			v.viewport.SetContent(v.renderContent())
		}

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, helpKeys.Close):
			request = CloseHelpRequest{}
		case key.Matches(msg, helpKeys.Up):
			v.viewport.LineUp(1)
		case key.Matches(msg, helpKeys.Down):
			v.viewport.LineDown(1)
		case key.Matches(msg, helpKeys.PageUp):
			v.viewport.HalfViewUp()
		case key.Matches(msg, helpKeys.PageDown):
			v.viewport.HalfViewDown()
		}
	}

	return v, cmd, request
}

// View implements tea.Model
func (v HelpView) View() string {
	var sb strings.Builder

	// Title bar
	sb.WriteString(v.styles.title.Render("GOWT Help"))
	sb.WriteString("  ")
	sb.WriteString(v.styles.hint.Render("Press Esc to go back"))
	sb.WriteString("\n")

	// Scrollable content
	if v.ready {
		sb.WriteString(v.viewport.View())
	} else {
		sb.WriteString(v.renderContent())
	}

	return sb.String()
}

func (v HelpView) renderContent() string {
	if v.source == HelpSourceLog {
		return v.renderLogContent()
	}
	return v.renderTreeContent()
}

func (v HelpView) renderTreeContent() string {
	var sb strings.Builder

	// Navigation
	sb.WriteString("\n")
	sb.WriteString(v.styles.section.Render("Navigation"))
	sb.WriteString("\n")
	sb.WriteString(v.renderKey("↑/k ↓/j", "Move up/down"))
	sb.WriteString(v.renderKey("PgUp PgDn", "Page up/down"))
	sb.WriteString(v.renderKey("g G", "Go to top/bottom"))

	// Tree
	sb.WriteString("\n")
	sb.WriteString(v.styles.section.Render("Tree"))
	sb.WriteString("\n")
	sb.WriteString(v.renderKey("←/h", "Collapse or go to parent"))
	sb.WriteString(v.renderKey("→/l", "Expand"))
	sb.WriteString(v.renderKey("e", "Toggle expand/collapse all"))

	// Actions
	sb.WriteString("\n")
	sb.WriteString(v.styles.section.Render("Actions"))
	sb.WriteString("\n")
	sb.WriteString(v.renderKey("Enter", "View test logs"))
	sb.WriteString(v.renderKey("Space", "Toggle filter (All/Focus)"))
	sb.WriteString(v.renderKey("r", "Rerun selected test"))
	sb.WriteString(v.renderKey("R", "Rerun all failed tests"))

	// Other
	sb.WriteString("\n")
	sb.WriteString(v.styles.section.Render("Other"))
	sb.WriteString("\n")
	sb.WriteString(v.renderKey("?", "Toggle help"))
	sb.WriteString(v.renderKey("q", "Quit"))

	// Status Icons
	sb.WriteString("\n")
	sb.WriteString(v.styles.section.Render("Status Icons"))
	sb.WriteString("\n")
	sb.WriteString(v.renderKey("✓", "Passed"))
	sb.WriteString(v.renderKey("↯", "Passed (cached)"))
	sb.WriteString(v.renderKey("✗", "Failed"))
	sb.WriteString(v.renderKey("⊘", "Skipped"))
	sb.WriteString(v.renderKey("●", "Running"))
	sb.WriteString(v.renderKey("○", "Pending"))

	return sb.String()
}

func (v HelpView) renderLogContent() string {
	var sb strings.Builder

	// Navigation
	sb.WriteString("\n")
	sb.WriteString(v.styles.section.Render("Navigation"))
	sb.WriteString("\n")
	sb.WriteString(v.renderKey("↑/k ↓/j", "Scroll up/down"))
	sb.WriteString(v.renderKey("PgUp PgDn", "Page up/down"))
	sb.WriteString(v.renderKey("Ctrl+u/d", "Half page up/down"))
	sb.WriteString(v.renderKey("g G", "Go to top/bottom"))

	// Search
	sb.WriteString("\n")
	sb.WriteString(v.styles.section.Render("Search"))
	sb.WriteString("\n")
	sb.WriteString(v.renderKey("/", "Start search"))
	sb.WriteString(v.renderKey("n", "Jump to next match"))
	sb.WriteString(v.renderKey("N", "Jump to previous match"))
	sb.WriteString(v.renderKey("Enter", "Confirm search (in search mode)"))
	sb.WriteString(v.renderKey("Esc", "Cancel search (in search mode)"))

	// Actions
	sb.WriteString("\n")
	sb.WriteString(v.styles.section.Render("Actions"))
	sb.WriteString("\n")
	sb.WriteString(v.renderKey("Space", "Toggle view mode (Processed/Raw)"))
	sb.WriteString(v.renderKey("c", "Copy logs to clipboard"+getClipboardHint()))
	sb.WriteString(v.renderKey("r", "Rerun this test"))

	// Other
	sb.WriteString("\n")
	sb.WriteString(v.styles.section.Render("Other"))
	sb.WriteString("\n")
	sb.WriteString(v.renderKey("Esc/q", "Go back to tree view"))
	sb.WriteString(v.renderKey("Backspace", "Go back to tree view"))
	sb.WriteString(v.renderKey("?", "Toggle help"))

	// Log Markers
	sb.WriteString("\n")
	sb.WriteString(v.styles.section.Render("Log Markers"))
	sb.WriteString("\n")
	sb.WriteString(v.renderKey("=== RUN", "Test started"))
	sb.WriteString(v.renderKey("=== PAUSE", "Test paused (parallel)"))
	sb.WriteString(v.renderKey("=== CONT", "Test continued"))
	sb.WriteString(v.renderKey("--- PASS", "Test passed"))
	sb.WriteString(v.renderKey("--- FAIL", "Test failed"))
	sb.WriteString(v.renderKey("--- SKIP", "Test skipped"))

	return sb.String()
}

func (v HelpView) renderKey(key, desc string) string {
	return v.styles.key.Render(padRight(key, 12)) + v.styles.desc.Render(desc) + "\n"
}

func padRight(s string, width int) string {
	// Use visual width, not byte length (for Unicode characters)
	visualWidth := lipgloss.Width(s)
	if visualWidth >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visualWidth)
}

// getClipboardHint returns a hint about clipboard availability
func getClipboardHint() string {
	// Check if any clipboard command is available
	clipboardCmds := []string{"wl-copy", "xclip", "xsel", "pbcopy", "clip.exe"}
	for _, cmd := range clipboardCmds {
		if _, err := exec.LookPath(cmd); err == nil {
			return "" // Clipboard available, no hint needed
		}
	}

	// No clipboard command found - suggest installation based on display server
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return "\n             (install: sudo apt install wl-clipboard)"
	} else if os.Getenv("DISPLAY") != "" {
		return "\n             (install: sudo apt install xclip)"
	}
	return "\n             (no clipboard tool found)"
}
