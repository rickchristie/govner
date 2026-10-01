package vm

import (
	"context"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/chatgpt"
)

// CheckHostSessions runs before launch preparation and again before VM start.
// The desktop instance lock alone cannot detect a separate host Codex CLI.
func CheckHostSessions(ctx context.Context, tool string) error {
	if !strings.EqualFold(strings.TrimSpace(tool), "chatgpt") {
		return nil
	}
	return chatgpt.CheckHostProcesses(ctx)
}
