package main

import (
	"encoding/json"
	"fmt"
	"go/build"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	model "github.com/rickchristie/govner/gowt/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func installFakeGo(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "go")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755))
	t.Setenv("PATH", dir)
	return path
}

func receiveWithTimeout[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for channel value")
		var zero T
		return zero
	}
}

func readArgsFile(t *testing.T, path string) []string {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return strings.Fields(string(content))
}

func TestNewRealTestRunner(t *testing.T) {
	assert.NotNil(t, NewRealTestRunner())
}

func TestRealTestRunnerStartStreamsEventsStderrAndExitCode(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("GOWT_FAKE_ARGS_FILE", argsFile)
	installFakeGo(t, `
printf '%s\n' "$@" > "$GOWT_FAKE_ARGS_FILE"
printf '%s\n' 'not-json'
printf '%s\n' '{"Action":"run","Package":"example.com/pkg","Test":"TestOne"}'
printf '%s\n' 'compiler error' >&2
exit 3
`)

	stream, err := NewRealTestRunner().Start([]string{"-race", "./..."})
	require.NoError(t, err)
	require.NotNil(t, stream)

	event := receiveWithTimeout(t, stream.Events())
	assert.Equal(t, model.TestEvent{
		Action:  "run",
		Package: "example.com/pkg",
		Test:    "TestOne",
	}, event)
	stderrLines := []string{
		receiveWithTimeout(t, stream.Stderr()),
		receiveWithTimeout(t, stream.Stderr()),
	}
	assert.ElementsMatch(t, []string{"not-json\n", "compiler error\n"}, stderrLines)

	result := receiveWithTimeout(t, stream.Done())
	require.Error(t, result.Err)
	assert.Contains(t, result.Err.Error(), "decode go test JSON")
	assert.Equal(t, 3, result.ExitCode)
	assert.Equal(t, []string{"test", "-json", "-race", "./..."}, readArgsFile(t, argsFile))
}

func TestRealTestRunnerStartReportsSuccessfulExit(t *testing.T) {
	installFakeGo(t, `
printf '%s\n' '{"Action":"pass","Package":"example.com/pkg","Elapsed":0.1}'
`)

	stream, err := NewRealTestRunner().Start(nil)
	require.NoError(t, err)
	event := receiveWithTimeout(t, stream.Events())
	assert.Equal(t, "pass", event.Action)
	result := receiveWithTimeout(t, stream.Done())
	assert.NoError(t, result.Err)
	assert.Zero(t, result.ExitCode)
}

func TestRealTestRunnerCompletionIgnoresInheritedStderrAfterDrainingOutput(t *testing.T) {
	installFakeGo(t, `
printf '%s\n' '{"Action":"pass","Package":"example.com/pkg","Elapsed":0.1}'
printf '%s' 'stderr before parent exit' >&2
/usr/bin/nohup /bin/sleep 4 >/dev/null &
exit 0
`)

	start := time.Now()
	stream, err := NewRealTestRunner().Start(nil)
	require.NoError(t, err)
	assert.Equal(t, "pass", receiveWithTimeout(t, stream.Events()).Action)
	assert.Equal(t, "stderr before parent exit", receiveWithTimeout(t, stream.Stderr()))
	result := receiveWithTimeout(t, stream.Done())

	assert.NoError(t, result.Err)
	assert.Zero(t, result.ExitCode)
	assert.Less(t, time.Since(start), 2*time.Second,
		"a descendant holding stderr must not keep Gowt running")
}

func TestRealTestRunnerCompletionIsNotBlockedByOutputBackpressure(t *testing.T) {
	const recordCount = streamChannelCapacity + 100
	installFakeGo(t, fmt.Sprintf(`
i=0
while [ "$i" -lt %d ]; do
  printf '%%s\n' '{"Action":"output","Package":"example.com/pkg","Output":"line\n"}'
  printf '%%s\n' 'diagnostic' >&2
  i=$((i + 1))
done
`, recordCount))

	stream, err := NewRealTestRunner().Start(nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Kill() })

	// Deliberately do not consume Events or Stderr before Done. The runner must
	// still drain the child pipes instead of letting public-channel backpressure
	// prevent the process and stream from completing.
	result := receiveWithTimeout(t, stream.Done())
	assert.NoError(t, result.Err)
	assert.Zero(t, result.ExitCode)

	var events []model.TestEvent
	for event := range stream.Events() {
		events = append(events, event)
	}
	events = append(events, result.PendingEvents...)
	assert.Len(t, events, recordCount)
	assert.NotEmpty(t, result.PendingEvents,
		"overflow must be retained after the public event channel fills")

	var stderrLines []string
	for line := range stream.Stderr() {
		stderrLines = append(stderrLines, line)
	}
	stderrLines = append(stderrLines, result.PendingStderr...)
	assert.Len(t, stderrLines, recordCount)
	assert.NotEmpty(t, result.PendingStderr,
		"overflow must be retained after the public stderr channel fills")
}

