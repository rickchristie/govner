package locker

import (
	"context"
	"encoding/json"
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

const serverVersion = "2"

// RestartRequest represents a restart request from HTTP API to TUI
type RestartRequest struct {
	ResponseChan chan error // TUI sends completion status here
}

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
	restartRequestChan    chan RestartRequest

	// resetDatabase is the function used to reset a database before handing it to a client.
	// Defaults to ResetDatabase. Overridable in tests to skip actual Postgres operations.
	resetDatabase func(cfg *config.Config, connStr string) error
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
		resetDatabase:         ResetDatabase,
	}

	// Initially all databases are available
	for connStr := range testDatabases {
		h.cLockedDbConn <- connStr
	}

	// Safety-net cleanup for any locks that somehow lose their cancel func
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
	case "/restart":
		h.handleRestart(resp, req)
	case "/unlock-all":
		h.handleUnlockAll(resp, req)
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

// handleLock acquires a database lock using a streaming connection.
//
// The response writes the connection string (newline-terminated) and then keeps
// the connection open. The open connection IS the lock — when the client closes
// it (explicit unlock or process death), the server's request context is
// cancelled, waking the handler which then releases the lock immediately.
//
// WriteTimeout on the HTTP server enforces the auto-unlock duration: if the
// connection remains open longer than auto_unlock_minutes, the server closes it,
// cancelling the handler and releasing the lock.
func (h *Handler) handleLock(resp http.ResponseWriter, req *http.Request) {
	marker, valid := h.validateAuth(req)
	if !valid {
		http.Error(resp, "Invalid marker or password", http.StatusUnauthorized)
		return
	}

	h.waitingCount.Add(1)
	h.sendStateUpdate()

	select {
	case connStr := <-h.cLockedDbConn:
		// Decrement waiting count now that we have a database
		h.waitingCount.Add(-1)
		h.sendStateUpdate()

		if err := h.resetDatabase(h.cfg, connStr); err != nil {
			h.cLockedDbConn <- connStr
			log.Error().Err(err).Str("connStr", connStr).Msg("Failed to reset database")
			http.Error(resp, fmt.Sprintf("Failed to reset database: %v", err), http.StatusInternalServerError)
			return
		}

		// lockCtx enforces auto-unlock: if the lock is held longer than
		// autoUnlockDuration the context times out, waking the select below and
		// releasing the lock. External callers (ForceUnlock, UnlockAll, handleUnlock)
		// cancel the context early to release the lock on demand.
		lockCtx, lockCancel := context.WithTimeout(context.Background(), h.autoUnlockDuration)

		h.withLocksLock(func() {
			h.locks[connStr] = &LockInfo{
				ConnString: connStr,
				Marker:     marker,
				LockedAt:   time.Now(),
				cancel:     lockCancel,
			}
		})

		// Send version header and connection string. The connection stays open
		// after this — the open connection is what holds the lock.
		resp.Header().Set("X-PGFlock-Version", serverVersion)
		resp.WriteHeader(http.StatusOK)
		fmt.Fprintf(resp, "%s\n", connStr)
		if f, ok := resp.(http.Flusher); ok {
			f.Flush()
		}

		// Disable any server-level write deadline for this streaming connection.
		// Auto-unlock is handled by lockCtx timeout above, not by connection I/O deadlines.
		rc := http.NewResponseController(resp)
		if err := rc.SetWriteDeadline(time.Time{}); err != nil {
			log.Debug().Err(err).Msg("Could not clear write deadline (non-fatal)")
		}

		log.Info().Str("connStr", connStr).Str("marker", marker).Msg("LOCK")
		h.sendStateUpdate()

		// Block until either the client disconnects or an external force-unlock
		// cancels lockCtx.
		select {
		case <-req.Context().Done():
		case <-lockCtx.Done():
		}

		// Release the lock if it hasn't already been released by the external caller
		// (ForceUnlock/UnlockAll/handleUnlock remove it from the map before cancelling).
		var released bool
		h.withLocksLock(func() {
			if _, exists := h.locks[connStr]; exists {
				delete(h.locks, connStr)
				released = true
			}
		})

		if released {
			h.cLockedDbConn <- connStr
			log.Info().Str("connStr", connStr).Str("marker", marker).Msg("UNLOCK (connection closed)")
			h.sendStateUpdate()
		}

		lockCancel() // always clean up the context

	case <-req.Context().Done():
		h.waitingCount.Add(-1)
		h.sendStateUpdate()
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

	if !h.testDatabases[connStr] {
		http.Error(resp, "Database connection does not exist", http.StatusBadRequest)
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
		http.Error(resp, "Database is not currently locked", http.StatusBadRequest)
		return
	}

	// Return to pool before cancelling so the streaming handler sees released=false
	// and skips its own pool return, avoiding a double-send.
	h.cLockedDbConn <- connStr

	// Wake the streaming handler (if any) so it exits cleanly.
	if lockInfo.cancel != nil {
		lockInfo.cancel()
	}

	log.Info().Str("connStr", connStr).Str("marker", lockInfo.Marker).Msg("UNLOCK")
	h.sendStateUpdate()

	resp.WriteHeader(http.StatusOK)
	_, err = resp.Write([]byte("Database unlocked successfully"))
	if err != nil {
		log.Error().Err(err).Msg("Failed to write response")
	}
}

func (h *Handler) handleHealthCheck(resp http.ResponseWriter, req *http.Request) {
	now := time.Now()
	var locks []LockInfoJSON

	h.withLocksRLock(func() {
		for _, lockInfo := range h.locks {
			locks = append(locks, LockInfoJSON{
				ConnString:      lockInfo.ConnString,
				Marker:          lockInfo.Marker,
				LockedAt:        lockInfo.LockedAt.Format(time.RFC3339),
				DurationSeconds: int64(now.Sub(lockInfo.LockedAt).Seconds()),
			})
		}
	})

	// Sort locks by duration (longest first)
	sort.Slice(locks, func(i, j int) bool {
		return locks[i].DurationSeconds > locks[j].DurationSeconds
	})

	response := HealthCheckResponse{
		Status:            "ok",
		TotalDatabases:    len(h.testDatabases),
		LockedDatabases:   len(locks),
		FreeDatabases:     len(h.cLockedDbConn),
		WaitingRequests:   int(h.waitingCount.Load()),
		AutoUnlockMinutes: h.cfg.AutoUnlockMins,
		Locks:             locks,
	}

	resp.Header().Set("Content-Type", "application/json")
	resp.WriteHeader(http.StatusOK)
	json.NewEncoder(resp).Encode(response)
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

	if h.ForceUnlock(connStr) {
		resp.WriteHeader(http.StatusOK)
		resp.Write([]byte("Database force unlocked"))
	} else {
		log.Info().Str("connStr", connStr).Msg("FORCE-UNLOCK attempted on unlocked database")
		resp.WriteHeader(http.StatusOK)
		resp.Write([]byte("Database was not locked"))
	}
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

	count := h.UnlockByMarker(targetMarker)

	log.Info().Str("marker", targetMarker).Int("count", count).Msg("UNLOCK-BY-MARKER")
	h.sendStateUpdate()

	resp.WriteHeader(http.StatusOK)
	fmt.Fprintf(resp, "Unlocked %d databases", count)
}

// cleanupExpiredLocks is a safety-net for any locks that somehow lack a cancel
// func (e.g. created by tests or edge cases). Normal v2 streaming locks are
// released immediately when their connection closes or via cancel().
func (h *Handler) cleanupExpiredLocks() {
	ticker := time.NewTicker(h.cleanupTickerInterval)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		var unlocked []string

		h.withLocksLock(func() {
			for connStr, lockInfo := range h.locks {
				if lockInfo.cancel == nil && now.Sub(lockInfo.LockedAt) > h.autoUnlockDuration {
					delete(h.locks, connStr)
					unlocked = append(unlocked, connStr)
					log.Info().Str("connStr", connStr).Str("marker", lockInfo.Marker).
						Dur("duration", h.autoUnlockDuration).Msg("AUTO-UNLOCK (safety-net)")
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

// cancelAndRelease removes the lock from the map, returns the database to the pool,
// and cancels the streaming handler. It is the shared implementation for all
// force-release operations (ForceUnlock, UnlockByMarker, UnlockAll).
// Must NOT be called with locksMu held.
func (h *Handler) cancelAndRelease(connStr string, lockInfo *LockInfo) {
	h.cLockedDbConn <- connStr
	if lockInfo.cancel != nil {
		lockInfo.cancel()
	}
}

// ForceUnlock unlocks a database without going through HTTP (for TUI use)
func (h *Handler) ForceUnlock(connStr string) bool {
	var lockInfo *LockInfo
	h.withLocksLock(func() {
		var exists bool
		lockInfo, exists = h.locks[connStr]
		if exists {
			delete(h.locks, connStr)
		} else {
			lockInfo = nil
		}
	})

	if lockInfo == nil {
		return false
	}

	h.cancelAndRelease(connStr, lockInfo)
	log.Info().Str("connStr", connStr).Msg("TUI FORCE-UNLOCK")
	h.sendStateUpdate()
	return true
}

// UnlockByMarker unlocks all databases by marker (for TUI use)
func (h *Handler) UnlockByMarker(marker string) int {
	released := make(map[string]*LockInfo)
	h.withLocksLock(func() {
		for connStr, lockInfo := range h.locks {
			if lockInfo.Marker == marker {
				released[connStr] = lockInfo
				delete(h.locks, connStr)
			}
		}
	})

	for connStr, lockInfo := range released {
		h.cancelAndRelease(connStr, lockInfo)
	}

	if len(released) > 0 {
		log.Info().Str("marker", marker).Int("count", len(released)).Msg("TUI UNLOCK-BY-MARKER")
		h.sendStateUpdate()
	}

	return len(released)
}

// UnlockAll unlocks all databases (for restart)
func (h *Handler) UnlockAll() int {
	released := make(map[string]*LockInfo)
	h.withLocksLock(func() {
		for connStr, lockInfo := range h.locks {
			released[connStr] = lockInfo
		}
		h.locks = make(map[string]*LockInfo)
	})

	for connStr, lockInfo := range released {
		h.cancelAndRelease(connStr, lockInfo)
	}

	if len(released) > 0 {
		log.Info().Int("count", len(released)).Msg("UNLOCK-ALL")
		h.sendStateUpdate()
	}

	return len(released)
}

// SetRestartRequestChan sets the channel for sending restart requests to TUI
func (h *Handler) SetRestartRequestChan(ch chan RestartRequest) {
	h.restartRequestChan = ch
}

// RestartRequestChan returns the restart request channel
func (h *Handler) RestartRequestChan() chan RestartRequest {
	return h.restartRequestChan
}

func (h *Handler) handleRestart(resp http.ResponseWriter, req *http.Request) {
	_, valid := h.validateAuth(req)
	if !valid {
		http.Error(resp, "Invalid marker or password", http.StatusUnauthorized)
		return
	}

	if req.Method != "POST" {
		http.Error(resp, "Method not allowed, use POST", http.StatusMethodNotAllowed)
		return
	}

	if h.restartRequestChan == nil {
		http.Error(resp, "Restart not available (TUI not connected)", http.StatusServiceUnavailable)
		return
	}

	log.Info().Msg("RESTART requested via HTTP API")

	responseChan := make(chan error, 1)

	select {
	case h.restartRequestChan <- RestartRequest{ResponseChan: responseChan}:
		err := <-responseChan
		if err != nil {
			log.Error().Err(err).Msg("Restart failed")
			http.Error(resp, fmt.Sprintf("Restart failed: %v", err), http.StatusInternalServerError)
			return
		}

		resp.Header().Set("Content-Type", "application/json")
		resp.WriteHeader(http.StatusOK)
		fmt.Fprintf(resp, `{"status":"ok","message":"Restart completed successfully"}`)

	default:
		http.Error(resp, "Restart already in progress", http.StatusConflict)
	}
}

func (h *Handler) handleUnlockAll(resp http.ResponseWriter, req *http.Request) {
	_, valid := h.validateAuth(req)
	if !valid {
		http.Error(resp, "Invalid marker or password", http.StatusUnauthorized)
		return
	}

	if req.Method != "POST" {
		http.Error(resp, "Method not allowed, use POST", http.StatusMethodNotAllowed)
		return
	}

	count := h.UnlockAll()

	resp.Header().Set("Content-Type", "application/json")
	resp.WriteHeader(http.StatusOK)
	fmt.Fprintf(resp, `{"status":"ok","unlocked":%d}`, count)
}
