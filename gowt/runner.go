package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	model "github.com/rickchristie/govner/gowt/model"
)

// TestRunner abstracts test execution for testability.
// Implementations can run real go test commands or provide mock events.
type TestRunner interface {
	// Start runs go test with the given args and returns an EventStream.
	Start(args []string) (EventStream, error)
	// StartSingle reruns a package or test while preserving the original build
	// and test flags. Package operands and selection flags are replaced.
	StartSingle(originalArgs []string, pkg, testName string) (EventStream, error)
	// CleanCache runs go clean -testcache.
	CleanCache() error
}

// EventStream provides channels for receiving test events. Done is delivered
// only after the command exits and all available stdout and stderr have been
// drained. Output that did not fit in the public channels is attached to the
// terminal TestResult. Descriptors retained only by orphaned descendants are
// bounded.
type EventStream interface {
	Events() <-chan model.TestEvent
	Stderr() <-chan string
	Done() <-chan TestResult
	Kill() error
}

// TestResult contains both the go test exit status and operational errors such
// as malformed JSON or a pipe read failure. A normal test failure has a nonzero
// ExitCode and a nil Err.
type TestResult struct {
	Err      error
	ExitCode int
	// PendingEvents and PendingStderr are ordered suffixes that could not be
	// placed on the bounded public channels without delaying completion.
	PendingEvents []model.TestEvent
	PendingStderr []string
}

// RealTestRunner implements TestRunner using exec.Command.
type RealTestRunner struct{}

// Once the go command exits, any bytes it produced are already in these pipes
// and are drained immediately by the readers. The deadline exists only for
// descriptors inherited by orphaned descendants, which otherwise have no
// finite EOF. Unix process groups normally make this path instantaneous; the
// bound also protects platforms without process-group cleanup.
const readerDrainTimeout = time.Second

// This capacity absorbs ordinary TUI scheduling gaps. Correctness does not
// depend on it: streamQueue retains overflow until terminal delivery.
const streamChannelCapacity = 1000

func NewRealTestRunner() *RealTestRunner { return &RealTestRunner{} }

func (r *RealTestRunner) Start(args []string) (EventStream, error) {
	return r.startCommand(buildGoTestCommandArgs(args))
}

func (r *RealTestRunner) StartSingle(originalArgs []string, pkg, testName string) (EventStream, error) {
	return r.startCommand(buildSingleTestArgs(originalArgs, pkg, testName))
}

func (r *RealTestRunner) CleanCache() error {
	return exec.Command("go", "clean", "-testcache").Run()
}

func (r *RealTestRunner) startCommand(args []string) (EventStream, error) {
	cmd := exec.Command("go", args...)
	configureProcessGroup(cmd)

	// Own the pipes instead of using Cmd.StdoutPipe and Cmd.StderrPipe. Those
	// helpers make Cmd.Wait close the read ends as soon as the parent exits,
	// which races readers and can truncate buffered output. Caller-owned pipes
	// let Wait observe process exit first while the readers finish draining.
	stdout, stdoutWriter, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	stderr, stderrWriter, err := os.Pipe()
	if err != nil {
		_ = stdout.Close()
		_ = stdoutWriter.Close()
		return nil, err
	}
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = stdoutWriter.Close()
		_ = stderr.Close()
		_ = stderrWriter.Close()
		return nil, err
	}
	// The child owns duplicated writer descriptors now. Retaining the parent's
	// copies would prevent EOF even when the complete process tree has exited.
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()

	stream := &realEventStream{
		cmd:         cmd,
		stdout:      stdout,
		stderr:      stderr,
		eventQueue:  newStreamQueue[model.TestEvent](streamChannelCapacity),
		stderrQueue: newStreamQueue[string](streamChannelCapacity),
		done:        make(chan TestResult, 1),
		stop:        make(chan struct{}),
		readersDone: make(chan struct{}),
	}
	stream.readers.Add(2)
	go stream.readEvents()
	go stream.readStderr()
	go func() {
		stream.readers.Wait()
		close(stream.readersDone)
	}()
	go stream.wait()
	return stream, nil
}

type realEventStream struct {
	cmd         *exec.Cmd
	stdout      io.ReadCloser
	stderr      io.ReadCloser
	eventQueue  *streamQueue[model.TestEvent]
	stderrQueue *streamQueue[string]
	done        chan TestResult
	stop        chan struct{}
	stopOnce    sync.Once
	readers     sync.WaitGroup

	readersDone      chan struct{}
	closeReadersOnce sync.Once

	errMu    sync.Mutex
	readErrs []error
}

func (s *realEventStream) Events() <-chan model.TestEvent { return s.eventQueue.Output() }
func (s *realEventStream) Stderr() <-chan string          { return s.stderrQueue.Output() }
func (s *realEventStream) Done() <-chan TestResult        { return s.done }

func (s *realEventStream) Kill() error {
	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	s.stopOnce.Do(func() { close(s.stop) })
	return killProcessGroup(s.cmd)
}

