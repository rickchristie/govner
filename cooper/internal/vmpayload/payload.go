// Package vmpayload contains fixed executable payloads for the Linux VM
// runtime. Cooper verifies each payload before it writes a build context.
package vmpayload

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
)

const (
	VirtioFSDVersion = "1.14.0"
	VirtioFSDSize    = 2966496
	VirtioFSDSHA256  = "3bde9d848edf61fd30448dbd98c016533500ecffbf990455a0bd72e34b7a26b3"
)

//go:embed virtiofsd-v1.14.0-linux-amd64.gz
var compressedVirtioFSD []byte

// VirtioFSD returns the verified static Linux x86-64 executable.
func VirtioFSD() ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(compressedVirtioFSD))
	if err != nil {
		return nil, fmt.Errorf("open embedded virtiofsd: %w", err)
	}
	payload, readErr := io.ReadAll(io.LimitReader(reader, VirtioFSDSize+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read embedded virtiofsd: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close embedded virtiofsd: %w", closeErr)
	}
	if len(payload) != VirtioFSDSize {
		return nil, fmt.Errorf("embedded virtiofsd size is %d; want %d", len(payload), VirtioFSDSize)
	}
	digest := sha256.Sum256(payload)
	if hex.EncodeToString(digest[:]) != VirtioFSDSHA256 {
		return nil, fmt.Errorf("embedded virtiofsd SHA-256 is not the reviewed value")
	}
	return payload, nil
}
