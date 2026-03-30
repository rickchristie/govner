package proxy

import (
	"sync"
	"testing"
	"time"
)

func TestMonitorRecordAllowed(t *testing.T) {
	m := NewMonitor(100, 100)

	req := ACLRequest{
		ID:        "test-1",
		Domain:    "example.com",
		Port:      "443",
		SourceIP:  "10.0.0.2",
		Timestamp: time.Now(),
	}

	m.RecordAllowed(req, 200, "Content-Type: text/html")

	history := m.AllowedHistory()
	if len(history) != 1 {
		t.Fatalf("expected 1 allowed entry, got %d", len(history))
	}

	entry := history[0]
	if entry.Domain != "example.com" {
		t.Errorf("expected domain example.com, got %s", entry.Domain)
	}
	if entry.SourceIP != "10.0.0.2" {
		t.Errorf("expected source IP 10.0.0.2, got %s", entry.SourceIP)
	}
	if entry.Decision != "allowed" {
		t.Errorf("expected decision allowed, got %s", entry.Decision)
	}
	if entry.ResponseStatus != 200 {
		t.Errorf("expected response status 200, got %d", entry.ResponseStatus)
	}
	if entry.ResponseHeaders != "Content-Type: text/html" {
		t.Errorf("expected response headers, got %s", entry.ResponseHeaders)
	}

	// Blocked history should be empty.
	blocked := m.BlockedHistory()
	if len(blocked) != 0 {
		t.Fatalf("expected 0 blocked entries, got %d", len(blocked))
	}
}

func TestMonitorRecordBlocked(t *testing.T) {
	m := NewMonitor(100, 100)

	req := ACLRequest{
		ID:        "test-2",
		Domain:    "evil.com",
		Port:      "443",
		SourceIP:  "10.0.0.3",
		Timestamp: time.Now(),
	}

	m.RecordBlocked(req, "domain not whitelisted")

	history := m.BlockedHistory()
	if len(history) != 1 {
		t.Fatalf("expected 1 blocked entry, got %d", len(history))
	}

	entry := history[0]
	if entry.Domain != "evil.com" {
		t.Errorf("expected domain evil.com, got %s", entry.Domain)
	}
	if entry.SourceIP != "10.0.0.3" {
		t.Errorf("expected source IP 10.0.0.3, got %s", entry.SourceIP)
	}
	if entry.Decision != "blocked" {
		t.Errorf("expected decision blocked, got %s", entry.Decision)
	}
	if entry.Reason != "domain not whitelisted" {
		t.Errorf("expected reason 'domain not whitelisted', got %s", entry.Reason)
	}

	// Allowed history should be empty.
	allowed := m.AllowedHistory()
	if len(allowed) != 0 {
		t.Fatalf("expected 0 allowed entries, got %d", len(allowed))
	}
}

func TestMonitorHistoryTrimAllowed(t *testing.T) {
	maxAllowed := 5
	m := NewMonitor(maxAllowed, 100)

	now := time.Now()
	for i := 0; i < 10; i++ {
		req := ACLRequest{
			ID:        "trim-a-" + string(rune('0'+i)),
			Domain:    "example.com",
			Port:      "443",
			SourceIP:  "10.0.0.1",
			Timestamp: now.Add(time.Duration(i) * time.Second),
		}
		m.RecordAllowed(req, 200, "")
	}

	history := m.AllowedHistory()
	if len(history) != maxAllowed {
		t.Fatalf("expected %d allowed entries after trim, got %d", maxAllowed, len(history))
	}

	// The oldest entries should have been dropped, so the first retained
	// entry should have a timestamp 5 seconds after "now".
	expectedFirst := now.Add(5 * time.Second)
	if !history[0].Timestamp.Equal(expectedFirst) {
		t.Errorf("expected first entry timestamp %v, got %v", expectedFirst, history[0].Timestamp)
	}
}

func TestMonitorHistoryTrimBlocked(t *testing.T) {
	maxBlocked := 3
	m := NewMonitor(100, maxBlocked)

	now := time.Now()
	for i := 0; i < 7; i++ {
		req := ACLRequest{
			ID:        "trim-b-" + string(rune('0'+i)),
			Domain:    "bad.com",
			Port:      "443",
			SourceIP:  "10.0.0.1",
			Timestamp: now.Add(time.Duration(i) * time.Second),
		}
		m.RecordBlocked(req, "denied")
	}

	history := m.BlockedHistory()
	if len(history) != maxBlocked {
		t.Fatalf("expected %d blocked entries after trim, got %d", maxBlocked, len(history))
	}

	// Oldest retained should be at index 4 (0-based from 7 entries, keep last 3).
	expectedFirst := now.Add(4 * time.Second)
	if !history[0].Timestamp.Equal(expectedFirst) {
		t.Errorf("expected first entry timestamp %v, got %v", expectedFirst, history[0].Timestamp)
	}
}

