package proxy

import (
	"sync"
	"time"
)

// MonitorStats holds aggregate counts for the proxy monitor.
type MonitorStats struct {
	TotalAllowed int
	TotalBlocked int
	TotalPending int
}

// HistoryEntry represents a single recorded request in the monitor history.
type HistoryEntry struct {
	Domain          string
	URL             string
	Method          string
	SourceIP        string
	Timestamp       time.Time
	Decision        string // "allowed" or "blocked"
	Reason          string
	ResponseStatus  int
	ResponseHeaders string
}

// Monitor tracks proxy request history and statistics. It maintains
// separate bounded lists for allowed and blocked requests, plus a
// pending count that can be incremented/decremented externally.
//
// All methods are safe for concurrent use.
type Monitor struct {
	mu sync.Mutex

	maxAllowed int
	maxBlocked int

	allowed []HistoryEntry
	blocked []HistoryEntry

	totalAllowed int
	totalBlocked int
	totalPending int
}

// NewMonitor creates a Monitor that retains at most maxAllowed allowed
// entries and maxBlocked blocked entries.
func NewMonitor(maxAllowed, maxBlocked int) *Monitor {
	return &Monitor{
		maxAllowed: maxAllowed,
		maxBlocked: maxBlocked,
		allowed:    make([]HistoryEntry, 0, maxAllowed),
		blocked:    make([]HistoryEntry, 0, maxBlocked),
	}
}

// RecordAllowed adds an allowed request to the history. If the allowed
// history exceeds maxAllowed, the oldest entry is removed.
func (m *Monitor) RecordAllowed(req ACLRequest, responseStatus int, responseHeaders string) {
	entry := HistoryEntry{
		Domain:          req.Domain,
		URL:             req.Domain + ":" + req.Port,
		Method:          "CONNECT",
		SourceIP:        req.SourceIP,
		Timestamp:       req.Timestamp,
		Decision:        "allowed",
		ResponseStatus:  responseStatus,
		ResponseHeaders: responseHeaders,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.allowed = append(m.allowed, entry)
	if len(m.allowed) > m.maxAllowed {
		// Drop the oldest entry.
		m.allowed = m.allowed[1:]
	}
	m.totalAllowed++
}

// RecordBlocked adds a blocked request to the history. If the blocked
// history exceeds maxBlocked, the oldest entry is removed.
func (m *Monitor) RecordBlocked(req ACLRequest, reason string) {
	entry := HistoryEntry{
		Domain:   req.Domain,
		URL:      req.Domain + ":" + req.Port,
		Method:   "CONNECT",
		SourceIP: req.SourceIP,
		Timestamp: req.Timestamp,
		Decision: "blocked",
		Reason:   reason,
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.blocked = append(m.blocked, entry)
	if len(m.blocked) > m.maxBlocked {
		// Drop the oldest entry.
		m.blocked = m.blocked[1:]
	}
	m.totalBlocked++
}

// IncrementPending atomically increments the pending request count.
func (m *Monitor) IncrementPending() {
	m.mu.Lock()
	m.totalPending++
	m.mu.Unlock()
}

// DecrementPending atomically decrements the pending request count.
// It will not go below zero.
func (m *Monitor) DecrementPending() {
	m.mu.Lock()
	if m.totalPending > 0 {
		m.totalPending--
	}
	m.mu.Unlock()
}

// AllowedHistory returns a copy of the allowed history slice.
func (m *Monitor) AllowedHistory() []HistoryEntry {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]HistoryEntry, len(m.allowed))
	copy(out, m.allowed)
	return out
}

// BlockedHistory returns a copy of the blocked history slice.
func (m *Monitor) BlockedHistory() []HistoryEntry {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]HistoryEntry, len(m.blocked))
	copy(out, m.blocked)
	return out
}

// Stats returns a snapshot of the current monitor statistics.
func (m *Monitor) Stats() MonitorStats {
	m.mu.Lock()
	defer m.mu.Unlock()

	return MonitorStats{
		TotalAllowed: m.totalAllowed,
		TotalBlocked: m.totalBlocked,
		TotalPending: m.totalPending,
	}
}
