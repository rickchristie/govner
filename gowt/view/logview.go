package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	model "github.com/rickchristie/govner/gowt/model"
)

// LogViewRequest represents a request from LogView to the controller
type LogViewRequest interface {
	isLogViewRequest()
}

// BackRequest is emitted when user wants to go back to tree view
type BackRequest struct{}

func (BackRequest) isLogViewRequest() {}

// CopyLogsRequest is emitted when user wants to copy logs
type CopyLogsRequest struct {
	Logs string
}

func (CopyLogsRequest) isLogViewRequest() {}

// OpenEditorRequest is emitted when user wants to open file in editor
type OpenEditorRequest struct {
	File string
	Line int
}

func (OpenEditorRequest) isLogViewRequest() {}

// LogRerunTestRequest is emitted when user wants to rerun the current test
type LogRerunTestRequest struct {
	Node *model.TestNode
}

func (LogRerunTestRequest) isLogViewRequest() {}

// ShowLogHelpRequest is emitted when user wants to see log help
type ShowLogHelpRequest struct{}

func (ShowLogHelpRequest) isLogViewRequest() {}

// LogViewMode represents the log display mode
type LogViewMode int

const (
	LogModeProcessed LogViewMode = iota // Styled/colored output (default)
	LogModeRaw                          // Raw unprocessed output
)

// LogView is a pure view for displaying test logs (Screen 2)
type LogView struct {
	node            *model.TestNode
	buffer          *model.LogBuffer   // Shared processed log buffer
	rawBuffer       *model.LogBuffer   // Shared raw log buffer
	renderer        *model.LogRenderer // Renders processed logs efficiently with incremental updates
	rawRenderer     *model.LogRenderer // Renders raw logs efficiently with incremental updates
	width           int
	height          int
	viewport        viewport.Model
	ready           bool
	styles          logStyles
	autoScroll      bool        // Auto-scroll to bottom when new content arrives
	animFrame       int         // Animation frame for spinner
	gotoBottom      bool        // Flag to scroll to bottom on next render
	copyAnimTime    int         // Frames remaining for copy animation (0 = not animating)
	copyAnimSuccess bool        // Whether copy was successful
	viewMode        LogViewMode // Current view mode (processed or raw)

	// Separate scroll states for each mode (-1 means "go to bottom")
	processedYOffset int // Saved scroll position for processed mode
	rawYOffset       int // Saved scroll position for raw mode

	// Search state
	searchMode         bool   // Whether search mode is active
	searchQuery        string // Current search query
	searchMatches      []int  // Line numbers (0-indexed) that match the query
	currentMatchIndex  int    // Index into searchMatches (-1 if none selected)
	searchYOffsetSaved int    // Scroll position before entering search mode

	// Highlighted content buffer (mirrors renderer but with search highlights applied)
	searchActive       bool            // Whether confirmed search is active (after Enter)
	highlightedContent strings.Builder // Content with search highlights applied
	highlightedLastEnd int             // Last renderer position we've highlighted up to
}

const scrollOffsetBottom = -1 // Sentinel value meaning "scroll to bottom"

type logStyles struct {
	header          lipgloss.Style
	helpBar         lipgloss.Style
	passed          lipgloss.Style
	failed          lipgloss.Style
	skipped         lipgloss.Style
	pending         lipgloss.Style
	scrollInfo      lipgloss.Style
	copySuccess     lipgloss.Style
	copyFailed      lipgloss.Style
	copySheen       lipgloss.Style // Bright highlight for sheen animation
	searchHighlight lipgloss.Style // Highlight for search matches
}

func defaultLogStyles() logStyles {
	return logStyles{
		header:          lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")),
		helpBar:         lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		passed:          lipgloss.NewStyle().Foreground(lipgloss.Color("82")),
		failed:          lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
		skipped:         lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		pending:         lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		scrollInfo:      lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		copySuccess:     lipgloss.NewStyle().Foreground(lipgloss.Color("82")).Bold(true),
		copyFailed:      lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true),
		copySheen:       lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Bold(true),                      // Bright white
		searchHighlight: lipgloss.NewStyle().Background(lipgloss.Color("220")).Foreground(lipgloss.Color("0")), // Yellow bg, black text
	}
}

