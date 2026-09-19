package profiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/rickchristie/govner/cooper/internal/profilelink"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

// Native state can contain an absolute path obtained through realpath. Keep
// every directory location that was exposed as live state for this account.
// Restore and detach retain those names as links, without rewriting databases.
func (s *Service) addCanonicalPaths(profile *Manifest) error {
	for position, root := range profile.Roots {
		if root.Kind != workload.Directory {
			continue
		}
		path, err := filepath.EvalSymlinks(filepath.Join(s.storePath(), dataPath(*profile), "roots", root.ID))
		if err != nil {
			return err
		}
		paths := append(append([]string(nil), root.Aliases...), path)
		slices.Sort(paths)
		profile.Roots[position].Aliases = slices.Compact(paths)
	}
	return nil
}

func preserveCanonicalPaths(next *Manifest, previous Manifest) {
	for position, root := range next.Roots {
		for _, old := range previous.Roots {
			if old.ID == root.ID {
				paths := append(append([]string(nil), root.Aliases...), old.Aliases...)
				slices.Sort(paths)
				next.Roots[position].Aliases = slices.Compact(paths)
			}
		}
	}
}

func canonicalParent(profile Manifest, root Root, path string, create bool) (*os.Root, error) {
	owner, err := profilelink.CanonicalStore(path, profile.Harness, profile.ID, root.ID)
	if err != nil {
		return nil, err
	}
	if err := workload.ValidateCanonicalTarget(path, profile.Account.Home); err != nil {
		return nil, err
	}
	parentPath := filepath.Dir(path)
	resolved, err := workload.ResolvedPath(parentPath)
	if err != nil || resolved != parentPath {
		return nil, errors.New("canonical profile parent changed; state was retained")
	}
	if create {
		if err := os.MkdirAll(owner, 0700); err != nil {
			return nil, err
		}
	}
	store, err := os.OpenRoot(owner)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	relative, _ := filepath.Rel(owner, parentPath)
	if err := privatePath(store, relative, create); err != nil {
		return nil, err
	}
	return store.OpenRoot(relative)
}

func (s *Service) checkCanonicalPaths(state index) error {
	for _, profile := range state.Profiles {
		for _, root := range profile.Roots {
			if len(root.Aliases) == 0 {
				continue
			}
			source, err := filepath.EvalSymlinks(filepath.Join(s.storePath(), dataPath(profile), "roots", root.ID))
			if err != nil {
				return err
			}
			for _, path := range root.Aliases {
				parent, err := canonicalParent(profile, root, path, false)
				if err != nil {
					return err
				}
				info, err := parent.Lstat(filepath.Base(path))
				if err == nil && path != source {
					var target string
					target, err = parent.Readlink(filepath.Base(path))
					if err == nil && target != source {
						err = errors.New("canonical profile alias changed; state was retained")
					}
				} else if err == nil && !info.IsDir() {
					err = errors.New("canonical profile directory changed its type")
				}
				parent.Close()
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *Service) stageCanonicalPaths(ctx context.Context, profile Manifest, txn *managedTransaction, detach bool) error {
	for _, root := range profile.Roots {
		if len(root.Aliases) == 0 {
			continue
		}
		target := root.HostPath
		if !detach {
			var err error
			target, err = filepath.EvalSymlinks(filepath.Join(s.storePath(), dataPath(profile), "roots", root.ID))
			if err != nil {
				return err
			}
		}
		for _, path := range root.Aliases {
			if path == target {
				continue
			}
			parent, err := canonicalParent(profile, root, path, true)
			if err != nil {
				return err
			}
			link, err := parent.Readlink(filepath.Base(path))
			parent.Close()
			if err == nil && link == target {
				continue
			}
			if err == nil && link != root.HostPath && !slices.Contains(root.Aliases, link) {
				return errors.New("canonical path points outside this account; state was retained")
			}
			if err := s.stageManagedChange(ctx, txn, path, "", target); err != nil {
				return err
			}
		}
	}
	return nil
}

// Detach keeps native absolute paths through links to the ordinary host root.
// A runtime also needs those names because it does not mount the host store.
func (s *Service) copyHostCanonicalPaths(harness string, paths workload.AgentPaths) (workload.AgentPaths, error) {
	store, err := s.open(false)
	if errors.Is(err, os.ErrNotExist) {
		return paths, nil
	}
	if err != nil {
		return workload.AgentPaths{}, err
	}
	defer store.Close()
	state, err := readIndex(store)
	if err != nil {
		return workload.AgentPaths{}, err
	}
	profile := state.byID(state.Hosts[harness].ProfileID)
	if profile == nil {
		return paths, nil
	}
	for position, mount := range paths.Mounts {
		for _, root := range profile.Roots {
			if root.ID != mount.ID || root.Target != mount.Target || len(root.Aliases) == 0 {
				continue
			}
			for _, alias := range root.Aliases {
				if err := workload.ValidateCanonicalTarget(alias, s.options.Account.Home); err != nil {
					return workload.AgentPaths{}, err
				}
			}
			paths.Mounts[position].CanonicalPaths = root.Aliases
		}
	}
	return paths, nil
}
