package vmguest

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rickchristie/govner/cooper/internal/vmproto"
)

func mountExports(manifest vmproto.Manifest, log io.Writer) error {
	for _, mount := range manifest.Mounts {
		fmt.Fprintf(log, "Cooper VM guest: mounting export %s\n", mount.ID)
		result := make(chan error, 1)
		go func() { result <- mountExport(mount) }()
		select {
		case err := <-result:
			if err != nil {
				return fmt.Errorf("mount export %s: %w", mount.ID, err)
			}
		case <-time.After(15 * time.Second):
			return fmt.Errorf("mount export %s: operation timed out", mount.ID)
		}
		fmt.Fprintf(log, "Cooper VM guest: mounted export %s\n", mount.ID)
	}
	return nil
}

func mountExport(mount vmproto.GuestMount) error {
	if mount.Kind == "directory" {
		if err := os.MkdirAll(mount.Target, 0o755); err != nil {
			return fmt.Errorf("create target: %w", err)
		}
		if err := syscall.Mount(mount.Tag, mount.Target, "virtiofs", 0, ""); err != nil {
			return fmt.Errorf("mount virtiofs tag %s: %w", mount.Tag, err)
		}
		if mount.ReadOnly {
			if err := syscall.Mount("", mount.Target, "", syscall.MS_REMOUNT|syscall.MS_RDONLY, ""); err != nil {
				return fmt.Errorf("make target read-only: %w", err)
			}
		}
		return nil
	}

	// virtiofs exports directories. A file export therefore contains one
	// host-bound entry. Mount the private wrapper first, and then bind only that
	// entry to the authorized guest target.
	wrapper := filepath.Join("/run/cooper/mounts", mount.Tag)
	if err := os.MkdirAll(wrapper, 0o700); err != nil {
		return fmt.Errorf("create file export wrapper: %w", err)
	}
	if err := syscall.Mount(mount.Tag, wrapper, "virtiofs", 0, ""); err != nil {
		return fmt.Errorf("mount file export tag %s: %w", mount.Tag, err)
	}
	if err := os.MkdirAll(filepath.Dir(mount.Target), 0o755); err != nil {
		return fmt.Errorf("create target parent: %w", err)
	}
	file, err := os.OpenFile(mount.Target, os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("create target file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close target file: %w", err)
	}
	source := filepath.Join(wrapper, mount.Entry)
	if err := syscall.Mount(source, mount.Target, "", syscall.MS_BIND, ""); err != nil {
		return fmt.Errorf("bind file export: %w", err)
	}
	if mount.ReadOnly {
		if err := syscall.Mount("", mount.Target, "", syscall.MS_BIND|syscall.MS_REMOUNT|syscall.MS_RDONLY, ""); err != nil {
			return fmt.Errorf("make target read-only: %w", err)
		}
	}
	return nil
}
