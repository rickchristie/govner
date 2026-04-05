// Package configure implements the interactive TUI wizard for `cooper configure`.
// It walks the user through programming tools, AI CLI tools, proxy whitelist,
// port forwarding, proxy settings, and a save/build step.
package configure

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// Screen identifies the current wizard screen.
type Screen int

const (
	ScreenWelcome Screen = iota
	ScreenProgramming
	ScreenAICLI
	ScreenWhitelist
	ScreenPortForward
	ScreenProxy
	ScreenSave
)

// model is the top-level bubbletea model for the configure wizard.
type model struct {
	screen       Screen
	cfg          *config.Config
	cooperDir    string
	configPath   string
	configureApp *app.ConfigureApp
	width        int
	height       int
	existing     bool // true if config was loaded from disk

	// Quit confirmation modal.
	showQuitModal    bool
	quitModalConfirm bool // true = Confirm focused, false = Cancel focused

	// Version changes modal — shown on startup when mirror versions changed.
	showChangesModal  bool
	versionChanges    []string // e.g. "claude: 2.1.89 → 2.1.90"

	// Sub-screen models.
	welcome     welcomeModel
	programming programmingModel
	aicli       aicliModel
	whitelist   whitelistModel
	portForward portFwdModel
	proxySetup  proxyModel
	save        saveModel
}

// dockerInstallHint returns OS-specific instructions for installing Docker.
func dockerInstallHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "Install Docker Desktop for Mac:\n" +
			"  https://docs.docker.com/desktop/setup/install/mac-install/\n" +
			"\n" +
			"Or via Homebrew:\n" +
			"  brew install --cask docker"
	case "windows":
		return "Install Docker Desktop for Windows:\n" +
			"  https://docs.docker.com/desktop/setup/install/windows-install/\n" +
			"\n" +
			"Or via winget:\n" +
			"  winget install Docker.DockerDesktop"
	default: // linux
		return "Install Docker Engine for Linux:\n" +
			"  https://docs.docker.com/engine/install/\n" +
			"\n" +
			"Quick install (convenience script):\n" +
			"  curl -fsSL https://get.docker.com | sh\n" +
			"\n" +
			"After installing, ensure your user is in the docker group:\n" +
			"  sudo usermod -aG docker $USER\n" +
			"  (log out and back in for this to take effect)"
	}
}

// checkDocker verifies that Docker is installed and running, and warns if the
// version is older than 20.10.
func checkDocker() error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("Docker is not installed.\n\n%s", dockerInstallHint())
	}

	cmd := exec.Command("docker", "version", "--format", "{{.Server.Version}}")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("Docker is installed but the daemon is not running.\n"+
			"Please start the Docker daemon and try again.\n\n"+
			"On Linux:  sudo systemctl start docker\n"+
			"On macOS:  open -a Docker\n"+
			"On Windows: start Docker Desktop")
	}

	version := strings.TrimSpace(string(output))
	if version == "" {
		return fmt.Errorf("Docker is not running. Please start the Docker daemon")
	}

	// Parse major.minor to warn on old versions.
	parts := strings.SplitN(version, ".", 3)
	if len(parts) >= 2 {
		major := 0
		minor := 0
		fmt.Sscanf(parts[0], "%d", &major)
		fmt.Sscanf(parts[1], "%d", &minor)
		if major < 20 || (major == 20 && minor < 10) {
			fmt.Fprintf(os.Stderr, "Warning: Docker version %s detected. Cooper requires Docker >= 20.10 for full compatibility.\n", version)
		}
	}

	return nil
}

// RunResult holds the outcome of the configure wizard.
type RunResult struct {
	// Saved is true if the user saved the configuration.
	Saved bool
	// BuildRequested is true if the user chose "Save & Build" or "Save & Clean Build".
	BuildRequested bool
	// CleanBuild is true if the user chose "Save & Clean Build" (no Docker cache).
	CleanBuild bool
}

