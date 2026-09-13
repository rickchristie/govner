// Package antigravity resolves the native CLI archive. Google puts an opaque
// build ID in archive URLs, so a version alone cannot construct a download.
package antigravity

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const ManifestHost = "antigravity-cli-auto-updater-974169037036.us-central1.run.app"

// Release is a frozen, platform-specific build input. Rendering never fetches
// a moving manifest. Saved records let Mirror and Pin retain an older release.
type Release struct {
	Version string `json:"version"`
	Arch    string `json:"arch"`
	URL     string `json:"url"`
	SHA512  string `json:"sha512"`
}

var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{128}$`)
var buildPattern = regexp.MustCompile(`^[0-9]+$`)

// KnownReleases returns independent records for an offline, reviewed release.
// An empty result means the caller must resolve official metadata first.
func KnownReleases(version string) []Release {
	var result []Release
	for _, release := range retained {
		if release.Version == version {
			result = append(result, release)
		}
	}
	return result
}

// Validate rejects metadata that could install another product, escape the
// official bucket, or become shell syntax in a generated Dockerfile.
func (r Release) Validate() error {
	platform := map[string]string{"amd64": "linux-x64", "arm64": "linux-arm"}[r.Arch]
	if platform == "" || !versionPattern.MatchString(r.Version) || !digestPattern.MatchString(r.SHA512) {
		return fmt.Errorf("invalid Antigravity release version, architecture, or digest")
	}
	u, err := url.Parse(r.URL)
	if err != nil || u.Scheme != "https" || u.Host != "storage.googleapis.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return fmt.Errorf("invalid Antigravity archive URL")
	}
	file := map[string]string{"amd64": "cli_linux_x64.tar.gz", "arm64": "cli_linux_arm64.tar.gz"}[r.Arch]
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 5 || parts[0] != "antigravity-public" || parts[1] != "antigravity-cli" || parts[3] != platform || parts[4] != file {
		return fmt.Errorf("invalid Antigravity archive path")
	}
	build, ok := strings.CutPrefix(parts[2], r.Version+"-")
	if !ok || !buildPattern.MatchString(build) {
		return fmt.Errorf("Antigravity archive path does not match its version")
	}
	return nil
}

// Client keeps network access at version resolution, where callers can show a
// useful error before a build starts. Tests use an in-process manifest server.
type Client struct {
	HTTP        *http.Client
	ManifestURL func(arch string) string
}

func NewClient() Client {
	return Client{
		HTTP:        &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }},
		ManifestURL: func(arch string) string { return "https://" + ManifestHost + "/manifests/linux_" + arch + ".json" },
	}
}

func (c Client) Latest(arch string) (Release, error) {
	if arch != "amd64" && arch != "arm64" {
		return Release{}, fmt.Errorf("unsupported Antigravity architecture %q", arch)
	}
	resp, err := c.HTTP.Get(c.ManifestURL(arch))
	if err != nil {
		return Release{}, fmt.Errorf("fetch Antigravity manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("fetch Antigravity manifest: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8193))
	if err != nil {
		return Release{}, err
	}
	if len(data) > 8192 {
		return Release{}, fmt.Errorf("Antigravity manifest exceeds 8192 bytes")
	}
	var release Release
	if err := json.Unmarshal(data, &release); err != nil {
		return Release{}, fmt.Errorf("decode Antigravity manifest: %w", err)
	}
	// The platform comes from the selected endpoint, not an optional payload
	// field that could make a cross-platform build use the wrong executable.
	if release.Arch != "" && release.Arch != arch {
		return Release{}, fmt.Errorf("Antigravity manifest architecture mismatch")
	}
	release.Arch = arch
	if err := release.Validate(); err != nil {
		return Release{}, err
	}
	return release, nil
}

// Resolve uses a previously checked record or the current official manifest.
// It never substitutes Latest for an unavailable requested version.
func (c Client) Resolve(version, arch string, saved []Release) (Release, error) {
	if !versionPattern.MatchString(version) {
		return Release{}, fmt.Errorf("invalid Antigravity version %q", version)
	}
	for _, records := range [][]Release{saved, retained} {
		for _, release := range records {
			if release.Version != version || release.Arch != arch {
				continue
			}
			if err := release.Validate(); err != nil {
				return Release{}, err
			}
			return release, nil
		}
	}
	release, err := c.Latest(arch)
	if err != nil {
		return Release{}, err
	}
	if release.Version != version {
		return Release{}, fmt.Errorf("Antigravity %s for %s has no retained verified release record; the current release is %s", version, arch, release.Version)
	}
	return release, nil
}

// These records were read from both official manifests on 2026-09-13. Retain
// exact URLs and digests; do not guess historical build IDs. Archive downloads
// are checked again inside the image build before any executable runs.
var retained = []Release{
	{Version: "1.2.2", Arch: "amd64", URL: "https://storage.googleapis.com/antigravity-public/antigravity-cli/1.2.2-6061403484848128/linux-x64/cli_linux_x64.tar.gz", SHA512: "74342cf2a78b344392e573b638a648a6ad1f8e877f494b96e20f9c2b79158d5c423c40b2dcf788703362bb0a9150f09c707fde599d7557ce01c12208802a63cb"},
	{Version: "1.2.2", Arch: "arm64", URL: "https://storage.googleapis.com/antigravity-public/antigravity-cli/1.2.2-6061403484848128/linux-arm/cli_linux_arm64.tar.gz", SHA512: "a1645a30f36b767c7534c2f6a53e99a9bfade993267efcca715f7a45d797d47d6561df787e9d4a51a3bdfc9be855d49d23fa3f4b91b2c661fd17314050836048"},
}
