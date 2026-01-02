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

// InstanceStatus represents the status of a PostgreSQL instance
type InstanceStatus struct {
	Port    int
	Running bool
}
