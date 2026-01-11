package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"

	model "github.com/rickchristie/govner/gowt/model"
)

// TwoPhaseRunner implements two-phase test execution:
// Phase 1: Build all test binaries in parallel
// Phase 2: Run pre-compiled binaries sequentially (alphabetically)
type TwoPhaseRunner struct {
	patterns    []string          // Package patterns (e.g., "./...")
	tempDir     string            // Directory for compiled binaries
	workDir     string            // Working directory
	packages    []string          // Discovered packages (sorted)
	binaries    map[string]string // pkg -> binary path
	parallelism int               // Max concurrent builds
	buildFlags  []string          // Flags for go test -c (e.g., -race)
	testFlags   []string          // Flags for test binary (e.g., -test.v)

	ctx    context.Context
	cancel context.CancelFunc

	// Mutex for binaries map access
	mu sync.RWMutex

	// Current running process for Kill()
	currentCmd *exec.Cmd
	cmdMu      sync.Mutex
}

// NewTwoPhaseRunner creates a new TwoPhaseRunner for the given package patterns
func NewTwoPhaseRunner(patterns, buildFlags, testFlags []string) (*TwoPhaseRunner, error) {
	ctx, cancel := context.WithCancel(context.Background())

	workDir, err := os.Getwd()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	r := &TwoPhaseRunner{
		patterns:    patterns,
		workDir:     workDir,
		binaries:    make(map[string]string),
		parallelism: runtime.NumCPU(),
		buildFlags:  buildFlags,
		testFlags:   testFlags,
		ctx:         ctx,
		cancel:      cancel,
	}

	if err := r.initTempDir(); err != nil {
		cancel()
		return nil, err
	}

	return r, nil
}

// initTempDir creates the temp directory for compiled binaries
func (r *TwoPhaseRunner) initTempDir() error {
	// Create unique temp dir based on working directory hash
	hash := sha256.Sum256([]byte(r.workDir))
	r.tempDir = filepath.Join(os.TempDir(), fmt.Sprintf("gowt-%x", hash[:8]))
	return os.MkdirAll(r.tempDir, 0755)
}

// CleanTempDir removes the temp directory and all compiled binaries
func (r *TwoPhaseRunner) CleanTempDir() error {
	if r.tempDir == "" {
		return nil
	}
	return os.RemoveAll(r.tempDir)
}

// binaryPath returns the path where a package's test binary should be stored
func (r *TwoPhaseRunner) binaryPath(pkg string) string {
	// Sanitize package path for filesystem
	safe := strings.ReplaceAll(pkg, "/", "_")
	safe = strings.ReplaceAll(safe, ".", "_")
	return filepath.Join(r.tempDir, safe+".test")
}

// DiscoverPackages finds all packages with tests matching the patterns
func (r *TwoPhaseRunner) DiscoverPackages() ([]string, error) {
	// Build go list command
	args := append([]string{"list", "-f",
		"{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}"},
		r.patterns...)

	cmd := exec.CommandContext(r.ctx, "go", args...)
	cmd.Dir = r.workDir

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("go list failed: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("go list failed: %w", err)
	}

	// Parse output (one package per line, skip empty lines)
	var packages []string
	for _, line := range strings.Split(string(output), "\n") {
		pkg := strings.TrimSpace(line)
		if pkg != "" {
			packages = append(packages, pkg)
		}
	}

	// Sort alphabetically for consistent ordering
	sort.Strings(packages)
	r.packages = packages

	return packages, nil
}

