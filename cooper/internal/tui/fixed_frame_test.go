package tui

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

type overflowingSubModel struct {
	mouseEvents int
}

func (m *overflowingSubModel) Init() tea.Cmd { return nil }

func (m *overflowingSubModel) Update(msg tea.Msg) (theme.SubModel, tea.Cmd) {
	if _, ok := msg.(tea.MouseMsg); ok {
		m.mouseEvents++
	}
	return m, nil
}

func (m *overflowingSubModel) View(width, height int) string {
	var lines []string
	for i := range height + 20 {
		lines = append(lines, fmt.Sprintf("oversized line %02d", i))
	}
	return strings.Join(lines, "\n")
}

func TestRootFrameClampsOversizedScreenAndRoutesMouse(t *testing.T) {
	child := &overflowingSubModel{}
	m := NewModel(nil)
	m.SetSize(100, 12)
	m.SetContainersModel(child)

	rendered := m.View()
	lines := strings.Split(rendered, "\n")
	if len(lines) != 12 {
		t.Fatalf("root screen height = %d, want 12", len(lines))
	}
	if !strings.Contains(lines[0], "Cooper") {
		t.Fatalf("fixed header missing from first row: %q", lines[0])
	}
	if !strings.Contains(lines[len(lines)-1], "Quit") {
		t.Fatalf("fixed footer missing from final row: %q", lines[len(lines)-1])
	}

	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if child.mouseEvents != 1 {
		t.Fatalf("active screen mouse events = %d, want 1", child.mouseEvents)
	}
}

func TestNewProgramEnablesTerminalMouseReporting(t *testing.T) {
	m := NewModel(nil)
	m.SetSize(80, 20)

	inputReader, inputWriter := io.Pipe()
	var output bytes.Buffer
	program := NewProgram(
		m,
		tea.WithInput(inputReader),
		tea.WithOutput(&output),
	)

	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()

	if _, err := inputWriter.Write([]byte("q\r")); err != nil {
		t.Fatalf("write terminal input: %v", err)
	}
	inputWriter.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program run failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		program.Kill()
		t.Fatal("program did not exit")
	}

	if got := output.String(); !strings.Contains(got, "\x1b[?1002h") {
		t.Fatalf("terminal output never enabled cell-motion mouse reporting: %q", got)
	}
}