// Run is the entry point called from main.go. It accepts a ConfigureApp
// that provides the configuration state, and runs the bubbletea TUI wizard.
// The returned RunResult indicates whether the user saved and whether they
// requested a build.
func Run(ca *app.ConfigureApp) (RunResult, error) {
	// Check Docker is installed and running before proceeding.
	if err := checkDocker(); err != nil {
		return RunResult{}, err
	}

	cfg := ca.Config()
	cooperDir := ca.CooperDir()
	existing := ca.IsExisting()

	m := newModel(cfg, cooperDir, ca, existing)
	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return RunResult{}, fmt.Errorf("configure wizard: %w", err)
	}

	var result RunResult
	if fm, ok := finalModel.(*model); ok && fm.save.saved {
		result.Saved = true
		result.BuildRequested = fm.save.buildRequested
		result.CleanBuild = fm.save.cleanBuildRequested
		if fm.save.buildRequested {
			if fm.save.cleanBuildRequested {
				fmt.Fprintln(os.Stderr, "\nConfiguration saved. Starting clean build (no cache)...")
			} else {
				fmt.Fprintln(os.Stderr, "\nConfiguration saved. Starting build...")
			}
		} else {
			fmt.Fprintln(os.Stderr, "\nConfiguration saved.")
		}
	}

	return result, nil
}


func newModel(cfg *config.Config, cooperDir string, ca *app.ConfigureApp, existing bool) *model {
	configPath := ""
	if cooperDir != "" {
		configPath = cooperDir + "/config.json"
	}
	m := &model{
		screen:       ScreenWelcome,
		cfg:          cfg,
		cooperDir:    cooperDir,
		configPath:   configPath,
		configureApp: ca,
		existing:     existing,
	}
	m.welcome = newWelcomeModel(existing)
	m.programming = newProgrammingModel(cfg.ProgrammingTools)
	m.aicli = newAICLIModel(cfg.AITools)
	m.whitelist = newWhitelistModel(cfg.WhitelistedDomains)
	m.portForward = newPortFwdModel(cfg.PortForwardRules)
	m.proxySetup = newProxyModel(cfg.ProxyPort, cfg.BridgePort, cfg.BarrelSHMSize)
	m.save = newSaveModel(cfg, cooperDir, configPath, ca)

	// Detect version changes for mirror mode tools.
	m.versionChanges = m.detectMirrorChanges()
	if len(m.versionChanges) > 0 {
		m.showChangesModal = true
	}

	return m
}

// detectMirrorChanges finds mirror-mode tools where the host version
// differs from the built container version.
func (m *model) detectMirrorChanges() []string {
	var changes []string
	allTools := [][]toolEntry{m.programming.tools, m.aicli.tools}
	for _, tools := range allTools {
		for _, t := range tools {
			if !t.enabled || t.mode != config.ModeMirror {
				continue
			}
			if t.containerVersion != "" && t.hostVersion != "" && t.containerVersion != t.hostVersion {
				changes = append(changes, fmt.Sprintf("%s: %s → %s", t.displayName, t.containerVersion, t.hostVersion))
			}
		}
	}
	return changes
}

func (m *model) Init() tea.Cmd {
	return nil
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		// Handle version changes modal when it is showing.
		if m.showChangesModal {
			switch msg.String() {
			case "enter", "esc", " ":
				m.showChangesModal = false
			}
			return m, nil
		}

		// Handle quit confirmation modal when it is showing.
		if m.showQuitModal {
			switch msg.String() {
			case "enter":
				if m.quitModalConfirm {
					return m, tea.Quit
				}
				// Cancel focused — close modal.
				m.showQuitModal = false
				return m, nil
			case "y":
				return m, tea.Quit
			case "esc", "n":
				m.showQuitModal = false
				return m, nil
			case "up", "down", "left", "right":
				m.quitModalConfirm = !m.quitModalConfirm
				return m, nil
			}
			return m, nil
		}

		// Show quit modal on ctrl+c or q, unless a text input is focused.
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			if !m.isTextInputActive() {
				m.showQuitModal = true
				m.quitModalConfirm = true // Default focus on Confirm.
				return m, nil
			}
		}
	}

	switch m.screen {
	case ScreenWelcome:
		return m.updateWelcome(msg)
	case ScreenProgramming:
		return m.updateProgramming(msg)
	case ScreenAICLI:
		return m.updateAICLI(msg)
	case ScreenWhitelist:
		return m.updateWhitelist(msg)
	case ScreenPortForward:
		return m.updatePortForward(msg)
	case ScreenProxy:
		return m.updateProxy(msg)
	case ScreenSave:
		return m.updateSave(msg)
	}

	return m, nil
}

