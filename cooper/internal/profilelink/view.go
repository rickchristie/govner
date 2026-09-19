// Package profilelink validates managed host aliases without importing the
// profile service. Workload policy uses this boundary before creating mounts.
package profilelink

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

const Schema = 2
const Limit = 4 << 20

var ID = regexp.MustCompile(`^[a-f0-9]{24}$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var rootName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Binding grants one host path access to one complete catalog root. File
// bindings use checked copies because native writers can replace file links.
type Binding struct {
	ID        string   `json:"id"`
	Path      string   `json:"path"`
	Source    string   `json:"source"`
	ProfileID string   `json:"profile_id"`
	RootID    string   `json:"root_id"`
	Kind      string   `json:"kind"`
	Base      string   `json:"base,omitempty"`
	Aliases   []string `json:"canonical_paths,omitempty"`
}

type View struct {
	Schema   int             `json:"schema"`
	ID       string          `json:"id"`
	Previous string          `json:"previous,omitempty"`
	Bindings []Binding       `json:"bindings"`
	Catalog  json.RawMessage `json:"catalog"`
}

func Private(info os.FileInfo) bool {
	return info.Mode().Perm()&0077 == 0 && info.Sys().(*syscall.Stat_t).Uid == uint32(os.Getuid())
}

func Open(cooperDir string) (*os.Root, error) {
	parent, err := os.OpenRoot(cooperDir)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	info, err := parent.Lstat("profiles")
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || !Private(info) {
		return nil, errors.New("profile store must be a private directory without symlinks")
	}
	return parent.OpenRoot("profiles")
}

func ReadJSON(root *os.Root, name string, value any) error {
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || !Private(info) || info.Size() > Limit {
		return errors.New("profile metadata must be a private regular file below 4 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, Limit+1))
	if err != nil {
		return err
	}
	if len(data) > Limit {
		return errors.New("profile metadata exceeds 4 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return errors.New("profile metadata does not match its schema")
	}
	var extra any
	if !errors.Is(decoder.Decode(&extra), io.EOF) {
		return errors.New("profile metadata has trailing data")
	}
	return nil
}

// Managed checks the format without accepting an absent descriptor when
// managed control data remains. Corruption must not create a new empty store.
func Managed(root *os.Root) (bool, error) {
	var descriptor map[string]json.RawMessage
	err := ReadJSON(root, "index.json", &descriptor)
	if errors.Is(err, os.ErrNotExist) {
		for _, name := range []string{"current", "views", "credentials"} {
			if _, check := root.Lstat(name); !errors.Is(check, os.ErrNotExist) {
				return false, errors.New("managed profile descriptor is missing; recover the store")
			}
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var schema int
	if err := json.Unmarshal(descriptor["schema"], &schema); err != nil {
		return false, errors.New("profile schema is missing or invalid")
	}
	if schema == 1 {
		if _, err := root.Lstat("current"); !errors.Is(err, os.ErrNotExist) {
			return false, errors.New("copy-mode descriptor has a managed selector; recover the store")
		}
		return false, nil
	}
	if schema != Schema || len(descriptor) != 1 {
		return false, errors.New("unsupported profile store format")
	}
	return true, nil
}

func Ready(root *os.Root) error {
	if _, err := root.Lstat("transaction.json"); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return err
		}
		return errors.New("profile state needs recovery; run 'cooper profiles recover' on the host")
	}
	return nil
}

func Read(root *os.Root) (View, error) {
	link, err := root.Readlink("current")
	if err != nil {
		return View{}, fmt.Errorf("read profile selector: %w", err)
	}
	parts := strings.Split(link, "/")
	if len(parts) != 2 || parts[0] != "views" || !ID.MatchString(parts[1]) {
		return View{}, errors.New("profile selector must name one private view")
	}
	return ReadID(root, parts[1])
}

// ReadID validates immutable view links before recovery or publication can use
// that view. Metadata alone cannot authorize a different symlink destination.
func ReadID(root *os.Root, id string) (View, error) {
	return readID(root, id, true)
}

// ReadControlID checks retained view metadata and link destinations. A past
// data directory can now be a recorded compatibility alias or be between two
// journaled renames. Recovery separately checks exact entries and parents.
func ReadControlID(root *os.Root, id string) (View, error) {
	return readID(root, id, false)
}

func readID(root *os.Root, id string, current bool) (View, error) {
	if !ID.MatchString(id) {
		return View{}, errors.New("invalid profile view ID")
	}
	link := filepath.Join("views", id)
	if err := CheckDirectories(root, filepath.Join(link, "roots")); err != nil {
		return View{}, err
	}
	var view View
	if err := ReadJSON(root, filepath.Join(link, "state.json"), &view); err != nil {
		return View{}, err
	}
	if err := Validate(view); err != nil {
		return View{}, err
	}
	if view.ID != id {
		return View{}, errors.New("profile view ID does not match its selector")
	}
	for _, binding := range view.Bindings {
		if err := CheckDirectories(root, filepath.Dir(binding.Source)); err != nil {
			return View{}, err
		}
		if binding.Kind != "directory" {
			continue
		}
		if current {
			info, err := root.Lstat(binding.Source)
			if err != nil {
				return View{}, err
			}
			if !info.IsDir() {
				return View{}, errors.New("managed directory root is missing or changed its type")
			}
		}
		target, err := root.Readlink(filepath.Join(link, "roots", binding.ID))
		if err != nil {
			return View{}, err
		}
		if target != filepath.Join("..", "..", "..", binding.Source) {
			return View{}, errors.New("profile view root link does not match its binding")
		}
	}
	return view, nil
}

func CheckDirectories(root *os.Root, path string) error {
	current := ""
	for _, part := range strings.Split(path, string(filepath.Separator)) {
		if part == "" || part == "." || part == ".." {
			return errors.New("invalid private profile path")
		}
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() || !Private(info) {
			return errors.New("profile control parents must be private directories without symlinks")
		}
	}
	return nil
}

func Validate(view View) error {
	if view.Schema != Schema || !ID.MatchString(view.ID) || (view.Previous != "" && !ID.MatchString(view.Previous)) || len(view.Catalog) == 0 {
		return errors.New("invalid managed profile view")
	}
	seenIDs, seenPaths := map[string]bool{}, map[string]bool{}
	for _, binding := range view.Bindings {
		if !ID.MatchString(binding.ID) || !ID.MatchString(binding.ProfileID) || seenIDs[binding.ID] || seenPaths[binding.Path] {
			return errors.New("duplicate or invalid profile binding")
		}
		if !filepath.IsAbs(binding.Path) || filepath.Clean(binding.Path) != binding.Path || binding.Path == "/" || strings.ContainsAny(binding.Path, "\x00\r\n") {
			return errors.New("invalid managed host path")
		}
		parts := strings.Split(binding.Source, "/")
		if len(parts) != 6 || parts[0] != "harnesses" || parts[2] != binding.ProfileID || !ID.MatchString(parts[3]) || parts[4] != "roots" || parts[5] != binding.RootID || !rootName.MatchString(parts[1]) || !rootName.MatchString(binding.RootID) {
			return errors.New("managed source must name one profile root")
		}
		for _, alias := range binding.Aliases {
			if _, err := CanonicalStore(alias, parts[1], binding.ProfileID, binding.RootID); err != nil || binding.Kind != "directory" {
				return errors.New("invalid canonical root path")
			}
		}
		if binding.Kind == "file" && binding.Base != "absent" && !digestPattern.MatchString(binding.Base) {
			return errors.New("invalid standalone file base digest")
		}
		if binding.Kind == "directory" && binding.Base != "" {
			return errors.New("directory binding has a file digest")
		}
		if binding.Kind != "directory" && binding.Kind != "file" {
			return errors.New("invalid managed root kind")
		}
		for path := range seenPaths {
			if strings.HasPrefix(path, binding.Path+"/") || strings.HasPrefix(binding.Path, path+"/") {
				return errors.New("managed host roots overlap")
			}
		}
		seenIDs[binding.ID], seenPaths[binding.Path] = true, true
	}
	return nil
}

func Alias(cooperDir string, binding Binding) string {
	return filepath.Join(cooperDir, "profiles", "current", "roots", binding.ID)
}

// Locate follows user aliases but stops before a Cooper host link. Stopping
// here preserves the public path when an explicit CODEX_HOME is canonicalized.
func Locate(path string) (logical, owner, bindingID string, err error) {
	path, err = filepath.Abs(path)
	if err != nil {
		return "", "", "", err
	}
	for links := 0; links < 40; {
		parts := strings.Split(strings.TrimPrefix(filepath.Clean(path), "/"), "/")
		current, followed := "/", false
		for position, part := range parts {
			current = filepath.Join(current, part)
			info, check := os.Lstat(current)
			if errors.Is(check, os.ErrNotExist) {
				return path, "", "", nil
			}
			if check != nil {
				return "", "", "", check
			}
			if info.Mode()&os.ModeSymlink == 0 {
				continue
			}
			target, check := os.Readlink(current)
			if check != nil {
				return "", "", "", check
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(current), target)
			}
			target = filepath.Clean(target)
			marker := "/profiles/current/roots/"
			if offset := strings.LastIndex(target, marker); offset > 0 && ID.MatchString(target[offset+len(marker):]) {
				if position != len(parts)-1 {
					return "", "", "", errors.New("a managed agent root cannot be selected through a child path")
				}
				return current, target[:offset], target[offset+len(marker):], nil
			}
			path = filepath.Join(append([]string{target}, parts[position+1:]...)...)
			links++
			followed = true
			break
		}
		if !followed {
			return path, "", "", nil
		}
	}
	return "", "", "", errors.New("too many state path symlinks")
}

// Resolve grants an exact registered directory root, never the store parent.
// Callers retain responsibility for catalog, protected targets, and use locks.
func Resolve(path, cooperDir string) (Binding, bool, error) {
	logical, owner, id, err := Locate(path)
	if err != nil || owner == "" {
		return Binding{}, false, err
	}
	if filepath.Clean(owner) != filepath.Clean(cooperDir) {
		return Binding{}, false, errors.New("agent state belongs to another Cooper profile store; use that Cooper directory")
	}
	store, err := Open(owner)
	if err != nil {
		return Binding{}, false, err
	}
	defer store.Close()
	managed, err := Managed(store)
	if err != nil {
		return Binding{}, false, err
	}
	if !managed {
		return Binding{}, false, errors.New("managed host link has no managed store")
	}
	if err := Ready(store); err != nil {
		return Binding{}, false, err
	}
	view, err := Read(store)
	if err != nil {
		return Binding{}, false, err
	}
	for _, binding := range view.Bindings {
		if binding.ID != id {
			continue
		}
		if binding.Path != logical || binding.Kind != "directory" {
			return Binding{}, false, errors.New("host link does not match its registered path")
		}
		if err := CheckDirectories(store, filepath.Dir(binding.Source)); err != nil {
			return Binding{}, false, err
		}
		info, err := store.Lstat(binding.Source)
		if err != nil {
			return Binding{}, false, err
		}
		if !info.IsDir() {
			return Binding{}, false, errors.New("managed root is missing or changed its type")
		}
		return binding, true, nil
	}
	return Binding{}, false, errors.New("host link has no registered profile binding")
}

// CheckAliases detects an application that replaced a host link. Do not create
// a second state tree or silently mount it when a registered link is missing.
func CheckAliases(cooperDir string, view View) error {
	for _, binding := range view.Bindings {
		info, err := os.Lstat(binding.Path)
		if binding.Kind == "file" && errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("managed host root %s is missing; restore its link or detach from a backup: %w", binding.Path, err)
		}
		if binding.Kind == "file" {
			if !info.Mode().IsRegular() {
				return errors.New("managed standalone file changed its type")
			}
			continue
		}
		link, err := os.Readlink(binding.Path)
		if err != nil || link != Alias(cooperDir, binding) {
			return fmt.Errorf("managed host link %s changed; state was retained; restore the registered link before continuing", binding.Path)
		}
	}
	return nil
}
