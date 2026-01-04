package locker

import (
	"time"
)

// State represents the current state of the locker for TUI display
type State struct {
	TotalDatabases  int
	LockedDatabases int
	FreeDatabases   int
	WaitingRequests int
	Locks           []LockInfo
	Instances       []InstanceStatus
}

// LockInfo stores information about a locked database
type LockInfo struct {
	ConnString string
	Marker     string
	LockedAt   time.Time
}

// LockInfoJSON is the JSON representation of LockInfo for API responses
type LockInfoJSON struct {
	ConnString      string `json:"conn_string"`
	Marker          string `json:"marker"`
	LockedAt        string `json:"locked_at"`
	DurationSeconds int64  `json:"duration_seconds"`
}

// HealthCheckResponse is the JSON response for the health-check endpoint
type HealthCheckResponse struct {
	Status            string         `json:"status"`
	TotalDatabases    int            `json:"total"`
	LockedDatabases   int            `json:"locked"`
	FreeDatabases     int            `json:"free"`
	WaitingRequests   int            `json:"waiting"`
	AutoUnlockMinutes int            `json:"auto_unlock_minutes"`
	Locks             []LockInfoJSON `json:"locks"`
}

// InstanceStatus represents the status of a PostgreSQL instance
type InstanceStatus struct {
	Port    int
	Running bool
}