func TestRealEventStreamDrainTimeoutPreservesBufferedOutput(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	configureProcessGroup(cmd)
	require.NoError(t, cmd.Start())

	stdout, stdoutWriter, err := os.Pipe()
	require.NoError(t, err)
	defer stdoutWriter.Close()
	stderr, stderrWriter, err := os.Pipe()
	require.NoError(t, err)
	defer stderrWriter.Close()

	stream := &realEventStream{
		cmd:         cmd,
		stdout:      stdout,
		stderr:      stderr,
		eventQueue:  newStreamQueue[model.TestEvent](10),
		stderrQueue: newStreamQueue[string](10),
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
	start := time.Now()
	go stream.wait()

	_, err = fmt.Fprint(stdoutWriter, `{"Action":"pass","Package":"example.com/pkg"}`)
	require.NoError(t, err)
	_, err = fmt.Fprint(stderrWriter, "unterminated diagnostic")
	require.NoError(t, err)

	assert.Equal(t, "pass", receiveWithTimeout(t, stream.Events()).Action)
	result := receiveWithTimeout(t, stream.Done())
	assert.NoError(t, result.Err)
	assert.Zero(t, result.ExitCode)
	assert.Equal(t, "unterminated diagnostic", receiveWithTimeout(t, stream.Stderr()))
	assert.Less(t, time.Since(start), 4*readerDrainTimeout,
		"completion must remain bounded even while a writer stays open")
}

func TestRealTestRunnerStartSingleBuildsExpectedArguments(t *testing.T) {
	tests := []struct {
		name     string
		pkg      string
		testName string
		wantArgs []string
	}{
		{
			name:     "whole package",
			pkg:      "example.com/project/pkg",
			wantArgs: []string{"test", "-json", "example.com/project/pkg"},
		},
		{
			name:     "top-level test",
			pkg:      "example.com/project/pkg",
			testName: "TestOne",
			wantArgs: []string{
				"test", "-json", "example.com/project/pkg", "-run", "^TestOne$",
			},
		},
		{
			name:     "nested subtest",
			pkg:      "example.com/project/pkg",
			testName: "TestOne/child/grandchild",
			wantArgs: []string{
				"test", "-json", "example.com/project/pkg", "-run",
				"^TestOne$/^child$/^grandchild$",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argsFile := filepath.Join(t.TempDir(), "args")
			t.Setenv("GOWT_FAKE_ARGS_FILE", argsFile)
			installFakeGo(t, `printf '%s\n' "$@" > "$GOWT_FAKE_ARGS_FILE"`)

			stream, err := NewRealTestRunner().StartSingle(nil, tt.pkg, tt.testName)
			require.NoError(t, err)
			result := receiveWithTimeout(t, stream.Done())
			assert.Zero(t, result.ExitCode)
			assert.Equal(t, tt.wantArgs, readArgsFile(t, argsFile))
		})
	}
}

func TestRealTestRunnerCleanCache(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("GOWT_FAKE_ARGS_FILE", argsFile)
	installFakeGo(t, `printf '%s\n' "$@" > "$GOWT_FAKE_ARGS_FILE"`)

	require.NoError(t, NewRealTestRunner().CleanCache())
	assert.Equal(t, []string{"clean", "-testcache"}, readArgsFile(t, argsFile))
}

func TestRealTestRunnerCleanCacheReturnsCommandFailure(t *testing.T) {
	installFakeGo(t, `exit 7`)

	err := NewRealTestRunner().CleanCache()
	require.Error(t, err)
}

func TestRealTestRunnerStartReturnsLookupError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	stream, err := NewRealTestRunner().Start(nil)

	assert.Nil(t, stream)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "executable file not found")
}

func TestRealEventStreamKillWithoutProcessIsNoOp(t *testing.T) {
	stream := &realEventStream{}
	assert.NoError(t, stream.Kill())
}

func TestRealEventStreamKillTerminatesProcessGroup(t *testing.T) {
	installFakeGo(t, `exec /bin/sleep 30`)
	stream, err := NewRealTestRunner().Start(nil)
	require.NoError(t, err)

	start := time.Now()
	require.NoError(t, stream.Kill())
	assert.Less(t, time.Since(start), 5*time.Second)

	result := receiveWithTimeout(t, stream.Done())
	assert.NotZero(t, result.ExitCode)
}

