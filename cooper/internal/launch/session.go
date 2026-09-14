// Package launch owns session policy that is identical for CLI barrels and
// VM workloads. Execution back ends receive only the prepared command and
// environment values.
package launch

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/rickchristie/govner/cooper/internal/auth"
	"github.com/rickchristie/govner/cooper/internal/barrelenv"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/names"
	"github.com/rickchristie/govner/cooper/internal/profiles"
	"github.com/rickchristie/govner/cooper/internal/runtimefs"
)

// SessionRequest identifies one shell or one-shot command in a running
// workload.
type SessionRequest struct {
	Config       *config.Config
	CooperDir    string
	RuntimeID    string
	ToolName     string
	WorkspaceDir string
	OneShot      string
	// State is optional. A named selection supplies all supported credentials;
	// the default selection retains host credential resolution.
	State *profiles.Selection
}

// Session is the prepared runtime-neutral execution contract. Environment can
// contain credentials. Callers must pass it only through the exec channel and
// must not log it.
type Session struct {
	Name        string
	Title       string
	Command     []string
	Environment []string
	Interactive bool

	timezonePath string
	envPath      string
	markerPath   string
	closed       bool
}

// PrepareSession resolves credentials and creates all per-shell files through
// one policy path for CLI and VM execution.
func PrepareSession(request SessionRequest) (*Session, []string, error) {
	if request.Config == nil {
		return nil, nil, errors.New("session configuration is required")
	}
	for name, value := range map[string]string{
		"Cooper directory": request.CooperDir, "runtime ID": request.RuntimeID,
		"tool": request.ToolName, "workspace": request.WorkspaceDir,
	} {
		if value == "" {
			return nil, nil, fmt.Errorf("session %s is required", name)
		}
	}

	tokens, err := sessionTokens(request)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve tokens: %w", err)
	}
	if err := checkAntigravityAuth(request, tokens); err != nil {
		return nil, nil, err
	}
	session := &Session{
		Name: names.Generate(request.WorkspaceDir), Title: filepath.Base(request.WorkspaceDir) + "-" + request.ToolName,
		Interactive: request.OneShot == "",
	}
	session.Title += "-" + session.Name
	if request.State != nil && request.State.ID != "" {
		session.Title += "-" + request.State.Name
	}
	fail := func(cause error, warnings []string) (*Session, []string, error) {
		return nil, warnings, errors.Join(cause, session.Close())
	}

	timezone, err := runtimefs.PrepareSessionTimezoneFile(request.CooperDir, request.RuntimeID)
	if err != nil {
		return fail(fmt.Errorf("prepare session timezone: %w", err), nil)
	}
	session.timezonePath = timezone.HostPath
	if timezone.ContainerPath != "" {
		session.Environment = append(session.Environment, "TZ=:"+timezone.ContainerPath)
	}

	envFile, warnings, err := barrelenv.PrepareSessionEnvFileForTool(
		request.CooperDir, request.RuntimeID, session.Name,
		request.Config.BarrelEnvVars, request.ToolName,
	)
	if err != nil {
		return fail(fmt.Errorf("prepare session environment: %w", err), warnings)
	}
	session.envPath = envFile.HostPath
	var protectedNames []string
	var unsetNames []string
	if request.State != nil && request.State.ID != "" {
		for _, value := range request.State.Credentials {
			protectedNames = append(protectedNames, value.Name)
			if value.Unset {
				unsetNames = append(unsetNames, value.Name)
			}
		}
	}
	for _, token := range tokens {
		session.Environment = append(session.Environment, token.Name+"="+token.Value)
		protectedNames = append(protectedNames, token.Name)
	}
	target := []string{"bash", "-l"}
	if !session.Interactive {
		target = []string{"bash", "-c", request.OneShot}
	}
	session.Command, err = barrelenv.BuildExecWrapperCommand(
		envFile.ContainerPath,
		barrelenv.ProtectedRuntimeEnvNamesForTool(request.ToolName, protectedNames),
		target,
	)
	if err != nil {
		return fail(fmt.Errorf("build session command: %w", err), warnings)
	}
	if len(unsetNames) > 0 {
		prefix := []string{"env"}
		for _, name := range unsetNames {
			prefix = append(prefix, "-u", name)
		}
		session.Command = append(prefix, session.Command...)
	}
	if session.Interactive {
		marker, err := runtimefs.CreateShellMarker(request.CooperDir, request.RuntimeID)
		if err != nil {
			return fail(err, warnings)
		}
		session.markerPath = marker.HostPath
	}
	return session, warnings, nil
}

func sessionTokens(request SessionRequest) ([]auth.TokenResult, error) {
	if request.State == nil || request.State.ID == "" {
		return auth.ResolveTokens(request.WorkspaceDir, request.CooperDir, []string{request.ToolName})
	}
	tokens := auth.TerminalEnvironment()
	for _, value := range request.State.Credentials {
		if !value.Unset {
			tokens = append(tokens, auth.TokenResult{Name: value.Name, Value: value.Value, Source: "profile"})
		}
	}
	return tokens, nil
}

// Close removes all per-shell files and releases the generated display name.
// It is safe to call more than once.
func (s *Session) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	var failures []error
	if err := runtimefs.RemoveShellMarker(s.markerPath); err != nil {
		failures = append(failures, err)
	}
	if err := barrelenv.RemoveSessionEnvFile(s.envPath); err != nil {
		failures = append(failures, err)
	}
	if err := runtimefs.RemoveSessionTimezoneFile(s.timezonePath); err != nil {
		failures = append(failures, err)
	}
	names.Release(s.Name)
	return errors.Join(failures...)
}
