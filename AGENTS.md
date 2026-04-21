# Govner
Govner is a collection of Go development tools.

# Critical Behavior
- **ALWAYS write all outputs of test, bash commands to /tmp file** when running test, scripts that you need the result for.
  You can then run head/tail or search on the result file. This avoids re-running the script again whenever there are issues.
- **ALWAYS fully finish your tasks** when executing anything. Never stop to ask "would you like to continue?" or anything similar.
  You are given tasks, fully complete them, don't waste our time.
- **ALWAYS verify your work and assumptions**, don't just read the code, actually test what you're doing.
  Write tests, write scripts to test behaviors, run playwright to check console, take screenshots, run the commands.
- **NEVER blame without evidence**, don't say something like "X fail due to Y", find evidence! 
  **Always investigate to find root cause**, if unable to find evidence, state why and clarify it's a hypothesis.
- **ALWAYS write proper documentation**, write *why* it was done this way, and *how* only if it's not obvious.
  Write for a human or yourself when they revisit this code in the future, what is important for them so they work faster and with less mistakes?
- **Cooper test suites:** when validating `cooper`, run `go test ./... > /tmp/cooper-go-test.txt 2>&1` and `timeout 90m ./test-e2e.sh > /tmp/cooper-e2e.txt 2>&1` from `cooper/`; use targeted package tests while iterating, but finish with both full suites.

# TUI Code Architecture Standard

## Architecture Laws
- **LAW: The TUI is presentation only.** It does not know Docker, SQL, filesystems, HTTP, or shell details.
- **LAW: Put one application boundary in front of the TUI.** All business actions go through an app/service interface.
- **LAW: The root model is a shell.** It owns global layout, global keys, routing, and nothing else.
- **LAW: Each screen is its own model.** Large screens become sub-models. Large sub-models become components.
- **LAW: Depend on the smallest interface that works.** Never inject a god interface into leaf screens.
- **LAW: Use one startup path and one shutdown path.** Do not duplicate lifecycle logic for loading screens, tests, and real runs.
- **LAW: Shared widgets live in shared packages.** Scrollers, tables, tabs, modals, and text inputs must not be copy-pasted.

```go
type App interface {
	ListJobs() []Job
	ApproveJob(id string) error
}

type JobApprover interface {
	ApproveJob(id string) error
}
```

## State Laws
- **LAW: Keep one source of truth for each piece of state.**
- **LAW: State mutates in `Update`, not in `View`.**
- **LAW: `View` is a pure function of model state.** No I/O. No sleeps. No mutations. No goroutines.
- **LAW: UI state and domain state are different things.** Cursor index is UI state. Approved request is domain state.
- **LAW: Do not leak mutable config pointers across layers.** Pass snapshots in, send commands out.
- **LAW: Every modal, form, and editor has explicit state.** No hidden booleans scattered across packages.
- **LAW: Derived state should be recomputed, not stored, unless measurement proves otherwise.**

```go
type Model struct {
	items    []Job
	selected int
	editing  bool
	errMsg   string
}
```

## Event Laws
- **LAW: Every external event becomes a typed message.**
- **LAW: Every side effect returns to the model as a message.**
- **LAW: Message names must describe facts, not intentions.** Prefer `JobApprovedMsg`, not `ApproveMsgDoneMaybe`.
- **LAW: Root routing must be explicit.** Do not rely on side channels or hidden callbacks between screens.
- **LAW: Cross-screen updates flow through messages, not concrete type assertions.**
- **LAW: Long-running work runs in commands or services, never inline in key handlers.**
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
- **LAW: Every key path must be easy to scan.** Small switch, small helper methods.
- **LAW: Handle global keys before local keys.** Quit, help, tab switch, modal dismiss.
- **LAW: When a modal is active, it owns the keyboard.**
- **LAW: Invalid actions are explicit no-ops.** They do not panic, mutate random state, or silently half-run.
- **LAW: Error results are first-class state.** Never drop them on the floor.
- **LAW: A command that can fail must report failure back to the model.**