func NewLogView() LogView {
	return LogView{
		styles:           defaultLogStyles(),
		processedYOffset: scrollOffsetBottom,
		rawYOffset:       scrollOffsetBottom,
	}
}

func (v LogView) Init() tea.Cmd {
	return nil
}

// Note: Spinner frames moved to icons.go as SpinnerFrames

func (v LogView) Tick() LogView {
	v.animFrame++
	if v.copyAnimTime > 0 {
		v.copyAnimTime--
	}
	return v
}

// TriggerCopyAnimation starts the copy animation (success or failure)
func (v LogView) TriggerCopyAnimation(success bool) LogView {
	v.copyAnimTime = 20 // Show for ~2 seconds (at 100ms tick rate)
	v.copyAnimSuccess = success
	return v
}

func (v LogView) SetData(node *model.TestNode, processedBuffer, rawBuffer *model.LogBuffer) LogView {
	v.node = node
	v.buffer = processedBuffer
	v.rawBuffer = rawBuffer
	v.autoScroll = node != nil && node.Status == model.StatusRunning

	// Reset scroll offsets for the new node (start at bottom for both modes)
	v.processedYOffset = scrollOffsetBottom
	v.rawYOffset = scrollOffsetBottom

	if node != nil && node.ProcessedLog != nil {
		v.renderer = model.NewLogRenderer(processedBuffer, node.ProcessedLog)
	} else {
		v.renderer = nil
	}

	if node != nil && node.RawLog != nil {
		v.rawRenderer = model.NewLogRenderer(rawBuffer, node.RawLog)
	} else {
		v.rawRenderer = nil
	}

	if v.ready {
		v.viewport.SetContent(v.getContent())
		v.viewport.GotoBottom()
	} else {
		v.gotoBottom = true
	}
	return v
}

func (v LogView) UpdateContent(node *model.TestNode) LogView {
	if v.node == nil || node.FullPath != v.node.FullPath {
		return v
	}

	wasRunning := v.node.Status == model.StatusRunning
	v.node = node

	// Create renderers if they don't exist yet but logs are now available
	if v.renderer == nil && node.ProcessedLog != nil && v.buffer != nil {
		v.renderer = model.NewLogRenderer(v.buffer, node.ProcessedLog)
	}
	if v.rawRenderer == nil && node.RawLog != nil && v.rawBuffer != nil {
		v.rawRenderer = model.NewLogRenderer(v.rawBuffer, node.RawLog)
	}

	// Update both renderers
	processedNew := v.renderer != nil && v.renderer.AppendNew()
	rawNew := v.rawRenderer != nil && v.rawRenderer.AppendNew()

	// Refresh viewport if the current mode's renderer has new content
	hasNew := (v.viewMode == LogModeProcessed && processedNew) || (v.viewMode == LogModeRaw && rawNew)
	if hasNew {
		// If search highlighting is active, append new content with highlights
		if v.searchActive {
			(&v).appendHighlightedContent()
		}

		if v.ready {
			wasAtBottom := v.viewport.AtBottom()
			v.viewport.SetContent(v.getContent())

			if v.gotoBottom || (v.autoScroll && wasAtBottom) {
				v.viewport.GotoBottom()
			}
			v.gotoBottom = false
		}
	}

	// When test transitions from running to completed, do final scroll if auto-scroll was active
	if wasRunning && node.Status != model.StatusRunning {
		if v.autoScroll && v.ready {
			v.viewport.GotoBottom()
		}
		v.autoScroll = false
	}

	return v
}

