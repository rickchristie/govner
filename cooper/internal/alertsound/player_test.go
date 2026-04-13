package alertsound

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type recordingBackend struct {
	attempts   int
	plays      []phraseID
	err        error
	closeCount int
}

func (b *recordingBackend) Play(p phrase) error {
	b.attempts++
	if b.err != nil {
		return b.err
	}
	b.plays = append(b.plays, p.ID)
	return nil
}

func (b *recordingBackend) Close() error {
	b.closeCount++
	return nil
}

func TestPlayerSwitchesAfterEightAudiblePlays(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	backend := &recordingBackend{}
	p := &player{
		backend:     backend,
		clock:       clock,
		minInterval: proxyAlertCooldown,
		home:        phrase{ID: phraseHome},
		minor:       phrase{ID: phraseMinor},
	}

	for range 17 {
		clock.now = clock.now.Add(800 * time.Millisecond)
		if err := p.PlayProxyApprovalNeeded(); err != nil {
			t.Fatalf("PlayProxyApprovalNeeded() failed: %v", err)
		}
	}

	if backend.attempts != 17 {
		t.Fatalf("attempts = %d, want 17", backend.attempts)
	}
	if len(backend.plays) != 17 {
		t.Fatalf("plays = %d, want 17", len(backend.plays))
	}
	for i := 0; i < 8; i++ {
		if backend.plays[i] != phraseHome {
			t.Fatalf("play %d = %s, want %s", i+1, backend.plays[i], phraseHome)
		}
	}
	for i := 8; i < 16; i++ {
		if backend.plays[i] != phraseMinor {
			t.Fatalf("play %d = %s, want %s", i+1, backend.plays[i], phraseMinor)
		}
	}
	if backend.plays[16] != phraseHome {
		t.Fatalf("play 17 = %s, want %s", backend.plays[16], phraseHome)
	}
}

func TestPlayerCooldownDoesNotAdvanceSequence(t *testing.T) {
	base := time.Unix(200, 0)
	clock := &fakeClock{now: base}
	backend := &recordingBackend{}
	p := &player{
		backend:     backend,
		clock:       clock,
		minInterval: proxyAlertCooldown,
		home:        phrase{ID: phraseHome},
		minor:       phrase{ID: phraseMinor},
	}

	if err := p.PlayProxyApprovalNeeded(); err != nil {
		t.Fatalf("first play failed: %v", err)
	}
	clock.now = base.Add(100 * time.Millisecond)
	if err := p.PlayProxyApprovalNeeded(); err != nil {
		t.Fatalf("suppressed play returned error: %v", err)
	}
	clock.now = base.Add(proxyAlertCooldown + 50*time.Millisecond)
	if err := p.PlayProxyApprovalNeeded(); err != nil {
		t.Fatalf("third play failed: %v", err)
	}

	if backend.attempts != 2 {
		t.Fatalf("attempts = %d, want 2", backend.attempts)
	}
	if len(backend.plays) != 2 {
		t.Fatalf("plays = %d, want 2", len(backend.plays))
	}
	if backend.plays[0] != phraseHome || backend.plays[1] != phraseHome {
		t.Fatalf("plays = %v, want two home phrases", backend.plays)
	}
}

func TestPlayerDisablesAfterPlaybackFailure(t *testing.T) {
	clock := &fakeClock{now: time.Unix(300, 0)}
	backend := &recordingBackend{err: errors.New("boom")}
	var lines []string
	p := &player{
		backend:     backend,
		clock:       clock,
		logf:        func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) },
		minInterval: proxyAlertCooldown,
		home:        phrase{ID: phraseHome},
		minor:       phrase{ID: phraseMinor},
	}

	if err := p.PlayProxyApprovalNeeded(); err == nil {
		t.Fatal("expected playback failure")
	}
	clock.now = clock.now.Add(800 * time.Millisecond)
	if err := p.PlayProxyApprovalNeeded(); err != nil {
		t.Fatalf("disabled player should become no-op, got %v", err)
	}

	if p.playCount != 0 {
		t.Fatalf("playCount = %d, want 0", p.playCount)
	}
	if backend.attempts != 1 {
		t.Fatalf("attempts = %d, want 1", backend.attempts)
	}
	if len(lines) != 1 {
		t.Fatalf("log lines = %d, want 1", len(lines))
	}
	if !strings.Contains(lines[0], "proxy alert sound disabled after playback failure") {
		t.Fatalf("log line = %q, want playback failure message", lines[0])
	}
}

func TestPlayerClosePreventsPlayback(t *testing.T) {
	clock := &fakeClock{now: time.Unix(400, 0)}
	backend := &recordingBackend{}
	p := &player{
		backend: backend,
		clock:   clock,
		home:    phrase{ID: phraseHome},
		minor:   phrase{ID: phraseMinor},
	}

	if err := p.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}
	if err := p.PlayProxyApprovalNeeded(); err != nil {
		t.Fatalf("PlayProxyApprovalNeeded() after close returned error: %v", err)
	}

	if backend.attempts != 0 {
		t.Fatalf("attempts = %d, want 0", backend.attempts)
	}
	if backend.closeCount != 1 {
		t.Fatalf("closeCount = %d, want 1", backend.closeCount)
	}
}
