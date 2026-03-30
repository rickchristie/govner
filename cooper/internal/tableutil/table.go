// Package tableutil provides a reusable table renderer for TUI screens.
// It measures actual visible width (accounting for ANSI escape codes via
// lipgloss.Width) to produce properly aligned columns.
package tableutil

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// HeaderStyle is applied to column headers when rendering.
// Callers can set this before calling Render, or leave nil for unstyled headers.
type HeaderStyle struct {
	Foreground lipgloss.Color
	Bold       bool
}

// TableRenderer renders aligned text tables with fixed-width columns.
// It measures actual content width (accounting for ANSI escape codes via
// lipgloss.Width) to produce properly aligned output.
type TableRenderer struct {
	headers   []string
	rows      [][]string // each row is a slice of cell strings (may contain ANSI styling)
	minWidths []int      // minimum width per column
	colGap    int        // gap between columns in spaces (default 2)

	// Header styling.
	headerStyle *HeaderStyle

	// Separator line character (default "─").
	separatorChar string

	// Separator line color (optional).
	separatorColor *lipgloss.Color
}

// NewTable creates a new table renderer with the given column headers.
func NewTable(headers ...string) *TableRenderer {
	t := &TableRenderer{
		headers:       headers,
		minWidths:     make([]int, len(headers)),
		colGap:        2,
		separatorChar: "\u2500", // ─
	}
	return t
}

// AddRow adds a data row. Cells may contain lipgloss-styled strings with ANSI
// escape codes; the renderer uses lipgloss.Width to measure visible width.
func (t *TableRenderer) AddRow(cells ...string) {
	// Pad or truncate to match header count.
	row := make([]string, len(t.headers))
	for i := range row {
		if i < len(cells) {
			row[i] = cells[i]
		}
	}
	t.rows = append(t.rows, row)
}

// SetMinWidth sets the minimum width for a column (0-indexed).
func (t *TableRenderer) SetMinWidth(col, width int) {
	if col >= 0 && col < len(t.minWidths) {
		t.minWidths[col] = width
	}
}

// SetColGap sets the gap (in spaces) between columns. Default is 2.
func (t *TableRenderer) SetColGap(gap int) {
	if gap >= 0 {
		t.colGap = gap
	}
}

// SetHeaderStyle sets the style applied to header cells.
func (t *TableRenderer) SetHeaderStyle(fg lipgloss.Color, bold bool) {
	t.headerStyle = &HeaderStyle{Foreground: fg, Bold: bold}
}

// SetSeparator sets the character and optional color for the separator line.
func (t *TableRenderer) SetSeparator(char string, color *lipgloss.Color) {
	t.separatorChar = char
	t.separatorColor = color
}

// Render renders the table as a string. The totalWidth parameter is used to
// determine separator line length; columns are auto-sized based on content.
func (t *TableRenderer) Render(totalWidth int) string {
	if len(t.headers) == 0 {
		return ""
	}

	numCols := len(t.headers)
	colWidths := make([]int, numCols)

	// Measure header widths.
	for i, h := range t.headers {
		w := lipgloss.Width(h)
		if w > colWidths[i] {
			colWidths[i] = w
		}
	}

	// Measure row cell widths.
	for _, row := range t.rows {
		for i, cell := range row {
			if i >= numCols {
				break
			}
			w := lipgloss.Width(cell)
			if w > colWidths[i] {
				colWidths[i] = w
			}
		}
	}

	// Apply minimum widths.
	for i := range colWidths {
		if i < len(t.minWidths) && t.minWidths[i] > colWidths[i] {
			colWidths[i] = t.minWidths[i]
		}
	}

	gap := strings.Repeat(" ", t.colGap)

	// Build header line.
	var headerParts []string
	for i, h := range t.headers {
		styled := h
		if t.headerStyle != nil {
			s := lipgloss.NewStyle().Foreground(t.headerStyle.Foreground)
			if t.headerStyle.Bold {
				s = s.Bold(true)
			}
			styled = s.Render(h)
		}
		padded := padToWidth(styled, colWidths[i])
		headerParts = append(headerParts, padded)
	}
	headerLine := strings.Join(headerParts, gap)

	// Build separator line.
	sepWidth := totalWidth
	if sepWidth <= 0 {
		// Calculate from column widths.
		sepWidth = 0
		for _, w := range colWidths {
			sepWidth += w
		}
		sepWidth += t.colGap * (numCols - 1)
	}
	sepLine := strings.Repeat(t.separatorChar, sepWidth)
	if t.separatorColor != nil {
		sepLine = lipgloss.NewStyle().Foreground(*t.separatorColor).Render(sepLine)
	}

	// Build data rows.
	var dataLines []string
	for _, row := range t.rows {
		var parts []string
		for i := 0; i < numCols; i++ {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			padded := padToWidth(cell, colWidths[i])
			parts = append(parts, padded)
		}
		dataLines = append(dataLines, strings.Join(parts, gap))
	}

	// Assemble: header, separator, data rows.
	var lines []string
	lines = append(lines, headerLine)
	lines = append(lines, sepLine)
	lines = append(lines, dataLines...)

	return strings.Join(lines, "\n")
}

