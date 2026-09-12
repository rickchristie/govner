package docker

import (
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestStartBarrelWithHomeDirRejectsRelativeHomeBeforeDocker(t *testing.T) {
	t.Parallel()
	err := StartBarrelWithHomeDir(config.DefaultConfig(), t.TempDir(), t.TempDir(), "relative-home", "claude")
	if err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("StartBarrelWithHomeDir() error = %v", err)
	}
}

func TestClipboardModeFromEnvironment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		environment []string
		want        string
		wantError   bool
	}{
		{name: "default", want: "auto"},
		{name: "off", environment: []string{"PATH=/bin", "COOPER_CLIPBOARD_MODE=off"}, want: "off"},
		{name: "last value wins", environment: []string{"COOPER_CLIPBOARD_MODE=shim", "COOPER_CLIPBOARD_MODE=x11"}, want: "x11"},
		{name: "normalized", environment: []string{"COOPER_CLIPBOARD_MODE= SHIM "}, want: "shim"},
		{name: "invalid", environment: []string{"COOPER_CLIPBOARD_MODE=unsafe"}, wantError: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := clipboardModeFromEnvironment(test.environment)
			if (err != nil) != test.wantError {
				t.Fatalf("clipboardModeFromEnvironment() error = %v, wantError %v", err, test.wantError)
			}
			if got != test.want {
				t.Fatalf("clipboardModeFromEnvironment() = %q, want %q", got, test.want)
			}
		})
	}
}
