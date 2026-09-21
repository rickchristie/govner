//go:build !linux

package profiles

import (
	"strings"
	"testing"
)

func TestProfilesAreDisabledBeforeAccessingState(t *testing.T) {
	service := New(Options{})
	checks := []func() error{
		Supported,
		func() error { _, err := service.Save(t.Context(), SaveRequest{Harness: "codex"}); return err },
		func() error {
			_, err := service.Load(t.Context(), LoadRequest{Harness: "codex", Name: "Work", Confirmed: true})
			return err
		},
		func() error { _, err := service.List(t.Context()); return err },
		func() error { _, err := service.Select(t.Context(), "codex", "Work"); return err },
		func() error { _, err := service.SelectID(t.Context(), "codex", "012345678901234567890123"); return err },
		func() error { return service.Backup(t.Context(), "/unused-backup") },
		func() error {
			_, err := service.Restore(t.Context(), RestoreRequest{Harness: "codex", Name: "Work", Confirmed: true})
			return err
		},
		func() error { return service.Delete(t.Context(), "codex", "Work") },
		func() error { return service.Recover(t.Context()) },
		func() error { return service.PruneRecovery(t.Context()) },
	}
	for _, check := range checks {
		if err := check(); err == nil || !strings.Contains(err.Error(), "require Linux") {
			t.Fatalf("profile operation was not disabled: %v", err)
		}
	}
}
