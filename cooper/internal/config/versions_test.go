package config

import (
	"fmt"
	"testing"
)

func TestParseVersion(t *testing.T) {
	cases := []struct {
		name    string
		output  string
		want    string
		wantErr bool
	}{
		{name: "go", output: "go version go1.22.5 linux/amd64", want: "1.22.5"},
		{name: "node", output: "v20.11.0\n", want: "20.11.0"},
		{name: "python", output: "Python 3.12.1", want: "3.12.1"},
		{name: "claude", output: "2.1.87 (Claude Code)", want: "2.1.87"},
		{name: "grok stable", output: "grok 1.0.4 (d846eb93d9) [stable]", want: "1.0.4"},
		{name: "grok prerelease", output: "grok 1.0.5-rc.1 (deadbeef) [beta]", want: "1.0.5-rc.1"},
		{name: "leading text", output: "installed version is 1.2.3 extra", want: "1.2.3"},
		{name: "malformed", output: "not a version", wantErr: true},
		{name: "empty", output: "   ", wantErr: true},
		{name: "url rejected as version", output: "see https://example.com/v1/foo", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseVersion(tc.output)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseVersion(%q) = %q, want error", tc.output, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseVersion(%q) error: %v", tc.output, err)
			}
			if got != tc.want {
				t.Fatalf("parseVersion(%q) = %q, want %q", tc.output, got, tc.want)
			}
		})
	}
}

func TestDetectHostVersionUnknown(t *testing.T) {
	if _, err := DetectHostVersion("not-a-tool"); err == nil {
		t.Fatal("expected unknown tool error")
	}
}

func TestHostVersionCommandGrok(t *testing.T) {
	args, ok := hostVersionCommand("grok")
	if !ok {
		t.Fatal("hostVersionCommand(grok) failed")
	}
	if fmt.Sprint(args) != "[grok --version]" {
		t.Fatalf("hostVersionCommand(grok) = %v", args)
	}
	if _, ok := hostVersionCommand("not-a-tool"); ok {
		t.Fatal("unknown tool unexpectedly found")
	}
}
