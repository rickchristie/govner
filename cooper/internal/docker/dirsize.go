package docker

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// DirSizeBytes returns the total size of regular files beneath dir.
func DirSizeBytes(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("walk dir size %s: %w", dir, err)
	}
	return total, nil
}
