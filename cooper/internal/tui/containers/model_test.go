package containers

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/tui/events"
)

type fakeManager struct {
	stopped    []string
	restarted  []string
	stopErr    error
	restartErr error
}

func (m *fakeManager) StopWorkload(name string) error {
	m.stopped = append(m.stopped, name)
	return m.stopErr
}

func (m *fakeManager) RestartWorkload(name string) error {
	m.restarted = append(m.restarted, name)
	return m.restartErr
}

func TestStopKeyRemovesContainerAndShowsSuccess(t *testing.T) {
	mgr := &fakeManager{}
	m := New(mgr)
	updated, _ := m.Update(events.WorkloadStatsMsg{Stats: []app.WorkloadStat{{
		ID:           "barrel-demo-claude",
		Kind:         app.WorkloadCLI,
		Status:       "Running",
		ShellCount:   2,
		CPUPercent:   "1%",
		MemUsage:     "10MiB / 1GiB",
		StorageUsage: "12KB",
	}}})
	m = updated.(*Model)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(*Model)
	request, ok := cmd().(events.WorkloadActionRequestMsg)
	if !ok {
		t.Fatalf("request message type = %T, want WorkloadActionRequestMsg", cmd())
	}
	if request.Action != "stop" || request.Name != "barrel-demo-claude" {
		t.Fatalf("request = %#v", request)
	}
	if m.actionState != actionNone {
		t.Fatalf("actionState = %v, want none before confirmation", m.actionState)
	}

	updated, cmd = m.Update(events.WorkloadActionConfirmMsg{Action: "stop", Name: request.Name})
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
		t.Fatalf("stopped workloads = %#v", mgr.stopped)
	}
	if len(m.workloads) != 0 {
		t.Fatalf("expected stopped workload to be removed, got %#v", m.workloads)
	}
	if m.actionState != actionSuccess {
		t.Fatalf("actionState = %v, want success", m.actionState)
	}
}

func TestRestartKeyShowsSuccess(t *testing.T) {
	mgr := &fakeManager{}
	m := New(mgr)
	updated, _ := m.Update(events.WorkloadStatsMsg{Stats: []app.WorkloadStat{{
		ID:           "barrel-demo-opencode",
		Kind:         app.WorkloadCLI,
		Status:       "Running",
		ShellCount:   3,
		CPUPercent:   "2%",
		MemUsage:     "20MiB / 1GiB",
		StorageUsage: "1.5MB",
	}}})
	m = updated.(*Model)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(*Model)
	request, ok := cmd().(events.WorkloadActionRequestMsg)
	if !ok {
		t.Fatalf("request message type = %T, want WorkloadActionRequestMsg", cmd())
	}
	if request.Action != "restart" || request.Name != "barrel-demo-opencode" {
		t.Fatalf("request = %#v", request)
	}
	updated, cmd = m.Update(events.WorkloadActionConfirmMsg{Action: "restart", Name: request.Name})
	m = updated.(*Model)
	msg := cmd()
	updated, _ = m.Update(msg)
	m = updated.(*Model)

	if len(mgr.restarted) != 1 || mgr.restarted[0] != "barrel-demo-opencode" {
		t.Fatalf("restarted workloads = %#v", mgr.restarted)
	}
	if got := m.workloads[0].Status; got != "Running" {
		t.Fatalf("status after restart = %q, want Running", got)
	}
	if m.actionState != actionSuccess {
		t.Fatalf("actionState = %v, want success", m.actionState)
	}
}

