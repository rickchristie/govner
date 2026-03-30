// Package loading implements the Loading and Shutdown screens for the
// Cooper TUI. It shows a centered brand animation, progress bar, and
// step-by-step status as cooper up starts or shuts down services.
package loading

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

// StepStatus represents the state of a loading step.
type StepStatus int

const (
	StepPending StepStatus = iota
	StepRunning
	StepDone
	StepError
)

// LoadingStep describes one discrete startup or shutdown task.
type LoadingStep struct {
	Name   string
	Status StepStatus
	Detail string // error detail when Status == StepError
}

// progressTarget maps step index to the target progress percentage
// when that step completes. The slice is parallel to Model.Steps.
var startupProgress = []float64{0.15, 0.35, 0.50, 0.65, 0.80, 0.95, 1.0}
var shutdownProgress = []float64{0.10, 0.40, 0.70, 0.90, 1.0}

// StartupSteps returns the default step list for startup.
func StartupSteps() []LoadingStep {
	return []LoadingStep{
		{Name: "Creating cooper networks..."},
		{Name: "Proxy container starting..."},
		{Name: "SSL certificates loaded"},
		{Name: "Execution bridge starting..."},
		{Name: "CLI image version check..."},
		{Name: "ACL listener ready"},
		{Name: "barrel-proof and ready"},
	}
}

// ShutdownSteps returns the default step list for shutdown.
func ShutdownSteps() []LoadingStep {
	return []LoadingStep{
		{Name: "Denying pending requests..."},
		{Name: "Stopping containers..."},
		{Name: "Stopping proxy container..."},
		{Name: "Removing cooper networks..."},
		{Name: "barrel sealed"},
	}
}

// Model is the BubbleTea model for the loading/shutdown screen.
type Model struct {
	Steps       []LoadingStep
	CurrentStep int
	Done        bool
	HasError    bool
	Width       int
	Height      int
	IsShutdown  bool

	// Animation state.
	barrelFrame   int
	displayProg   float64 // displayed progress (chases target)
	targetProg    float64 // target progress based on completed steps
	lastAnimTick  time.Time
	holdStartTime time.Time // set when progress reaches 100%
	holdComplete  bool

	// error subtitle state.
	errorMsg string
}

// New creates a loading model. If shutdown is true, the shutdown step
// set and subtitle are used.
func New(shutdown bool) Model {
	steps := StartupSteps()
	if shutdown {
		steps = ShutdownSteps()
	}
	// Mark first step as running.
	if len(steps) > 0 {
		steps[0].Status = StepRunning
	}
	return Model{
		Steps:        steps,
		IsShutdown:   shutdown,
		lastAnimTick: time.Now(),
	}
}

// ----- Messages -----

// StepCompleteMsg signals that the step at Index finished successfully.
type StepCompleteMsg struct{ Index int }

// StepErrorMsg signals that the step at Index failed with Err.
type StepErrorMsg struct {
	Index int
	Err   error
}

// animTickMsg drives smooth progress bar and barrel roll animation.
type animTickMsg struct{}

// holdDoneMsg fires after the 800 ms hold at 100%.
type holdDoneMsg struct{}

// ----- tea.Model interface -----

