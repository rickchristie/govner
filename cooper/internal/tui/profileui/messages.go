package profileui

import "github.com/rickchristie/govner/cooper/internal/profiles"

// Results are exported so the root shell can route late results to this
// screen even when another tab is active.
type ProfilesListedMsg struct {
	Items []profiles.Summary
	Err   error
}

type ProfileActionCompletedMsg struct {
	Action string
	Save   profiles.SaveRequest
	Load   profiles.LoadRequest
	Result profiles.Result
	Err    error
}
