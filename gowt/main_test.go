package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	model "github.com/rickchristie/govner/gowt/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMainCLIHelperProcess(t *testing.T) {
	if os.Getenv("GOWT_TEST_MAIN_HELPER") != "1" {
		return
	}

	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("GOWT_TEST_MAIN_ARGS")), &args); err != nil {
		panic(err)
	}
	os.Args = append([]string{"gowt"}, args...)
	main()
}

func runMainCLI(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	encoded, err := json.Marshal(args)
	require.NoError(t, err)

	command := exec.Command(os.Args[0], "-test.run=^TestMainCLIHelperProcess$")
	command.Env = append(os.Environ(),
		"GOWT_TEST_MAIN_HELPER=1",
		"GOWT_TEST_MAIN_ARGS="+string(encoded),
	)
	var stdoutBuffer, stderrBuffer bytes.Buffer
	command.Stdout = &stdoutBuffer
	command.Stderr = &stderrBuffer
	err = command.Run()
	if err == nil {
		return stdoutBuffer.String(), stderrBuffer.String(), 0
	}
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	return stdoutBuffer.String(), stderrBuffer.String(), exitErr.ExitCode()
}

func TestMainHelpFlags(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		t.Run(flag, func(t *testing.T) {
			stdout, stderr, exitCode := runMainCLI(t, flag)
			assert.Zero(t, exitCode)
			assert.Empty(t, stderr)
			assert.Contains(t, stdout, "gowt - Go Test Watcher TUI")
			assert.Contains(t, stdout, "Usage:")
			assert.Contains(t, stdout, "gowt --load <file>")
		})
	}
}

func TestMainLongVersionFlag(t *testing.T) {
	stdout, stderr, exitCode := runMainCLI(t, "--version")

	assert.Zero(t, exitCode)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "gowt version ")
}

func TestVersionFlagDoesNotConsumeGoTestVerboseFlag(t *testing.T) {
	assert.True(t, isVersionFlag("--version"))
	assert.False(t, isVersionFlag("-v"), "-v must be forwarded to go test")
	assert.False(t, isVersionFlag("--verbose"))
}

func TestParseCLIInvocationStopsAtTestBinaryArgs(t *testing.T) {
	for _, boundary := range []string{"-args", "--args", "--"} {
		t.Run(boundary, func(t *testing.T) {
			args := []string{
				"./pkg", boundary, "--storybook", "--load", "fixture.json",
				"--help", "--version",
			}

			invocation, err := parseCLIInvocation(args)
			require.NoError(t, err)
			assert.Equal(t, cliModeLive, invocation.mode)
			assert.Empty(t, invocation.loadPath)
			assert.Equal(t, []string{
				"./pkg", boundary, "--storybook", "--load", "fixture.json",
				"--help", "--version",
			}, args, "mode parsing must not rewrite forwarded arguments")
		})
	}
}

func TestParseCLIInvocationDoesNotInterpretGoFlagValues(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "help is run pattern", args: []string{"-run", "--help", "./..."}},
		{name: "version is overlay path", args: []string{"-overlay", "--version", "./..."}},
		{name: "storybook is exec command", args: []string{"-exec", "--storybook", "./..."}},
		{name: "load is tag value", args: []string{"--tags", "--load", "./..."}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invocation, err := parseCLIInvocation(tt.args)
			require.NoError(t, err)
			assert.Equal(t, cliModeLive, invocation.mode)
		})
	}
}

func TestParseCLIInvocationRecognizesModesBeforeTestBinaryArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantMode cliMode
		wantPath string
	}{
		{name: "help", args: []string{"./pkg", "--help"}, wantMode: cliModeHelp},
		{name: "version", args: []string{"--version"}, wantMode: cliModeVersion},
		{name: "storybook", args: []string{"--storybook"}, wantMode: cliModeStorybook},
		{name: "load", args: []string{"-l", "results.json"}, wantMode: cliModeLoad, wantPath: "results.json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invocation, err := parseCLIInvocation(tt.args)
			require.NoError(t, err)
			assert.Equal(t, tt.wantMode, invocation.mode)
			assert.Equal(t, tt.wantPath, invocation.loadPath)
		})
	}
}

