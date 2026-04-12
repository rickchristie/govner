// Package about implements the About tab sub-model.
// It shows Cooper version info, tool versions, and infrastructure details.
package about

import (
	"fmt"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/tableutil"
	"github.com/rickchristie/govner/cooper/internal/tui/components"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
	"github.com/rickchristie/govner/cooper/meta"
)

// RunUpdateMsg is emitted when the user presses `u` to trigger an update.
// The root model should handle this by initiating the update process.
type RunUpdateMsg struct{}

// StartupWarningsMsg carries version mismatch warnings detected during
// startup. The root model forwards this to the about sub-model.
type StartupWarningsMsg struct {
	Warnings []string
}

// Model is the sub-model for the About tab.
type Model struct {
	// Tool configs (programming + AI).
	progTools     []config.ToolConfig
	aiTools       []config.ToolConfig
	implicitTools []config.ImplicitToolConfig

	// Infrastructure info.
	proxyPort  int
	bridgePort int

	// Whether version mismatches exist.
	hasMismatch bool

	// Startup warnings from the version mismatch check.
	startupWarnings []string

	// Scrollable viewport for content.
	viewport   components.ScrollableContent
	lastHeight int
}

// New creates a new about model populated from the given config.
func New(cfg *config.Config) *Model {
	m := &Model{
		proxyPort:  cfg.ProxyPort,
		bridgePort: cfg.BridgePort,
	}
	m.progTools = make([]config.ToolConfig, len(cfg.ProgrammingTools))
	copy(m.progTools, cfg.ProgrammingTools)
	m.aiTools = make([]config.ToolConfig, len(cfg.AITools))
	copy(m.aiTools, cfg.AITools)
	m.implicitTools = config.VisibleImplicitLSPs(cfg.ImplicitTools)
	m.checkMismatches()
	return m
}

// expectedVersion returns the correct expected version for comparison based on mode.
//   - Mirror: compare against HostVersion
//   - Pin: compare against PinnedVersion
//   - Latest: compare against PinnedVersion (resolved latest, set by resolveLatestVersions)
func expectedVersion(t config.ToolConfig) string {
	switch t.Mode {
	case config.ModeMirror:
		return t.HostVersion
	case config.ModePin, config.ModeLatest:
		return t.PinnedVersion
	default:
		return ""
	}
}

// checkMismatches sets hasMismatch if any enabled tool has a version mismatch.
func (m *Model) checkMismatches() {
	m.hasMismatch = false
	for _, t := range m.progTools {
		if !t.Enabled {
			continue
		}
		status := config.CompareVersions(t.ContainerVersion, expectedVersion(t), t.Mode)
		if status == config.VersionMismatch {
			m.hasMismatch = true
			return
		}
	}
	for _, t := range m.aiTools {
		if !t.Enabled {
			continue
		}
		status := config.CompareVersions(t.ContainerVersion, expectedVersion(t), t.Mode)
		if status == config.VersionMismatch {
			m.hasMismatch = true
			return
		}
	}
}

// Init satisfies the SubModel interface.
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update satisfies theme.SubModel.
func (m *Model) Update(msg tea.Msg) (theme.SubModel, tea.Cmd) {
	switch msg := msg.(type) {
	case StartupWarningsMsg:
		m.startupWarnings = msg.Warnings
		if len(msg.Warnings) > 0 {
			m.hasMismatch = true
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "u":
			if m.hasMismatch {
				return m, func() tea.Msg { return RunUpdateMsg{} }
			}
		}
		m.viewport.HandleKey(msg, m.lastHeight)
	case tea.MouseMsg:
		m.viewport.HandleMouse(msg, m.lastHeight)
	}
	return m, nil
}

