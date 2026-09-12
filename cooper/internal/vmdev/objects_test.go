package vmdev

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCleanupRejectsReplacementBeforeAnyRemoval(t *testing.T) {
	id := strings.Repeat("a", 64)
	objects := []Object{{"container", "run-proxy", id}, {"network", "run-external", id}}
	for _, scenario := range []string{"replacement", "daemon-lost", "bad-type", "foreign-name"} {
		t.Run(scenario, func(t *testing.T) {
			copy := append([]Object(nil), objects...)
			if scenario == "bad-type" {
				copy[1].Type = "volume"
			}
			if scenario == "foreign-name" {
				copy[1].Name = "cooper-external"
			}
			calls := 0
			err := RemoveObjects(context.Background(), "run", copy, func(_ context.Context, args ...string) ([]byte, error) {
				calls++
				if args[1] != "inspect" {
					t.Fatalf("removed before all checks passed: %v", args)
				}
				if args[0] == "container" {
					return []byte(id), nil
				}
				if scenario == "replacement" {
					return []byte(strings.Repeat("b", 64)), nil
				}
				return []byte("dial unix /var/run/docker.sock: no such file or directory"), errors.New("daemon unavailable")
			})
			if err == nil || calls == 0 {
				t.Fatalf("unsafe cleanup was accepted: %v", err)
			}
		})
	}
}
func TestCleanupUsesRecordedIDsAndRemovesContainersBeforeNetworks(t *testing.T) {
	id := strings.Repeat("c", 64)
	objects := []Object{{"network", "run-net", id}, {"container", "run-proxy", id}}
	var removed []string
	err := RemoveObjects(context.Background(), "run", objects, func(_ context.Context, args ...string) ([]byte, error) {
		if args[1] == "inspect" {
			return []byte(id), nil
		}
		if args[len(args)-1] != id {
			t.Fatalf("cleanup used a name: %v", args)
		}
		removed = append(removed, args[0])
		return nil, nil
	})
	if err != nil || strings.Join(removed, ",") != "container,network" {
		t.Fatalf("cleanup order: %v %v", removed, err)
	}
}
func TestMissingDockerObjectFormats(t *testing.T) {
	for _, text := range []string{"error: no such object: name", "Error: No such container: name", "Error response from daemon: network name not found"} {
		if !ObjectMissing([]byte(text)) {
			t.Fatalf("missing object was not recognized: %s", text)
		}
	}
	if ObjectMissing([]byte("dial unix /var/run/docker.sock: no such file or directory")) {
		t.Fatal("lost daemon was treated as successful cleanup")
	}
}
