package profiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rickchristie/govner/cooper/internal/profilelink"
	"github.com/rickchristie/govner/cooper/internal/statelock"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

// Selection is the shared Docker/VM launch input. Empty ID means live host
// state. Credentials are kept out of runtime metadata, labels, and digests.
type Selection struct {
	ID, Name    string
	Paths       workload.AgentPaths
	Credentials []workload.EnvVar
}

// Select checks stored metadata and the small identity documents, not session
// content. Callers hold a shared state lock until runtime startup or reuse has
// completed. This inner lock also makes standalone selection reads consistent.
func (s *Service) Select(ctx context.Context, harness, name string) (Selection, error) {
	return s.selectProfile(ctx, harness, name, false)
}

func (s *Service) SelectID(ctx context.Context, harness, id string) (Selection, error) {
	return s.selectProfile(ctx, harness, id, true)
}

func (s *Service) selectProfile(ctx context.Context, harness, key string, byID bool) (Selection, error) {
	if managed, err := s.usesManaged(); err != nil {
		return Selection{}, err
	} else if managed {
		return s.managedSelect(ctx, harness, key, byID)
	}
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
		paths, err = s.copyHostCanonicalPaths(harness, paths)
		return Selection{Paths: paths}, err
	}
	if byID && !storedID.MatchString(key) {
		return Selection{}, errors.New("invalid profile ID")
	}
	if !byID {
		if err := ValidateName(key); err != nil {
			return Selection{}, err
		}
	}
	store, err := s.open(false)
	if err != nil {
		return Selection{}, err
	}
	defer store.Close()
	if _, err := store.Lstat(transactionFile); err == nil {
		return Selection{}, errors.New("a profile load needs recovery; run 'cooper save <harness>' on the host first")
	} else if !errors.Is(err, os.ErrNotExist) {
		return Selection{}, err
	}
	if err := requireCopyMode(store); err != nil {
		return Selection{}, err
	}
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
	if profile.Identity.Key == "" {
		return Selection{}, errors.New("profile is awaiting login; log in on the host and run 'cooper save <harness>' before starting a named session")
	}
	credentials, err := s.readCredentials(store, *profile)
	if err != nil {
		return Selection{}, err
	}
	if err := s.checkSavedIdentity(ctx, *profile, credentials); err != nil {
		return Selection{}, err
	}
	path := filepath.Join(dataPath(*profile), "roots")
	if err := privatePath(store, path, false); err != nil {
		return Selection{}, err
	}
	selection := Selection{ID: profile.ID, Name: profile.Name, Credentials: credentials,
		Paths: workload.AgentPaths{Environment: profile.Environment}}
	for _, root := range profile.Roots {
		source := filepath.Join(s.storePath(), path, root.ID)
		if root.Kind == workload.File {
			if _, err := os.Lstat(source); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return Selection{}, err
			}
		}
		mount := workload.MountSpec{ID: root.ID, Source: source, Target: root.Target, Access: workload.ReadWrite, Kind: root.Kind, Ownership: workload.ProfileState, CanonicalPaths: root.Aliases}
		if err := workload.ValidateProfileSource(mount, s.options.CooperDir); err != nil {
			return Selection{}, err
		}
		selection.Paths.Mounts = append(selection.Paths.Mounts, mount)
	}
	return selection, nil
}

// CheckReady protects ordinary host-state launches after an interrupted load.
// It does not create a store or require any profile to exist.
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
	store, err := parent.OpenRoot("profiles")
	if err != nil {
		return err
	}
	defer store.Close()
	if err := profilelink.Ready(store); err != nil {
		return err
	}
	managed, err := profilelink.Managed(store)
	if err != nil || !managed {
		return err
	}
	view, err := profilelink.Read(store)
	if err != nil {
		return err
	}
	return profilelink.CheckAliases(cooperDir, view)
}

func (s *Service) checkSavedIdentity(ctx context.Context, profile Manifest, credentials []workload.EnvVar) error {
	env := make(map[string]string, len(credentials)+len(profile.PathEnvironment))
	for name, value := range profile.PathEnvironment {
		env[name] = value
	}
	for _, value := range credentials {
		if !value.Unset {
			env[value.Name] = value.Value
		}
	}
	identity, err := s.options.Reader.Read(ctx, profile.Harness, snapshotMounts(profile, filepath.Join(s.storePath(), dataPath(profile))), env)
	if err != nil || identity.Key != profile.Identity.Key {
		return &Issue{Kind: AccountConflict, Message: "saved profile login no longer matches its account mapping; its state was retained"}
	}
	return nil
}
