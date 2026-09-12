package vm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RemoveCache removes only the Cooper-owned VM tree after all VM resources
// have stopped. Host agent state is never below this validated target.
func RemoveCache(cooperDir string) error {
	if !filepath.IsAbs(cooperDir) || filepath.Clean(cooperDir) == string(filepath.Separator) {
		return fmt.Errorf("cooper directory must be absolute")
	}
	target := filepath.Join(filepath.Clean(cooperDir), "vm")
	relative, err := filepath.Rel(filepath.Clean(cooperDir), target)
	if err != nil || relative != "vm" || strings.HasPrefix(relative, "..") {
		return fmt.Errorf("refuse unsafe Cooper VM cache removal")
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("remove Cooper VM cache: %w", err)
	}
	if err := os.RemoveAll(controlRoot(cooperDir)); err != nil {
		return fmt.Errorf("remove Cooper VM control cache: %w", err)
	}
	return nil
}
