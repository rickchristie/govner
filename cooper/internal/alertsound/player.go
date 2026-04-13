package alertsound

import (
	"log"
	"sync"
	"time"
)

const proxyAlertCooldown = 750 * time.Millisecond

// Player plays proxy approval alerts for the current Cooper session.
type Player interface {
	PlayProxyApprovalNeeded() error
	Close() error
}

// Clock provides time for cooldown decisions.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type player struct {
	mu sync.Mutex

	backend     backend
	clock       Clock
	logf        func(string, ...any)
	lastPlayAt  time.Time
	minInterval time.Duration
	playCount   int
	home        phrase
	minor       phrase
	closed      bool
	disabled    bool
}

// New creates a real proxy alert player for the current platform.
func New() (Player, error) {
	backend, err := newBackend()
	if err != nil {
		return nil, err
	}
	home, minor := buildSwitchbackPhrases()
	return &player{
		backend:     backend,
		clock:       realClock{},
		logf:        log.Printf,
		minInterval: proxyAlertCooldown,
		home:        home,
		minor:       minor,
	}, nil
}

func (p *player) PlayProxyApprovalNeeded() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed || p.disabled || p.backend == nil {
		return nil
	}

	now := p.clock.Now()
	if !p.lastPlayAt.IsZero() && now.Sub(p.lastPlayAt) < p.minInterval {
		return nil
	}

	selected, _ := resolvePhraseForPlayIndex(p.playCount, p.home, p.minor)
	if err := p.backend.Play(selected); err != nil {
		if p.logf != nil {
			p.logf("cooper: proxy alert sound disabled after playback failure: %v", err)
		}
		p.disabled = true
		return err
	}

	p.lastPlayAt = now
	p.playCount++
	return nil
}

func (p *player) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil
	}
	p.closed = true
	if p.backend == nil {
		return nil
	}
	return p.backend.Close()
}
