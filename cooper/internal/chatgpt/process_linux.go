package chatgpt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// CheckHostProcesses blocks VM use while a visible ChatGPT or Codex process
// can write host state. Different conversations still share SQLite databases.
// This check does not stop a host process from starting after it returns.
func CheckHostProcesses(ctx context.Context) error {
	return checkHostProcesses(ctx, "/proc", os.Getpid())
}

func checkHostProcesses(ctx context.Context, directory string, self int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("cannot check host ChatGPT and Codex processes: %w", err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 || pid == self {
			continue
		}
		tool, err := hostProcessTool(filepath.Join(directory, entry.Name()))
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
			continue // A process can exit during the scan.
		}
		if err != nil {
			return fmt.Errorf("cannot check host process %d before starting ChatGPT VM: %w", pid, err)
		}
		if tool != "" {
			return fmt.Errorf("cannot start ChatGPT VM: host %s process %d is running; close all ChatGPT apps and Codex CLI sessions on the host, then retry", tool, pid)
		}
	}
	return nil
}

func hostProcessTool(directory string) (string, error) {
	command, err := os.ReadFile(filepath.Join(directory, "cmdline"))
	if err != nil || len(command) == 0 {
		return "", err // Kernel threads and exited processes have no command.
	}
	args := strings.Split(strings.TrimRight(string(command), "\x00"), "\x00")
	if tool := hostToolName(filepath.Base(args[0])); tool != "" {
		return tool, nil
	}
	name, err := os.ReadFile(filepath.Join(directory, "comm"))
	if err != nil {
		return "", err
	}
	if tool := hostToolName(strings.TrimSpace(string(name))); tool != "" {
		return tool, nil
	}
	// Recognize the installed launch scripts before they start their native
	// child. Do not match a tool name in another program's ordinary arguments.
	if len(args) < 2 {
		return "", nil
	}
	switch filepath.Base(args[0]) {
	case "node", "nodejs":
		if filepath.Base(args[1]) == "codex" {
			return "Codex", nil // npm can pass its bin symlink to Node.
		}
		if filepath.Base(args[1]) == "codex.js" && strings.Contains(filepath.ToSlash(args[1]), "/@openai/codex/") {
			return "Codex", nil
		}
	case "sh", "bash", "dash", "zsh":
		return hostToolName(strings.TrimSuffix(filepath.Base(args[1]), ".sh")), nil
	}
	return "", nil
}

func hostToolName(name string) string {
	switch strings.ToLower(name) {
	case "chatgpt":
		return "ChatGPT"
	case "codex":
		return "Codex"
	default:
		return ""
	}
}
