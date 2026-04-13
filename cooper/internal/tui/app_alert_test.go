package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/tui/events"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

type fakeAlertPlayer struct{ count int }

func (f *fakeAlertPlayer) PlayProxyApprovalNeeded() error {
	f.count++
	return nil
}

type alertRecordingSubModel struct{ messages []tea.Msg }

func (m *alertRecordingSubModel) Init() tea.Cmd { return nil }

func (m *alertRecordingSubModel) Update(msg tea.Msg) (theme.SubModel, tea.Cmd) {
	m.messages = append(m.messages, msg)
	return m, nil
}

func (m *alertRecordingSubModel) View(_, _ int) string { return "" }

func runCmdAndBatchSubcommands(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		msgs := make([]tea.Msg, 0, len(batch))
		for _, sub := range batch {
			if sub == nil {
				continue
			}
			msgs = append(msgs, sub())
		}
		return msgs
	}
	return []tea.Msg{msg}
}

func TestPlayProxyAlertCmd(t *testing.T) {
	player := &fakeAlertPlayer{}
	cmd := playProxyAlertCmd(player)
	if cmd == nil {
		t.Fatal("expected alert command")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("command message = %#v, want nil", msg)
	}
	if player.count != 1 {
		t.Fatalf("count = %d, want 1", player.count)
	}
}

func TestACLRequestTriggersAlert(t *testing.T) {
	player := &fakeAlertPlayer{}
	recorder := &alertRecordingSubModel{}
	m := NewModel(nil)
	m.SetAlertPlayer(player)
	m.SetProxyMonModel(recorder)

	req := events.ACLRequestMsg{Request: app.ACLRequest{ID: "req-1", Domain: "example.com", Timestamp: time.Now()}}
	updated, cmd := m.Update(req)
	if updated != m {
		t.Fatal("expected root model to update in place")
	}
	msgs := runCmdAndBatchSubcommands(t, cmd)

	if player.count != 1 {
		t.Fatalf("count = %d, want 1", player.count)
	}
	if len(recorder.messages) != 1 {
		t.Fatalf("proxy monitor messages = %d, want 1", len(recorder.messages))
	}
	if _, ok := recorder.messages[0].(events.ACLRequestMsg); !ok {
		t.Fatalf("proxy monitor message type = %T, want ACLRequestMsg", recorder.messages[0])
	}
	_ = msgs
}

func TestACLRequestTriggersAlertOutsideMonitorTab(t *testing.T) {
	player := &fakeAlertPlayer{}
	m := NewModel(nil)
	m.SetAlertPlayer(player)
	m.SetActiveTab(theme.TabAbout)

	_, cmd := m.Update(events.ACLRequestMsg{Request: app.ACLRequest{ID: "req-1", Timestamp: time.Now()}})
	runCmdAndBatchSubcommands(t, cmd)

	if player.count != 1 {
		t.Fatalf("count = %d, want 1", player.count)
	}
}

func TestACLDecisionDoesNotTriggerAlert(t *testing.T) {
	player := &fakeAlertPlayer{}
	m := NewModel(nil)
	m.SetAlertPlayer(player)

	_, cmd := m.Update(events.ACLDecisionMsg{})
	runCmdAndBatchSubcommands(t, cmd)

	if player.count != 0 {
		t.Fatalf("count = %d, want 0", player.count)
	}
}

func TestACLRequestWithNilAlertPlayerIsSafe(t *testing.T) {
	m := NewModel(nil)
	_, cmd := m.Update(events.ACLRequestMsg{Request: app.ACLRequest{ID: "req-1", Timestamp: time.Now()}})
	runCmdAndBatchSubcommands(t, cmd)
}
