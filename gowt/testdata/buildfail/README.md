# Build-failure fixtures

This nested module contains invalid packages by design. The Gowt integration
suite runs each package with the real `go test -json` command and verifies that
all diagnostics reach the model and log view.

The packages cover these Go failure producers:

- source and test compilation
- syntax checks
- package loading and import cycles
- module package resolution
- vet
- the Go assembler
- the Go linker
- `TestMain`, which can fail outside a named test

The `passlogs` package is valid. Its passing test writes error-level JSON to
both stdout and stderr. This protects the rule that log severity does not set
test status.

The assembler and linker fixtures use amd64 assembly. Their integration cases
are skipped on other architectures. The parent Gowt module does not discover
files below `testdata`, and this nested `go.mod` prevents invalid fixture code
from changing the normal Gowt build.
