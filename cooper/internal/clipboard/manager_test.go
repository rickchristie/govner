package clipboard

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func withMockInspectContainerSession(t *testing.T, fn func(string) (*BarrelSession, error)) {
	t.Helper()
	original := inspectContainerSession
	inspectContainerSession = fn
	t.Cleanup(func() {
		inspectContainerSession = original
	})
}

func TestStageCreatesValidSnapshot(t *testing.T) {
	mgr := NewManager(5*time.Second, 1024)

	obj := ClipboardObject{
		Kind: ClipboardKindImage,
		MIME: "image/png",
		Raw:  []byte("fake-png-data"),
	}

	snap, err := mgr.Stage(obj, 0)
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}

	if snap.ID == "" {
		t.Error("snapshot ID is empty")
	}
	if len(snap.ID) != 32 { // 16 bytes = 32 hex chars
		t.Errorf("snapshot ID length = %d, want 32", len(snap.ID))
	}
	if snap.Object.Kind != ClipboardKindImage {
		t.Errorf("Object.Kind = %q, want %q", snap.Object.Kind, ClipboardKindImage)
	}
	if snap.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
	if snap.ExpiresAt.IsZero() {
		t.Error("ExpiresAt is zero")
	}
	expectedExpiry := snap.CreatedAt.Add(5 * time.Second)
	if snap.ExpiresAt.Sub(expectedExpiry).Abs() > time.Millisecond {
		t.Errorf("ExpiresAt = %v, want ~%v", snap.ExpiresAt, expectedExpiry)
	}
	if snap.AccessCount != 0 {
		t.Errorf("AccessCount = %d, want 0", snap.AccessCount)
	}

	cur := mgr.Current()
	if cur == nil || cur.ID != snap.ID {
		t.Error("Current() does not match staged snapshot")
	}
}

func TestStageReplacesPrevious(t *testing.T) {
	mgr := NewManager(5*time.Second, 1024)

	obj1 := ClipboardObject{Kind: ClipboardKindText, Raw: []byte("first")}
	snap1, err := mgr.Stage(obj1, 0)
	if err != nil {
		t.Fatalf("Stage 1: %v", err)
	}

	obj2 := ClipboardObject{Kind: ClipboardKindText, Raw: []byte("second")}
	snap2, err := mgr.Stage(obj2, 0)
	if err != nil {
		t.Fatalf("Stage 2: %v", err)
	}

	if snap1.ID == snap2.ID {
		t.Error("second snapshot has same ID as first")
	}

	cur := mgr.Current()
	if cur == nil || cur.ID != snap2.ID {
		t.Error("Current() should return second snapshot")
	}
}

func TestClearRemovesSnapshot(t *testing.T) {
	mgr := NewManager(5*time.Second, 1024)

	obj := ClipboardObject{Kind: ClipboardKindText, Raw: []byte("hello")}
	if _, err := mgr.Stage(obj, 0); err != nil {
		t.Fatalf("Stage: %v", err)
	}

	mgr.Clear()

	if cur := mgr.Current(); cur != nil {
		t.Errorf("Current() = %v after Clear, want nil", cur)
	}
}

func TestTouchUpdatesAccessTimeAndCount(t *testing.T) {
	mgr := NewManager(5*time.Second, 1024)

	obj := ClipboardObject{Kind: ClipboardKindText, Raw: []byte("hello")}
	snap, err := mgr.Stage(obj, 0)
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}

	originalAccess := snap.LastAccessAt
	time.Sleep(5 * time.Millisecond)

	mgr.Touch(snap.ID)
	cur := mgr.Current()
	if cur == nil {
		t.Fatal("Current() is nil after Touch")
	}
	if cur.AccessCount != 1 {
		t.Errorf("AccessCount = %d, want 1", cur.AccessCount)
	}
	if !cur.LastAccessAt.After(originalAccess) {
		t.Error("LastAccessAt was not updated by Touch")
	}

	// Touch again
	mgr.Touch(snap.ID)
	cur = mgr.Current()
	if cur.AccessCount != 2 {
		t.Errorf("AccessCount = %d after second Touch, want 2", cur.AccessCount)
	}
}

