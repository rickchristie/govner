package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rickchristie/govner/gowt/meta"
)

func main() {
	args := os.Args[1:]
	invocation, err := parseCLIInvocation(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	switch invocation.mode {
	case cliModeHelp:
		printUsage()
	case cliModeVersion:
		fmt.Printf("gowt version %s\n", meta.Version)
	case cliModeStorybook:
		if err := runStorybookModeWithProgram(runProgram); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case cliModeLoad:
		if err := runLoadMode(invocation.loadPath); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case cliModeLive:
		// Preserve the complete invocation, including everything after -args,
		// when handing live mode to go test.
		os.Exit(runLiveMode(args))
	}
}

type cliMode int

const (
	cliModeLive cliMode = iota
	cliModeHelp
	cliModeVersion
	cliModeStorybook
	cliModeLoad
)

type cliInvocation struct {
	mode     cliMode
	loadPath string
}

// parseCLIInvocation only interprets Gowt's own flags before go test's -args
// boundary. The remainder belongs to the test binary and must stay opaque even
// when it contains names such as --storybook, --load, or --help.
func parseCLIInvocation(args []string) (cliInvocation, error) {
	gowtArgs := args
	for i, arg := range args {
		if arg == "-args" || arg == "--args" {
			gowtArgs = args[:i]
			break
		}
	}

	// Help and version retain their existing precedence over load/storybook.
	for _, arg := range gowtArgs {
		if arg == "--help" || arg == "-h" {
			return cliInvocation{mode: cliModeHelp}, nil
		}
		if isVersionFlag(arg) {
			return cliInvocation{mode: cliModeVersion}, nil
		}
	}

	for i, arg := range gowtArgs {
		switch arg {
		case "--storybook":
			return cliInvocation{mode: cliModeStorybook}, nil
		case "--load", "-l":
			if i+1 >= len(gowtArgs) {
				return cliInvocation{}, fmt.Errorf("--load requires a file path")
			}
			return cliInvocation{mode: cliModeLoad, loadPath: gowtArgs[i+1]}, nil
		}
	}

	return cliInvocation{mode: cliModeLive}, nil
}

// -v belongs to go test and must be forwarded. Only the unambiguous long flag
// is reserved by Gowt for its own version output.
func isVersionFlag(arg string) bool { return arg == "--version" }

// runLoadMode runs the TUI with pre-loaded test results
func runLoadMode(path string) error {
	return runLoadModeWithProgram(path, runProgram)
}

// runLoadModeWithProgram keeps file loading separate from terminal ownership.
// Tests can verify startup behavior without opening a real TTY, while production
// still has one Bubble Tea startup path through runProgram.
func runLoadModeWithProgram(path string, run func(tea.Model) (tea.Model, error)) error {
	tree, err := loadTestResults(path)
	if err != nil {
		return err
	}

	app := NewApp(tree)
	if _, err := run(app); err != nil {
		return fmt.Errorf("error running app: %w", err)
	}

	return nil
}

// runLiveMode runs tests with the live TUI
func runLiveMode(args []string) int {
	return runLiveModeWithProgram(args, NewRealTestRunner(), runProgram)
}

// runLiveModeWithProgram makes runner and terminal lifecycle results explicit.
// The injected callback is an application boundary, not a second startup path.
func runLiveModeWithProgram(
	args []string,
	runner TestRunner,
	run func(tea.Model) (tea.Model, error),
) int {
	app := NewLiveApp(args, runner)

	finalModel, err := run(app)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running app: %v\n", err)
		return 1
	}

	// Return the exit code from go test
	if finalApp, ok := finalModel.(App); ok {
		return finalApp.exitCode
	}
	return 0
}

func runProgram(app tea.Model) (tea.Model, error) {
	return tea.NewProgram(app, tea.WithAltScreen()).Run()
}

func printUsage() {
	fmt.Printf("gowt - Go Test Watcher TUI (v%s)\n", meta.Version)
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  gowt [packages]              Run go test with live TUI")
	fmt.Println("  gowt --load <file>           Load and view test results from JSON file")
	fmt.Println("  gowt --storybook            Open deterministic UI fixtures for manual QA")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --load, -l <file>   Load test results from a JSON file (go test -json output)")
	fmt.Println("  --version           Show version")
	fmt.Println("  --storybook         Open deterministic UI fixtures for manual QA")
	fmt.Println("  --help, -h          Show this help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  gowt ./...                   Run all tests with TUI")
	fmt.Println("  gowt -v ./pkg/...            Run tests with verbose flag")
	fmt.Println("  gowt --load results.json     View saved test results")
	fmt.Println("  go test -json ./... > results.json && gowt -l results.json")
}
