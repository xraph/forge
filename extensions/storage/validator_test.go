package storage

import (
	"testing"
)

func TestPathValidator_ValidateKey(t *testing.T) {
	pv := NewPathValidator()

	tests := []struct {
		name      string
		key       string
		shouldErr bool
	}{
		// Valid keys
		{"simple key", "file.txt", false},
		{"with path", "folder/file.txt", false},
		{"deep path", "a/b/c/d/file.txt", false},
		{"with hyphens", "my-file-name.txt", false},
		{"with underscores", "my_file_name.txt", false},
		{"with dots", "file.backup.txt", false},

		// Invalid keys
		{"empty key", "", true},
		{"path traversal dot-dot", "../file.txt", true},
		{"path traversal in middle", "folder/../file.txt", true},
		{"absolute path", "/file.txt", true},
		{"double slash", "folder//file.txt", true},
		{"windows path", "C:\\file.txt", true},
		{"null byte", "file\x00.txt", true},
		{"starts with dot", ".hidden", true},
		{"ends with slash", "folder/", true},
		{"special chars", "file@#$.txt", true},

		// Edge cases
		{"very long", string(make([]byte, 2000)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pv.ValidateKey(tt.key)
			if tt.shouldErr && err == nil {
				t.Errorf("Expected error for key '%s', got nil", tt.key)
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("Expected no error for key '%s', got %v", tt.key, err)
			}
		})
	}
}

func TestPathValidator_SanitizeKey(t *testing.T) {
	pv := NewPathValidator()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"clean key", "file.txt", "file.txt"},
		{"leading slash", "/file.txt", "file.txt"},
		{"trailing slash", "file.txt/", "file.txt"},
		{"double slash", "folder//file.txt", "folder/file.txt"},
		{"spaces", "  file.txt  ", "file.txt"},
		{"dot-dot", "../file.txt", "file.txt"},
		{"complex", "//folder/.././file.txt//", "folder/file.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pv.SanitizeKey(tt.input)
			if result != tt.expected {
				t.Errorf("Expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestPathValidator_ValidateContentType(t *testing.T) {
	pv := NewPathValidator()

	tests := []struct {
		name        string
		contentType string
		shouldErr   bool
	}{
		// Valid content types
		{"empty", "", false},
		{"simple", "text/plain", false},
		{"with subtype", "application/json", false},
		{"with suffix", "application/json+xml", false},
		{"with params", "text/plain; charset=utf-8", false},

		// Invalid content types
		{"invalid format", "invalid", true},
		{"special chars", "text/@#$", true},
		{"no subtype", "text/", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pv.ValidateContentType(tt.contentType)
			if tt.shouldErr && err == nil {
				t.Errorf("Expected error for content type '%s', got nil", tt.contentType)
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("Expected no error for content type '%s', got %v", tt.contentType, err)
			}
		})
	}
}

func TestIsValidSize(t *testing.T) {
	tests := []struct {
		name      string
		size      int64
		maxSize   int64
		shouldErr bool
	}{
		{"within limit", 100, 1000, true},
		{"at limit", 1000, 1000, true},
		{"over limit", 1001, 1000, false},
		{"zero size", 0, 1000, false},
		{"negative size", -1, 1000, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := IsValidSize(tt.size, tt.maxSize)
			if valid != tt.shouldErr {
				t.Errorf("Expected %v, got %v for size %d with max %d", tt.shouldErr, valid, tt.size, tt.maxSize)
			}
		})
	}
}

func TestValidateMetadata(t *testing.T) {
	tests := []struct {
		name      string
		metadata  map[string]string
		shouldErr bool
	}{
		{"empty", map[string]string{}, false},
		{"valid", map[string]string{"key1": "value1", "key2": "value2"}, false},
		{"too many keys", generateMetadata(101), true},
		{"key too long", map[string]string{string(make([]byte, 300)): "value"}, true},
		{"value too long", map[string]string{"key": string(make([]byte, 5000))}, true},
		{"invalid key chars", map[string]string{"key@#$": "value"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMetadata(tt.metadata)
			if tt.shouldErr && err == nil {
				t.Error("Expected error, got nil")
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("Expected no error, got %v", err)
			}
		})
	}
}

func generateMetadata(count int) map[string]string {
	m := make(map[string]string)
	for i := 0; i < count; i++ {
		m[string(rune('a'+i%26))] = "value"
	}
	return m
}

func BenchmarkPathValidator_ValidateKey(b *testing.B) {
	pv := NewPathValidator()
	key := "folder/subfolder/file.txt"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pv.ValidateKey(key)
	}
}

func BenchmarkPathValidator_SanitizeKey(b *testing.B) {
	pv := NewPathValidator()
	key := "//folder/../subfolder//./file.txt"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pv.SanitizeKey(key)
	}
}