// getContent returns log content with end mark when test is completed
func (v LogView) getContent() string {
	var content string

	// Use highlighted content buffer if search is active
	if v.searchActive && v.highlightedContent.Len() > 0 {
		content = v.highlightedContent.String()
	} else {
		// Use regular renderer content
		if v.viewMode == LogModeRaw {
			if v.rawRenderer == nil || !v.rawRenderer.HasContent() {
				return "  (no output)"
			}
			content = v.rawRenderer.String()
		} else {
			if v.renderer == nil || !v.renderer.HasContent() {
				return "  (no output)"
			}
			content = v.renderer.String()
		}
	}

	// Add end mark when test is completed
	if v.node != nil && v.node.Status != model.StatusRunning && v.node.Status != model.StatusPending {
		endMark := lipgloss.NewStyle().Faint(true).Render("·  end of log  ·")
		content += "\n" + endMark + "\n"
	}

	// Wrap long lines to viewport width so viewport line count matches terminal display.
	// Without this, scrolling breaks because viewport thinks there are N lines but
	// terminal shows M lines (M > N due to wrapping).
	if v.ready && v.viewport.Width > 0 {
		content = softWrap(content, v.viewport.Width)
	}

	return content
}

// softWrap wraps content to fit within width, preserving ANSI codes.
// Optimized: scans content without allocating a slice for all lines.
func softWrap(content string, width int) string {
	if width <= 0 || len(content) == 0 {
		return content
	}

	// Quick check: scan for any line that needs wrapping without splitting
	needsWrap := false
	lineStart := 0
	for i := 0; i <= len(content); i++ {
		if i == len(content) || content[i] == '\n' {
			line := content[lineStart:i]
			if lipgloss.Width(line) > width {
				needsWrap = true
				break
			}
			lineStart = i + 1
		}
	}

	if !needsWrap {
		return content
	}

	// Need to wrap - process line by line without intermediate slice
	var result strings.Builder
	result.Grow(len(content) + len(content)/width) // Estimate extra newlines

	lineStart = 0
	firstLine := true
	for i := 0; i <= len(content); i++ {
		if i == len(content) || content[i] == '\n' {
			if !firstLine {
				result.WriteByte('\n')
			}
			firstLine = false

			line := content[lineStart:i]
			lineWidth := lipgloss.Width(line)

			if lineWidth <= width {
				result.WriteString(line)
			} else if !strings.Contains(line, "\x1b") {
				// No ANSI codes - simple and fast byte slicing
				for len(line) > 0 {
					if len(line) <= width {
						result.WriteString(line)
						break
					}
					result.WriteString(line[:width])
					result.WriteByte('\n')
					line = line[width:]
				}
			} else {
				// Has ANSI codes - need careful handling
				result.WriteString(wrapLineWithANSI(line, width))
			}

			lineStart = i + 1
		}
	}

	return result.String()
}

// wrapLineWithANSI wraps a line that contains ANSI escape codes.
func wrapLineWithANSI(line string, width int) string {
	var result strings.Builder
	var visibleWidth int
	var inEscape bool

	for _, r := range line {
		if r == '\x1b' {
			inEscape = true
			result.WriteRune(r)
			continue
		}

		if inEscape {
			result.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}

		if visibleWidth >= width {
			result.WriteByte('\n')
			visibleWidth = 0
		}

		result.WriteRune(r)
		visibleWidth++
	}

	return result.String()
}

type logKeyMap struct {
	Up         key.Binding
	Down       key.Binding
	PageUp     key.Binding
	PageDown   key.Binding
	Top        key.Binding
	Bottom     key.Binding
	Back       key.Binding
	Copy       key.Binding
	Rerun      key.Binding
	Help       key.Binding
	ToggleMode key.Binding
	Search     key.Binding
	NextMatch  key.Binding
	PrevMatch  key.Binding
}

