package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rickchristie/govner/gowt/meta"
)

func main() {
	args := os.Args[1:]

	// Check for help and version flags
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			printUsage()
			return
		}
		if arg == "--version" || arg == "-v" {
			fmt.Printf("gowt version %s\n", meta.Version)
			return
		}
	}

	// Check for --load or -l flag
	for i, arg := range args {
		if arg == "--load" || arg == "-l" {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "Error: --load requires a file path\n")
				os.Exit(1)
			}
			if err := runLoadMode(args[i+1]); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	// Check for --clean-cache flag
	for _, arg := range args {
		if arg == "--clean-cache" {
			if err := cleanCache(); err != nil {
				fmt.Fprintf(os.Stderr, "Error cleaning cache: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Cache cleaned successfully")
			return
		}
	}

	// Check for --legacy flag
	legacy := false
	var filteredArgs []string
	for _, arg := range args {
		if arg == "--legacy" || arg == "-L" {
			legacy = true
		} else {
			filteredArgs = append(filteredArgs, arg)
		}
	}

	// Live mode: run go test with TUI
	var exitCode int
	if legacy {
		exitCode = runLegacyMode(filteredArgs)
	} else {
		exitCode = runTwoPhaseMode(filteredArgs)
	}
	os.Exit(exitCode)
}

// runLoadMode runs the TUI with pre-loaded test results
func runLoadMode(path string) error {
	tree, err := loadTestResults(path)
	if err != nil {
		return err
	}

	app := NewApp(tree)
	p := tea.NewProgram(app, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("error running app: %w", err)
	}

	return nil
}

// runLegacyMode runs tests with the legacy single-phase mode
func runLegacyMode(args []string) int {
	runner := NewRealTestRunner()
	app := NewLiveApp(args, runner)
	p := tea.NewProgram(app, tea.WithAltScreen())

	finalModel, err := p.Run()
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

// runTwoPhaseMode runs tests with two-phase execution (build then test)
func runTwoPhaseMode(args []string) int {
	// Check Go version (require 1.10+ for test2json)
	if err := checkGoVersion(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	// Check that test2json is available
	if err := CheckTest2JsonAvailable(); err != nil {
		fmt.Fprintln(os.Stderr, "Error: go tool test2json is not available.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "This tool should be included with Go 1.10+. If you have Go installed")
		fmt.Fprintln(os.Stderr, "but test2json is missing, your Go installation may be incomplete.")
		fmt.Fprintln(os.Stderr, "Try reinstalling Go from https://go.dev/dl/")
		return 1
	}

	// Parse arguments
	parsed := ParseArgs(args)

	// Convert test flags to -test.* format for the binary
	testFlags := ConvertToTestFlags(parsed.TestFlags)

	// Create two-phase runner
	twoPhase, err := NewTwoPhaseRunner(parsed.Patterns, parsed.BuildFlags, testFlags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating runner: %v\n", err)
		return 1
	}

	// Create legacy runner for single test reruns
	runner := NewRealTestRunner()

	app := NewTwoPhaseApp(twoPhase, runner)
	p := tea.NewProgram(app, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running app: %v\n", err)
		return 1
	}

	// Return the exit code from tests
	if finalApp, ok := finalModel.(App); ok {
		return finalApp.exitCode
	}
	return 0
}

// cleanCache removes all cached test binaries
func cleanCache() error {
	// Create a temporary runner just to get the temp dir path
	twoPhase, err := NewTwoPhaseRunner([]string{"./..."}, nil, nil)
	if err != nil {
		return err
	}
	return twoPhase.CleanTempDir()
}

func printUsage() {
	fmt.Printf("gowt - Go Test Watcher TUI (v%s)\n", meta.Version)
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  gowt [packages]              Run go test with live TUI (two-phase mode)")
	fmt.Println("  gowt --legacy [packages]     Run go test with live TUI (legacy mode)")
	fmt.Println("  gowt --load <file>           Load and view test results from JSON file")
	fmt.Println("  gowt --clean-cache           Remove cached test binaries")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --legacy, -L        Use legacy single-phase mode (go test -json directly)")
	fmt.Println("  --clean-cache       Remove cached test binaries from temp directory")
	fmt.Println("  --load, -l <file>   Load test results from a JSON file (go test -json output)")
	fmt.Println("  --version, -v       Show version")
	fmt.Println("  --help, -h          Show this help message")
	fmt.Println()
	fmt.Println("Two-Phase Mode (default):")
	fmt.Println("  Phase 1: Build all test binaries in parallel")
	fmt.Println("  Phase 2: Run tests sequentially (alphabetically)")
	fmt.Println("  Benefits: Faster builds, no resource contention, predictable ordering")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  gowt ./...                   Run all tests with TUI")
	fmt.Println("  gowt -race ./pkg/...         Run tests with race detector")
	fmt.Println("  gowt --legacy ./...          Use legacy single-phase mode")
	fmt.Println("  gowt --load results.json     View saved test results")
	fmt.Println("  go test -json ./... > results.json && gowt -l results.json")
}

// checkGoVersion verifies that Go 1.10+ is installed (required for test2json)
func checkGoVersion() error {
	cmd := exec.Command("go", "version")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("Go is not installed or not in PATH")
	}

	// Parse version from output like "go version go1.21.0 linux/amd64"
	versionStr := strings.TrimSpace(string(output))
	re := regexp.MustCompile(`go(\d+)\.(\d+)`)
	matches := re.FindStringSubmatch(versionStr)
	if len(matches) < 3 {
		return fmt.Errorf("unable to parse Go version from: %s", versionStr)
	}

	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])

	if major < 1 || (major == 1 && minor < 10) {
		return fmt.Errorf("Go 1.10+ is required (found go%d.%d)", major, minor)
	}

	return nil
}
