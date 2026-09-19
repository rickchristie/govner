// Package profiles owns complete harness state copies and account switching.
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

const Schema = 1

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
	Aliases  []string          `json:"canonical_paths,omitempty"`
}

type Manifest struct {
	Schema             int                 `json:"schema"`
	ID                 string              `json:"id"`
	Generation         string              `json:"generation"`
	PreviousGeneration string              `json:"previous_generation,omitempty"`
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
	Digest             string              `json:"digest"`
	CredentialRevision string              `json:"credential_revision,omitempty"`
}

type HostSelection struct {
	ProfileID  string `json:"profile_id"`
	BaseDigest string `json:"base_digest"`
	Pending    bool   `json:"pending"`
	RecoveryID string `json:"recovery_id,omitempty"`
}

type HostRoot struct {
	Path, Harness, Profile string
	Selected               bool
}

type Summary struct {
	ID, Harness, Name, Account string
	Saved                      time.Time
	Loaded, Pending, InUse     bool
	Managed, Mixed, Mismatch   bool
	HostRoots                  []HostRoot
}

type SaveRequest struct {
	Harness string
	// NewName is permitted only for an unmapped account. It cannot name an
	// existing destination or override the detected account mapping.
	NewName        string
	ConflictChoice ConflictChoice
}

type LoadRequest struct {
	Harness, Name, NewName string
	ConflictChoice         ConflictChoice
}

type ConflictChoice string

const (
	KeepHost  ConflictChoice = "host"
	KeepSaved ConflictChoice = "saved"
)

type Result struct {
	Saved, Loaded, Recovery     string
	Warning                     string
	Created, Pending, Unchanged bool
	Managed                     bool
}

type IssueKind string

const (
	NameRequired    IssueKind = "name-required"
	IdentityUnknown IssueKind = "identity-unknown"
	AccountConflict IssueKind = "account-conflict"
	StateConflict   IssueKind = "state-conflict"
	StateInUse      IssueKind = "state-in-use"
	MixedState      IssueKind = "mixed-state"
)

// Issue is a domain result that needs an explicit user action. UI code uses
// Kind rather than parsing Message. Recovery contains no credential values.
type Issue struct {
	Kind              IssueKind
	Message, Recovery string
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
