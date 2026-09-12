// Package vmcontext defines the host-written boundary contract for Cooper
// commands that run inside a Cooper VM.
package vmcontext

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	Schema      = 3
	DefaultPath = "/run/cooper/vm-context.json"
)

var dockerNetworkName = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,127}$`)

// Context describes the verified outer VM boundary for a nested Cooper run.
// It contains no credential values.
type Context struct {
	Schema         int    `json:"schema"`
	Depth          int    `json:"depth"`
	ParentNetwork  string `json:"parent_network"`
	ParentProxy    string `json:"parent_proxy"`
	AgentContainer string `json:"agent_container"`
	ProxyPort      int    `json:"proxy_port"`
	BridgePort     int    `json:"bridge_port"`
	WorkspaceDir   string `json:"workspace_dir"`
	TempDir        string `json:"temp_dir"`
	CooperDir      string `json:"cooper_dir"`
}

// Validate rejects values that can select an arbitrary route or mount root.
func (c Context) Validate() error {
	if c.Schema != Schema {
		return fmt.Errorf("unsupported Cooper VM context schema %d", c.Schema)
	}
	if c.Depth < 1 || c.Depth > 2 {
		return fmt.Errorf("cooper VM depth %d is outside 1-2", c.Depth)
	}
	if !dockerNetworkName.MatchString(c.ParentNetwork) {
		return errors.New("cooper VM parent network is invalid")
	}
	if !dockerNetworkName.MatchString(c.AgentContainer) {
		return errors.New("cooper VM agent container is invalid")
	}
	if ip := net.ParseIP(c.ParentProxy); ip == nil || ip.To4() == nil || !ip.IsPrivate() {
		return fmt.Errorf("cooper VM parent proxy %q is not a private IPv4 address", c.ParentProxy)
	}
	for name, port := range map[string]int{"proxy": c.ProxyPort, "bridge": c.BridgePort} {
		if port < 1 || port > 65535 {
			return fmt.Errorf("cooper VM parent %s port %d is invalid", name, port)
		}
	}
	for name, path := range map[string]string{
		"workspace":           c.WorkspaceDir,
		"temporary directory": c.TempDir,
		"Cooper directory":    c.CooperDir,
	} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return fmt.Errorf("cooper VM %s path %q is invalid", name, path)
		}
	}
	if c.TempDir != "/tmp" || c.CooperDir != "/home/user/.cooper" {
		return errors.New("cooper VM shared development paths do not match the fixed guest paths")
	}
	return nil
}

// Load loads the outer VM context. A missing file means physical-host mode.
// COOPER_VM_CONTEXT is a protected test hook and is also set explicitly in a
// Cooper VM agent container.
func Load() (*Context, error) {
	path := DefaultPath
	if testPath := strings.TrimSpace(os.Getenv("COOPER_VM_CONTEXT")); testPath != "" {
		path = testPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read Cooper VM context: %w", err)
	}
	var context Context
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&context); err != nil {
		return nil, fmt.Errorf("decode Cooper VM context: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("unexpected data after JSON object")
		}
		return nil, fmt.Errorf("decode Cooper VM context: %w", err)
	}
	if err := context.Validate(); err != nil {
		return nil, err
	}
	return &context, nil
}

// Write writes an atomic, read-only context file.
func Write(path string, context Context) error {
	if err := context.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(context, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Cooper VM context: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create Cooper VM context directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".vm-context-*.part")
	if err != nil {
		return fmt.Errorf("create Cooper VM context temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return fmt.Errorf("write Cooper VM context: %w", err)
	}
	if err := temporary.Chmod(0o444); err != nil {
		temporary.Close()
		return fmt.Errorf("set Cooper VM context mode: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close Cooper VM context: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install Cooper VM context: %w", err)
	}
	return nil
}

// ProxyURL returns the only parent proxy URL allowed by the context.
func (c Context) ProxyURL() string {
	return fmt.Sprintf("http://%s:%d", c.ParentProxy, c.ProxyPort)
}
