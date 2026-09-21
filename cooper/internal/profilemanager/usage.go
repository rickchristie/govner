package profilemanager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rickchristie/govner/cooper/internal/profiles"
	"github.com/rickchristie/govner/cooper/internal/workload"
)

type HostUsage struct{}

func (HostUsage) Check(ctx context.Context, paths []string) error {
	if err := profiles.Supported(); err != nil {
		return err
	}
	if len(paths) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resolved := make([]string, 0, len(paths))
	for _, path := range paths {
		canonical, err := workload.ResolvedPath(path)
		if err != nil {
			return err
		}
		resolved = append(resolved, canonical)
	}
	if err := dockerUsage(ctx, resolved); err != nil {
		return err
	}
	return processUsage(ctx, resolved)
}

func dockerUsage(ctx context.Context, paths []string) error {
	data, err := exec.CommandContext(ctx, "docker", "ps", "--quiet", "--no-trunc").Output()
	if err != nil {
		return errors.New("cannot check running Docker mounts; start or connect to the Docker daemon before changing profiles")
	}
	ids := strings.Fields(string(data))
	if len(ids) == 0 {
		return nil
	}
	args := append([]string{"inspect", "--format", "{{json .Mounts}}"}, ids...)
	data, err = exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		return errors.New("running containers changed during the profile use check; retry")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var mounts []struct{ Source string }
		err := decoder.Decode(&mounts)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return errors.New("cannot read Docker mount information")
		}
		for _, mount := range mounts {
			if mount.Source == "" {
				continue
			}
			source, err := workload.ResolvedPath(mount.Source)
			if err != nil {
				return err
			}
			if overlapsAny(source, paths) {
				return &profiles.Issue{Kind: profiles.StateInUse, Message: "a running Docker container or VM uses this state; stop it before changing profiles"}
			}
		}
	}
}

func overlapsAny(path string, roots []string) bool {
	for _, root := range roots {
		if contains(root, path) || contains(path, root) {
			return true
		}
	}
	return false
}

func contains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && (relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))))
}

func harnessName(args []string) string {
	for _, arg := range args[:min(3, len(args))] {
		base := filepath.Base(arg)
		// Gemini CLI and the Antigravity desktop product share .gemini with
		// agy. Their presence must also block replacement of that root.
		if base == "agy" || base == "antigravity" || base == "gemini" || strings.Contains(arg, "/@google/gemini-cli/") {
			return "antigravity"
		}
		for _, name := range []string{"claude", "codex", "copilot", "opencode", "grok"} {
			if base == name || strings.Contains(arg, "/@anthropic-ai/claude-code/") && name == "claude" || strings.Contains(arg, "/@github/copilot/") && name == "copilot" || strings.Contains(arg, "/@openai/codex/") && name == "codex" {
				return name
			}
		}
	}
	return ""
}

func processInUse(pid int, harness string) error {
	return &profiles.Issue{Kind: profiles.StateInUse, Message: fmt.Sprintf("host %s process %d can use this state; exit it before changing profiles", harness, pid)}
}

func processFields(data []byte) map[string]string {
	values := map[string]string{}
	for _, entry := range strings.Split(string(data), "\x00") {
		name, value, found := strings.Cut(entry, "=")
		if found {
			values[name] = value
		}
	}
	return values
}

func ownedProcess(info os.FileInfo) bool { return processOwner(info) == os.Getuid() }

func parsePID(name string) (int, bool) {
	pid, err := strconv.Atoi(name)
	return pid, err == nil && pid > 0 && pid != os.Getpid()
}
