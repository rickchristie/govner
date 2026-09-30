package chatgpt

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// HostVersion reads package metadata without starting a desktop application
// or attaching to the user's existing instance.
func HostVersion() (string, error) {
	path, err := exec.LookPath("chatgpt")
	if err != nil {
		return "", err
	}
	for depth := 0; depth < 8; depth++ {
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return "", err
		}
		version, err := readHostVersion(filepath.Join(filepath.Dir(path), "resources", "linux-package-metadata.json"))
		if err == nil || !errors.Is(err, os.ErrNotExist) {
			return version, err
		}
		path, err = wrapperTarget(path)
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("ChatGPT launcher chain is too long or contains a loop")
}

// A user can wrap the package launcher to add display flags. Follow only a
// simple, literal exec path. Never start the wrapper or guess a different app
// from PATH, because its version can differ from the selected host app.
func wrapperTarget(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil {
		return "", err
	}
	var target string
	if len(data) <= 65536 && strings.HasPrefix(string(data), "#!/") {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Fields(line)
			if target != "" || len(fields) < 2 || fields[0] != "exec" || !filepath.IsAbs(fields[1]) ||
				strings.ContainsAny(fields[1], "\"'\\$`;&|()<>*?[]{}") {
				return "", fmt.Errorf("cannot read ChatGPT version through unsupported launcher %s", path)
			}
			target = fields[1]
		}
	}
	if target == "" {
		return "", fmt.Errorf("ChatGPT launcher %s has no package metadata or literal exec path", path)
	}
	return target, nil
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
