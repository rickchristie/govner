package profilelink

import (
	"errors"
	"path/filepath"
	"strings"
)

// CanonicalStore accepts only a former complete root of this same account.
// It returns the owning Cooper directory, which can differ after relocation.
func CanonicalStore(path, harness, profile, root string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\x00\r\n") {
		return "", errors.New("canonical profile path must be clean and absolute")
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) < 8 {
		return "", errors.New("canonical path must name an account root")
	}
	tail := parts[len(parts)-7:]
	if tail[0] != "profiles" || tail[1] != "harnesses" || tail[2] != harness || tail[3] != profile ||
		!ID.MatchString(tail[4]) || tail[5] != "roots" || tail[6] != root {
		return "", errors.New("canonical path does not belong to this account root")
	}
	return "/" + filepath.Join(parts[:len(parts)-7]...), nil
}
