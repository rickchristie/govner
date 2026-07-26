package proxymon

import (
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/tui/events"
)

type fakeACLApprover struct {
	approved    []string
	denied      []string
	allowCalls  []string
	revokeCalls []string
	allowErr    error
	domains     map[string]struct{}
}

func newFakeACLApprover(domains ...string) *fakeACLApprover {
	fake := &fakeACLApprover{domains: make(map[string]struct{})}
	for _, domain := range domains {
		fake.domains[canonicalDomain(domain)] = struct{}{}
	}
	return fake
}

func (f *fakeACLApprover) ApproveRequest(id string) {
	f.approved = append(f.approved, id)
}

func (f *fakeACLApprover) DenyRequest(id string) {
	f.denied = append(f.denied, id)
}

func (f *fakeACLApprover) PendingRequests() []*app.PendingRequest {
	return nil
}

func (f *fakeACLApprover) AllowDomainForSession(domain string) (string, error) {
	f.allowCalls = append(f.allowCalls, domain)
	if f.allowErr != nil {
		return "", f.allowErr
	}
	normalized := canonicalDomain(domain)
	f.domains[normalized] = struct{}{}
	return normalized, nil
}

func (f *fakeACLApprover) RevokeDomainForSession(domain string) bool {
	f.revokeCalls = append(f.revokeCalls, domain)
	normalized := canonicalDomain(domain)
	if _, ok := f.domains[normalized]; !ok {
		return false
	}
	delete(f.domains, normalized)
	return true
}

func (f *fakeACLApprover) IsDomainAllowedForSession(domain string) bool {
	_, ok := f.domains[canonicalDomain(domain)]
	return ok
}

