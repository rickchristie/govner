package vm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/vmproto"
	"github.com/rickchristie/govner/cooper/internal/vmrelay"
)

type reloadTarget struct {
	runtime Runtime
	path    string
	old     vmrelay.Policy
}

// ReloadPortForwards applies one complete forward-port set to every running
// VM. It restores all changed VMs if one guest rejects the update.
func (m Manager) ReloadPortForwards(ctx context.Context, rules []config.PortForwardRule) error {
	ports := expandedContainerPorts(rules)
	if len(ports) > vmproto.MaxForwardPorts {
		return fmt.Errorf("VM port policy has %d ports; maximum is %d", len(ports), vmproto.MaxForwardPorts)
	}
	ids, err := m.runtimeIDs(ctx)
	if err != nil {
		return err
	}
	targets := make([]reloadTarget, 0, len(ids))
	for _, id := range ids {
		running, err := containerRunning(ctx, m.Runner, id)
		if err != nil {
			return fmt.Errorf("inspect VM %s before port reload: %w", id, err)
		}
		if !running {
			continue
		}
		runtimeDir := RuntimeDir(m.CooperDir, id)
		controlDir := ControlDir(m.CooperDir, id)
		path := relayPolicyPath(runtimeDir)
		policy, err := vmrelay.LoadPolicy(path)
		if err != nil {
			return fmt.Errorf("load VM %s relay policy: %w", id, err)
		}
		targets = append(targets, reloadTarget{
			runtime: Runtime{ID: id, ContainerName: id, RuntimeDir: runtimeDir, ControlDir: controlDir, ControlSocket: ControlSocketPath(m.CooperDir, id)},
			path:    path,
			old:     policy,
		})
	}

	changed := make([]reloadTarget, 0, len(targets))
	for _, target := range targets {
		policy := target.old
		policy.Forwards = append([]int(nil), ports...)
		policy.ForwardPorts = nil
		if err := writeJSON(target.path, policy, 0o444); err != nil {
			return m.rollbackPortForwards(ctx, changed, fmt.Errorf("write VM %s relay policy: %w", target.runtime.ID, err))
		}
		changed = append(changed, target)
		if err := m.reloadRuntimePorts(ctx, target.runtime, ports); err != nil {
			return m.rollbackPortForwards(ctx, changed, fmt.Errorf("reload VM %s ports: %w", target.runtime.ID, err))
		}
	}
	return nil
}

func (m Manager) rollbackPortForwards(ctx context.Context, changed []reloadTarget, cause error) error {
	var failures []string
	for index := len(changed) - 1; index >= 0; index-- {
		target := changed[index]
		if err := writeJSON(target.path, target.old, 0o444); err != nil {
			failures = append(failures, fmt.Sprintf("restore %s policy: %v", target.runtime.ID, err))
			continue
		}
		if err := m.reloadRuntimePorts(ctx, target.runtime, target.old.Forwards); err != nil {
			failures = append(failures, fmt.Sprintf("restore %s guest ports: %v", target.runtime.ID, err))
		}
	}
	if len(failures) == 0 {
		return cause
	}
	return fmt.Errorf("%w; rollback failed: %s", cause, strings.Join(failures, "; "))
}

func (m Manager) reloadRuntimePorts(ctx context.Context, runtime Runtime, ports []int) error {
	connection, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", runtime.ControlSocket)
	if err != nil {
		return err
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(10 * time.Second))
	header := vmproto.NewHeader(vmproto.ServiceReload, "reload-ports")
	header.ForwardPorts = append([]int(nil), ports...)
	if err := vmproto.WriteHeader(connection, header); err != nil {
		return err
	}
	var guestError string
	for {
		frame, err := vmproto.ReadFrame(connection)
		if err != nil {
			return err
		}
		switch frame.Type {
		case vmproto.FrameError:
			guestError = string(frame.Data)
		case vmproto.FrameExit:
			status, err := vmproto.DecodeExit(frame.Data)
			if err != nil {
				return err
			}
			if status == 0 {
				return nil
			}
			if guestError == "" {
				guestError = fmt.Sprintf("guest returned status %d", status)
			}
			return errors.New(guestError)
		}
	}
}
