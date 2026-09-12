package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultVMConfigIsValid(t *testing.T) {
	t.Parallel()
	if err := DefaultVMConfig().Validate(); err != nil {
		t.Fatalf("DefaultVMConfig().Validate() error = %v", err)
	}
}

func TestLoadConfigAddsVMDefaults(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.json")
	data := `{"proxy_port":3128,"bridge_port":4343}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VM != DefaultVMConfig() {
		t.Fatalf("loaded VM config = %#v, want %#v", cfg.VM, DefaultVMConfig())
	}
}

func TestVMConfigValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*VMConfig)
		want   string
	}{
		{name: "CPU low", change: func(c *VMConfig) { c.CPUs = 0 }, want: "CPU count"},
		{name: "CPU high", change: func(c *VMConfig) { c.CPUs = 129 }, want: "CPU count"},
		{name: "memory low", change: func(c *VMConfig) { c.MemoryMiB = 1024 }, want: "memory"},
		{name: "disk low", change: func(c *VMConfig) { c.DiskGiB = 4 }, want: "disk"},
		{name: "depth", change: func(c *VMConfig) { c.MaxDepth = 3 }, want: "maximum depth"},
		{name: "start timeout", change: func(c *VMConfig) { c.StartTimeoutS = 29 }, want: "start timeout"},
		{name: "stop timeout", change: func(c *VMConfig) { c.StopTimeoutS = 4 }, want: "stop timeout"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cfg := DefaultVMConfig()
			test.change(&cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want text %q", err, test.want)
			}
		})
	}
}

func TestConfigValidateUsesVMDefaultsForLegacyValues(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	cfg.VM = VMConfig{}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("legacy zero VM config must use defaults: %v", err)
	}
}
