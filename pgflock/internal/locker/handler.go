package locker

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/rickchristie/govner/pgflock/internal/config"
)

// Handler manages the HTTP endpoints and state
type Handler struct {
	cfg                   *config.Config
	password              string
	testDatabases         map[string]bool
	cLockedDbConn         chan string
	locks                 map[string]*LockInfo
	locksMu               sync.RWMutex
	cleanupTickerInterval time.Duration
	autoUnlockDuration    time.Duration
	stateUpdateChan       chan<- *State
	waitingCount          atomic.Int32
}

// NewHandler creates a new Handler instance
func NewHandler(cfg *config.Config, stateUpdateChan chan<- *State) *Handler {
	return NewHandlerWithCleanupInterval(cfg, stateUpdateChan, 1*time.Minute)
}

// NewHandlerWithCleanupInterval creates a new Handler instance with configurable cleanup interval
func NewHandlerWithCleanupInterval(cfg *config.Config, stateUpdateChan chan<- *State, cleanupInterval time.Duration) *Handler {
	// Build test databases map from config
	testDatabases := make(map[string]bool)
	for _, port := range cfg.InstancePorts() {
		for i := 1; i <= cfg.DatabasesPerInstance; i++ {
			connString := fmt.Sprintf("postgresql://%s:%s@localhost:%d/%s%d",
				cfg.PGUsername, cfg.Password, port, cfg.DatabasePrefix, i)
			testDatabases[connString] = true
		}
	}

	h := &Handler{
		cfg:                   cfg,
		password:              cfg.Password,
		testDatabases:         testDatabases,
		cLockedDbConn:         make(chan string, len(testDatabases)),
		locks:                 make(map[string]*LockInfo),
		cleanupTickerInterval: cleanupInterval,
		autoUnlockDuration:    time.Duration(cfg.AutoUnlockMins) * time.Minute,
		stateUpdateChan:       stateUpdateChan,
	}

	// Initially all databases are available
	for connStr := range testDatabases {
		h.cLockedDbConn <- connStr
	}

	// Start cleanup routine for expired locks
	go h.cleanupExpiredLocks()

	return h
}

// withLocksLock executes the given function while holding the locks write lock
func (h *Handler) withLocksLock(fn func()) {
	h.locksMu.Lock()
	defer h.locksMu.Unlock()
	fn()
}

// withLocksRLock executes the given function while holding the locks read lock
func (h *Handler) withLocksRLock(fn func()) {
	h.locksMu.RLock()
	defer h.locksMu.RUnlock()
	fn()
}

func (h *Handler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	log.Debug().Str("path", req.URL.Path).Str("method", req.Method).Msg("Request received")

	switch req.URL.Path {
	case "/lock":
		h.handleLock(resp, req)
	case "/unlock":
		h.handleUnlock(resp, req)
	case "/health-check":
		h.handleHealthCheck(resp, req)
	case "/force-unlock":
		h.handleForceUnlock(resp, req)
	case "/unlock-by-marker":
		h.handleUnlockByMarker(resp, req)
	default:
		http.NotFound(resp, req)
	}
}

func (h *Handler) validateAuth(req *http.Request) (string, bool) {
	marker := req.URL.Query().Get("marker")
	password := req.URL.Query().Get("password")

	if marker == "" {
		return "", false
	}

	if password != h.password {
		return "", false
	}

	return marker, true
}

func (h *Handler) handleLock(resp http.ResponseWriter, req *http.Request) {
	marker, valid := h.validateAuth(req)
	if !valid {
		http.Error(resp, "Invalid marker or password", http.StatusUnauthorized)
		return
	}

	// Increment waiting count
	h.waitingCount.Add(1)
	h.sendStateUpdate()
	defer func() {
		h.waitingCount.Add(-1)
		h.sendStateUpdate()
	}()

	// Wait for a database to be freed or request context to be cancelled
	select {
	case connStr := <-h.cLockedDbConn:
		// Reset the database before giving it to the client
		if err := ResetDatabase(h.cfg, connStr); err != nil {
			// If reset fails, return the database to the pool and report error
			h.cLockedDbConn <- connStr
			log.Error().Err(err).Str("connStr", connStr).Msg("Failed to reset database")
			http.Error(resp, fmt.Sprintf("Failed to reset database: %v", err), http.StatusInternalServerError)
			return
		}

		// Record the lock
		h.withLocksLock(func() {
			h.locks[connStr] = &LockInfo{
				ConnString: connStr,
				Marker:     marker,
				LockedAt:   time.Now(),
			}
		})

		_, err := resp.Write([]byte(connStr))
		if err != nil {
			log.Error().Err(err).Msg("Failed to write response")
		}

		log.Info().Str("connStr", connStr).Str("marker", marker).Msg("LOCK")
		h.sendStateUpdate()

	case <-req.Context().Done():
		http.Error(resp, "Request cancelled or timed out", http.StatusRequestTimeout)
		log.Warn().Str("marker", marker).Msg("Lock request cancelled or timed out")
	}
}