var logKeys = logKeyMap{
	Up:         key.NewBinding(key.WithKeys("up", "k", "K")),
	Down:       key.NewBinding(key.WithKeys("down", "j", "J")),
	PageUp:     key.NewBinding(key.WithKeys("pgup", "ctrl+u", "ctrl+U")),
	PageDown:   key.NewBinding(key.WithKeys("pgdown", "ctrl+d", "ctrl+D")),
	Top:        key.NewBinding(key.WithKeys("g")),
	Bottom:     key.NewBinding(key.WithKeys("G")),
	Back:       key.NewBinding(key.WithKeys("esc", "backspace", "q", "Q")),
	Copy:       key.NewBinding(key.WithKeys("c", "C")),
	Rerun:      key.NewBinding(key.WithKeys("r", "R")),
	Help:       key.NewBinding(key.WithKeys("?")),
	ToggleMode: key.NewBinding(key.WithKeys(" ")),
	Search:     key.NewBinding(key.WithKeys("/")),
	NextMatch:  key.NewBinding(key.WithKeys("n")),
	PrevMatch:  key.NewBinding(key.WithKeys("N")),
}

func (v LogView) Update(msg tea.Msg) (LogView, tea.Cmd, LogViewRequest) {
	var cmd tea.Cmd
	var request LogViewRequest

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height

		headerHeight := 3 // header + help bar + empty line

		if !v.ready {
			v.viewport = viewport.New(msg.Width, msg.Height-headerHeight)
			v.viewport.Style = lipgloss.NewStyle()
			v.ready = true
			if v.renderer != nil || v.rawRenderer != nil {
				v.viewport.SetContent(v.getContent())
				if v.gotoBottom {
					v.viewport.GotoBottom()
					v.gotoBottom = false
				}
			}
		} else {
			// Check if width changed - need to recalculate line wrapping
			widthChanged := v.viewport.Width != msg.Width
			v.viewport.Width = msg.Width
			v.viewport.Height = msg.Height - headerHeight

			// Re-set content to recalculate line wrapping for new width
			if widthChanged {
				wasAtBottom := v.viewport.AtBottom()
				v.viewport.SetContent(v.getContent())
				if wasAtBottom {
					v.viewport.GotoBottom()
				}
			}
		}

	case tea.KeyMsg:
		// Handle search mode input
		if v.searchMode {
			switch msg.Type {
			case tea.KeyEsc:
				// Exit search mode, clear search and restore scroll position
				v.searchMode = false
				v.searchQuery = ""
				v.searchMatches = nil
				v.currentMatchIndex = -1
				v.searchActive = false
				v.highlightedContent.Reset()
				v.highlightedLastEnd = 0
				if v.ready {
					v.viewport.SetContent(v.getContent())
					v.viewport.SetYOffset(v.searchYOffsetSaved)
				}
				return v, cmd, request

			case tea.KeyEnter:
				// Confirm search, activate highlighting and stay at current match position
				v.searchMode = false
				if v.searchQuery != "" && len(v.searchMatches) > 0 {
					v.searchActive = true
					v.rebuildHighlightedContent()
					if v.ready {
						v.viewport.SetContent(v.getContent())
					}
				}
				return v, cmd, request

			case tea.KeyBackspace:
				if len(v.searchQuery) > 0 {
					v.searchQuery = v.searchQuery[:len(v.searchQuery)-1]
					v.performSearch()
				}
				return v, cmd, request

			case tea.KeyRunes:
				v.searchQuery += string(msg.Runes)
				v.performSearch()
				return v, cmd, request
			}
			return v, cmd, request
		}

		switch {
		case key.Matches(msg, logKeys.Search):
			// Enter search mode, clear any previous search highlighting
			v.searchMode = true
			v.searchQuery = ""
			v.searchMatches = nil
			v.currentMatchIndex = -1
			v.searchActive = false
			v.highlightedContent.Reset()
			v.highlightedLastEnd = 0
			if v.ready {
				v.searchYOffsetSaved = v.viewport.YOffset
				v.viewport.SetContent(v.getContent()) // Refresh to clear old highlights
			}
			return v, cmd, request

		case key.Matches(msg, logKeys.NextMatch):
			// Jump to next match
			if len(v.searchMatches) > 0 {
				v.currentMatchIndex = (v.currentMatchIndex + 1) % len(v.searchMatches)
				v.scrollToCurrentMatch()
			}
			return v, cmd, request

		case key.Matches(msg, logKeys.PrevMatch):
			// Jump to previous match
			if len(v.searchMatches) > 0 {
				v.currentMatchIndex--
				if v.currentMatchIndex < 0 {
					v.currentMatchIndex = len(v.searchMatches) - 1
				}
				v.scrollToCurrentMatch()
			}
			return v, cmd, request

		case key.Matches(msg, logKeys.Help):
			request = ShowLogHelpRequest{}
			return v, cmd, request

		case key.Matches(msg, logKeys.Back):
			request = BackRequest{}
			return v, cmd, request

		case key.Matches(msg, logKeys.Copy):
			if v.node != nil {
				var content string
				if v.viewMode == LogModeRaw && v.rawRenderer != nil {
					// Copy raw log content
					content = v.rawRenderer.String()
				} else if v.renderer != nil {
					// Copy processed log content (strip ANSI codes)
					content = stripAnsi(v.renderer.String())
				}
				if content != "" {
					request = CopyLogsRequest{Logs: content}
				}
			}
			return v, cmd, request

		case key.Matches(msg, logKeys.Rerun):
			if v.node != nil {
				request = LogRerunTestRequest{Node: v.node}
			}
			return v, cmd, request

		case key.Matches(msg, logKeys.ToggleMode):
			// Save current scroll position before switching
			if v.ready {
				if v.viewMode == LogModeProcessed {
					v.processedYOffset = v.viewport.YOffset
				} else {
					v.rawYOffset = v.viewport.YOffset
				}
			}

			// Toggle between processed and raw view modes
			if v.viewMode == LogModeProcessed {
				v.viewMode = LogModeRaw
			} else {
				v.viewMode = LogModeProcessed
			}

			// Rebuild the renderer for the new mode to ensure all content is captured
			// This handles cases where refs arrived out of order or AppendNew missed updates
			if v.viewMode == LogModeRaw {
				if v.rawBuffer != nil && v.node != nil && v.node.RawLog != nil {
					v.rawRenderer = model.NewLogRenderer(v.rawBuffer, v.node.RawLog)
				}
			} else {
				if v.buffer != nil && v.node != nil && v.node.ProcessedLog != nil {
					v.renderer = model.NewLogRenderer(v.buffer, v.node.ProcessedLog)
				}
			}

			// Refresh viewport content with new mode and restore scroll position
			if v.ready {
				v.viewport.SetContent(v.getContent())
				// Restore the saved scroll position for the new mode
				var targetOffset int
				if v.viewMode == LogModeProcessed {
					targetOffset = v.processedYOffset
				} else {
					targetOffset = v.rawYOffset
				}
				if targetOffset == scrollOffsetBottom {
					v.viewport.GotoBottom()
				} else {
					v.viewport.SetYOffset(targetOffset)
				}
			}
			return v, cmd, request

		case key.Matches(msg, logKeys.Top):
			v.viewport.GotoTop()
			v.autoScroll = false
			return v, cmd, request

		case key.Matches(msg, logKeys.Bottom):
			v.viewport.GotoBottom()
			if v.node != nil && v.node.Status == model.StatusRunning {
				v.autoScroll = true
			}
			return v, cmd, request

		case key.Matches(msg, logKeys.Up), key.Matches(msg, logKeys.PageUp):
			v.autoScroll = false
			v.viewport, cmd = v.viewport.Update(msg)
			return v, cmd, request

		case key.Matches(msg, logKeys.Down), key.Matches(msg, logKeys.PageDown):
			v.viewport, cmd = v.viewport.Update(msg)
			if v.viewport.AtBottom() && v.node != nil && v.node.Status == model.StatusRunning {
				v.autoScroll = true
			}
			return v, cmd, request
		}

		v.viewport, cmd = v.viewport.Update(msg)
	}

	return v, cmd, request
}

