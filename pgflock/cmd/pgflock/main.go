package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/rickchristie/govner/pgflock/internal/config"
	"github.com/rickchristie/govner/pgflock/internal/configure"
	"github.com/rickchristie/govner/pgflock/internal/docker"
	"github.com/rickchristie/govner/pgflock/internal/locker"
	"github.com/rickchristie/govner/pgflock/internal/tui"
	"github.com/rickchristie/govner/pgflock/meta"
)

var configDir string

// Flags for 'up' command
var (
	upInstances int
	upDatabases int
)

var rootCmd = &cobra.Command{
	Use:   "pgflock",
	Short: "PostgreSQL test database pool manager",
	Long: `pgflock - Shepherd your test databases

Spawn, lock, and control memory-backed PostgreSQL databases for testing.

Quick Start:
  pgflock configure        Create configuration in .pgflock/
  pgflock build            Build the PostgreSQL Docker image
  pgflock up               Start containers and open TUI

Runtime Overrides (for 'up' command):
  -i, --instances <n>      Number of PostgreSQL instances
  -d, --databases <n>      Databases per instance

  pgflock up -i 2 -d 5     Run with 2 instances, 5 databases each`,
	Version: meta.Version,
}

var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Run interactive configuration wizard",
	Long:  `Runs an interactive wizard to configure pgflock and generate necessary files.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := configDir
		if dir == "" {
			dir = ".pgflock"
		}

		cfg, err := configure.Run(dir)
		if err != nil {
			return err
		}

		if err := configure.Save(cfg, dir); err != nil {
			return err
		}

		fmt.Println()
		fmt.Println("Configuration complete!")
		fmt.Printf("Next steps:\n")
		fmt.Printf("  1. pgflock build    # Build the Docker image\n")
		fmt.Printf("  2. pgflock up       # Start the database pool\n")
		return nil
	},
}

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build the PostgreSQL Docker image",
	Long:  `Builds the PostgreSQL Docker image using the generated Dockerfile.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, cfgDir, err := loadConfig()
		if err != nil {
			return err
		}

		fmt.Printf("Building Docker image: %s\n", cfg.ImageName())
		return buildImage(cfg, cfgDir)
	},
}

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Start the database pool with TUI",
	Long:  `Starts PostgreSQL containers, the locker server, and opens the TUI.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _, err := loadConfig()
		if err != nil {
			return err
		}

		// Override config with flags if provided
		if upInstances > 0 {
			cfg.InstanceCount = upInstances
		}
		if upDatabases > 0 {
			cfg.DatabasesPerInstance = upDatabases
		}

		return runUp(cfg)
	},
}

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Stop all PostgreSQL containers",
	Long:  `Stops all running PostgreSQL containers managed by pgflock.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _, err := loadConfig()
		if err != nil {
			return err
		}

		return stopContainers(cfg)
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of containers and locker",
	Long:  `Shows the current status of PostgreSQL containers and the locker server.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _, err := loadConfig()
		if err != nil {
			return err
		}

		return showStatus(cfg)
	},
}

var connectCmd = &cobra.Command{
	Use:   "connect [port] [dbname]",
	Short: "Connect to a database via psql",
	Long:  `Opens a psql session to a specified database.`,
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _, err := loadConfig()
		if err != nil {
			return err
		}

		port := args[0]
		dbname := args[1]
		return connectToDatabase(cfg, port, dbname)
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configDir, "config", "",
		"Path to .pgflock directory (default: ./.pgflock)")

	// Flags for 'up' command
	upCmd.Flags().IntVarP(&upInstances, "instances", "i", 0,
		"Number of PostgreSQL instances (overrides config)")
	upCmd.Flags().IntVarP(&upDatabases, "databases", "d", 0,
		"Databases per instance (overrides config)")

	rootCmd.AddCommand(configureCmd)
	rootCmd.AddCommand(buildCmd)
	rootCmd.AddCommand(upCmd)
	rootCmd.AddCommand(downCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(connectCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func loadConfig() (*config.Config, string, error) {
	dir := configDir
	if dir == "" {
		dir = ".pgflock"
	}

	configPath := filepath.Join(dir, "config.yaml")
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to load config from %s: %w\n\nRun 'pgflock configure' first", configPath, err)
	}

	return cfg, dir, nil
}

func buildImage(cfg *config.Config, cfgDir string) error {
	return docker.BuildImageWithOutput(cfg, cfgDir)
}

func setupLogging(cfgDir string) (*os.File, error) {
	logPath := filepath.Join(cfgDir, "up.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	log.Logger = zerolog.New(logFile).With().Timestamp().Logger()
	return logFile, nil
}

func runUp(cfg *config.Config) error {
	// Set up logging to file
	dir := configDir
	if dir == "" {
		dir = ".pgflock"
	}
	logFile, err := setupLogging(dir)
	if err != nil {
		return err
	}
	defer logFile.Close()

	// Create loading progress channel
	loadingProgressChan := make(chan tui.LoadingProgress, 10)

	// Create TUI model (starts in loading mode)
	model := tui.NewModel(cfg, loadingProgressChan)

	// Variables to hold server state (set during startup)
	var server *http.Server
	var handler *locker.Handler
	var stateUpdateChan chan *locker.State
	var startupErr error

	// Set up quit callback (called only during startup cancel)
	model.SetOnQuit(func() {
		// During startup, we need to clean up whatever was started
		if server != nil {
			locker.StopServer(server)
		}
		docker.StopContainers(cfg)
	})

	// Run startup process in background
	go func() {
		defer close(loadingProgressChan)

		// Step 1: Stop any existing containers
		loadingProgressChan <- tui.LoadingProgress{
			Step:    tui.StepStoppingContainers,
			Message: "Stopping existing containers...",
		}
		_ = docker.StopContainers(cfg)

		// Step 2: Start containers
		loadingProgressChan <- tui.LoadingProgress{
			Step:    tui.StepStartingContainers,
			Message: "Starting PostgreSQL containers...",
		}
		if err := docker.RunContainers(cfg); err != nil {
			loadingProgressChan <- tui.LoadingProgress{
				Step:  tui.StepFailed,
				Error: fmt.Errorf("failed to start containers: %w", err),
			}
			startupErr = err
			return
		}

		// Step 3: Wait for PostgreSQL to be ready (per instance)
		loadingProgressChan <- tui.LoadingProgress{
			Step:    tui.StepWaitingPostgres,
			Message: "Waiting for PostgreSQL...",
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		// Wait for each instance and report progress
		for _, port := range cfg.InstancePorts() {
			if err := docker.WaitForPostgresOnPort(ctx, cfg, port); err != nil {
				loadingProgressChan <- tui.LoadingProgress{
					Step:  tui.StepFailed,
					Error: fmt.Errorf("PostgreSQL on port %d not ready: %w", port, err),
				}
				startupErr = err
				return
			}
			loadingProgressChan <- tui.LoadingProgress{
				Step:    tui.StepWaitingPostgres,
				Message: fmt.Sprintf("PostgreSQL on port %d is ready", port),
				Port:    port,
				Done:    true,
			}
		}

		// Step 4: Start locker server
		loadingProgressChan <- tui.LoadingProgress{
			Step:    tui.StepStartingLocker,
			Message: "Starting locker server...",
		}

		stateUpdateChan = make(chan *locker.State, 10)
		var err error
		server, handler, err = locker.StartServer(cfg, stateUpdateChan)
		if err != nil {
			loadingProgressChan <- tui.LoadingProgress{
				Step:  tui.StepFailed,
				Error: fmt.Errorf("failed to start locker: %w", err),
			}
			startupErr = err
			return
		}

		// Set handler and state channel on model
		model.SetHandler(handler)
		model.SetStateChan(stateUpdateChan)

		// Set up restart callback (now that handler is available)
		model.SetOnRestart(func() <-chan tui.LoadingProgress {
			restartChan := make(chan tui.LoadingProgress, 10)

			go func() {
				defer close(restartChan)

				// Step 1: Unlock all databases
				restartChan <- tui.LoadingProgress{
					Step:    tui.StepStoppingContainers,
					Message: "Unlocking all databases...",
				}
				handler.UnlockAll()

				// Step 2: Stop containers
				restartChan <- tui.LoadingProgress{
					Step:    tui.StepStoppingContainers,
					Message: "Stopping containers...",
				}
				if err := docker.StopContainers(cfg); err != nil {
					restartChan <- tui.LoadingProgress{
						Step:  tui.StepFailed,
						Error: fmt.Errorf("failed to stop containers: %w", err),
					}
					return
				}

				// Step 3: Start containers
				restartChan <- tui.LoadingProgress{
					Step:    tui.StepStartingContainers,
					Message: "Starting containers...",
				}
				if err := docker.RunContainers(cfg); err != nil {
					restartChan <- tui.LoadingProgress{
						Step:  tui.StepFailed,
						Error: fmt.Errorf("failed to start containers: %w", err),
					}
					return
				}

				// Step 4: Wait for PostgreSQL (per instance)
				restartChan <- tui.LoadingProgress{
					Step:    tui.StepWaitingPostgres,
					Message: "Waiting for PostgreSQL...",
				}

				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()

				for _, port := range cfg.InstancePorts() {
					if err := docker.WaitForPostgresOnPort(ctx, cfg, port); err != nil {
						restartChan <- tui.LoadingProgress{
							Step:  tui.StepFailed,
							Error: fmt.Errorf("PostgreSQL on port %d not ready: %w", port, err),
						}
						return
					}
					restartChan <- tui.LoadingProgress{
						Step:    tui.StepWaitingPostgres,
						Message: fmt.Sprintf("PostgreSQL on port %d is ready", port),
						Port:    port,
						Done:    true,
					}
				}

				// Step 5: Ready!
				restartChan <- tui.LoadingProgress{
					Step:    tui.StepReady,
					Message: "Ready!",
				}
			}()

			return restartChan
		})

		// Set up graceful shutdown callback
		model.SetOnShutdown(func() <-chan tui.LoadingProgress {
			shutdownChan := make(chan tui.LoadingProgress, 10)

			go func() {
				defer close(shutdownChan)

				// Step 1: Stopping locker server
				shutdownChan <- tui.LoadingProgress{
					Step:    tui.StepStoppingContainers,
					Message: "Stopping locker server...",
				}
				if server != nil {
					locker.StopServer(server)
				}

				// Step 2: Stopping containers
				shutdownChan <- tui.LoadingProgress{
					Step:    tui.StepStartingContainers, // Reuse step for progress bar
					Message: "Stopping containers...",
				}
				docker.StopContainers(cfg)

				// Step 3: Done
				shutdownChan <- tui.LoadingProgress{
					Step:    tui.StepReady,
					Message: "Shutdown complete",
				}
			}()

			return shutdownChan
		})

		// Step 5: Ready!
		loadingProgressChan <- tui.LoadingProgress{
			Step:    tui.StepReady,
			Message: "Ready!",
		}
	}()

	// Run TUI (starts immediately with startup animation)
	if err := tui.Run(model); err != nil {
		// Clean up on error
		if server != nil {
			locker.StopServer(server)
		}
		docker.StopContainers(cfg)
		return err
	}

	if startupErr != nil {
		return startupErr
	}

	return nil
}

func stopContainers(cfg *config.Config) error {
	fmt.Println("Stopping containers...")
	if err := docker.StopContainers(cfg); err != nil {
		return err
	}
	fmt.Println("All containers stopped")
	return nil
}

func showStatus(cfg *config.Config) error {
	// Container status
	infos, err := docker.ContainerStatus(cfg)
	if err != nil {
		return err
	}

	fmt.Println("Container Status:")
	fmt.Println("-----------------")
	for _, info := range infos {
		pgStatus := "not responding"
		if info.Running && docker.PostgresStatus(cfg, info.Port) {
			pgStatus = "ready"
		}
		fmt.Printf("  %s (port %d): %s, PostgreSQL: %s\n", info.Name, info.Port, info.Status, pgStatus)
	}

	// Locker status
	fmt.Println()
	fmt.Println("Locker Status:")
	fmt.Println("--------------")
	resp, err := healthCheck(cfg.LockerPort)
	if err != nil {
		fmt.Printf("  Locker on port %d: not running\n", cfg.LockerPort)
	} else {
		fmt.Printf("  Locker on port %d: %s\n", cfg.LockerPort, resp)
	}

	return nil
}

func connectToDatabase(cfg *config.Config, port, dbname string) error {
	connStr := fmt.Sprintf("postgresql://%s:%s@localhost:%s/%s",
		cfg.PGUsername, cfg.Password, port, dbname)

	cmd := exec.Command("psql", connStr)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func healthCheck(port int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := exec.CommandContext(ctx, "curl", "-s", fmt.Sprintf("http://localhost:%d/health-check", port)).Output()
	if err != nil {
		return "", err
	}
	return string(req), nil
}