// isTextInputActive returns true if any text input field across all sub-screens
// is currently focused. Used to prevent the quit modal from intercepting the 'q'
// key when the user is typing.
func (m *model) isTextInputActive() bool {
	switch m.screen {
	case ScreenProgramming:
		return m.programming.pinInput.focused
	case ScreenAICLI:
		return m.aicli.pinInput.focused
	case ScreenWhitelist:
		if m.whitelist.modal.active {
			return true
		}
	case ScreenPortForward:
		if m.portForward.portModal.active {
			return true
		}
	case ScreenProxy:
		// Proxy screen always has a text input focused.
		return true
	}
	return false
}

func (m *model) View() string {
	var content string

	switch m.screen {
	case ScreenWelcome:
		content = m.welcome.view(m.width, m.height, m.existing)
	case ScreenProgramming:
		content = m.programming.view(m.width, m.height)
	case ScreenAICLI:
		content = m.aicli.view(m.width, m.height)
	case ScreenWhitelist:
		content = m.whitelist.view(m.width, m.height)
	case ScreenPortForward:
		content = m.portForward.view(m.width, m.height)
	case ScreenProxy:
		content = m.proxySetup.view(m.width, m.height)
	case ScreenSave:
		content = m.save.view(m.width, m.height)
	default:
		content = "Unknown screen"
	}

	if m.showChangesModal {
		content = overlayModal(content, m.viewChangesModal(), m.width, m.height)
	}

	if m.showQuitModal {
		content = overlayModal(content, m.viewQuitModal(), m.width, m.height)
	}

	return content
}

// viewChangesModal renders the version changes notification modal.
func (m *model) viewChangesModal() string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(theme.ColorCopper).
		Padding(1, 3).
		Width(min(60, m.width-10))

	titleStyle := lipgloss.NewStyle().Foreground(theme.ColorCopper).Bold(true)
	changeStyle := lipgloss.NewStyle().Foreground(theme.ColorParchment)
	hintStyle := lipgloss.NewStyle().Foreground(theme.ColorDusty)

	var inner string
	inner += "  " + titleStyle.Render(theme.IconWarn+" Version Changes Detected") + "\n\n"
	inner += "  " + hintStyle.Render("Mirror mode tools have new host versions:") + "\n\n"

	for _, change := range m.versionChanges {
		inner += "  " + changeStyle.Render("  "+theme.IconArrowRight+" "+change) + "\n"
	}

	inner += "\n"
	inner += "  " + hintStyle.Render("Go to Save & Build to apply these changes.") + "\n\n"
	inner += "  " + lipgloss.NewStyle().Foreground(theme.ColorProof).Render("[Enter] OK")

	return boxStyle.Render(inner)
}

// viewQuitModal renders the quit confirmation modal.
func (m *model) viewQuitModal() string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(theme.ColorAmber).
		Padding(1, 3).
		Width(min(48, m.width-10))

	titleStyle := lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true)
	bodyStyle := lipgloss.NewStyle().Foreground(theme.ColorLinen)
	confirmStyle := lipgloss.NewStyle().Foreground(theme.ColorProof).Bold(true)
	cancelStyle := lipgloss.NewStyle().Foreground(theme.ColorDusty)
	sepStyle := lipgloss.NewStyle().Foreground(theme.ColorOakLight)

	var inner string
	inner += "  " + theme.IconWarn + " " + titleStyle.Render("Quit Cooper Configure?") + "\n\n"
	inner += "  " + sepStyle.Render(repeatStr(theme.BorderH, 38)) + "\n\n"
	inner += "  " + bodyStyle.Render("All unsaved changes will be lost.") + "\n\n"
	inner += "  " + sepStyle.Render(repeatStr(theme.BorderH, 38)) + "\n\n"
	confirmLabel := "[Confirm]"
	cancelLabel := "[Cancel]"
	if m.quitModalConfirm {
		inner += "  " + lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render(theme.IconArrowRight+" ") +
			confirmStyle.Render(confirmLabel) + "    " + cancelStyle.Render(cancelLabel)
	} else {
		inner += "  " + confirmStyle.Render("  "+confirmLabel) + "    " +
			lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true).Render(theme.IconArrowRight+" ") +
			cancelStyle.Render(cancelLabel)
	}

	return boxStyle.Render(inner)
}