func TestBuildRunPattern(t *testing.T) {
	tests := map[string]string{
		"":                            "^$",
		"TestOne":                     "^TestOne$",
		"TestOne/child":               "^TestOne$/^child$",
		"TestOne/child/grandchild":    "^TestOne$/^child$/^grandchild$",
		"TestOne/space_is_normalized": "^TestOne$/^space_is_normalized$",
		"TestOne/a+b.[x](y)":          `^TestOne$/^a\+b\.\[x\]\(y\)$`,
	}

	for input, want := range tests {
		assert.Equal(t, want, buildRunPattern(input), input)
	}
}

func TestBuildSingleTestArgsPreservesOriginalFlags(t *testing.T) {
	got := buildSingleTestArgs([]string{
		"-race", "-tags", "integration", "./...", "-count=2",
		"-run", "OldSelection", "-args", "-custom", "value", "-test.run", "OlderSelection",
	}, "example.com/project/pkg", "TestMeta/a+b")

	assert.Equal(t, []string{
		"test", "-json", "-race", "-tags", "integration", "-count=2",
		"example.com/project/pkg", "-run", `^TestMeta$/^a\+b$`,
		"-args", "-custom", "value",
	}, got)

	benchmark := buildSingleTestArgs(
		[]string{"-shuffle=on", "./pkg"}, "example.com/project/pkg", "BenchmarkLookup/size=10",
	)
	assert.Equal(t, []string{
		"test", "-json", "-shuffle=on", "example.com/project/pkg",
		"-run", "^$", "-bench", `^BenchmarkLookup$/^size=10$`,
	}, benchmark)
}

func TestBuildSingleTestArgsPreservesSplitValuedSkipFlag(t *testing.T) {
	got := buildSingleTestArgs(
		[]string{"-skip", "Slow", "./..."},
		"example.com/project/selected",
		"TestFast",
	)

	assert.Equal(t, []string{
		"test", "-json", "-skip", "Slow", "example.com/project/selected",
		"-run", "^TestFast$",
	}, got)
}

func TestBuildSingleTestArgsPreservesSplitValueFlags(t *testing.T) {
	tests := []struct {
		name  string
		flag  string
		value string
	}{
		{name: "build mode", flag: "-buildmode", value: "pie"},
		{name: "CPU profile", flag: "-cpuprofile", value: "cpu.out"},
		{name: "test binary output", flag: "-o", value: "pkg.test"},
		{name: "compiler", flag: "-compiler", value: "gccgo"},
		{name: "install suffix", flag: "-installsuffix", value: "integration"},
		{name: "debug action graph", flag: "-debug-actiongraph", value: "actions.json"},
		{name: "debug runtime trace", flag: "-debug-runtime-trace", value: "runtime.trace"},
		{name: "debug trace", flag: "-debug-trace", value: "build.trace"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSingleTestArgs(
				[]string{tt.flag, tt.value, "./..."},
				"example.com/project/selected",
				"TestFast",
			)
			assert.Equal(t, []string{
				"test", "-json", tt.flag, tt.value,
				"example.com/project/selected", "-run", "^TestFast$",
			}, got)
		})
	}
}

func TestGoTestCommandArgsKeepsChangeDirectoryFirst(t *testing.T) {
	assert.Equal(t, []string{
		"test", "-C", "../project", "-json", "./...",
	}, buildGoTestCommandArgs([]string{"-C", "../project", "./..."}))
	assert.Equal(t, []string{
		"test", "--C=../project", "-json", "./...",
	}, buildGoTestCommandArgs([]string{"--C=../project", "./..."}))

	assert.Equal(t, []string{
		"test", "-C", "../project", "-json", "example.com/project/selected",
		"-run", "^TestFast$",
	}, buildSingleTestArgs(
		[]string{"-C", "../project", "./..."},
		"example.com/project/selected",
		"TestFast",
	))
}

func TestGoTestCommandArgsCannotDisableRequiredJSON(t *testing.T) {
	assert.Equal(t, []string{
		"test", "-json", "./...",
	}, buildGoTestCommandArgs([]string{"-json=false", "--json", "./..."}))
	assert.Equal(t, []string{
		"test", "-json", "-exec", "-json=false", "./...",
	}, buildGoTestCommandArgs([]string{"-exec", "-json=false", "./...", "--json=false"}))
	assert.Equal(t, []string{
		"test", "-json", "./...", "-args", "-json=false",
	}, buildGoTestCommandArgs([]string{"./...", "-args", "-json=false"}))
}

