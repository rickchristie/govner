package templates

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/rickchristie/govner/pgflock/internal/config"
)

//go:embed *.tmpl
var templateFS embed.FS

// DockerfileData holds data for Dockerfile template
type DockerfileData struct {
	PostgresVersion string
	Password        string
	HasPostGIS      bool
}

// InitScriptData holds data for init.sh template
type InitScriptData struct {
	NumDatabases   int
	Username       string
	Password       string
	DatabasePrefix string
	Extensions     []string
	Encoding       string
	LCCollate      string
	LCCtype        string
}

// PostgresConfData holds data for postgresql.conf template
type PostgresConfData struct {
	Port           int
	MaxConnections int
}

// GenerateDockerfile generates Dockerfile content from config
func GenerateDockerfile(cfg *config.Config) (string, error) {
	tmpl, err := template.ParseFS(templateFS, "Dockerfile.tmpl")
	if err != nil {
		return "", fmt.Errorf("failed to parse Dockerfile template: %w", err)
	}

	data := DockerfileData{
		PostgresVersion: cfg.PostgresVersion,
		Password:        cfg.Password,
		HasPostGIS:      hasExtension(cfg.Extensions, "postgis"),
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute Dockerfile template: %w", err)
	}

	return buf.String(), nil
}

// GenerateInitScript generates init.sh content from config
func GenerateInitScript(cfg *config.Config) (string, error) {
	tmpl, err := template.ParseFS(templateFS, "init.sh.tmpl")
	if err != nil {
		return "", fmt.Errorf("failed to parse init.sh template: %w", err)
	}

	data := InitScriptData{
		NumDatabases:   cfg.DatabasesPerInstance,
		Username:       cfg.PGUsername,
		Password:       cfg.Password,
		DatabasePrefix: cfg.DatabasePrefix,
		Extensions:     cfg.Extensions,
		Encoding:       cfg.Encoding,
		LCCollate:      cfg.LCCollate,
		LCCtype:        cfg.LCCtype,
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute init.sh template: %w", err)
	}

	return buf.String(), nil
}

// GeneratePostgresConf generates postgresql.conf content from config
func GeneratePostgresConf(cfg *config.Config, port int) (string, error) {
	tmpl, err := template.ParseFS(templateFS, "postgresql.conf.tmpl")
	if err != nil {
		return "", fmt.Errorf("failed to parse postgresql.conf template: %w", err)
	}

	data := PostgresConfData{
		Port:           port,
		MaxConnections: cfg.MaxConnections,
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute postgresql.conf template: %w", err)
	}

	return buf.String(), nil
}

// WriteAllTemplates writes all generated files to the output directory
func WriteAllTemplates(cfg *config.Config, outputDir string) error {
	// Generate and write Dockerfile
	dockerfile, err := GenerateDockerfile(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outputDir, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}

	// Generate and write init.sh
	initScript, err := GenerateInitScript(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outputDir, "init.sh"), []byte(initScript), 0755); err != nil {
		return fmt.Errorf("failed to write init.sh: %w", err)
	}

	// Generate and write postgresql.conf for first instance port
	port := cfg.StartingPort
	pgConf, err := GeneratePostgresConf(cfg, port)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outputDir, "postgresql.conf"), []byte(pgConf), 0644); err != nil {
		return fmt.Errorf("failed to write postgresql.conf: %w", err)
	}

	return nil
}

func hasExtension(extensions []string, name string) bool {
	for _, ext := range extensions {
		if strings.EqualFold(ext, name) {
			return true
		}
	}
	return false
}
