package vmdev

import (
	"context"
	"fmt"
	"strings"
)

type Object struct{ Type, Name, ID string }
type DockerOutput func(context.Context, ...string) ([]byte, error)

// ObjectMissing recognizes Docker's object errors, not a lost daemon socket.
// A failed connection to Docker must never be treated as completed cleanup.
func ObjectMissing(output []byte) bool {
	text := strings.ToLower(string(output))
	for _, kind := range []string{"object", "container", "network"} {
		if strings.Contains(text, "no such "+kind+":") {
			return true
		}
	}
	return strings.Contains(text, "error response from daemon: network ") && strings.Contains(text, " not found")
}

// CheckObjects finishes every ownership check before a caller removes anything.
// The run record supplies exact IDs; a name prefix alone does not prove ownership.
func CheckObjects(ctx context.Context, namespace string, objects []Object, output DockerOutput) error {
	for _, object := range objects {
		if (object.Type != "container" && object.Type != "network") || !strings.HasPrefix(object.Name, namespace+"-") || len(object.ID) != 64 || strings.Trim(object.ID, "0123456789abcdef") != "" {
			return fmt.Errorf("invalid recorded runtime object")
		}
		data, err := output(ctx, object.Type, "inspect", "--format", "{{.Id}}", object.Name)
		if err != nil {
			if ObjectMissing(data) {
				continue
			}
			return fmt.Errorf("inspect cleanup object: %w: %s", err, data)
		}
		if strings.TrimSpace(string(data)) != object.ID {
			return fmt.Errorf("refuse cleanup of replaced %s %s", object.Type, object.Name)
		}
	}
	return nil
}

func RemoveObjects(ctx context.Context, namespace string, objects []Object, output DockerOutput) error {
	if err := CheckObjects(ctx, namespace, objects, output); err != nil {
		return err
	}
	for _, kind := range []string{"container", "network"} {
		for _, object := range objects {
			if object.Type != kind {
				continue
			}
			args := []string{kind, "rm"}
			if kind == "container" {
				args = append(args, "-f")
			}
			args = append(args, object.ID)
			data, err := output(ctx, args...)
			if err != nil && !ObjectMissing(data) {
				return fmt.Errorf("remove owned object: %w: %s", err, data)
			}
		}
	}
	return nil
}
