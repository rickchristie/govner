package clipboard

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Manager is the staged clipboard grant manager. It holds at most one
// immutable StagedSnapshot at a time and tracks per-barrel authentication
// sessions.
//
// Concurrency model:
//   - A single RWMutex guards the current snapshot pointer and access
//     metadata. Stage() builds the full immutable object first, then
//     swaps the pointer under a write lock. Clear() sets the pointer to
//     nil. In-flight readers that captured the old pointer finish safely.
//   - A separate Mutex guards the session maps to avoid lock ordering
//     issues between snapshot reads and session lookups.
type Manager struct {
	policyMu sync.RWMutex
	ttl      time.Duration
	maxBytes int

	// cooperDir is the path to the cooper configuration directory.
	// Used for file-based token validation (cooper cli barrels).
	cooperDir string

	// snapshotMu guards current.
	snapshotMu sync.RWMutex
	current    *StagedSnapshot

	// sessionMu guards tokenToSession and nameToToken.
	sessionMu      sync.Mutex
	tokenToSession map[string]*BarrelSession
	nameToToken    map[string]string
}

// NewManager creates a Manager with the given default TTL and maximum
// object size in bytes.
func NewManager(ttl time.Duration, maxBytes int) *Manager {
	return &Manager{
		ttl:            ttl,
		maxBytes:       maxBytes,
		tokenToSession: make(map[string]*BarrelSession),
		nameToToken:    make(map[string]string),
	}
}

// SetCooperDir sets the cooper directory for file-based token validation.
// This enables barrels started by `cooper cli` (a separate process) to
// authenticate by writing token files to the shared tokens directory.
func (m *Manager) SetCooperDir(dir string) {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	m.cooperDir = dir
}

// UpdatePolicy updates the default TTL and maximum staged size used for
// subsequent Stage calls. Zero or negative values are ignored.
func (m *Manager) UpdatePolicy(ttl time.Duration, maxBytes int) {
	m.policyMu.Lock()
	defer m.policyMu.Unlock()

	if ttl > 0 {
		m.ttl = ttl
	}
	if maxBytes > 0 {
		m.maxBytes = maxBytes
	}
}

func (m *Manager) policy() (time.Duration, int) {
	m.policyMu.RLock()
	defer m.policyMu.RUnlock()
	return m.ttl, m.maxBytes
}

// Stage creates a new immutable StagedSnapshot from obj and atomically
// replaces the current snapshot. If ttl is zero the manager's default
// TTL is used. Returns an error if the total object size exceeds
// maxBytes.
func (m *Manager) Stage(obj ClipboardObject, ttl time.Duration) (*StagedSnapshot, error) {
	defaultTTL, maxBytes := m.policy()
	if ttl == 0 {
		ttl = defaultTTL
	}

	// Enforce size limit.
	total := int64(len(obj.Raw))
	for _, v := range obj.Variants {
		total += int64(len(v.Bytes))
	}
	if total > int64(maxBytes) {
		return nil, fmt.Errorf("clipboard object size %d exceeds maximum %d bytes", total, maxBytes)
	}

	id, err := randomHex(16)
	if err != nil {
		return nil, fmt.Errorf("generate snapshot ID: %w", err)
	}

	now := time.Now()
	snap := &StagedSnapshot{
		ID:           id,
		Object:       obj,
		CreatedAt:    now,
		ExpiresAt:    now.Add(ttl),
		LastAccessAt: now,
		AccessCount:  0,
	}

	m.snapshotMu.Lock()
	m.current = snap
	m.snapshotMu.Unlock()

	return snap, nil
}

// Clear removes the current snapshot. Subsequent calls to Current()
// return nil. In-flight readers that already captured the pointer are
// unaffected.
func (m *Manager) Clear() {
	m.snapshotMu.Lock()
	m.current = nil
	m.snapshotMu.Unlock()
}

