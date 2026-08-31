# TUI Code Architecture Standard

Read and follow this file before you change TUI code in Govner.

## Architecture Laws

- **LAW: The TUI is presentation only.** It does not know Docker, SQL, filesystem, HTTP, or shell details.
- **LAW: Put one application boundary in front of the TUI.** All business actions go through an app or service interface.
- **LAW: The root model is a shell.** It owns global layout, global keys, and routing. It owns nothing else.
- **LAW: Each screen is its own model.** Large screens become submodels. Large submodels become components.
- **LAW: Depend on the smallest interface that works.** Never give a large application interface to a leaf screen.
- **LAW: Use one startup path and one shutdown path.** Do not duplicate lifecycle logic for loading screens, tests, and real runs.
- **LAW: Shared widgets live in shared packages.** Do not copy scrollers, tables, tabs, modals, or text inputs.

The non-test Go snippets below are consecutive parts of one `model.go` file.

```go
package jobs

import tea "github.com/charmbracelet/bubbletea"

type App interface {
	JobApprover
	ListJobs() []Job
}

type JobApprover interface {
	ApproveJob(id string) error
}
```

## State Laws

- **LAW: Keep one source of truth for each piece of state.**
- **LAW: State changes in `Update`, not in `View`.**
- **LAW: `View` is a pure function of model state.** No I/O. No sleeps. No state changes. No goroutines.
- **LAW: UI state and domain state are different.** A cursor index is UI state. An approved request is domain state.
- **LAW: Do not share mutable config pointers across layers.** Pass snapshots in and send commands out.
- **LAW: Every modal, form, and editor has explicit state.** Do not spread hidden booleans across packages.
- **LAW: Recompute derived state unless a measurement proves that stored state is necessary.**

```go
type Job struct {
	ID string
}

type Model struct {
	app      JobApprover
	items    []Job
	selected int
	editing  bool
	showQuit bool
	width    int
	height   int
	errMsg   string
}
```

## Event Laws

- **LAW: Every external event becomes a typed message.**
- **LAW: Every side effect returns to the model as a message.**
- **LAW: Message names describe facts, not intentions.** Prefer `JobApprovedMsg`, not `ApproveMsgDoneMaybe`.
- **LAW: Root routing is explicit.** Do not use side channels or hidden callbacks between screens.
- **LAW: Cross-screen updates use messages, not concrete type assertions.**
- **LAW: Long work runs in commands or services, never in key handlers.**
- **LAW: Timer ticks are messages. Polling is a command.**

```go
type JobApprovedMsg struct {
	ID  string
	Err error
}

func approveJobCmd(app JobApprover, id string) tea.Cmd {
	return func() tea.Msg {
		err := app.ApproveJob(id)
		return JobApprovedMsg{ID: id, Err: err}
	}
}
```

## Update Laws

- **LAW: `Update` is the only state transition function.**
- **LAW: Every key path is easy to scan.** Use a small switch and small helper functions.
- **LAW: Handle global keys before local keys.** Handle quit, help, tab changes, and modal dismissal first.
- **LAW: When a modal is active, it owns the keyboard.**
- **LAW: Invalid actions are explicit no-ops.** They do not panic, change unrelated state, or start part of an action.
- **LAW: Error results are first-class state.** Do not discard them.
- **LAW: A command that can fail reports the failure to the model.**

```go
func (m *Model) Init() tea.Cmd { return nil }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q":
			m.showQuit = true
			return m, nil
		case "enter":
			if m.selected >= 0 && m.selected < len(m.items) {
				return m, approveJobCmd(m.app, m.items[m.selected].ID)
			}
		}
	case JobApprovedMsg:
		if msg.Err != nil {
			m.errMsg = msg.Err.Error()
			return m, nil
		}
		m.errMsg = ""
	}
	return m, nil
}
```

## View Laws

- **LAW: `View` composes strings. It does not decide behavior.**
- **LAW: Layout math is central.** Put width, height, scroll area, and pane split calculations in helper functions.
- **LAW: ANSI-aware width handling is mandatory.**
- **LAW: Empty states are designed states.**
- **LAW: Help bars are contextual and accurate.**
- **LAW: Selection, focus, disabled, pending, success, and error states look different.**
- **LAW: Do not copy the same layout math into many screens.**