func (h *Handler) handleUnlock(resp http.ResponseWriter, req *http.Request) {
	_, valid := h.validateAuth(req)
	if !valid {
		http.Error(resp, "Invalid marker or password", http.StatusUnauthorized)
		return
	}

	// Expect POST method with connection string in body
	if req.Method != "POST" {
		http.Error(resp, "Method not allowed, use POST", http.StatusMethodNotAllowed)
		return
	}

	// Read connection string from request body
	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(resp, "Failed to read request body", http.StatusBadRequest)
		return
	}

	connStr := string(bodyBytes)
	if connStr == "" {
		http.Error(resp, "Connection string required in request body", http.StatusBadRequest)
		return
	}

	if !h.testDatabases[connStr] {
		http.Error(resp, "Database connection does not exist", http.StatusBadRequest)
		return
	}

	// Check if this database is actually locked
	var lockInfo *LockInfo
	var exists bool
	h.withLocksLock(func() {
		lockInfo, exists = h.locks[connStr]
		if exists {
			delete(h.locks, connStr)
		}
	})

	if !exists {
		http.Error(resp, "Database is not currently locked", http.StatusBadRequest)
		return
	}

	// Return the database to the available pool
	h.cLockedDbConn <- connStr

	log.Info().Str("connStr", connStr).Str("marker", lockInfo.Marker).Msg("UNLOCK")
	h.sendStateUpdate()

	resp.WriteHeader(http.StatusOK)
	_, err = resp.Write([]byte("Database unlocked successfully"))
	if err != nil {
		log.Error().Err(err).Msg("Failed to write response")
	}
}

func (h *Handler) handleHealthCheck(resp http.ResponseWriter, req *http.Request) {
	var locked, free int
	h.withLocksRLock(func() {
		locked = len(h.locks)
	})
	free = len(h.cLockedDbConn)
	waiting := int(h.waitingCount.Load())

	resp.Header().Set("Content-Type", "application/json")
	resp.WriteHeader(http.StatusOK)
	fmt.Fprintf(resp, `{"status":"ok","locked":%d,"free":%d,"waiting":%d}`, locked, free, waiting)
}

func (h *Handler) handleForceUnlock(resp http.ResponseWriter, req *http.Request) {
	_, valid := h.validateAuth(req)
	if !valid {
		http.Error(resp, "Invalid marker or password", http.StatusUnauthorized)
		return
	}

	if req.Method != "POST" {
		http.Error(resp, "Method not allowed, use POST", http.StatusMethodNotAllowed)
		return
	}

	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(resp, "Failed to read request body", http.StatusBadRequest)
		return
	}

	connStr := string(bodyBytes)
	if connStr == "" {
		http.Error(resp, "Connection string required in request body", http.StatusBadRequest)
		return
	}

	var lockInfo *LockInfo
	var exists bool
	h.withLocksLock(func() {
		lockInfo, exists = h.locks[connStr]
		if exists {
			delete(h.locks, connStr)
		}
	})

	if !exists {
		log.Info().Str("connStr", connStr).Msg("FORCE-UNLOCK attempted on unlocked database")
		resp.WriteHeader(http.StatusOK)
		resp.Write([]byte("Database was not locked"))
		return
	}

	h.cLockedDbConn <- connStr
	log.Info().Str("connStr", connStr).Str("originalMarker", lockInfo.Marker).Msg("FORCE-UNLOCK")
	h.sendStateUpdate()

	resp.WriteHeader(http.StatusOK)
	resp.Write([]byte("Database force unlocked"))
}

