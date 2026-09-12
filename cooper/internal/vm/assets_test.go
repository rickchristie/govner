package vm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAssetStoreDownloadsAndReusesVerifiedAsset(t *testing.T) {
	t.Parallel()
	data := []byte("locked VM asset")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write(data)
	}))
	defer server.Close()
	asset := testAsset("asset.bin", server.URL, data)
	store := AssetStore{Dir: t.TempDir(), Client: server.Client()}

	first, err := store.Ensure(context.Background(), asset)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Ensure(context.Background(), asset)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || requests.Load() != 1 {
		t.Fatalf("paths = %q,%q; requests = %d", first, second, requests.Load())
	}
	got, err := os.ReadFile(first)
	if err != nil || string(got) != string(data) {
		t.Fatalf("asset data = %q, %v", got, err)
	}
}

func TestAssetStoreRejectsWrongHashAndLeavesNoFinalFile(t *testing.T) {
	t.Parallel()
	data := []byte("wrong")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(data)
	}))
	defer server.Close()
	asset := testAsset("asset.bin", server.URL, data)
	asset.SHA256 = strings.Repeat("0", 64)
	store := AssetStore{Dir: t.TempDir(), Client: server.Client()}
	if _, err := store.Ensure(context.Background(), asset); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("Ensure() error = %v, want SHA-256 error", err)
	}
	if _, err := os.Stat(filepath.Join(store.Dir, asset.Name)); !os.IsNotExist(err) {
		t.Fatalf("invalid final asset exists: %v", err)
	}
}

func TestAssetStoreReplacesInvalidExistingFile(t *testing.T) {
	t.Parallel()
	data := []byte("correct")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(data)
	}))
	defer server.Close()
	asset := testAsset("asset.bin", server.URL, data)
	store := AssetStore{Dir: t.TempDir(), Client: server.Client()}
	path := filepath.Join(store.Dir, asset.Name)
	if err := os.WriteFile(path, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	gotPath, err := store.Ensure(context.Background(), asset)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(gotPath)
	if string(got) != string(data) {
		t.Fatalf("asset = %q, want %q", got, data)
	}
}

func TestValidateAssetRejectsUnsafeValues(t *testing.T) {
	t.Parallel()
	tests := []Asset{
		{Name: "../asset", URL: "https://example.test", SHA256: strings.Repeat("0", 64), Size: 1},
		{Name: ".", URL: "https://example.test", SHA256: strings.Repeat("0", 64), Size: 1},
		{Name: "..", URL: "https://example.test", SHA256: strings.Repeat("0", 64), Size: 1},
		{Name: "asset", SHA256: strings.Repeat("0", 64), Size: 1},
		{Name: "asset", URL: "https://example.test", SHA256: "bad", Size: 1},
		{Name: "asset", URL: "https://example.test", SHA256: strings.Repeat("0", 64)},
	}
	for _, asset := range tests {
		if err := validateAsset(asset); err == nil {
			t.Fatalf("validateAsset(%#v) succeeded", asset)
		}
	}
}

func testAsset(name, url string, data []byte) Asset {
	hash := sha256.Sum256(data)
	return Asset{Name: name, URL: url, SHA256: hex.EncodeToString(hash[:]), Size: int64(len(data))}
}