func TestTouchNoOpForWrongID(t *testing.T) {
	mgr := NewManager(5*time.Second, 1024)

	obj := ClipboardObject{Kind: ClipboardKindText, Raw: []byte("hello")}
	snap, err := mgr.Stage(obj, 0)
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}

	mgr.Touch("nonexistent-id")
	cur := mgr.Current()
	if cur.AccessCount != snap.AccessCount {
		t.Error("Touch with wrong ID should be a no-op")
	}
}

func TestStageExceedsMaxBytes(t *testing.T) {
	mgr := NewManager(5*time.Second, 100)

	obj := ClipboardObject{
		Kind: ClipboardKindImage,
		Raw:  make([]byte, 60),
		Variants: map[string]ClipboardVariant{
			"image/png": {Bytes: make([]byte, 50)},
		},
	}

	_, err := mgr.Stage(obj, 0)
	if err == nil {
		t.Error("Stage should fail when object exceeds maxBytes")
	}
}

func TestStageCustomTTL(t *testing.T) {
	mgr := NewManager(5*time.Second, 1024)

	obj := ClipboardObject{Kind: ClipboardKindText, Raw: []byte("x")}
	snap, err := mgr.Stage(obj, 10*time.Second)
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}

	expectedExpiry := snap.CreatedAt.Add(10 * time.Second)
	if snap.ExpiresAt.Sub(expectedExpiry).Abs() > time.Millisecond {
		t.Errorf("ExpiresAt = %v, want ~%v (custom TTL)", snap.ExpiresAt, expectedExpiry)
	}
}

func TestUpdatePolicyAffectsSubsequentStages(t *testing.T) {
	mgr := NewManager(5*time.Second, 1024)
	mgr.UpdatePolicy(30*time.Second, 64)

	snap, err := mgr.Stage(ClipboardObject{Kind: ClipboardKindText, Raw: make([]byte, 32)}, 0)
	if err != nil {
		t.Fatalf("Stage with updated policy: %v", err)
	}

	expectedExpiry := snap.CreatedAt.Add(30 * time.Second)
	if snap.ExpiresAt.Sub(expectedExpiry).Abs() > time.Millisecond {
		t.Errorf("ExpiresAt = %v, want ~%v", snap.ExpiresAt, expectedExpiry)
	}

	_, err = mgr.Stage(ClipboardObject{Kind: ClipboardKindText, Raw: make([]byte, 65)}, 0)
	if err == nil {
		t.Fatal("expected updated maxBytes policy to reject oversized object")
	}
}

func TestExpiredSnapshot(t *testing.T) {
	mgr := NewManager(5*time.Second, 1024)

	obj := ClipboardObject{Kind: ClipboardKindText, Raw: []byte("ephemeral")}
	snap, err := mgr.Stage(obj, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}

	if snap.IsExpired() {
		t.Error("snapshot should not be expired immediately")
	}

	time.Sleep(20 * time.Millisecond)

	// The snapshot struct itself knows it's expired.
	cur := mgr.Current()
	if cur == nil {
		t.Fatal("Current() should still return the snapshot (manager does not auto-evict)")
	}
	if !cur.IsExpired() {
		t.Error("snapshot should be expired after TTL")
	}
	if cur.RemainingTTL() != 0 {
		t.Errorf("RemainingTTL() = %v, want 0", cur.RemainingTTL())
	}
}

// --- Token generation ---

func TestGenerateTokenUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		tok, err := GenerateToken()
		if err != nil {
			t.Fatalf("GenerateToken: %v", err)
		}
		if len(tok) != 64 {
			t.Fatalf("token length = %d, want 64", len(tok))
		}
		if seen[tok] {
			t.Fatalf("duplicate token on iteration %d", i)
		}
		seen[tok] = true
	}
}

// --- Session management ---

func TestRegisterAndValidateToken(t *testing.T) {
	mgr := NewManager(5*time.Second, 1024)

	sess := BarrelSession{
		ContainerName: "barrel-test",
		ToolName:      "claude-code",
		ClipboardMode: "auto",
		Eligible:      true,
	}

	if err := mgr.RegisterBarrel(sess); err != nil {
		t.Fatalf("RegisterBarrel: %v", err)
	}

	sessions := mgr.ActiveSessions()
	if len(sessions) != 1 {
		t.Fatalf("ActiveSessions() = %d, want 1", len(sessions))
	}

	token := sessions[0].Token
	if token == "" {
		t.Fatal("registered session has empty token")
	}

	validated, err := mgr.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if validated.ContainerName != "barrel-test" {
		t.Errorf("ContainerName = %q, want %q", validated.ContainerName, "barrel-test")
	}
	if validated.ToolName != "claude-code" {
		t.Errorf("ToolName = %q, want %q", validated.ToolName, "claude-code")
	}
}