func (f *fakeACLApprover) SessionAllowedDomains() []string {
	domains := make([]string, 0, len(f.domains))
	for domain := range f.domains {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	return domains
}

func TestSessionAllowConfirmsScopeAndRemovesSameDomainRequests(t *testing.T) {
	approver := newFakeACLApprover()
	model := New(approver, 30*time.Second)
	for _, request := range []app.ACLRequest{
		{ID: "selected", Domain: "API.Example.com.", Timestamp: time.Now()},
		{ID: "same-domain", Domain: "api.example.com", Timestamp: time.Now()},
		{ID: "other", Domain: "other.example.com", Timestamp: time.Now()},
	} {
		updated, _ := model.Update(events.ACLRequestMsg{Request: request})
		model = updated.(*Model)
	}

	updated, _ := model.Update(runeKey('w'))
	model = updated.(*Model)
	if !model.ModalActive() || model.dialog != sessionDialogAllow {
		t.Fatal("w did not open the session confirmation dialog")
	}
	if len(approver.allowCalls) != 0 {
		t.Fatal("domain was allowed before confirmation")
	}
	view := model.View(110, 28)
	for _, want := range []string{
		"Allow for This Session?",
		"API.Example.com.",
		"every barrel",
		"No proxy settings will be changed",
		"Allow Exact Host",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("confirmation view missing %q:\n%s", want, view)
		}
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(*Model)
	if model.ModalActive() {
		t.Fatal("confirmation dialog remained open after allow")
	}
	if len(approver.allowCalls) != 1 || approver.allowCalls[0] != "API.Example.com." {
		t.Fatalf("allow calls = %v, want selected exact domain", approver.allowCalls)
	}
	if len(model.pending) != 1 || model.pending[0].Request.ID != "other" {
		t.Fatalf("pending requests after session allow = %+v, want only other domain", model.pending)
	}
	if got := model.sessionDomains; len(got) != 1 || got[0] != "api.example.com" {
		t.Fatalf("session domains = %v, want api.example.com", got)
	}
	if !strings.Contains(model.View(110, 28), "allowed until Cooper exits") {
		t.Fatal("success feedback is not visible after session allow")
	}
}

func TestSessionAllowCancellationAndFailurePreserveRequest(t *testing.T) {
	approver := newFakeACLApprover()
	model := New(approver, 30*time.Second)
	updated, _ := model.Update(events.ACLRequestMsg{Request: app.ACLRequest{
		ID: "request", Domain: "api.example.com", Timestamp: time.Now(),
	}})
	model = updated.(*Model)

	updated, _ = model.Update(runeKey('w'))
	model = updated.(*Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model = updated.(*Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(*Model)
	if len(approver.allowCalls) != 0 || len(model.pending) != 1 {
		t.Fatal("cancelled session allow changed application or pending state")
	}

	approver.allowErr = errors.New("invalid exact hostname")
	updated, _ = model.Update(runeKey('w'))
	model = updated.(*Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(*Model)
	if len(model.pending) != 1 {
		t.Fatal("failed session allow removed the pending request")
	}
	if !strings.Contains(model.View(100, 24), "invalid exact hostname") {
		t.Fatal("session allow failure is not surfaced in the monitor")
	}
}

func TestSessionManagerListsAndRevokesExactDomains(t *testing.T) {
	approver := newFakeACLApprover("z.example.com", "a.example.com")
	model := New(approver, 30*time.Second)

	updated, _ := model.Update(runeKey('s'))
	model = updated.(*Model)
	if !model.ModalActive() || model.dialog != sessionDialogManage {
		t.Fatal("s did not open the session manager")
	}
	view := model.View(110, 28)
	for _, want := range []string{
		"Session Access",
		"a.example.com",
		"z.example.com",
		"automatically approved for every barrel",
		"until this Cooper instance exits",
		"Revoke",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("session manager missing %q:\n%s", want, view)
		}
	}

	// Domains are sorted, so the second row is z.example.com.
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(*Model)
	updated, _ = model.Update(runeKey('r'))
	model = updated.(*Model)
	if len(approver.revokeCalls) != 1 || approver.revokeCalls[0] != "z.example.com" {
		t.Fatalf("revoke calls = %v, want z.example.com", approver.revokeCalls)
	}
	if got := approver.SessionAllowedDomains(); len(got) != 1 || got[0] != "a.example.com" {
		t.Fatalf("remaining session domains = %v, want a.example.com", got)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(*Model)
	if model.ModalActive() {
		t.Fatal("Esc did not close the session manager")
	}
}

func TestResolvedOrSessionAllowedRequestsDoNotRemainPending(t *testing.T) {
	approver := newFakeACLApprover("session.example.com")
	model := New(approver, 30*time.Second)

	updated, _ := model.Update(events.ACLRequestMsg{Request: app.ACLRequest{
		ID: "stale", Domain: "SESSION.EXAMPLE.COM.", Timestamp: time.Now(),
	}})
	model = updated.(*Model)
	if len(model.pending) != 0 {
		t.Fatal("queued request for an already session-allowed domain was shown")
	}

	updated, _ = model.Update(events.ACLRequestMsg{Request: app.ACLRequest{
		ID: "manual", Domain: "manual.example.com", Timestamp: time.Now(),
	}})
	model = updated.(*Model)
	updated, _ = model.Update(events.ACLDecisionMsg{Event: app.DecisionEvent{
		Request:  app.ACLRequest{ID: "manual", Domain: "manual.example.com"},
		Decision: app.DecisionAllow,
		Reason:   "session",
	}})
	model = updated.(*Model)
	if len(model.pending) != 0 {
		t.Fatal("resolved request remained visible in the monitor")
	}
}

func TestSessionAccessRailFitsAndExplainsLifetime(t *testing.T) {
	approver := newFakeACLApprover("api.example.com", "packages.example.net")
	model := New(approver, 30*time.Second)
	const width = 96
	view := model.View(width, 22)
	for _, want := range []string{"SESSION ACCESS", "2 exact hosts", "cleared on exit"} {
		if !strings.Contains(view, want) {
			t.Fatalf("session rail missing %q:\n%s", want, view)
		}
	}
	for index, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("line %d width = %d, exceeds %d:\n%s", index, got, width, line)
		}
	}
}

func TestSessionAllowWithoutSelectionShowsConcreteError(t *testing.T) {
	model := New(newFakeACLApprover(), 30*time.Second)
	updated, _ := model.Update(runeKey('w'))
	model = updated.(*Model)
	if model.ModalActive() {
		t.Fatal("session dialog opened without a pending request")
	}
	if !strings.Contains(model.View(90, 20), "Select a pending request") {
		t.Fatal("missing selection error is not visible")
	}
}

func runeKey(value rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{value}}
}
