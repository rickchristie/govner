package antigravity

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRetainedReleasesNeedNoNetwork(t *testing.T) {
	client := Client{ManifestURL: func(string) string { t.Fatal("retained release fetched a moving manifest"); return "" }}
	for _, arch := range []string{"amd64", "arm64"} {
		release, err := client.Resolve("1.2.2", arch, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := release.Validate(); err != nil {
			t.Fatal(err)
		}
		if release.Arch != arch || release.Version != "1.2.2" {
			t.Fatalf("wrong release: %+v", release)
		}
	}
}

func TestLatestAndUnavailableVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/amd64" {
			t.Errorf("wrong platform URL: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(retained[0])
	}))
	defer server.Close()
	client := Client{HTTP: server.Client(), ManifestURL: func(arch string) string { return server.URL + "/" + arch }}
	got, err := client.Latest("amd64")
	if err != nil || got != retained[0] {
		t.Fatalf("latest = %+v, %v", got, err)
	}
	if _, err := client.Resolve("1.1.0", "amd64", nil); err == nil || !strings.Contains(err.Error(), "no retained verified release record") {
		t.Fatalf("missing version silently changed: %v", err)
	}
}

func TestRejectReleaseMetadata(t *testing.T) {
	cases := map[string]func(*Release){
		"version mismatch":         func(r *Release) { r.Version = "1.2.1" },
		"unsupported architecture": func(r *Release) { r.Arch = "386" },
		"wrong architecture":       func(r *Release) { r.Arch = "arm64" },
		"missing checksum":         func(r *Release) { r.SHA512 = "" },
		"shell version":            func(r *Release) { r.Version = "1.2.2;true" },
		"http":                     func(r *Release) { r.URL = strings.Replace(r.URL, "https:", "http:", 1) },
		"foreign host":             func(r *Release) { r.URL = strings.Replace(r.URL, "storage.googleapis.com", "example.test", 1) },
		"URL credentials":          func(r *Release) { r.URL = strings.Replace(r.URL, "https://", "https://user:secret@", 1) },
		"query":                    func(r *Release) { r.URL += "?other=true" },
		"fragment":                 func(r *Release) { r.URL += "#other" },
		"wrong bucket":             func(r *Release) { r.URL = strings.Replace(r.URL, "antigravity-public", "other", 1) },
		"path traversal":           func(r *Release) { r.URL = strings.Replace(r.URL, "linux-x64/", "linux-x64/../", 1) },
		"guessed build":            func(r *Release) { r.URL = strings.Replace(r.URL, "6061403484848128", "latest", 1) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			release := retained[0]
			change(&release)
			if err := release.Validate(); err == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
}

func TestRejectBadManifestResponse(t *testing.T) {
	for _, body := range []string{"null", "{}", "{", strings.Repeat(" ", 8193), `{"version":"1.2.2","arch":"arm64"}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
		client := Client{HTTP: server.Client(), ManifestURL: func(string) string { return server.URL }}
		_, err := client.Latest("amd64")
		server.Close()
		if err == nil {
			t.Fatalf("bad manifest accepted (length %d)", len(body))
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer server.Close()
	client := Client{HTTP: server.Client(), ManifestURL: func(string) string { return server.URL }}
	if _, err := client.Latest("amd64"); err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("status error = %v", err)
	}
}
