package profiles

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Directory and file modification times can affect native cache validation.
// Restore them only on new private copies, before they become live. Child
// symlinks keep their targets; their own timestamps are not part of the copy
// contract. Copy mode already checks bytes, names, links, and permission bits.
func (s *Service) copyManaged(ctx context.Context, source, target string) error {
	if err := s.copy(ctx, source, target); err != nil {
		return err
	}
	input, err := os.OpenRoot(filepath.Dir(source))
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenRoot(filepath.Dir(target))
	if err != nil {
		return err
	}
	defer output.Close()
	type entry struct {
		name     string
		modified time.Time
	}
	var entries []entry
	err = visitTree(ctx, input, filepath.Base(source), ".", func(_ *os.Root, _ string, relative string, info fs.FileInfo) error {
		if info.Mode()&os.ModeSymlink == 0 {
			entries = append(entries, entry{filepath.Join(filepath.Base(target), relative), info.ModTime()})
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i := len(entries) - 1; i >= 0; i-- {
		value := entries[i]
		if err := output.Chtimes(value.name, value.modified, value.modified); err != nil {
			return err
		}
		if err := syncRoot(output, value.name); err != nil {
			return err
		}
	}
	return syncRoot(output, ".")
}
