package sources

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// =============================================================================
// ENVIRONMENT VARIABLE EXPANSION WITH DEFAULTS TESTS
// =============================================================================

func TestExpandEnvWithDefaults(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		envVars  map[string]string
		expected string
	}{
		// Standard expansion
		{
			name:     "simple expansion - var set",
			input:    "${VAR}",
			envVars:  map[string]string{"VAR": "value"},
			expected: "value",
		},
		{
			name:     "simple expansion - var unset",
			input:    "${VAR}",
			envVars:  map[string]string{},
			expected: "",
		},
		{
			name:     "dollar sign expansion",
			input:    "$VAR",
			envVars:  map[string]string{"VAR": "value"},
			expected: "value",
		},

		// ${VAR:-default} - use default if unset or empty
		{
			name:     "colon-dash when unset",
			input:    "${VAR:-default}",
			envVars:  map[string]string{},
			expected: "default",
		},
		{
			name:     "colon-dash when empty",
			input:    "${VAR:-default}",
			envVars:  map[string]string{"VAR": ""},
			expected: "default",
		},
		{
			name:     "colon-dash when set",
			input:    "${VAR:-default}",
			envVars:  map[string]string{"VAR": "actual"},
			expected: "actual",
		},

		// ${VAR-default} - use default only if unset (not if empty)
		{
			name:     "dash when unset",
			input:    "${VAR-default}",
			envVars:  map[string]string{},
			expected: "default",
		},
		{
			name:     "dash when empty",
			input:    "${VAR-default}",
			envVars:  map[string]string{"VAR": ""},
			expected: "", // Returns empty, not default
		},
		{
			name:     "dash when set",
			input:    "${VAR-default}",
			envVars:  map[string]string{"VAR": "actual"},
			expected: "actual",
		},

		// Complex real-world examples
		{
			name:  "database DSN with defaults",
			input: "postgres://${DB_USER:-postgres}:${DB_PASS:-postgres}@${DB_HOST:-localhost}:${DB_PORT:-5432}/${DB_NAME:-mydb}",
			envVars: map[string]string{
				"DB_HOST": "prod-db.com",
				"DB_NAME": "production",
			},
			expected: "postgres://postgres:postgres@prod-db.com:5432/production",
		},
		{
			name:  "database DSN all env vars set",
			input: "postgres://${DB_USER:-postgres}:${DB_PASS:-postgres}@${DB_HOST:-localhost}:${DB_PORT:-5432}/${DB_NAME:-mydb}",
			envVars: map[string]string{
				"DB_USER": "admin",
				"DB_PASS": "secret",
				"DB_HOST": "db.example.com",
				"DB_PORT": "5433",
				"DB_NAME": "proddb",
			},
			expected: "postgres://admin:secret@db.example.com:5433/proddb",
		},
		{
			name:     "URL with query parameters",
			input:    "${DATABASE_DSN:-postgres://postgres:postgres@localhost:5432/kineta?sslmode=disable}",
			envVars:  map[string]string{},
			expected: "postgres://postgres:postgres@localhost:5432/kineta?sslmode=disable",
		},
		{
			name:     "URL override",
			input:    "${DATABASE_DSN:-postgres://postgres:postgres@localhost:5432/kineta?sslmode=disable}",
			envVars:  map[string]string{"DATABASE_DSN": "postgres://user:pass@prod:5432/db"},
			expected: "postgres://user:pass@prod:5432/db",
		},

		// Multiple variables in one string
		{
			name:  "multiple variables",
			input: "${HOST:-localhost}:${PORT:-8080}",
			envVars: map[string]string{
				"PORT": "3000",
			},
			expected: "localhost:3000",
		},

		// Edge cases
		{
			name:     "default with special characters",
			input:    "${VAR:-value-with-dashes}",
			envVars:  map[string]string{},
			expected: "value-with-dashes",
		},
		{
			name:     "default with colons",
			input:    "${VAR:-http://localhost:8080}",
			envVars:  map[string]string{},
			expected: "http://localhost:8080",
		},
		{
			name:     "default with equals",
			input:    "${VAR:-key=value}",
			envVars:  map[string]string{},
			expected: "key=value",
		},
		{
			name:     "empty default",
			input:    "${VAR:-}",
			envVars:  map[string]string{},
			expected: "",
		},
		{
			name:     "spaces in default",
			input:    "${VAR:-value with spaces}",
			envVars:  map[string]string{},
			expected: "value with spaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all env vars first
			for k := range tt.envVars {
				os.Unsetenv(k)
			}

			// Set up environment
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			defer func() {
				for k := range tt.envVars {
					os.Unsetenv(k)
				}
			}()

			result := expandEnvWithDefaults(tt.input)
			if result != tt.expected {
				t.Errorf("expandEnvWithDefaults() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestExpandEnvWithDefaults_Assignment(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		envVars      map[string]string
		expected     string
		shouldAssign bool
		varName      string
	}{
		// ${VAR:=default} - assign and use default if unset or empty
		{
			name:         "colon-equals when unset",
			input:        "${VAR:=assigned}",
			envVars:      map[string]string{},
			expected:     "assigned",
			shouldAssign: true,
			varName:      "VAR",
		},
		{
			name:         "colon-equals when empty",
			input:        "${VAR:=assigned}",
			envVars:      map[string]string{"VAR": ""},
			expected:     "assigned",
			shouldAssign: true,
			varName:      "VAR",
		},
		{
			name:         "colon-equals when set",
			input:        "${VAR:=assigned}",
			envVars:      map[string]string{"VAR": "existing"},
			expected:     "existing",
			shouldAssign: false,
			varName:      "VAR",
		},

		// ${VAR=default} - assign and use default only if unset
		{
			name:         "equals when unset",
			input:        "${VAR=assigned}",
			envVars:      map[string]string{},
			expected:     "assigned",
			shouldAssign: true,
			varName:      "VAR",
		},
		{
			name:         "equals when empty",
			input:        "${VAR=assigned}",
			envVars:      map[string]string{"VAR": ""},
			expected:     "",
			shouldAssign: false,
			varName:      "VAR",
		},
		{
			name:         "equals when set",
			input:        "${VAR=assigned}",
			envVars:      map[string]string{"VAR": "existing"},
			expected:     "existing",
			shouldAssign: false,
			varName:      "VAR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment
			os.Unsetenv(tt.varName)

			// Set up environment
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			defer func() {
				for k := range tt.envVars {
					os.Unsetenv(k)
				}

				os.Unsetenv(tt.varName)
			}()

			result := expandEnvWithDefaults(tt.input)
			if result != tt.expected {
				t.Errorf("expandEnvWithDefaults() = %q, want %q", result, tt.expected)
			}

			// Check if variable was assigned
			afterValue := os.Getenv(tt.varName)
			if tt.shouldAssign {
				if afterValue != tt.expected {
					t.Errorf("Variable %s = %q, want %q (should have been assigned)", tt.varName, afterValue, tt.expected)
				}
			}
		})
	}
}

func TestFileSource_ExpandEnvVars_WithDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.yaml")

	// Create test file with environment variable placeholders using defaults
	content := `
database:
  host: ${DB_HOST:-localhost}
  port: ${DB_PORT:-5432}
  name: ${DB_NAME:-testdb}
  dsn: ${DATABASE_DSN:-postgres://postgres:postgres@localhost:5432/testdb?sslmode=disable}
  
server:
  address: ${SERVER_HOST:-0.0.0.0}:${SERVER_PORT:-8080}
  
api:
  url: ${API_URL:-http://localhost:3000}
`

	os.WriteFile(testFile, []byte(content), 0644)

	tests := []struct {
		name     string
		envVars  map[string]string
		expected map[string]any
	}{
		{
			name:    "all defaults",
			envVars: map[string]string{},
			expected: map[string]any{
				"database": map[string]any{
					"host": "localhost",
					"port": "5432",
					"name": "testdb",
					"dsn":  "postgres://postgres:postgres@localhost:5432/testdb?sslmode=disable",
				},
				"server": map[string]any{
					"address": "0.0.0.0:8080",
				},
				"api": map[string]any{
					"url": "http://localhost:3000",
				},
			},
		},
		{
			name: "some overrides",
			envVars: map[string]string{
				"DB_HOST":      "prod-db.example.com",
				"DB_NAME":      "production",
				"SERVER_PORT":  "3000",
				"DATABASE_DSN": "postgres://user:pass@prod:5432/db",
			},
			expected: map[string]any{
				"database": map[string]any{
					"host": "prod-db.example.com",
					"port": "5432",
					"name": "production",
					"dsn":  "postgres://user:pass@prod:5432/db",
				},
				"server": map[string]any{
					"address": "0.0.0.0:3000",
				},
				"api": map[string]any{
					"url": "http://localhost:3000",
				},
			},
		},
		{
			name: "all overrides",
			envVars: map[string]string{
				"DB_HOST":      "db.prod.com",
				"DB_PORT":      "5433",
				"DB_NAME":      "proddb",
				"DATABASE_DSN": "postgres://admin:secret@db.prod.com:5433/proddb",
				"SERVER_HOST":  "app.example.com",
				"SERVER_PORT":  "443",
				"API_URL":      "https://api.example.com",
			},
			expected: map[string]any{
				"database": map[string]any{
					"host": "db.prod.com",
					"port": "5433",
					"name": "proddb",
					"dsn":  "postgres://admin:secret@db.prod.com:5433/proddb",
				},
				"server": map[string]any{
					"address": "app.example.com:443",
				},
				"api": map[string]any{
					"url": "https://api.example.com",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all env vars
			allKeys := []string{"DB_HOST", "DB_PORT", "DB_NAME", "DATABASE_DSN", "SERVER_HOST", "SERVER_PORT", "API_URL"}
			for _, k := range allKeys {
				os.Unsetenv(k)
			}

			// Set up environment
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			defer func() {
				for k := range tt.envVars {
					os.Unsetenv(k)
				}
			}()

			source, err := NewFileSource(testFile, FileSourceOptions{
				ExpandEnvVars: true,
			})
			if err != nil {
				t.Fatalf("NewFileSource() error = %v", err)
			}

			ctx := context.Background()

			data, err := source.Load(ctx)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			// Check database section
			if db, ok := data["database"].(map[string]any); ok {
				expectedDB := tt.expected["database"].(map[string]any)
				if db["host"] != expectedDB["host"] {
					t.Errorf("database.host = %v, want %v", db["host"], expectedDB["host"])
				}

				if db["port"] != expectedDB["port"] {
					t.Errorf("database.port = %v, want %v", db["port"], expectedDB["port"])
				}

				if db["name"] != expectedDB["name"] {
					t.Errorf("database.name = %v, want %v", db["name"], expectedDB["name"])
				}

				if db["dsn"] != expectedDB["dsn"] {
					t.Errorf("database.dsn = %v, want %v", db["dsn"], expectedDB["dsn"])
				}
			} else {
				t.Error("database section not found or wrong type")
			}

			// Check server section
			if server, ok := data["server"].(map[string]any); ok {
				expectedServer := tt.expected["server"].(map[string]any)
				if server["address"] != expectedServer["address"] {
					t.Errorf("server.address = %v, want %v", server["address"], expectedServer["address"])
				}
			} else {
				t.Error("server section not found or wrong type")
			}

			// Check api section
			if api, ok := data["api"].(map[string]any); ok {
				expectedAPI := tt.expected["api"].(map[string]any)
				if api["url"] != expectedAPI["url"] {
					t.Errorf("api.url = %v, want %v", api["url"], expectedAPI["url"])
				}
			} else {
				t.Error("api section not found or wrong type")
			}
		})
	}
}

// Test that existing standard expansion still works.
func TestFileSource_ExpandEnvVars_BackwardCompatibility(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.yaml")

	// Create test file with standard environment variable placeholders
	content := `
database:
  host: $DB_HOST
  port: ${DB_PORT}
  name: ${DB_NAME}
`

	os.WriteFile(testFile, []byte(content), 0644)

	// Set environment variables
	os.Setenv("DB_HOST", "testhost")
	os.Setenv("DB_PORT", "5432")
	os.Setenv("DB_NAME", "testdb")

	defer func() {
		os.Unsetenv("DB_HOST")
		os.Unsetenv("DB_PORT")
		os.Unsetenv("DB_NAME")
	}()

	source, err := NewFileSource(testFile, FileSourceOptions{
		ExpandEnvVars: true,
	})
	if err != nil {
		t.Fatalf("NewFileSource() error = %v", err)
	}

	ctx := context.Background()

	data, err := source.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify standard expansion still works
	db := data["database"].(map[string]any)
	if db["host"] != "testhost" {
		t.Errorf("host = %v, want testhost", db["host"])
	}

	if db["port"] != "5432" {
		t.Errorf("port = %v, want 5432", db["port"])
	}

	if db["name"] != "testdb" {
		t.Errorf("name = %v, want testdb", db["name"])
	}
}
