package buildflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/rickchristie/govner/cooper/internal/profilemanager"
	"github.com/rickchristie/govner/cooper/internal/profiles"
)

const profileStepName = "Setting up live profiles..."

func (p *Prepared) prepareProfiles(opts Options, home string) error {
	emitOutput(opts, profileStepName)
	if runtime.GOOS != "linux" {
		emitOutput(opts, "Live directory profiles require Linux; macOS keeps its existing profile storage.")
		return nil
	}
	workspace, err := os.Getwd()
	if err != nil {
		return err
	}
	result, err := profilemanager.PrepareBuild(context.Background(), p.cooperDir, workspace, home)
	if err != nil {
		var issue *profiles.Issue
		if errors.As(err, &issue) && issue.Kind == profiles.StateConflict {
			return fmt.Errorf("set up live profiles: %w; run cooper profiles migrate with --conflict host or --conflict saved and the same --config path, then retry this build", err)
		}
		return fmt.Errorf("set up live profiles: %w", err)
	}
	if result.Unchanged {
		emitOutput(opts, "Live profile storage is ready.")
		return nil
	}
	emitOutput(opts, "Live profile storage is ready. Save checks the account; use cooper profiles backup for an independent copy.")
	if result.Recovery != "" {
		emitOutput(opts, "Converted saved profiles. Original state is retained. Recovery record: "+result.Recovery)
	}
	return nil
}
