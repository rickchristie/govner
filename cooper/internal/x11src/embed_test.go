package x11src

import (
	"os"
	"testing"
)

func TestEmbeddedMainMatchesSource(t *testing.T) {
	actual, err := os.ReadFile("../../cmd/cooper-x11-bridge/main.go")
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	if string(MainGo) != string(actual) {
		t.Fatal("embedded main.go.src differs from cmd/cooper-x11-bridge/main.go\n" +
			"Run: cp cmd/cooper-x11-bridge/main.go internal/x11src/main.go.src")
	}
}
