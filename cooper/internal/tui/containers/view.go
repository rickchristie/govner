package containers

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// Column widths as fractions of total width.
const (
	colNameFrac   = 0.35
	colStatusFrac = 0.15
	colCPUFrac    = 0.15
	colMemFrac    = 0.35
)

// renderHeader builds the column header line.
func renderHeader(width int) string {
	nameW, statusW, cpuW, memW := columnWidths(width)

	name := theme.ColumnHeaderStyle.Width(nameW).Render(" NAME")
	status := theme.ColumnHeaderStyle.Width(statusW).Render("STATUS")
	cpu := theme.ColumnHeaderStyle.Width(cpuW).Render("CPU")
	mem := theme.ColumnHeaderStyle.Width(memW).Render("MEM")

	return name + status + cpu + mem
}

// renderRow formats a single container row. Implements the callback
// signature expected by components.ScrollableList.View.
func renderRow(item components.ListItem, selected bool, width int) string {
	ci, ok := item.Data.(containerItem)
	if !ok {
		return ""
	}

	nameW, statusW, cpuW, memW := columnWidths(width)

	// Selection arrow.
	var arrow string
	if selected {
		arrow = theme.SelectionArrowStyle.Render(theme.IconArrowRight) + " "
	} else {
		arrow = "  "
	}

	// Container name.
	nameText := ci.Name
	// Truncate if needed; leave room for the arrow prefix.
	maxName := nameW - 3
	if maxName < 1 {
		maxName = 1
	}
	if len(nameText) > maxName {
		nameText = nameText[:maxName]
	}

	var nameStyled string
	if selected {
		nameStyled = theme.ContainerNameStyle.Bold(true).Render(nameText)
	} else {
		nameStyled = theme.ContainerNameStyle.Render(nameText)
	}

	nameCol := lipgloss.NewStyle().Width(nameW).Render(arrow + nameStyled)

	// Status column.
	var statusStyled string
	if strings.Contains(strings.ToLower(ci.Status), "up") ||
		ci.Status == "running" {
		statusStyled = theme.StatusRunningStyle.Render(theme.IconDot + " Run")
	} else {
		statusStyled = theme.StatusStoppedStyle.Render(theme.IconDot + " Stop")
	}
	statusCol := lipgloss.NewStyle().Width(statusW).Render(statusStyled)

	// CPU column -- highlight high usage.
	cpuText := ci.CPUPercent
	if cpuText == "" {
		cpuText = "--"
	}
	cpuStyle := theme.RowNormalStyle
	if cpuPercent := parseCPU(cpuText); cpuPercent > 80.0 {
		cpuStyle = theme.CopperStyle
	}
	cpuCol := lipgloss.NewStyle().Width(cpuW).Render(cpuStyle.Render(cpuText))

	// Memory column.
	memText := ci.MemUsage
	if memText == "" {
		memText = "--"
	}
	memCol := lipgloss.NewStyle().Width(memW).Render(theme.RowNormalStyle.Render(memText))

	row := nameCol + statusCol + cpuCol + memCol

	// Apply selected row background.
	if selected {
		row = theme.RowSelectedStyle.Width(width).Render(row)
	}

	return row
}

// renderDetail renders the expanded detail pane for a container.
func renderDetail(ci containerItem, width int) string {
	treeLine := theme.DividerStyle.Render("\u251C\u2500 ") // ├─
	treeEnd := theme.DividerStyle.Render("\u2514\u2500 ")  // └─

	lines := []string{
		treeLine + theme.DetailLabelStyle.Render("Name:    ") + theme.DetailValueStyle.Render(ci.Name),
		treeLine + theme.DetailLabelStyle.Render("Status:  ") + theme.DetailValueStyle.Render(ci.Status),
		treeLine + theme.DetailLabelStyle.Render("CPU:     ") + theme.DetailValueStyle.Render(ci.CPUPercent),
		treeEnd + theme.DetailLabelStyle.Render("Memory:  ") + theme.DetailValueStyle.Render(ci.MemUsage),
	}

	content := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		PaddingLeft(3).
		Width(width).
		Render(content)
}

// columnWidths computes absolute column widths from the total width.
func columnWidths(width int) (nameW, statusW, cpuW, memW int) {
	nameW = int(float64(width) * colNameFrac)
	statusW = int(float64(width) * colStatusFrac)
	cpuW = int(float64(width) * colCPUFrac)
	memW = width - nameW - statusW - cpuW
	if memW < 1 {
		memW = 1
	}
	return
}

// parseCPU extracts a float from a CPU percentage string like "8.4%".
func parseCPU(s string) float64 {
	s = strings.TrimSuffix(s, "%")
	s = strings.TrimSpace(s)
	var v float64
	fmt.Sscanf(s, "%f", &v)
	return v
}
