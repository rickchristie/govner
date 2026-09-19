package profilemanager

import (
	"context"
	"fmt"

	"github.com/rickchristie/govner/cooper/internal/profiles"
)

// PrepareBuild makes live storage part of every Linux build. An inner build
// can create an empty local catalog, but it cannot convert existing host
// profiles with only a partial view of host mounts and running processes.
func PrepareBuild(ctx context.Context, cooperDir, workspace, home string) (profiles.Result, error) {
	guard := profiles.GuardFunc(func(ctx context.Context, paths []string) error {
		if err := CheckHost(); err != nil {
			return fmt.Errorf("run cooper build on the physical host to convert existing profiles: %w", err)
		}
		return (HostUsage{}).Check(ctx, paths)
	})
	service, err := newService(cooperDir, workspace, home, guard)
	if err != nil {
		return profiles.Result{}, err
	}
	return service.Migrate(ctx, "")
}
