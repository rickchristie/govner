package vm

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

type ownershipRunner struct {
	output []byte
	err    error
}

func (r ownershipRunner) Run(context.Context, io.Reader, io.Writer, io.Writer, string, ...string) error {
	return errors.New("unexpected run")
}

func (r ownershipRunner) Output(context.Context, string, ...string) ([]byte, error) {
	return r.output, r.err
}

func TestInspectOwnedDockerObjectsRequiresExactLabels(t *testing.T) {
	t.Parallel()
	id := strings.Repeat("a", 64)
	container := ownershipRunner{output: []byte(id + "\tvm-supervisor\tunit-vm-project-codex-aabbccddeeff\n")}
	got, exists, err := inspectOwnedContainer(context.Background(), container, "name", "vm-supervisor", "unit-vm-project-codex-aabbccddeeff")
	if err != nil || !exists || got != id {
		t.Fatalf("inspectOwnedContainer() = %q, %t, %v", got, exists, err)
	}
	foreign := ownershipRunner{output: []byte(id + "\tcli\tunit-vm-project-codex-aabbccddeeff\n")}
	if _, _, err := inspectOwnedContainer(context.Background(), foreign, "name", "vm-supervisor", "unit-vm-project-codex-aabbccddeeff"); err == nil || !strings.Contains(err.Error(), "refuse") {
		t.Fatalf("foreign container error = %v", err)
	}
	missing := ownershipRunner{err: errors.New("No such container")}
	if got, exists, err := inspectOwnedContainer(context.Background(), missing, "name", "vm-supervisor", "runtime"); err != nil || exists || got != "" {
		t.Fatalf("missing container = %q, %t, %v", got, exists, err)
	}
}

func TestInspectOwnedNetworkRejectsInvalidObjectID(t *testing.T) {
	t.Parallel()
	runner := ownershipRunner{output: []byte("not-an-id\tvm-relay-network\truntime\n")}
	if _, _, err := inspectOwnedNetwork(context.Background(), runner, "network", "vm-relay-network", "runtime"); err == nil {
		t.Fatal("inspectOwnedNetwork accepted an invalid Docker ID")
	}
}
