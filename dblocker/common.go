package main

import "fmt"

// Authentication password - hardcoded as required (VPN protected)
const dbLockerPassword = "gotestyourcode"

// Global state initialized from config
var testDatabases map[string]bool

// InitFromConfig initializes the global state from a config
func InitFromConfig(cfg *Config) {
	testDatabases = make(map[string]bool)
	for i := 1; i <= cfg.TestDBCount; i++ {
		connString := fmt.Sprintf("postgresql://%s:%s@%s:%s/%s%v",
			cfg.DBUsername, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBDatabasePrefix, i)
		testDatabases[connString] = true
	}
}
