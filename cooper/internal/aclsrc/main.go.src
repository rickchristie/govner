package main

import (
	"fmt"
	"os"

	"github.com/rickchristie/govner/cooper/internal/proxy"
)

func main() {
	// First check CLI args for --socket flag (squid passes this as a CLI arg).
	socketPath := ""
	for i, arg := range os.Args[1:] {
		if arg == "--socket" && i+1 < len(os.Args[1:]) {
			socketPath = os.Args[i+2] // +2 because os.Args[1:] shifts by 1
			break
		}
	}

	// Fall back to environment variable.
	if socketPath == "" {
		socketPath = os.Getenv("COOPER_ACL_SOCKET")
	}

	if socketPath == "" {
		fmt.Fprintln(os.Stderr, "socket path not provided via --socket flag or COOPER_ACL_SOCKET env var")
		// Fail closed: if we can't find the socket, every request is denied.
		// Write ERR for any input that arrives, then exit.
		proxy.RunHelper("", os.Stdin, os.Stdout)
		os.Exit(1)
	}

	proxy.RunHelper(socketPath, os.Stdin, os.Stdout)
}
