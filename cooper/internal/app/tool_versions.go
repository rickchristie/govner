package app

import "github.com/rickchristie/govner/cooper/internal/config"

// ToolVersions keeps host commands and package requests outside the UI.
type ToolVersions struct{}

func (ToolVersions) DetectHostVersion(name string) (string, error) {
	return config.DetectHostVersion(name)
}

func (ToolVersions) ValidateVersion(name, version string) (bool, error) {
	return config.ValidateVersion(name, version)
}
