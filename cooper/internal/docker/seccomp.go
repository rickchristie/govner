package docker

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed seccomp-bwrap.json
var seccompProfile []byte

// EnsureSeccompProfile writes the embedded seccomp profile to
// {cooperDir}/cli/seccomp.json if it does not already exist, or if the
// content has changed. Returns the path to the written file.
func EnsureSeccompProfile(cooperDir string) (string, error) {
	dir := filepath.Join(cooperDir, "cli")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create cli dir for seccomp: %w", err)
	}

	outPath := filepath.Join(dir, "seccomp.json")

	// Check if file already exists with same content.
	existing, err := os.ReadFile(outPath)
	if err == nil && string(existing) == string(seccompProfile) {
		return outPath, nil
	}

	if err := os.WriteFile(outPath, seccompProfile, 0644); err != nil {
		return "", fmt.Errorf("write seccomp profile: %w", err)
	}

	return outPath, nil
}
