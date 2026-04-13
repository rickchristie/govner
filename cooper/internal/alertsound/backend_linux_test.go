//go:build linux

package alertsound

import (
	"errors"
	"testing"
)

func TestNewBackendReturnsPulseError(t *testing.T) {
	prev := newPulseClient
	newPulseClient = func() (pulseClient, error) {
		return nil, errors.New("pulse unavailable")
	}
	defer func() { newPulseClient = prev }()

	_, err := newBackend()
	if err == nil {
		t.Fatal("expected newBackend to fail")
	}
	if err.Error() != "pulse unavailable" {
		t.Fatalf("error = %v, want pulse unavailable", err)
	}
}