// Init starts the animation ticker.
func (m Model) Init() tea.Cmd {
	return animTick()
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil

	case StepCompleteMsg:
		return m.completeStep(msg.Index)

	case StepErrorMsg:
		return m.failStep(msg.Index, msg.Err)

	case animTickMsg:
		return m.tickAnimation()

	case holdDoneMsg:
		m.holdComplete = true
		m.Done = true
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// View renders the full loading screen.
func (m Model) View(width, height int) string {
	if width > 0 {
		m.Width = width
	}
	if height > 0 {
		m.Height = height
	}

	var lines []string

	// Barrel roll animation.
	frames := theme.BarrelRollFrames()
	frame := frames[m.barrelFrame%len(frames)]
	barrelLine := lipgloss.NewStyle().Foreground(theme.ColorDusty).Render(frame)
	lines = append(lines, "")
	lines = append(lines, barrelLine)
	lines = append(lines, "")

	// Title: c o o p e r
	title := theme.TitleStyle.Render("c o o p e r")
	lines = append(lines, title)
	lines = append(lines, "")

	// Subtitle varies by state.
	subtitle := m.subtitle()
	subtitleStyle := lipgloss.NewStyle().Foreground(theme.ColorDusty)
	lines = append(lines, subtitleStyle.Render(subtitle))
	lines = append(lines, "")

	// Progress bar.
	lines = append(lines, m.progressBar())
	lines = append(lines, "")

	// Step list.
	for _, step := range m.Steps {
		lines = append(lines, m.renderStep(step))
	}
	lines = append(lines, "")

	// Help bar.
	lines = append(lines, m.helpLine())
	lines = append(lines, "")

	// Center the content block.
	content := strings.Join(lines, "\n")
	contentWidth := maxLineWidth(lines)

	// Horizontal centering: pad each line.
	centeredLines := make([]string, len(lines))
	for i, line := range lines {
		lw := lipgloss.Width(line)
		pad := (m.Width - lw) / 2
		if pad < 0 {
			pad = 0
		}
		centeredLines[i] = strings.Repeat(" ", pad) + line
	}
	content = strings.Join(centeredLines, "\n")

	// Vertical centering.
	contentHeight := strings.Count(content, "\n") + 1
	topPad := (m.Height - contentHeight) / 2
	if topPad < 0 {
		topPad = 0
	}
	_ = contentWidth // used for centering calculation above

	return strings.Repeat("\n", topPad) + content
}

// ----- SubModel adapter -----
// The root TUI model expects SubModel interface with (SubModel, tea.Cmd)
// returns. We expose UpdateSub to convert.

// UpdateSub adapts the loading Model for use as a tui.SubModel.
func (m Model) UpdateSub(msg tea.Msg) (Model, tea.Cmd) {
	return m.Update(msg)
}

// ViewSub adapts the loading Model for use as a tui.SubModel.
func (m Model) ViewSub(width, height int) string {
	return m.View(width, height)
}

// ----- Internal -----

func (m Model) subtitle() string {
	if m.HasError {
		if m.IsShutdown {
			return "the barrel won't close"
		}
		return "the barrel sprung a leak"
	}
	if m.Done {
		if m.IsShutdown {
			return "barrel sealed"
		}
		return "barrel-proof and ready"
	}
	if m.IsShutdown {
		return "sealing the barrel..."
	}
	return "rolling out the barrel..."
}

func (m Model) progressBar() string {
	barWidth := 30
	if m.Width > 60 {
		barWidth = 40
	}

	filled := int(m.displayProg * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled

	barColor := theme.ColorAmber
	if m.HasError {
		barColor = theme.ColorFlame
	}

	filledStyle := lipgloss.NewStyle().Foreground(barColor)
	tipStyle := lipgloss.NewStyle().Foreground(theme.ColorWheat)
	emptyStyle := lipgloss.NewStyle().Foreground(theme.ColorOakLight)
	pctStyle := lipgloss.NewStyle().Foreground(barColor)

	var bar string
	if filled > 0 && empty > 0 && !m.HasError {
		// Show tip at leading edge during chase.
		bar = filledStyle.Render(strings.Repeat(theme.ProgressFull, filled-1)) +
			tipStyle.Render(theme.ProgressTip) +
			emptyStyle.Render(strings.Repeat(theme.ProgressEmpty, empty))
	} else if filled > 0 {
		bar = filledStyle.Render(strings.Repeat(theme.ProgressFull, filled)) +
			emptyStyle.Render(strings.Repeat(theme.ProgressEmpty, empty))
	} else {
		bar = emptyStyle.Render(strings.Repeat(theme.ProgressEmpty, barWidth))
	}

	pct := pctStyle.Render(fmt.Sprintf(" %d%%", int(m.displayProg*100)))
	return bar + pct
}

func (m Model) renderStep(step LoadingStep) string {
	switch step.Status {
	case StepDone:
		icon := lipgloss.NewStyle().Foreground(theme.ColorProof).Render(theme.IconCheck)
		text := lipgloss.NewStyle().Foreground(theme.ColorLinen).Render(step.Name)
		return icon + " " + text

	case StepRunning:
		icon := lipgloss.NewStyle().Foreground(theme.ColorAmber).Render("\u00B7")
		text := lipgloss.NewStyle().Foreground(theme.ColorDusty).Render(step.Name)
		return icon + " " + text

	case StepError:
		icon := lipgloss.NewStyle().Foreground(theme.ColorFlame).Render(theme.IconCross)
		text := lipgloss.NewStyle().Foreground(theme.ColorFlame).Render(step.Name)
		line := icon + " " + text
		if step.Detail != "" {
			detail := lipgloss.NewStyle().Foreground(theme.ColorDusty).Render("  " + step.Detail)
			line += "\n" + detail
		}
		return line

	default: // StepPending
		icon := lipgloss.NewStyle().Foreground(theme.ColorDusty).Render("\u00B7")
		text := lipgloss.NewStyle().Foreground(theme.ColorFaded).Render(step.Name)
		return icon + " " + text
	}
}

func (m Model) helpLine() string {
	if m.IsShutdown {
		if m.HasError {
			return "[" + theme.HelpKeyStyle.Render("q") + " " +
				theme.HelpDescStyle.Render("Force Quit") + "]" +
				strings.Repeat(" ", 30) + theme.BarrelEmoji
		}
		// No keys during normal shutdown.
		return strings.Repeat(" ", 40) + theme.BarrelEmoji
	}

	if m.HasError {
		return "[" + theme.HelpKeyStyle.Render("q") + " " +
			theme.HelpDescStyle.Render("Quit") + "]" +
			strings.Repeat(" ", 30) + theme.BarrelEmoji
	}

	return "[" + theme.HelpKeyStyle.Render("q") + " " +
		theme.HelpDescStyle.Render("Cancel") + "]" +
		strings.Repeat(" ", 30) + theme.BarrelEmoji
}

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		if m.IsShutdown && !m.HasError {
			// Cannot cancel a normal shutdown.
			return m, nil
		}
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) completeStep(idx int) (Model, tea.Cmd) {
	if idx < 0 || idx >= len(m.Steps) {
		return m, nil
	}

	m.Steps[idx].Status = StepDone

	// Update target progress.
	progTable := startupProgress
	if m.IsShutdown {
		progTable = shutdownProgress
	}
	if idx < len(progTable) {
		m.targetProg = progTable[idx]
	}

	// Advance to next step.
	next := idx + 1
	m.CurrentStep = next
	if next < len(m.Steps) {
		m.Steps[next].Status = StepRunning
	}

	// If this was the last step, set target to 100%.
	if next >= len(m.Steps) {
		m.targetProg = 1.0
	}

	return m, nil
}

func (m Model) failStep(idx int, err error) (Model, tea.Cmd) {
	if idx < 0 || idx >= len(m.Steps) {
		return m, nil
	}

	m.Steps[idx].Status = StepError
	if err != nil {
		m.Steps[idx].Detail = err.Error()
		m.errorMsg = err.Error()
	}
	m.HasError = true
	return m, nil
}

func (m Model) tickAnimation() (Model, tea.Cmd) {
	now := time.Now()

	// Barrel roll: advance frame every ~150ms.
	if now.Sub(m.lastAnimTick) >= theme.BarrelRollInterval {
		m.barrelFrame++
		m.lastAnimTick = now
	}

	// Progress bar stagger: chase target in increments.
	if m.displayProg < m.targetProg {
		increment := 0.15 // 15% increments per tick
		m.displayProg += increment
		if m.displayProg > m.targetProg {
			m.displayProg = m.targetProg
		}
	}

	// Check if we hit 100% and need to hold.
	if m.displayProg >= 1.0 && !m.Done && !m.HasError {
		if m.holdStartTime.IsZero() {
			m.holdStartTime = now
			return m, tea.Batch(animTick(), holdTimer())
		}
	}

	return m, animTick()
}

// animTick returns a command that sends an animTickMsg after the stagger delay.
func animTick() tea.Cmd {
	return tea.Tick(theme.ProgressStaggerDelay, func(time.Time) tea.Msg {
		return animTickMsg{}
	})
}

// holdTimer returns a command that sends holdDoneMsg after the loading hold duration.
func holdTimer() tea.Cmd {
	return tea.Tick(theme.LoadingHoldDuration, func(time.Time) tea.Msg {
		return holdDoneMsg{}
	})
}

// CompleteStep returns a tea.Cmd that sends a StepCompleteMsg for the given index.
func CompleteStep(idx int) tea.Cmd {
	return func() tea.Msg {
		return StepCompleteMsg{Index: idx}
	}
}

// FailStep returns a tea.Cmd that sends a StepErrorMsg for the given index.
func FailStep(idx int, err error) tea.Cmd {
	return func() tea.Msg {
		return StepErrorMsg{Index: idx, Err: err}
	}
}

// maxLineWidth returns the maximum rendered width among a set of lines.
func maxLineWidth(lines []string) int {
	max := 0
	for _, l := range lines {
		w := lipgloss.Width(l)
		if w > max {
			max = w
		}
	}
	return max
}
