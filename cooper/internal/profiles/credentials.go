package profiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

func emptyCredentials(values []workload.EnvVar) []workload.EnvVar {
	result := make([]workload.EnvVar, 0, len(values))
	for _, value := range values {
		result = append(result, workload.EnvVar{Name: value.Name, Secret: true, Unset: true})
	}
	return result
}

func compatibleCredentials(current, incoming []workload.EnvVar) error {
	if len(current) != len(incoming) {
		return errors.New("profile credential rules changed")
	}
	for position, value := range current {
		next := incoming[position]
		if value.Name != next.Name {
			return errors.New("profile credential rules changed")
		}
		if value.Unset == next.Unset && value.Value == next.Value {
			continue
		}
		if next.Unset {
			return fmt.Errorf("unset %s in your shell before loading; it would override the profile login", value.Name)
		}
		return fmt.Errorf("set %s to this profile's saved value before loading, or use a named Cooper session; Cooper cannot change its parent shell", value.Name)
	}
	return nil
}

func (s *Service) readCredentials(store *os.Root, profile Manifest) ([]workload.EnvVar, error) {
	if !storedID.MatchString(profile.CredentialRevision) {
		return nil, errors.New("profile has no valid credential record")
	}
	if err := privatePath(store, "credentials", false); err != nil {
		return nil, err
	}
	var values []workload.EnvVar
	if err := readJSON(store, filepath.Join("credentials", profile.CredentialRevision+".json"), &values); err != nil {
		return nil, err
	}
	expected := s.credentials(profile.Harness)
	if len(values) != len(expected) {
		return nil, errors.New("profile credential rules changed")
	}
	for position, value := range values {
		if value.Name != expected[position].Name || !value.Secret || (value.Unset && value.Value != "") {
			return nil, errors.New("profile credential record has invalid variables")
		}
	}
	return values, nil
}

func recordCredentials(store *os.Root, profile *Manifest, values []workload.EnvVar) error {
	if err := privatePath(store, "credentials", true); err != nil {
		return err
	}
	if profile.CredentialRevision != "" {
		var previous []workload.EnvVar
		err := readJSON(store, filepath.Join("credentials", profile.CredentialRevision+".json"), &previous)
		if err == nil && reflect.DeepEqual(previous, values) {
			return nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	id, err := newID()
	if err != nil {
		return err
	}
	if err := writeJSON(store, filepath.Join("credentials", id+".json"), values); err != nil {
		return err
	}
	profile.CredentialRevision = id
	return nil
}

func pathValues(paths workload.AgentPaths) map[string]string {
	values := map[string]string{}
	for _, value := range paths.Environment {
		if !value.Unset && value.Name != "GROK_LEADER_SOCKET" {
			values[value.Name] = value.Value
		}
	}
	return values
}

func identityEnvironment(profile Manifest, credentials []workload.EnvVar) map[string]string {
	env := make(map[string]string, len(profile.PathEnvironment)+len(credentials))
	for name, value := range profile.PathEnvironment {
		env[name] = value
	}
	for _, value := range credentials {
		if !value.Unset {
			env[value.Name] = value.Value
		}
	}
	return env
}

func identityMounts(state index, profile Manifest) []workload.MountSpec {
	var mounts []workload.MountSpec
	for _, root := range profile.Roots {
		mounts = append(mounts, workload.MountSpec{ID: root.ID, Source: sourcePath(state, profile, root), Target: root.Target, Kind: root.Kind})
	}
	return mounts
}

func (s *Service) checkIdentity(ctx context.Context, state index, profile Manifest, credentials []workload.EnvVar) error {
	identity, err := s.options.Reader.Read(ctx, profile.Harness, identityMounts(state, profile), s.profileEnvironment(state, profile, credentials))
	if err != nil || identity.Key == "" || identity.Key != profile.Identity.Key {
		return &Issue{Kind: AccountConflict, Message: "profile login does not match its account; restore the original login or an independent backup"}
	}
	return nil
}

func (s *Service) profileEnvironment(state index, profile Manifest, credentials []workload.EnvVar) map[string]string {
	env := identityEnvironment(profile, credentials)
	if state.Hosts[profile.Harness].ProfileID == profile.ID {
		// A live desktop keyring can override file auth. This observation is
		// never saved or sent to a runtime as a credential setting.
		if value := s.options.Environment["DBUS_SESSION_BUS_ADDRESS"]; value != "" {
			env["DBUS_SESSION_BUS_ADDRESS"] = value
		}
	}
	return env
}