// Build compiles test binaries for all packages in parallel
// Returns a channel that receives progress updates
func (r *TwoPhaseRunner) Build(packages []string) <-chan BuildProgressMsg {
	results := make(chan BuildProgressMsg, len(packages))

	go func() {
		defer close(results)

		if len(packages) == 0 {
			return
		}

		sem := make(chan struct{}, r.parallelism)
		var wg sync.WaitGroup
		var completed int
		var completedMu sync.Mutex

		for _, pkg := range packages {
			wg.Add(1)
			go func(pkg string) {
				defer wg.Done()

				// Check for cancellation before acquiring semaphore
				select {
				case <-r.ctx.Done():
					return
				case sem <- struct{}{}:
					defer func() { <-sem }()
				}

				// Check for cancellation after acquiring semaphore
				select {
				case <-r.ctx.Done():
					return
				default:
				}

				// Build: go test -c -o <path> <pkg>
				binaryPath := r.binaryPath(pkg)
				args := []string{"test", "-c", "-o", binaryPath}
				args = append(args, r.buildFlags...)
				args = append(args, pkg)

				cmd := exec.CommandContext(r.ctx, "go", args...)
				cmd.Dir = r.workDir

				stderr, err := cmd.CombinedOutput()

				completedMu.Lock()
				completed++
				current := completed
				completedMu.Unlock()

				msg := BuildProgressMsg{
					Package:   pkg,
					Completed: current,
					Total:     len(packages),
				}

				if err != nil {
					msg.Err = err
					msg.Stderr = string(stderr)
				} else {
					// Store successful binary path
					r.mu.Lock()
					r.binaries[pkg] = binaryPath
					r.mu.Unlock()
				}

				// Send progress (non-blocking with select for cancellation)
				select {
				case results <- msg:
				case <-r.ctx.Done():
				}
			}(pkg)
		}

		wg.Wait()
	}()

	return results
}

// GetBinaries returns a copy of the binaries map
func (r *TwoPhaseRunner) GetBinaries() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	binaries := make(map[string]string, len(r.binaries))
	for k, v := range r.binaries {
		binaries[k] = v
	}
	return binaries
}

// Run executes pre-built test binaries sequentially
// Returns an EventStream compatible with the existing App
func (r *TwoPhaseRunner) Run(packages []string, binaries map[string]string) EventStream {
	stream := &sequentialEventStream{
		events:   make(chan model.TestEvent, 1000),
		stderr:   make(chan string, 1000),
		done:     make(chan TestResult, 1),
		packages: packages,
		binaries: binaries,
		runner:   r,
		ctx:      r.ctx,
	}

	go stream.run()
	return stream
}

// Kill terminates any running processes
func (r *TwoPhaseRunner) Kill() error {
	r.cancel()

	r.cmdMu.Lock()
	cmd := r.currentCmd
	r.cmdMu.Unlock()

	if cmd != nil && cmd.Process != nil {
		// Kill the entire process group
		pgid, err := syscall.Getpgid(cmd.Process.Pid)
		if err == nil {
			syscall.Kill(-pgid, syscall.SIGKILL)
		} else {
			cmd.Process.Kill()
		}
		cmd.Wait()
	}

	return nil
}

// Reset prepares the runner for a new run
func (r *TwoPhaseRunner) Reset() {
	r.mu.Lock()
	r.binaries = make(map[string]string)
	r.packages = nil
	r.mu.Unlock()

	// Create new context
	r.ctx, r.cancel = context.WithCancel(context.Background())
}

// sequentialEventStream implements EventStream for sequential test execution
type sequentialEventStream struct {
	events   chan model.TestEvent
	stderr   chan string
	done     chan TestResult
	packages []string
	binaries map[string]string
	runner   *TwoPhaseRunner
	ctx      context.Context

	// Track current running command for kill
	currentCmd *exec.Cmd
	cmdMu      sync.Mutex
}

func (s *sequentialEventStream) Events() <-chan model.TestEvent {
	return s.events
}

func (s *sequentialEventStream) Stderr() <-chan string {
	return s.stderr
}

func (s *sequentialEventStream) Done() <-chan TestResult {
	return s.done
}

func (s *sequentialEventStream) Kill() error {
	return s.runner.Kill()
}