```go
func (m *Model) View() string {
	if len(m.items) == 0 {
		return "No jobs.\nPress n to create one."
	}
	return renderTable(m.items, m.selected, m.width, m.height)
}

func renderTable(items []Job, selected, width, height int) string {
	if selected < 0 || selected >= len(items) {
		return "No job selected."
	}
	return items[selected].ID
}
```

## Reliability Laws

- **LAW: The TUI survives partial failure.** One failed command does not stop the full app.
- **LAW: On failure, keep a usable screen and show a concrete error.**
- **LAW: Startup is a staged state machine.** Each step has a name, status, and failure path.
- **LAW: Shutdown is also a staged state machine.**
- **LAW: Background goroutines have an owner and a stop condition.**
- **LAW: Channels exposed to the TUI are read-only.**
- **LAW: Hidden dependencies have a timeout or cancellation.**
- **LAW: The user always knows if the app is idle, loading, waiting, failed, or done.**

```go
type Step struct {
	Name   string
	Status string
	Err    error
}
```

## Readability Laws

- **LAW: Name things by role.** Examples are `App`, `RoutesModel`, `SettingsChangedMsg`, and `ScrollableList`.
- **LAW: Keep files easy to find.** `model.go`, `view.go`, `messages.go`, and `component.go` are good names.
- **LAW: Split by responsibility, not by an arbitrary line count.**
- **LAW: Comments explain intent and invariants, not syntax.**
- **LAW: Every exported type makes an architecture boundary clearer.**
- **LAW: Many concrete model assertions show that the boundary is wrong.**
- **LAW: A root model that knows each tab detail has degraded.**

## Testability Laws

- **LAW: Business logic is testable without a terminal.**
- **LAW: Parsing, validation, sorting, trimming, and state transitions have unit tests.**
- **LAW: Every bug gets a reproducing test before or with the fix.**
- **LAW: Complex screens use a fake app and deterministic messages.**
- **LAW: A `tui-test` or storybook mode is required for manual QA.**
- **LAW: Test messages and commands, not only helper functions.**
- **LAW: Time-dependent behavior is injectable or message-driven.**
- **LAW: Clipboard, network, shell, Docker, and filesystem access is mockable behind interfaces.**

The test example below is valid with the model example above. It selects one job before it sends `enter`, and it checks that the command is not nil before it runs the command.

```go
package jobs

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type FakeApp struct {
	approveErr error
}

func (f FakeApp) ApproveJob(id string) error { return f.approveErr }

func TestApproveFailureShowsError(t *testing.T) {
	m := &Model{
		app:      FakeApp{approveErr: errors.New("boom")},
		items:    []Job{{ID: "job-1"}},
		selected: 0,
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected approval command")
	}
	m.Update(cmd())
	if m.errMsg != "boom" {
		t.Fatalf("error message = %q, want %q", m.errMsg, "boom")
	}
}
```

## Refactoring Laws

- **LAW: When you refactor a TUI, preserve behavior first. Then improve the structure.**
- **LAW: Remove duplication with small primitives, not large abstractions.**
- **LAW: Do not move business logic into `View` to make files smaller.**
- **LAW: Do not hide architecture debt behind helper names.**
- **LAW: After a refactor, the message flow is easier to explain.**
- **LAW: An abstraction that makes tests more difficult is probably wrong.**

## Red Flags

- **RED FLAG: `View` writes files, uses the network, or starts goroutines.**
- **RED FLAG: A screen imports infrastructure packages directly.**
- **RED FLAG: The root model changes child internals through concrete casts.**
- **RED FLAG: Errors are only logged and are not shown in UI state.**
- **RED FLAG: The same table, modal, or scroll code exists in many packages.**
- **RED FLAG: Startup logic is duplicated in production, tests, and loading flows.**
- **RED FLAG: A key press does blocking work directly in `Update`.**
- **RED FLAG: Screen tests have no fake app.**
