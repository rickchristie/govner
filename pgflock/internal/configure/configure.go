package configure

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rickchristie/govner/pgflock/internal/config"
	"github.com/rickchristie/govner/pgflock/internal/templates"
)

// Run runs the interactive configuration wizard.
// If configDir contains an existing config.yaml, those values are used as defaults.
func Run(configDir string) (*config.Config, error) {
	reader := bufio.NewReader(os.Stdin)

	// Try to load existing config as defaults
	cfg := config.DefaultConfig()
	existingConfigPath := filepath.Join(configDir, "config.yaml")
	if existingCfg, err := config.LoadConfig(existingConfigPath); err == nil {
		cfg = existingCfg
		fmt.Println("pgflock configuration wizard (updating existing config)")
		fmt.Println("========================================================")
	} else {
		fmt.Println("pgflock configuration wizard")
		fmt.Println("============================")
	}
	fmt.Println("Press Enter to accept default values shown in [brackets]")
	fmt.Println()

	// Docker name prefix - use existing or current directory name
	defaultPrefix := cfg.DockerNamePrefix
	if defaultPrefix == "" {
		currentDir, _ := os.Getwd()
		defaultPrefix = filepath.Base(currentDir)
	}
	cfg.DockerNamePrefix = promptString(reader, "Docker name prefix", defaultPrefix)

	// Number of instances
	cfg.InstanceCount = promptInt(reader, "Number of PostgreSQL instances", cfg.InstanceCount)

	// Starting port (subsequent instances get port+1, port+2, etc.)
	cfg.StartingPort = promptInt(reader, "Starting port (instances get consecutive ports)", cfg.StartingPort)

	// Databases per instance
	cfg.DatabasesPerInstance = promptInt(reader, "Databases per instance", cfg.DatabasesPerInstance)

	// tmpfs size
	cfg.TmpfsSize = promptString(reader, "tmpfs size (e.g., 1024m, 2g)", cfg.TmpfsSize)

	// shm-size
	cfg.ShmSize = promptString(reader, "shm-size (e.g., 1g, 512m)", cfg.ShmSize)

	// CPU limit
	cfg.CPULimit = promptString(reader, "CPU limit per container (e.g., 2.0, empty for no limit)", cfg.CPULimit)

	// Locker port
	cfg.LockerPort = promptInt(reader, "Locker port", cfg.LockerPort)

	// PostgreSQL settings
	cfg.PGUsername = promptString(reader, "PostgreSQL username", cfg.PGUsername)
	cfg.Password = promptString(reader, "Password (shared for all)", cfg.Password)
	cfg.DatabasePrefix = promptString(reader, "Database name prefix", cfg.DatabasePrefix)

	// Extensions - show existing as default
	existingExts := strings.Join(cfg.Extensions, ",")
	extStr := promptString(reader, "Extensions (comma-separated, e.g., postgis,pg_trgm)", existingExts)
	if extStr != "" {
		exts := strings.Split(extStr, ",")
		cfg.Extensions = make([]string, 0, len(exts))
		for _, ext := range exts {
			ext = strings.TrimSpace(ext)
			if ext != "" {
				cfg.Extensions = append(cfg.Extensions, ext)
			}
		}
	} else {
		cfg.Extensions = nil
	}

	// Postgres version
	cfg.PostgresVersion = promptString(reader, "PostgreSQL version", cfg.PostgresVersion)

	// Encoding
	cfg.Encoding = promptString(reader, "Database encoding", cfg.Encoding)

	// Locale
	cfg.LCCollate = promptString(reader, "LC_COLLATE", cfg.LCCollate)
	cfg.LCCtype = promptString(reader, "LC_CTYPE", cfg.LCCtype)

	// Auto-unlock timeout
	cfg.AutoUnlockMins = promptInt(reader, "Auto-unlock timeout (minutes)", cfg.AutoUnlockMins)

	// Max connections
	cfg.MaxConnections = promptInt(reader, "max_connections", cfg.MaxConnections)

	// Validate config
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// Save saves the configuration and generates template files
func Save(cfg *config.Config, configDir string) error {
	// Create .pgflock directory
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Save config.yaml
	configPath := filepath.Join(configDir, "config.yaml")
	if err := config.SaveConfig(configPath, cfg); err != nil {
		return err
	}
	fmt.Printf("Config saved to %s\n", configPath)

	// Generate template files
	if err := templates.WriteAllTemplates(cfg, configDir); err != nil {
		return err
	}
	fmt.Printf("Template files generated in %s\n", configDir)

	return nil
}

func promptString(reader *bufio.Reader, prompt, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", prompt, defaultVal)
	} else {
		fmt.Printf("%s: ", prompt)
	}

	input, err := reader.ReadString('\n')
	if err != nil {
		return defaultVal
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	return input
}

func promptInt(reader *bufio.Reader, prompt string, defaultVal int) int {
	fmt.Printf("%s [%d]: ", prompt, defaultVal)

	input, err := reader.ReadString('\n')
	if err != nil {
		return defaultVal
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}

	val, err := strconv.Atoi(input)
	if err != nil {
		fmt.Printf("Invalid number, using default: %d\n", defaultVal)
		return defaultVal
	}
	return val
}
