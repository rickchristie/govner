package clipboard

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// GenerateToken creates a cryptographically random 32-byte token,
// returned as a 64-character hex string.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// TokenFilePath returns the path where a barrel's token file is stored.
func TokenFilePath(dir, containerName string) string {
	return filepath.Join(dir, "tokens", containerName)
}

// WriteTokenFile writes a token to disk for the given barrel. The file
// is stored at {dir}/tokens/{containerName} with mode 0600 so only
// the owning user can read it. The tokens subdirectory is created if
// it does not exist.
func WriteTokenFile(dir, containerName, token string) (string, error) {
	tokensDir := filepath.Join(dir, "tokens")
	if err := os.MkdirAll(tokensDir, 0700); err != nil {
		return "", fmt.Errorf("create tokens directory: %w", err)
	}

	p := TokenFilePath(dir, containerName)
	if err := os.WriteFile(p, []byte(token), 0600); err != nil {
		return "", fmt.Errorf("write token file: %w", err)
	}
	return p, nil
}

// RemoveTokenFile deletes the token file for the given barrel.
func RemoveTokenFile(dir, containerName string) error {
	p := TokenFilePath(dir, containerName)
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove token file: %w", err)
	}
	return nil
}
