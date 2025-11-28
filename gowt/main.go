package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	args := os.Args[1:]

	// Check for help flag
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			printUsage()
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

	// Live mode: run go test with TUI
	exitCode := runLiveMode(args)
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

// runLiveMode runs tests with the live TUI
func runLiveMode(args []string) int {
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

func printUsage() {
	fmt.Println("gowt - Go Test Watcher TUI")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  gowt [packages]              Run go test with live TUI")
	fmt.Println("  gowt --load <file>           Load and view test results from JSON file")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --load, -l <file>   Load test results from a JSON file (go test -json output)")
	fmt.Println("  --help, -h          Show this help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  gowt ./...                   Run all tests with TUI")
	fmt.Println("  gowt -v ./pkg/...            Run tests with verbose flag")
	fmt.Println("  gowt --load results.json     View saved test results")
	fmt.Println("  go test -json ./... > results.json && gowt -l results.json")
}
