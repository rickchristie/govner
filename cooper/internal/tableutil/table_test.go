package tableutil

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestBasicTwoColumnAlignment(t *testing.T) {
	tbl := NewTable("NAME", "VALUE")
	tbl.AddRow("short", "one")
	tbl.AddRow("a longer name", "two")

	result := tbl.Render(0)
	lines := strings.Split(result, "\n")

	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines (header + separator + 2 rows), got %d", len(lines))
	}

	// Header and rows should all have the same visible width pattern.
	headerWidth := lipgloss.Width(lines[0])
	for i := 2; i < len(lines); i++ {
		rowWidth := lipgloss.Width(lines[i])
		if rowWidth != headerWidth {
			t.Errorf("row %d width %d != header width %d\nheader: %q\nrow:    %q",
				i, rowWidth, headerWidth, lines[0], lines[i])
		}
	}
}

func TestANSIStyledCells(t *testing.T) {
	tbl := NewTable("TOOL", "STATUS")

	// Create styled cells.
	styledName := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")).Render("Go")
	styledStatus := lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff00")).Render("on")
	tbl.AddRow(styledName, styledStatus)

	plainName := "Python"
	plainStatus := "off"
	tbl.AddRow(plainName, plainStatus)

	result := tbl.Render(0)
	lines := strings.Split(result, "\n")

	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines, got %d", len(lines))
	}

	// All lines (except separator) should have the same visible width.
	headerWidth := lipgloss.Width(lines[0])
	for i := 2; i < len(lines); i++ {
		rowWidth := lipgloss.Width(lines[i])
		if rowWidth != headerWidth {
			t.Errorf("row %d visible width %d != header width %d", i, rowWidth, headerWidth)
		}
	}

	// The styled "Go" cell should be padded to match "Python" (6 chars).
	// Visible width of "Go" is 2, "Python" is 6. Column should be 6 wide.
	// Check that "Go" row has proper padding by verifying visible widths match.
	row1Width := lipgloss.Width(lines[2])
	row2Width := lipgloss.Width(lines[3])
	if row1Width != row2Width {
		t.Errorf("styled row width %d != plain row width %d", row1Width, row2Width)
	}
}

func TestEmptyTable(t *testing.T) {
	tbl := NewTable("COL1", "COL2", "COL3")
	result := tbl.Render(0)

	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (header + separator) for empty table, got %d", len(lines))
	}

	// Header should contain all column names.
	if !strings.Contains(lines[0], "COL1") || !strings.Contains(lines[0], "COL2") || !strings.Contains(lines[0], "COL3") {
		t.Errorf("header missing column names: %q", lines[0])
	}
}

func TestSingleRow(t *testing.T) {
	tbl := NewTable("KEY", "VALUE")
	tbl.AddRow("name", "cooper")

	result := tbl.Render(0)
	lines := strings.Split(result, "\n")

	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (header + separator + 1 row), got %d", len(lines))
	}

	// Row should align with header.
	headerWidth := lipgloss.Width(lines[0])
	rowWidth := lipgloss.Width(lines[2])
	if headerWidth != rowWidth {
		t.Errorf("header width %d != row width %d", headerWidth, rowWidth)
	}
}

func TestColumnWidthsAdaptToLongestContent(t *testing.T) {
	tbl := NewTable("A", "B")
	tbl.AddRow("short", "x")
	tbl.AddRow("a very long cell value", "y")
	tbl.AddRow("mid", "z")

	result := tbl.Render(0)
	lines := strings.Split(result, "\n")

	// The "A" column should be wide enough for "a very long cell value" (22 chars).
	// The "B" column should be 1 char wide (max of "B" and "x"/"y"/"z").
	// Total visible width: 22 + 2 (gap) + 1 = 25.
	headerWidth := lipgloss.Width(lines[0])
	expectedWidth := 22 + 2 + 1
	if headerWidth != expectedWidth {
		t.Errorf("expected total width %d, got %d", expectedWidth, headerWidth)
	}

	for i := 2; i < len(lines); i++ {
		rowWidth := lipgloss.Width(lines[i])
		if rowWidth != expectedWidth {
			t.Errorf("row %d width %d != expected %d", i, rowWidth, expectedWidth)
		}
	}
}

func TestMinWidth(t *testing.T) {
	tbl := NewTable("A", "B")
	tbl.SetMinWidth(0, 20) // Force column A to be at least 20 wide.
	tbl.AddRow("hi", "there")

	result := tbl.Render(0)
	lines := strings.Split(result, "\n")

	// Column A should be 20 wide (min), column B should be 5 ("there").
	// Total: 20 + 2 (gap) + 5 = 27.
	expectedWidth := 20 + 2 + 5
	headerWidth := lipgloss.Width(lines[0])
	if headerWidth != expectedWidth {
		t.Errorf("expected width %d with min width, got %d", expectedWidth, headerWidth)
	}
}

