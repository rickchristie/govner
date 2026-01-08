package main

// Phase represents the current execution phase in two-phase mode
type Phase int

const (
	// PhaseLegacy indicates legacy single-phase mode (go test -json directly)
	PhaseLegacy Phase = iota
	// PhaseDiscovery indicates package discovery is in progress
	PhaseDiscovery
	// PhaseBuild indicates parallel build is in progress
	PhaseBuild
	// PhaseTest indicates sequential test execution is in progress
	PhaseTest
	// PhaseDone indicates all phases are complete
	PhaseDone
)

// PackagesDiscoveredMsg is sent after go list completes package discovery
type PackagesDiscoveredMsg struct {
	Packages []string // List of packages with tests, sorted alphabetically
	Err      error    // Error if discovery failed
}

// BuildProgressMsg is sent per-package during the build phase
type BuildProgressMsg struct {
	Package   string // Package that was built
	Completed int    // Number of packages completed so far
	Total     int    // Total number of packages to build
	Err       error  // Error if build failed (nil on success)
	Stderr    string // Stderr output (contains build errors)
}

// BuildCompleteMsg is sent when all builds have finished
type BuildCompleteMsg struct {
	Binaries map[string]string // pkg -> binary path for successful builds
	Errors   []BuildError      // List of build failures
}

// BuildError represents a single build failure
type BuildError struct {
	Package string // Package that failed to build
	Stderr  string // Build error output
}
