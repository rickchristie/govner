// Package testdriver provides a reusable runtime driver for Cooper.
//
// This package is intentionally below the TUI layer. It starts a real
// CooperApp, prepares a temporary Cooper config directory, drives Docker
// barrel lifecycle, talks to the live bridge HTTP endpoints, and verifies
// persisted state. It is the foundation for end-to-end runtime checks.
//
// This is not the same thing as `cooper tui-test`, which renders the TUI with
// fake data for visual QA. A future TUI driver can sit on top of this runtime
// driver to exercise `cooper up` and terminal interaction without re-creating
// the infrastructure setup.
package testdriver
