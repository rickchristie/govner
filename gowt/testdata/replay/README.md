# Saved event traces

These JSON Lines files reproduce Go event sequences that are difficult to see
in a small unit event. Keep the records in their original order. Build records
can omit `Time`, while package records include it in real `go test -json`
output.

`test-build-failure.jsonl` protects the association between a bracket-qualified
test build ID and its package. It also verifies that load mode shows one package
with the complete compiler diagnostic.
