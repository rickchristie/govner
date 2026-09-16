package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/tui/events"
	"github.com/rickchristie/govner/cooper/internal/tui/proxymon"
	"github.com/rickchristie/govner/cooper/internal/tui/theme"
)

func TestSessionManagerRemainsInsideVisibleRootFrame(t *testing.T) {
	a := app.NewMockApp(config.DefaultConfig(), t.TempDir())
	m := NewModel(a)
	m.SetProxyMonModel(proxymon.New(a, 30*time.Second))
	m.SetSize(80, 24)
	m.SetActiveTab(theme.TabMonitor)
	m.Update(events.ACLRequestMsg{Request: app.ACLRequest{ID: "request", Domain: "api.example.test", Port: "443", SourceIP: "172.19.0.3", Timestamp: time.Now()}})
	_, allow := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	if allow == nil {
		t.Fatal("missing session allow command")
	}
	m.Update(allow())
	if !a.IsDomainAllowedForSession("api.example.test") {
		t.Fatal("w did not allow the exact host")
	}
	for i := 0; i < 25; i++ {
		a.AllowDomainForSession(fmt.Sprintf("host-%02d.test", i))
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	view := ansi.Strip(m.View())
	for _, want := range []string{"Session Access", "api.example.test", "r Remove host", "until Cooper exits"} {
		if !strings.Contains(view, want) {
			t.Fatalf("visible root frame lacks %q:\n%s", want, view)
		}
	}
	for i := 0; i < 25; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if !strings.Contains(m.View(), "host-24.test") {
		t.Fatal("last session host is unreachable")
	}
	_, remove := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if remove == nil {
		t.Fatal("missing session removal command")
	}
	m.Update(remove())
	if a.IsDomainAllowedForSession("host-24.test") {
		t.Fatal("manager did not remove the selected host")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !strings.Contains(m.View(), "25 exact hosts") {
		t.Fatal("host count was not refreshed")
	}
}
