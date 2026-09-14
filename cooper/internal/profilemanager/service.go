// Package profilemanager connects the profile service to the host account,
// identity adapters, and runtime-use checks. CLI, TUI, and launch code use this
// boundary so policy does not depend on which interface started an operation.
package profilemanager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/profileauth"
	"github.com/rickchristie/govner/cooper/internal/profiles"
	"github.com/rickchristie/govner/cooper/internal/usercontext"
	"github.com/rickchristie/govner/cooper/internal/vmcontext"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

func New(cooperDir, workspace, home string) (*profiles.Service, error) {
	account, err := usercontext.Current()
	if err != nil {
		return nil, err
	}
	account.Home = home
	if err := account.Validate(); err != nil {
		return nil, err
	}
	environment := map[string]string{}
	for _, entry := range os.Environ() {
		name, value, found := strings.Cut(entry, "=")
		if found {
			environment[name] = value
		}
	}
	if runtime.GOOS == "linux" {
		noteSessionBus(environment, filepath.Join("/run/user", strconv.Itoa(account.UID), "bus"))
	}
	return profiles.New(profiles.Options{CooperDir: cooperDir, Workspace: workspace, Account: account,
		Environment: environment, CredentialNames: profileauth.CredentialNames,
		Reader: profileauth.Reader{AntigravityHostHome: home}, Guard: HostUsage{}}), nil
}

// The native keyring library can find the default user bus without an env
// value. Tell the local identity reader that a file can have another active
// credential source. This observation is not a saved or forwarded variable.
func noteSessionBus(environment map[string]string, path string) {
	if environment["DBUS_SESSION_BUS_ADDRESS"] != "" {
		return
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		environment["DBUS_SESSION_BUS_ADDRESS"] = "unix:path=" + path
	}
}

func SelectID(ctx context.Context, cooperDir, workspace, home, harness, id string) (profiles.Selection, error) {
	if err := profiles.CheckReady(cooperDir); err != nil {
		return profiles.Selection{}, err
	}
	if id == "" {
		paths, err := workload.ResolveAgentPaths(harness, home, workspace, workload.HostPathEnvironment())
		return profiles.Selection{Paths: paths}, err
	}
	service, err := New(cooperDir, workspace, home)
	if err != nil {
		return profiles.Selection{}, err
	}
	return service.SelectID(ctx, harness, id)
}

// CheckHost prevents an inner workload's partial mount and process view from
// authorizing replacement of the physical host's state roots.
func CheckHost() error {
	outer, err := vmcontext.Load()
	if err != nil {
		return err
	}
	if outer != nil || os.Getenv("COOPER_CLI_TOOL") != "" {
		return errors.New("change profiles on the host, outside a Cooper Docker or VM session")
	}
	return nil
}
