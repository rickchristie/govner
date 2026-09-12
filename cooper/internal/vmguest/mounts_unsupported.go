//go:build !linux

package vmguest

import (
	"errors"
	"io"

	"github.com/rickchristie/govner/cooper/internal/vmproto"
)

// The guest uses Linux mount calls. Keep them out of host CLI builds for
// other systems, which still include the internal VM command entry points.
func mountExports(vmproto.Manifest, io.Writer) error {
	return errors.New("cooper VM guest mounts require Linux")
}