// syncConfigFromSubModels updates cfg from the current sub-model state.
func (m *model) syncConfigFromSubModels() {
	m.cfg.ProgrammingTools = m.programming.toToolConfigs()
	m.cfg.AITools = m.aicli.toToolConfigs()
	m.cfg.WhitelistedDomains = m.whitelist.toDomainEntries()
	m.cfg.PortForwardRules = m.portForward.toPortForwardRules()
	m.cfg.ProxyPort = m.proxySetup.proxyPort
	m.cfg.BridgePort = m.proxySetup.bridgePort
	m.cfg.BarrelSHMSize = m.proxySetup.shmSize
}

// navigateTo switches to a new screen and syncs config.
func (m *model) navigateTo(screen Screen) {
	m.syncConfigFromSubModels()
	m.screen = screen
	// Refresh save model with latest config.
	if screen == ScreenSave {
		m.save = newSaveModel(m.cfg, m.cooperDir, m.configPath, m.configureApp)
	}
}

// --- Update dispatchers ---

func (m *model) updateWelcome(msg tea.Msg) (tea.Model, tea.Cmd) {
	result := m.welcome.update(msg)
	switch result {
	case welcomeQuit:
		return m, tea.Quit
	case welcomeSelect:
		switch m.welcome.cursor {
		case 0:
			m.navigateTo(ScreenProgramming)
		case 1:
			m.navigateTo(ScreenAICLI)
		case 2:
			m.navigateTo(ScreenWhitelist)
		case 3:
			m.navigateTo(ScreenPortForward)
		case 4:
			m.navigateTo(ScreenProxy)
		case 5:
			m.navigateTo(ScreenSave)
		}
	}
	return m, nil
}

func (m *model) updateProgramming(msg tea.Msg) (tea.Model, tea.Cmd) {
	result := m.programming.update(msg)
	if result == toolScreenBack {
		m.navigateTo(ScreenWelcome)
	}
	return m, nil
}

func (m *model) updateAICLI(msg tea.Msg) (tea.Model, tea.Cmd) {
	result := m.aicli.update(msg)
	if result == toolScreenBack {
		m.navigateTo(ScreenWelcome)
	}
	return m, nil
}

func (m *model) updateWhitelist(msg tea.Msg) (tea.Model, tea.Cmd) {
	result := m.whitelist.update(msg)
	if result == whitelistBack {
		m.navigateTo(ScreenWelcome)
	}
	return m, nil
}

func (m *model) updatePortForward(msg tea.Msg) (tea.Model, tea.Cmd) {
	result := m.portForward.update(msg)
	if result == portFwdBack {
		m.navigateTo(ScreenWelcome)
	}
	return m, nil
}

func (m *model) updateProxy(msg tea.Msg) (tea.Model, tea.Cmd) {
	result := m.proxySetup.update(msg)
	if result == proxyBack {
		m.navigateTo(ScreenWelcome)
	}
	return m, nil
}

func (m *model) updateSave(msg tea.Msg) (tea.Model, tea.Cmd) {
	result := m.save.update(msg)
	switch result {
	case saveBack:
		m.navigateTo(ScreenWelcome)
	case saveQuit:
		return m, tea.Quit
	}
	return m, nil
}

// --- Welcome screen ---

type welcomeResult int

const (
	welcomeNone   welcomeResult = iota
	welcomeSelect               // user pressed enter on a menu item
	welcomeQuit                 // user wants to quit
)

type welcomeModel struct {
	cursor int
	items  []welcomeItem
}

type welcomeItem struct {
	label string
	desc  string
}

func newWelcomeModel(existing bool) welcomeModel {
	return welcomeModel{
		items: []welcomeItem{
			{label: "Programming Tools", desc: "Go, Node.js, Python"},
			{label: "AI CLI Tools", desc: "Claude Code, Copilot, Codex, OpenCode"},
			{label: "Proxy Whitelist", desc: "Domain whitelist for network access"},
			{label: "Port Forwarding to Host", desc: "Route container ports to host services"},
			{label: "Proxy Settings", desc: "Proxy port, bridge port"},
			{label: "Save & Build", desc: "Write config, build images"},
		},
	}
}