// Current returns the current staged snapshot, or nil if nothing is
// staged. The returned pointer is immutable and safe to use without
// holding any lock.
func (m *Manager) Current() *StagedSnapshot {
	m.snapshotMu.RLock()
	snap := m.current
	m.snapshotMu.RUnlock()
	return snap
}

// Touch records an access on the current snapshot by creating a new
// snapshot with updated LastAccessAt and AccessCount. If there is no
// current snapshot or the id does not match, Touch is a no-op.
func (m *Manager) Touch(id string) {
	m.snapshotMu.Lock()
	defer m.snapshotMu.Unlock()

	if m.current == nil || m.current.ID != id {
		return
	}

	updated := *m.current
	updated.LastAccessAt = time.Now()
	updated.AccessCount = m.current.AccessCount + 1
	m.current = &updated
}

// RegisterBarrel creates a new authenticated session for a barrel. A
// cryptographically random token is generated and stored. If a session
// already exists for the container name it is replaced.
func (m *Manager) RegisterBarrel(session BarrelSession) error {
	token, err := GenerateToken()
	if err != nil {
		return fmt.Errorf("register barrel: %w", err)
	}

	session.Token = token

	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()

	// If a previous session exists for this container, remove the old token.
	if oldToken, ok := m.nameToToken[session.ContainerName]; ok {
		delete(m.tokenToSession, oldToken)
	}

	m.tokenToSession[token] = &session
	m.nameToToken[session.ContainerName] = token

	return nil
}

// UnregisterBarrel removes the session for the given container name.
// The associated token becomes invalid immediately.
func (m *Manager) UnregisterBarrel(containerName string) {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()

	token, ok := m.nameToToken[containerName]
	if !ok {
		return
	}

	delete(m.tokenToSession, token)
	delete(m.nameToToken, containerName)
}

// ValidateToken checks a token against registered sessions. If the token
// is not found in memory, it falls back to scanning token files in the
// cooperDir/tokens/ directory. This enables barrels started by `cooper cli`
// (a separate process) to authenticate without inter-process registration.
func (m *Manager) ValidateToken(token string) (*BarrelSession, error) {
	m.sessionMu.Lock()
	if sess, ok := m.tokenToSession[token]; ok {
		cp := *sess
		m.sessionMu.Unlock()
		return &cp, nil
	}
	cooperDir := m.cooperDir
	m.sessionMu.Unlock()

	// Slow path: scan token files on disk. This handles barrels started by
	// `cooper cli` which writes token files but can't register in-memory
	// with the `cooper up` process.
	if cooperDir != "" {
		if sess := m.validateTokenFromDisk(cooperDir, token); sess != nil {
			return sess, nil
		}
	}

	return nil, fmt.Errorf("invalid token")
}

// validateTokenFromDisk scans the tokens directory for a file containing
// the given token. Returns a BarrelSession if found, nil otherwise.
func (m *Manager) validateTokenFromDisk(cooperDir, token string) *BarrelSession {
	tokensDir := filepath.Join(cooperDir, "tokens")
	entries, err := os.ReadDir(tokensDir)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(tokensDir, entry.Name()))
		if err != nil {
			continue
		}
		fileToken := strings.TrimSpace(string(data))
		if fileToken == token {
			sess, err := inspectContainerSession(entry.Name())
			if err != nil || sess == nil {
				continue
			}
			sess.Token = token
			sess.ContainerName = entry.Name()
			return sess
		}
	}
	return nil
}

// ActiveSessions returns a snapshot of all currently registered barrel
// sessions. Tokens are included in the returned structs; callers must
// not log them.
func (m *Manager) ActiveSessions() []BarrelSession {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()

	sessions := make([]BarrelSession, 0, len(m.tokenToSession))
	for _, s := range m.tokenToSession {
		sessions = append(sessions, *s)
	}
	return sessions
}

// randomHex generates n random bytes and returns them hex-encoded.
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