func (s *sequentialEventStream) run() {
	defer close(s.events)
	defer close(s.stderr)

	var hasFailure bool

	for _, pkg := range s.packages {
		// Check for cancellation
		select {
		case <-s.ctx.Done():
			s.done <- TestResult{Err: s.ctx.Err(), ExitCode: 1}
			return
		default:
		}

		binaryPath, ok := s.binaries[pkg]
		if !ok {
			// No binary for this package (build failed), skip
			continue
		}

		exitCode := s.runSinglePackage(pkg, binaryPath)
		if exitCode != 0 {
			hasFailure = true
		}
	}

	finalExitCode := 0
	if hasFailure {
		finalExitCode = 1
	}
	s.done <- TestResult{ExitCode: finalExitCode}
}

func (s *sequentialEventStream) runSinglePackage(pkg, binaryPath string) int {
	// Create pipes for the test binary
	testCmd := exec.CommandContext(s.ctx, binaryPath, "-test.v")
	testCmd.Dir = s.runner.workDir
	testCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	testStdout, err := testCmd.StdoutPipe()
	if err != nil {
		s.sendBuildError(pkg, fmt.Sprintf("failed to create stdout pipe: %v", err))
		return 1
	}

	testStderr, err := testCmd.StderrPipe()
	if err != nil {
		testStdout.Close()
		s.sendBuildError(pkg, fmt.Sprintf("failed to create stderr pipe: %v", err))
		return 1
	}

	// Create test2json command to convert output
	jsonCmd := exec.CommandContext(s.ctx, "go", "tool", "test2json", "-p", pkg)
	jsonCmd.Stdin = io.MultiReader(testStdout, testStderr)
	jsonCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	jsonStdout, err := jsonCmd.StdoutPipe()
	if err != nil {
		testStdout.Close()
		testStderr.Close()
		s.sendBuildError(pkg, fmt.Sprintf("failed to create test2json pipe: %v", err))
		return 1
	}

	// Start test binary
	if err := testCmd.Start(); err != nil {
		s.sendBuildError(pkg, fmt.Sprintf("failed to start test: %v", err))
		return 1
	}

	// Store current command for kill
	s.cmdMu.Lock()
	s.currentCmd = testCmd
	s.runner.cmdMu.Lock()
	s.runner.currentCmd = testCmd
	s.runner.cmdMu.Unlock()
	s.cmdMu.Unlock()

	// Start test2json
	if err := jsonCmd.Start(); err != nil {
		testCmd.Process.Kill()
		testCmd.Wait()
		s.sendBuildError(pkg, fmt.Sprintf("failed to start test2json: %v", err))
		return 1
	}

	// Read and forward JSON events
	scanner := bufio.NewScanner(jsonStdout)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		var event model.TestEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}

		select {
		case s.events <- event:
		case <-s.ctx.Done():
			testCmd.Process.Kill()
			jsonCmd.Process.Kill()
			return 1
		}
	}

	// Wait for commands to finish
	testCmd.Wait()
	jsonCmd.Wait()

	// Clear current command
	s.cmdMu.Lock()
	s.currentCmd = nil
	s.runner.cmdMu.Lock()
	s.runner.currentCmd = nil
	s.runner.cmdMu.Unlock()
	s.cmdMu.Unlock()

	exitCode := 0
	if testCmd.ProcessState != nil && !testCmd.ProcessState.Success() {
		exitCode = testCmd.ProcessState.ExitCode()
	}

	return exitCode
}

func (s *sequentialEventStream) sendBuildError(pkg, msg string) {
	s.events <- model.TestEvent{
		Action:  "build-output",
		Package: pkg,
		Output:  msg + "\n",
	}
	s.events <- model.TestEvent{
		Action:  "build-fail",
		Package: pkg,
	}
}