func TestSetColGap(t *testing.T) {
	tbl := NewTable("A", "B")
	tbl.SetColGap(4)
	tbl.AddRow("x", "y")

	result := tbl.Render(0)
	lines := strings.Split(result, "\n")

	// A=1 wide, B=1 wide, gap=4. Total: 1+4+1=6.
	expectedWidth := 1 + 4 + 1
	headerWidth := lipgloss.Width(lines[0])
	if headerWidth != expectedWidth {
		t.Errorf("expected width %d with gap=4, got %d", expectedWidth, headerWidth)
	}
}

func TestHeaderStyle(t *testing.T) {
	tbl := NewTable("NAME", "AGE")
	tbl.SetHeaderStyle(lipgloss.Color("#ff0000"), true)
	tbl.AddRow("alice", "30")

	result := tbl.Render(0)

	// Header should contain ANSI codes (styled). Just verify it renders without panic.
	if result == "" {
		t.Error("expected non-empty result with header style")
	}

	lines := strings.Split(result, "\n")
	// The styled header's visible width should still match rows.
	headerWidth := lipgloss.Width(lines[0])
	rowWidth := lipgloss.Width(lines[2])
	if headerWidth != rowWidth {
		t.Errorf("styled header width %d != row width %d", headerWidth, rowWidth)
	}
}

func TestRenderRowsSeparately(t *testing.T) {
	tbl := NewTable("COL1", "COL2")
	tbl.AddRow("aaa", "bbb")
	tbl.AddRow("cc", "dd")

	colWidths, rows := tbl.RenderRows(0)
	if len(colWidths) != 2 {
		t.Fatalf("expected 2 column widths, got %d", len(colWidths))
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	// All rows should have same visible width.
	w0 := lipgloss.Width(rows[0])
	w1 := lipgloss.Width(rows[1])
	if w0 != w1 {
		t.Errorf("row widths differ: %d vs %d", w0, w1)
	}
}

func TestSetColGap_AffectsColumnSpacing(t *testing.T) {
	// Verify that changing the gap changes the total rendered width.
	tblNarrow := NewTable("X", "Y")
	tblNarrow.SetColGap(1)
	tblNarrow.AddRow("a", "b")
	narrowResult := tblNarrow.Render(0)
	narrowWidth := lipgloss.Width(strings.Split(narrowResult, "\n")[0])

	tblWide := NewTable("X", "Y")
	tblWide.SetColGap(6)
	tblWide.AddRow("a", "b")
	wideResult := tblWide.Render(0)
	wideWidth := lipgloss.Width(strings.Split(wideResult, "\n")[0])

	// X=1, Y=1. Narrow: 1+1+1=3, Wide: 1+6+1=8. Diff should be 5.
	diff := wideWidth - narrowWidth
	if diff != 5 {
		t.Errorf("expected width difference of 5 from gap change, got %d (narrow=%d, wide=%d)", diff, narrowWidth, wideWidth)
	}
}

func TestSetColGap_ZeroGap(t *testing.T) {
	tbl := NewTable("A", "B")
	tbl.SetColGap(0)
	tbl.AddRow("x", "y")

	result := tbl.Render(0)
	lines := strings.Split(result, "\n")

	// A=1, B=1, gap=0. Total: 2.
	headerWidth := lipgloss.Width(lines[0])
	if headerWidth != 2 {
		t.Errorf("expected width 2 with gap=0, got %d", headerWidth)
	}
}

func TestSetColGap_NegativeIgnored(t *testing.T) {
	tbl := NewTable("A", "B")
	tbl.SetColGap(-5)
	tbl.AddRow("x", "y")

	// Negative gap should be ignored; default gap of 2 should remain.
	result := tbl.Render(0)
	lines := strings.Split(result, "\n")
	headerWidth := lipgloss.Width(lines[0])
	// A=1, B=1, gap=2 (default). Total: 4.
	if headerWidth != 4 {
		t.Errorf("expected width 4 (negative gap ignored), got %d", headerWidth)
	}
}

func TestSetMinWidth_EnforcedWhenContentNarrower(t *testing.T) {
	tbl := NewTable("A", "B", "C")
	tbl.SetMinWidth(1, 15) // Column B min width 15.
	tbl.AddRow("x", "y", "z")

	colWidths, _ := tbl.RenderRows(0)
	if colWidths[1] < 15 {
		t.Errorf("expected column B width >= 15, got %d", colWidths[1])
	}
}

func TestSetMinWidth_DoesNotShrinkWiderContent(t *testing.T) {
	tbl := NewTable("A", "B")
	tbl.SetMinWidth(0, 3)               // Min width 3 for column A.
	tbl.AddRow("wide content here", "x") // Column A content is 17 chars.

	colWidths, _ := tbl.RenderRows(0)
	if colWidths[0] < 17 {
		t.Errorf("expected column A width >= 17 (content), got %d", colWidths[0])
	}
}

func TestSetMinWidth_OutOfBoundsIgnored(t *testing.T) {
	tbl := NewTable("A", "B")
	tbl.SetMinWidth(5, 20)  // Column 5 does not exist.
	tbl.SetMinWidth(-1, 20) // Negative column index.
	tbl.AddRow("x", "y")

	// Should not panic, and widths should be default.
	colWidths, _ := tbl.RenderRows(0)
	if len(colWidths) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(colWidths))
	}
}