// performSearch searches the log content for the query and updates matches.
// Note: This only finds matches; highlighting is applied on Enter via rebuildHighlightedContent.
func (v *LogView) performSearch() {
	v.searchMatches = nil
	v.currentMatchIndex = -1

	if v.searchQuery == "" {
		return
	}

	// Get the content to search (strip ANSI codes for searching)
	var content string
	if v.viewMode == LogModeRaw {
		if v.rawRenderer != nil {
			content = v.rawRenderer.String()
		}
	} else {
		if v.renderer != nil {
			content = stripAnsi(v.renderer.String())
		}
	}

	if content == "" {
		return
	}

	// Search for exact matches line by line
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.Contains(line, v.searchQuery) {
			v.searchMatches = append(v.searchMatches, i)
		}
	}

	// Jump to first match if any found
	if len(v.searchMatches) > 0 {
		v.currentMatchIndex = 0
		v.scrollToCurrentMatch()
	}
}

// rebuildHighlightedContent rebuilds the entire highlighted content buffer from scratch.
// Called when search is confirmed (Enter) to apply highlighting to all content.
func (v *LogView) rebuildHighlightedContent() {
	v.highlightedContent.Reset()
	v.highlightedLastEnd = 0

	if v.searchQuery == "" {
		return
	}

	// Get the current renderer based on view mode
	var renderer *model.LogRenderer
	if v.viewMode == LogModeRaw {
		renderer = v.rawRenderer
	} else {
		renderer = v.renderer
	}

	if renderer == nil || !renderer.HasContent() {
		return
	}

	// Get full content and apply highlighting
	rawContent := renderer.String()
	highlighted := v.styles.searchHighlight.Render(v.searchQuery)
	content := strings.ReplaceAll(rawContent, v.searchQuery, highlighted)

	v.highlightedContent.WriteString(content)
	v.highlightedLastEnd = len(rawContent)
}