func (s *realEventStream) recordReadError(err error) {
	if err == nil {
		return
	}
	s.errMu.Lock()
	s.readErrs = append(s.readErrs, err)
	s.errMu.Unlock()
}

func (s *realEventStream) sendEvent(event model.TestEvent) bool {
	select {
	case <-s.stop:
		return false
	default:
		return s.eventQueue.Enqueue(event)
	}
}

func (s *realEventStream) sendStderr(line string) bool {
	select {
	case <-s.stop:
		return false
	default:
		return s.stderrQueue.Enqueue(line)
	}
}

func (s *realEventStream) readEvents() {
	defer s.readers.Done()
	reader := bufio.NewReader(s.stdout)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			var event model.TestEvent
			if decodeErr := json.Unmarshal([]byte(strings.TrimSuffix(line, "\n")), &event); decodeErr != nil {
				s.recordReadError(fmt.Errorf("decode go test JSON: %w", decodeErr))
				// Keep malformed output visible; otherwise a broken toolchain or
				// wrapper would fail with no diagnostic in the TUI.
				if !s.sendStderr(line) {
					return
				}
			} else if !s.sendEvent(event) {
				return
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
				s.recordReadError(fmt.Errorf("read go test JSON: %w", err))
			}
			return
		}
	}
}

func (s *realEventStream) readStderr() {
	defer s.readers.Done()
	reader := bufio.NewReader(s.stderr)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 && !s.sendStderr(line) {
			return
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
				s.recordReadError(fmt.Errorf("read go test stderr: %w", err))
			}
			return
		}
	}
}

func (s *realEventStream) closeReaders() {
	s.closeReadersOnce.Do(func() {
		if s.stdout != nil {
			_ = s.stdout.Close()
		}
		if s.stderr != nil {
			_ = s.stderr.Close()
		}
	})
}

// wait is the sole owner of Cmd.Wait. The command is observed before waiting
// for EOF because descendants can inherit an output descriptor indefinitely.
// The pipes are caller-owned, so Wait cannot race the readers by closing them.
func (s *realEventStream) wait() {
	waitErr := s.cmd.Wait()
	cleanupErr := cleanupProcessGroupAfterExit(s.cmd)

	timer := time.NewTimer(readerDrainTimeout)
	select {
	case <-s.readersDone:
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	case <-timer.C:
		// At this point the parent exited a full drain interval ago. A reader
		// still waiting is held by an inherited descriptor, not parent output.
		s.closeReaders()
		<-s.readersDone
	}
	s.closeReaders()

	exitCode := 0
	var operationalErr error
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
			operationalErr = fmt.Errorf("wait for go test: %w", waitErr)
		}
	}

	s.errMu.Lock()
	readErr := errors.Join(s.readErrs...)
	s.errMu.Unlock()
	if cleanupErr != nil {
		cleanupErr = fmt.Errorf("clean up test process group: %w", cleanupErr)
	}
	operationalErr = errors.Join(operationalErr, cleanupErr, readErr)

	// Finalization never waits for the TUI to consume the bounded public
	// channels. Their undelivered suffixes travel with Done, preserving every
	// record while keeping process completion independent of presentation.
	pendingEvents := s.eventQueue.Finalize()
	pendingStderr := s.stderrQueue.Finalize()
	s.done <- TestResult{
		Err:           operationalErr,
		ExitCode:      exitCode,
		PendingEvents: pendingEvents,
		PendingStderr: pendingStderr,
	}
	close(s.done)
}

// buildRunPattern quotes each slash-delimited component because go test gives
// every component to regexp.MatchString independently.
func buildRunPattern(testName string) string {
	parts := strings.Split(testName, "/")
	for i, part := range parts {
		parts[i] = "^" + regexp.QuoteMeta(part) + "$"
	}
	return strings.Join(parts, "/")
}

var selectionFlags = map[string]bool{
	"-bench": true, "-fuzz": true, "-list": true, "-run": true,
	"-test.bench": true, "-test.fuzz": true, "-test.list": true, "-test.run": true,
}

