package containers

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

const (
	colNameFrac   = 0.28
	colStatusFrac = 0.14
	colShellFrac  = 0.08
	colCPUFrac    = 0.10
	colMemFrac    = 0.20
	colTmpFrac    = 0.20
)

// renderHeader builds the column header line.
func renderHeader(width int) string {
	nameW, statusW, shellW, cpuW, memW, tmpW := columnWidths(width)

	name := theme.ColumnHeaderStyle.Width(nameW).Render(" NAME")
	status := theme.ColumnHeaderStyle.Width(statusW).Render("STATUS")
	shells := theme.ColumnHeaderStyle.Width(shellW).Render("SHELLS")
	cpu := theme.ColumnHeaderStyle.Width(cpuW).Render("CPU")
	mem := theme.ColumnHeaderStyle.Width(memW).Render("MEM")
	tmp := theme.ColumnHeaderStyle.Width(tmpW).Render("TMP")

	return name + status + shells + cpu + mem + tmp
}

// renderRow formats a single container row.
func renderRow(item components.ListItem, selected bool, width int) string {
	ci, ok := item.Data.(containerItem)
	if !ok {
		return ""
	}

	nameW, statusW, shellW, cpuW, memW, tmpW := columnWidths(width)

	arrow := "  "
	if selected {
		arrow = theme.SelectionArrowStyle.Render(theme.IconArrowRight) + " "
	}

	maxName := nameW - 3
	if maxName < 1 {
		maxName = 1
	}
	nameText := ci.Name
	if len(nameText) > maxName {
		nameText = nameText[:maxName]
	}
	nameStyled := theme.ContainerNameStyle.Render(nameText)
	if selected {
		nameStyled = theme.ContainerNameStyle.Bold(true).Render(nameText)
	}
	nameCol := lipgloss.NewStyle().Width(nameW).Render(arrow + nameStyled)

	statusText, statusStyle := renderStatus(ci.Status)
	statusCol := lipgloss.NewStyle().Width(statusW).Render(statusStyle.Render(statusText))

	shellText := "--"
	if ci.Name != app.ContainerProxy {
		shellText = fmt.Sprintf("%d", ci.ShellCount)
	}
	shellCol := lipgloss.NewStyle().Width(shellW).Render(theme.RowNormalStyle.Render(shellText))

	cpuText := ci.CPUPercent
	if cpuText == "" {
		cpuText = "--"
	}
	cpuStyle := theme.RowNormalStyle
	if cpuPercent := parseCPU(cpuText); cpuPercent > 80.0 {
		cpuStyle = theme.CopperStyle
	}
	cpuCol := lipgloss.NewStyle().Width(cpuW).Render(cpuStyle.Render(cpuText))

	memText := ci.MemUsage
	if memText == "" {
		memText = "--"
	}
	memCol := lipgloss.NewStyle().Width(memW).Render(theme.RowNormalStyle.Render(memText))

	tmpText := ci.TmpUsage
	if tmpText == "" {
		tmpText = "--"
	}
	tmpCol := lipgloss.NewStyle().Width(tmpW).Render(theme.RowNormalStyle.Render(tmpText))

	row := nameCol + statusCol + shellCol + cpuCol + memCol + tmpCol
	if selected {
		row = theme.RowSelectedStyle.Width(width).Render(row)
	}
	return row
}

func renderStatus(status string) (string, lipgloss.Style) {
	normalized := strings.ToLower(strings.TrimSpace(status))
	switch {
	case strings.Contains(normalized, "restart"):
		return theme.IconDot + " Restarting", theme.CopperStyle
	case strings.Contains(normalized, "stop"):
		return theme.IconDot + " Stopping", theme.StatusStoppedStyle
	case normalized == "":
		fallthrough
	case strings.Contains(normalized, "run"):
		return theme.IconDot + " Running", theme.StatusRunningStyle
	default:
		return theme.IconDot + " " + status, theme.RowNormalStyle
	}
}

func renderActionStatus(state actionState, text string, width int) string {
	style := lipgloss.NewStyle().Foreground(theme.ColorDusty).Italic(true)
	switch state {
	case actionPending:
		style = lipgloss.NewStyle().Foreground(theme.ColorAmber).Italic(true)
	case actionSuccess:
		style = lipgloss.NewStyle().Foreground(theme.ColorProof).Italic(true)
		text = theme.IconCheck + " " + text
	case actionFailed:
		style = lipgloss.NewStyle().Foreground(theme.ColorFlame).Italic(true)
		text = theme.IconCross + " " + text
	}
	return lipgloss.NewStyle().Width(width).Render(" " + style.Render(text))
}

// renderDetail renders the expanded detail pane for a container.
func renderDetail(ci containerItem, width int) string {
	treeMid := theme.DividerStyle.Render("├─ ")
	treeEnd := theme.DividerStyle.Render("└─ ")
	shellText := "--"
	if ci.Name != app.ContainerProxy {
		shellText = fmt.Sprintf("%d", ci.ShellCount)
	}
	lines := []string{
		treeMid + theme.DetailLabelStyle.Render("Name:    ") + theme.DetailValueStyle.Render(ci.Name),
		treeMid + theme.DetailLabelStyle.Render("Status:  ") + theme.DetailValueStyle.Render(ci.Status),
		treeMid + theme.DetailLabelStyle.Render("Shells:  ") + theme.DetailValueStyle.Render(shellText),
		treeMid + theme.DetailLabelStyle.Render("CPU:     ") + theme.DetailValueStyle.Render(ci.CPUPercent),
		treeMid + theme.DetailLabelStyle.Render("Memory:  ") + theme.DetailValueStyle.Render(ci.MemUsage),
		treeEnd + theme.DetailLabelStyle.Render("/tmp:    ") + theme.DetailValueStyle.Render(ci.TmpUsage),
	}
	content := strings.Join(lines, "\n")
	return lipgloss.NewStyle().PaddingLeft(3).Width(width).Render(content)
}

// columnWidths computes absolute column widths from the total width.
func columnWidths(width int) (nameW, statusW, shellW, cpuW, memW, tmpW int) {
	nameW = int(float64(width) * colNameFrac)
	statusW = int(float64(width) * colStatusFrac)
	shellW = int(float64(width) * colShellFrac)
	cpuW = int(float64(width) * colCPUFrac)
	memW = int(float64(width) * colMemFrac)
	tmpW = width - nameW - statusW - shellW - cpuW - memW
	if tmpW < 1 {
		tmpW = 1
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