// appendHighlightedContent appends only new content with highlighting applied.
// Called during streaming when searchActive is true.
// Also updates searchMatches with any new matches found.
func (v *LogView) appendHighlightedContent() {
	if !v.searchActive || v.searchQuery == "" {
		return
	}

	// Get the current renderer based on view mode
	var renderer *model.LogRenderer
	if v.viewMode == LogModeRaw {
		renderer = v.rawRenderer
	} else {
		renderer = v.renderer
	}

	if renderer == nil {
		return
	}

	// Get full content to check length and extract new portion
	fullContent := renderer.String()
	currentLen := len(fullContent)
	if currentLen <= v.highlightedLastEnd {
		return // No new content
	}

	// Count existing lines to calculate correct line numbers for new matches
	existingContent := fullContent[:v.highlightedLastEnd]
	baseLineNum := strings.Count(existingContent, "\n")

	// Get only the new portion of content
	newContent := fullContent[v.highlightedLastEnd:]

	// Count new matches and add to searchMatches
	// Strip ANSI for searching in processed mode
	searchContent := newContent
	if v.viewMode == LogModeProcessed {
		searchContent = stripAnsi(newContent)
	}
	newLines := strings.Split(searchContent, "\n")
	for i, line := range newLines {
		if strings.Contains(line, v.searchQuery) {
			v.searchMatches = append(v.searchMatches, baseLineNum+i)
		}
	}

	// Apply highlighting to new content only
	highlighted := v.styles.searchHighlight.Render(v.searchQuery)
	newContent = strings.ReplaceAll(newContent, v.searchQuery, highlighted)

	v.highlightedContent.WriteString(newContent)
	v.highlightedLastEnd = currentLen
}

// scrollToCurrentMatch scrolls the viewport to show the current match
func (v *LogView) scrollToCurrentMatch() {
	if v.currentMatchIndex < 0 || v.currentMatchIndex >= len(v.searchMatches) {
		return
	}

	if !v.ready {
		return
	}

	matchLine := v.searchMatches[v.currentMatchIndex]

	// Center the match in the viewport
	viewportHeight := v.viewport.Height
	targetOffset := matchLine - viewportHeight/2
	if targetOffset < 0 {
		targetOffset = 0
	}

	v.viewport.SetYOffset(targetOffset)
	v.autoScroll = false
}

