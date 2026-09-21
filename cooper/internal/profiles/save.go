package profiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/rickchristie/govner/cooper/internal/workload"
)

func (s *Service) Save(ctx context.Context, request SaveRequest) (Result, error) {
	store, lock, state, err := s.locked(ctx, true, true)
	if err != nil {
		return Result{}, err
	}
	defer store.Close()
	defer lock.Close()
	paths, roots, err := s.hostScope(request.Harness)
	if err != nil {
		return Result{}, err
	}
	var names []string
	for _, root := range roots {
		if err := checkRootMounts(root.HostPath); err != nil {
			return Result{}, err
		}
		names = append(names, root.HostPath)
	}
	if err := s.options.Guard.Check(ctx, names); err != nil {
		return Result{}, err
	}
	identity, err := s.options.Reader.Read(ctx, request.Harness, paths.Mounts, s.options.Environment)
	if err != nil || identity.Key == "" {
		return Result{}, &Issue{Kind: IdentityUnknown, Message: "account identity could not be verified; sign in with a supported login before saving"}
	}
	current := state.byID(state.Hosts[request.Harness].ProfileID)
	created := current == nil
	var profile Manifest
	if created {
		if err := s.checkRootOwnership(state, request.Harness, roots); err != nil {
			return Result{}, err
		}
		// Keep an authoritative empty index before creating credential records.
		// An interrupted first save must not leave an ambiguous old-format store.
		if err := s.publish(store, state); err != nil {
			return Result{}, err
		}
		profile, err = s.newProfile(request.Harness, "Default", paths, roots)
		if err != nil {
			return Result{}, err
		}
		if err := prepareProfileRoots(&profile, true); err != nil {
			return Result{}, err
		}
	} else {
		if err := s.checkCurrentPaths(*current, paths, roots); err != nil {
			return Result{}, err
		}
		profile, err = inspectProfile(state, *current)
		if err != nil {
			return Result{}, err
		}
		if profile.Identity.Key != "" && profile.Identity.Key != identity.Key {
			return Result{}, &Issue{Kind: AccountConflict, Message: "the login differs from the selected profile; restore its original login or an independent backup"}
		}
	}
	if other := state.byIdentity(request.Harness, identity.Key); other != nil && other.ID != profile.ID {
		return Result{}, &Issue{Kind: AccountConflict, Message: fmt.Sprintf("this account already belongs to %s", other.Name)}
	}
	profile.Identity, profile.Saved = identity, s.now().UTC()
	if err := recordCredentials(store, &profile, s.credentials(request.Harness)); err != nil {
		return Result{}, err
	}
	state.put(profile)
	state.Hosts[request.Harness] = HostSelection{ProfileID: profile.ID}
	return Result{Saved: profile.Name, Created: created}, s.publish(store, state)
}

func (s *Service) newProfile(harness, name string, paths workload.AgentPaths, roots []Root) (Manifest, error) {
	id, err := newID()
	if err != nil {
		return Manifest{}, err
	}
	policy, err := workload.AgentStatePolicy(harness)
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{Schema: Schema, ID: id, Harness: harness, Name: name, Account: s.options.Account,
		Policy: policy, Created: s.now().UTC(), Saved: s.now().UTC(), Roots: append([]Root(nil), roots...),
		Environment: paths.Environment, PathEnvironment: pathValues(paths)}, nil
}

// Only create missing directory roots. Registration preserves existing
// inodes, contents, permissions, and extended attributes without a tree scan.
func prepareProfileRoots(profile *Manifest, active bool) error {
	for position, root := range profile.Roots {
		if err := os.MkdirAll(filepath.Dir(root.HostPath), 0700); err != nil {
			return err
		}
		parent, err := os.OpenRoot(filepath.Dir(root.HostPath))
		if err != nil {
			return err
		}
		info, err := parent.Stat(".")
		parent.Close()
		if err != nil {
			return err
		}
		root.Parent, root.Entry, root.Present = fileID(info), FileID{}, false
		path := root.HostPath
		if !active {
			path = sibling(root, profile.ID)
		}
		if root.Kind == workload.Directory {
			err := os.Mkdir(path, 0700)
			if err != nil && !(active && errors.Is(err, os.ErrExist)) {
				return err
			}
			if err == nil {
				if err := syncDirectory(filepath.Dir(path)); err != nil {
					return err
				}
			}
		} else if !active {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				return errors.New("inactive profile file already exists or cannot be checked")
			}
		}
		root, err = inspectRoot(root, path)
		if err != nil {
			return err
		}
		profile.Roots[position] = root
	}
	return nil
}

func (s *Service) checkCurrentPaths(profile Manifest, paths workload.AgentPaths, roots []Root) error {
	if err := s.checkProfile(profile); err != nil {
		return err
	}
	if len(roots) != len(profile.Roots) || !reflect.DeepEqual(paths.Environment, profile.Environment) {
		return errors.New("use the profile's original path settings before changing it")
	}
	for position, root := range roots {
		old := profile.Roots[position]
		if root.ID != old.ID || root.HostPath != old.HostPath || root.Target != old.Target || root.Kind != old.Kind {
			return errors.New("profile root paths changed")
		}
	}
	return nil
}

// Global skills are excluded from the profile catalog. Other roots have one
// harness owner so one account cannot rename another harness's live state.
func (s *Service) checkRootOwnership(state index, harness string, roots []Root) error {
	global, err := workload.ResolvedPath(filepath.Join(s.options.Account.Home, ".agents"))
	if err != nil {
		return err
	}
	for _, root := range roots {
		if containsPath(root.HostPath, global) || containsPath(global, root.HostPath) {
			return errors.New("profile state overlaps global .agents; use a separate state directory")
		}
		for _, profile := range state.Profiles {
			if profile.Harness == harness {
				continue
			}
			for _, other := range profile.Roots {
				for _, path := range []string{other.HostPath, sibling(other, profile.ID)} {
					if containsPath(path, root.HostPath) || containsPath(root.HostPath, path) {
						return errors.New("profile root overlaps another harness's state")
					}
				}
			}
		}
	}
	return nil
}
