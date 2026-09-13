package profiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rickchristie/govner/cooper/internal/workload"
)

func (s *Service) save(ctx context.Context, store *os.Root, state *index, request SaveRequest, paths workload.AgentPaths, roots []Root) (Result, error) {
	if err := s.options.Guard.Check(ctx, rootPaths(roots)); err != nil {
		return Result{}, err
	}
	identity, err := s.options.Reader.Read(ctx, request.Harness, paths.Mounts, s.options.Environment)
	if err != nil || identity.Key == "" {
		recovery, copyErr := s.preserve(ctx, store, request.Harness, paths, roots, "account identity could not be verified")
		if copyErr != nil {
			return Result{}, copyErr
		}
		return Result{}, &Issue{Kind: IdentityUnknown, Message: "account identity could not be verified; sign in with a supported file-based login before saving", Recovery: recovery}
	}
	profile, created, err := s.saveDestination(state, request, identity)
	if err != nil {
		return Result{}, err
	}
	var recovery string
	if profile.Generation != "" {
		if err := s.options.Guard.Check(ctx, []string{filepath.Join(s.storePath(), "harnesses", profile.Harness, profile.ID)}); err != nil {
			return Result{}, err
		}
	}
	if !created {
		if err := s.checkProfilePaths(profile); err != nil {
			return Result{}, err
		}
		credentials, err := s.readStoredCredentials(store, profile)
		if err != nil {
			return Result{}, err
		}
		if err := s.checkSavedIdentity(ctx, profile, credentials); err != nil {
			return Result{}, err
		}
		policy, err := workload.AgentStatePolicy(profile.Harness)
		if err != nil {
			return Result{}, err
		}
		rootRefresh := policy != profile.Policy
		refresh := rootRefresh || len(credentials) != len(s.credentials(profile.Harness))
		if !rootRefresh {
			if err := s.checkProfile(profile); err != nil {
				return Result{}, err
			}
		}
		compareRoots := roots
		if rootRefresh {
			compareRoots = profile.Roots
		}
		comparison := s.currentCredentialsFor(credentials)
		unchanged, preserved, err := s.checkSaveConflict(ctx, store, state, profile, paths, compareRoots, comparison, request.ConflictChoice)
		recovery = preserved
		if err != nil {
			return Result{}, err
		}
		if unchanged && !refresh {
			return Result{Saved: profile.Name, Unchanged: true, Recovery: recovery}, nil
		}
		if refresh && unchanged {
			hostDigest, err := digestRoots(ctx, compareRoots, hostSource, comparison)
			if err != nil {
				return Result{}, err
			}
			savedDigest, err := s.profileDigest(ctx, store, profile)
			if err != nil {
				return Result{}, err
			}
			if hostDigest != savedDigest && request.ConflictChoice != KeepHost {
				return Result{}, errors.New("saved profile has newer state; use --conflict host only to refresh from the current host and keep the saved generation for recovery")
			}
		}
	}
	generation, err := newID()
	if err != nil {
		return Result{}, err
	}
	previous, older := profile.Generation, profile.PreviousGeneration
	profile.Generation = generation
	path := dataPath(profile)
	snapshot, err := s.capture(ctx, store, path, request.Harness, paths, roots)
	if err != nil {
		return Result{}, err
	}
	// Read the copied credentials through the same adapter. Root aliases or a
	// login that changed during capture cannot bind old data to a new account.
	copiedIdentity, err := s.options.Reader.Read(ctx, request.Harness, snapshotMounts(snapshot, filepath.Join(s.storePath(), path)), s.options.Environment)
	if err != nil || copiedIdentity.Key != identity.Key {
		return Result{}, &Issue{Kind: AccountConflict, Message: "account changed during save; the previous profile is unchanged", Recovery: filepath.Join(s.storePath(), path)}
	}
	snapshot.ID, snapshot.Generation, snapshot.Name = profile.ID, generation, profile.Name
	snapshot.PreviousGeneration = previous
	snapshot.Identity, snapshot.Created, snapshot.Saved = identity, profile.Created, s.now().UTC()
	if err := writeJSON(store, filepath.Join(path, "snapshot.json"), snapshot); err != nil {
		return Result{}, err
	}
	state.put(snapshot)
	host := state.Hosts[request.Harness]
	state.Hosts[request.Harness] = HostSelection{ProfileID: snapshot.ID, BaseDigest: snapshot.Digest, RecoveryID: host.RecoveryID}
	if err := s.publish(store, *state); err != nil {
		return Result{}, err
	}
	result := Result{Saved: snapshot.Name, Created: created, Recovery: recovery}
	if older != "" {
		oldPath := filepath.Join("harnesses", snapshot.Harness, snapshot.ID, older)
		if err := s.removeGeneration(ctx, store, oldPath); err != nil {
			result.Warning = "saved profile; an older recovery copy was retained: " + err.Error()
		}
	}
	return result, nil
}

