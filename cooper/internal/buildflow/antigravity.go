package buildflow

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/rickchristie/govner/cooper/internal/antigravity"
	"github.com/rickchristie/govner/cooper/internal/vmcontext"
)

func (p *Prepared) prepareHostAuth(opts Options, home string) error {
	enabled := false
	for _, tool := range p.plan.enabledAITools {
		if tool == "antigravity" {
			enabled = true
			break
		}
	}
	if !enabled {
		return nil
	}
	if runtime.GOOS != "linux" {
		emitOutput(opts, "Antigravity host file-auth setup is supported on Linux only; host Keychain login is not shared.")
		return nil
	}
	outer, err := vmcontext.Load()
	if err != nil {
		return err
	}
	if outer != nil || os.Getenv("COOPER_CLI_TOOL") != "" {
		emitOutput(opts, "Run cooper build on the physical host to set up agy file authentication.")
		return nil
	}
	if _, err := exec.LookPath("agy"); err != nil {
		emitOutput(opts, "Host agy is not installed. Install it, then run cooper build to set up file authentication.")
		return nil
	}
	setup, err := antigravity.InstallHostFileAuth(context.Background(), home, os.Getenv("ZDOTDIR"))
	if err != nil {
		return fmt.Errorf("set up host Antigravity file authentication: %w", err)
	}
	emitOutput(opts, "Installed the host agy file-auth wrapper for Bash and Zsh.")
	if !antigravity.HostFileAuthActive(home) {
		// Build cannot change its parent shell. The printed source command also
		// clears a previously cached command, alias, or function in that shell.
		quoted := "'" + strings.ReplaceAll(setup.Shell, "'", "'\"'\"'") + "'"
		emitOutput(opts, "Activate it in this shell: . "+quoted)
	}
	emitOutput(opts, "Sign in with agy on the host, then use cooper save/load for account changes.")
	return nil
}