func TestValidateTokenRejectsUnknown(t *testing.T) {
	mgr := NewManager(5*time.Second, 1024)

	_, err := mgr.ValidateToken("not-a-real-token")
	if err == nil {
		t.Error("ValidateToken should reject unknown tokens")
	}
}

func TestUnregisterBarrelInvalidatesToken(t *testing.T) {
	mgr := NewManager(5*time.Second, 1024)

	sess := BarrelSession{
		ContainerName: "barrel-remove",
		ToolName:      "tool",
		ClipboardMode: "auto",
		Eligible:      true,
	}

	if err := mgr.RegisterBarrel(sess); err != nil {
		t.Fatalf("RegisterBarrel: %v", err)
	}

	sessions := mgr.ActiveSessions()
	token := sessions[0].Token

	mgr.UnregisterBarrel("barrel-remove")

	_, err := mgr.ValidateToken(token)
	if err == nil {
		t.Error("ValidateToken should fail after UnregisterBarrel")
	}

	sessions = mgr.ActiveSessions()
	if len(sessions) != 0 {
		t.Errorf("ActiveSessions() = %d after unregister, want 0", len(sessions))
	}
}

func TestRegisterBarrelReplacesExisting(t *testing.T) {
	mgr := NewManager(5*time.Second, 1024)

	sess := BarrelSession{
		ContainerName: "barrel-replace",
		ToolName:      "tool-v1",
		ClipboardMode: "auto",
		Eligible:      true,
	}
	if err := mgr.RegisterBarrel(sess); err != nil {
		t.Fatalf("RegisterBarrel 1: %v", err)
	}

	oldToken := mgr.ActiveSessions()[0].Token

	sess.ToolName = "tool-v2"
	if err := mgr.RegisterBarrel(sess); err != nil {
		t.Fatalf("RegisterBarrel 2: %v", err)
	}

	// Old token should be invalid.
	if _, err := mgr.ValidateToken(oldToken); err == nil {
		t.Error("old token should be invalid after re-registration")
	}

	// New token should work.
	sessions := mgr.ActiveSessions()
	if len(sessions) != 1 {
		t.Fatalf("ActiveSessions() = %d, want 1", len(sessions))
	}
	newToken := sessions[0].Token
	validated, err := mgr.ValidateToken(newToken)
	if err != nil {
		t.Fatalf("ValidateToken new: %v", err)
	}
	if validated.ToolName != "tool-v2" {
		t.Errorf("ToolName = %q, want %q", validated.ToolName, "tool-v2")
	}
}

func TestUnregisterBarrelNoOpForUnknown(t *testing.T) {
	mgr := NewManager(5*time.Second, 1024)
	// Should not panic.
	mgr.UnregisterBarrel("does-not-exist")
}

// --- Token file management ---

