package profiles

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/rickchristie/govner/cooper/internal/statelock"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

// Selection is shared by Docker and VM launch. Empty ID selects host state.
// Captured credentials never enter runtime labels or persisted mount plans.
type Selection struct {
	ID, Name    string
	Paths       workload.AgentPaths
	Credentials []workload.EnvVar
}

func (s *Service) Select(ctx context.Context, harness, name string) (Selection, error) {
	if err := Supported(); err != nil {
		return Selection{}, err
	}
	if err := ValidateName(name); err != nil {
		return Selection{}, err
	}
	return s.selectProfile(ctx, harness, name, false)
}

func (s *Service) SelectID(ctx context.Context, harness, id string) (Selection, error) {
	if id != "" {
		if err := Supported(); err != nil {
			return Selection{}, err
		}
		if !storedID.MatchString(id) {
			return Selection{}, errors.New("invalid profile ID")
		}
	}
	return s.selectProfile(ctx, harness, id, true)
}

// Runtime callers hold a shared lock through mount creation and reuse. This
// inner read lock also protects direct service callers. No history is read.
func (s *Service) selectProfile(ctx context.Context, harness, key string, byID bool) (Selection, error) {
	lock, err := statelock.Acquire(ctx, false)
	if err != nil {
		return Selection{}, err
	}
	defer lock.Close()
	if err := CheckReady(s.options.CooperDir); err != nil {
		return Selection{}, err
	}
	if key == "" {
		paths, err := workload.ResolveAgentPaths(harness, s.options.Account.Home, s.options.Workspace, s.options.Environment)
		if err != nil {
			return Selection{}, err
		}
		store, err := s.open(false)
		if errors.Is(err, os.ErrNotExist) {
			return Selection{Paths: paths}, nil
		}
		if err := Supported(); err != nil {
			return Selection{Paths: paths}, nil
		}
		if err != nil {
			return Selection{}, err
		}
		defer store.Close()
		state, err := readIndex(store)
		if err != nil {
			return Selection{}, err
		}
		if profile := state.byID(state.Hosts[harness].ProfileID); profile != nil {
			if err := s.checkProfile(*profile); err != nil {
				return Selection{}, err
			}
			if _, err := inspectProfile(state, *profile); err != nil {
				return Selection{}, err
			}
			if profile.Identity.Key != "" {
				identity, err := s.options.Reader.Read(ctx, harness, identityMounts(state, *profile), s.options.Environment)
				if err != nil || identity.Key != profile.Identity.Key {
					return Selection{}, &Issue{Kind: AccountConflict, Message: "host login does not match the selected profile; restore the original login or an independent backup"}
				}
			}
		}
		return Selection{Paths: paths}, nil
	}
	store, err := s.open(false)
	if err != nil {
		return Selection{}, err
	}
	defer store.Close()
	state, err := readIndex(store)
	if err != nil {
		return Selection{}, err
	}
	profile := state.byName(harness, key)
	if byID {
		profile = state.byID(key)
	}
	if profile == nil || profile.Harness != harness {
		return Selection{}, fmt.Errorf("profile %s/%s does not exist", harness, key)
	}
	if err := s.checkProfile(*profile); err != nil {
		return Selection{}, err
	}
	checked, err := inspectProfile(state, *profile)
	if err != nil {
		return Selection{}, err
	}
	if profile.Identity.Key == "" {
		return Selection{}, errors.New("profile is awaiting login; sign in on the host and save before starting a named session")
	}
	credentials, err := s.readCredentials(store, *profile)
	if err != nil {
		return Selection{}, err
	}
	if err := s.checkIdentity(ctx, state, checked, credentials); err != nil {
		return Selection{}, err
	}
	selection := Selection{ID: profile.ID, Name: profile.Name, Credentials: credentials, Paths: workload.AgentPaths{Environment: profile.Environment}}
	for _, root := range checked.Roots {
		if !root.Present {
			continue
		}
		mount := workload.MountSpec{ID: root.ID, Source: sourcePath(state, checked, root), Target: root.Target, Access: workload.ReadWrite, Kind: root.Kind, Ownership: workload.ProfileState}
		if err := workload.ValidateProfileSource(mount, s.options.CooperDir); err != nil {
			return Selection{}, err
		}
		selection.Paths.Mounts = append(selection.Paths.Mounts, mount)
	}
	shared, err := workload.ResolveAgentPaths(harness, profile.Account.Home, s.options.Workspace, profile.PathEnvironment)
	if err != nil {
		return Selection{}, err
	}
	for _, mount := range shared.Mounts {
		if mount.ID == "shared-agents" {
			selection.Paths.Mounts = append(selection.Paths.Mounts, mount)
		}
	}
	return selection, nil
}

// Ordinary launches must stop while a journal exists, even if no named
// profile was requested. Missing stores do not cause profile setup.
func CheckReady(cooperDir string) error {
	parent, err := os.OpenRoot(cooperDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer parent.Close()
	if err := privatePath(parent, "profiles", false); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := Supported(); err != nil {
		return err
	}
	store, err := parent.OpenRoot("profiles")
	if err != nil {
		return err
	}
	defer store.Close()
	if err := ready(store); err != nil {
		return err
	}
	_, err = readIndex(store)
	return err
}

func CanRemoveUnusedStore(cooperDir string) bool {
	store, err := os.OpenRoot(cooperDir)
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	if err != nil {
		return false
	}
	defer store.Close()
	if err := privatePath(store, "profiles", false); errors.Is(err, os.ErrNotExist) {
		return true
	} else if err != nil {
		return false
	}
	root, err := store.OpenRoot("profiles")
	if err != nil {
		return false
	}
	defer root.Close()
	state, err := readIndex(root)
	if err != nil || len(state.Profiles) != 0 {
		return false
	}
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return false
	}
	return len(entries) == 0 || (len(entries) == 1 && entries[0].Name() == "index.json")
}