```go
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
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
- **LAW: `View` composes strings; it does not decide behavior.**
- **LAW: Layout math must be centralized.** Width, height, scroll area, and pane splits belong in helpers.
- **LAW: ANSI-aware width handling is mandatory.**
- **LAW: Empty states are designed states.**
- **LAW: Help bars are contextual and truthful.**
- **LAW: Selection, focus, disabled, pending, success, and error states must look different.**
- **LAW: Never hardcode the same layout math in five screens.**

```go
func (m Model) View() string {
	if len(m.items) == 0 {
		return "No jobs.\nPress n to create one."
	}
	return renderTable(m.items, m.selected, m.width, m.height)
}
```

## Reliability Laws
- **LAW: The TUI must survive partial failure.** One failed command must not crash the whole app.
- **LAW: On failure, preserve a usable screen and show a concrete error.**
- **LAW: Startup is a staged state machine.** Each step has a name, status, and failure path.
- **LAW: Shutdown is also a staged state machine.**
- **LAW: Background goroutines must have ownership and a stop condition.**
- **LAW: Channels exposed to the TUI are read-only.**
- **LAW: Never block forever on a hidden dependency without timeout or cancellation.**
- **LAW: The user must always know whether the app is idle, loading, waiting, failed, or done.**

```go
type Step struct {
	Name   string
	Status string
	Err    error
}
```

## Readability Laws
- **LAW: Name things by role.** `App`, `RoutesModel`, `SettingsChangedMsg`, `ScrollableList`.
- **LAW: Keep files boring to navigate.** `model.go`, `view.go`, `messages.go`, `component.go` are fine.
- **LAW: Split by responsibility, not by arbitrary line count.**
- **LAW: Comments explain intent and invariants, not obvious syntax.**
- **LAW: Every exported type should make architectural boundaries clearer.**
- **LAW: If a package needs many type assertions to concrete models, the boundary is wrong.**
- **LAW: If the root model knows every detail of every tab, the architecture has already degraded.**

## Testability Laws
- **LAW: Business logic is testable without a terminal.**
- **LAW: Parsing, validation, sorting, trimming, and state transitions get unit tests.**
- **LAW: Every bug gets a reproducing test before or with the fix.**
- **LAW: Complex screens need a fake app and deterministic messages.**
- **LAW: `tui-test` or storybook mode is required for manual QA.**
- **LAW: Test messages and commands, not just helper functions.**
- **LAW: Time-dependent behavior must be injectable or message-driven.**
- **LAW: Clipboard, network, shell, Docker, and filesystem access must be mockable behind interfaces.**

```go
type FakeApp struct {
	approveErr error
}

func (f FakeApp) ApproveJob(id string) error { return f.approveErr }

func TestApproveFailureShowsError(t *testing.T) {
	m := Model{app: FakeApp{approveErr: errors.New("boom")}}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := cmd()
	updated, _ = updated.Update(msg)
	if updated.errMsg == "" {
		t.Fatal("expected error message")
	}
}
```

## Refactoring Laws
- **LAW: When refactoring a TUI, preserve behavior first, then improve structure.**
- **LAW: Remove duplication by extracting primitives, not by inventing giant abstractions.**
- **LAW: Do not move business logic into `View` to make files look smaller.**
- **LAW: Do not hide architectural debt behind helper names.**
- **LAW: After refactor, the message flow must be easier to explain than before.**
- **LAW: If a new abstraction makes tests harder, it is probably the wrong abstraction.**

## Red Flags
- **RED FLAG: `View` writes files, hits the network, or starts goroutines.**
- **RED FLAG: A screen imports infra packages directly.**
- **RED FLAG: Root model mutates child internals through concrete casts.**
- **RED FLAG: Errors are only logged, not surfaced in UI state.**
- **RED FLAG: The same table, modal, or scroll code exists in multiple packages.**
- **RED FLAG: Startup logic is duplicated in production, tests, and loading flow.**
- **RED FLAG: A key press directly performs blocking work in `Update`.**
- **RED FLAG: There is no fake app for screen tests.**