func TestWriteAndRemoveTokenFile(t *testing.T) {
	dir := t.TempDir()

	path, err := WriteTokenFile(dir, "my-barrel", "secret-token-value")
	if err != nil {
		t.Fatalf("WriteTokenFile: %v", err)
	}

	expectedPath := filepath.Join(dir, "tokens", "my-barrel")
	if path != expectedPath {
		t.Errorf("path = %q, want %q", path, expectedPath)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "secret-token-value" {
		t.Errorf("token file content = %q, want %q", string(data), "secret-token-value")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}

	// Remove
	if err := RemoveTokenFile(dir, "my-barrel"); err != nil {
		t.Fatalf("RemoveTokenFile: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("token file should be removed")
	}
}

func TestRemoveTokenFileNoOpForMissing(t *testing.T) {
	dir := t.TempDir()
	if err := RemoveTokenFile(dir, "nonexistent"); err != nil {
		t.Errorf("RemoveTokenFile should not error for missing file: %v", err)
	}
}

func TestTokenFilePath(t *testing.T) {
	p := TokenFilePath("/tmp/cooper", "barrel-1")
	expected := filepath.Join("/tmp/cooper", "tokens", "barrel-1")
	if p != expected {
		t.Errorf("TokenFilePath = %q, want %q", p, expected)
	}
}

func TestValidateTokenFromDiskUsesContainerInspection(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(5*time.Second, 1024)
	mgr.SetCooperDir(dir)

	token := "disk-token"
	if _, err := WriteTokenFile(dir, "barrel-custom-off", token); err != nil {
		t.Fatalf("WriteTokenFile: %v", err)
	}

	withMockInspectContainerSession(t, func(containerName string) (*BarrelSession, error) {
		if containerName != "barrel-custom-off" {
			t.Fatalf("unexpected container inspection: %s", containerName)
		}
		return &BarrelSession{
			ToolName:      "custom-off",
			ClipboardMode: "off",
			Eligible:      false,
		}, nil
	})

	sess, err := mgr.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if sess.Eligible {
		t.Fatal("expected disk-backed session to inherit ineligible clipboard mode")
	}
	if sess.ClipboardMode != "off" {
		t.Fatalf("ClipboardMode = %q, want off", sess.ClipboardMode)
	}
	if got := len(mgr.ActiveSessions()); got != 0 {
		t.Fatalf("disk-backed sessions should not be cached in ActiveSessions, got %d", got)
	}
}

func TestValidateTokenFromDiskRejectsStoppedContainer(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(5*time.Second, 1024)
	mgr.SetCooperDir(dir)

	token := "stopped-token"
	if _, err := WriteTokenFile(dir, "barrel-stopped", token); err != nil {
		t.Fatalf("WriteTokenFile: %v", err)
	}

	withMockInspectContainerSession(t, func(containerName string) (*BarrelSession, error) {
		if containerName != "barrel-stopped" {
			t.Fatalf("unexpected container inspection: %s", containerName)
		}
		return nil, nil
	})

	if _, err := mgr.ValidateToken(token); err == nil {
		t.Fatal("expected stopped container token to be rejected")
	}
}

func TestValidateTokenFromDiskRejectsRotatedToken(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(5*time.Second, 1024)
	mgr.SetCooperDir(dir)

	withMockInspectContainerSession(t, func(containerName string) (*BarrelSession, error) {
		return &BarrelSession{
			ToolName:      containerName,
			ClipboardMode: "auto",
			Eligible:      true,
		}, nil
	})

	if _, err := WriteTokenFile(dir, "barrel-rotate", "token-one"); err != nil {
		t.Fatalf("WriteTokenFile token-one: %v", err)
	}
	if _, err := mgr.ValidateToken("token-one"); err != nil {
		t.Fatalf("ValidateToken token-one: %v", err)
	}

	if _, err := WriteTokenFile(dir, "barrel-rotate", "token-two"); err != nil {
		t.Fatalf("WriteTokenFile token-two: %v", err)
	}

	if _, err := mgr.ValidateToken("token-one"); err == nil {
		t.Fatal("expected rotated-out token to be rejected")
	}
	if _, err := mgr.ValidateToken("token-two"); err != nil {
		t.Fatalf("ValidateToken token-two: %v", err)
	}
}

// --- Concurrency tests (run with -race) ---

func TestConcurrentStageClearCurrent(t *testing.T) {
	mgr := NewManager(5*time.Second, 1024)
	var wg sync.WaitGroup

	// Concurrent Stage calls.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			obj := ClipboardObject{Kind: ClipboardKindText, Raw: []byte("data")}
			mgr.Stage(obj, 0)
		}()
	}

	// Concurrent Clear calls.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mgr.Clear()
		}()
	}

	// Concurrent Current calls.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mgr.Current()
		}()
	}

	// Concurrent Touch calls.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mgr.Touch("any-id")
		}()
	}

	wg.Wait()
}

func TestConcurrentRegisterValidate(t *testing.T) {
	mgr := NewManager(5*time.Second, 1024)
	var wg sync.WaitGroup

	// Register several barrels concurrently.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sess := BarrelSession{
				ContainerName: fmt.Sprintf("barrel-%d", n),
				ToolName:      "tool",
				ClipboardMode: "auto",
				Eligible:      true,
			}
			mgr.RegisterBarrel(sess)
		}(i)
	}

	// Validate and unregister concurrently.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mgr.ValidateToken("random-token")
		}()
	}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			mgr.UnregisterBarrel(fmt.Sprintf("barrel-%d", n))
		}(i)
	}

	wg.Wait()
}
