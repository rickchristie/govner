package tui

import (
	"fmt"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rickchristie/govner/pgflock/internal/locker"
)

// Message types
type (
	// stateUpdateMsg is sent when locker state changes
	stateUpdateMsg struct {
		state *locker.State
	}

	// tickMsg is sent periodically to update time displays
	tickMsg time.Time

	// animationTickMsg is sent for animation updates (faster rate)
	animationTickMsg time.Time

	// loadingTickMsg is sent for loading screen sheep animation (100ms)
	loadingTickMsg time.Time

	// loadingProgressTickMsg is sent for staggered progress animation (200ms)
	loadingProgressTickMsg time.Time

	// loadingProgressMsg is sent when loading progress updates
	loadingProgressMsg struct {
		progress LoadingProgress
	}

	// copyShimmerTickMsg is sent for copy shimmer animation
	copyShimmerTickMsg time.Time

	// stopCopyShimmerMsg stops the copy shimmer animation
	stopCopyShimmerMsg struct{}

	// errMsg is sent when an error occurs
	errMsg struct {
		err error
	}
)

// Init initializes the TUI model
func (m *Model) Init() tea.Cmd {
	var cmds []tea.Cmd

	// Start appropriate ticks based on mode
	if m.showingLoading {
		cmds = append(cmds, m.loadingTick(), m.loadingProgressTick())
		if m.loadingProgressChan != nil {
			cmds = append(cmds, m.waitForLoadingProgress())
		}
	} else {
		if m.stateChan != nil {
			cmds = append(cmds, m.waitForStateUpdate())
		}
		cmds = append(cmds, m.tick(), m.animationTick())
	}

	return tea.Batch(cmds...)
}

// waitForStateUpdate waits for state updates from the locker
func (m *Model) waitForStateUpdate() tea.Cmd {
	return func() tea.Msg {
		if m.stateChan == nil {
			return nil
		}
		state, ok := <-m.stateChan
		if !ok {
			return nil
		}
		return stateUpdateMsg{state: state}
	}
}

// waitForLoadingProgress waits for loading progress updates
func (m *Model) waitForLoadingProgress() tea.Cmd {
	return func() tea.Msg {
		if m.loadingProgressChan == nil {
			return nil
		}
		progress, ok := <-m.loadingProgressChan
		if !ok {
			return nil
		}
		return loadingProgressMsg{progress: progress}
	}
}

// tick sends periodic tick messages (1 second) for time display updates
func (m *Model) tick() tea.Cmd {
	return tea.Tick(TickInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// animationTick sends periodic tick messages for animations (100ms)
func (m *Model) animationTick() tea.Cmd {
	return tea.Tick(LockedAnimationInterval, func(t time.Time) tea.Msg {
		return animationTickMsg(t)
	})
}

// loadingTick sends periodic tick messages for loading screen sheep animation (100ms)
func (m *Model) loadingTick() tea.Cmd {
	return tea.Tick(StartupFrameInterval, func(t time.Time) tea.Msg {
		return loadingTickMsg(t)
	})
}

// loadingProgressTick sends periodic tick messages for staggered progress animation (50ms)
func (m *Model) loadingProgressTick() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(t time.Time) tea.Msg {
		return loadingProgressTickMsg(t)
	})
}

// copyShimmerTick sends tick messages for copy shimmer animation (250ms)
func (m *Model) copyShimmerTick() tea.Cmd {
	return tea.Tick(CopyShimmerInterval, func(t time.Time) tea.Msg {
		return copyShimmerTickMsg(t)
	})
}

// stopCopyShimmerAfterDelay returns a command that stops shimmer after the duration
func (m *Model) stopCopyShimmerAfterDelay() tea.Cmd {
	return tea.Tick(CopyShimmerDuration, func(t time.Time) tea.Msg {
		return stopCopyShimmerMsg{}
	})
}

