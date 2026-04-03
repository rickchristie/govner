// Package x11src embeds the cooper-x11-bridge source so it can be written
// into the base Docker build context. This ensures the base image compiles
// the exact same tested code — no duplication, no drift.
package x11src

import (
	_ "embed"
)

// MainGo is the x11-bridge entry point (cmd/cooper-x11-bridge/main.go).
//
//go:embed main.go.src
var MainGo []byte