func (w *welcomeModel) update(msg tea.Msg) welcomeResult {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if w.cursor > 0 {
				w.cursor--
			}
		case "down", "j":
			if w.cursor < len(w.items)-1 {
				w.cursor++
			}
		case "enter":
			return welcomeSelect
		case "1":
			w.cursor = 0
			return welcomeSelect
		case "2":
			w.cursor = 1
			return welcomeSelect
		case "3":
			w.cursor = 2
			return welcomeSelect
		case "4":
			w.cursor = 3
			return welcomeSelect
		case "5":
			w.cursor = 4
			return welcomeSelect
		case "6":
			w.cursor = 5
			return welcomeSelect
		}
	}
	return welcomeNone
}

func (w *welcomeModel) view(width, height int, existing bool) string {
	titleAmber := lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true)
	titleLinen := lipgloss.NewStyle().Foreground(theme.ColorLinen)
	tagline := lipgloss.NewStyle().Foreground(theme.ColorDusty).Italic(true)
	menuNum := lipgloss.NewStyle().Foreground(theme.ColorAmber)
	menuLabel := lipgloss.NewStyle().Foreground(theme.ColorParchment)
	menuDesc := lipgloss.NewStyle().Foreground(theme.ColorDusty)
	selectedBg := lipgloss.NewStyle().Background(theme.ColorOakMid)
	arrow := lipgloss.NewStyle().Foreground(theme.ColorAmber).Bold(true)

	var s string
	s += "\n\n"
	s += center(theme.BarrelEmoji, width) + "\n\n"
	s += center(titleAmber.Render("c o o p e r")+"  "+titleLinen.Render("c o n f i g u r e"), width) + "\n\n"
	s += center(tagline.Render("Barrel-proof containers for undiluted AI."), width) + "\n\n\n"

	// Menu items — rendered as a left-aligned block, then the block is centered.
	var menuLines []string
	for i, item := range w.items {
		prefix := "   "
		if i == w.cursor {
			prefix = " " + arrow.Render(theme.IconArrowRight) + " "
		}
		numStr := menuNum.Render(fmt.Sprintf("%d.", i+1))
		labelStr := menuLabel.Render(item.label)
		descStr := menuDesc.Render(item.desc)
		line := fmt.Sprintf("%s%s %-24s %s", prefix, numStr, labelStr, descStr)
		if i == w.cursor {
			line = selectedBg.Render(line)
		}
		menuLines = append(menuLines, line)
	}
	menuBlock := strings.Join(menuLines, "\n")

	// Status line.
	var statusLine string
	if existing {
		statusLine = lipgloss.NewStyle().Foreground(theme.ColorProof).Render("Status: Existing configuration loaded.")
	} else {
		statusLine = lipgloss.NewStyle().Foreground(theme.ColorCopper).Render("Status: No existing configuration found. Starting fresh.")
	}

	// Help bar.
	helpLine := helpBar("\u2191\u2193 Nav", "Enter Select", "q Quit")

	// Combine menu + status + help into a single left-aligned content block,
	// then center the entire block within the terminal width by adding
	// a uniform left margin to every line.
	contentBlock := menuBlock + "\n\n" + statusLine + "\n\n" + helpLine

	// Find the widest line in the content block to determine the block width.
	blockWidth := 0
	for _, line := range strings.Split(contentBlock, "\n") {
		lw := lipgloss.Width(line)
		if lw > blockWidth {
			blockWidth = lw
		}
	}

	// Calculate left margin to center the block.
	margin := (width - blockWidth) / 2
	if margin < 0 {
		margin = 0
	}
	pad := strings.Repeat(" ", margin)

	// Apply uniform left margin to every line.
	centeredLines := strings.Split(contentBlock, "\n")
	for i, line := range centeredLines {
		centeredLines[i] = pad + line
	}
	s += strings.Join(centeredLines, "\n") + "\n"

	return s
}

// center centers a string within the given width.
func center(s string, width int) string {
	return lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(s)
}
