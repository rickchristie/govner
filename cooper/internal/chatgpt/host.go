package chatgpt

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// HostVersion reads package metadata without starting a desktop application
// or attaching to the user's existing instance.
func HostVersion() (string, error) {
	path, err := exec.LookPath("chatgpt")
	if err != nil {
		return "", err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	return readHostVersion(filepath.Join(filepath.Dir(path), "resources", "linux-package-metadata.json"))
}

func readHostVersion(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read ChatGPT package metadata: %w", err)
	}
	var metadata struct {
		Version string `json:"version"`
		Brand   string `json:"codexAppBrand"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return "", fmt.Errorf("decode ChatGPT package metadata: %w", err)
	}
	if metadata.Brand != "chatgpt" || !versionPattern.MatchString(metadata.Version) {
		return "", fmt.Errorf("ChatGPT package metadata has an invalid brand or version")
	}
	return metadata.Version, nil
}