func (v LogView) View() string {
	if v.node == nil {
		return "No test selected"
	}

	var sb strings.Builder

	// ANSI reset at start: clears any lingering state from previous frame
	// (necessary due to Bubble Tea's partial screen updates)
	sb.WriteString("\x1b[0m")

	sb.WriteString(v.renderHeader())
	sb.WriteString("\n")
	sb.WriteString(v.renderHelpBar())
	sb.WriteString("\n\n")

	if v.ready {
		sb.WriteString(v.viewport.View())
	} else {
		sb.WriteString(v.getContent())
	}

	// ANSI reset at end: ensures clean state for next frame
	sb.WriteString("\x1b[0m")

	return sb.String()
}

func (v LogView) renderHeader() string {
	if v.node == nil {
		return v.styles.header.Render(IconCharGear + " GOWT")
	}

	var logo string
	switch v.node.Status {
	case model.StatusRunning:
		logo = GetSpinnerGear(v.animFrame) // Pre-rendered gear with spinner color
	case model.StatusFailed:
		logo = IconGearFailed // Pre-rendered red gear
	default:
		logo = IconGearPassed // Pre-rendered green gear
	}

	statusIndicator := v.renderStatusIcon(v.node.Status)
	path := model.ShortPath(v.node.FullPath)

	return logo + " " + v.styles.header.Render("GOWT") + "  " + statusIndicator + " " + path
}

func (v LogView) renderHelpBar() string {
	var helpRendered string
	var helpWidth int

	// Search mode has its own help bar
	if v.searchMode {
		searchPrefix := v.styles.helpBar.Render("/")
		searchQuery := v.searchQuery
		cursor := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Render("█")
		var matchInfo string
		if len(v.searchMatches) > 0 {
			matchInfo = fmt.Sprintf(" [%d/%d]", v.currentMatchIndex+1, len(v.searchMatches))
		} else if v.searchQuery != "" {
			matchInfo = " [no matches]"
		}
		hint := v.styles.helpBar.Render("  [Enter Confirm]  [Esc Cancel]")
		helpRendered = searchPrefix + searchQuery + cursor + v.styles.helpBar.Render(matchInfo) + hint
		helpWidth = lipgloss.Width("/" + searchQuery + "█" + matchInfo + "  [Enter Confirm]  [Esc Cancel]")

		scrollInfo := ""
		if v.ready {
			totalLines := v.viewport.TotalLineCount()
			currentLine := v.viewport.YOffset + v.viewport.Height
			if currentLine > totalLines {
				currentLine = totalLines
			}
			scrollInfo = fmt.Sprintf("─ %3.f%% ─ %d/%d", v.viewport.ScrollPercent()*100, currentLine, totalLines)
		}

		scrollLen := len(scrollInfo)
		padding := v.width - helpWidth - scrollLen
		if padding < 1 {
			padding = 1
		}

		return helpRendered + strings.Repeat(" ", padding) + v.styles.scrollInfo.Render(scrollInfo)
	}

	// Mode indicator
	var modeText string
	if v.viewMode == LogModeRaw {
		modeText = "Raw"
	} else {
		modeText = "Processed"
	}

	if v.copyAnimTime > 0 {
		// Show copy animation - build with mixed styles
		prefix := v.styles.helpBar.Render("[Esc Back]  [↑↓ Scroll]  [Space " + modeText + "]  ")
		var statusText string
		if v.copyAnimSuccess {
			statusText = v.renderCopyWithSheen()
		} else {
			statusText = v.styles.copyFailed.Render("✗ No clipboard")
		}
		suffix := v.styles.helpBar.Render("  [r Rerun]  [? Help]")
		helpRendered = prefix + statusText + suffix
		// Use longer text for width calculation to ensure consistent padding
		helpWidth = lipgloss.Width("[Esc Back]  [↑↓ Scroll]  [Space Processed]  ✗ No clipboard  [r Rerun]  [? Help]")
	} else {
		// Show search hint with n/N if there are matches
		var searchHint string
		if len(v.searchMatches) > 0 {
			searchHint = fmt.Sprintf("  [n/N %d matches]", len(v.searchMatches))
		} else {
			searchHint = "  [/ Search]"
		}
		help := "[Esc Back]  [↑↓ Scroll]  [Space " + modeText + "]  [c Copy]" + searchHint + "  [? Help]"
		helpRendered = v.styles.helpBar.Render(help)
		helpWidth = lipgloss.Width(help)
	}

	scrollInfo := ""
	if v.ready {
		totalLines := v.viewport.TotalLineCount()
		currentLine := v.viewport.YOffset + v.viewport.Height
		if currentLine > totalLines {
			currentLine = totalLines
		}
		scrollInfo = fmt.Sprintf("─ %3.f%% ─ %d/%d", v.viewport.ScrollPercent()*100, currentLine, totalLines)
	}

	scrollLen := len(scrollInfo)
	padding := v.width - helpWidth - scrollLen
	if padding < 1 {
		padding = 1
	}

	return helpRendered + strings.Repeat(" ", padding) + v.styles.scrollInfo.Render(scrollInfo)
}

