package templates

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/rickchristie/govner/cooper/internal/chatgpt"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
)

//go:embed desktop/*
var desktopFiles embed.FS

func renderChatGPTDockerfile(cfg *config.Config) (string, error) {
	version := getToolVersion(cfg.AITools, "chatgpt")
	releases := map[string]chatgpt.Release{}
	for _, tool := range cfg.AITools {
		if tool.Name != "chatgpt" {
			continue
		}
		for _, release := range tool.ChatGPTReleases {
			if err := release.Validate(); err != nil {
				return "", err
			}
			if release.Version != version {
				return "", fmt.Errorf("ChatGPT package record does not match the selected version")
			}
			if _, present := releases[release.Arch]; present {
				return "", fmt.Errorf("duplicate ChatGPT package architecture")
			}
			releases[release.Arch] = release
		}
	}
	if len(releases) != 2 {
		return "", fmt.Errorf("ChatGPT build needs exact amd64 and arm64 package records; run cooper build to resolve them")
	}
	data := struct {
		BaseImage string
		Version   string
		ProxyPort int
		AMD       chatgpt.Release
		ARM       chatgpt.Release
	}{docker.GetImageDesktopBase(), version, cfg.ProxyPort, releases["amd64"], releases["arm64"]}
	tmpl, err := template.ParseFS(templateFS, "chatgpt.Dockerfile.tmpl")
	if err != nil {
		return "", err
	}
	var output strings.Builder
	if err := tmpl.Execute(&output, data); err != nil {
		return "", err
	}
	return output.String(), nil
}

func writeDesktopFiles(toolDir string) error {
	entries, err := desktopFiles.ReadDir("desktop")
	if err != nil {
		return err
	}
	directory := filepath.Join(toolDir, "desktop")
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	for _, entry := range entries {
		data, err := desktopFiles.ReadFile("desktop/" + entry.Name())
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(directory, entry.Name()), data, 0644); err != nil {
			return err
		}
	}
	return nil
}
