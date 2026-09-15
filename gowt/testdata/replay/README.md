# Saved event traces

These JSON Lines files reproduce Go event sequences that are difficult to see
in a small unit event. Keep the records in their original order. Build records
can omit `Time`, while package records include it in real `go test -json`
output.

`test-build-failure.jsonl` protects the association between a bracket-qualified
test build ID and its package. It also verifies that load mode shows one package
with the complete compiler diagnostic.

`slash-name-package-failure.jsonl` is a reduced, anonymized trace from a
`go test -parallel=6 -json` run. It keeps two test branches and their original
event order, including pause and continue events. All package and test names
use fixed fixture values. All application output is replaced with `fixture log`.
Go markers use the new names. Times and durations use fixed values.

The trace has 24 named tests: 21 pass and 3 fail. The failed records belong to
one leaf test and its two parent tests. `TestAccess/#00/accepts_read` is only a
group created by a slash in a case name. It has no event of its own and must
stay passed when the package fails. It must not add to the test counts or
appear in Focus mode. The load and live replay tests check these results.

Keep the private source trace outside testdata. This small fixture retains the
failure condition without the source project's paths, names, or log data.
