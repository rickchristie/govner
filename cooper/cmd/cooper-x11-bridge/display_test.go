package main

import "testing"

func TestParseDisplayNormalizesHostsForJoinHostPort(t *testing.T) {
	tests := []struct {
		display  string
		wantHost string
		wantNum  int
	}{
		{display: ":99", wantHost: "127.0.0.1", wantNum: 99},
		{display: "127.0.0.1:1", wantHost: "127.0.0.1", wantNum: 1},
		{display: "[::1]:2", wantHost: "::1", wantNum: 2},
		{display: "::1:3", wantHost: "::1", wantNum: 3},
	}
	for _, tt := range tests {
		t.Run(tt.display, func(t *testing.T) {
			host, num, err := parseDisplay(tt.display)
			if err != nil {
				t.Fatalf("parseDisplay(%q) failed: %v", tt.display, err)
			}
			if host != tt.wantHost || num != tt.wantNum {
				t.Fatalf("parseDisplay(%q) = (%q, %d), want (%q, %d)", tt.display, host, num, tt.wantHost, tt.wantNum)
			}
		})
	}
}
