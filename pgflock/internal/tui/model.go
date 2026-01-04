package tui

import (
	"fmt"
	"sort"

	"github.com/rickchristie/govner/pgflock/internal/config"
	"github.com/rickchristie/govner/pgflock/internal/locker"
)

// ConfirmAction represents an action that requires confirmation
type ConfirmAction int

const (
	ConfirmNone ConfirmAction = iota
	ConfirmQuit
	ConfirmUnlock
	ConfirmRestart
	ConfirmLockerDied // Modal shown when locker server dies
)

// HealthStatus represents the health of a component
type HealthStatus int

const (
	HealthUnknown HealthStatus = iota
	HealthOK
	HealthDown
)

// ContainerHealth tracks the health of a PostgreSQL container
type ContainerHealth struct {
	Port   int
	Status HealthStatus
}

// DatabaseInfo represents a database in the pool
type DatabaseInfo struct {
	ConnString string
	Port       int
	DBName     string
	IsLocked   bool
	LockInfo   *locker.LockInfo
}

// Model represents the TUI application state
type Model struct {
	// Core state
	cfg       *config.Config
	state     *locker.State
	stateChan <-chan *locker.State
	handler   *locker.Handler

	// UI state
	selectedIdx      int
	scrollOffset     int // For scrolling the content area
	confirm          ConfirmAction
	width            int
	height           int
	err              error
	quitting         bool
	showAllDatabases bool
	allDatabases     []DatabaseInfo

	// Health monitoring
	lockerHealth     HealthStatus
	containerHealth  []ContainerHealth
	lockerErrChan    <-chan error
	lockerDiedError  error // Stores the error when locker dies

	// Health status display (footer)
	healthStatusMsg string     // Current status message to display
	sheepState      SheepState // Current sheep animation state
	sheepFrame      int        // Animation frame index

	// Animation state
	lockedAnimator *LockedAnimator
	copyShimmer    *CopyShimmer

	// Progress bars
	lockTimeoutBar *ProgressBar // For showing lock timeout progress

	// Loading screen (reusable for startup/shutdown)
	loadingScreen      *LoadingScreen
	loadingProgressBar *ProgressBar
	showingLoading     bool

	// Loading progress channel (received from main.go)
	loadingProgressChan <-chan LoadingProgress

	// Callbacks for actions
	onRestart  func() <-chan LoadingProgress   // Called for restart with loading screen
	onQuit     func()                          // Called for immediate quit (startup cancel)
	onShutdown func() <-chan LoadingProgress   // Called for graceful shutdown with loading screen

	// HTTP API restart handling
	restartRequestChan     <-chan locker.RestartRequest // Channel for restart requests from HTTP API
	pendingRestartResponse chan error                   // Response channel for current restart request
}

// NewModel creates a new TUI model for startup mode.
// During startup, handler and stateChan may be nil until startup completes.
func NewModel(cfg *config.Config, loadingProgressChan <-chan LoadingProgress) *Model {
	// Build list of all databases
	var allDbs []DatabaseInfo
	for _, port := range cfg.InstancePorts() {
		for i := 1; i <= cfg.DatabasesPerInstance; i++ {
			connStr := fmt.Sprintf("postgresql://%s:%s@localhost:%d/%s%d",
				cfg.PGUsername, cfg.Password, port, cfg.DatabasePrefix, i)
			allDbs = append(allDbs, DatabaseInfo{
				ConnString: connStr,
				Port:       port,
				DBName:     fmt.Sprintf("%s%d", cfg.DatabasePrefix, i),
			})
		}
	}
	// Sort by port then by dbname
	sort.Slice(allDbs, func(i, j int) bool {
		if allDbs[i].Port != allDbs[j].Port {
			return allDbs[i].Port < allDbs[j].Port
		}
		return allDbs[i].DBName < allDbs[j].DBName
	})

	// Collect instance ports for startup animation
	instancePorts := cfg.InstancePorts()

	// Initialize container health tracking
	containerHealth := make([]ContainerHealth, len(instancePorts))
	for i, port := range instancePorts {
		containerHealth[i] = ContainerHealth{Port: port, Status: HealthUnknown}
	}

	return &Model{
		cfg:                 cfg,
		handler:             nil, // Set later via SetHandler
		stateChan:           nil, // Set later via SetStateChan
		state:               nil,
		selectedIdx:         0,
		confirm:             ConfirmNone,
		allDatabases:        allDbs,
		lockerHealth:        HealthUnknown,
		containerHealth:     containerHealth,
		lockedAnimator:      NewLockedAnimator(),
		copyShimmer:         NewCopyShimmer(),
		lockTimeoutBar:      NewProgressBar(WithWidth(10), WithColors(ColorAmber, ColorBorder)),
		loadingScreen:       NewLoadingScreen(LoadingModeStartup, instancePorts),
		loadingProgressBar:  NewProgressBar(WithWidth(20)),
		showingLoading:      true,
		loadingProgressChan: loadingProgressChan,
	}
}

