// Package aclsrc embeds the ACL helper source files so they can be written
// into the proxy Docker build context. This ensures the proxy image compiles
// the exact same tested code — no duplication, no drift.
package aclsrc

import (
	_ "embed"
)

// MainGo is the ACL helper entry point (cmd/acl-helper/main.go).
//
//go:embed main.go.src
var MainGo []byte

// HelperGo is the ACL helper logic (internal/proxy/helper.go).
//
//go:embed helper.go.src
var HelperGo []byte
