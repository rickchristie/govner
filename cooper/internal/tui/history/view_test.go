package history

import (
	"strings"
	"testing"
)

func TestRenderTypeShowsSessionApproval(t *testing.T) {
	model := NewWithCapacity(ModeAllowed, 10)
	rendered := model.renderType("session")
	if !strings.Contains(rendered, "session") {
		t.Fatalf("session approval badge = %q, want visible session label", rendered)
	}
}
