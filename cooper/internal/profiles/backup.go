package profiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rickchristie/govner/cooper/internal/workload"
)

type backupProfile struct {
	Manifest Manifest          `json:"manifest"`
	Digests  map[string]string `json:"digests"`
}

type backupIndex struct {
	Schema   int             `json:"schema"`
	Profiles []backupProfile `json:"profiles"`
}

func backupData(profile Manifest, root Root) string {
	return filepath.Join("data", profile.ID, root.ID)
}

// Backup makes an independent history copy. Check both sides of each copy
// and retain a failed stage so no partial copy can look like a backup.
func (s *Service) Backup(ctx context.Context, destination string) error {
	store, lock, state, err := s.locked(ctx, false, true)
	if err != nil {
		return err
	}
	defer store.Close()
	defer lock.Close()
	if len(state.Profiles) == 0 {
		return errors.New("there are no profiles to back up")
	}
	if err := s.checkBackupPath(state, destination); err != nil {
		return err
	}
	if err := s.checkUse(ctx, state, state.Profiles...); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(destination), ".cooper-backup-")
	if err != nil {
		return err
	}
	if err := s.writeBackup(ctx, store, state, stage); err != nil {
		return fmt.Errorf("backup failed; partial data is retained at %s: %w", stage, err)
	}
	parent, err := os.OpenRoot(filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer parent.Close()
	if err := renameEntry(parent, filepath.Base(stage), filepath.Base(destination)); err != nil {
		return fmt.Errorf("backup was retained at %s: %w", stage, err)
	}
	return syncDirectory(filepath.Dir(destination))
}

func (s *Service) checkBackupPath(state index, path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return errors.New("backup requires a new absolute directory")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return errors.New("backup destination already exists or cannot be checked")
	}
	resolved, err := workload.ResolvedPath(path)
	if err != nil {
		return err
	}
	if resolved != path {
		return errors.New("backup destination must use its resolved parent path")
	}
	if containsPath(s.options.CooperDir, path) || containsPath(path, s.options.CooperDir) {
		return errors.New("backup must be outside the Cooper directory")
	}
	for _, profile := range state.Profiles {
		if err := s.checkProfile(profile); err != nil {
			return err
		}
		for _, root := range profile.Roots {
			for _, source := range []string{root.HostPath, sibling(root, profile.ID)} {
				if containsPath(source, path) || containsPath(path, source) {
					return errors.New("backup destination overlaps profile state")
				}
			}
		}
	}
	return nil
}

func (s *Service) writeBackup(ctx context.Context, store *os.Root, state index, stage string) error {
	output, err := os.OpenRoot(stage)
	if err != nil {
		return err
	}
	defer output.Close()
	backup := backupIndex{Schema: Schema}
	for _, saved := range state.Profiles {
		profile, err := inspectProfile(state, saved)
		if err != nil {
			return err
		}
		credentials, err := s.readCredentials(store, saved)
		if err != nil {
			return err
		}
		if saved.Identity.Key != "" {
			if err := s.checkIdentity(ctx, state, profile, credentials); err != nil {
				return err
			}
		}
		entry := backupProfile{Manifest: profile, Digests: map[string]string{}}
		if err := privatePath(output, filepath.Join("data", profile.ID), true); err != nil {
			return err
		}
		for _, root := range profile.Roots {
			if !root.Present {
				continue
			}
			digest, err := s.checkedCopy(ctx, sourcePath(state, profile, root), filepath.Join(stage, backupData(profile, root)))
			if err != nil {
				return err
			}
			entry.Digests[root.ID] = digest
		}
		if err := recordCredentials(output, &entry.Manifest, credentials); err != nil {
			return err
		}
		backup.Profiles = append(backup.Profiles, entry)
	}
	return writeJSON(output, "backup.json", backup)
}

func (s *Service) checkedCopy(ctx context.Context, source, target string) (string, error) {
	before, err := treeDigest(ctx, source)
	if err != nil {
		return "", err
	}
	if err := s.copy(ctx, source, target); err != nil {
		return "", err
	}
	after, err := treeDigest(ctx, source)
	if err != nil {
		return "", err
	}
	copied, err := treeDigest(ctx, target)
	if err != nil {
		return "", err
	}
	if before != after || before != copied {
		return "", errors.New("profile state changed during the copy; no backup or restore was committed")
	}
	return copied, nil
}
