package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config holds all pgflock configuration
type Config struct {
	DockerNamePrefix string `yaml:"docker_name_prefix"`

	// PostgreSQL instances
	InstanceCount int `yaml:"instance_count"` // Number of PostgreSQL instances
	StartingPort  int `yaml:"starting_port"`  // First instance port, subsequent instances get port+1, port+2, etc.

	// Shared settings
	DatabasesPerInstance int    `yaml:"databases_per_instance"`
	TmpfsSize            string `yaml:"tmpfs_size"`
	ShmSize              string `yaml:"shm_size"`
	CPULimit             string `yaml:"cpu_limit,omitempty"` // CPU limit per container (e.g., "2.0"), empty for no limit

	// dblocker settings
	LockerPort     int `yaml:"locker_port"`
	AutoUnlockMins int `yaml:"auto_unlock_minutes"`

	// PostgreSQL settings
	PGUsername      string   `yaml:"pg_username"`
	Password        string   `yaml:"password"`
	DatabasePrefix  string   `yaml:"database_prefix"`
	Extensions      []string `yaml:"extensions"`
	PostgresVersion string   `yaml:"postgres_version"`
	Encoding        string   `yaml:"encoding"`
	LCCollate       string   `yaml:"lc_collate"`
	LCCtype         string   `yaml:"lc_ctype"`
	MaxConnections  int      `yaml:"max_connections"`
}

// InstancePorts returns the list of ports for all instances
func (c *Config) InstancePorts() []int {
	ports := make([]int, c.InstanceCount)
	for i := 0; i < c.InstanceCount; i++ {
		ports[i] = c.StartingPort + i
	}
	return ports
}

// LoadConfig loads configuration from a YAML file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &cfg, nil
}

// SaveConfig saves configuration to a YAML file
func SaveConfig(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		DockerNamePrefix:     "pgflock",
		InstanceCount:        1,
		StartingPort:         5432,
		DatabasesPerInstance: 10,
		TmpfsSize:            "1024m",
		ShmSize:              "1g",
		CPULimit:             "", // Empty = no CPU limit
		LockerPort:           9191,
		AutoUnlockMins:       5,
		PGUsername:           "tester",
		Password:             "pgflock",
		DatabasePrefix:       "tester",
		Extensions:           []string{},
		PostgresVersion:      "15",
		Encoding:             "UTF8",
		LCCollate:            "en_US.UTF-8",
		LCCtype:              "en_US.UTF-8",
		MaxConnections:       100,
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.DockerNamePrefix == "" {
		return fmt.Errorf("docker_name_prefix is required")
	}
	if c.InstanceCount <= 0 {
		return fmt.Errorf("instance_count must be at least 1")
	}
	if c.StartingPort <= 0 || c.StartingPort > 65535 {
		return fmt.Errorf("invalid starting_port %d", c.StartingPort)
	}
	// Check that all generated ports are valid
	lastPort := c.StartingPort + c.InstanceCount - 1
	if lastPort > 65535 {
		return fmt.Errorf("instance ports exceed valid range (last port would be %d)", lastPort)
	}
	if c.DatabasesPerInstance <= 0 {
		return fmt.Errorf("databases_per_instance must be positive")
	}
	if c.LockerPort <= 0 || c.LockerPort > 65535 {
		return fmt.Errorf("invalid locker_port %d", c.LockerPort)
	}
	if c.PGUsername == "" {
		return fmt.Errorf("pg_username is required")
	}
	if c.Password == "" {
		return fmt.Errorf("password is required")
	}
	if c.DatabasePrefix == "" {
		return fmt.Errorf("database_prefix is required")
	}
	return nil
}

// TotalDatabases returns the total number of databases across all instances
func (c *Config) TotalDatabases() int {
	return c.InstanceCount * c.DatabasesPerInstance
}

// ImageName returns the Docker image name
func (c *Config) ImageName() string {
	return c.DockerNamePrefix + "-pg-image"
}

// ContainerName returns the Docker container name for a given port
func (c *Config) ContainerName(port int) string {
	return fmt.Sprintf("%s-%d", c.DockerNamePrefix, port)
}
