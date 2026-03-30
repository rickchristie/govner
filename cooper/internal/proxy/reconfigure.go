package proxy

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/templates"
)

// ReconfigureSquid sends a reconfigure signal to Squid inside the proxy
// container, causing it to reload squid.conf without restarting. This is
// a convenience wrapper around docker.ReconfigureSquid.
func ReconfigureSquid() error {
	return docker.ReconfigureSquid()
}

// ReloadSquidConf regenerates squid.conf from the current config, writes
// it to cooperDir/proxy/squid.conf (which is volume-mounted into the
// proxy container), and then signals Squid to reload its configuration.
//
// This enables hot-reload of whitelist and ACL changes without
// restarting the proxy container.
func ReloadSquidConf(cooperDir string, cfg *config.Config) error {
	// Render squid.conf from the template and current config.
	content, err := templates.RenderSquidConf(cfg)
	if err != nil {
		return fmt.Errorf("render squid.conf: %w", err)
	}

	// Ensure the proxy directory exists.
	proxyDir := filepath.Join(cooperDir, "proxy")
	if err := os.MkdirAll(proxyDir, 0755); err != nil {
		return fmt.Errorf("create proxy directory: %w", err)
	}

	// Write the regenerated squid.conf.
	squidConfPath := filepath.Join(proxyDir, "squid.conf")
	if err := os.WriteFile(squidConfPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write squid.conf: %w", err)
	}

	// Signal Squid to reload the configuration.
	if err := ReconfigureSquid(); err != nil {
		return fmt.Errorf("reconfigure squid: %w", err)
	}

	return nil
}
