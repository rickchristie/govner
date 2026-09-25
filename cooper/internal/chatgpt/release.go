// Package chatgpt owns the official Linux desktop package contract.
package chatgpt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const PackageBase = "https://persistent.oaistatic.com/codex-app-prod/linux/deb/"

var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z][0-9A-Za-z._]*)?$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Release freezes an official package path before Docker starts. Older
// packages can remain available after leaving the moving APT index. Those
// packages have no index digest; the build still checks their exact package
// name, architecture, and version after the HTTPS download.
type Release struct {
	Version string `json:"version"`
	Arch    string `json:"arch"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256,omitempty"`
	Size    int64  `json:"size"`
}

func (r Release) Validate() error {
	if !versionPattern.MatchString(r.Version) {
		return errors.New("ChatGPT package version is invalid")
	}
	if r.Arch != "amd64" && r.Arch != "arm64" {
		return fmt.Errorf("unsupported ChatGPT package architecture %q", r.Arch)
	}
	if r.URL != packageURL(r.Version, r.Arch) {
		return errors.New("ChatGPT package URL is not the exact official version path")
	}
	if r.SHA256 != "" && !digestPattern.MatchString(r.SHA256) {
		return errors.New("ChatGPT package SHA256 is invalid")
	}
	if r.Size <= 0 || r.Size > 2<<30 {
		return errors.New("ChatGPT package size is invalid")
	}
	return nil
}

// Client bounds metadata reads and lets tests replace the HTTP transport.
type Client struct{ HTTP *http.Client }

func NewClient() Client {
	return Client{HTTP: &http.Client{Timeout: 20 * time.Second}}
}

func (c Client) Latest(arch string) (Release, error) {
	releases, err := c.index(arch)
	if err != nil {
		return Release{}, err
	}
	if len(releases) == 0 {
		return Release{}, errors.New("official ChatGPT package index has no release")
	}
	latest := releases[0]
	for _, release := range releases[1:] {
		if newerVersion(release.Version, latest.Version) {
			latest = release
		}
	}
	return latest, nil
}

// Resolve accepts every published version. A pin never changes to Latest.
func (c Client) Resolve(version, arch string, previous []Release) (Release, error) {
	wanted := Release{Version: version, Arch: arch, URL: packageURL(version, arch), Size: 1}
	if err := wanted.Validate(); err != nil {
		return Release{}, err
	}
	for _, release := range previous {
		if release.Version == version && release.Arch == arch {
			if err := release.Validate(); err != nil {
				return Release{}, err
			}
			return release, nil
		}
	}
	releases, err := c.index(arch)
	if err != nil {
		return Release{}, err
	}
	for _, release := range releases {
		if release.Version == version {
			return release, nil
		}
	}
	response, err := c.HTTP.Head(wanted.URL)
	if err != nil {
		return Release{}, fmt.Errorf("check ChatGPT %s package: %w", version, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("ChatGPT %s for %s is unavailable (HTTP %d)", version, arch, response.StatusCode)
	}
	wanted.Size = response.ContentLength
	if err := wanted.Validate(); err != nil {
		return Release{}, err
	}
	return wanted, nil
}

func (c Client) index(arch string) ([]Release, error) {
	if arch != "amd64" && arch != "arm64" {
		return nil, fmt.Errorf("unsupported ChatGPT package architecture %q", arch)
	}
	response, err := c.HTTP.Get(PackageBase + "dists/stable/main/binary-" + arch + "/Packages")
	if err != nil {
		return nil, fmt.Errorf("read ChatGPT package index: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ChatGPT package index returned HTTP %d", response.StatusCode)
	}
	const maximumIndex = 8 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, maximumIndex+1))
	if err != nil {
		return nil, fmt.Errorf("read ChatGPT package index: %w", err)
	}
	if len(data) > maximumIndex {
		return nil, errors.New("ChatGPT package index is too large")
	}
	return parseIndex(string(data), arch)
}

func parseIndex(data, arch string) ([]Release, error) {
	var releases []Release
	fields := map[string]string{}
	flush := func() error {
		if fields["Package"] != "chatgpt" || fields["Architecture"] != arch {
			fields = map[string]string{}
			return nil
		}
		size, err := strconv.ParseInt(fields["Size"], 10, 64)
		if err != nil {
			return errors.New("ChatGPT package index has an invalid size")
		}
		path, err := url.JoinPath(PackageBase, fields["Filename"])
		if err != nil {
			return err
		}
		release := Release{Version: fields["Version"], Arch: arch, URL: path, SHA256: fields["SHA256"], Size: size}
		if release.SHA256 == "" {
			return errors.New("ChatGPT package index is missing SHA256")
		}
		if err := release.Validate(); err != nil {
			return err
		}
		releases = append(releases, release)
		fields = map[string]string{}
		return nil
	}
	scanner := bufio.NewScanner(strings.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok || name == "" {
			return nil, errors.New("ChatGPT package index contains an invalid field")
		}
		if _, duplicate := fields[name]; duplicate {
			return nil, fmt.Errorf("ChatGPT package index repeats %s", name)
		}
		fields[name] = strings.TrimSpace(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return releases, nil
}

func packageURL(version, arch string) string {
	return PackageBase + "pool/main/c/chatgpt/chatgpt_" + version + "_" + arch + ".deb"
}

func newerVersion(left, right string) bool {
	leftParts := strings.FieldsFunc(left, func(r rune) bool { return r == '.' || r == '-' })
	rightParts := strings.FieldsFunc(right, func(r rune) bool { return r == '.' || r == '-' })
	for i := 0; i < 3; i++ {
		l, _ := strconv.ParseUint(leftParts[i], 10, 64)
		r, _ := strconv.ParseUint(rightParts[i], 10, 64)
		if l != r {
			return l > r
		}
	}
	return left > right
}
