package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os/exec"
	"strings"

	model "github.com/rickchristie/govner/gowt/model"
)

// TestRunner abstracts test execution for testability.
// Implementations can run real go test commands or provide mock events.
type TestRunner interface {
	// Start runs go test with the given args and returns an EventStream
	Start(args []string) (EventStream, error)
	// StartSingle runs go test for a specific package and optional test name
	StartSingle(pkg, testName string) (EventStream, error)
	// CleanCache runs go clean -testcache
	CleanCache() error
}

// EventStream provides channels for receiving test events.
// The caller should read from all channels until Done() receives a value.
type EventStream interface {
	// Events returns channel of parsed test events
	Events() <-chan model.TestEvent
	// Stderr returns channel of stderr lines
	Stderr() <-chan string
	// Done returns channel that receives result when tests complete
	Done() <-chan TestResult
	// Kill terminates the test process
	Kill() error
}

// TestResult contains the outcome of a test run
type TestResult struct {
	Err      error
	ExitCode int
}

// RealTestRunner implements TestRunner using exec.Command
type RealTestRunner struct{}

// NewRealTestRunner creates a new RealTestRunner
func NewRealTestRunner() *RealTestRunner {
	return &RealTestRunner{}
}

// Start implements TestRunner.Start
func (r *RealTestRunner) Start(args []string) (EventStream, error) {
	cmdArgs := append([]string{"test", "-json"}, args...)
	return r.startCommand(cmdArgs)
}

// StartSingle implements TestRunner.StartSingle
func (r *RealTestRunner) StartSingle(pkg, testName string) (EventStream, error) {
	var cmdArgs []string
	if testName == "" {
		// No specific test - run all tests in package
		cmdArgs = []string{"test", "-json", pkg}
	} else {
		// Build -run pattern: for "TestFoo/subtest" use "^TestFoo$/^subtest$"
		runPattern := buildRunPattern(testName)
		cmdArgs = []string{"test", "-json", pkg, "-run", runPattern}
	}
	return r.startCommand(cmdArgs)
}

// CleanCache implements TestRunner.CleanCache
func (r *RealTestRunner) CleanCache() error {
	cmd := exec.Command("go", "clean", "-testcache")
	return cmd.Run()
}

// startCommand creates and starts a command, returning an EventStream
func (r *RealTestRunner) startCommand(args []string) (EventStream, error) {
	cmd := exec.Command("go", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdout.Close()
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		stdout.Close()
		stderr.Close()
		return nil, err
	}

	stream := &realEventStream{
		cmd:    cmd,
		stdout: stdout,
		stderr: stderr,
		events: make(chan model.TestEvent, 1000),
		stderrCh: make(chan string, 1000),
		done:   make(chan TestResult, 1),
	}

	// Start goroutines to read stdout and stderr
	go stream.readEvents()
	go stream.readStderr()

	return stream, nil
}

// realEventStream implements EventStream for real test execution
type realEventStream struct {
	cmd      *exec.Cmd
	stdout   io.ReadCloser
	stderr   io.ReadCloser
	events   chan model.TestEvent
	stderrCh chan string
	done     chan TestResult
}

// Events implements EventStream.Events
func (s *realEventStream) Events() <-chan model.TestEvent {
	return s.events
}

// Stderr implements EventStream.Stderr
func (s *realEventStream) Stderr() <-chan string {
	return s.stderrCh
}

// Done implements EventStream.Done
func (s *realEventStream) Done() <-chan TestResult {
	return s.done
}

// Kill implements EventStream.Kill
func (s *realEventStream) Kill() error {
	if s.cmd != nil && s.cmd.Process != nil {
		err := s.cmd.Process.Kill()
		s.cmd.Wait() // Clean up zombie process
		return err
	}
	return nil
}

// readEvents reads test events from stdout and sends them to the channel
func (s *realEventStream) readEvents() {
	scanner := bufio.NewScanner(s.stdout)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		var event model.TestEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		s.events <- event
	}

	// Wait for command to finish
	err := s.cmd.Wait()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	s.done <- TestResult{Err: nil, ExitCode: exitCode}
}

// readStderr reads stderr output and sends it to the channel
func (s *realEventStream) readStderr() {
	scanner := bufio.NewScanner(s.stderr)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		s.stderrCh <- scanner.Text() + "\n"
	}
}

// buildRunPattern creates a -run regex pattern for a test name
// For "TestFoo" -> "^TestFoo$"
// For "TestFoo/subtest" -> "^TestFoo$/^subtest$"
func buildRunPattern(testName string) string {
	parts := strings.Split(testName, "/")
	for i, part := range parts {
		parts[i] = "^" + part + "$"
	}
	return strings.Join(parts, "/")
}
