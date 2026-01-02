package tui

// LoadingScreenMode determines the type of loading screen.
type LoadingScreenMode int

const (
	LoadingModeStartup LoadingScreenMode = iota
	LoadingModeShutdown
	LoadingModeRestart
)

// LoadingStep represents a step in the loading process.
type LoadingStep int

const (
	StepInit LoadingStep = iota
	StepStoppingContainers
	StepStartingContainers
	StepWaitingPostgres
	StepStartingLocker
	StepReady
	StepFailed
)

// LoadingProgress represents a progress update.
type LoadingProgress struct {
	Step    LoadingStep
	Message string
	Port    int  // For per-instance updates
	Done    bool // Whether this step is complete
	Error   error
}

// InstanceStatus tracks the ready state of each instance.
type InstanceStatus struct {
	Port  int
	Ready bool
}

// LoadingScreen is a reusable loading screen for startup/shutdown processes.
// Features staggered progress animation: display progress animates toward target
// in 10% increments at 200ms intervals, staying at 100% briefly before completing.
type LoadingScreen struct {
	mode LoadingScreenMode

	// Progress tracking
	step            LoadingStep
	targetProgress  float64 // Target progress from actual events
	displayProgress float64 // Animated display progress

	// State
	animFrame     int // For visual animation (sheep dots)
	done          bool
	failed        bool
	errorMsg      string
	instances     []InstanceStatus
	statusMessage string

	// Staggered animation state
	reachedTarget   bool // displayProgress has reached targetProgress
	holdingAt100    bool // Holding at 100% before completing
	holdTicksRemain int  // Ticks remaining at 100%
}

// NewLoadingScreen creates a new loading screen.
func NewLoadingScreen(mode LoadingScreenMode, instancePorts []int) *LoadingScreen {
	instances := make([]InstanceStatus, len(instancePorts))
	for i, port := range instancePorts {
		instances[i] = InstanceStatus{Port: port, Ready: false}
	}
	return &LoadingScreen{
		mode:            mode,
		step:            StepInit,
		targetProgress:  0.0,
		displayProgress: 0.0,
		animFrame:       0,
		done:            false,
		failed:          false,
		instances:       instances,
		statusMessage:   "",
	}
}

// Mode returns the loading screen mode.
func (s *LoadingScreen) Mode() LoadingScreenMode {
	return s.mode
}

// Tick advances the visual animation frame (for sheep dots).
func (s *LoadingScreen) Tick() {
	s.animFrame = (s.animFrame + 1) % 4
}

// TickProgress advances the staggered progress animation.
// Should be called every 200ms. Returns true if animation state changed.
func (s *LoadingScreen) TickProgress() bool {
	if s.done || s.failed {
		return false
	}

	// If holding at 100%, decrement hold counter
	if s.holdingAt100 {
		s.holdTicksRemain--
		if s.holdTicksRemain <= 0 {
			s.done = true
			return true
		}
		return false
	}

	// Animate display progress toward target in 20% increments
	if s.displayProgress < s.targetProgress {
		s.displayProgress += 0.2
		if s.displayProgress > s.targetProgress {
			s.displayProgress = s.targetProgress
		}
		// Clamp to 1.0
		if s.displayProgress > 1.0 {
			s.displayProgress = 1.0
		}

		// Check if we've reached 100% and should hold
		if s.displayProgress >= 1.0 && s.targetProgress >= 1.0 {
			s.holdingAt100 = true
			s.holdTicksRemain = 20 // Hold for 20 ticks (1s at 50ms/tick)
		}
		return true
	}

	return false
}

// UpdateProgress updates the loading screen with a progress event.
func (s *LoadingScreen) UpdateProgress(p LoadingProgress) {
	if p.Error != nil {
		s.failed = true
		s.errorMsg = p.Error.Error()
		s.step = StepFailed
		return
	}

	s.step = p.Step
	s.statusMessage = p.Message

	// Handle per-instance updates (startup mode)
	if p.Port > 0 && p.Done {
		s.MarkInstanceReady(p.Port)
	}

	// Update target progress based on step
	s.targetProgress = s.calculateTargetProgress()
}

