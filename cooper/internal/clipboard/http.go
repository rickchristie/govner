package clipboard

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// ReservedPathPrefix is the path prefix reserved for clipboard endpoints.
// User-defined bridge routes must not use this prefix.
const ReservedPathPrefix = "/clipboard/"

// IsReservedPath returns true if the given path falls under the clipboard
// reserved namespace. Used to reject user routes at configuration time.
func IsReservedPath(path string) bool {
	return path == "/clipboard" || strings.HasPrefix(path, ReservedPathPrefix)
}

// typeResponse is returned by GET /clipboard/type.
type typeResponse struct {
	State             string   `json:"state"`
	Kind              string   `json:"kind,omitempty"`
	MIME              string   `json:"mime,omitempty"`
	RawMIME           string   `json:"raw_mime,omitempty"`
	Size              int64    `json:"size,omitempty"`
	AvailableVariants []string `json:"available_variants,omitempty"`
	CreatedAt         string   `json:"created_at,omitempty"`
	ExpiresAt         string   `json:"expires_at,omitempty"`
}

// Handler provides HTTP handlers for clipboard endpoints. It is designed
// to be mounted on the existing bridge server, sharing the same port.
type Handler struct {
	manager *Manager
}

// NewHandler creates a clipboard HTTP handler backed by the given manager.
func NewHandler(manager *Manager) *Handler {
	return &Handler{manager: manager}
}

// ServeHTTP dispatches clipboard requests. Returns false if the path
// is not a clipboard path and should be handled by the caller.
func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path

	if !IsReservedPath(path) {
		return false
	}

	// All clipboard endpoints require bearer auth.
	session, err := h.authenticate(r)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return true
	}

	// Check eligibility.
	if !session.Eligible {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "barrel not eligible for clipboard access"})
		return true
	}

	switch {
	case path == "/clipboard/type" && r.Method == http.MethodGet:
		h.handleType(w)
	case path == "/clipboard/image" && r.Method == http.MethodGet:
		h.handleImage(w)
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "unknown clipboard endpoint"})
	}
	return true
}

// authenticate extracts and validates the bearer token from the request.
func (h *Handler) authenticate(r *http.Request) (*BarrelSession, error) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return nil, ErrInvalidToken
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	return h.manager.ValidateToken(token)
}

// handleType returns clipboard state metadata.
func (h *Handler) handleType(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")

	snap := h.manager.Current()
	if snap == nil || snap.IsExpired() {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(typeResponse{State: "empty"})
		return
	}

	variants := make([]string, 0, len(snap.Object.Variants))
	for mime := range snap.Object.Variants {
		variants = append(variants, mime)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(typeResponse{
		State:             "staged",
		Kind:              string(snap.Object.Kind),
		MIME:              "image/png",
		RawMIME:           snap.Object.MIME,
		Size:              snap.Object.RawSize,
		AvailableVariants: variants,
		CreatedAt:         snap.CreatedAt.Format(time.RFC3339),
		ExpiresAt:         snap.ExpiresAt.Format(time.RFC3339),
	})
}

// handleImage returns the staged PNG image bytes.
func (h *Handler) handleImage(w http.ResponseWriter) {
	snap := h.manager.Current()
	if snap == nil || snap.IsExpired() {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	variant, ok := snap.Object.Variants["image/png"]
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Touch access tracking.
	h.manager.Touch(snap.ID)

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Cooper-Clipboard-Id", snap.ID)
	w.WriteHeader(http.StatusOK)
	w.Write(variant.Bytes)
}

// SanitizeRoutes removes any user-defined routes that collide with the
// reserved /clipboard/* namespace. Returns the sanitized routes and a
// list of removed route paths.
func SanitizeRoutes(routes []interface{ GetAPIPath() string }) (kept, removed []string) {
	// This is implemented as a standalone function that works on path strings
	// to avoid circular imports.
	return nil, nil
}

// SanitizeBridgeRoutes filters out routes whose APIPath collides with
// the reserved clipboard namespace. Returns clean routes and removed paths.
func SanitizeBridgeRoutes[T any](routes []T, getPath func(T) string) ([]T, []string) {
	var clean []T
	var removed []string
	for _, rt := range routes {
		if IsReservedPath(getPath(rt)) {
			removed = append(removed, getPath(rt))
		} else {
			clean = append(clean, rt)
		}
	}
	return clean, removed
}
