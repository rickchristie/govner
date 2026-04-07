package docker

import (
	"reflect"
	"testing"
)

func TestDockerStopArgs_DefaultTimeout(t *testing.T) {
	got := dockerStopArgs("cooper-proxy", -1)
	want := []string{"stop", "cooper-proxy"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dockerStopArgs() = %v, want %v", got, want)
	}
}

func TestDockerStopArgs_ExplicitTimeout(t *testing.T) {
	got := dockerStopArgs("barrel-demo", 1)
	want := []string{"stop", "-t", "1", "barrel-demo"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dockerStopArgs() = %v, want %v", got, want)
	}
}
