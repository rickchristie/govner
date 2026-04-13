//go:build darwin

package alertsound

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type backend interface {
	Play(phrase) error
	Close() error
}

var execCommand = exec.Command

type darwinBackend struct{}

func newBackend() (backend, error) {
	return &darwinBackend{}, nil
}

func (b *darwinBackend) Play(p phrase) error {
	path := filepath.Join(os.TempDir(), fmt.Sprintf("cooper-alert-%d.wav", time.Now().UnixNano()))
	if err := os.WriteFile(path, p.WAV, 0644); err != nil {
		return err
	}
	defer os.Remove(path)

	cmd := execCommand("afplay", path)
	return cmd.Run()
}

func (b *darwinBackend) Close() error { return nil }