// renderCopyWithSheen renders "✓ Copied!" with a metal sheen animation
func (v LogView) renderCopyWithSheen() string {
	text := []rune("✓ Copied!")
	textLen := len(text)

	// Animation phases:
	// Frames 20-11: Sheen sweeps left to right (10 frames for 9 chars)
	// Frames 10-1: Solid green display
	sheenEnd := 11 // Frame where sheen animation ends

	if v.copyAnimTime >= sheenEnd {
		// Sheen animation phase
		// Calculate sheen position (0 to textLen+2 for smooth entry/exit)
		progress := 20 - v.copyAnimTime           // 0 to 9
		sheenPos := progress * (textLen + 2) / 10 // Position of sheen center
		sheenWidth := 3                           // Width of the sheen highlight

		var result strings.Builder
		for i, r := range text {
			// Check if this character is within the sheen highlight
			distFromSheen := sheenPos - i
			if distFromSheen >= 0 && distFromSheen < sheenWidth {
				// In the sheen - use bright white
				result.WriteString(v.styles.copySheen.Render(string(r)))
			} else {
				// Normal green
				result.WriteString(v.styles.copySuccess.Render(string(r)))
			}
		}
		return result.String()
	}

	// Solid display phase - just green
	return v.styles.copySuccess.Render(string(text))
}

func (v LogView) renderStatusIcon(status model.TestStatus) string {
	switch status {
	case model.StatusPassed:
		return IconPassedCompact // Pre-rendered, no trailing space
	case model.StatusFailed:
		return IconFailedCompact // Pre-rendered, no trailing space
	case model.StatusSkipped:
		return IconSkippedCompact // Pre-rendered, no trailing space
	case model.StatusRunning:
		return GetSpinnerIconCompact(v.animFrame) // Pre-rendered, no trailing space
	case model.StatusPending:
		return IconPendingCompact // Pre-rendered, no trailing space
	default:
		return "?"
	}
}

func (v LogView) GetNode() *model.TestNode {
	return v.node
}

// IsAnimating returns true if there's an active animation that needs ticks
func (v LogView) IsAnimating() bool {
	return v.copyAnimTime > 0
}

func (v LogView) GetCopyCommand() string {
	if v.node == nil {
		return ""
	}

	testName := v.node.Name
	pkg := v.node.Package

	if v.node.Parent != nil && v.node.Parent.Parent != nil {
		parts := []string{}
		current := v.node
		for current != nil && current.Parent != nil {
			parts = append([]string{current.Name}, parts...)
			current = current.Parent
		}
		testName = strings.Join(parts, "/")
	}

	return fmt.Sprintf("go test -v -run '%s' %s", testName, pkg)
}