// SetHandler sets the locker handler after startup completes.
func (m *Model) SetHandler(handler *locker.Handler) {
	m.handler = handler
	if handler != nil {
		m.state = handler.GetState()
		m.updateAllDatabasesLockStatus()
	}
}

// SetStateChan sets the state update channel after startup completes.
func (m *Model) SetStateChan(stateChan <-chan *locker.State) {
	m.stateChan = stateChan
}

// SetOnRestart sets the callback for restart action.
// The callback should start the restart process and return a progress channel.
func (m *Model) SetOnRestart(fn func() <-chan LoadingProgress) {
	m.onRestart = fn
}

// SetOnQuit sets the callback for immediate quit (startup cancel)
func (m *Model) SetOnQuit(fn func()) {
	m.onQuit = fn
}

// SetOnShutdown sets the callback for graceful shutdown with loading screen.
// The callback should start the shutdown process and return a progress channel.
func (m *Model) SetOnShutdown(fn func() <-chan LoadingProgress) {
	m.onShutdown = fn
}

// SetLockerErrChan sets the channel for locker server errors.
func (m *Model) SetLockerErrChan(errChan <-chan error) {
	m.lockerErrChan = errChan
	m.lockerHealth = HealthOK // Assume healthy when set
}

// SetRestartRequestChan sets the channel for restart requests from HTTP API.
func (m *Model) SetRestartRequestChan(ch <-chan locker.RestartRequest) {
	m.restartRequestChan = ch
}

// SetContainerHealthy marks a container as healthy.
func (m *Model) SetContainerHealthy(port int) {
	for i := range m.containerHealth {
		if m.containerHealth[i].Port == port {
			m.containerHealth[i].Status = HealthOK
			return
		}
	}
}

// SetAllContainersHealthy marks all containers as healthy.
func (m *Model) SetAllContainersHealthy() {
	for i := range m.containerHealth {
		m.containerHealth[i].Status = HealthOK
	}
}

// healthyContainerCount returns the number of healthy containers.
func (m *Model) healthyContainerCount() int {
	count := 0
	for _, c := range m.containerHealth {
		if c.Status == HealthOK {
			count++
		}
	}
	return count
}

// totalContainerCount returns the total number of containers.
func (m *Model) totalContainerCount() int {
	return len(m.containerHealth)
}

// StartShutdown transitions to the shutdown loading screen.
func (m *Model) StartShutdown(progressChan <-chan LoadingProgress) {
	m.loadingScreen = NewLoadingScreen(LoadingModeShutdown, m.cfg.InstancePorts())
	m.loadingProgressChan = progressChan
	m.showingLoading = true
}

// StartRestart transitions to the restart loading screen.
func (m *Model) StartRestart(progressChan <-chan LoadingProgress) {
	m.loadingScreen = NewLoadingScreen(LoadingModeRestart, m.cfg.InstancePorts())
	m.loadingProgressChan = progressChan
	m.showingLoading = true
}

