package clipboard

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const TokenMetadataVersion = 1

const (
	RuntimeCLI = "cli"
	RuntimeVM  = "vm"
)

// TokenMetadata is the host-written clipboard authorization record for one
// running workload. The guest can read it but cannot replace it.
type TokenMetadata struct {
	Version       int       `json:"version"`
	Token         string    `json:"token"`
	RuntimeID     string    `json:"runtime_id"`
	RuntimeKind   string    `json:"runtime_kind"`
	ToolName      string    `json:"tool_name"`
	ClipboardMode string    `json:"clipboard_mode"`
	CreatedAt     time.Time `json:"created_at"`
}

func (m TokenMetadata) validate() error {
	if m.Version != TokenMetadataVersion {
		return fmt.Errorf("unsupported clipboard token version %d", m.Version)
	}
	if strings.TrimSpace(m.Token) == "" || len(m.Token) > 256 {
		return fmt.Errorf("clipboard token is invalid")
	}
	if !validRuntimeID(m.RuntimeID) {
		return fmt.Errorf("clipboard runtime ID is invalid")
	}
	if m.RuntimeKind != RuntimeCLI && m.RuntimeKind != RuntimeVM {
		return fmt.Errorf("clipboard runtime kind %q is invalid", m.RuntimeKind)
	}
	if strings.TrimSpace(m.ToolName) == "" {
		return fmt.Errorf("clipboard tool name is required")
	}
	if normalized := normalizeClipboardMode(m.ClipboardMode); normalized != m.ClipboardMode || !validClipboardMode(m.ClipboardMode) {
		return fmt.Errorf("clipboard mode %q is invalid", m.ClipboardMode)
	}
	if m.CreatedAt.IsZero() || m.CreatedAt.After(time.Now().Add(time.Minute)) {
		return fmt.Errorf("clipboard creation time is invalid")
	}
	return nil
}

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

// WriteRuntimeToken writes an atomic host authorization record with mode 0600.
func WriteRuntimeToken(dir, runtimeID, token, runtimeKind, toolName, clipboardMode string) (string, error) {
	metadata := TokenMetadata{
		Version: TokenMetadataVersion, Token: token, RuntimeID: runtimeID,
		RuntimeKind: runtimeKind, ToolName: toolName,
		ClipboardMode: normalizeClipboardMode(clipboardMode), CreatedAt: time.Now().UTC(),
	}
	return writeTokenMetadata(dir, metadata)
}

func writeTokenMetadata(dir string, metadata TokenMetadata) (string, error) {
	if err := metadata.validate(); err != nil {
		return "", err
	}
	tokensDir := filepath.Join(dir, "tokens")
	if err := os.MkdirAll(tokensDir, 0700); err != nil {
		return "", fmt.Errorf("create tokens directory: %w", err)
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode clipboard token metadata: %w", err)
	}
	temporary, err := os.CreateTemp(tokensDir, ".clipboard-token-*.part")
	if err != nil {
		return "", fmt.Errorf("create clipboard token temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return "", fmt.Errorf("write clipboard token metadata: %w", err)
	}
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return "", fmt.Errorf("set clipboard token mode: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close clipboard token metadata: %w", err)
	}
	p := TokenFilePath(dir, metadata.RuntimeID)
	if err := os.Rename(temporaryPath, p); err != nil {
		return "", fmt.Errorf("install clipboard token metadata: %w", err)
	}
	return p, nil
}

// ReadTokenMetadata reads and validates one host authorization record.
func ReadTokenMetadata(path string) (TokenMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return TokenMetadata{}, err
	}
	var metadata TokenMetadata
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		return TokenMetadata{}, fmt.Errorf("decode clipboard token metadata: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("unexpected data after JSON object")
		}
		return TokenMetadata{}, fmt.Errorf("decode clipboard token metadata: %w", err)
	}
	if err := metadata.validate(); err != nil {
		return TokenMetadata{}, err
	}
	return metadata, nil
}

// RotateRuntimeToken keeps the reviewed identity and replaces only its token.
func RotateRuntimeToken(dir, runtimeID, token string) (string, error) {
	if !validRuntimeID(runtimeID) {
		return "", fmt.Errorf("clipboard runtime ID is invalid")
	}
	metadata, err := ReadTokenMetadata(TokenFilePath(dir, runtimeID))
	if err != nil {
		return "", fmt.Errorf("read clipboard token before rotation: %w", err)
	}
	if metadata.RuntimeID != runtimeID {
		return "", fmt.Errorf("clipboard runtime ID does not match its file name")
	}
	metadata.Token = token
	metadata.CreatedAt = time.Now().UTC()
	return writeTokenMetadata(dir, metadata)
}

// RemoveTokenFile deletes the token file for the given barrel.
func RemoveTokenFile(dir, containerName string) error {
	if !validRuntimeID(containerName) {
		return fmt.Errorf("clipboard runtime ID is invalid")
	}
	p := TokenFilePath(dir, containerName)
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove token file: %w", err)
	}
	return nil
}

func validRuntimeID(value string) bool {
	return value != "" && value != "." && value != ".." &&
		filepath.Base(value) == value && !strings.ContainsAny(value, "/\\\x00\r\n")
}
