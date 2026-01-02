package tui

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/rickchristie/govner/pgflock/internal/locker"
)

// View renders the entire TUI.
func (m *Model) View() string {
	if m.quitting {
		return "Shutting down...\n"
	}

	// Show loading screen if active (startup or shutdown)
	if m.showingLoading && !m.loadingScreen.IsDone() {
		return m.renderLoadingView()
	}

	// If modal is showing, render centered modal overlay
	if m.confirm != ConfirmNone {
		return m.renderModalOverlay()
	}

	return strings.Join(m.renderMainView(), "\n")
}

// renderMainView renders the main view and returns lines (for reuse in modal overlay).
func (m *Model) renderMainView() []string {
	// Get terminal dimensions
	width := m.width
	if width <= 0 {
		width = 80
	}
	height := m.height
	if height <= 0 {
		height = 24
	}

	// Fixed sections: header (1) + section header (1) + footer separator (1) + footer (1) = 4 lines
	headerHeight := 2 // header + section header
	footerHeight := 2 // separator + help bar
	contentAreaHeight := height - headerHeight - footerHeight
	if contentAreaHeight < 1 {
		contentAreaHeight = 1
	}

	// Build the output
	var lines []string

	// FIXED: Header line
	lines = append(lines, m.renderHeader(width))

	// FIXED: Section header (extends to terminal width)
	lines = append(lines, m.renderSectionHeader(width))

	// Get all content lines
	var contentLines []string
	if m.showAllDatabases {
		contentLines = strings.Split(m.renderAllDatabases(), "\n")
	} else {
		contentLines = strings.Split(m.renderLockedDatabases(), "\n")
	}

	// Error message if any
	if m.err != nil {
		contentLines = append(contentLines, ErrorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
	}

	// Check if we're showing empty state (need to center it)
	isEmptyState := !m.showAllDatabases && (m.state == nil || len(m.state.Locks) == 0)
	totalContentLines := len(contentLines)

	if isEmptyState && contentAreaHeight > 0 {
		// Center empty state vertically in content area
		contentHeight := len(contentLines)
		topPadding := (contentAreaHeight - contentHeight) / 2
		if topPadding < 0 {
			topPadding = 0
		}

		// Add top padding
		for i := 0; i < topPadding; i++ {
			lines = append(lines, "")
		}

		// Add centered content
		for _, line := range contentLines {
			lines = append(lines, centerText(line, width))
		}

		// Add bottom padding to fill content area
		bottomPadding := contentAreaHeight - topPadding - contentHeight
		if bottomPadding < 0 {
			bottomPadding = 0
		}
		for i := 0; i < bottomPadding; i++ {
			lines = append(lines, "")
		}
	} else {
		// Scrollable content area
		// Ensure scroll offset keeps selected item visible
		m.ensureSelectedVisible(totalContentLines, contentAreaHeight)

		// Apply scroll offset - show only visible portion
		startIdx := m.scrollOffset
		endIdx := m.scrollOffset + contentAreaHeight
		if endIdx > totalContentLines {
			endIdx = totalContentLines
		}

		// Add visible content lines
		visibleLines := 0
		for i := startIdx; i < endIdx; i++ {
			lines = append(lines, contentLines[i])
			visibleLines++
		}

		// Pad remaining space in content area
		for i := visibleLines; i < contentAreaHeight; i++ {
			lines = append(lines, "")
		}
	}

	// FIXED: Footer separator + help bar
	lines = append(lines, m.renderSectionHeader(width))
	lines = append(lines, m.renderHelpBar(width, totalContentLines, contentAreaHeight))

	return lines
}

// ensureSelectedVisible adjusts scroll offset to keep selected item visible
func (m *Model) ensureSelectedVisible(totalLines, visibleHeight int) {
	if totalLines <= visibleHeight {
		// No scrolling needed, reset offset
		m.scrollOffset = 0
		return
	}

	// If selected is above visible area, scroll up
	if m.selectedIdx < m.scrollOffset {
		m.scrollOffset = m.selectedIdx
	}

	// If selected is below visible area, scroll down
	if m.selectedIdx >= m.scrollOffset+visibleHeight {
		m.scrollOffset = m.selectedIdx - visibleHeight + 1
	}

	// Clamp scroll offset
	maxOffset := totalLines - visibleHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.scrollOffset > maxOffset {
		m.scrollOffset = maxOffset
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

// renderHeader renders: 🐑 pgflock ✓ 2 instances ● 3/25 locked ○ 22 free  [view toggle]
func (m *Model) renderHeader(width int) string {
	var parts []string

	// Brand
	parts = append(parts, TitleStyle.Render(SheepEmoji+" pgflock"))

	// Instances
	instanceText := fmt.Sprintf("%s %d instances", IconCheckmark, m.instanceCount())
	parts = append(parts, InstancesStyle.Render(instanceText))

	// Locked count - show sleeping when none locked, animated when locked
	if m.lockedCount() == 0 {
		parts = append(parts, DimStyle.Render(SleepingEmoji+" Sleeping"))
	} else {
		lockedIcon := m.lockedAnimator.Icon()
		lockedStyle := GetLockedCountStyle(m.lockedAnimator.Frame())
		lockedText := lockedStyle.Render(fmt.Sprintf("%s %d", lockedIcon, m.lockedCount()))
		totalText := DimStyle.Render(fmt.Sprintf("/%d locked", m.totalCount()))
		parts = append(parts, lockedText+totalText)
	}

	// Free count
	freeText := fmt.Sprintf("%s %d free", IconFree, m.freeCount())
	parts = append(parts, FreeCountStyle.Render(freeText))

	// Waiting (if any)
	if m.waitingCount() > 0 {
		waitingText := fmt.Sprintf("%s %d waiting", IconFarmer, m.waitingCount())
		parts = append(parts, WaitingCountStyle.Render(waitingText))
	}

	leftContent := strings.Join(parts, "  ")
	leftWidth := lipglossWidth(leftContent)

	// View toggle at right: "All Databases (10) | Locked Databases"
	var viewToggle string
	if m.showAllDatabases {
		viewToggle = TitleStyle.Render(fmt.Sprintf("All Databases (%d)", m.totalCount())) +
			DimStyle.Render(" | ") +
			DimStyle.Render("Locked Databases")
	} else {
		viewToggle = DimStyle.Render(fmt.Sprintf("All Databases (%d)", m.totalCount())) +
			DimStyle.Render(" | ") +
			TitleStyle.Render("Locked Databases")
	}
	toggleWidth := lipglossWidth(viewToggle)

	// Calculate padding to push toggle to far right
	paddingWidth := width - leftWidth - toggleWidth
	if paddingWidth < 2 {
		paddingWidth = 2
	}

	return leftContent + strings.Repeat(" ", paddingWidth) + viewToggle
}

// renderSectionHeader renders a horizontal line extending to terminal width
func (m *Model) renderSectionHeader(width int) string {
	return SectionHeaderStyle.Render(strings.Repeat(BorderLightH, width))
}

// renderLockedDatabases renders the list of locked databases
func (m *Model) renderLockedDatabases() string {
	if m.state == nil || len(m.state.Locks) == 0 {
		return m.renderEmptyState()
	}

	var b strings.Builder
	for i, lock := range m.state.Locks {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(m.renderDatabaseRow(i, lock.ConnString, true, &lock))
	}
	return b.String()
}

// renderAllDatabases renders all databases in the pool
func (m *Model) renderAllDatabases() string {
	if len(m.allDatabases) == 0 {
		return EmptyStateStyle.Render("(no databases configured)")
	}

	var b strings.Builder
	for i, db := range m.allDatabases {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(m.renderDatabaseRow(i, db.ConnString, db.IsLocked, db.LockInfo))
	}
	return b.String()
}

// renderDatabaseRow renders a single database row
func (m *Model) renderDatabaseRow(idx int, connStr string, isLocked bool, lockInfo *locker.LockInfo) string {
	isSelected := idx == m.selectedIdx
	dbName, port := parseConnString(connStr)

	// Status and details
	var statusPart string
	if isLocked && lockInfo != nil {
		// Calculate timeout progress
		elapsed := time.Since(lockInfo.LockedAt)
		timeout := time.Duration(m.cfg.AutoUnlockMins) * time.Minute
		progress := float64(elapsed) / float64(timeout)
		if progress > 1.0 {
			progress = 1.0
		}

		// Animated LOCKED status with timeout progress bar
		statusPart = m.lockedAnimator.Render() +
			"  " + MarkerStyle.Render(fmt.Sprintf("[%s]", lockInfo.Marker)) +
			"  " + DurationStyle.Render(formatDuration(elapsed)) +
			"  " + m.lockTimeoutBar.Render(progress)
	} else {
		// FREE status
		statusPart = FreeStatusStyle.Render(IconFree + " FREE")
	}

	// Apply row style - background must cover arrow and db identifier together
	if isSelected {
		// Selected row: apply background to entire "▶ dbname:port" as one styled unit
		selectablePart := IconSelectionArrow + " " + dbName + ":" + port
		return RowSelectedStyle.Render(selectablePart) + "  " + statusPart
	}
	// Normal row: add spacing to align with padded selected row (extra space for right padding)
	return RowNormalStyle.Render("   "+dbName) + PortStyle.Render(":"+port) + "   " + statusPart
}

// renderEmptyState renders the peaceful flock message
func (m *Model) renderEmptyState() string {
	line1 := "💤 " + SheepEmoji + " 💤"
	line2 := EmptyStateStyle.Render("The flock rests peacefully")
	line3 := EmptyStateStyle.Render(fmt.Sprintf("All %d databases are free", m.totalCount()))
	return line1 + "\n" + line2 + "\n" + line3
}

// renderHelpBar renders the help bar at the bottom with sheep at far right
func (m *Model) renderHelpBar(width, totalLines, visibleHeight int) string {
	var parts []string

	parts = append(parts, renderHelpKey("q", "Quit"))
	parts = append(parts, renderHelpKey("r", "Restart"))

	// Toggle view
	if m.showAllDatabases {
		parts = append(parts, renderHelpKey("Space", "Locked Only"))
	} else {
		parts = append(parts, renderHelpKey("Space", "Show All"))
	}

	// Context-sensitive options
	if db := m.selectedDatabase(); db != nil {
		if db.IsLocked {
			parts = append(parts, renderHelpKey("u", "Unlock"))
		}

		// Copy with shimmer animation
		if m.copyShimmer.IsActive() {
			parts = append(parts, m.copyShimmer.Render())
		} else {
			parts = append(parts, renderHelpKey("c", "Copy"))
		}

		parts = append(parts, renderHelpKey(NavArrows, "Nav"))
	}

	leftContent := strings.Join(parts, "  ")
	leftWidth := lipglossWidth(leftContent)

	// Build right side: scroll indicator + sheep
	var rightContent string

	// Add scroll indicator if content is scrollable
	if totalLines > visibleHeight {
		// Calculate scroll percentage
		maxOffset := totalLines - visibleHeight
		scrollPercent := 0
		if maxOffset > 0 {
			scrollPercent = (m.scrollOffset * 100) / maxOffset
		}
		// Show current line range and percentage
		startLine := m.scrollOffset + 1
		endLine := m.scrollOffset + visibleHeight
		if endLine > totalLines {
			endLine = totalLines
		}
		scrollInfo := DimStyle.Render(fmt.Sprintf("%d-%d/%d %d%%", startLine, endLine, totalLines, scrollPercent))
		rightContent = scrollInfo + "  " + SheepEmoji
	} else {
		rightContent = SheepEmoji
	}

	rightWidth := lipglossWidth(rightContent)

	// Calculate padding between left and right
	paddingWidth := width - leftWidth - rightWidth
	if paddingWidth < 2 {
		paddingWidth = 2
	}

	return leftContent + strings.Repeat(" ", paddingWidth) + rightContent
}

// renderHelpKey renders a help item like "[q Quit]"
func renderHelpKey(key, desc string) string {
	return "[" + HelpKeyStyle.Render(key) + " " + HelpDescStyle.Render(desc) + "]"
}

// renderModalOverlay renders the main view dimmed with a centered modal overlay.
func (m *Model) renderModalOverlay() string {
	width := m.width
	if width <= 0 {
		width = 80
	}

	// Get the main view lines and dim them
	bgLines := m.renderMainView()
	for i := range bgLines {
		bgLines[i] = dimLine(bgLines[i])
	}

	// Get the modal content
	modal := m.renderModal()
	modalLines := strings.Split(modal, "\n")
	modalHeight := len(modalLines)
	modalWidth := 0
	for _, line := range modalLines {
		w := lipglossWidth(line)
		if w > modalWidth {
			modalWidth = w
		}
	}

	// Calculate vertical position to center modal
	topRow := (len(bgLines) - modalHeight) / 2
	if topRow < 0 {
		topRow = 0
	}

	// Calculate horizontal padding to center modal
	leftPadding := (width - modalWidth) / 2
	if leftPadding < 0 {
		leftPadding = 0
	}

	// Overlay modal on background
	for i, modalLine := range modalLines {
		bgIdx := topRow + i
		if bgIdx >= 0 && bgIdx < len(bgLines) {
			// Replace the background line with: left-dim + modal + right-dim
			bgLines[bgIdx] = overlayLine(bgLines[bgIdx], modalLine, leftPadding, width)
		}
	}

	return strings.Join(bgLines, "\n")
}

// dimLine dims a line by stripping ANSI codes and applying dim color.
func dimLine(s string) string {
	// Strip ANSI codes and get plain text
	plain := stripAnsi(s)
	if plain == "" {
		return ""
	}
	return DimBackdropStyle.Render(plain)
}

// stripAnsi removes ANSI escape sequences from a string.
func stripAnsi(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// overlayLine overlays a modal line on a dimmed background line.
func overlayLine(bg, modal string, leftPad, totalWidth int) string {
	// Get plain background text for the dim portions
	plainBg := stripAnsi(bg)

	// Pad background to full width if needed
	for len(plainBg) < totalWidth {
		plainBg += " "
	}

	modalWidth := lipglossWidth(modal)
	rightStart := leftPad + modalWidth

	// Build: dimmed-left + modal + dimmed-right
	var b strings.Builder

	// Left portion (dimmed)
	if leftPad > 0 {
		leftText := truncateToWidth(plainBg, leftPad)
		b.WriteString(DimBackdropStyle.Render(leftText))
	}

	// Modal (as-is, with its own styling)
	b.WriteString(modal)

	// Right portion (dimmed)
	if rightStart < totalWidth {
		// Get remaining characters from background
		rightText := ""
		currentWidth := 0
		for _, r := range plainBg {
			charWidth := 1
			if isWideChar(r) {
				charWidth = 2
			}
			if currentWidth >= rightStart {
				rightText += string(r)
			}
			currentWidth += charWidth
		}
		if rightText != "" {
			b.WriteString(DimBackdropStyle.Render(rightText))
		}
	}

	return b.String()
}

// truncateToWidth truncates a string to fit within a given visible width.
func truncateToWidth(s string, maxWidth int) string {
	var b strings.Builder
	currentWidth := 0
	for _, r := range s {
		charWidth := 1
		if isWideChar(r) {
			charWidth = 2
		}
		if currentWidth+charWidth > maxWidth {
			break
		}
		b.WriteRune(r)
		currentWidth += charWidth
	}
	// Pad with spaces if needed
	for currentWidth < maxWidth {
		b.WriteRune(' ')
		currentWidth++
	}
	return b.String()
}

// renderModal renders the current confirmation modal
func (m *Model) renderModal() string {
	switch m.confirm {
	case ConfirmQuit:
		return QuitModal(m.lockedCount())
	case ConfirmRestart:
		return RestartModal(m.lockedCount())
	case ConfirmUnlock:
		if db := m.selectedDatabase(); db != nil && db.LockInfo != nil {
			duration := formatDuration(time.Since(db.LockInfo.LockedAt))
			return UnlockModal(db.DBName, db.LockInfo.Marker, duration)
		}
		return UnlockModal("unknown", "unknown", "0s")
	}
	return ""
}

// renderLoadingView renders the loading screen (startup or shutdown).
// The animation is always centered based on terminal dimensions.
func (m *Model) renderLoadingView() string {
	screen := m.loadingScreen

	// Use terminal dimensions, with sensible defaults
	width := m.width
	if width <= 0 {
		width = 80
	}
	height := m.height
	if height <= 0 {
		height = 24
	}

	// Build content lines first to calculate vertical centering
	var lines []string

	// Sheep display
	sheep := screen.SheepDisplay()
	lines = append(lines, centerText(sheep, width))
	lines = append(lines, "") // blank line

	// Title display
	title := screen.TitleDisplay()
	styledTitle := TitleStyle.Render(title)
	lines = append(lines, centerText(styledTitle, width))
	lines = append(lines, "") // blank line

	// Subtitle
	subtitle := screen.SubtitleDisplay()
	styledSubtitle := DimStyle.Render(subtitle)
	lines = append(lines, centerText(styledSubtitle, width))
	lines = append(lines, "") // blank line

	// Progress bar (smaller, reusable component)
	progress := screen.Progress()
	progressBar := m.loadingProgressBar.Render(progress)
	lines = append(lines, centerText(progressBar, width))
	lines = append(lines, "") // blank line

	// Status message (no spinner - sheep animation is enough)
	statusMsg := screen.StatusMessage()
	if statusMsg != "" && !screen.IsDone() && !screen.IsFailed() {
		styledStatus := DimStyle.Render(statusMsg)
		lines = append(lines, centerText(styledStatus, width))
	} else if screen.IsFailed() {
		errorMsg := ErrorStyle.Render("Error: " + screen.ErrorMessage())
		lines = append(lines, centerText(errorMsg, width))
	} else {
		lines = append(lines, "")
	}
	lines = append(lines, "") // blank line

	// Instance status (only for startup mode)
	if screen.ShowInstances() {
		for _, inst := range screen.GetInstances() {
			var status string
			if inst.Ready {
				status = InstancesStyle.Render(fmt.Sprintf(":%d  %s ready", inst.Port, IconCheckmark))
			} else {
				status = DimStyle.Render(fmt.Sprintf(":%d  waiting...", inst.Port))
			}
			lines = append(lines, centerText(status, width))
		}
	}

	// Help bar
	lines = append(lines, "")
	lines = append(lines, "")
	var helpText string
	if screen.IsFailed() {
		helpText = renderHelpKey("q", "Quit") + "  " + SheepEmoji
	} else if screen.Mode() == LoadingModeShutdown {
		// No cancel option during shutdown
		helpText = SheepEmoji
	} else {
		helpText = renderHelpKey("q", "Cancel") + "  " + SheepEmoji
	}
	lines = append(lines, centerText(helpText, width))

	// Calculate vertical padding to center content
	contentHeight := len(lines)
	topPadding := (height - contentHeight) / 2
	if topPadding < 0 {
		topPadding = 0
	}

	// Build final output with vertical centering
	var b strings.Builder
	for i := 0; i < topPadding; i++ {
		b.WriteString("\n")
	}
	for i, line := range lines {
		b.WriteString(line)
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

// centerText centers text within the given width.
func centerText(text string, width int) string {
	textWidth := lipglossWidth(text)
	if textWidth >= width {
		return text
	}
	padding := (width - textWidth) / 2
	return strings.Repeat(" ", padding) + text
}

// parseConnString extracts database name and port from a connection string
func parseConnString(connStr string) (dbName, port string) {
	u, err := url.Parse(connStr)
	if err != nil {
		return "unknown", "5432"
	}

	port = u.Port()
	if port == "" {
		port = "5432"
	}

	dbName = strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		dbName = "unknown"
	}

	return dbName, port
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm %ds", mins, secs)
	} else {
		hours := int(d.Hours())
		mins := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
}

// padRight pads a string to the right with spaces to reach the target width
func padRight(s string, width int) string {
	// Account for ANSI codes when measuring width
	visibleWidth := lipglossWidth(s)
	if visibleWidth >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visibleWidth)
}

// lipglossWidth measures the visible width of a string (excluding ANSI codes)
func lipglossWidth(s string) int {
	width := 0
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		// Emojis and other wide characters take 2 cells
		if isWideChar(r) {
			width += 2
		} else {
			width++
		}
	}
	return width
}

// isWideChar returns true if the rune is a wide character (takes 2 cells)
func isWideChar(r rune) bool {
	// Common emoji ranges
	if r >= 0x1F300 && r <= 0x1F9FF { // Miscellaneous Symbols and Pictographs, Emoticons, etc.
		return true
	}
	if r >= 0x2600 && r <= 0x26FF { // Miscellaneous Symbols
		return true
	}
	if r >= 0x2700 && r <= 0x27BF { // Dingbats
		return true
	}
	if r >= 0x2300 && r <= 0x23FF { // Miscellaneous Technical (includes ⏳)
		return true
	}
	if r >= 0x2B50 && r <= 0x2B55 { // Stars, circles
		return true
	}
	// CJK characters
	if r >= 0x4E00 && r <= 0x9FFF {
		return true
	}
	if r >= 0x3000 && r <= 0x303F {
		return true
	}
	// Fullwidth forms
	if r >= 0xFF00 && r <= 0xFFEF {
		return true
	}
	return false
}
