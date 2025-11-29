package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatDuration_Seconds(t *testing.T) {
	tests := []struct {
		name     string
		seconds  float64
		expected string
	}{
		{"zero", 0, "0.0s"},
		{"sub-second", 0.5, "0.5s"},
		{"one second", 1.0, "1.0s"},
		{"whole seconds", 5.0, "5.0s"},
		{"fractional seconds", 5.5, "5.5s"},
		{"rounds to one decimal", 5.55, "5.6s"},
		{"rounds up", 5.95, "6.0s"},
		{"just under a minute", 59.9, "59.9s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatDuration(tt.seconds)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatDuration_Minutes(t *testing.T) {
	tests := []struct {
		name     string
		seconds  float64
		expected string
	}{
		{"exactly one minute", 60, "1m0.0s"},
		{"one minute one second", 61, "1m1.0s"},
		{"one minute fractional", 61.5, "1m1.5s"},
		{"multiple minutes", 125, "2m5.0s"},
		{"multiple minutes fractional", 125.7, "2m5.7s"},
		{"just under an hour", 3599.9, "59m59.9s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatDuration(tt.seconds)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatDuration_Hours(t *testing.T) {
	tests := []struct {
		name     string
		seconds  float64
		expected string
	}{
		{"exactly one hour", 3600, "1h0m0.0s"},
		{"one hour one minute", 3660, "1h1m0.0s"},
		{"one hour one minute one second", 3661, "1h1m1.0s"},
		{"complex time", 3661.5, "1h1m1.5s"},
		{"multiple hours", 7325.3, "2h2m5.3s"},
		{"large duration", 36000, "10h0m0.0s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatDuration(tt.seconds)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatDuration_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		seconds  float64
		expected string
	}{
		{"negative zero", -0.0, "0.0s"},
		{"very small", 0.01, "0.0s"},
		{"very small rounds up", 0.05, "0.1s"},
		{"boundary 59.99", 59.99, "1m0.0s"},
		{"boundary 60", 60, "1m0.0s"},
		{"boundary 3599.99", 3599.99, "1h0m0.0s"},
		{"boundary 3600", 3600, "1h0m0.0s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatDuration(tt.seconds)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatDuration_ConsistentWidth(t *testing.T) {
	// Verify that durations that would have trailing zeros still show them
	// This prevents visual jumpiness in the UI

	// These should NOT be "5s", "1m0s", etc.
	assert.Equal(t, "5.0s", FormatDuration(5.0), "should show decimal for whole seconds")
	assert.Equal(t, "1m0.0s", FormatDuration(60.0), "should show decimal for whole minutes")
	assert.Equal(t, "1h0m0.0s", FormatDuration(3600.0), "should show decimal for whole hours")
}