// RunSingleTest runs a specific test from a pre-built binary
func (r *TwoPhaseRunner) RunSingleTest(pkg, binaryPath, testName string) EventStream {
	stream := &singleTestEventStream{
		events:     make(chan model.TestEvent, 1000),
		stderr:     make(chan string, 1000),
		done:       make(chan TestResult, 1),
		pkg:        pkg,
		binaryPath: binaryPath,
		testName:   testName,
		runner:     r,
		ctx:        r.ctx,
	}

	go stream.run()
	return stream
}

// singleTestEventStream implements EventStream for a single test run
type singleTestEventStream struct {
	events     chan model.TestEvent
	stderr     chan string
	done       chan TestResult
	pkg        string
	binaryPath string
	testName   string
	runner     *TwoPhaseRunner
	ctx        context.Context
}

func (s *singleTestEventStream) Events() <-chan model.TestEvent {
	return s.events
}

func (s *singleTestEventStream) Stderr() <-chan string {
	return s.stderr
}

func (s *singleTestEventStream) Done() <-chan TestResult {
	return s.done
}

func (s *singleTestEventStream) Kill() error {
	return s.runner.Kill()
}

func (s *singleTestEventStream) run() {
	defer close(s.events)
	defer close(s.stderr)

	// Build -test.run pattern
	runPattern := buildRunPattern(s.testName)
	args := []string{"-test.v", "-test.run", runPattern}

	testCmd := exec.CommandContext(s.ctx, s.binaryPath, args...)
	testCmd.Dir = s.runner.workDir
	testCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	testStdout, err := testCmd.StdoutPipe()
	if err != nil {
		s.done <- TestResult{Err: err, ExitCode: 1}
		return
	}

	testStderr, err := testCmd.StderrPipe()
	if err != nil {
		testStdout.Close()
		s.done <- TestResult{Err: err, ExitCode: 1}
		return
	}

	// Create test2json command
	jsonCmd := exec.CommandContext(s.ctx, "go", "tool", "test2json", "-p", s.pkg)
	jsonCmd.Stdin = io.MultiReader(testStdout, testStderr)
	jsonCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	jsonStdout, err := jsonCmd.StdoutPipe()
	if err != nil {
		testStdout.Close()
		testStderr.Close()
		s.done <- TestResult{Err: err, ExitCode: 1}
		return
	}

	if err := testCmd.Start(); err != nil {
		s.done <- TestResult{Err: err, ExitCode: 1}
		return
	}

	s.runner.cmdMu.Lock()
	s.runner.currentCmd = testCmd
	s.runner.cmdMu.Unlock()

	if err := jsonCmd.Start(); err != nil {
		testCmd.Process.Kill()
		testCmd.Wait()
		s.done <- TestResult{Err: err, ExitCode: 1}
		return
	}

	// Read events
	scanner := bufio.NewScanner(jsonStdout)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		var event model.TestEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}

		select {
		case s.events <- event:
		case <-s.ctx.Done():
			testCmd.Process.Kill()
			jsonCmd.Process.Kill()
			s.done <- TestResult{Err: s.ctx.Err(), ExitCode: 1}
			return
		}
	}

	testCmd.Wait()
	jsonCmd.Wait()

	s.runner.cmdMu.Lock()
	s.runner.currentCmd = nil
	s.runner.cmdMu.Unlock()

	exitCode := 0
	if testCmd.ProcessState != nil && !testCmd.ProcessState.Success() {
		exitCode = testCmd.ProcessState.ExitCode()
	}

	s.done <- TestResult{ExitCode: exitCode}
}

// CheckDependencies verifies that required tools are available
func CheckTest2JsonAvailable() error {
	cmd := exec.Command("go", "tool", "test2json", "-h")
	if err := cmd.Run(); err != nil {
		// If we get an ExitError, the tool exists but returned non-zero
		// (test2json -h returns exit code 1 or 2 depending on Go version)
		if _, ok := err.(*exec.ExitError); ok {
			return nil // Tool exists, just returned non-zero for -h
		}
		// Other errors (e.g., command not found)
		return fmt.Errorf("go tool test2json not available: %w", err)
	}
	return nil
}
