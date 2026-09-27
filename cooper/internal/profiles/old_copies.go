package profiles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/rickchristie/govner/cooper/internal/statelock"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

const copyBackupPrefix = "profiles-copy-backup-"

// These fields identify the old copy format. They never authorize a mount or
// an account switch. Keep the complete store together for manual recovery.
type oldCopyIndex struct {
	Schema          int                    `json:"schema"`
	Profiles        []oldCopyProfile       `json:"profiles"`
	Hosts           map[string]oldCopyHost `json:"hosts"`
	LastTransaction string                 `json:"last_transaction,omitempty"`
}

type oldCopyProfile struct {
	Manifest
	Generation         string `json:"generation"`
	PreviousGeneration string `json:"previous_generation,omitempty"`
	Digest             string `json:"digest"`
}

type oldCopyHost struct {
	HostSelection
	BaseDigest string `json:"base_digest"`
	RecoveryID string `json:"recovery_id,omitempty"`
}

// RetireOldCopies removes obsolete copy registrations from use. It moves only
// their store, never a live agent root. Call it on the physical host before
// taking a shared runtime lock. Build must not call it.
func (s *Service) RetireOldCopies(ctx context.Context) (string, error) {
	needed, err := s.hasOldCopies()
	if err != nil || !needed {
		return "", err
	}
	lock, err := statelock.Acquire(ctx, true)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	// Another command can finish the reset while this command waits. Read
	// again through the same directory handle that the rename will check.
	store, err := s.open(false)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer store.Close()
	old, err := readOldCopies(store)
	if err != nil || old == nil {
		return "", err
	}
	if err := s.checkOldCopyStore(store, *old); err != nil {
		return "", err
	}
	parent, err := os.OpenRoot(s.options.CooperDir)
	if err != nil {
		return "", err
	}
	defer parent.Close()
	expected, err := store.Stat(".")
	if err != nil {
		return "", err
	}
	actual, err := parent.Lstat("profiles")
	if err != nil || !os.SameFile(expected, actual) {
		return "", errors.New("profile store changed before reset; all profile data was retained")
	}
	id, err := newID()
	if err != nil {
		return "", err
	}
	name := copyBackupPrefix + id
	if err := s.rename(parent, "profiles", name); err != nil {
		return "", err
	}
	path := filepath.Join(s.options.CooperDir, name)
	if err := syncRoot(parent, "."); err != nil {
		return "", fmt.Errorf("old copy store is retained at %s, but the directory sync failed: %w", path, err)
	}
	// An absent store is already a valid empty selection. No second write is
	// needed, so a crash cannot leave a partly written replacement index.
	return path, nil
}

func (s *Service) checkOldCopyStore(store *os.Root, old oldCopyIndex) error {
	if err := ready(store); err != nil {
		return err
	}
	entries, err := fs.ReadDir(store.FS(), ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		switch entry.Name() {
		case "index.json", "harnesses", "recovery":
		default:
			return errors.New("old copy store contains unknown or symlink-format data; all profile data was retained")
		}
	}
	if err := workload.ValidateAllHostAgentStateRoots(s.options.Account.Home, s.options.CooperDir); err != nil {
		return err
	}
	for _, profile := range old.Profiles {
		if profile.Schema != 1 || !storedID.MatchString(profile.ID) || !storedID.MatchString(profile.Generation) || len(profile.Roots) == 0 {
			return errors.New("old copy profile has an incomplete storage identity; all profile data was retained")
		}
		for _, root := range profile.Roots {
			for _, path := range []string{root.HostPath, root.Target} {
				if !filepath.IsAbs(path) || filepath.Clean(path) != path {
					return errors.New("old copy profile has an invalid host path; all profile data was retained")
				}
				if err := workload.ValidateHostOwnedPath(path, s.options.CooperDir); err != nil {
					return err
				}
			}
		}
	}
	storePath, err := workload.ResolvedPath(s.storePath())
	if err != nil {
		return err
	}
	if err := checkRootMounts(storePath); err != nil {
		return err
	}
	return nil
}

func (s *Service) hasOldCopies() (bool, error) {
	store, err := s.open(false)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer store.Close()
	old, err := readOldCopies(store)
	return old != nil, err
}

// A nil result means that this store is not the old copy format.
func readOldCopies(store *os.Root) (*oldCopyIndex, error) {
	var data json.RawMessage
	if err := readJSON(store, "index.json", &data); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var header struct {
		Schema int `json:"schema"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, errors.New("profile index has an invalid schema")
	}
	if header.Schema != 1 {
		return nil, nil
	}
	var old oldCopyIndex
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&old); err != nil || old.Hosts == nil {
		return nil, errors.New("old copy profile index is incomplete or unsupported; all profile data was retained")
	}
	return &old, nil
}
