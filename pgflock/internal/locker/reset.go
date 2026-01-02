package locker

import (
	"fmt"
	"net/url"
	"os/exec"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/rickchristie/govner/pgflock/internal/config"
)

// parseConnString parses a PostgreSQL connection string and returns host, port, dbname
func parseConnString(connStr string) (host string, port string, dbname string, user string, password string, err error) {
	u, err := url.Parse(connStr)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("invalid connection string: %w", err)
	}

	host = u.Hostname()
	port = u.Port()
	if port == "" {
		port = "5432"
	}

	dbname = strings.TrimPrefix(u.Path, "/")
	user = u.User.Username()
	password, _ = u.User.Password()

	return host, port, dbname, user, password, nil
}

// ResetDatabase resets a database to pristine condition by dropping and recreating it from test_template
func ResetDatabase(cfg *config.Config, connStr string) error {
	host, port, dbname, user, password, err := parseConnString(connStr)
	if err != nil {
		return err
	}

	log.Debug().Str("dbname", dbname).Str("port", port).Msg("Resetting database")

	// Build environment for psql commands
	env := []string{
		fmt.Sprintf("PGPASSWORD=%s", password),
	}

	// Connect to 'postgres' database to drop/create the target database
	postgresConnStr := fmt.Sprintf("postgresql://%s@%s:%s/postgres", user, host, port)

	// Step 1: Terminate any existing connections to the database
	terminateSQL := fmt.Sprintf(
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid();",
		dbname,
	)
	if err := runPsql(postgresConnStr, terminateSQL, env); err != nil {
		// Log but don't fail - there might be no connections
		log.Debug().Err(err).Str("dbname", dbname).Msg("Failed to terminate connections (may be none)")
	}

	// Step 2: Drop the database if exists
	dropSQL := fmt.Sprintf("DROP DATABASE IF EXISTS %s;", dbname)
	if err := runPsql(postgresConnStr, dropSQL, env); err != nil {
		return fmt.Errorf("failed to drop database: %w", err)
	}

	// Step 3: Create the database from test_template
	createSQL := fmt.Sprintf(
		"CREATE DATABASE %s WITH ENCODING '%s' LC_COLLATE='%s' LC_CTYPE='%s' TEMPLATE=test_template;",
		dbname, cfg.Encoding, cfg.LCCollate, cfg.LCCtype,
	)
	if err := runPsql(postgresConnStr, createSQL, env); err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}

	// Step 4: Set owner
	alterOwnerSQL := fmt.Sprintf("ALTER DATABASE %s OWNER TO %s;", dbname, cfg.PGUsername)
	if err := runPsql(postgresConnStr, alterOwnerSQL, env); err != nil {
		return fmt.Errorf("failed to set database owner: %w", err)
	}

	// Step 5: Connect to the new database and set schema owner
	newDbConnStr := fmt.Sprintf("postgresql://%s@%s:%s/%s", user, host, port, dbname)
	alterSchemaSQL := fmt.Sprintf("ALTER SCHEMA public OWNER TO %s;", cfg.PGUsername)
	if err := runPsql(newDbConnStr, alterSchemaSQL, env); err != nil {
		return fmt.Errorf("failed to set schema owner: %w", err)
	}

	log.Debug().Str("dbname", dbname).Msg("Database reset complete")
	return nil
}

// runPsql executes a SQL command via psql
func runPsql(connStr, sql string, env []string) error {
	cmd := exec.Command("psql", connStr, "-c", sql)
	cmd.Env = append(cmd.Environ(), env...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("psql error: %w, output: %s", err, string(output))
	}

	return nil
}