// Update handles messages and updates the model
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case stateUpdateMsg:
		m.state = msg.state
		m.updateAllDatabasesLockStatus()

		// Adjust selection and scroll if out of bounds (for locked view)
		if !m.showAllDatabases && m.state != nil {
			maxIdx := len(m.state.Locks) - 1
			if maxIdx < 0 {
				maxIdx = 0
			}
			if m.selectedIdx > maxIdx {
				m.selectedIdx = maxIdx
			}
			// Reset scroll offset when content shrinks significantly
			m.adjustScrollOffset(len(m.state.Locks))
		}
		return m, m.waitForStateUpdate()

	case tickMsg:
		// Refresh state from handler directly to update time displays
		if m.handler != nil {
			m.state = m.handler.GetState()
			m.updateAllDatabasesLockStatus()

			// Adjust selection and scroll if out of bounds (for locked view)
			if !m.showAllDatabases && m.state != nil {
				maxIdx := len(m.state.Locks) - 1
				if maxIdx < 0 {
					maxIdx = 0
				}
				if m.selectedIdx > maxIdx {
					m.selectedIdx = maxIdx
				}
				// Reset scroll offset when content shrinks
				m.adjustScrollOffset(len(m.state.Locks))
			}
		}
		return m, m.tick()

	case animationTickMsg:
		// Advance the LOCKED animation (only when not in loading screen)
		if !m.showingLoading {
			m.lockedAnimator.Tick()
			return m, m.animationTick()
		}
		return m, nil

	case loadingTickMsg:
		// Advance the loading screen sheep animation
		if m.showingLoading {
			m.loadingScreen.Tick()
			return m, m.loadingTick()
		}
		return m, nil

	case loadingProgressTickMsg:
		// Advance the staggered progress animation
		if m.showingLoading && !m.loadingScreen.IsDone() && !m.loadingScreen.IsFailed() {
			m.loadingScreen.TickProgress()

			// Check if loading screen completed after progress tick
			if m.loadingScreen.IsDone() {
				return m.handleLoadingComplete()
			}

			return m, m.loadingProgressTick()
		}
		return m, nil

	case loadingProgressMsg:
		// Update loading screen with progress event
		m.loadingScreen.UpdateProgress(msg.progress)

		// Check if failed
		if m.loadingScreen.IsFailed() {
			// Stay in loading view showing error
			return m, nil
		}

		// Continue waiting for more progress (staggered animation handles completion)
		return m, m.waitForLoadingProgress()

	case copyShimmerTickMsg:
		// Advance the copy shimmer animation
		if m.copyShimmer.IsActive() {
			m.copyShimmer.Tick()
			return m, m.copyShimmerTick()
		}
		return m, nil

	case stopCopyShimmerMsg:
		m.copyShimmer.Stop()
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, nil
	}

	return m, nil
}

// handleLoadingComplete handles the transition from loading screen to main view or quit.
func (m *Model) handleLoadingComplete() (tea.Model, tea.Cmd) {
	// If shutdown mode, quit the application
	if m.loadingScreen.Mode() == LoadingModeShutdown {
		m.quitting = true
		return m, tea.Quit
	}

	// Startup/Restart mode: transition to main view
	m.showingLoading = false

	// Refresh state after restart
	if m.handler != nil {
		m.state = m.handler.GetState()
		m.updateAllDatabasesLockStatus()
	}

	var cmds []tea.Cmd
	if m.stateChan != nil {
		cmds = append(cmds, m.waitForStateUpdate())
	}
	cmds = append(cmds, m.tick(), m.animationTick())
	return m, tea.Batch(cmds...)
}

