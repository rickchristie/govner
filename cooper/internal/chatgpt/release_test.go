package chatgpt

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func packageRecord(version, arch string) string {
	return fmt.Sprintf("Package: chatgpt\nVersion: %s\nArchitecture: %s\nFilename: pool/main/c/chatgpt/chatgpt_%s_%s.deb\nSize: 420000000\nSHA256: %s\nDescription: Desktop\n continued text\n\n", version, arch, version, arch, strings.Repeat("a", 64))
}

func TestResolveExactPackagesAndUnindexedPins(t *testing.T) {
	var requests []string
	client := Client{HTTP: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.Method+" "+request.URL.Path)
		status, body, size := 200, "", int64(420000000)
		if request.Method == http.MethodGet {
			body = packageRecord("26.9.8", "amd64") + packageRecord("26.10.1", "amd64")
		} else if strings.Contains(request.URL.Path, "99.0.0") {
			status = 404
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), ContentLength: size}, nil
	})}}
	latest, err := client.Latest("amd64")
	if err != nil || latest.Version != "26.10.1" || latest.SHA256 == "" {
		t.Fatalf("latest = %+v, error = %v", latest, err)
	}
	indexed, err := client.Resolve("26.9.8", "amd64", nil)
	if err != nil || indexed.Version != "26.9.8" || indexed.SHA256 == "" {
		t.Fatalf("indexed package = %+v, error = %v", indexed, err)
	}
	older, err := client.Resolve("25.1.2", "amd64", nil)
	if err != nil || older.URL != packageURL("25.1.2", "amd64") || older.Size != 420000000 {
		t.Fatalf("older package = %+v, error = %v", older, err)
	}
	if _, err := client.Resolve("99.0.0", "amd64", nil); err == nil {
		t.Fatal("missing pin must not select Latest")
	}
	before := len(requests)
	if _, err := client.Resolve(indexed.Version, indexed.Arch, []Release{indexed}); err != nil {
		t.Fatal(err)
	}
	if len(requests) != before {
		t.Fatal("a frozen record must not need the moving index")
	}
}

func TestPackageIndexRejectsUnsafeRecords(t *testing.T) {
	valid := packageRecord("26.10.1", "amd64")
	for name, data := range map[string]string{
		"other package path": strings.ReplaceAll(valid, "pool/main/c/chatgpt/", "pool/main/malware/"),
		"shell version":      strings.ReplaceAll(valid, "26.10.1", "26.10.1;id"),
		"missing digest":     strings.ReplaceAll(valid, "SHA256: "+strings.Repeat("a", 64)+"\n", ""),
		"duplicate version":  strings.ReplaceAll(valid, "Version: 26.10.1", "Version: 26.10.1\nVersion: 26.10.2"),
		"invalid size":       strings.ReplaceAll(valid, "420000000", "-1"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseIndex(data, "amd64"); err == nil {
				t.Fatal("unsafe package record was accepted")
			}
		})
	}
	releases, err := parseIndex(packageRecord("26.10.1", "arm64"), "amd64")
	if err != nil || len(releases) != 0 {
		t.Fatalf("another architecture was selected: %+v, %v", releases, err)
	}
}

func TestHostVersionReadsDesktopMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "linux-package-metadata.json")
	if err := os.WriteFile(path, []byte(`{"codexAppBrand":"chatgpt","version":"26.917.71314"}`), 0600); err != nil {
		t.Fatal(err)
	}
	version, err := readHostVersion(path)
	if err != nil || version != "26.917.71314" {
		t.Fatalf("version = %q, error = %v", version, err)
	}
}

func TestHostVersionFollowsWrapperMetadataWithoutStartingApp(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	wrapper := filepath.Join(root, "desktop")
	resources := filepath.Join(root, "package", "resources")
	for _, directory := range []string{bin, wrapper, resources} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	executable := filepath.Join(wrapper, "chatgpt.sh")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\ntouch \"$0.started\"\nexit 17\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(executable, filepath.Join(bin, "chatgpt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(resources, filepath.Join(wrapper, "resources")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resources, "linux-package-metadata.json"), []byte(`{"codexAppBrand":"chatgpt","version":"26.917.71314"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	version, err := HostVersion()
	if err != nil || version != "26.917.71314" {
		t.Fatalf("nested Mirror version = %q, error = %v", version, err)
	}
	if _, err := os.Stat(executable + ".started"); !os.IsNotExist(err) {
		t.Fatal("version detection started the app")
	}
}
