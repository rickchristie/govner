package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rickchristie/govner/cooper/internal/profiles"
)

func (a *MockApp) ListProfiles(context.Context) ([]profiles.Summary, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]profiles.Summary(nil), a.ProfilesVal...), a.ProfilesErr
}

func (a *MockApp) SaveProfile(ctx context.Context, request profiles.SaveRequest) (profiles.Result, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.SaveProfileCalls = append(a.SaveProfileCalls, request)
	return a.ProfileResult, a.ProfileActionErr
}

func (a *MockApp) LoadProfile(ctx context.Context, request profiles.LoadRequest) (profiles.Result, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.LoadProfileCalls = append(a.LoadProfileCalls, request)
	return a.ProfileResult, a.ProfileActionErr
}

func (a *MockApp) DeleteProfile(ctx context.Context, harness, name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.DeleteProfileCalls = append(a.DeleteProfileCalls, harness+"/"+name)
	return a.ProfileActionErr
}

func (a *TestApp) ListProfiles(context.Context) ([]profiles.Summary, error) {
	a.profileMu.Lock()
	defer a.profileMu.Unlock()
	if a.profileScenario == "error" {
		return nil, errors.New("Cannot check running containers. Connect to the Docker daemon and refresh the list.")
	}
	return append([]profiles.Summary(nil), a.profileItems...), nil
}

func (a *TestApp) SaveProfile(ctx context.Context, request profiles.SaveRequest) (profiles.Result, error) {
	a.profileMu.Lock()
	defer a.profileMu.Unlock()
	if a.profileScenario == "conflict" && request.ConflictChoice == "" {
		return profiles.Result{}, &profiles.Issue{Kind: profiles.StateConflict, Message: "The host and saved profile have different changes. Both copies are preserved."}
	}
	if a.profileScenario == "busy" {
		return profiles.Result{}, &profiles.Issue{Kind: profiles.StateInUse, Message: "A running session uses this state. Stop it before saving."}
	}
	if a.profileScenario == "unmapped" && request.NewName == "" {
		return profiles.Result{}, &profiles.Issue{Kind: profiles.NameRequired, Message: "This account has no profile"}
	}
	for _, item := range a.profileItems {
		if item.Harness == request.Harness && item.Loaded && a.profileScenario != "unmapped" {
			if item.Pending {
				return profiles.Result{}, &profiles.Issue{Kind: profiles.IdentityUnknown, Message: "Fixture: log in on the host before saving this profile"}
			}
			return profiles.Result{Saved: item.Name}, nil
		}
	}
	if len(a.profileItems) == 0 {
		request.NewName = "Default"
	}
	if request.NewName == "" {
		return profiles.Result{}, &profiles.Issue{Kind: profiles.NameRequired, Message: "This account has no profile"}
	}
	if err := profiles.ValidateName(request.NewName); err != nil {
		return profiles.Result{}, err
	}
	for index := range a.profileItems {
		if a.profileItems[index].Harness == request.Harness {
			a.profileItems[index].Loaded = false
		}
	}
	a.profileScenario = ""
	a.profileItems = append(a.profileItems, profiles.Summary{ID: "fixture-" + request.NewName, Harness: request.Harness, Name: request.NewName, Account: "new@example.test", Loaded: true})
	return profiles.Result{Saved: request.NewName, Created: true}, nil
}

// SetProfileScenario selects only fixed, fabricated account state. It does
// not call the profile service or read a host account.
func (a *TestApp) SetProfileScenario(name string) error {
	a.profileMu.Lock()
	defer a.profileMu.Unlock()
	switch name {
	case "", "populated", "unmapped", "conflict", "busy", "error":
		a.profileItems = ProfileStory()
	case "empty":
		a.profileItems = nil
	default:
		return errors.New("profile scenario must be populated, empty, unmapped, conflict, busy, or error")
	}
	a.profileScenario = name
	return nil
}

func (a *TestApp) LoadProfile(ctx context.Context, request profiles.LoadRequest) (profiles.Result, error) {
	if err := profiles.ValidateName(request.Name); err != nil {
		return profiles.Result{}, err
	}
	a.profileMu.Lock()
	defer a.profileMu.Unlock()
	result := profiles.Result{Loaded: request.Name}
	found := false
	for index := range a.profileItems {
		item := &a.profileItems[index]
		if item.Harness != request.Harness {
			continue
		}
		if item.Loaded {
			result.Saved = item.Name
		}
		item.Loaded = strings.EqualFold(item.Name, request.Name)
		if item.Loaded {
			found = true
			result.Pending = item.Pending
			result.Loaded = item.Name
		}
	}
	if !found {
		result.Created, result.Pending = true, true
		a.profileItems = append(a.profileItems, profiles.Summary{ID: "fixture-new", Harness: request.Harness, Name: request.Name, Loaded: true, Pending: true})
	}
	return result, nil
}

func (a *TestApp) DeleteProfile(ctx context.Context, harness, name string) error {
	a.profileMu.Lock()
	defer a.profileMu.Unlock()
	for index, item := range a.profileItems {
		if item.Harness != harness || item.Name != name {
			continue
		}
		if item.Loaded || item.InUse {
			return &profiles.Issue{Kind: profiles.StateInUse, Message: "Load another profile and stop its sessions before deletion"}
		}
		a.profileItems = append(a.profileItems[:index], a.profileItems[index+1:]...)
		return nil
	}
	return nil
}

func ProfileStory() []profiles.Summary {
	date := time.Date(2026, 9, 1, 9, 30, 0, 0, time.UTC)
	return []profiles.Summary{
		{ID: "fixture-claude-default", Harness: "claude", Name: "Default", Account: "personal@example.test / Personal", Loaded: true, Saved: date},
		{ID: "fixture-claude-work", Harness: "claude", Name: "Work", Account: "developer@example.test / Example Enterprise", Saved: date},
		{ID: "fixture-codex-default", Harness: "codex", Name: "Default", Account: "personal@example.test / Personal", Loaded: true, Saved: date},
		{ID: "fixture-codex-work", Harness: "codex", Name: "Work", Account: "developer@example.test / Example Enterprise", InUse: true, Saved: date},
		{ID: "fixture-opencode-playground", Harness: "opencode", Name: "Playground", Loaded: true, Pending: true, Saved: date},
	}
}