// handleKeyPress handles keyboard input
func (m *Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle loading mode keys
	if m.showingLoading {
		// During shutdown, no keys allowed
		if m.loadingScreen.Mode() == LoadingModeShutdown {
			return m, nil
		}

		// During startup, only allow quit/cancel
		switch msg.String() {
		case "q", "ctrl+c":
			// Quit during startup (or after failure)
			m.quitting = true
			if m.onQuit != nil {
				m.onQuit()
			}
			return m, tea.Quit
		}
		// No other keys during loading
		return m, nil
	}

	// Handle confirmation dialog keys first
	if m.confirm != ConfirmNone {
		return m.handleConfirmKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		m.confirm = ConfirmQuit
		return m, nil

	case "r":
		m.confirm = ConfirmRestart
		return m, nil

	case "u":
		if db := m.selectedDatabase(); db != nil && db.IsLocked {
			m.confirm = ConfirmUnlock
		}
		return m, nil

	case "c":
		if db := m.selectedDatabase(); db != nil {
			return m.copyToClipboard(db.ConnString)
		}
		return m, nil

	case " ":
		m.showAllDatabases = !m.showAllDatabases
		m.selectedIdx = 0
		m.scrollOffset = 0
		return m, nil

	case "up", "k":
		if m.selectedIdx > 0 {
			m.selectedIdx--
			// Adjust scroll offset to keep selection visible
			m.adjustScrollOffset(m.getCurrentListSize())
		}
		return m, nil

	case "down", "j":
		maxIdx := m.getMaxSelectionIndex()
		if m.selectedIdx < maxIdx {
			m.selectedIdx++
			// Adjust scroll offset to keep selection visible
			m.adjustScrollOffset(m.getCurrentListSize())
		}
		return m, nil
	}

	return m, nil
}

// copyToClipboard copies the psql command to clipboard with shimmer animation
func (m *Model) copyToClipboard(connStr string) (tea.Model, tea.Cmd) {
	psqlCmd := fmt.Sprintf("psql '%s'", connStr)

	// Try xclip first (Linux), then xsel, then pbcopy (macOS)
	var cmd *exec.Cmd
	if _, err := exec.LookPath("xclip"); err == nil {
		cmd = exec.Command("xclip", "-selection", "clipboard")
	} else if _, err := exec.LookPath("xsel"); err == nil {
		cmd = exec.Command("xsel", "--clipboard", "--input")
	} else if _, err := exec.LookPath("pbcopy"); err == nil {
		cmd = exec.Command("pbcopy")
	} else {
		m.err = fmt.Errorf("no clipboard tool found (xclip/xsel/pbcopy)")
		return m, nil
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		m.err = fmt.Errorf("clipboard error: %w", err)
		return m, nil
	}

	if err := cmd.Start(); err != nil {
		m.err = fmt.Errorf("clipboard error: %w", err)
		return m, nil
	}

	stdin.Write([]byte(psqlCmd))
	stdin.Close()

	if err := cmd.Wait(); err != nil {
		m.err = fmt.Errorf("clipboard error: %w", err)
		return m, nil
	}

	// Start shimmer animation
	m.copyShimmer.Start()
	m.err = nil // Clear any previous error

	return m, tea.Batch(
		m.copyShimmerTick(),
		m.stopCopyShimmerAfterDelay(),
	)
}

// handleConfirmKey handles keys when a confirmation dialog is shown
func (m *Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "y":
		return m.executeConfirmedAction()

	case "esc", "n":
		m.confirm = ConfirmNone
		return m, nil
	}

	return m, nil
}

// executeConfirmedAction executes the confirmed action
func (m *Model) executeConfirmedAction() (tea.Model, tea.Cmd) {
	action := m.confirm
	m.confirm = ConfirmNone

	switch action {
	case ConfirmQuit:
		// Use graceful shutdown with loading screen if available
		if m.onShutdown != nil {
			progressChan := m.onShutdown()
			m.StartShutdown(progressChan)
			// Start loading screen ticks and progress listener
			return m, tea.Batch(
				m.loadingTick(),
				m.loadingProgressTick(),
				m.waitForLoadingProgress(),
			)
		}
		// Fallback to immediate quit
		m.quitting = true
		if m.onQuit != nil {
			m.onQuit()
		}
		return m, tea.Quit

	case ConfirmUnlock:
		if db := m.selectedDatabase(); db != nil && db.IsLocked {
			m.handler.ForceUnlock(db.ConnString)
		}
		return m, nil

	case ConfirmRestart:
		if m.onRestart != nil {
			progressChan := m.onRestart()
			m.StartRestart(progressChan)
			// Start loading screen ticks and progress listener
			return m, tea.Batch(
				m.loadingTick(),
				m.loadingProgressTick(),
				m.waitForLoadingProgress(),
			)
		}
		return m, nil
	}

	return m, nil
}

// Run starts the TUI application
func Run(m *Model) error {
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
