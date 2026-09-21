package profiles

import (
	"context"
	"errors"
	"fmt"
	"os"
)

func (s *Service) Load(ctx context.Context, request LoadRequest) (Result, error) {
	if err := Supported(); err != nil {
		return Result{}, err
	}
	if err := ValidateName(request.Name); err != nil {
		return Result{}, err
	}
	store, lock, state, err := s.locked(ctx, false, true)
	if err != nil {
		return Result{}, err
	}
	defer store.Close()
	defer lock.Close()
	current := state.byID(state.Hosts[request.Harness].ProfileID)
	if current == nil {
		return Result{}, errors.New("save this harness once before loading another profile")
	}
	paths, roots, err := s.hostScope(request.Harness)
	if err != nil {
		return Result{}, err
	}
	if err := s.checkCurrentPaths(*current, paths, roots); err != nil {
		return Result{}, err
	}
	outgoing, err := inspectProfile(state, *current)
	if err != nil {
		return Result{}, err
	}
	outgoingCredentials, err := s.readCredentials(store, outgoing)
	if err != nil {
		return Result{}, err
	}
	if outgoing.Identity.Key != "" {
		// The shell must already match the incoming profile. Use the saved
		// outgoing environment to check its files, never the incoming login.
		if err := s.checkIdentity(ctx, state, outgoing, outgoingCredentials); err != nil {
			return Result{}, err
		}
	}
	target := state.byName(request.Harness, request.Name)
	var incoming Manifest
	users := []Manifest{outgoing}
	credentials := emptyCredentials(s.credentials(request.Harness))
	if target != nil {
		if err := s.checkCurrentPaths(*target, paths, roots); err != nil {
			return Result{}, err
		}
		incoming, err = inspectProfile(state, *target)
		if err != nil {
			return Result{}, err
		}
		credentials, err = s.readCredentials(store, incoming)
		if err != nil {
			return Result{}, err
		}
		if incoming.Identity.Key != "" {
			if err := s.checkIdentity(ctx, state, incoming, credentials); err != nil {
				return Result{}, err
			}
		}
		users = append(users, incoming)
	}
	if err := compatibleCredentials(s.credentials(request.Harness), credentials); err != nil {
		return Result{}, err
	}
	if err := s.checkUse(ctx, state, users...); err != nil {
		return Result{}, err
	}
	if target != nil && target.ID == outgoing.ID {
		return Result{Loaded: target.Name, Pending: target.Identity.Key == "", Unchanged: true}, nil
	}
	if !request.Confirmed || (request.ExpectedProfileID != "" && request.ExpectedProfileID != outgoing.ID) {
		return Result{}, &Issue{Kind: ConfirmationRequired, ProfileID: outgoing.ID,
			Message: fmt.Sprintf("Close all %s CLI instances and apps that use the affected state. Stop affected Cooper runtimes. Keep them closed until the switch completes. Switch %s to %s?", request.Harness, outgoing.Name, request.Name)}
	}
	created := target == nil
	if created {
		incoming, err = s.newProfile(request.Harness, request.Name, paths, roots)
		if err != nil {
			return Result{}, err
		}
		if err := prepareProfileRoots(&incoming, false); err != nil {
			return Result{}, err
		}
		if err := recordCredentials(store, &incoming, credentials); err != nil {
			return Result{}, err
		}
	}
	// A pending account can be bound when load saves the outgoing state. An
	// unreadable login stays pending, with every byte retained in its profile.
	if outgoing.Identity.Key == "" {
		identity, readErr := s.options.Reader.Read(ctx, request.Harness, identityMounts(state, outgoing), s.profileEnvironment(state, outgoing, outgoingCredentials))
		if readErr == nil && identity.Key != "" {
			if other := state.byIdentity(request.Harness, identity.Key); other != nil && other.ID != outgoing.ID {
				return Result{}, &Issue{Kind: AccountConflict, Message: "the pending login belongs to another profile"}
			}
			outgoing.Identity = identity
		}
	}
	outgoing.Saved = s.now().UTC()
	state.put(outgoing)
	state.put(incoming)
	state.Hosts[request.Harness] = HostSelection{ProfileID: outgoing.ID, Pending: outgoing.Identity.Key == ""}
	// Register fresh inactive roots before a switch. An interruption here
	// leaves an ordinary pending profile, never an unnamed replacement tree.
	if err := s.publish(store, state); err != nil {
		return Result{}, err
	}
	txn, err := newTransaction("switch", state)
	if err != nil {
		return Result{}, err
	}
	txn.After.Hosts[request.Harness] = HostSelection{ProfileID: incoming.ID, Pending: incoming.Identity.Key == ""}
	txn.planSwitch(outgoing, incoming)
	if err := s.commit(ctx, store, txn); err != nil {
		return Result{}, err
	}
	return Result{Saved: outgoing.Name, Loaded: incoming.Name, Created: created, Pending: incoming.Identity.Key == ""}, nil
}

func ready(store *os.Root) error {
	if _, err := store.Lstat(transactionFile); err == nil {
		return errors.New("a profile operation needs recovery; stop affected apps and run cooper profiles recover")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
