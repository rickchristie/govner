// Package profiles owns complete harness state roots and account switching.
// The CLI and TUI call the same service. Execution boundaries receive only a
// validated selection; they never implement their own save or load policy.
package profiles

import (
	"context"
	"fmt"
	"time"

	"github.com/rickchristie/govner/cooper/internal/usercontext"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

const Schema = 3

// FileID identifies an entry without reading its contents. Parent identities
// keep a recovery operation from following a replaced ancestor directory.
type FileID struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

// Identity describes local credential identity. It does not assert that a
// remote service currently accepts the credentials. Key contains no token.
type Identity struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type Root struct {
	ID       string            `json:"id"`
	Target   string            `json:"target"`
	HostPath string            `json:"host_path"`
	Kind     workload.PathKind `json:"kind"`
	Present  bool              `json:"present"`
	Parent   FileID            `json:"parent"`
	Entry    FileID            `json:"entry"`
}

type Manifest struct {
	Schema             int                 `json:"schema"`
	ID                 string              `json:"id"`
	Policy             string              `json:"policy"`
	Harness            string              `json:"harness"`
	Name               string              `json:"name"`
	Account            usercontext.Account `json:"account"`
	Identity           Identity            `json:"identity"`
	Created            time.Time           `json:"created"`
	Saved              time.Time           `json:"saved"`
	Roots              []Root              `json:"roots"`
	PathEnvironment    map[string]string   `json:"path_environment"`
	Environment        []workload.EnvVar   `json:"environment"`
	CredentialRevision string              `json:"credential_revision,omitempty"`
}

type HostSelection struct {
	ProfileID string `json:"profile_id"`
	Pending   bool   `json:"pending"`
}

type Summary struct {
	ID, Harness, Name, Account string
	Saved                      time.Time
	Loaded, Pending, InUse     bool
	Mismatch                   bool
}

type SaveRequest struct {
	Harness string
}

type LoadRequest struct {
	Harness, Name string
	Confirmed     bool
	// Interactive confirmation names the outgoing profile. Re-prompt when
	// another Cooper command selected a different profile while the user read.
	ExpectedProfileID string
}

type RestoreRequest struct {
	Harness, Name, Backup string
	Confirmed             bool
	ExpectedProfileID     string
}

type Result struct {
	Saved, Loaded, Recovery     string
	Warning                     string
	Created, Pending, Unchanged bool
}

type IssueKind string

const (
	IdentityUnknown      IssueKind = "identity-unknown"
	AccountConflict      IssueKind = "account-conflict"
	StateInUse           IssueKind = "state-in-use"
	ConfirmationRequired IssueKind = "confirmation-required"
)

// Issue is a domain result that needs an explicit user action. UI code uses
// Kind rather than parsing Message. Recovery contains no credential values.
type Issue struct {
	Kind              IssueKind
	Message, Recovery string
	ProfileID         string
}

func (e *Issue) Error() string {
	if e.Recovery != "" {
		return fmt.Sprintf("%s; current state is preserved at %s", e.Message, e.Recovery)
	}
	return e.Message
}

type IdentityReader interface {
	Read(context.Context, string, []workload.MountSpec, map[string]string) (Identity, error)
}

type UsageGuard interface {
	Check(context.Context, []string) error
}

// ReaderFunc and GuardFunc keep filesystem tests real while replacing only
// provider identity and external runtime discovery at the service boundary.
type ReaderFunc func(context.Context, string, []workload.MountSpec, map[string]string) (Identity, error)

func (f ReaderFunc) Read(ctx context.Context, harness string, roots []workload.MountSpec, env map[string]string) (Identity, error) {
	return f(ctx, harness, roots, env)
}

type GuardFunc func(context.Context, []string) error

func (f GuardFunc) Check(ctx context.Context, paths []string) error { return f(ctx, paths) }
