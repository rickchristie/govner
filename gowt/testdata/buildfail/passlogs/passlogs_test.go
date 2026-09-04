package passlogs

import (
	"fmt"
	"os"
	"testing"
)

func TestExpectedErrorLogs(t *testing.T) {
	fmt.Fprintln(os.Stdout, `{"level":"error","message":"expected stdout service failure"}`)
	fmt.Fprintln(os.Stderr, `{"level":"error","message":"expected stderr service failure"}`)
}
