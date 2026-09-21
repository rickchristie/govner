//go:build !linux

package profilemanager

import (
	"context"
	"os"
	"syscall"

	"github.com/rickchristie/govner/cooper/internal/profiles"
)

func processOwner(info os.FileInfo) int { return int(info.Sys().(*syscall.Stat_t).Uid) }

func processUsage(context.Context, []string) error { return profiles.Supported() }