func (h *Handler) handleUnlockByMarker(resp http.ResponseWriter, req *http.Request) {
	_, valid := h.validateAuth(req)
	if !valid {
		http.Error(resp, "Invalid marker or password", http.StatusUnauthorized)
		return
	}

	if req.Method != "POST" {
		http.Error(resp, "Method not allowed, use POST", http.StatusMethodNotAllowed)
		return
	}

	targetMarker := req.URL.Query().Get("target")
	if targetMarker == "" {
		http.Error(resp, "target query parameter required", http.StatusBadRequest)
		return
	}

	var unlockedDbs []string
	h.withLocksLock(func() {
		for connStr, lockInfo := range h.locks {
			if lockInfo.Marker == targetMarker {
				delete(h.locks, connStr)
				unlockedDbs = append(unlockedDbs, connStr)
			}
		}
	})

	for _, connStr := range unlockedDbs {
		h.cLockedDbConn <- connStr
	}

	log.Info().Str("marker", targetMarker).Int("count", len(unlockedDbs)).Msg("UNLOCK-BY-MARKER")
	h.sendStateUpdate()

	resp.WriteHeader(http.StatusOK)
	fmt.Fprintf(resp, "Unlocked %d databases", len(unlockedDbs))
}

// cleanupExpiredLocks automatically unlocks databases after the configured timeout
func (h *Handler) cleanupExpiredLocks() {
	ticker := time.NewTicker(h.cleanupTickerInterval)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		var unlocked []string

		h.withLocksLock(func() {
			for connStr, lockInfo := range h.locks {
				if now.Sub(lockInfo.LockedAt) > h.autoUnlockDuration {
					delete(h.locks, connStr)
					unlocked = append(unlocked, connStr)
					log.Info().Str("connStr", connStr).Str("marker", lockInfo.Marker).
						Dur("duration", h.autoUnlockDuration).Msg("AUTO-UNLOCK")
				}
			}
		})

		for _, connStr := range unlocked {
			h.cLockedDbConn <- connStr
		}

		if len(unlocked) > 0 {
			h.sendStateUpdate()
		}
	}
}

// sendStateUpdate sends the current state to the TUI
func (h *Handler) sendStateUpdate() {
	if h.stateUpdateChan == nil {
		return
	}

	state := h.GetState()

	// Non-blocking send
	select {
	case h.stateUpdateChan <- state:
	default:
		// Channel full, skip this update
	}
}

// GetState returns the current state of the locker
func (h *Handler) GetState() *State {
	var locks []LockInfo
	h.withLocksRLock(func() {
		for _, lockInfo := range h.locks {
			locks = append(locks, *lockInfo)
		}
	})

	// Sort by LockedAt time (oldest first)
	sort.Slice(locks, func(i, j int) bool {
		return locks[i].LockedAt.Before(locks[j].LockedAt)
	})

	return &State{
		TotalDatabases:  len(h.testDatabases),
		LockedDatabases: len(locks),
		FreeDatabases:   len(h.testDatabases) - len(locks),
		WaitingRequests: int(h.waitingCount.Load()),
		Locks:           locks,
	}
}

// ForceUnlock unlocks a database without going through HTTP (for TUI use)
func (h *Handler) ForceUnlock(connStr string) bool {
	var exists bool
	h.withLocksLock(func() {
		_, exists = h.locks[connStr]
		if exists {
			delete(h.locks, connStr)
		}
	})

	if exists {
		h.cLockedDbConn <- connStr
		log.Info().Str("connStr", connStr).Msg("TUI FORCE-UNLOCK")
		h.sendStateUpdate()
	}

	return exists
}

// UnlockByMarker unlocks all databases by marker (for TUI use)
func (h *Handler) UnlockByMarker(marker string) int {
	var unlockedDbs []string
	h.withLocksLock(func() {
		for connStr, lockInfo := range h.locks {
			if lockInfo.Marker == marker {
				delete(h.locks, connStr)
				unlockedDbs = append(unlockedDbs, connStr)
			}
		}
	})

	for _, connStr := range unlockedDbs {
		h.cLockedDbConn <- connStr
	}

	if len(unlockedDbs) > 0 {
		log.Info().Str("marker", marker).Int("count", len(unlockedDbs)).Msg("TUI UNLOCK-BY-MARKER")
		h.sendStateUpdate()
	}

	return len(unlockedDbs)
}

// UnlockAll unlocks all databases (for restart)
func (h *Handler) UnlockAll() int {
	var unlockedDbs []string
	h.withLocksLock(func() {
		for connStr := range h.locks {
			unlockedDbs = append(unlockedDbs, connStr)
			delete(h.locks, connStr)
		}
	})

	for _, connStr := range unlockedDbs {
		h.cLockedDbConn <- connStr
	}

	if len(unlockedDbs) > 0 {
		log.Info().Int("count", len(unlockedDbs)).Msg("UNLOCK-ALL")
		h.sendStateUpdate()
	}

	return len(unlockedDbs)
}
