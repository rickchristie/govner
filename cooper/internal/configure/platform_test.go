package configure

import (
	"strings"
	"testing"
)

func TestDockerStartHint(t *testing.T) {
	tests := []struct {
		goos string
		want string
	}{
		{goos: "darwin", want: "open -a Docker"},
		{goos: "windows", want: "Docker Desktop"},
		{goos: "linux", want: "sudo systemctl start docker"},
	}

	for _, tt := range tests {
		if got := dockerStartHint(tt.goos); !strings.Contains(got, tt.want) {
			t.Errorf("dockerStartHint(%q) = %q, want substring %q", tt.goos, got, tt.want)
		}
	}
}

func TestPortForwardInfoText(t *testing.T) {
	darwin := portForwardInfoText("darwin")
	if !strings.Contains(darwin, "Docker Desktop") || !strings.Contains(darwin, "127.0.0.1") {
		t.Fatalf("darwin port forward hint missing macOS guidance: %q", darwin)
	}
	if strings.Contains(darwin, "NOT be accessible") {
		t.Fatalf("darwin port forward hint should not warn that 127.0.0.1 is inaccessible: %q", darwin)
	}

	linux := portForwardInfoText("linux")
	if !strings.Contains(linux, "0.0.0.0") || !strings.Contains(linux, "HostRelay") {
		t.Fatalf("linux port forward hint missing Linux guidance: %q", linux)
	}
}
