package configure

import (
	"reflect"
	"testing"
)

func TestParseExtensions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty brackets clears extensions",
			input:    "[]",
			expected: []string{},
		},
		{
			name:     "empty string returns nil (keep default)",
			input:    "",
			expected: nil,
		},
		{
			name:     "single extension",
			input:    "postgis",
			expected: []string{"postgis"},
		},
		{
			name:     "multiple extensions",
			input:    "postgis,pg_trgm,uuid-ossp",
			expected: []string{"postgis", "pg_trgm", "uuid-ossp"},
		},
		{
			name:     "extensions with spaces",
			input:    "postgis, pg_trgm , uuid-ossp",
			expected: []string{"postgis", "pg_trgm", "uuid-ossp"},
		},
		{
			name:     "trailing comma",
			input:    "postgis,pg_trgm,",
			expected: []string{"postgis", "pg_trgm"},
		},
		{
			name:     "leading comma",
			input:    ",postgis,pg_trgm",
			expected: []string{"postgis", "pg_trgm"},
		},
		{
			name:     "multiple commas",
			input:    "postgis,,pg_trgm",
			expected: []string{"postgis", "pg_trgm"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseExtensions(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("parseExtensions(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseExtensions_EmptyBracketsIsNotNil(t *testing.T) {
	// This is the key test: "[]" should return an empty slice, not nil
	// This allows users to explicitly clear extensions
	result := parseExtensions("[]")
	if result == nil {
		t.Error("parseExtensions(\"[]\") returned nil, want empty slice")
	}
	if len(result) != 0 {
		t.Errorf("parseExtensions(\"[]\") = %v, want empty slice", result)
	}
}
