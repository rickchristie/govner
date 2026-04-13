package alertsound

type noopPlayer struct{}

// NewNoop returns a silent player used when host audio is unavailable.
func NewNoop() Player { return noopPlayer{} }

func (noopPlayer) PlayProxyApprovalNeeded() error { return nil }

func (noopPlayer) Close() error { return nil }