// calculateTargetProgress calculates target progress based on current step.
func (s *LoadingScreen) calculateTargetProgress() float64 {
	switch s.step {
	case StepInit:
		return 0.0
	case StepStoppingContainers:
		return 0.1
	case StepStartingContainers:
		return 0.3
	case StepWaitingPostgres:
		// Progress based on ready instances
		readyCount := 0
		for _, inst := range s.instances {
			if inst.Ready {
				readyCount++
			}
		}
		if len(s.instances) == 0 {
			return 0.5
		}
		return 0.3 + 0.5*float64(readyCount)/float64(len(s.instances))
	case StepStartingLocker:
		return 0.9
	case StepReady:
		return 1.0
	default:
		return 0.0
	}
}

// Progress returns the display progress (0.0 to 1.0) for rendering.
func (s *LoadingScreen) Progress() float64 {
	return s.displayProgress
}

// TargetProgress returns the actual target progress.
func (s *LoadingScreen) TargetProgress() float64 {
	return s.targetProgress
}

// IsDone returns whether the loading screen has completed.
func (s *LoadingScreen) IsDone() bool {
	return s.done
}

// IsFailed returns whether the process failed.
func (s *LoadingScreen) IsFailed() bool {
	return s.failed
}

// ErrorMessage returns the error message if failed.
func (s *LoadingScreen) ErrorMessage() string {
	return s.errorMsg
}

// Frame returns the current animation frame.
func (s *LoadingScreen) Frame() int {
	return s.animFrame
}

// Step returns the current step.
func (s *LoadingScreen) Step() LoadingStep {
	return s.step
}

// MarkInstanceReady marks an instance as ready.
func (s *LoadingScreen) MarkInstanceReady(port int) {
	for i := range s.instances {
		if s.instances[i].Port == port {
			s.instances[i].Ready = true
		}
	}
	// Recalculate target progress
	s.targetProgress = s.calculateTargetProgress()
}

// AllInstancesReady returns true if all instances are ready.
func (s *LoadingScreen) AllInstancesReady() bool {
	for _, inst := range s.instances {
		if !inst.Ready {
			return false
		}
	}
	return true
}

// GetInstances returns the instance statuses.
func (s *LoadingScreen) GetInstances() []InstanceStatus {
	return s.instances
}

// StatusMessage returns the current status message.
// Returns empty string when step is Ready but progress bar hasn't caught up yet.
func (s *LoadingScreen) StatusMessage() string {
	// Don't show "Ready!" until progress bar has reached 100%
	if s.step == StepReady && s.displayProgress < 1.0 {
		return ""
	}
	return s.statusMessage
}

// SheepDisplay returns the sheep display based on current state.
func (s *LoadingScreen) SheepDisplay() string {
	if s.failed {
		return SheepEmoji + " !"
	}
	if s.done || s.holdingAt100 {
		// Use sleeping emoji for shutdown, sparkles for startup
		if s.mode == LoadingModeShutdown {
			return SleepingEmoji + " " + SheepEmoji + " " + SleepingEmoji
		}
		return SparklesEmoji + " " + SheepEmoji + " " + SparklesEmoji
	}
	// Animate dots based on frame
	dots := []string{".", ". .", ". . .", ". ."}
	return ". " + SheepEmoji + " " + dots[s.animFrame%len(dots)]
}

// TitleDisplay returns the title display.
func (s *LoadingScreen) TitleDisplay() string {
	return "p g f l o c k"
}

// SubtitleDisplay returns the subtitle based on current state and mode.
func (s *LoadingScreen) SubtitleDisplay() string {
	if s.failed {
		switch s.mode {
		case LoadingModeShutdown:
			return "shutdown failed"
		case LoadingModeRestart:
			return "restart failed"
		default:
			return "startup failed"
		}
	}
	if s.done || s.holdingAt100 {
		if s.mode == LoadingModeShutdown {
			return "flock resting safely"
		}
		return "ready to serve"
	}
	switch s.mode {
	case LoadingModeShutdown:
		return "tucking in the flock..."
	case LoadingModeRestart:
		return "waking up the flock..."
	default:
		return "gathering the flock..."
	}
}

// ShowInstances returns whether to show instance status (startup and restart).
func (s *LoadingScreen) ShowInstances() bool {
	return s.mode == LoadingModeStartup || s.mode == LoadingModeRestart
}
