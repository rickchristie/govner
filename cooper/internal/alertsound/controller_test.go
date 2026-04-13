package alertsound

import (
	"errors"
	"testing"
)

type stubPlayer struct {
	plays  int
	closed int
}

func (s *stubPlayer) PlayProxyApprovalNeeded() error {
	s.plays++
	return nil
}

func (s *stubPlayer) Close() error {
	s.closed++
	return nil
}

func TestControllerDisabledByDefault(t *testing.T) {
	controller := NewController()
	factoryCalls := 0
	controller.factory = func() (Player, error) {
		factoryCalls++
		return &stubPlayer{}, nil
	}

	if err := controller.PlayProxyApprovalNeeded(); err != nil {
		t.Fatalf("PlayProxyApprovalNeeded() failed: %v", err)
	}
	if factoryCalls != 0 {
		t.Fatalf("factoryCalls = %d, want 0", factoryCalls)
	}
}

func TestControllerEnableCreatesPlayerAndPlays(t *testing.T) {
	controller := NewController()
	player := &stubPlayer{}
	factoryCalls := 0
	controller.factory = func() (Player, error) {
		factoryCalls++
		return player, nil
	}

	if err := controller.SetEnabled(true); err != nil {
		t.Fatalf("SetEnabled(true) failed: %v", err)
	}
	if err := controller.PlayProxyApprovalNeeded(); err != nil {
		t.Fatalf("PlayProxyApprovalNeeded() failed: %v", err)
	}

	if factoryCalls != 1 {
		t.Fatalf("factoryCalls = %d, want 1", factoryCalls)
	}
	if player.plays != 1 {
		t.Fatalf("plays = %d, want 1", player.plays)
	}
}

func TestControllerDisableClosesPlayer(t *testing.T) {
	controller := NewController()
	player := &stubPlayer{}
	controller.factory = func() (Player, error) { return player, nil }

	if err := controller.SetEnabled(true); err != nil {
		t.Fatalf("SetEnabled(true) failed: %v", err)
	}
	if err := controller.SetEnabled(false); err != nil {
		t.Fatalf("SetEnabled(false) failed: %v", err)
	}
	if err := controller.PlayProxyApprovalNeeded(); err != nil {
		t.Fatalf("PlayProxyApprovalNeeded() after disable failed: %v", err)
	}

	if player.closed != 1 {
		t.Fatalf("closed = %d, want 1", player.closed)
	}
	if player.plays != 0 {
		t.Fatalf("plays = %d, want 0", player.plays)
	}
}

func TestControllerEnableReturnsFactoryError(t *testing.T) {
	controller := NewController()
	controller.factory = func() (Player, error) { return nil, errors.New("audio unavailable") }

	err := controller.SetEnabled(true)
	if err == nil {
		t.Fatal("expected SetEnabled(true) to fail")
	}
	if got := err.Error(); got != "audio unavailable" {
		t.Fatalf("error = %q, want audio unavailable", got)
	}
}
