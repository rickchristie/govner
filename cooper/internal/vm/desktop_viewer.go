package vm

import (
	"context"
	"errors"
	"net"
	"os"

	"github.com/rickchristie/govner/cooper/internal/aitool"
	"github.com/rickchristie/govner/cooper/internal/desktop"
)

// OpenDesktopViewer uses the same private viewer for each launch of this VM.
func OpenDesktopViewer(ctx context.Context, runtime Runtime, executable, cooperDir string) (string, error) {
	if !aitool.IsDesktop(runtime.ToolName) {
		return "", errors.New("this VM does not have a desktop")
	}
	return desktop.OpenViewer(ctx, runtime.RuntimeDir, executable, []string{"--config", cooperDir, "__desktop-viewer", "vm", runtime.ID})
}

// ServeDesktopViewer reads validated host metadata rather than accepting a
// guest-selected socket or destination. Replacing the socket ends its life.
func ServeDesktopViewer(ctx context.Context, cooperDir, runtimeID string) error {
	if runtimeID == "" || cleanNamePart(runtimeID) != runtimeID {
		return errors.New("desktop runtime ID is invalid")
	}
	metadata, err := loadRuntimeMetadata(cooperDir, runtimeID)
	if err != nil {
		return err
	}
	if !aitool.IsDesktop(metadata.ToolName) {
		return errors.New("this VM does not have a desktop")
	}
	runtime := runtimeFor(cooperDir, metadata.startRequest(), metadata.Depth)
	socket, err := os.Stat(runtime.ControlSocket)
	if err != nil {
		return err
	}
	return desktop.ServeViewer(ctx, runtime.RuntimeDir, func(ctx context.Context) (net.Conn, error) {
		return DialDesktop(ctx, runtime)
	}, func() bool {
		current, err := os.Stat(runtime.ControlSocket)
		return err == nil && os.SameFile(socket, current)
	})
}