// RenderRows renders only the data rows (no header or separator), with
// the same column alignment. Useful when rows need individual post-processing
// (e.g., highlight selected row).
func (t *TableRenderer) RenderRows(totalWidth int) (colWidths []int, rows []string) {
	if len(t.headers) == 0 {
		return nil, nil
	}

	numCols := len(t.headers)
	colWidths = t.computeColWidths()

	gap := strings.Repeat(" ", t.colGap)

	for _, row := range t.rows {
		var parts []string
		for i := 0; i < numCols; i++ {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			padded := padToWidth(cell, colWidths[i])
			parts = append(parts, padded)
		}
		rows = append(rows, strings.Join(parts, gap))
	}
	return colWidths, rows
}

// RenderHeader renders just the header line using the computed column widths.
func (t *TableRenderer) RenderHeader() string {
	if len(t.headers) == 0 {
		return ""
	}

	colWidths := t.computeColWidths()
	gap := strings.Repeat(" ", t.colGap)

	var parts []string
	for i, h := range t.headers {
		styled := h
		if t.headerStyle != nil {
			s := lipgloss.NewStyle().Foreground(t.headerStyle.Foreground)
			if t.headerStyle.Bold {
				s = s.Bold(true)
			}
			styled = s.Render(h)
		}
		padded := padToWidth(styled, colWidths[i])
		parts = append(parts, padded)
	}
	return strings.Join(parts, gap)
}

// RenderSeparator renders just the separator line.
func (t *TableRenderer) RenderSeparator(totalWidth int) string {
	sepWidth := totalWidth
	if sepWidth <= 0 {
		colWidths := t.computeColWidths()
		sepWidth = 0
		for _, w := range colWidths {
			sepWidth += w
		}
		sepWidth += t.colGap * (len(t.headers) - 1)
	}
	sepLine := strings.Repeat(t.separatorChar, sepWidth)
	if t.separatorColor != nil {
		sepLine = lipgloss.NewStyle().Foreground(*t.separatorColor).Render(sepLine)
	}
	return sepLine
}

// computeColWidths calculates the width needed for each column.
func (t *TableRenderer) computeColWidths() []int {
	numCols := len(t.headers)
	colWidths := make([]int, numCols)

	for i, h := range t.headers {
		w := lipgloss.Width(h)
		if w > colWidths[i] {
			colWidths[i] = w
		}
	}

	for _, row := range t.rows {
		for i, cell := range row {
			if i >= numCols {
				break
			}
			w := lipgloss.Width(cell)
			if w > colWidths[i] {
				colWidths[i] = w
			}
		}
	}

	for i := range colWidths {
		if i < len(t.minWidths) && t.minWidths[i] > colWidths[i] {
			colWidths[i] = t.minWidths[i]
		}
	}

	return colWidths
}

// padToWidth pads a string with trailing spaces so its visible width
// (as measured by lipgloss.Width, which accounts for ANSI codes) reaches
// the target width. If the string is already wider, it is returned as-is.
func padToWidth(s string, targetWidth int) string {
	currentWidth := lipgloss.Width(s)
	if currentWidth >= targetWidth {
		return s
	}
	return s + strings.Repeat(" ", targetWidth-currentWidth)
}
