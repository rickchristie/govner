package antigravity

import "strings"

// The native executable has no Go module metadata. Its compiled browser cache
// path names the required Playwright driver. Read that dependency from the
// selected binary so a harness upgrade does not need a Cooper version entry.
const driverVersionScript = `#!/bin/sh
set -eu
if [ "$#" -ne 1 ]; then
    echo 'Usage: agy-driver-version <native-executable>' >&2
    exit 2
fi
paths=$(LC_ALL=C grep -aoE '\.cache/ms-playwright-go/[0-9]+\.[0-9]+\.[0-9]+' -- "$1" | sort -u)
if [ -z "$paths" ] || [ "$(printf '%s\n' "$paths" | wc -l)" -ne 1 ]; then
    echo 'Cannot identify one Playwright driver version in the selected Antigravity executable.' >&2
    exit 1
fi
printf '%s\n' "${paths##*/}"
`

// DriverVersionCommand installs the same dependency reader used by image
// builds and their offline checks. It never starts the native agent.
func DriverVersionCommand(target string) string {
	return shellScriptCommand(driverVersionScript, target)
}

func shellScriptCommand(script, target string) string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSuffix(script, "\n"), "\n") {
		lines = append(lines, shellQuote(line))
	}
	return "printf '%s\\n' " + strings.Join(lines, " ") + " > " + shellQuote(target) +
		" && chmod 0755 " + shellQuote(target)
}