func TestStopKeyShowsFailureAndKeepsContainer(t *testing.T) {
	mgr := &fakeManager{stopErr: errors.New("stop failed")}
	m := New(mgr)
	updated, _ := m.Update(events.WorkloadStatsMsg{Stats: []app.WorkloadStat{{
		ID:           "barrel-demo-claude",
		Kind:         app.WorkloadCLI,
		Status:       "Running",
		ShellCount:   1,
		CPUPercent:   "1%",
		MemUsage:     "10MiB / 1GiB",
		StorageUsage: "12KB",
	}}})
	m = updated.(*Model)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(*Model)
	request, ok := cmd().(events.WorkloadActionRequestMsg)
	if !ok {
		t.Fatalf("request message type = %T, want WorkloadActionRequestMsg", cmd())
	}
	updated, cmd = m.Update(events.WorkloadActionConfirmMsg{Action: "stop", Name: request.Name})
	m = updated.(*Model)
	updated, _ = m.Update(cmd())
	m = updated.(*Model)

	if len(m.workloads) != 1 {
		t.Fatalf("expected workload to remain after stop failure, got %#v", m.workloads)
	}
	if got := m.workloads[0].Status; got != "Running" {
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
	updated, _ := m.Update(events.WorkloadStatsMsg{Stats: []app.WorkloadStat{{
		ID:           "barrel-demo-opencode",
		Kind:         app.WorkloadCLI,
		Status:       "Running",
		ShellCount:   2,
		CPUPercent:   "2%",
		MemUsage:     "20MiB / 1GiB",
		StorageUsage: "1.5MB",
	}}})
	m = updated.(*Model)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(*Model)
	request, ok := cmd().(events.WorkloadActionRequestMsg)
	if !ok {
		t.Fatalf("request message type = %T, want WorkloadActionRequestMsg", cmd())
	}
	updated, cmd = m.Update(events.WorkloadActionConfirmMsg{Action: "restart", Name: request.Name})
	m = updated.(*Model)
	updated, _ = m.Update(cmd())
	m = updated.(*Model)

	if len(m.workloads) != 1 {
		t.Fatalf("expected workload to remain after restart failure, got %#v", m.workloads)
	}
	if got := m.workloads[0].Status; got != "Running" {
		t.Fatalf("status after failed restart = %q, want Running", got)
	}
	if m.actionState != actionFailed {
		t.Fatalf("actionState = %v, want failed", m.actionState)
	}
	if m.actionText == "" {
		t.Fatal("expected error text after failed restart")
	}
}

func TestViewShowsRunningShellsAndStorageColumns(t *testing.T) {
	m := New(&fakeManager{})
	updated, _ := m.Update(events.WorkloadStatsMsg{Stats: []app.WorkloadStat{{
		ID:           "barrel-demo-opencode",
		Kind:         app.WorkloadCLI,
		Status:       "Running",
		ShellCount:   4,
		CPUPercent:   "3%",
		MemUsage:     "30MiB / 1GiB",
		StorageUsage: "42KB",
	}}})
	m = updated.(*Model)

	view := m.View(100, 10)
	for _, want := range []string{"SHELLS", "STORAGE", "Running", "42KB"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected view to contain %q:\n%s", want, view)
		}
	}
}

func TestViewKeepsRuntimeRowsWithinSmallTerminalWidth(t *testing.T) {
	t.Parallel()
	m := New(&fakeManager{})
	updated, _ := m.Update(events.WorkloadStatsMsg{Stats: []app.WorkloadStat{
		{ID: "cooper-proxy", Kind: app.WorkloadProxy, Status: "Running", CPUPercent: "0.4%", MemUsage: "42MiB / 256MiB"},
		{ID: "barrel-demo-claude", Kind: app.WorkloadCLI, Status: "Running", ShellCount: 2, CPUPercent: "1.2%", MemUsage: "380MiB / 4GiB", StorageUsage: "18MiB"},
		{ID: "cooper-vm-govner-codex-aabbccddeeff", Kind: app.WorkloadVM, Depth: 1, Status: "Running", ShellCount: 1, CPUPercent: "7.8%", MemUsage: "4.2GiB / 12.8GiB", StorageUsage: "3.1GiB"},
	}})
	m = updated.(*Model)

	const width = 80
	view := m.View(width, 16)
	for lineNumber, line := range strings.Split(view, "\n") {
		if got := ansi.StringWidth(line); got > width {
			t.Fatalf("line %d width = %d, want at most %d:\n%s", lineNumber+1, got, width, view)
		}
	}
}

func TestViewShowsVMKindDepthAndHealth(t *testing.T) {
	t.Parallel()
	m := New(&fakeManager{})
	updated, _ := m.Update(events.WorkloadStatsMsg{Stats: []app.WorkloadStat{
		{ID: "cooper-proxy", Kind: app.WorkloadProxy, Status: "Running"},
		{
			ID: "cooper-vm-project-codex-aabbccddeeff", Kind: app.WorkloadVM,
			Tool: "codex", Workspace: "/work/project", Depth: 1,
			Status: "Unhealthy", HealthReason: "Guest is not ready",
		},
	}})
	m = updated.(*Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(*Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(*Model)
	view := m.View(110, 20)
	for _, want := range []string{"KIND", "PROXY", "VM1", "codex", "/work/project", "Guest is not ready"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view does not contain %q:\n%s", want, view)
		}
	}
}