// selectedLock returns the currently selected lock, or nil if none (locked view only)
func (m *Model) selectedLock() *locker.LockInfo {
	if m.state == nil || len(m.state.Locks) == 0 {
		return nil
	}
	if m.selectedIdx < 0 || m.selectedIdx >= len(m.state.Locks) {
		return nil
	}
	return &m.state.Locks[m.selectedIdx]
}

// selectedDatabase returns the currently selected database info
func (m *Model) selectedDatabase() *DatabaseInfo {
	if m.showAllDatabases {
		if m.selectedIdx < 0 || m.selectedIdx >= len(m.allDatabases) {
			return nil
		}
		return &m.allDatabases[m.selectedIdx]
	}
	// In locked view, get from locks
	if m.state == nil || len(m.state.Locks) == 0 {
		return nil
	}
	if m.selectedIdx < 0 || m.selectedIdx >= len(m.state.Locks) {
		return nil
	}
	lock := &m.state.Locks[m.selectedIdx]
	return &DatabaseInfo{
		ConnString: lock.ConnString,
		IsLocked:   true,
		LockInfo:   lock,
	}
}

// updateAllDatabasesLockStatus updates the lock status of all databases
func (m *Model) updateAllDatabasesLockStatus() {
	if m.state == nil {
		return
	}
	// Build a map of locked databases
	lockMap := make(map[string]*locker.LockInfo)
	for i := range m.state.Locks {
		lockMap[m.state.Locks[i].ConnString] = &m.state.Locks[i]
	}
	// Update allDatabases
	for i := range m.allDatabases {
		if lock, ok := lockMap[m.allDatabases[i].ConnString]; ok {
			m.allDatabases[i].IsLocked = true
			m.allDatabases[i].LockInfo = lock
		} else {
			m.allDatabases[i].IsLocked = false
			m.allDatabases[i].LockInfo = nil
		}
	}
}

// getMaxSelectionIndex returns the max valid selection index based on current view
func (m *Model) getMaxSelectionIndex() int {
	if m.showAllDatabases {
		return len(m.allDatabases) - 1
	}
	if m.state == nil {
		return 0
	}
	return len(m.state.Locks) - 1
}

// getCurrentListSize returns the number of items in the current view
func (m *Model) getCurrentListSize() int {
	if m.showAllDatabases {
		return len(m.allDatabases)
	}
	if m.state == nil {
		return 0
	}
	return len(m.state.Locks)
}

// adjustScrollOffset ensures scrollOffset is valid for the given content size.
// This should be called from Update() whenever content changes.
func (m *Model) adjustScrollOffset(totalItems int) {
	// Calculate visible height based on current terminal size
	height := m.height
	if height <= 0 {
		height = 24
	}
	visibleHeight := height - 4 // header (2) + footer (2)
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	// If all content fits, no scrolling needed
	if totalItems <= visibleHeight {
		m.scrollOffset = 0
		return
	}

	// Ensure selected item is visible
	if m.selectedIdx < m.scrollOffset {
		m.scrollOffset = m.selectedIdx
	}
	if m.selectedIdx >= m.scrollOffset+visibleHeight {
		m.scrollOffset = m.selectedIdx - visibleHeight + 1
	}

	// Clamp scroll offset to valid range
	maxOffset := totalItems - visibleHeight
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

// lockedCount returns the number of locked databases
func (m *Model) lockedCount() int {
	if m.state == nil {
		return 0
	}
	return m.state.LockedDatabases
}

// totalCount returns the total number of databases
func (m *Model) totalCount() int {
	if m.state == nil {
		return 0
	}
	return m.state.TotalDatabases
}

// freeCount returns the number of free databases
func (m *Model) freeCount() int {
	if m.state == nil {
		return 0
	}
	return m.state.FreeDatabases
}

// waitingCount returns the number of waiting requests
func (m *Model) waitingCount() int {
	if m.state == nil {
		return 0
	}
	return m.state.WaitingRequests
}

// instanceCount returns the number of PostgreSQL instances
func (m *Model) instanceCount() int {
	return m.cfg.InstanceCount
}
