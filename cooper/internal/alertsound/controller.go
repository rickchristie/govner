package alertsound

import "sync"

// Controller manages whether proxy approval alerts are enabled at runtime.
// When disabled it stays silent; when enabled it lazily creates the platform
// player and keeps using it for the rest of the session.
type Controller struct {
	mu      sync.Mutex
	factory func() (Player, error)
	player  Player
	enabled bool
}

// NewController creates a runtime-toggleable alert controller.
func NewController() *Controller {
	return &Controller{factory: New}
}

// SetEnabled updates whether proxy approval alerts should play.
func (c *Controller) SetEnabled(enabled bool) error {
	c.mu.Lock()
	if !enabled {
		player := c.player
		c.enabled = false
		c.player = nil
		c.mu.Unlock()
		if player != nil {
			return player.Close()
		}
		return nil
	}

	c.enabled = true
	err := c.ensurePlayerLocked()
	if err != nil {
		c.enabled = false
	}
	c.mu.Unlock()
	return err
}

// PlayProxyApprovalNeeded plays the current proxy alert if alerts are enabled.
func (c *Controller) PlayProxyApprovalNeeded() error {
	c.mu.Lock()
	if !c.enabled {
		c.mu.Unlock()
		return nil
	}
	player := c.player
	c.mu.Unlock()
	if player == nil {
		return nil
	}
	return player.PlayProxyApprovalNeeded()
}

// Close releases any active alert backend.
func (c *Controller) Close() error {
	c.mu.Lock()
	player := c.player
	c.enabled = false
	c.player = nil
	c.mu.Unlock()
	if player != nil {
		return player.Close()
	}
	return nil
}

func (c *Controller) ensurePlayerLocked() error {
	if c.player != nil {
		return nil
	}
	player, err := c.factory()
	if err != nil {
		return err
	}
	c.player = player
	return nil
}