// View satisfies the SubModel interface.
func (m *Model) View(width, height int) string {
	m.lastHeight = height

	// Render all content into a string, then display through the viewport.
	var b strings.Builder

	// Version header.
	ver := lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).
		Render(fmt.Sprintf("Cooper v%s", meta.Version))
	goVer := lipgloss.NewStyle().Foreground(theme.ColorDusty).
		Render(fmt.Sprintf("Go %s", runtime.Version()))
	b.WriteString(" " + ver + "    " + goVer + "\n")

	// Mismatch warning banner.
	if m.hasMismatch {
		b.WriteString("\n")
		banner := lipgloss.NewStyle().
			Background(theme.ColorCopper).
			Foreground(theme.ColorVoid).
			Width(width-2).
			Padding(0, 1).
			Render(theme.IconWarn + " Version mismatches detected. Run  cooper update  to rebuild the container image.")
		b.WriteString(" " + banner + "\n")
	}

	// Startup version warnings (detected during cooper up).
	if len(m.startupWarnings) > 0 {
		warnStyle := lipgloss.NewStyle().Foreground(theme.ColorCopper)
		for _, w := range m.startupWarnings {
			b.WriteString(" " + warnStyle.Render(theme.IconWarn+" "+w) + "\n")
		}
	}

	b.WriteString("\n")

	// Programming Tools section.
	b.WriteString(sectionHeader("Programming Tools", width))
	b.WriteString("\n")
	b.WriteString(renderToolTable(m.progTools, width))

	b.WriteString("\n")

	// AI CLI Tools section.
	b.WriteString(sectionHeader("AI CLI Tools", width))
	b.WriteString("\n")
	b.WriteString(renderToolTable(m.aiTools, width))

	b.WriteString("\n")

	if len(m.implicitTools) > 0 {
		b.WriteString(sectionHeader("Implicit Language Servers", width))
		b.WriteString("\n")
		b.WriteString(renderImplicitToolTable(m.implicitTools, width))
		b.WriteString("\n")
	}

	// Infrastructure section.
	b.WriteString(sectionHeader("Infrastructure", width))
	b.WriteString("\n")
	b.WriteString(renderInfraRow("Proxy Port", fmt.Sprintf("%d", m.proxyPort)))
	b.WriteString(renderInfraRow("Bridge Port", fmt.Sprintf("%d", m.bridgePort)))

	m.viewport.SetContent(b.String())
	return m.viewport.View(width, height)
}

// ----- Rendering helpers -----

func sectionHeader(label string, width int) string {
	divStyle := lipgloss.NewStyle().Foreground(theme.ColorDusty)
	lineLen := width - len(label) - 6
	if lineLen < 0 {
		lineLen = 0
	}
	return divStyle.Render(" " + theme.BorderH + theme.BorderH + " " + label + " " +
		strings.Repeat(theme.BorderH, lineLen))
}

func renderToolTable(tools []config.ToolConfig, width int) string {
	if len(tools) == 0 {
		return lipgloss.NewStyle().Foreground(theme.ColorFaded).Italic(true).
			Render("  No tools configured.") + "\n"
	}

	verStyle := lipgloss.NewStyle().Foreground(theme.ColorLinen)
	notInstalled := lipgloss.NewStyle().Foreground(theme.ColorFaded).Italic(true)

	tbl := tableutil.NewTable("TOOL", "CONTAINER VERSION", "HOST VERSION", "MODE", "STATUS")
	tbl.SetHeaderStyle(theme.ColorDusty, true)
	sepColor := theme.ColorOakLight
	tbl.SetSeparator(theme.BorderH, &sepColor)

	for _, t := range tools {
		name := lipgloss.NewStyle().Foreground(theme.ColorParchment).Render(t.Name)

		var containerVer string
		if t.ContainerVersion == "" {
			containerVer = notInstalled.Render("(not installed)")
		} else {
			containerVer = verStyle.Render(t.ContainerVersion)
		}

		var hostVer string
		if t.HostVersion == "" {
			hostVer = notInstalled.Render("(not installed)")
		} else {
			hostVer = verStyle.Render(t.HostVersion)
		}

		modeStr := renderMode(t.Mode)
		statusStr := renderVersionStatus(t)

		tbl.AddRow(name, containerVer, hostVer, modeStr, statusStr)
	}

	var b strings.Builder
	b.WriteString(" " + tbl.RenderHeader() + "\n")
	b.WriteString(theme.DividerStyle.Render(" "+strings.Repeat(theme.BorderH, width-2)) + "\n")
	_, rows := tbl.RenderRows(0)
	for _, row := range rows {
		b.WriteString(" " + row + "\n")
	}
	return b.String()
}