// Flags that consume the following argument when they do not use -flag=value.
// Arguments after -args bypass this table and are preserved verbatim except
// for the selection flags that a single-test rerun must replace.
var goTestValueFlags = map[string]bool{
	"-C": true, "-asmflags": true, "-bench": true, "-benchtime": true,
	"-blockprofile": true, "-blockprofilerate": true, "-buildmode": true,
	"-compiler": true, "-covermode": true, "-coverpkg": true,
	"-coverprofile": true, "-count": true, "-cpu": true,
	"-cpuprofile": true, "-exec": true, "-fuzz": true,
	"-fuzzminimizetime": true, "-fuzztime": true, "-gccgoflags": true,
	"-gcflags": true, "-installsuffix": true, "-ldflags": true,
	"-list": true, "-memprofile": true, "-memprofilerate": true,
	"-mod": true, "-modfile": true, "-mutexprofile": true,
	"-mutexprofilefraction": true, "-o": true, "-outputdir": true,
	"-overlay": true, "-p": true, "-parallel": true, "-pgo": true,
	"-pkgdir": true, "-run": true, "-shuffle": true, "-skip": true,
	"-tags": true, "-timeout": true, "-toolexec": true, "-trace": true,
	"-vet": true,

	// AddBuildFlags registers these unstable options for go test even though
	// they are omitted from the help text. Their artifact paths are values, not
	// package operands.
	"-debug-actiongraph": true, "-debug-runtime-trace": true,
	"-debug-trace": true,

	// The go command also accepts test-binary-prefixed spellings. Preserve
	// their split values when users pass them before -args.
	"-test.bench": true, "-test.benchtime": true,
	"-test.blockprofile": true, "-test.blockprofilerate": true,
	"-test.count": true, "-test.coverprofile": true, "-test.cpu": true,
	"-test.cpuprofile": true, "-test.fuzz": true,
	"-test.fuzzcachedir": true, "-test.fuzzminimizetime": true,
	"-test.fuzztime": true, "-test.gocoverdir": true, "-test.list": true,
	"-test.memprofile":     true,
	"-test.memprofilerate": true, "-test.mutexprofile": true,
	"-test.mutexprofilefraction": true, "-test.outputdir": true,
	"-test.parallel": true, "-test.run": true, "-test.shuffle": true,
	"-test.skip": true, "-test.testlogfile": true, "-test.timeout": true,
	"-test.trace": true,
}

func flagName(arg string) string {
	name := arg
	if idx := strings.IndexByte(arg, '='); idx >= 0 {
		name = arg[:idx]
	}
	// The Go flag parser accepts either one or two leading dashes. Internal
	// lookup tables use the documented single-dash spelling so selection and
	// value semantics remain identical for both forms. A bare "--" is left
	// unchanged.
	if len(name) > 2 && strings.HasPrefix(name, "--") {
		name = name[1:]
	}
	return name
}

// buildGoTestCommandArgs adds the required machine-readable output flag while
// keeping -C first. The go command rejects `go test -json -C dir`, so blindly
// prepending -json breaks an otherwise valid Gowt invocation.
func buildGoTestCommandArgs(args []string) []string {
	cmdArgs := make([]string, 0, len(args)+2)
	cmdArgs = append(cmdArgs, "test")
	if len(args) > 0 && flagName(args[0]) == "-C" {
		cmdArgs = append(cmdArgs, args[0])
		hasInlineValue := strings.Contains(args[0], "=")
		args = args[1:]
		if !hasInlineValue && len(args) > 0 {
			cmdArgs = append(cmdArgs, args[0])
			args = args[1:]
		}
	}
	cmdArgs = append(cmdArgs, "-json")
	return append(cmdArgs, args...)
}

func buildSingleTestArgs(originalArgs []string, pkg, testName string) []string {
	filtered := make([]string, 0, len(originalArgs)+6)
	var binaryArgs []string
	for i := 0; i < len(originalArgs); i++ {
		arg := originalArgs[i]
		if arg == "--" {
			// A bare separator makes every remaining token positional. Keep that
			// tail byte-for-byte, but place the generated selection before it so
			// Go still interprets the rerun flags.
			binaryArgs = append(binaryArgs, originalArgs[i:]...)
			break
		}
		if arg == "-args" || arg == "--args" {
			binaryArgs = append(binaryArgs, arg)
			for i++; i < len(originalArgs); i++ {
				binaryArg := originalArgs[i]
				name := flagName(binaryArg)
				if selectionFlags[name] {
					if !strings.Contains(binaryArg, "=") && i+1 < len(originalArgs) {
						i++
					}
					continue
				}
				binaryArgs = append(binaryArgs, binaryArg)
			}
			break
		}
		if !strings.HasPrefix(arg, "-") {
			// Before -args, positional operands are package patterns. A rerun
			// targets the selected package instead.
			continue
		}

		name := flagName(arg)
		hasInlineValue := strings.Contains(arg, "=")
		if name == "-json" {
			continue
		}
		if selectionFlags[name] {
			if !hasInlineValue && i+1 < len(originalArgs) {
				i++
			}
			continue
		}
		filtered = append(filtered, arg)
		if goTestValueFlags[name] && !hasInlineValue && i+1 < len(originalArgs) {
			i++
			filtered = append(filtered, originalArgs[i])
		}
	}

	cmdArgs := buildGoTestCommandArgs(filtered)
	cmdArgs = append(cmdArgs, pkg)
	if testName == "" {
		return append(cmdArgs, binaryArgs...)
	}
	pattern := buildRunPattern(testName)
	if strings.HasPrefix(testName, "Benchmark") {
		cmdArgs = append(cmdArgs, "-run", "^$", "-bench", pattern)
	} else {
		cmdArgs = append(cmdArgs, "-run", pattern)
	}
	return append(cmdArgs, binaryArgs...)
}
