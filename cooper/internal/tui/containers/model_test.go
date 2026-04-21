package containers

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/tui/events"
)

type fakeManager struct {
	stopped    []string
	restarted  []string
	stopErr    error
	restartErr error
}

func (m *fakeManager) StopContainer(name string) error {
	m.stopped = append(m.stopped, name)
	return m.stopErr
}

func (m *fakeManager) RestartContainer(name string) error {
	m.restarted = append(m.restarted, name)
	return m.restartErr
}

func TestStopKeyRemovesContainerAndShowsSuccess(t *testing.T) {
	mgr := &fakeManager{}
	m := New(mgr)
	updated, _ := m.Update(events.ContainerStatsMsg{Stats: []app.ContainerStat{{
		Name:       "barrel-demo-claude",
		Status:     "Running",
		ShellCount: 2,
		CPUPercent: "1%",
		MemUsage:   "10MiB / 1GiB",
		TmpUsage:   "12KB",
	}}})
	m = updated.(*Model)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(*Model)
	request, ok := cmd().(events.ContainerActionRequestMsg)
	if !ok {
		t.Fatalf("request message type = %T, want ContainerActionRequestMsg", cmd())
	}
	if request.Action != "stop" || request.Name != "barrel-demo-claude" {
		t.Fatalf("request = %#v", request)
	}
	if m.actionState != actionNone {
		t.Fatalf("actionState = %v, want none before confirmation", m.actionState)
	}

	updated, cmd = m.Update(events.ContainerActionConfirmMsg{Action: "stop", Name: request.Name})
	m = updated.(*Model)
	if m.actionState != actionPending {
		t.Fatalf("actionState = %v, want pending after confirmation", m.actionState)
	}
	if !strings.Contains(m.actionText, "Stopping barrel-demo-claude") {
		t.Fatalf("actionText = %q, want stop pending message", m.actionText)
	}

	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(*Model)
	if len(mgr.stopped) != 1 || mgr.stopped[0] != "barrel-demo-claude" {
		t.Fatalf("stopped containers = %#v", mgr.stopped)
	}
	if len(m.containers) != 0 {
		t.Fatalf("expected stopped container to be removed, got %#v", m.containers)
	}
	if m.actionState != actionSuccess {
		t.Fatalf("actionState = %v, want success", m.actionState)
	}
}

func TestRestartKeyShowsSuccess(t *testing.T) {
	mgr := &fakeManager{}
	m := New(mgr)
	updated, _ := m.Update(events.ContainerStatsMsg{Stats: []app.ContainerStat{{
		Name:       "barrel-demo-opencode",
		Status:     "Running",
		ShellCount: 3,
		CPUPercent: "2%",
		MemUsage:   "20MiB / 1GiB",
		TmpUsage:   "1.5MB",
	}}})
	m = updated.(*Model)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(*Model)
	request, ok := cmd().(events.ContainerActionRequestMsg)
	if !ok {
		t.Fatalf("request message type = %T, want ContainerActionRequestMsg", cmd())
	}
	if request.Action != "restart" || request.Name != "barrel-demo-opencode" {
		t.Fatalf("request = %#v", request)
	}
	updated, cmd = m.Update(events.ContainerActionConfirmMsg{Action: "restart", Name: request.Name})
	m = updated.(*Model)
	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(*Model)

	if len(mgr.restarted) != 1 || mgr.restarted[0] != "barrel-demo-opencode" {
		t.Fatalf("restarted containers = %#v", mgr.restarted)
	}
	if got := m.containers[0].Status; got != "Running" {
		t.Fatalf("status after restart = %q, want Running", got)
	}
	if m.actionState != actionSuccess {
		t.Fatalf("actionState = %v, want success", m.actionState)
	}
}

func TestStopKeyShowsFailureAndKeepsContainer(t *testing.T) {
	mgr := &fakeManager{stopErr: errors.New("stop failed")}
	m := New(mgr)
	updated, _ := m.Update(events.ContainerStatsMsg{Stats: []app.ContainerStat{{
		Name:       "barrel-demo-claude",
		Status:     "Running",
		ShellCount: 1,
		CPUPercent: "1%",
		MemUsage:   "10MiB / 1GiB",
		TmpUsage:   "12KB",
	}}})
	m = updated.(*Model)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(*Model)
	request, ok := cmd().(events.ContainerActionRequestMsg)
	if !ok {
		t.Fatalf("request message type = %T, want ContainerActionRequestMsg", cmd())
	}
	updated, cmd = m.Update(events.ContainerActionConfirmMsg{Action: "stop", Name: request.Name})
	m = updated.(*Model)
	updated, _ = m.Update(cmd())
	m = updated.(*Model)

	if len(m.containers) != 1 {
		t.Fatalf("expected container to remain after stop failure, got %#v", m.containers)
	}
	if got := m.containers[0].Status; got != "Running" {
		t.Fatalf("status after failed stop = %q, want Running", got)
	}
	if m.actionState != actionFailed {
		t.Fatalf("actionState = %v, want failed", m.actionState)
	}
	if m.actionText == "" {
		t.Fatal("expected error text after failed stop")
	}
}

func TestRestartKeyShowsFailureAndKeepsRunningStatus(t *testing.T) {
	mgr := &fakeManager{restartErr: errors.New("restart failed")}
	m := New(mgr)
	updated, _ := m.Update(events.ContainerStatsMsg{Stats: []app.ContainerStat{{
		Name:       "barrel-demo-opencode",
		Status:     "Running",
		ShellCount: 2,
		CPUPercent: "2%",
		MemUsage:   "20MiB / 1GiB",
		TmpUsage:   "1.5MB",
	}}})
	m = updated.(*Model)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(*Model)
	request, ok := cmd().(events.ContainerActionRequestMsg)
	if !ok {
		t.Fatalf("request message type = %T, want ContainerActionRequestMsg", cmd())
	}
	updated, cmd = m.Update(events.ContainerActionConfirmMsg{Action: "restart", Name: request.Name})
	m = updated.(*Model)
	updated, _ = m.Update(cmd())
	m = updated.(*Model)

	if len(m.containers) != 1 {
		t.Fatalf("expected container to remain after restart failure, got %#v", m.containers)
	}
	if got := m.containers[0].Status; got != "Running" {
		t.Fatalf("status after failed restart = %q, want Running", got)
	}
	if m.actionState != actionFailed {
		t.Fatalf("actionState = %v, want failed", m.actionState)
	}
	if m.actionText == "" {
		t.Fatal("expected error text after failed restart")
	}
}

func TestViewShowsRunningShellsAndTmpColumns(t *testing.T) {
	m := New(&fakeManager{})
	updated, _ := m.Update(events.ContainerStatsMsg{Stats: []app.ContainerStat{{
		Name:       "barrel-demo-opencode",
		Status:     "Running",
		ShellCount: 4,
		CPUPercent: "3%",
		MemUsage:   "30MiB / 1GiB",
		TmpUsage:   "42KB",
	}}})
	m = updated.(*Model)

	view := m.View(100, 10)
	for _, want := range []string{"SHELLS", "TMP", "Running", "42KB"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q:\n%s", want, view)
		}
	}
}
