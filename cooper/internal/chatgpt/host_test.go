package chatgpt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostVersionFollowsLiteralLauncherWithoutStartingApp(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	app := filepath.Join(root, "package", "chatgpt")
	resources := filepath.Join(filepath.Dir(app), "resources")
	for _, dir := range []string{bin, resources} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(app, []byte("#!/bin/sh\ntouch \"$0.started\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resources, "linux-package-metadata.json"), []byte(`{"codexAppBrand":"chatgpt","version":"26.928.20755"}`), 0600); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(bin, "chatgpt")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\n# Host display settings.\nexec "+app+" --ozone-platform=wayland \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	version, err := HostVersion()
	if err != nil || version != "26.928.20755" {
		t.Fatalf("host version = %q: %v", version, err)
	}
	if _, err := os.Stat(app + ".started"); !os.IsNotExist(err) {
		t.Fatal("version detection started the desktop app")
	}
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nexec "+launcher+" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := HostVersion(); err == nil || !strings.Contains(err.Error(), "loop") {
		t.Fatalf("launcher loop was not rejected: %v", err)
	}
}

func TestLauncherRejectsUncertainTargets(t *testing.T) {
	for _, script := range []string{
		"exec $APP/chatgpt",
		"exec /opt/$APP/chatgpt",
		"exec /opt/chatgpt;exit",
		"if true; then exec /opt/chatgpt; fi",
		"echo setup\nexec /opt/chatgpt",
		"exec /opt/first\nexec /opt/second",
	} {
		path := filepath.Join(t.TempDir(), "chatgpt")
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := wrapperTarget(path); err == nil {
			t.Fatalf("accepted uncertain launcher: %s", script)
		}
	}
}
