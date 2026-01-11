package main

import "strings"

// ParsedArgs separates command-line arguments into patterns and flags
type ParsedArgs struct {
	Patterns   []string // Package patterns (e.g., "./...", "./pkg/...")
	BuildFlags []string // Flags that affect build (e.g., -race, -cover)
	TestFlags  []string // Flags that affect test execution (e.g., -v, -run)
}

// buildFlagSet contains flags that affect the build phase
// These are passed to `go test -c`
var buildFlagSet = map[string]bool{
	"-race":         true,
	"-cover":        true,
	"-covermode":    true,
	"-coverpkg":     true,
	"-tags":         true,
	"-ldflags":      true,
	"-mod":          true,
	"-modfile":      true,
	"-trimpath":     true,
	"-gcflags":      true,
	"-asmflags":     true,
	"-buildvcs":     true,
	"-compiler":     true,
	"-gccgoflags":   true,
	"-installsuffix": true,
	"-linkshared":   true,
	"-msan":         true,
	"-asan":         true,
	"-pkgdir":       true,
	"-pgo":          true,
	"-toolexec":     true,
}

// buildFlagsWithValues contains flags that take a value argument
var buildFlagsWithValues = map[string]bool{
	"-covermode":    true,
	"-coverpkg":     true,
	"-tags":         true,
	"-ldflags":      true,
	"-mod":          true,
	"-modfile":      true,
	"-gcflags":      true,
	"-asmflags":     true,
	"-compiler":     true,
	"-gccgoflags":   true,
	"-installsuffix": true,
	"-pkgdir":       true,
	"-pgo":          true,
	"-toolexec":     true,
}

// testFlagSet contains flags that affect test execution
// These are passed to the test binary as -test.* flags
var testFlagSet = map[string]bool{
	"-v":            true,
	"-count":        true,
	"-run":          true,
	"-timeout":      true,
	"-parallel":     true,
	"-short":        true,
	"-bench":        true,
	"-benchtime":    true,
	"-benchmem":     true,
	"-blockprofile": true,
	"-coverprofile": true,
	"-cpuprofile":   true,
	"-memprofile":   true,
	"-mutexprofile": true,
	"-trace":        true,
	"-failfast":     true,
	"-list":         true,
	"-shuffle":      true,
}

// testFlagsWithValues contains test flags that take a value argument
var testFlagsWithValues = map[string]bool{
	"-count":        true,
	"-run":          true,
	"-timeout":      true,
	"-parallel":     true,
	"-bench":        true,
	"-benchtime":    true,
	"-blockprofile": true,
	"-coverprofile": true,
	"-cpuprofile":   true,
	"-memprofile":   true,
	"-mutexprofile": true,
	"-trace":        true,
	"-list":         true,
	"-shuffle":      true,
}

// ParseArgs separates arguments into patterns, build flags, and test flags
func ParseArgs(args []string) ParsedArgs {
	result := ParsedArgs{
		Patterns:   make([]string, 0),
		BuildFlags: make([]string, 0),
		TestFlags:  make([]string, 0),
	}

	i := 0
	for i < len(args) {
		arg := args[i]

		// Check if it's a flag
		if strings.HasPrefix(arg, "-") {
			// Handle -flag=value format
			flagName := arg
			flagValue := ""
			if idx := strings.Index(arg, "="); idx != -1 {
				flagName = arg[:idx]
				flagValue = arg[idx+1:]
			}

			if buildFlagSet[flagName] {
				// Build flag
				if flagValue != "" {
					// -flag=value format
					result.BuildFlags = append(result.BuildFlags, arg)
				} else if buildFlagsWithValues[flagName] && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
					// -flag value format (two args)
					result.BuildFlags = append(result.BuildFlags, arg, args[i+1])
					i++
				} else {
					// Boolean flag
					result.BuildFlags = append(result.BuildFlags, arg)
				}
			} else if testFlagSet[flagName] {
				// Test flag
				if flagValue != "" {
					// -flag=value format
					result.TestFlags = append(result.TestFlags, arg)
				} else if testFlagsWithValues[flagName] && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
					// -flag value format (two args)
					result.TestFlags = append(result.TestFlags, arg, args[i+1])
					i++
				} else {
					// Boolean flag
					result.TestFlags = append(result.TestFlags, arg)
				}
			} else {
				// Unknown flag - assume it's a test flag (go test passes through unknown flags)
				if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
					// Might have a value
					result.TestFlags = append(result.TestFlags, arg, args[i+1])
					i++
				} else {
					result.TestFlags = append(result.TestFlags, arg)
				}
			}
		} else {
			// Not a flag - it's a pattern
			result.Patterns = append(result.Patterns, arg)
		}

		i++
	}

	// Default pattern if none specified (matches go test behavior: current package only)
	if len(result.Patterns) == 0 {
		result.Patterns = []string{"."}
	}

	return result
}

// ConvertToTestFlags converts parsed test flags to -test.* format for the binary
func ConvertToTestFlags(flags []string) []string {
	result := make([]string, 0, len(flags))

	for i := 0; i < len(flags); i++ {
		flag := flags[i]

		if !strings.HasPrefix(flag, "-") {
			// Not a flag, skip (shouldn't happen but be safe)
			result = append(result, flag)
			continue
		}

		// Handle -flag=value format
		flagName := flag
		flagValue := ""
		if idx := strings.Index(flag, "="); idx != -1 {
			flagName = flag[:idx]
			flagValue = flag[idx+1:]
		}

		// Convert -v to -test.v, -run to -test.run, etc.
		testFlag := "-test." + strings.TrimPrefix(flagName, "-")

		if flagValue != "" {
			result = append(result, testFlag+"="+flagValue)
		} else if testFlagsWithValues[flagName] && i+1 < len(flags) && !strings.HasPrefix(flags[i+1], "-") {
			// Two-arg format: -run TestFoo -> -test.run TestFoo
			result = append(result, testFlag, flags[i+1])
			i++
		} else {
			result = append(result, testFlag)
		}
	}

	return result
}
