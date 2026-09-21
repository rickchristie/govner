package profiles

import (
	"errors"
	"maps"
	"path/filepath"
	"reflect"
)

func (txn *transaction) planSwitch(outgoing, incoming Manifest) {
	for position, root := range outgoing.Roots {
		txn.Entries = append(txn.Entries, rootEntry{Root: root, Name: filepath.Base(root.HostPath), Entry: root.Entry}, rootEntry{Root: root, Name: filepath.Base(sibling(root, outgoing.ID))})
		if root.Present {
			txn.Moves = append(txn.Moves, rootMove{Root: root, From: filepath.Base(root.HostPath), To: filepath.Base(sibling(root, outgoing.ID)), Entry: root.Entry})
		}
		next := incoming.Roots[position]
		txn.Entries = append(txn.Entries, rootEntry{Root: next, Name: filepath.Base(sibling(next, incoming.ID)), Entry: next.Entry})
		if next.Present {
			txn.Moves = append(txn.Moves, rootMove{Root: next, From: filepath.Base(sibling(next, incoming.ID)), To: filepath.Base(next.HostPath), Entry: next.Entry})
		}
	}
}

func (txn *transaction) planRestoreRoot(profile Manifest, root, next Root) {
	path := sourcePath(txn.Before, profile, root)
	stage, recovery := root.HostPath+".cooper-restore-"+txn.ID, root.HostPath+".cooper-recovery-"+txn.ID
	txn.Entries = append(txn.Entries, rootEntry{Root: root, Name: filepath.Base(path), Entry: root.Entry}, rootEntry{Root: root, Name: filepath.Base(stage), Entry: next.Entry}, rootEntry{Root: root, Name: filepath.Base(recovery)})
	if root.Present {
		txn.Moves = append(txn.Moves, rootMove{Root: root, From: filepath.Base(path), To: filepath.Base(recovery), Entry: root.Entry})
	}
	if next.Present {
		txn.Moves = append(txn.Moves, rootMove{Root: root, From: filepath.Base(stage), To: filepath.Base(path), Entry: next.Entry})
	}
}

func (txn *transaction) planDelete(profile Manifest) {
	for _, root := range profile.Roots {
		txn.Entries = append(txn.Entries, rootEntry{Root: root, Name: filepath.Base(sibling(root, profile.ID)), Entry: root.Entry})
	}
}

// Do not execute a free-form list of journal instructions. Rebuild the only
// valid plan from its index change and require an exact match before recovery.
func validateChange(txn transaction) error {
	expected := txn
	expected.Entries, expected.Moves = nil, nil
	var err error
	switch txn.Operation {
	case "switch":
		err = expected.checkSwitchChange()
	case "restore":
		err = expected.checkRestoreChange()
	case "delete":
		err = expected.checkDeleteChange()
	}
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected.Entries, txn.Entries) || !reflect.DeepEqual(expected.Moves, txn.Moves) {
		return errors.New("profile recovery plan does not match its index change")
	}
	return nil
}

func (txn *transaction) checkSwitchChange() error {
	if !reflect.DeepEqual(txn.Before.Profiles, txn.After.Profiles) || len(txn.Before.Hosts) != len(txn.After.Hosts) {
		return errors.New("switch journal changed profile data")
	}
	changes := 0
	for harness, selected := range txn.Before.Hosts {
		next, ok := txn.After.Hosts[harness]
		if !ok {
			return errors.New("switch journal removed a host selection")
		}
		if selected == next {
			continue
		}
		changes++
		outgoing, incoming := txn.Before.byID(selected.ProfileID), txn.After.byID(next.ProfileID)
		if outgoing == nil || incoming == nil || outgoing.Harness != incoming.Harness {
			return errors.New("switch journal has an invalid account selection")
		}
		txn.planSwitch(*outgoing, *incoming)
	}
	if changes != 1 {
		return errors.New("switch journal must change one harness")
	}
	return nil
}

func (txn *transaction) checkRestoreChange() error {
	if !maps.Equal(txn.Before.Hosts, txn.After.Hosts) || len(txn.Before.Profiles) != len(txn.After.Profiles) {
		return errors.New("restore journal changed profile selection")
	}
	changes := 0
	for position, before := range txn.Before.Profiles {
		after := txn.After.Profiles[position]
		if reflect.DeepEqual(before, after) {
			continue
		}
		changes++
		if before.Identity.Key != after.Identity.Key || len(before.Roots) != len(after.Roots) {
			return errors.New("restore journal changed account identity or root count")
		}
		for rootPosition, root := range before.Roots {
			next := after.Roots[rootPosition]
			shape := next
			shape.Entry, shape.Present = root.Entry, root.Present
			if shape != root {
				return errors.New("restore journal changed a root path")
			}
			txn.planRestoreRoot(before, root, next)
		}
		after.Roots, after.Saved, after.Identity, after.CredentialRevision = before.Roots, before.Saved, before.Identity, before.CredentialRevision
		if !reflect.DeepEqual(before, after) {
			return errors.New("restore journal changed profile metadata")
		}
	}
	if changes != 1 {
		return errors.New("restore journal must change one profile")
	}
	return nil
}

func (txn *transaction) checkDeleteChange() error {
	if !maps.Equal(txn.Before.Hosts, txn.After.Hosts) || len(txn.Before.Profiles) != len(txn.After.Profiles)+1 {
		return errors.New("deletion journal changed profile selection")
	}
	changes := 0
	for _, before := range txn.Before.Profiles {
		after := txn.After.byID(before.ID)
		if after != nil {
			if !reflect.DeepEqual(before, *after) {
				return errors.New("deletion journal changed another profile")
			}
			continue
		}
		changes++
		if txn.Before.Hosts[before.Harness].ProfileID == before.ID {
			return errors.New("deletion journal names a selected profile")
		}
		txn.planDelete(before)
	}
	if changes != 1 {
		return errors.New("deletion journal must remove one inactive profile")
	}
	return nil
}
