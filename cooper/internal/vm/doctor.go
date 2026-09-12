package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/rickchristie/govner/cooper/internal/vmproto"
)

// InfrastructureStatus is the local state of one trusted helper image.
type InfrastructureStatus struct {
	Name    string
	ImageID string
	Ready   bool
	Detail  string
}

// InfrastructureStatuses inspects both VM helper images without changing them.
func InfrastructureStatuses(ctx context.Context, prefix string, runner CommandRunner) []InfrastructureStatus {
	result := make([]InfrastructureStatus, 0, 2)
	for _, name := range []string{SupervisorImageName(prefix), RelayImageName(prefix)} {
		status := InfrastructureStatus{Name: name}
		output, err := runnerOrSystem(runner).Output(ctx, "docker", "image", "inspect", "--format",
			`{{.Id}}/{{index .Config.Labels "cooper.vm.image-schema"}}`, name)
		if err != nil {
			status.Detail = "not built"
			result = append(result, status)
			continue
		}
		parts := strings.SplitN(strings.TrimSpace(string(output)), "/", 2)
		if len(parts) != 2 || !strings.HasPrefix(parts[0], "sha256:") || parts[1] != infrastructureImageSchema {
			status.Detail = "image schema is stale"
			result = append(result, status)
			continue
		}
		status.ImageID = parts[0]
		status.Ready = true
		status.Detail = "ready"
		result = append(result, status)
	}
	return result
}

// PreparedStatus reports whether the exact prepared guest contract is ready.
func PreparedStatus(cooperDir string) (bool, string) {
	assetDir := AssetDir(cooperDir)
	if !preparedGuestValid(assetDir) {
		return false, "not prepared"
	}
	data, err := os.ReadFile(PreparedGuestPath(cooperDir) + ".json")
	if err != nil {
		return false, "not prepared"
	}
	var metadata preparedMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return false, "prepared metadata is invalid"
	}
	return true, fmt.Sprintf("ready, schema %d, asset schema %s", metadata.Schema, metadata.AssetSchema)
}

// GuestDiagnostic asks the guest host namespace for its current boundary state.
func (m Manager) GuestDiagnostic(ctx context.Context, runtime Runtime) (vmproto.GuestDiagnostic, error) {
	connection, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "unix", runtime.ControlSocket)
	if err != nil {
		return vmproto.GuestDiagnostic{}, fmt.Errorf("connect to VM %s: %w", runtime.ID, err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(10 * time.Second))
	header := vmproto.NewHeader(vmproto.ServiceDoctor, "doctor")
	if err := vmproto.WriteHeader(connection, header); err != nil {
		return vmproto.GuestDiagnostic{}, err
	}
	var payload []byte
	var guestError string
	for {
		frame, err := vmproto.ReadFrame(connection)
		if err != nil {
			return vmproto.GuestDiagnostic{}, err
		}
		switch frame.Type {
		case vmproto.FrameStdout:
			payload = append(payload, frame.Data...)
		case vmproto.FrameError:
			guestError = string(frame.Data)
		case vmproto.FrameExit:
			status, err := vmproto.DecodeExit(frame.Data)
			if err != nil {
				return vmproto.GuestDiagnostic{}, err
			}
			if status != 0 {
				return vmproto.GuestDiagnostic{}, fmt.Errorf("guest diagnostic failed: %s", guestError)
			}
			var diagnostic vmproto.GuestDiagnostic
			if err := json.Unmarshal(payload, &diagnostic); err != nil {
				return vmproto.GuestDiagnostic{}, fmt.Errorf("decode guest diagnostic: %w", err)
			}
			if err := diagnostic.Validate(); err != nil {
				return vmproto.GuestDiagnostic{}, err
			}
			return diagnostic, nil
		}
	}
}
