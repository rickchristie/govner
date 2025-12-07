package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds all configuration for dblocker
type Config struct {
	DBHost           string `json:"db_host"`
	DBPort           string `json:"db_port"`
	DBUsername       string `json:"db_username"`
	DBPassword       string `json:"db_password"`
	DBDatabasePrefix string `json:"db_database_prefix"`
	TestDBCount      int    `json:"test_db_count"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		DBHost:           "localhost",
		DBPort:           "9090",
		DBUsername:       "tester",
		DBPassword:       "LegacyCodeIsOneWithNoTest",
		DBDatabasePrefix: "tester",
		TestDBCount:      25,
	}
}

// LoadConfig loads configuration from a JSON file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate
	if cfg.TestDBCount < 1 {
		cfg.TestDBCount = 1
	}
	if cfg.TestDBCount > 100 {
		cfg.TestDBCount = 100
	}

	return &cfg, nil
}

// Save saves the configuration to a JSON file
func (c *Config) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// RunSetup runs the interactive setup wizard
func RunSetup() (*Config, string, error) {
	reader := bufio.NewReader(os.Stdin)
	cfg := DefaultConfig()

	fmt.Println("DBLocker Setup")
	fmt.Println("==============")
	fmt.Println()

	// DB Host
	fmt.Printf("Database host [%s]: ", cfg.DBHost)
	if input := readLine(reader); input != "" {
		cfg.DBHost = input
	}

	// DB Port
	fmt.Printf("Database port [%s]: ", cfg.DBPort)
	if input := readLine(reader); input != "" {
		cfg.DBPort = input
	}

	// DB Username
	fmt.Printf("Database username [%s]: ", cfg.DBUsername)
	if input := readLine(reader); input != "" {
		cfg.DBUsername = input
	}

	// DB Password
	fmt.Printf("Database password [%s]: ", cfg.DBPassword)
	if input := readLine(reader); input != "" {
		cfg.DBPassword = input
	}

	// DB Database Prefix
	fmt.Printf("Database name prefix [%s]: ", cfg.DBDatabasePrefix)
	if input := readLine(reader); input != "" {
		cfg.DBDatabasePrefix = input
	}

	// Test DB Count
	fmt.Printf("Number of test databases (1-100) [%d]: ", cfg.TestDBCount)
	if input := readLine(reader); input != "" {
		if count, err := strconv.Atoi(input); err == nil {
			if count < 1 {
				count = 1
			}
			if count > 100 {
				count = 100
			}
			cfg.TestDBCount = count
		}
	}

	fmt.Println()

	// Config file path
	defaultPath := getDefaultConfigPath()
	fmt.Printf("Save config to [%s]: ", defaultPath)
	configPath := readLine(reader)
	if configPath == "" {
		configPath = defaultPath
	}

	// Expand ~ to home directory
	if strings.HasPrefix(configPath, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			configPath = filepath.Join(home, configPath[2:])
		}
	}

	return cfg, configPath, nil
}

func readLine(reader *bufio.Reader) string {
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

func getDefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "./dblocker.json"
	}
	return filepath.Join(home, ".config", "dblocker", "config.json")
}
