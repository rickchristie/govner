package testmain

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	_ = m
	fmt.Println("gowt-testmain-marker: setup could not continue")
	os.Exit(7)
}