func TestMainLoadRequiresPath(t *testing.T) {
	stdout, stderr, exitCode := runMainCLI(t, "--load")

	assert.Equal(t, 1, exitCode)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "Error: --load requires a file path")
}

func TestMainDirectHelpAndVersionBranches(t *testing.T) {
	originalArgs := os.Args
	originalStdout := os.Stdout
	t.Cleanup(func() {
		os.Args = originalArgs
		os.Stdout = originalStdout
	})

	for _, flag := range []string{"--help", "--version"} {
		reader, writer, err := os.Pipe()
		require.NoError(t, err)
		os.Stdout = writer
		os.Args = []string{"gowt", flag}

		main()

		require.NoError(t, writer.Close())
		var output bytes.Buffer
		_, err = output.ReadFrom(reader)
		require.NoError(t, err)
		require.NoError(t, reader.Close())
		assert.Contains(t, output.String(), "gowt")
	}
}

func TestRunLoadModeReturnsLoadError(t *testing.T) {
	err := runLoadMode(t.TempDir() + "/missing.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open file")
}

func TestRunLoadModeWithProgram(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.json")
	require.NoError(t, os.WriteFile(path, []byte(
		`{"Action":"pass","Package":"pkg","Test":"TestOne"}`+"\n",
	), 0o600))

	called := false
	err := runLoadModeWithProgram(path, func(teaModel tea.Model) (tea.Model, error) {
		called = true
		app, ok := teaModel.(App)
		require.True(t, ok)
		assert.False(t, app.running)
		assert.Equal(t, model.StatusPassed, app.tree.GetNode("pkg/TestOne").Status)
		return app, nil
	})
	require.NoError(t, err)
	assert.True(t, called)

	runErr := errors.New("terminal failed")
	err = runLoadModeWithProgram(path, func(tea.Model) (tea.Model, error) {
		return nil, runErr
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, runErr)
	assert.Contains(t, err.Error(), "error running app")
}

func TestRunLiveModeWithProgram(t *testing.T) {
	runner := &fakeTestRunner{}

	exitCode := runLiveModeWithProgram(
		[]string{"-race", "./..."},
		runner,
		func(teaModel tea.Model) (tea.Model, error) {
			app, ok := teaModel.(App)
			require.True(t, ok)
			assert.True(t, app.running)
			assert.Equal(t, []string{"-race", "./..."}, app.testArgs)
			assert.Same(t, runner, app.runner)
			app.exitCode = 7
			return app, nil
		},
	)
	assert.Equal(t, 7, exitCode)

	exitCode = runLiveModeWithProgram(nil, runner, func(tea.Model) (tea.Model, error) {
		return struct{ tea.Model }{}, nil
	})
	assert.Zero(t, exitCode, "an unexpected final model has no go test exit code")

	exitCode = runLiveModeWithProgram(nil, runner, func(tea.Model) (tea.Model, error) {
		return nil, errors.New("terminal failed")
	})
	assert.Equal(t, 1, exitCode)

	exitCode = runLiveModeWithProgram(nil, runner, func(teaModel tea.Model) (tea.Model, error) {
		updated, _ := teaModel.Update(TestDoneMsg{
			Err: errors.New("stream decoder failed"), ExitCode: 0, RunGen: 0,
		})
		return updated, nil
	})
	assert.Equal(t, 1, exitCode, "operational failures must propagate to the shell")
}

func TestPrintUsage(t *testing.T) {
	original := os.Stdout
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = original
	})

	printUsage()
	require.NoError(t, writer.Close())
	var output bytes.Buffer
	_, err = output.ReadFrom(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	assert.Contains(t, output.String(), "Examples:")
	assert.Contains(t, output.String(), "--version")
	assert.Contains(t, output.String(), "--storybook")
	assert.Contains(t, output.String(), "go test -json")
}