func (s *Service) removeGeneration(ctx context.Context, store *os.Root, path string) error {
	if err := privatePath(store, path, false); err != nil {
		return err
	}
	if err := s.options.Guard.Check(ctx, []string{filepath.Join(s.storePath(), path)}); err != nil {
		return err
	}
	return removeTree(store, path)
}

func (s *Service) saveDestination(state *index, request SaveRequest, identity Identity) (Manifest, bool, error) {
	mapped := state.byIdentity(request.Harness, identity.Key)
	host := state.Hosts[request.Harness]
	pending := state.byID(host.ProfileID)
	if host.Pending && pending != nil {
		if mapped != nil && mapped.ID != pending.ID {
			return Manifest{}, false, &Issue{Kind: AccountConflict, Message: fmt.Sprintf("this account already belongs to %s; %s is awaiting a different login", mapped.Name, pending.Name)}
		}
		if request.NewName != "" {
			return Manifest{}, false, errors.New("the pending profile already has a name; run 'cooper save' without a new name")
		}
		return *pending, true, nil
	}
	if mapped != nil {
		if request.NewName != "" {
			return Manifest{}, false, errors.New("this account is already mapped; a new name cannot select or replace another profile")
		}
		return *mapped, false, nil
	}
	first := true
	for _, profile := range state.Profiles {
		if profile.Harness == request.Harness {
			first = false
			break
		}
	}
	name := request.NewName
	if first {
		if name != "" {
			return Manifest{}, false, errors.New("the first profile is named Default; run 'cooper save' without a new name")
		}
		name = "Default"
	}
	if name == "" {
		return Manifest{}, false, &Issue{Kind: NameRequired, Message: "this account has no profile; enter a new profile name"}
	}
	if err := ValidateName(name); err != nil {
		return Manifest{}, false, err
	}
	if state.byName(request.Harness, name) != nil {
		return Manifest{}, false, errors.New("that profile name already exists; choose a new unused name")
	}
	id, err := newID()
	if err != nil {
		return Manifest{}, false, err
	}
	return Manifest{ID: id, Name: name, Harness: request.Harness, Created: s.now().UTC()}, true, nil
}

func (s *Service) checkSaveConflict(ctx context.Context, store *os.Root, state *index, profile Manifest, paths workload.AgentPaths, roots []Root, credentials []workload.EnvVar, choice ConflictChoice) (bool, string, error) {
	hostDigest, err := digestRoots(ctx, roots, hostSource, credentials)
	if err != nil {
		return false, "", err
	}
	savedDigest, err := s.profileDigest(ctx, store, profile)
	if err != nil {
		return false, "", err
	}
	host := state.Hosts[profile.Harness]
	if hostDigest == savedDigest {
		state.Hosts[profile.Harness] = HostSelection{ProfileID: profile.ID, BaseDigest: hostDigest, RecoveryID: host.RecoveryID}
		return true, "", s.publish(store, *state)
	}
	if host.ProfileID == profile.ID && savedDigest == host.BaseDigest {
		return false, "", nil // Only the host changed.
	}
	if host.ProfileID == profile.ID && hostDigest == host.BaseDigest {
		return true, "", nil // Preserve changes made directly in the saved profile.
	}
	recovery, err := s.preserve(ctx, store, profile.Harness, paths, roots, "host and saved profile have different changes")
	if err != nil {
		return false, "", err
	}
	if choice == KeepHost {
		return false, recovery, nil
	}
	if choice == KeepSaved {
		return true, recovery, nil
	}
	return false, recovery, &Issue{Kind: StateConflict, Message: fmt.Sprintf("host state and profile %s have different changes; neither version was overwritten; choose --conflict host or --conflict saved to keep one as the saved profile", profile.Name), Recovery: recovery}
}

func validateConflictChoice(choice ConflictChoice) error {
	if choice != "" && choice != KeepHost && choice != KeepSaved {
		return errors.New("conflict choice must be host or saved")
	}
	return nil
}