func TestRenderRows_ColumnWidths(t *testing.T) {
	tbl := NewTable("NAME", "STATUS", "VERSION")
	tbl.AddRow("Go", "on", "1.22.5")
	tbl.AddRow("Python", "off", "3.12")

	colWidths, rows := tbl.RenderRows(0)

	// Column 0 should be at least wide enough for "Python" (6) and header "NAME" (4).
	if colWidths[0] < 6 {
		t.Errorf("expected colWidths[0] >= 6 for 'Python', got %d", colWidths[0])
	}

	// Column 1 should be at least wide enough for "STATUS" (6).
	if colWidths[1] < 6 {
		t.Errorf("expected colWidths[1] >= 6 for 'STATUS', got %d", colWidths[1])
	}

	// Column 2 should be at least wide enough for "VERSION" (7).
	if colWidths[2] < 7 {
		t.Errorf("expected colWidths[2] >= 7 for 'VERSION', got %d", colWidths[2])
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	// Verify all rows have the same total visible width.
	w0 := lipgloss.Width(rows[0])
	w1 := lipgloss.Width(rows[1])
	if w0 != w1 {
		t.Errorf("rows should have equal width: row0=%d, row1=%d", w0, w1)
	}
}

func TestRenderRows_EmptyHeaders(t *testing.T) {
	tbl := NewTable()
	colWidths, rows := tbl.RenderRows(0)
	if colWidths != nil {
		t.Errorf("expected nil colWidths for empty headers, got %v", colWidths)
	}
	if rows != nil {
		t.Errorf("expected nil rows for empty headers, got %v", rows)
	}
}

func TestRenderRows_FewerCellsThanHeaders(t *testing.T) {
	tbl := NewTable("A", "B", "C")
	tbl.AddRow("x") // Only provides 1 of 3 columns.

	_, rows := tbl.RenderRows(0)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	// Should not panic and should produce a valid row.
	w := lipgloss.Width(rows[0])
	if w == 0 {
		t.Error("expected non-zero width for partial row")
	}
}

func TestRenderHeader_MatchesRowWidth(t *testing.T) {
	tbl := NewTable("TOOL", "STATUS")
	tbl.AddRow("GoLang", "enabled")

	header := tbl.RenderHeader()
	_, rows := tbl.RenderRows(0)

	headerWidth := lipgloss.Width(header)
	rowWidth := lipgloss.Width(rows[0])
	if headerWidth != rowWidth {
		t.Errorf("header width %d != row width %d", headerWidth, rowWidth)
	}
}

func TestSetSeparator_CustomChar(t *testing.T) {
	tbl := NewTable("A", "B")
	tbl.SetSeparator("=", nil)
	tbl.AddRow("x", "y")

	result := tbl.Render(0)
	lines := strings.Split(result, "\n")
	if len(lines) < 2 {
		t.Fatal("expected at least 2 lines")
	}

	// Separator line should use "=" instead of default "─".
	sepLine := lines[1]
	if !strings.Contains(sepLine, "=") {
		t.Errorf("expected separator to contain '=', got %q", sepLine)
	}
	if strings.Contains(sepLine, "\u2500") {
		t.Errorf("separator should not contain default '─'")
	}
}

func TestThreeColumnTable(t *testing.T) {
	tbl := NewTable("A", "BB", "CCC")
	tbl.AddRow("1", "22", "333")
	tbl.AddRow("4444", "55555", "6")

	result := tbl.Render(0)
	lines := strings.Split(result, "\n")

	if len(lines) != 4 {
		t.Fatalf("expected 4 lines (header + sep + 2 rows), got %d", len(lines))
	}

	// All lines (except separator) should have the same visible width.
	headerWidth := lipgloss.Width(lines[0])
	for i := 2; i < len(lines); i++ {
		rw := lipgloss.Width(lines[i])
		if rw != headerWidth {
			t.Errorf("row %d width %d != header width %d", i, rw, headerWidth)
		}
	}
}

func TestPadToWidth(t *testing.T) {
	// Already wider than target -- returned as-is.
	result := padToWidth("longstring", 3)
	if result != "longstring" {
		t.Errorf("expected no padding when wider, got %q", result)
	}

	// Shorter than target -- should be padded.
	result = padToWidth("ab", 5)
	if lipgloss.Width(result) != 5 {
		t.Errorf("expected visible width 5, got %d", lipgloss.Width(result))
	}

	// Exact width -- no padding needed.
	result = padToWidth("abc", 3)
	if result != "abc" {
		t.Errorf("expected exact match, got %q", result)
	}
}
