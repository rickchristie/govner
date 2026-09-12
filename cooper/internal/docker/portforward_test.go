package docker

import (
	"reflect"
	"testing"

	"github.com/rickchristie/govner/cooper/internal/config"
)

func TestPortForwardConfigRoundTrip(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	rules := []config.PortForwardRule{
		{ContainerPort: 8080, HostPort: 18080, Description: "web"},
		{ContainerPort: 9000, HostPort: 19000, Description: "range", IsRange: true, RangeEnd: 9002},
	}
	if err := WritePortForwardConfig(root, 4343, rules); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPortForwardConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.BridgePort != 4343 || !reflect.DeepEqual(loaded.Rules, rules) {
		t.Fatalf("loaded config = %#v, want bridge 4343 and %#v", loaded, rules)
	}
}

func TestForwardPortsExpandsRangesAndRemovesDuplicates(t *testing.T) {
	t.Parallel()
	rules := []config.PortForwardRule{
		{ContainerPort: 9000, IsRange: true, RangeEnd: 9002},
		{ContainerPort: 9001},
		{ContainerPort: 8000},
	}
	want := []int{8000, 9000, 9001, 9002}
	if got := forwardPorts(rules); !reflect.DeepEqual(got, want) {
		t.Fatalf("forwardPorts() = %v, want %v", got, want)
	}
}

func TestRemovedPortsKeepsDesiredPorts(t *testing.T) {
	t.Parallel()
	want := []int{7000, 7002}
	if got := removedPorts([]int{7000, 7001, 7002}, []int{7001, 8000}); !reflect.DeepEqual(got, want) {
		t.Fatalf("removedPorts() = %v, want %v", got, want)
	}
}
