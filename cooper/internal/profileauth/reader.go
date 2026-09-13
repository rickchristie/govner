// Package profileauth reads local account identity without running a harness,
// refreshing a token, or changing its state. Each adapter has a small reviewed
// schema. Unknown login methods cannot authorize a named profile replacement.
package profileauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unicode"

	"github.com/pelletier/go-toml/v2"
	"github.com/rickchristie/govner/cooper/internal/profiles"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

const maximumDocument = 2 << 20

var errUnknown = errors.New("local account identity is not available for this login method")

type Reader struct{}

func (Reader) Read(ctx context.Context, harness string, mounts []workload.MountSpec, env map[string]string) (profiles.Identity, error) {
	if err := ctx.Err(); err != nil {
		return profiles.Identity{}, err
	}
	view := stateView{mounts: mounts, environment: env}
	var identity profiles.Identity
	var err error
	switch harness {
	case "codex":
		identity, err = view.codex()
	case "claude":
		identity, err = view.claude()
	case "copilot":
		identity, err = view.copilot()
	case "opencode":
		identity, err = view.opencode()
	case "grok":
		identity, err = view.grok()
	case "antigravity":
		identity, err = view.antigravity()
	default:
		return profiles.Identity{}, fmt.Errorf("profiles do not support harness %q", harness)
	}
	if err != nil {
		return profiles.Identity{}, errUnknown
	}
	return identity, ctx.Err()
}

type stateView struct {
	mounts      []workload.MountSpec
	environment map[string]string
}

func (v stateView) root(id string) (workload.MountSpec, error) {
	for _, mount := range v.mounts {
		if mount.ID == id {
			return mount, nil
		}
	}
	return workload.MountSpec{}, os.ErrNotExist
}

func (v stateView) read(id, child string, target any, tomlFormat bool) error {
	mount, err := v.root(id)
	if err != nil {
		return err
	}
	path := mount.Source
	// Root aliases are part of the host mount contract. Resolve the root once;
	// boundedFile still rejects links inside that root.
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if child != "" {
		path = filepath.Join(path, child)
	}
	data, err := boundedFile(path)
	if err != nil {
		return err
	}
	if tomlFormat {
		if err := toml.Unmarshal(data, target); err != nil {
			return errUnknown
		}
		return nil
	}
	return decodeJSON(data, target)
}

// Reject symlinked credentials, including parent components. A profile copy
// preserves links but cannot claim ownership of credentials outside its roots.
func boundedFile(path string) ([]byte, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	if resolved != filepath.Clean(path) {
		return nil, errUnknown
	}
	parent, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	file, err := parent.OpenFile(filepath.Base(path), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maximumDocument {
		return nil, errUnknown
	}
	data, err := io.ReadAll(io.LimitReader(file, maximumDocument+1))
	if err != nil || len(data) > maximumDocument {
		return nil, errUnknown
	}
	return data, nil
}

func decodeJSON(data []byte, target any) error {
	if len(data) > maximumDocument || string(data) == "null" {
		return errUnknown
	}
	// Duplicate object keys can hide a second account value. Reject them before
	// decoding the narrow schema; ordinary unknown fields remain compatible.
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	if err := uniqueJSONValue(decoder); err != nil {
		return errUnknown
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errUnknown
	}
	if err := json.Unmarshal(data, target); err != nil {
		return errUnknown
	}
	return nil
}

func uniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	seen := map[string]bool{}
	for decoder.More() {
		if delimiter == '{' {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errUnknown
			}
			seen[name] = true
		}
		if err := uniqueJSONValue(decoder); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func accountIdentity(parts []string, label string) profiles.Identity {
	data, _ := json.Marshal(parts)
	return profiles.Identity{Key: fmt.Sprintf("%x", sha256.Sum256(data)), Label: safeLabel(label)}
}

func apiIdentity(provider, endpoint, key string) (profiles.Identity, error) {
	if strings.TrimSpace(key) == "" {
		return profiles.Identity{}, errUnknown
	}
	return accountIdentity([]string{"api-key", provider, endpoint, key}, provider+" API credential"), nil
}

func safeLabel(value string) string {
	runes := []rune(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, value))
	if len(runes) > 100 {
		runes = runes[:100]
	}
	return string(runes)
}

func jwtClaims(token string, target any) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] == "" || parts[2] == "" || len(token) > maximumDocument {
		return errUnknown
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return errUnknown
	}
	return decodeJSON(data, target)
}

func anyValue(env map[string]string, names ...string) bool {
	for _, name := range names {
		if env[name] != "" {
			return true
		}
	}
	return false
}

func combinedIdentity(identities map[string]profiles.Identity) (profiles.Identity, error) {
	if len(identities) == 0 {
		return profiles.Identity{}, errUnknown
	}
	names := make([]string, 0, len(identities))
	for name := range identities {
		names = append(names, name)
	}
	sort.Strings(names)
	var parts, labels []string
	for _, name := range names {
		parts = append(parts, name, identities[name].Key)
		labels = append(labels, identities[name].Label)
	}
	return accountIdentity(parts, strings.Join(labels, "; ")), nil
}
