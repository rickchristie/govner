package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/antigravity"
	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestAntigravityNativeImageContract(t *testing.T) {
	cfg := testConfig()
	text, err := RenderCLIToolDockerfile(cfg, "antigravity")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"COOPER_CLI_TOOL=antigravity", "COOPER_CLI_EXECUTABLE=agy", "COOPER_CLIPBOARD_MODE=x11",
		"COOPER_CLI_AUTO_APPROVE=\"--dangerously-skip-permissions\"", "AGY_CLI_DISABLE_AUTO_UPDATE=true",
		"sha512sum -c -", "tar -xOzf", "/opt/cooper/bin/agy", `"playwright@$agy_driver_version"`,
		"/opt/cooper/libexec/agy-driver-version /opt/cooper/libexec/agy",
		"/opt/cooper/libexec/agy", "export DBUS_SESSION_BUS_ADDRESS=" + antigravity.FileBusAddress,
		"PLAYWRIGHT_DRIVER_PATH=/opt/cooper/agy-playwright", "PLAYWRIGHT_NODEJS_PATH=/usr/local/bin/node",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("native image is missing %q", want)
		}
	}
	for _, release := range antigravity.KnownReleases("1.2.2") {
		if !strings.Contains(text, release.URL) || !strings.Contains(text, release.SHA512) {
			t.Errorf("missing frozen %s archive", release.Arch)
		}
	}
	for _, unwanted := range []string{"curl -fsSL", "install.sh", "pip install antigravity", "npm install -g antigravity", "ENV GEMINI_API_KEY", "ENV AGY_ADC_AUTH"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("image changes native installation or host auth: %s", unwanted)
		}
	}
}

func TestAntigravityAcceptsSelectedVersionsInEveryMode(t *testing.T) {
	for _, version := range []string{"1.2.2", "1.2.7", "24.7.3"} {
		for _, mode := range []config.VersionMode{config.ModeMirror, config.ModeLatest, config.ModePin} {
			t.Run(version+"/"+mode.String(), func(t *testing.T) {
				releases := antigravity.KnownReleases("1.2.2")
				for index := range releases {
					releases[index].Version = version
					releases[index].URL = strings.Replace(releases[index].URL, "/1.2.2-", "/"+version+"-", 1)
				}
				cfg := &config.Config{AITools: []config.ToolConfig{{Name: "antigravity", Enabled: true,
					Mode: mode, HostVersion: version, PinnedVersion: version, AntigravityReleases: releases}}}
				text, err := RenderCLIToolDockerfile(cfg, "antigravity")
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(text, "test \"$(/opt/cooper/libexec/agy --version)\" = '"+version+"'") {
					t.Fatal("the image does not check the selected native version")
				}
			})
		}
	}
}

func TestAntigravityRenderRejectsIncompleteOrUntrustedInputs(t *testing.T) {
	for _, change := range []func(*config.ToolConfig){
		func(tool *config.ToolConfig) { tool.AntigravityReleases = nil },
		func(tool *config.ToolConfig) { tool.AntigravityReleases = tool.AntigravityReleases[:1] },
		func(tool *config.ToolConfig) { tool.AntigravityReleases[0].URL = "https://other.test/archive.tar.gz" },
		func(tool *config.ToolConfig) { tool.AntigravityReleases[0].SHA512 = "bad" },
		func(tool *config.ToolConfig) { tool.AntigravityReleases[0].Version = "1.2.1" },
		func(tool *config.ToolConfig) {
			tool.AntigravityReleases = append(tool.AntigravityReleases, tool.AntigravityReleases[0])
		},
		func(tool *config.ToolConfig) { tool.PinnedVersion = "1.2.3" },
	} {
		tool := config.ToolConfig{Name: "antigravity", Enabled: true, Mode: config.ModePin, PinnedVersion: "1.2.2", AntigravityReleases: antigravity.KnownReleases("1.2.2")}
		change(&tool)
		if _, err := RenderCLIToolDockerfile(&config.Config{AITools: []config.ToolConfig{tool}}, "antigravity"); err == nil {
			t.Fatalf("unsafe image inputs accepted: %#v", tool)
		}
	}
}

func TestAntigravityCustomDirectoryCollisionPreservesFiles(t *testing.T) {
	cli := t.TempDir()
	dir := filepath.Join(cli, "antigravity")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "Dockerfile")
	const original = "FROM scratch\n# user image\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	err := WriteAllTemplates(t.TempDir(), cli, testConfig(), nil)
	if err == nil || !strings.Contains(err.Error(), "antigravity-custom") {
		t.Fatalf("missing custom image guidance: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != original {
		t.Fatal("custom image was changed")
	}
}
