package configure

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestSaveScreenFixedFrameScrollsWithKeyboardAndMouse(t *testing.T) {
	m := newSaveModel(config.DefaultConfig(), t.TempDir(), "", nil)
	const width, height = 90, 12

	initial := m.view(width, height)
	assertFixedScreen(t, initial, height, []string{"Configure > ", "Save & Build"}, "Enter Build")
	if m.lastMaxScroll <= 0 {
		t.Fatalf("Save & Build body should overflow at height %d", height)
	}

	m.update(tea.KeyMsg{Type: tea.KeyDown})
	if m.scrollOffset != 1 {
		t.Fatalf("down key offset = %d, want 1", m.scrollOffset)
	}
	afterKey := m.view(width, height)
	assertFixedScreen(t, afterKey, height, []string{"Configure > ", "Save & Build"}, "Enter Build")
	if afterKey == initial {
		t.Fatal("down key did not change the visible middle section")
	}

	m.update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if m.scrollOffset <= 1 {
		t.Fatalf("mouse wheel did not advance body offset: %d", m.scrollOffset)
	}
	afterMouse := m.view(width, height)
	assertFixedScreen(t, afterMouse, height, []string{"Configure > ", "Save & Build"}, "Enter Build")
}

func TestWelcomeScreenUsesFixedScrollableFrame(t *testing.T) {
	m := newWelcomeModel(true)
	const width, height = 90, 9

	initial := m.view(width, height, true)
	assertFixedScreen(t, initial, height, []string{"c o o p e r", "c o n f i g u r e"}, "Enter Select")
	if m.lastMaxScroll <= 0 {
		t.Fatalf("welcome body should overflow at height %d", height)
	}

	m.update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if m.scrollOffset == 0 {
		t.Fatal("welcome mouse wheel did not scroll")
	}
	afterMouse := m.view(width, height, true)
	assertFixedScreen(t, afterMouse, height, []string{"c o o p e r", "c o n f i g u r e"}, "Enter Select")
}

func TestConfigureProgramEnablesTerminalMouseReporting(t *testing.T) {
	m := newModel(config.DefaultConfig(), t.TempDir(), nil, false)
	inputReader, inputWriter := io.Pipe()
	var output bytes.Buffer
	program := newConfigureProgram(
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
		t.Fatal("configure program did not exit")
	}
	if got := output.String(); !strings.Contains(got, "\x1b[?1002h") {
		t.Fatalf("configure never enabled cell-motion mouse reporting: %q", got)
	}
}

func TestBuildFeedbackStreamsAndScrollsInsideFixedFrame(t *testing.T) {
	m := newBuildFeedbackModel([]string{
		"Building proxy image...",
		"Building base image...",
	})
	m.Update(tea.WindowSizeMsg{Width: 88, Height: 12})
	for i := range 30 {
		m.Update(dockerBuildLineMsg{Line: fmt.Sprintf("\x1b[32m#%02d docker output\x1b[0m\r\a", i)})
	}

	if !m.follow || !m.viewport.AtBottom(m.bodyHeight()) {
		t.Fatal("live build output did not auto-follow")
	}
	view := m.View()
	assertFixedScreen(t, view, 12, []string{"Configure > ", "Save & Build"}, "LIVE")
	if strings.Contains(view, "\x1b[32m") || strings.Contains(view, "\a") {
		t.Fatal("unsafe Docker control sequences were not stripped")
	}

	bottom := m.viewport.ScrollOffset
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.follow {
		t.Fatal("manual keyboard scrolling should pause auto-follow")
	}
	if m.viewport.ScrollOffset != bottom-1 {
		t.Fatalf("offset after up = %d, want %d", m.viewport.ScrollOffset, bottom-1)
	}
	paused := m.viewport.ScrollOffset
	m.Update(dockerBuildLineMsg{Line: "new output while paused"})
	if m.viewport.ScrollOffset != paused {
		t.Fatalf("paused viewport jumped from %d to %d", paused, m.viewport.ScrollOffset)
	}

	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	if m.viewport.ScrollOffset >= paused {
		t.Fatal("mouse wheel did not scroll build output upward")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	if !m.follow || !m.viewport.AtBottom(m.bodyHeight()) {
		t.Fatal("End did not resume live auto-follow")
	}

	m.Update(dockerBuildStepFinishedMsg{Index: 0})
	if !strings.Contains(ansi.Strip(m.header()), "1/2") {
		t.Fatalf("header did not report completed Docker step: %q", ansi.Strip(m.header()))
	}
}

func TestBuildFeedbackFailurePreservesLogsAndShowsConcreteError(t *testing.T) {
	m := newBuildFeedbackModel([]string{"Building proxy image..."})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	m.Update(dockerBuildLineMsg{Line: "docker says nope"})
	buildErr := strings.Repeat("proxy-layer-failure ", 8) + "tail-marker"
	m.Update(dockerBuildFinishedMsg{Err: fmt.Errorf("%s", buildErr)})

	view := ansi.Strip(m.View())
	for _, want := range []string{"docker says nope", "tail-marker", "Docker build failed", "q Close"} {
		if !strings.Contains(view, want) {
			t.Fatalf("failure view missing %q:\n%s", want, view)
		}
	}
}

func assertFixedScreen(t *testing.T, rendered string, height int, headerParts []string, footerPart string) {
	t.Helper()
	lines := strings.Split(rendered, "\n")
	if len(lines) != height {
		t.Fatalf("screen height = %d, want %d:\n%s", len(lines), height, ansi.Strip(rendered))
	}
	headerEnd := min(4, len(lines))
	header := ansi.Strip(strings.Join(lines[:headerEnd], "\n"))
	for _, want := range headerParts {
		if !strings.Contains(header, want) {
			t.Fatalf("fixed header missing %q:\n%s", want, ansi.Strip(rendered))
		}
	}
	footer := ansi.Strip(lines[len(lines)-1])
	if !strings.Contains(footer, footerPart) {
		t.Fatalf("fixed footer missing %q: %q", footerPart, footer)
	}
}
