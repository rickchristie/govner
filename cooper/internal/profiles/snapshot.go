package profiles

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/rickchristie/govner/cooper/internal/workload"
)

func (s *Service) credentials(harness string) []workload.EnvVar {
	names := append([]string(nil), s.options.CredentialNames(harness)...)
	sort.Strings(names)
	values := make([]workload.EnvVar, 0, len(names))
	for _, name := range names {
		value, present := s.options.Environment[name]
		values = append(values, workload.EnvVar{Name: name, Value: value, Secret: true, Unset: !present})
	}
	return values
}

func (s *Service) currentCredentialsFor(scope []workload.EnvVar) []workload.EnvVar {
	values := make([]workload.EnvVar, 0, len(scope))
	for _, variable := range scope {
		value, present := s.options.Environment[variable.Name]
		values = append(values, workload.EnvVar{Name: variable.Name, Value: value, Secret: true, Unset: !present})
	}
	return values
}

func digestRoots(ctx context.Context, roots []Root, source func(Root) string, credentials []workload.EnvVar) (string, error) {
	hash := sha256.New()
	encoded, err := json.Marshal(credentials)
	if err != nil {
		return "", err
	}
	hash.Write(encoded)
	for _, root := range roots {
		fmt.Fprintf(hash, "\x00%s\x00%s\x00%s\x00", root.ID, root.Target, root.Kind)
		path := source(root)
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			hash.Write([]byte("absent"))
			continue
		}
		if err != nil {
			return "", err
		}
		if (root.Kind == workload.Directory && !info.IsDir()) || (root.Kind == workload.File && !info.Mode().IsRegular()) {
			return "", fmt.Errorf("state root %s changed its file type", root.Target)
		}
		digest, err := treeDigest(ctx, path)
		if err != nil {
			return "", err
		}
		hash.Write([]byte(digest))
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func hostSource(root Root) string { return root.HostPath }

func snapshotSource(base string) func(Root) string {
	return func(root Root) string { return filepath.Join(base, "roots", root.ID) }
}

func (s *Service) profileDigest(ctx context.Context, store *os.Root, profile Manifest) (string, error) {
	path := dataPath(profile)
	if err := privatePath(store, filepath.Join(path, "roots"), false); err != nil {
		return "", err
	}
	var credentials []workload.EnvVar
	if err := readJSON(store, filepath.Join(path, "credentials.json"), &credentials); err != nil {
		return "", err
	}
	return digestRoots(ctx, profile.Roots, snapshotSource(filepath.Join(s.storePath(), path)), credentials)
}

// capture verifies both ends of a copy. Source changes or different account
// information in the copied view prevent publication, even if the metadata
// lookup that selected the destination originally succeeded.
func (s *Service) capture(ctx context.Context, store *os.Root, path, harness string, paths workload.AgentPaths, roots []Root) (Manifest, error) {
	credentials := s.credentials(harness)
	before, err := digestRoots(ctx, roots, hostSource, credentials)
	if err != nil {
		return Manifest{}, err
	}
	if err := privatePath(store, filepath.Join(path, "roots"), true); err != nil {
		return Manifest{}, err
	}
	complete := false
	defer func() {
		if !complete {
			_ = removeTree(store, path)
		}
	}()
	base := filepath.Join(s.storePath(), path)
	for index := range roots {
		root := &roots[index]
		_, err := os.Stat(root.HostPath)
		root.Present = err == nil
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return Manifest{}, err
		}
		if err := s.copy(ctx, root.HostPath, filepath.Join(base, "roots", root.ID)); err != nil {
			return Manifest{}, fmt.Errorf("copy state root %s: %w", root.Target, err)
		}
	}
	after, err := digestRoots(ctx, roots, hostSource, credentials)
	if err != nil {
		return Manifest{}, err
	}
	copied, err := digestRoots(ctx, roots, snapshotSource(base), credentials)
	if err != nil {
		return Manifest{}, err
	}
	if before != after || before != copied {
		return Manifest{}, errors.New("agent state changed during the copy; close the harness and retry")
	}
	if err := writeJSON(store, filepath.Join(path, "credentials.json"), credentials); err != nil {
		return Manifest{}, err
	}
	policy, err := workload.AgentStatePolicy(harness)
	if err != nil {
		return Manifest{}, err
	}
	complete = true
	return Manifest{Schema: Schema, Harness: harness, Account: s.options.Account, Policy: policy,
		Roots: roots, PathEnvironment: pathValues(paths), Environment: paths.Environment, Digest: copied}, nil
}

func pathValues(paths workload.AgentPaths) map[string]string {
	values := map[string]string{}
	for _, variable := range paths.Environment {
		if !variable.Unset && variable.Name != "GROK_LEADER_SOCKET" {
			values[variable.Name] = variable.Value
		}
	}
	return values
}

func snapshotMounts(profile Manifest, base string) []workload.MountSpec {
	var mounts []workload.MountSpec
	for _, root := range profile.Roots {
		mounts = append(mounts, workload.MountSpec{ID: root.ID, Source: filepath.Join(base, "roots", root.ID), Target: root.Target, Kind: root.Kind})
	}
	return mounts
}

func (s *Service) preserve(ctx context.Context, store *os.Root, harness string, paths workload.AgentPaths, roots []Root, reason string) (string, error) {
	id, err := newID()
	if err != nil {
		return "", err
	}
	path := filepath.Join("recovery", id)
	snapshot, err := s.capture(ctx, store, path, harness, paths, roots)
	if err != nil {
		return "", err
	}
	if err := writeJSON(store, filepath.Join(path, "snapshot.json"), struct {
		Reason   string   `json:"reason"`
		Snapshot Manifest `json:"snapshot"`
	}{reason, snapshot}); err != nil {
		return "", err
	}
	return filepath.Join(s.storePath(), path), nil
}
