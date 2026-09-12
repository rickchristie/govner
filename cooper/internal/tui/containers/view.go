package containers

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

const (
	colKindFrac   = 0.08
	colNameFrac   = 0.24
	colStatusFrac = 0.15
	colShellFrac  = 0.08
	colCPUFrac    = 0.09
	colMemFrac    = 0.18
)

// renderHeader builds the column header line.
func renderHeader(width int) string {
	kindW, nameW, statusW, shellW, cpuW, memW, storageW := columnWidths(width)

	kind := renderFixedCell(theme.ColumnHeaderStyle.Render(" KIND"), kindW)
	name := renderFixedCell(theme.ColumnHeaderStyle.Render(" NAME"), nameW)
	status := renderFixedCell(theme.ColumnHeaderStyle.Render(" STATUS"), statusW)
	shells := renderFixedCell(theme.ColumnHeaderStyle.Render(" SHELLS"), shellW)
	cpu := renderFixedCell(theme.ColumnHeaderStyle.Render(" CPU"), cpuW)
	mem := renderFixedCell(theme.ColumnHeaderStyle.Render(" MEM"), memW)
	storage := renderFixedCell(theme.ColumnHeaderStyle.Render(" STORAGE"), storageW)

	return kind + name + status + shells + cpu + mem + storage
}

// renderRow formats a single runtime row.
func renderRow(item components.ListItem, selected bool, width int) string {
	workload, ok := item.Data.(workloadItem)
	if !ok {
		return ""
	}

	kindW, nameW, statusW, shellW, cpuW, memW, storageW := columnWidths(width)

	arrow := "  "
	if selected {
		arrow = theme.SelectionArrowStyle.Render(theme.IconArrowRight) + " "
	}

	kindCol := renderFixedCell(" "+theme.DimStyle.Render(workloadKindLabel(workload)), kindW)
	nameText := workload.ID
	nameStyled := theme.ContainerNameStyle.Render(nameText)
	if selected {
		nameStyled = theme.ContainerNameStyle.Bold(true).Render(nameText)
	}
	nameCol := renderFixedCell(arrow+nameStyled, nameW)

	statusText, statusStyle := renderStatus(workload.Status)
	statusCol := renderFixedCell(" "+statusStyle.Render(statusText), statusW)

	shellText := "--"
	if workload.Kind != app.WorkloadProxy {
		shellText = fmt.Sprintf("%d", workload.ShellCount)
	}
	shellCol := renderFixedCell(" "+theme.RowNormalStyle.Render(shellText), shellW)

	cpuText := workload.CPUPercent
	if cpuText == "" {
		cpuText = "--"
	}
	cpuStyle := theme.RowNormalStyle
	if cpuPercent := parseCPU(cpuText); cpuPercent > 80.0 {
		cpuStyle = theme.CopperStyle
	}
	cpuCol := renderFixedCell(" "+cpuStyle.Render(cpuText), cpuW)

	memText := workload.MemUsage
	if memText == "" {
		memText = "--"
	}
	memCol := renderFixedCell(" "+theme.RowNormalStyle.Render(memText), memW)

	storageText := workload.StorageUsage
	if storageText == "" {
		storageText = "--"
	}
	storageCol := renderFixedCell(" "+theme.RowNormalStyle.Render(storageText), storageW)

	row := kindCol + nameCol + statusCol + shellCol + cpuCol + memCol + storageCol
	if selected {
		row = theme.RowSelectedStyle.Width(width).Render(row)
	}
	return row
}

// renderFixedCell keeps one table value inside its assigned columns. Lipgloss
// Width adds padding but does not truncate long content. Explicit truncation
// prevents the terminal from wrapping one logical runtime row into two rows.
func renderFixedCell(value string, width int) string {
	if width < 1 {
		return ""
	}
	return lipgloss.NewStyle().Width(width).Render(ansi.Truncate(value, width, "…"))
}

func workloadKindLabel(workload workloadItem) string {
	switch workload.Kind {
	case app.WorkloadProxy:
		return "PROXY"
	case app.WorkloadCLI:
		return "CLI"
	case app.WorkloadVM:
		return fmt.Sprintf("VM%d", workload.Depth)
	default:
		return "?"
	}
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

// renderDetail renders identity and health values that do not fit the table.
func renderDetail(workload workloadItem, width int) string {
	treeMid := theme.DividerStyle.Render("├─ ")
	treeEnd := theme.DividerStyle.Render("└─ ")
	shellText := "--"
	if workload.Kind != app.WorkloadProxy {
		shellText = fmt.Sprintf("%d", workload.ShellCount)
	}
	lines := []string{
		treeMid + theme.DetailLabelStyle.Render("ID:      ") + theme.DetailValueStyle.Render(workload.ID),
		treeMid + theme.DetailLabelStyle.Render("Kind:    ") + theme.DetailValueStyle.Render(workloadKindLabel(workload)),
		treeMid + theme.DetailLabelStyle.Render("Tool:    ") + theme.DetailValueStyle.Render(valueOrDash(workload.Tool)),
		treeMid + theme.DetailLabelStyle.Render("Status:  ") + theme.DetailValueStyle.Render(workload.Status),
		treeMid + theme.DetailLabelStyle.Render("Shells:  ") + theme.DetailValueStyle.Render(shellText),
		treeMid + theme.DetailLabelStyle.Render("CPU:     ") + theme.DetailValueStyle.Render(valueOrDash(workload.CPUPercent)),
		treeMid + theme.DetailLabelStyle.Render("Memory:  ") + theme.DetailValueStyle.Render(valueOrDash(workload.MemUsage)),
		treeMid + theme.DetailLabelStyle.Render("Storage: ") + theme.DetailValueStyle.Render(valueOrDash(workload.StorageUsage)),
		treeMid + theme.DetailLabelStyle.Render("Health:  ") + theme.DetailValueStyle.Render(valueOrDash(workload.HealthReason)),
		treeEnd + theme.DetailLabelStyle.Render("Path:    ") + theme.DetailValueStyle.Render(valueOrDash(workload.Workspace)),
	}
	content := strings.Join(lines, "\n")
	return lipgloss.NewStyle().PaddingLeft(3).Width(width).Render(content)
}

func valueOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "--"
	}
	return value
}

// columnWidths computes absolute column widths from the total width.
func columnWidths(width int) (kindW, nameW, statusW, shellW, cpuW, memW, storageW int) {
	kindW = int(float64(width) * colKindFrac)
	nameW = int(float64(width) * colNameFrac)
	statusW = int(float64(width) * colStatusFrac)
	shellW = int(float64(width) * colShellFrac)
	cpuW = int(float64(width) * colCPUFrac)
	memW = int(float64(width) * colMemFrac)
	storageW = width - kindW - nameW - statusW - shellW - cpuW - memW
	// The leading separator and the SHELLS label need seven cells. Move one
	// cell from the last column at narrow supported widths so headers stay
	// visually separate.
	if shellW < 7 && storageW > 1 {
		delta := 7 - shellW
		if delta >= storageW {
			delta = storageW - 1
		}
		shellW += delta
		storageW -= delta
	}
	if storageW < 1 {
		storageW = 1
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
