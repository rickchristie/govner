//go:build !linux

package chatgpt

import (
	"context"
	"errors"
)

func CheckHostProcesses(context.Context) error {
	return errors.New("ChatGPT VM host process checks require Linux")
}
