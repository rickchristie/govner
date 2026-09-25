package vm

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/rickchristie/govner/cooper/internal/aitool"
	"github.com/rickchristie/govner/cooper/internal/launch"
	"github.com/rickchristie/govner/cooper/internal/profilemanager"
	"github.com/rickchristie/govner/cooper/internal/vmproto"
)

// StartDesktop applies the same credentials and session environment used by
// the launch command. Restart callers must not keep a saved copy of secrets.
func (m Manager) StartDesktop(ctx context.Context, runtime Runtime) error {
	metadata, err := loadRuntimeMetadata(m.CooperDir, runtime.ID)
	if err != nil {
		return err
	}
	selection, err := profilemanager.SelectID(ctx, m.CooperDir, runtime.WorkspaceDir, m.HomeDir, runtime.ToolName, metadata.ProfileID)
	if err != nil {
		return err
	}
	session, warnings, err := launch.PrepareSession(launch.SessionRequest{
		Config: m.Config, CooperDir: m.CooperDir, RuntimeID: runtime.ID,
		ToolName: runtime.ToolName, WorkspaceDir: runtime.WorkspaceDir,
		OneShot: "cooper-desktop-start", State: &selection,
	})
	if err != nil {
		return err
	}
	defer session.Close()
	for _, warning := range warnings {
		fmt.Fprintln(m.output(), warning)
	}
	return m.ExecCommand(ctx, runtime, session.Command, session.Environment, false, nil, m.output(), m.output())
}

// DialDesktop opens one HTTP or WebSocket connection to the fixed guest
// desktop service. The caller never supplies a guest destination.
func DialDesktop(ctx context.Context, runtime Runtime) (net.Conn, error) {
	if !aitool.IsDesktop(runtime.ToolName) {
		return nil, fmt.Errorf("VM %s is not a desktop workload", runtime.ID)
	}
	connection, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "unix", runtime.ControlSocket)
	if err != nil {
		return nil, fmt.Errorf("connect desktop control channel: %w", err)
	}
	stop := context.AfterFunc(ctx, func() { connection.Close() })
	defer stop()
	_ = connection.SetDeadline(time.Now().Add(10 * time.Second))
	requestID, err := randomHex(12)
	if err == nil {
		err = vmproto.WriteHeader(connection, vmproto.NewHeader(vmproto.ServiceDesktop, requestID))
	}
	if err != nil {
		connection.Close()
		return nil, err
	}
	frame, err := vmproto.ReadFrame(connection)
	if err != nil {
		connection.Close()
		return nil, err
	}
	if frame.Type != vmproto.FrameStdout || string(frame.Data) != "ready" {
		connection.Close()
		return nil, fmt.Errorf("guest desktop is unavailable: %s", frame.Data)
	}
	_ = connection.SetDeadline(time.Time{})
	return connection, nil
}
