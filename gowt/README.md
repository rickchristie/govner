# gowt - Go Test Watcher TUI

A terminal-based UI for running and viewing Go test results and logs in real-time.

Useful when you have thousands of tests to run.

![gowt demo](/gowt/docs/peek.gif)

## Features

- 🎯 **Live test streaming** - Watch tests run in real-time with animated spinners
- 🌳 **Tree view** - Hierarchical display of packages and tests
- 📋 **Log viewer** - View detailed test output with search functionality
- 🔍 **Focus mode** - Filter to show only failed and running tests
- 🔄 **Rerun tests** - Quickly rerun all tests or specific failed tests
- 📋 **Copy to clipboard** - Copy test logs for easy sharing
- ⚡ **Cached test detection** - See which tests used cached results

## Installation

```bash
go install github.com/rickchristie/govner/gowt@latest
```

Make sure `$GOPATH/bin` is in your `PATH`. Add this to your shell profile (`~/.bashrc`, `~/.zshrc`, or `~/.profile`):

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

Then reload your shell or run `source ~/.bashrc`.

## Usage

### Run tests with live TUI

`Gowt` wraps `go test` and provides a live TUI for viewing test results. You can pass any `go test` flags and specify packages as usual: 

Gowt always enables `-json` because the TUI reads the structured event stream.
User-supplied command-level `-json` options cannot disable this requirement.
Arguments after `-args` remain unchanged and belong to the test binary.

```bash
# Run all tests in current directory
gowt ./...

# Run tests in a specific package
gowt ./pkg/mypackage/...

# Run with verbose flag
gowt -v ./...

# Run with any go test flags
gowt -race -count=1 ./...
```

### Load saved test results

You can also view previously saved test results:

```bash
# Save test results to a file
go test -json ./... > results.json

# View saved results
gowt --load results.json
gowt -l results.json
```

### Manual TUI QA

Use the deterministic storybook to inspect layout, colors, resize behavior,
search, raw/processed logs, and keyboard navigation without running a project:

```bash
gowt --storybook
```

## Keyboard Shortcuts

### Tree View

![Tree view demo](/gowt/docs/treeview.png)

| Key | Action |
|-----|--------|
| `↑`/`k` | Move up |
| `↓`/`j` | Move down |
| `←`/`h` | Collapse or go to parent |
| `→`/`l` | Expand |
| `Enter` | View test logs |
| `Space` | Toggle filter (All/Focus) |
| `e` | Toggle expand/collapse all |
| `g` | Go to top |
| `G` | Go to bottom |
| `PgUp`/`Ctrl+u` | Page up |
| `PgDn`/`Ctrl+d` | Page down |
| `r` | Rerun all tests |
| `?` | Show help |
| `q` | Quit |

### Log View

![Log view demo](/gowt/docs/logview.png)

| Key | Action |
|-----|--------|
| `↑`/`k` | Scroll up |
| `↓`/`j` | Scroll down |
| `g` | Go to top |
| `G` | Go to bottom |
| `PgUp`/`Ctrl+u` | Page up |
| `PgDn`/`Ctrl+d` | Page down |
| `/` | Start search |
| `n` | Jump to next match |
| `N` | Jump to previous match |
| `Space` | Toggle view mode (Processed/Raw) |
| `c` | Copy logs to clipboard |
| `r` | Rerun this test |
| `Esc`/`q`/`Backspace` | Go back to tree view |
| `?` | Show help |

## Rerun Tests

![Rerun tests demo](/gowt/docs/rerun.png)

Press `r` to rerun tests:
- Rerun all tests in Tree View.
- Rerun the __only__ the selected test in Log View.

## Status Icons

| Icon | Meaning |
|------|---------|
| ✓ | Passed |
| ↯ | Passed (cached) |
| ✗ | Failed |
| ⊘ | Skipped |
| ● | Running |
| ○ | Pending |

## Focus Mode

Press `Space` to toggle Focus mode, which filters the tree to show only:
- Failed tests
- Currently running tests

This is useful for quickly identifying and fixing failing tests in large test suites.

## Clipboard Support

The `c` key in log view copies test output to your clipboard. Supported clipboard tools:
- **Wayland**: `wl-copy` (install with `sudo apt install wl-clipboard`)
- **X11**: `xclip` or `xsel` (install with `sudo apt install xclip`)
- **macOS**: `pbcopy` (built-in)
- **Windows/WSL**: `clip.exe` (built-in)

## License

MIT License - see [LICENSE](../LICENSE) for details.

## ✨ Made with Claude

🛠️ Built together with [Claude Code](https://claude.ai/code)
