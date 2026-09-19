package profiles

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"unicode"

	"github.com/rickchristie/govner/cooper/internal/profilelink"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

var profileName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,39}$`)
var storedID = regexp.MustCompile(`^[a-f0-9]{24}$`)
var contentDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

// index is the only authority for profiles and host selections. A save writes
// a new data generation and atomically publishes this small index. It never
// removes a good profile before the replacement copy is complete.
type index struct {
	Schema          int                      `json:"schema"`
	Profiles        []Manifest               `json:"profiles"`
	Hosts           map[string]HostSelection `json:"hosts"`
	LastTransaction string                   `json:"last_transaction,omitempty"`
}

func ValidateName(name string) error {
	if !profileName.MatchString(name) {
		return errors.New("profile name must start with a letter and contain at most 40 letters, digits, underscores, or hyphens")
	}
	return nil
}

func newID() (string, error) {
	data := make([]byte, 12)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func newIndex() index { return index{Schema: Schema, Hosts: make(map[string]HostSelection)} }

func (i *index) byName(harness, name string) *Manifest {
	for pos := range i.Profiles {
		profile := &i.Profiles[pos]
		if profile.Harness == harness && strings.EqualFold(profile.Name, name) {
			return profile
		}
	}
	return nil
}

func (i *index) byID(id string) *Manifest {
	for pos := range i.Profiles {
		if i.Profiles[pos].ID == id {
			return &i.Profiles[pos]
		}
	}
	return nil
}

func (i *index) byIdentity(harness, key string) *Manifest {
	if key == "" {
		return nil
	}
	for pos := range i.Profiles {
		if i.Profiles[pos].Harness == harness && i.Profiles[pos].Identity.Key == key {
			return &i.Profiles[pos]
		}
	}
	return nil
}

func (i *index) put(profile Manifest) {
	if existing := i.byID(profile.ID); existing != nil {
		*existing = profile
		return
	}
	i.Profiles = append(i.Profiles, profile)
}

func readIndex(root *os.Root) (index, error) {
	managed, err := profilelink.Managed(root)
	if err != nil {
		return index{}, err
	}
	if managed {
		view, err := profilelink.Read(root)
		if err != nil {
			return index{}, err
		}
		return viewIndex(view)
	}
	var state index
	err = readJSON(root, "index.json", &state)
	if errors.Is(err, os.ErrNotExist) {
		return newIndex(), nil
	}
	if err != nil {
		return index{}, fmt.Errorf("read profile index: %w", err)
	}
	return validateIndex(state)
}

func validateIndex(state index) (index, error) {
	if state.Schema != Schema || state.Hosts == nil {
		return index{}, errors.New("unsupported or incomplete profile index")
	}
	if state.LastTransaction != "" && !storedID.MatchString(state.LastTransaction) {
		return index{}, errors.New("profile index has an invalid transaction ID")
	}
	ids, names, identities := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, profile := range state.Profiles {
		if err := validateManifest(profile); err != nil {
			return index{}, err
		}
		name := profile.Harness + ":" + strings.ToLower(profile.Name)
		identity := profile.Harness + ":" + profile.Identity.Key
		if ids[profile.ID] || names[name] || (profile.Identity.Key != "" && identities[identity]) {
			return index{}, errors.New("profile index has a duplicate ID, name, or account mapping")
		}
		ids[profile.ID], names[name], identities[identity] = true, true, true
	}
	for harness, host := range state.Hosts {
		profile := state.byID(host.ProfileID)
		if profile == nil || profile.Harness != harness || host.Pending != (profile.Identity.Key == "") || !contentDigest.MatchString(host.BaseDigest) || (host.RecoveryID != "" && !storedID.MatchString(host.RecoveryID)) {
			return index{}, errors.New("profile index has an invalid host selection")
		}
	}
	return state, nil
}

func validateManifest(profile Manifest) error {
	if profile.CredentialRevision != "" && !storedID.MatchString(profile.CredentialRevision) {
		return errors.New("profile has an invalid credential revision")
	}
	if profile.Schema != Schema || !storedID.MatchString(profile.ID) || !storedID.MatchString(profile.Generation) {
		return errors.New("profile has an invalid schema or storage identity")
	}
	if profile.PreviousGeneration != "" && (!storedID.MatchString(profile.PreviousGeneration) || profile.PreviousGeneration == profile.Generation) {
		return errors.New("profile has an invalid recovery generation")
	}
	if err := ValidateName(profile.Name); err != nil {
		return err
	}
	if _, err := workload.AgentStatePolicy(profile.Harness); err != nil {
		return err
	}
	if err := profile.Account.Validate(); err != nil {
		return err
	}
	for _, value := range []string{profile.Identity.Key, profile.Identity.Label} {
		if len(value) > 512 || strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) {
			return errors.New("profile has an invalid account identity")
		}
	}
	if len(profile.Roots) == 0 || !contentDigest.MatchString(profile.Policy) || !contentDigest.MatchString(profile.Digest) {
		return errors.New("profile has an invalid root policy or content digest")
	}
	ids := map[string]bool{}
	for _, root := range profile.Roots {
		if !profileName.MatchString(root.ID) || ids[root.ID] {
			return errors.New("profile has an invalid or duplicate root ID")
		}
		ids[root.ID] = true
		if root.Kind != workload.Directory && root.Kind != workload.File {
			return errors.New("profile has an invalid root kind")
		}
		for _, path := range []string{root.Target, root.HostPath} {
			if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" || strings.ContainsAny(path, "\x00\r\n") {
				return errors.New("profile has an invalid state path")
			}
		}
		seen := map[string]bool{}
		for _, alias := range root.Aliases {
			if _, err := profilelink.CanonicalStore(alias, profile.Harness, profile.ID, root.ID); err != nil || root.Kind != workload.Directory || seen[alias] {
				return errors.New("profile has an invalid canonical root path")
			}
			if err := workload.ValidateCanonicalTarget(alias, profile.Account.Home); err != nil {
				return err
			}
			seen[alias] = true
		}
	}
	for position, root := range profile.Roots {
		for _, other := range profile.Roots[position+1:] {
			if containsPath(root.Target, other.Target) || containsPath(other.Target, root.Target) || containsPath(root.HostPath, other.HostPath) || containsPath(other.HostPath, root.HostPath) {
				return errors.New("profile roots must not overlap")
			}
		}
	}
	return nil
}

func dataPath(profile Manifest) string {
	return filepath.Join("harnesses", profile.Harness, profile.ID, profile.Generation)
}

// Private paths below the anchored store cannot contain symlink components.
// The source roots themselves can contain symlinks; they are copied as links.
func privatePath(root *os.Root, path string, create bool) error {
	current := ""
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		if part == "" || part == "." || part == ".." {
			return errors.New("invalid profile storage path")
		}
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) && create {
			if err := root.Mkdir(current, 0o700); err != nil {
				return err
			}
			if err := syncRoot(root, filepath.Dir(current)); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
			return fmt.Errorf("profile storage path %s must be a private directory without symlinks", current)
		}
	}
	return nil
}

func readJSON(root *os.Root, path string, value any) error {
	file, err := root.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 4<<20 || info.Mode().Perm()&0o077 != 0 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) {
		return errors.New("profile metadata must be a private regular file below 4 MiB")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 4<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return errors.New("profile metadata is not valid JSON for this schema")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("profile metadata has trailing data")
	}
	return nil
}

func writeJSON(root *os.Root, path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	id, err := newID()
	if err != nil {
		return err
	}
	if len(data)+1 > 4<<20 {
		return errors.New("profile metadata exceeds 4 MiB; no change was published")
	}
	temporary := path + "." + id + ".part"
	defer root.Remove(temporary)
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := io.Copy(file, bytes.NewReader(append(data, '\n')))
	err = errors.Join(writeErr, file.Sync(), file.Close())
	if err != nil {
		return err
	}
	if err := root.Rename(temporary, path); err != nil {
		return err
	}
	return syncRoot(root, filepath.Dir(path))
}

func syncRoot(root *os.Root, path string) error {
	directory, err := root.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
