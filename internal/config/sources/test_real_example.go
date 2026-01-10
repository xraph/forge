package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestRealConfigExample_WithDefaults(t *testing.T) {
	// Clear all related env vars
	clearEnvVars := []string{
		"DATABASE_DSN", "DATABASE_MAX_OPEN_CONNS", "DATABASE_MAX_IDLE_CONNS",
		"DATABASE_DEFAULT", "DATABASE_DISABLE_SLOW_QUERY_LOGGING", "REDIS_DSN",
		"AI_LLM_ENABLED", "AI_AGENTS_ENABLED", "AI_INFERENCE_ENABLED",
		"AI_TRAINING_ENABLED", "AI_COORDINATION_ENABLED", "AI_MAX_CONCURRENCY",
		"AI_REQUEST_TIMEOUT", "AI_CACHE_SIZE", "AI_LLM_DEFAULT_PROVIDER",
		"AI_LLM_TIMEOUT", "AI_LLM_MAX_RETRIES", "LMSTUDIO_BASE_URL", "OLLAMA_BASE_URL",
	}
	for _, k := range clearEnvVars {
		os.Unsetenv(k)
	}

	source, err := NewFileSource("test_real_config.yaml", FileSourceOptions{
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

	// Verify database config uses defaults
	if db, ok := data["database"].(map[string]any); ok {
		if databases, ok := db["databases"].([]any); ok {
			if len(databases) > 0 {
				firstDB := databases[0].(map[string]any)

				expectedDSN := "postgres://postgres:postgres@localhost:5432/kineta?sslmode=disable"
				if firstDB["dsn"] != expectedDSN {
					t.Errorf("database.databases[0].dsn = %v, want %v", firstDB["dsn"], expectedDSN)
				}

				if firstDB["max_open_conns"] != "25" {
					t.Errorf("database.databases[0].max_open_conns = %v, want 25", firstDB["max_open_conns"])
				}
			}
		}
	}

	// Verify extensions.database uses defaults
	if extensions, ok := data["extensions"].(map[string]any); ok {
		if dbExt, ok := extensions["database"].(map[string]any); ok {
			if dbExt["default"] != "default" {
				t.Errorf("extensions.database.default = %v, want default", dbExt["default"])
			}

			if databases, ok := dbExt["databases"].([]any); ok {
				if len(databases) >= 2 {
					// Check redis database
					redisDB := databases[1].(map[string]any)
					if redisDB["dsn"] != "redis://localhost:6379" {
						t.Errorf("redis dsn = %v, want redis://localhost:6379", redisDB["dsn"])
					}
				}
			}
		}
	}

	// Verify AI config uses defaults
	if ai, ok := data["ai"].(map[string]any); ok {
		if ai["llm_enabled"] != "true" {
			t.Errorf("ai.llm_enabled = %v, want true", ai["llm_enabled"])
		}

		if ai["max_concurrency"] != "10" {
			t.Errorf("ai.max_concurrency = %v, want 10", ai["max_concurrency"])
		}

		if llm, ok := ai["llm"].(map[string]any); ok {
			if llm["default_provider"] != "lmstudio" {
				t.Errorf("ai.llm.default_provider = %v, want lmstudio", llm["default_provider"])
			}

			if providers, ok := llm["providers"].(map[string]any); ok {
				if lmstudio, ok := providers["lmstudio"].(map[string]any); ok {
					if lmstudio["base_url"] != "http://localhost:1234/v1" {
						t.Errorf("lmstudio.base_url = %v, want http://localhost:1234/v1", lmstudio["base_url"])
					}
				}
			}
		}
	}

	// Print config for visual verification
	t.Log("Config loaded successfully with all defaults:")

	jsonData, _ := json.MarshalIndent(data, "", "  ")
	t.Log(string(jsonData))
}

func TestRealConfigExample_WithEnvOverrides(t *testing.T) {
	// Set some env vars to override defaults
	os.Setenv("DATABASE_DSN", "postgres://admin:secret@prod-db:5432/proddb")
	os.Setenv("DATABASE_MAX_OPEN_CONNS", "100")
	os.Setenv("REDIS_DSN", "redis://:password@prod-redis:6379")
	os.Setenv("AI_LLM_DEFAULT_PROVIDER", "openai")
	os.Setenv("OLLAMA_BASE_URL", "http://ollama.prod:11434")

	defer func() {
		os.Unsetenv("DATABASE_DSN")
		os.Unsetenv("DATABASE_MAX_OPEN_CONNS")
		os.Unsetenv("REDIS_DSN")
		os.Unsetenv("AI_LLM_DEFAULT_PROVIDER")
		os.Unsetenv("OLLAMA_BASE_URL")
	}()

	source, err := NewFileSource("test_real_config.yaml", FileSourceOptions{
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

	// Verify database config uses env vars
	if db, ok := data["database"].(map[string]any); ok {
		if databases, ok := db["databases"].([]any); ok {
			if len(databases) > 0 {
				firstDB := databases[0].(map[string]any)

				expectedDSN := "postgres://admin:secret@prod-db:5432/proddb"
				if firstDB["dsn"] != expectedDSN {
					t.Errorf("database.databases[0].dsn = %v, want %v", firstDB["dsn"], expectedDSN)
				}

				if firstDB["max_open_conns"] != "100" {
					t.Errorf("database.databases[0].max_open_conns = %v, want 100", firstDB["max_open_conns"])
				}
			}
		}
	}

	// Verify extensions.database uses env vars
	if extensions, ok := data["extensions"].(map[string]any); ok {
		if dbExt, ok := extensions["database"].(map[string]any); ok {
			if databases, ok := dbExt["databases"].([]any); ok {
				if len(databases) >= 2 {
					// Check redis database
					redisDB := databases[1].(map[string]any)

					expectedRedis := "redis://:password@prod-redis:6379"
					if redisDB["dsn"] != expectedRedis {
						t.Errorf("redis dsn = %v, want %v", redisDB["dsn"], expectedRedis)
					}
				}
			}
		}
	}

	// Verify AI config uses env overrides
	if ai, ok := data["ai"].(map[string]any); ok {
		if llm, ok := ai["llm"].(map[string]any); ok {
			if llm["default_provider"] != "openai" {
				t.Errorf("ai.llm.default_provider = %v, want openai", llm["default_provider"])
			}

			if providers, ok := llm["providers"].(map[string]any); ok {
				if ollama, ok := providers["ollama"].(map[string]any); ok {
					if ollama["base_url"] != "http://ollama.prod:11434" {
						t.Errorf("ollama.base_url = %v, want http://ollama.prod:11434", ollama["base_url"])
					}
				}
			}
		}
	}

	// Print config for visual verification
	t.Log("Config loaded successfully with environment overrides:")

	jsonData, _ := json.MarshalIndent(data, "", "  ")
	t.Log(string(jsonData))
}

func TestRealConfigExample_MixedDefaults(t *testing.T) {
	// Set only SOME env vars - others should use defaults
	os.Setenv("DATABASE_DSN", "postgres://user:pass@custom-db:5432/mydb")
	// DATABASE_MAX_OPEN_CONNS not set - should use default 25
	os.Setenv("AI_LLM_DEFAULT_PROVIDER", "ollama")
	// OLLAMA_BASE_URL not set - should use default http://localhost:11434

	defer func() {
		os.Unsetenv("DATABASE_DSN")
		os.Unsetenv("AI_LLM_DEFAULT_PROVIDER")
	}()

	source, err := NewFileSource("test_real_config.yaml", FileSourceOptions{
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

	// Verify mixed behavior
	if db, ok := data["database"].(map[string]any); ok {
		if databases, ok := db["databases"].([]any); ok {
			if len(databases) > 0 {
				firstDB := databases[0].(map[string]any)
				// DSN should use env var
				if firstDB["dsn"] != "postgres://user:pass@custom-db:5432/mydb" {
					t.Errorf("database.databases[0].dsn = %v, want custom DSN", firstDB["dsn"])
				}
				// max_open_conns should use default
				if firstDB["max_open_conns"] != "25" {
					t.Errorf("database.databases[0].max_open_conns = %v, want 25 (default)", firstDB["max_open_conns"])
				}
			}
		}
	}

	if ai, ok := data["ai"].(map[string]any); ok {
		if llm, ok := ai["llm"].(map[string]any); ok {
			// provider should use env var
			if llm["default_provider"] != "ollama" {
				t.Errorf("ai.llm.default_provider = %v, want ollama", llm["default_provider"])
			}

			if providers, ok := llm["providers"].(map[string]any); ok {
				if ollama, ok := providers["ollama"].(map[string]any); ok {
					// URL should use default
					if ollama["base_url"] != "http://localhost:11434" {
						t.Errorf("ollama.base_url = %v, want http://localhost:11434 (default)", ollama["base_url"])
					}
				}
			}
		}
	}

	t.Log("Config loaded successfully with mixed defaults and overrides:")

	jsonData, _ := json.MarshalIndent(data, "", "  ")
	t.Log(string(jsonData))
}

func ExampleFileSource_expandEnvWithDefaults() {
	// This example shows how the new bash-style default syntax works

	// Without environment variables set
	result1 := expandEnvWithDefaults("${DATABASE_DSN:-postgres://localhost:5432/mydb}")
	fmt.Println("Without env var:", result1)

	// With environment variable set
	os.Setenv("DATABASE_DSN", "postgres://prod:5432/proddb")

	result2 := expandEnvWithDefaults("${DATABASE_DSN:-postgres://localhost:5432/mydb}")
	fmt.Println("With env var:", result2)
	os.Unsetenv("DATABASE_DSN")

	// Complex example with multiple variables
	os.Setenv("DB_HOST", "prod-server")

	result3 := expandEnvWithDefaults("postgres://${DB_USER:-postgres}:${DB_PASS:-postgres}@${DB_HOST:-localhost}:${DB_PORT:-5432}/${DB_NAME:-mydb}")
	fmt.Println("Complex example:", result3)
	os.Unsetenv("DB_HOST")

	// Output:
	// Without env var: postgres://localhost:5432/mydb
	// With env var: postgres://prod:5432/proddb
	// Complex example: postgres://postgres:postgres@prod-server:5432/mydb
}