func TestMonitorStats(t *testing.T) {
	m := NewMonitor(100, 100)

	now := time.Now()
	for i := 0; i < 5; i++ {
		req := ACLRequest{
			ID:        "stats-a",
			Domain:    "ok.com",
			Port:      "443",
			SourceIP:  "10.0.0.1",
			Timestamp: now,
		}
		m.RecordAllowed(req, 200, "")
	}
	for i := 0; i < 3; i++ {
		req := ACLRequest{
			ID:        "stats-b",
			Domain:    "bad.com",
			Port:      "443",
			SourceIP:  "10.0.0.1",
			Timestamp: now,
		}
		m.RecordBlocked(req, "denied")
	}

	m.IncrementPending()
	m.IncrementPending()

	stats := m.Stats()
	if stats.TotalAllowed != 5 {
		t.Errorf("expected TotalAllowed 5, got %d", stats.TotalAllowed)
	}
	if stats.TotalBlocked != 3 {
		t.Errorf("expected TotalBlocked 3, got %d", stats.TotalBlocked)
	}
	if stats.TotalPending != 2 {
		t.Errorf("expected TotalPending 2, got %d", stats.TotalPending)
	}

	// Decrement pending and verify.
	m.DecrementPending()
	stats = m.Stats()
	if stats.TotalPending != 1 {
		t.Errorf("expected TotalPending 1 after decrement, got %d", stats.TotalPending)
	}

	// Decrement below zero should clamp to 0.
	m.DecrementPending()
	m.DecrementPending()
	stats = m.Stats()
	if stats.TotalPending != 0 {
		t.Errorf("expected TotalPending 0 after over-decrement, got %d", stats.TotalPending)
	}
}

func TestMonitorStatsTotalExceedsHistory(t *testing.T) {
	// Total counts should keep growing even after history is trimmed.
	m := NewMonitor(3, 3)

	now := time.Now()
	for i := 0; i < 10; i++ {
		req := ACLRequest{
			ID:        "overflow",
			Domain:    "ok.com",
			Port:      "443",
			SourceIP:  "10.0.0.1",
			Timestamp: now,
		}
		m.RecordAllowed(req, 200, "")
	}

	stats := m.Stats()
	if stats.TotalAllowed != 10 {
		t.Errorf("expected TotalAllowed 10 (not capped by history), got %d", stats.TotalAllowed)
	}

	history := m.AllowedHistory()
	if len(history) != 3 {
		t.Errorf("expected 3 history entries, got %d", len(history))
	}
}

func TestMonitorConcurrentAccess(t *testing.T) {
	m := NewMonitor(100, 100)

	const goroutines = 20
	const opsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			now := time.Now()
			for i := 0; i < opsPerGoroutine; i++ {
				req := ACLRequest{
					ID:        "conc",
					Domain:    "test.com",
					Port:      "443",
					SourceIP:  "10.0.0.1",
					Timestamp: now,
				}
				if i%2 == 0 {
					m.RecordAllowed(req, 200, "")
				} else {
					m.RecordBlocked(req, "test")
				}

				// Interleave reads to stress the mutex.
				_ = m.AllowedHistory()
				_ = m.BlockedHistory()
				_ = m.Stats()

				m.IncrementPending()
				m.DecrementPending()
			}
		}(g)
	}

	wg.Wait()

	stats := m.Stats()
	expectedAllowed := goroutines * opsPerGoroutine / 2
	expectedBlocked := goroutines * opsPerGoroutine / 2

	if stats.TotalAllowed != expectedAllowed {
		t.Errorf("expected TotalAllowed %d, got %d", expectedAllowed, stats.TotalAllowed)
	}
	if stats.TotalBlocked != expectedBlocked {
		t.Errorf("expected TotalBlocked %d, got %d", expectedBlocked, stats.TotalBlocked)
	}
	if stats.TotalPending != 0 {
		t.Errorf("expected TotalPending 0, got %d", stats.TotalPending)
	}
}

func TestMonitorHistoryReturnsCopy(t *testing.T) {
	m := NewMonitor(100, 100)

	req := ACLRequest{
		ID:        "copy-test",
		Domain:    "original.com",
		Port:      "443",
		SourceIP:  "10.0.0.1",
		Timestamp: time.Now(),
	}
	m.RecordAllowed(req, 200, "")
	m.RecordBlocked(req, "test")

	// Mutate the returned slices and verify the monitor's internal
	// state is not affected.
	allowed := m.AllowedHistory()
	allowed[0].Domain = "mutated.com"

	blocked := m.BlockedHistory()
	blocked[0].Domain = "mutated.com"

	// Original data should be unchanged.
	if m.AllowedHistory()[0].Domain != "original.com" {
		t.Error("AllowedHistory did not return a copy; mutation affected internal state")
	}
	if m.BlockedHistory()[0].Domain != "original.com" {
		t.Error("BlockedHistory did not return a copy; mutation affected internal state")
	}
}
