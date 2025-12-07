package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// LockInfo stores information about a locked database
type LockInfo struct {
	ConnString string
	Username   string
	LockedAt   time.Time
}

// Handler manages the HTTP endpoints and state
type Handler struct {
	cLockedDbConn         chan string
	locks                 map[string]*LockInfo // connString -> LockInfo
	locksMu               sync.RWMutex
	adminSessions         map[string]time.Time // sessionID -> lastActivity
	adminSessionsMu       sync.RWMutex
	cleanupTickerInterval time.Duration
}

// NewHandler creates a new Handler instance
func NewHandler() *Handler {
	return NewHandlerWithCleanupInterval(1 * time.Minute)
}

// NewHandlerWithCleanupInterval creates a new Handler instance with configurable cleanup interval
func NewHandlerWithCleanupInterval(cleanupInterval time.Duration) *Handler {
	h := &Handler{
		cLockedDbConn:         make(chan string, len(testDatabases)),
		locks:                 make(map[string]*LockInfo),
		adminSessions:         make(map[string]time.Time),
		cleanupTickerInterval: cleanupInterval,
	}

	// Initially all databases are available
	for connStr := range testDatabases {
		h.cLockedDbConn <- connStr
	}

	// Start cleanup routine for expired locks
	go h.cleanupExpiredLocks()

	// Start cleanup routine for expired admin sessions
	go h.cleanupExpiredSessions()

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

// withAdminSessionsLock executes the given function while holding the admin sessions write lock
func (h *Handler) withAdminSessionsLock(fn func()) {
	h.adminSessionsMu.Lock()
	defer h.adminSessionsMu.Unlock()
	fn()
}

// withAdminSessionsRLock executes the given function while holding the admin sessions read lock
func (h *Handler) withAdminSessionsRLock(fn func()) {
	h.adminSessionsMu.RLock()
	defer h.adminSessionsMu.RUnlock()
	fn()
}

func (h *Handler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	log.Info().Str("path", req.URL.Path).Str("method", req.Method).Msg("Request received")

	switch req.URL.Path {
	case "/lock":
		h.handleLock(resp, req)
	case "/unlock":
		h.handleUnlock(resp, req)
	case "/admin":
		h.handleAdmin(resp, req)
	case "/admin/login":
		h.handleAdminLogin(resp, req)
	case "/admin/logout":
		h.handleAdminLogout(resp, req)
	case "/admin/force-unlock":
		h.handleAdminForceUnlock(resp, req)
	case "/admin/unlock-by-username":
		h.handleAdminUnlockByUsername(resp, req)
	default:
		http.NotFound(resp, req)
	}
}

func (h *Handler) validateAuth(req *http.Request) (string, bool) {
	username := req.URL.Query().Get("username")
	password := req.URL.Query().Get("password")

	if username == "" {
		return "", false
	}

	if password != dbLockerPassword {
		return "", false
	}

	return username, true
}

func (h *Handler) handleLock(resp http.ResponseWriter, req *http.Request) {
	username, valid := h.validateAuth(req)
	if !valid {
		http.Error(resp, "Invalid username or password", http.StatusUnauthorized)
		return
	}

	// Wait for a database to be freed or request context to be cancelled
	select {
	case connStr := <-h.cLockedDbConn:
		// Record the lock
		h.withLocksLock(func() {
			h.locks[connStr] = &LockInfo{
				ConnString: connStr,
				Username:   username,
				LockedAt:   time.Now(),
			}
		})

		_, err := resp.Write([]byte(connStr))
		if err != nil {
			log.Error().Err(err).Msg("Failed to write response")
		}

		log.Info().Str("connStr", connStr).Str("username", username).Msg("LOCK")

	case <-req.Context().Done():
		http.Error(resp, "Request cancelled or timed out", http.StatusRequestTimeout)
		log.Warn().Str("username", username).Msg("Lock request cancelled or timed out")
	}
}

func (h *Handler) handleUnlock(resp http.ResponseWriter, req *http.Request) {
	username, valid := h.validateAuth(req)
	if !valid {
		http.Error(resp, "Invalid username or password", http.StatusUnauthorized)
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

	// It's okay to read from testDatabases as it's not modified after initialization.
	if testDatabases[connStr] == false {
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

	log.Info().Str("connStr", connStr).Str("username", username).Str("originalUser", lockInfo.Username).Msg("UNLOCK")

	resp.WriteHeader(http.StatusOK)
	_, err = resp.Write([]byte("Database unlocked successfully"))
	if err != nil {
		log.Error().Err(err).Msg("Failed to write response")
		return
	}
}

// cleanupExpiredLocks automatically unlocks databases after 30 minutes
func (h *Handler) cleanupExpiredLocks() {
	ticker := time.NewTicker(h.cleanupTickerInterval)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		h.withLocksLock(func() {
			for connStr, lockInfo := range h.locks {
				if now.Sub(lockInfo.LockedAt) > 30*time.Minute {
					delete(h.locks, connStr)
					h.cLockedDbConn <- connStr
					log.Info().Str("connStr", connStr).Str("username", lockInfo.Username).Msg("AUTO-UNLOCK after 30 minutes")
				}
			}
		})
	}
}

func main() {
	configPath := flag.String("config", "", "Path to config JSON file")
	flag.Parse()

	var cfg *Config
	var err error

	if *configPath != "" {
		// Load config from file
		cfg, err = LoadConfig(*configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Loaded config from %s\n", *configPath)
	} else {
		// Run interactive setup
		var savePath string
		cfg, savePath, err = RunSetup()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Setup error: %v\n", err)
			os.Exit(1)
		}

		if err := cfg.Save(savePath); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\nConfig saved to %s\n", savePath)
		fmt.Printf("Run with: dblocker --config %q\n\n", savePath)
	}

	// Initialize global state from config
	InitFromConfig(cfg)

	h := NewHandler()

	// Start status logging (reduced frequency to minimize log growth)
	go func() {
		for {
			available := len(h.cLockedDbConn)
			var locked int
			h.withLocksRLock(func() {
				locked = len(h.locks)
			})

			log.Info().Int("available", available).Int("locked", locked).Msg("Database status")
			time.Sleep(5 * time.Minute) // Log every 5 minutes instead of 10 seconds
		}
	}()

	log.Info().Msg(">>> Start listening on port :9191")
	s := &http.Server{
		Addr:           ":9191",
		Handler:        h,
		ReadTimeout:    10 * time.Minute,
		WriteTimeout:   10 * time.Minute,
		MaxHeaderBytes: 1 << 20,
	}
	err = s.ListenAndServe()
	if err != nil {
		panic(err)
	}
}
