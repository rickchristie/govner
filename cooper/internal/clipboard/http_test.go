package clipboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIsReservedPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/clipboard", true},
		{"/clipboard/type", true},
		{"/clipboard/image", true},
		{"/clipboard/anything", true},
		{"/health", false},
		{"/routes", false},
		{"/deploy", false},
		{"/clipboardx", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := IsReservedPath(tt.path); got != tt.want {
				t.Errorf("IsReservedPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestSanitizeBridgeRoutes(t *testing.T) {
	type route struct{ path string }
	routes := []route{
		{"/deploy"},
		{"/clipboard/type"},
		{"/restart"},
		{"/clipboard/image"},
	}
	clean, removed := SanitizeBridgeRoutes(routes, func(r route) string { return r.path })
	if len(clean) != 2 {
		t.Fatalf("expected 2 clean routes, got %d", len(clean))
	}
	if clean[0].path != "/deploy" || clean[1].path != "/restart" {
		t.Errorf("unexpected clean routes: %v", clean)
	}
	if len(removed) != 2 {
		t.Fatalf("expected 2 removed routes, got %d", len(removed))
	}
}

func setupTestManager(t *testing.T) *Manager {
	t.Helper()
	m := NewManager(5*time.Minute, 20*1024*1024)
	return m
}

func TestHandleNonClipboardPath(t *testing.T) {
	m := setupTestManager(t)
	h := NewHandler(m)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handled := h.Handle(w, req)
	if handled {
		t.Fatal("expected non-clipboard path to not be handled")
	}
}

func TestHandleNoAuth(t *testing.T) {
	m := setupTestManager(t)
	h := NewHandler(m)

	req := httptest.NewRequest(http.MethodGet, "/clipboard/type", nil)
	w := httptest.NewRecorder()

	handled := h.Handle(w, req)
	if !handled {
		t.Fatal("expected clipboard path to be handled")
	}
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleInvalidToken(t *testing.T) {
	m := setupTestManager(t)
	h := NewHandler(m)

	req := httptest.NewRequest(http.MethodGet, "/clipboard/type", nil)
	req.Header.Set("Authorization", "Bearer invalid-token-xyz")
	w := httptest.NewRecorder()

	handled := h.Handle(w, req)
	if !handled {
		t.Fatal("expected clipboard path to be handled")
	}
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandleTypeEmpty(t *testing.T) {
	m := setupTestManager(t)
	h := NewHandler(m)

	// Register a barrel to get a valid token.
	session := BarrelSession{
		ContainerName: "barrel-test",
		ToolName:      "claude",
		ClipboardMode: "shim",
		Eligible:      true,
	}
	if err := m.RegisterBarrel(session); err != nil {
		t.Fatal(err)
	}
	sessions := m.ActiveSessions()
	token := sessions[0].Token

	req := httptest.NewRequest(http.MethodGet, "/clipboard/type", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp typeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.State != "empty" {
		t.Errorf("expected state 'empty', got %q", resp.State)
	}
}

func TestHandleTypeStaged(t *testing.T) {
	m := setupTestManager(t)
	h := NewHandler(m)

	// Stage an image.
	obj := ClipboardObject{
		Kind:    ClipboardKindImage,
		MIME:    "image/jpeg",
		RawSize: 100,
		Raw:     make([]byte, 100),
		Variants: map[string]ClipboardVariant{
			"image/png": {MIME: "image/png", Bytes: make([]byte, 150), Size: 150},
		},
	}
	if _, err := m.Stage(obj, 5*time.Minute); err != nil {
		t.Fatal(err)
	}

	// Register barrel.
	session := BarrelSession{
		ContainerName: "barrel-test",
		ToolName:      "claude",
		ClipboardMode: "shim",
		Eligible:      true,
	}
	if err := m.RegisterBarrel(session); err != nil {
		t.Fatal(err)
	}
	token := m.ActiveSessions()[0].Token

	req := httptest.NewRequest(http.MethodGet, "/clipboard/type", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp typeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.State != "staged" {
		t.Errorf("expected state 'staged', got %q", resp.State)
	}
	if resp.Kind != "image" {
		t.Errorf("expected kind 'image', got %q", resp.Kind)
	}
	if resp.RawMIME != "image/jpeg" {
		t.Errorf("expected raw_mime 'image/jpeg', got %q", resp.RawMIME)
	}
}

func TestHandleImageEmpty(t *testing.T) {
	m := setupTestManager(t)
	h := NewHandler(m)

	session := BarrelSession{
		ContainerName: "barrel-test",
		ToolName:      "claude",
		ClipboardMode: "shim",
		Eligible:      true,
	}
	if err := m.RegisterBarrel(session); err != nil {
		t.Fatal(err)
	}
	token := m.ActiveSessions()[0].Token

	req := httptest.NewRequest(http.MethodGet, "/clipboard/image", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestHandleImageStaged(t *testing.T) {
	m := setupTestManager(t)
	h := NewHandler(m)

	pngBytes := []byte("fake-png-data-for-test")
	obj := ClipboardObject{
		Kind:    ClipboardKindImage,
		MIME:    "image/png",
		RawSize: int64(len(pngBytes)),
		Raw:     pngBytes,
		Variants: map[string]ClipboardVariant{
			"image/png": {MIME: "image/png", Bytes: pngBytes, Size: int64(len(pngBytes))},
		},
	}
	if _, err := m.Stage(obj, 5*time.Minute); err != nil {
		t.Fatal(err)
	}

	session := BarrelSession{
		ContainerName: "barrel-test",
		ToolName:      "claude",
		ClipboardMode: "shim",
		Eligible:      true,
	}
	if err := m.RegisterBarrel(session); err != nil {
		t.Fatal(err)
	}
	token := m.ActiveSessions()[0].Token

	req := httptest.NewRequest(http.MethodGet, "/clipboard/image", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("expected Content-Type image/png, got %q", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("expected Cache-Control no-store, got %q", cc)
	}
	if w.Header().Get("X-Cooper-Clipboard-Id") == "" {
		t.Error("expected X-Cooper-Clipboard-Id header")
	}
	if string(w.Body.Bytes()) != string(pngBytes) {
		t.Error("response body does not match staged image")
	}
}

func TestHandleIneligibleBarrel(t *testing.T) {
	m := setupTestManager(t)
	h := NewHandler(m)

	session := BarrelSession{
		ContainerName: "barrel-off",
		ToolName:      "custom",
		ClipboardMode: "off",
		Eligible:      false,
	}
	if err := m.RegisterBarrel(session); err != nil {
		t.Fatal(err)
	}
	token := m.ActiveSessions()[0].Token

	req := httptest.NewRequest(http.MethodGet, "/clipboard/type", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestHandleUnknownClipboardEndpoint(t *testing.T) {
	m := setupTestManager(t)
	h := NewHandler(m)

	session := BarrelSession{
		ContainerName: "barrel-test",
		ToolName:      "claude",
		ClipboardMode: "shim",
		Eligible:      true,
	}
	if err := m.RegisterBarrel(session); err != nil {
		t.Fatal(err)
	}
	token := m.ActiveSessions()[0].Token

	req := httptest.NewRequest(http.MethodGet, "/clipboard/unknown", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}