func TestGoTestValueFlagTableCoversAcceptedSplitFlags(t *testing.T) {
	// Keep this list aligned with `go help build`, `go help test`,
	// `go help testflag`, and the unstable debug flags registered by
	// cmd/go/internal/work.AddBuildFlags. A missing entry silently turns its
	// value into a discarded package operand during selected reruns.
	valueFlags := []string{
		"-C", "-asmflags", "-bench", "-benchtime", "-blockprofile",
		"-blockprofilerate", "-buildmode", "-compiler", "-covermode",
		"-coverpkg", "-coverprofile", "-count", "-cpu", "-cpuprofile",
		"-debug-actiongraph", "-debug-runtime-trace", "-debug-trace",
		"-exec", "-fuzz", "-fuzzminimizetime", "-fuzztime", "-gccgoflags",
		"-gcflags", "-installsuffix", "-ldflags", "-list", "-memprofile",
		"-memprofilerate", "-mod", "-modfile", "-mutexprofile",
		"-mutexprofilefraction", "-o", "-outputdir", "-overlay", "-p",
		"-parallel", "-pgo", "-pkgdir", "-run", "-shuffle", "-skip",
		"-tags", "-timeout", "-toolexec", "-trace", "-vet",
	}
	for _, flag := range valueFlags {
		assert.Truef(t, goTestValueFlags[flag], "%s must consume its split value", flag)
	}

	booleanFlags := []string{
		"-a", "-asan", "-benchmem", "-buildvcs", "-c", "-cover",
		"-failfast", "-fullpath", "-json", "-linkshared", "-modcacherw",
		"-msan", "-n", "-race", "-short", "-trimpath", "-v", "-work", "-x",
	}
	for _, flag := range booleanFlags {
		assert.Falsef(t, goTestValueFlags[flag], "%s must not consume a package operand", flag)
	}
}

func TestBuildSingleTestArgsDoesNotConsumePackageAfterBooleanFlag(t *testing.T) {
	got := buildSingleTestArgs(
		[]string{"-buildvcs", "./..."},
		"example.com/project/selected",
		"TestFast",
	)

	assert.Equal(t, []string{
		"test", "-json", "-buildvcs", "example.com/project/selected",
		"-run", "^TestFast$",
	}, got)
}

func TestBuildSingleTestArgsNormalizesDoubleDashFlags(t *testing.T) {
	got := buildSingleTestArgs(
		[]string{"--json", "--debug-trace", "build.trace", "--run", "OldSelection", "./..."},
		"example.com/project/selected",
		"TestFast",
	)

	assert.Equal(t, []string{
		"test", "-json", "--debug-trace", "build.trace",
		"example.com/project/selected", "-run", "^TestFast$",
	}, got)
	assert.Equal(t, "-run", flagName("--run=OldSelection"))
}

func TestBuildSingleTestArgsRemovesDoubleDashSelectionAfterArgs(t *testing.T) {
	got := buildSingleTestArgs(
		[]string{"./...", "--args", "--test.run", "OldSelection", "-custom", "value"},
		"example.com/project/selected",
		"TestFast",
	)

	assert.Equal(t, []string{
		"test", "-json", "example.com/project/selected", "-run", "^TestFast$",
		"--args", "-custom", "value",
	}, got)
}

func TestBuildSingleTestArgsKeepsBareSeparatorTailAfterSelection(t *testing.T) {
	got := buildSingleTestArgs(
		[]string{"./...", "--", "-custom", "value"},
		"example.com/project/selected",
		"TestFast",
	)

	assert.Equal(t, []string{
		"test", "-json", "example.com/project/selected", "-run", "^TestFast$",
		"--", "-custom", "value",
	}, got)
}

func TestIOSProcessBuildConstraintsAreMutuallyExclusive(t *testing.T) {
	ctx := build.Default
	ctx.GOOS = "ios"
	ctx.GOARCH = "arm64"
	ctx.CgoEnabled = true

	unixMatch, err := ctx.MatchFile(".", "runner_process_unix.go")
	require.NoError(t, err)
	fallbackMatch, err := ctx.MatchFile(".", "runner_process_fallback.go")
	require.NoError(t, err)

	assert.NotEqual(t, unixMatch, fallbackMatch,
		"exactly one process implementation must be selected for iOS")
}

func TestRealEventStreamReadsLargeAndUnterminatedJSONRecords(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{name: "larger than scanner limit", payload: strings.Repeat("x", 2*1024*1024)},
		{name: "final record without newline", payload: "final output"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := json.Marshal(model.TestEvent{Action: "output", Package: "pkg", Output: tt.payload})
			require.NoError(t, err)
			line := string(encoded)
			if tt.name != "final record without newline" {
				line += "\n"
			}
			stream := &realEventStream{
				stdout:     io.NopCloser(strings.NewReader(line)),
				eventQueue: newStreamQueue[model.TestEvent](1),
				stop:       make(chan struct{}),
			}
			stream.readers.Add(1)
			stream.readEvents()
			stream.readers.Wait()
			assert.Empty(t, stream.eventQueue.Finalize())

			event := receiveWithTimeout(t, stream.Events())
			assert.Equal(t, tt.payload, event.Output)
			assert.Empty(t, stream.readErrs)
		})
	}
}
