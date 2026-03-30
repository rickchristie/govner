package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rickchristie/govner/cooper/internal/config"
)

const bridgeRoutesFile = "bridge-routes.json"

// LoadBridgeRoutes loads persisted bridge routes from {cooperDir}/bridge-routes.json.
// If the file does not exist, it returns an empty slice (not an error).
func LoadBridgeRoutes(cooperDir string) ([]config.BridgeRoute, error) {
	path := filepath.Join(cooperDir, bridgeRoutesFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []config.BridgeRoute{}, nil
		}
		return nil, fmt.Errorf("read bridge routes: %w", err)
	}

	var routes []config.BridgeRoute
	if err := json.Unmarshal(data, &routes); err != nil {
		return nil, fmt.Errorf("parse bridge routes: %w", err)
	}

	return routes, nil
}

// SaveBridgeRoutes persists the given bridge routes to {cooperDir}/bridge-routes.json.
func SaveBridgeRoutes(cooperDir string, routes []config.BridgeRoute) error {
	if err := os.MkdirAll(cooperDir, 0755); err != nil {
		return fmt.Errorf("create cooper directory %s: %w", cooperDir, err)
	}
	path := filepath.Join(cooperDir, bridgeRoutesFile)
	data, err := json.MarshalIndent(routes, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal bridge routes: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write bridge routes: %w", err)
	}
	return nil
}