func renderMode(mode config.VersionMode) string {
	switch mode {
	case config.ModeMirror:
		return lipgloss.NewStyle().Foreground(theme.ColorSlateBlue).Render("mirror")
	case config.ModeLatest:
		return lipgloss.NewStyle().Foreground(theme.ColorVerdigris).Render("latest")
	case config.ModePin:
		return lipgloss.NewStyle().Foreground(theme.ColorAmber).Render("pin")
	case config.ModeOff:
		return lipgloss.NewStyle().Foreground(theme.ColorFaded).Render("off")
	default:
		return lipgloss.NewStyle().Foreground(theme.ColorFaded).Render("unknown")
	}
}

func renderVersionStatus(t config.ToolConfig) string {
	if !t.Enabled || t.Mode == config.ModeOff {
		return lipgloss.NewStyle().Foreground(theme.ColorFaded).Render(theme.BorderH)
	}

	status := config.CompareVersions(t.ContainerVersion, expectedVersion(t), t.Mode)
	switch status {
	case config.VersionMatch:
		label := theme.IconCheck + " match"
		if t.Mode == config.ModePin {
			label = theme.IconCheck + " pinned"
		} else if t.Mode == config.ModeLatest {
			label = theme.IconCheck + " latest"
		}
		return lipgloss.NewStyle().Foreground(theme.ColorProof).Render(label)
	case config.VersionMismatch:
		return lipgloss.NewStyle().Foreground(theme.ColorCopper).Bold(true).
			Render(theme.IconWarn + " mismatch")
	default:
		return lipgloss.NewStyle().Foreground(theme.ColorFaded).Italic(true).Render("unknown")
	}
}

func renderInfraRow(label, value string) string {
	l := lipgloss.NewStyle().Foreground(theme.ColorDusty).Width(18).Render(label)
	v := lipgloss.NewStyle().Foreground(theme.ColorLinen).Render(value)
	return " " + l + v + "\n"
}

func renderImplicitToolTable(tools []config.ImplicitToolConfig, width int) string {
	if len(tools) == 0 {
		return lipgloss.NewStyle().Foreground(theme.ColorFaded).Italic(true).
			Render("  No implicit language servers built.") + "\n"
	}

	verStyle := lipgloss.NewStyle().Foreground(theme.ColorLinen)
	nameStyle := lipgloss.NewStyle().Foreground(theme.ColorParchment)
	notInstalled := lipgloss.NewStyle().Foreground(theme.ColorFaded).Italic(true)

	tbl := tableutil.NewTable("TOOL", "FOR", "BINARY", "CONTAINER VERSION")
	tbl.SetHeaderStyle(theme.ColorDusty, true)
	sepColor := theme.ColorOakLight
	tbl.SetSeparator(theme.BorderH, &sepColor)

	for _, tool := range tools {
		version := notInstalled.Render("(not built)")
		if strings.TrimSpace(tool.ContainerVersion) != "" {
			version = verStyle.Render(tool.ContainerVersion)
		}
		tbl.AddRow(
			nameStyle.Render(tool.Name),
			nameStyle.Render(tool.ParentTool),
			verStyle.Render(displayImplicitValue(tool.Binary)),
			version,
		)
	}

	var b strings.Builder
	b.WriteString(" " + tbl.RenderHeader() + "\n")
	b.WriteString(theme.DividerStyle.Render(" "+strings.Repeat(theme.BorderH, width-2)) + "\n")
	_, rows := tbl.RenderRows(0)
	for _, row := range rows {
		b.WriteString(" " + row + "\n")
	}
	return b.String()
}

func displayImplicitValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return theme.BorderH
	}
	return value
}
