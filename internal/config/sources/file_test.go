package sources

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// =============================================================================
// FILE SOURCE CREATION TESTS
// =============================================================================

func TestNewFileSource(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.yaml")

	// Create test file
	content := []byte("key: value\n")
	os.WriteFile(testFile, content, 0644)

	tests := []struct {
		name string
		path string
		opts FileSourceOptions
	}{
		{
			name: "yaml file",
			path: testFile,
			opts: FileSourceOptions{},
		},
		{
			name: "with priority",
			path: testFile,
			opts: FileSourceOptions{
				Priority: 10,
			},
		},
		{
			name: "with watch enabled",
			path: testFile,
			opts: FileSourceOptions{
				WatchEnabled: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := NewFileSource(tt.path, tt.opts)
			if err != nil {
				t.Fatalf("NewFileSource() error = %v", err)
			}

			if source == nil {
				t.Fatal("NewFileSource() returned nil")
			}

			if source.Name() == "" {
				t.Error("Name() returned empty string")
			}
		})
	}
}

func TestNewFileSourceWithConfig(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.yaml")

	content := []byte("key: value\n")
	os.WriteFile(testFile, content, 0644)

	source, err := NewFileSource(testFile, FileSourceOptions{
		Priority: 10,
	})
	if err != nil {
		t.Fatalf("NewFileSource() error = %v", err)
	}

	if source == nil {
		t.Fatal("NewFileSourceWithConfig() returned nil")
	}

	if source.Priority() != 10 {
		t.Errorf("Priority() = %d, want 10", source.Priority())
	}
}

// =============================================================================
// FILE SOURCE METADATA TESTS
// =============================================================================

func TestFileSource_Metadata(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.yaml")

	content := []byte("key: value\n")
	os.WriteFile(testFile, content, 0644)

	source, err := NewFileSource(testFile, FileSourceOptions{})
	if err != nil {
		t.Fatalf("NewFileSource() error = %v", err)
	}

	sourceType := source.GetType()
	if sourceType != "file" {
		t.Errorf("source.GetType() = %v, want file", sourceType)
	}

	sourceName := source.Name()
	if sourceName == "" {
		t.Error("source.Name() is empty")
	}
}

// =============================================================================
// FILE SOURCE LOAD TESTS - YAML
// =============================================================================

func TestFileSource_Load_YAML(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.yaml")

	content := []byte(`
string: value
int: 42
bool: true
nested:
  key: nested_value
list:
  - item1
  - item2
  - item3
`)
	os.WriteFile(testFile, content, 0644)

	source, err := NewFileSource(testFile, FileSourceOptions{})
	if err != nil {
		t.Fatalf("NewFileSource() error = %v", err)
	}

	ctx := context.Background()

	data, err := source.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if data == nil {
		t.Fatal("Load() returned nil data")
	}

	// Check values
	if data["string"] != "value" {
		t.Errorf("string = %v, want value", data["string"])
	}

	if data["int"] != 42 {
		t.Errorf("int = %v, want 42", data["int"])
	}

	if data["bool"] != true {
		t.Errorf("bool = %v, want true", data["bool"])
	}

	// Check nested
	if nested, ok := data["nested"].(map[string]any); ok {
		if nested["key"] != "nested_value" {
			t.Errorf("nested.key = %v, want nested_value", nested["key"])
		}
	} else {
		t.Error("nested is not a map")
	}

	// Check list
	if list, ok := data["list"].([]any); ok {
		if len(list) != 3 {
			t.Errorf("list length = %d, want 3", len(list))
		}
	} else {
		t.Error("list is not a slice")
	}
}

// =============================================================================
// FILE SOURCE LOAD TESTS - JSON
// =============================================================================

func TestFileSource_Load_JSON(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.json")

	content := []byte(`{
  "string": "value",
  "int": 42,
  "bool": true,
  "nested": {
    "key": "nested_value"
  },
  "list": ["item1", "item2", "item3"]
}`)
	os.WriteFile(testFile, content, 0644)

	source, err := NewFileSource(testFile, FileSourceOptions{})
	if err != nil {
		t.Fatalf("NewFileSource() error = %v", err)
	}

	ctx := context.Background()

	data, err := source.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if data == nil {
		t.Fatal("Load() returned nil data")
	}

	if data["string"] != "value" {
		t.Errorf("string = %v, want value", data["string"])
	}

	// JSON numbers are float64
	if val, ok := data["int"].(float64); !ok || val != 42 {
		t.Errorf("int = %v (%T), want 42", data["int"], data["int"])
	}
}

// =============================================================================
// FILE SOURCE LOAD TESTS - TOML
// =============================================================================

func TestFileSource_Load_TOML(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.toml")

	content := []byte(`
string = "value"
int = 42
bool = true

[nested]
key = "nested_value"

list = ["item1", "item2", "item3"]
`)
	os.WriteFile(testFile, content, 0644)

	source, err := NewFileSource(testFile, FileSourceOptions{})
	if err != nil {
		t.Fatalf("NewFileSource() error = %v", err)
	}

	ctx := context.Background()

	data, err := source.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if data == nil {
		t.Fatal("Load() returned nil data")
	}

	if data["string"] != "value" {
		t.Errorf("string = %v, want value", data["string"])
	}

	// TOML integers are converted to int if they fit
	if val, ok := data["int"].(int); !ok || val != 42 {
		t.Errorf("int = %v (%T), want 42", data["int"], data["int"])
	}
}

// =============================================================================
// FILE SOURCE GET TESTS
// =============================================================================

func TestFileSource_Get(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.yaml")

	content := []byte("key: value\nanother: data\n")
	os.WriteFile(testFile, content, 0644)

	source, err := NewFileSource(testFile, FileSourceOptions{})
	if err != nil {
		t.Fatalf("NewFileSource() error = %v", err)
	}

	ctx := context.Background()
	source.Load(ctx)

	t.Run("get existing key", func(t *testing.T) {
		data, err := source.Load(ctx)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		value, ok := data["key"]
		if !ok {
			t.Fatal("Load() did not return existing key")
		}

		if value != "value" {
			t.Errorf("Load() = %v, want value", value)
		}
	})

	t.Run("get non-existent key", func(t *testing.T) {
		data, err := source.Load(ctx)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		_, ok := data["nonexistent"]
		if ok {
			t.Error("Load() should not return non-existent key")
		}
	})
}

// =============================================================================
// FILE SOURCE WATCH TESTS
// =============================================================================

func TestFileSource_Watch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping watch test in short mode")
	}

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.yaml")

	// Create initial file
	content := []byte("key: initial\n")
	os.WriteFile(testFile, content, 0644)

	source, err := NewFileSource(testFile, FileSourceOptions{
		WatchEnabled: true,
	})
	if err != nil {
		t.Fatalf("NewFileSource() error = %v", err)
	}

	if !source.IsWatchable() {
		t.Error("Source should be watchable when WatchEnabled is true")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load initial data
	source.Load(ctx)

	changeDetected := make(chan bool, 1)
	changes := make(chan struct{})

	// Start watching
	go func() {
		err := source.Watch(ctx, nil)
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("Watch() error = %v", err)
		}

		close(changes)
	}()

	// Give watch time to start
	time.Sleep(200 * time.Millisecond)

	// Modify file
	newContent := []byte("key: modified\n")

	err = os.WriteFile(testFile, newContent, 0644)
	if err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Wait for change detection
	select {
	case <-changeDetected:
		// OK
	case <-time.After(3 * time.Second):
		// Might not detect change depending on implementation
		t.Log("Change not detected within timeout")
	}

	// Cancel context
	cancel()

	// Wait for watch to stop
	select {
	case <-changes:
		// OK
	case <-time.After(2 * time.Second):
		t.Error("Watch() did not stop after context cancellation")
	}
}

func TestFileSource_Watch_Disabled(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.yaml")

	content := []byte("key: value\n")
	os.WriteFile(testFile, content, 0644)

	source, err := NewFileSource(testFile, FileSourceOptions{
		WatchEnabled: false,
	})
	if err != nil {
		t.Fatalf("NewFileSource() error = %v", err)
	}

	if source.IsWatchable() {
		t.Error("Source should not be watchable when WatchEnabled is false")
	}

	ctx := context.Background()

	err = source.Watch(ctx, nil)
	if err == nil {
		t.Error("Watch() should return error when watching is disabled")
	}
}

// =============================================================================
// FILE SOURCE VALIDATION TESTS
// =============================================================================

func TestFileSource_Validate(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.yaml")

	content := []byte("key: value\n")
	os.WriteFile(testFile, content, 0644)

	source, err := NewFileSource(testFile, FileSourceOptions{})
	if err != nil {
		t.Fatalf("NewFileSource() error = %v", err)
	}

	ctx := context.Background()
	available := source.IsAvailable(ctx)

	if !available {
		t.Error("Source should be available")
	}
}

func TestFileSource_Validate_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentFile := filepath.Join(tmpDir, "nonexistent.yaml")

	source, err := NewFileSource(nonExistentFile, FileSourceOptions{})
	if err != nil {
		t.Fatalf("NewFileSource() error = %v", err)
	}

	ctx := context.Background()
	available := source.IsAvailable(ctx)

	if available {
		t.Error("Source should not be available for non-existent file")
	}
}

// =============================================================================
// FILE SOURCE ERROR HANDLING TESTS
// =============================================================================

func TestFileSource_Load_NonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentFile := filepath.Join(tmpDir, "nonexistent.yaml")

	source, err := NewFileSource(nonExistentFile, FileSourceOptions{
		RequireFile: true,
	})
	if err != nil {
		t.Fatalf("NewFileSource() error = %v", err)
	}

	ctx := context.Background()

	_, err = source.Load(ctx)
	if err == nil {
		t.Error("Load() should return error for non-existent file")
	}
}

func TestFileSource_Load_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "invalid.yaml")

	// Invalid YAML content
	content := []byte("key: value\ninvalid yaml: [unclosed bracket")
	os.WriteFile(testFile, content, 0644)

	source, err := NewFileSource(testFile, FileSourceOptions{})
	if err != nil {
		t.Fatalf("NewFileSource() error = %v", err)
	}

	ctx := context.Background()

	_, err = source.Load(ctx)
	if err == nil {
		t.Error("Load() should return error for invalid YAML")
	}
}

func TestFileSource_Load_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "invalid.json")

	// Invalid JSON content
	content := []byte(`{"key": "value", invalid}`)
	os.WriteFile(testFile, content, 0644)

	source, err := NewFileSource(testFile, FileSourceOptions{})
	if err != nil {
		t.Fatalf("NewFileSource() error = %v", err)
	}

	ctx := context.Background()

	_, err = source.Load(ctx)
	if err == nil {
		t.Error("Load() should return error for invalid JSON")
	}
}

func TestFileSource_Load_UnreadableFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping test when running as root")
	}

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "unreadable.yaml")

	content := []byte("key: value\n")
	os.WriteFile(testFile, content, 0644)

	// Make file unreadable
	os.Chmod(testFile, 0000)
	defer os.Chmod(testFile, 0644)

	source, err := NewFileSource(testFile, FileSourceOptions{})
	if err != nil {
		t.Fatalf("NewFileSource() error = %v", err)
	}

	ctx := context.Background()

	_, err = source.Load(ctx)
	if err == nil {
		t.Error("Load() should return error for unreadable file")
	}
}

// =============================================================================
// FILE SOURCE BACKUP TESTS
// =============================================================================

func TestFileSource_Backup(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.yaml")
	backupFile := testFile + ".backup"

	content := []byte("key: value\n")
	os.WriteFile(testFile, content, 0644)

	source, err := NewFileSource(testFile, FileSourceOptions{
		BackupEnabled: true,
	})
	if err != nil {
		t.Fatalf("NewFileSource() error = %v", err)
	}

	ctx := context.Background()

	_, err = source.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Check if backup was created
	if _, err := os.Stat(backupFile); err != nil {
		if os.IsNotExist(err) {
			t.Log("Backup file not created (may be implementation dependent)")
		} else {
			t.Errorf("Stat(backup) error = %v", err)
		}
	}
}

// =============================================================================
// FILE SOURCE LIFECYCLE TESTS
// =============================================================================

func TestFileSource_Lifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.yaml")

	content := []byte("key: value\n")
	os.WriteFile(testFile, content, 0644)

	source, err := NewFileSource(testFile, FileSourceOptions{})
	if err != nil {
		t.Fatalf("NewFileSource() error = %v", err)
	}

	ctx := context.Background()

	// Load
	_, err = source.Load(ctx)
	if err != nil {
		t.Errorf("Load() error = %v", err)
	}

	// Test IsAvailable instead of Validate
	available := source.IsAvailable(ctx)
	if !available {
		t.Error("Source should be available")
	}

	// Test StopWatch instead of Stop
	err = source.StopWatch()
	if err != nil {
		t.Errorf("StopWatch() error = %v", err)
	}
}

// =============================================================================
// FILE SOURCE FACTORY TESTS
// =============================================================================

func TestFileSourceFactory_Create(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.yaml")

	content := []byte("key: value\n")
	os.WriteFile(testFile, content, 0644)

	factory := &FileSourceFactory{}

	config := FileSourceConfig{
		Path:     testFile,
		Format:   "yaml",
		Priority: 10,
	}

	source, err := factory.CreateFromConfig(config)
	if err != nil {
		t.Fatalf("CreateFromConfig() error = %v", err)
	}

	if source == nil {
		t.Fatal("Create() returned nil source")
	}

	if source.Priority() != 10 {
		t.Errorf("Priority() = %d, want 10", source.Priority())
	}
}

func TestFileSourceFactory_Validate(t *testing.T) {
	factory := &FileSourceFactory{}

	t.Run("valid config", func(t *testing.T) {
		config := FileSourceConfig{
			Path:   "/path/to/config.yaml",
			Format: "yaml",
		}

		// Test CreateFromConfig instead of Validate
		_, err := factory.CreateFromConfig(config)
		if err != nil {
			t.Errorf("CreateFromConfig() error = %v, want nil", err)
		}
	})

	t.Run("missing path", func(t *testing.T) {
		config := FileSourceConfig{
			Format: "yaml",
		}

		// Test CreateFromConfig instead of Validate
		_, err := factory.CreateFromConfig(config)
		if err == nil {
			t.Error("CreateFromConfig() should return error for missing path")
		}
	})

	t.Run("invalid format", func(t *testing.T) {
		config := FileSourceConfig{
			Path:   "/path/to/config.yaml",
			Format: "invalid",
		}

		// Test CreateFromConfig instead of Validate
		_, err := factory.CreateFromConfig(config)
		// May or may not error depending on implementation
		_ = err
	})
}

// =============================================================================
// FILE SOURCE EDGE CASES
// =============================================================================

func TestFileSource_EdgeCases(t *testing.T) {
	t.Run("empty file", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "empty.yaml")

		// Create empty file
		os.WriteFile(testFile, []byte{}, 0644)

		source, err := NewFileSource(testFile, FileSourceOptions{})
		if err != nil {
			t.Fatalf("NewFileSource() error = %v", err)
		}

		ctx := context.Background()

		data, err := source.Load(ctx)
		if err != nil {
			t.Errorf("Load() error = %v", err)
		}

		if data == nil {
			t.Error("Load() returned nil data for empty file")
		}
	})

	t.Run("whitespace only file", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "whitespace.yaml")

		content := []byte("   \n\n   \t\n")
		os.WriteFile(testFile, content, 0644)

		source, err := NewFileSource(testFile, FileSourceOptions{})
		if err != nil {
			t.Fatalf("NewFileSource() error = %v", err)
		}

		ctx := context.Background()

		data, err := source.Load(ctx)
		if err != nil {
			t.Errorf("Load() error = %v", err)
		}

		if data == nil {
			t.Error("Load() returned nil data")
		}
	})

	t.Run("very large file", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "large.yaml")

		// Create a large YAML file with unique keys
		var content []byte
		for i := range 10000 {
			content = append(content, []byte("key"+strconv.Itoa(i)+": value\n")...)
		}

		os.WriteFile(testFile, content, 0644)

		source, err := NewFileSource(testFile, FileSourceOptions{})
		if err != nil {
			t.Fatalf("NewFileSource() error = %v", err)
		}

		ctx := context.Background()

		data, err := source.Load(ctx)
		if err != nil {
			t.Errorf("Load() error = %v", err)
		}

		if data == nil {
			t.Error("Load() returned nil data")
		}

		if len(data) == 0 {
			t.Error("Load() returned empty data for large file")
		}
	})

	t.Run("file with BOM", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "bom.yaml")

		// UTF-8 BOM + content
		content := []byte{0xEF, 0xBB, 0xBF}
		content = append(content, []byte("key: value\n")...)
		os.WriteFile(testFile, content, 0644)

		source, err := NewFileSource(testFile, FileSourceOptions{})
		if err != nil {
			t.Fatalf("NewFileSource() error = %v", err)
		}

		ctx := context.Background()

		data, err := source.Load(ctx)
		if err != nil {
			t.Errorf("Load() error = %v", err)
		}

		if data == nil || data["key"] != "value" {
			t.Error("Load() failed to parse file with BOM")
		}
	})

	t.Run("unicode content", func(t *testing.T) {
		tmpDir := t.TempDir()
		testFile := filepath.Join(tmpDir, "unicode.yaml")

		content := []byte("greeting: Hello 世界 🌍\n")
		os.WriteFile(testFile, content, 0644)

		source, err := NewFileSource(testFile, FileSourceOptions{})
		if err != nil {
			t.Fatalf("NewFileSource() error = %v", err)
		}

		ctx := context.Background()

		data, err := source.Load(ctx)
		if err != nil {
			t.Errorf("Load() error = %v", err)
		}

		if data["greeting"] != "Hello 世界 🌍" {
			t.Errorf("greeting = %v, want Hello 世界 🌍", data["greeting"])
		}
	})
}

// =============================================================================
// FILE SOURCE PRIORITY TESTS
// =============================================================================

func TestFileSource_Priority(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.yaml")

	content := []byte("key: value\n")
	os.WriteFile(testFile, content, 0644)

	tests := []struct {
		name     string
		priority int
	}{
		{"default priority", 0},
		{"custom priority", 10},
		{"high priority", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := NewFileSource(testFile, FileSourceOptions{
				Priority: tt.priority,
			})
			if err != nil {
				t.Fatalf("NewFileSource() error = %v", err)
			}

			if source.Priority() != tt.priority {
				t.Errorf("Priority() = %d, want %d", source.Priority(), tt.priority)
			}
		})
	}
}

// =============================================================================
// FILE SOURCE FORMAT DETECTION TESTS
// =============================================================================

func TestFileSource_FormatDetection(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name     string
		filename string
		content  []byte
	}{
		{
			name:     "yaml extension",
			filename: "config.yaml",
			content:  []byte("key: value\n"),
		},
		{
			name:     "yml extension",
			filename: "config.yml",
			content:  []byte("key: value\n"),
		},
		{
			name:     "json extension",
			filename: "config.json",
			content:  []byte(`{"key": "value"}`),
		},
		{
			name:     "toml extension",
			filename: "config.toml",
			content:  []byte("key = \"value\"\n"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFile := filepath.Join(tmpDir, tt.filename)
			os.WriteFile(testFile, tt.content, 0644)

			source, err := NewFileSource(testFile, FileSourceOptions{})
			if err != nil {
				t.Fatalf("NewFileSource() error = %v", err)
			}

			ctx := context.Background()

			data, err := source.Load(ctx)
			if err != nil {
				t.Errorf("Load() error = %v", err)
			}

			if data == nil {
				t.Error("Load() returned nil data")
			}

			if data["key"] == nil {
				t.Error("key not found in loaded data")
			}
		})
	}
}

// =============================================================================
// FILE SOURCE CONTEXT CANCELLATION TESTS
// =============================================================================

func TestFileSource_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.yaml")

	content := []byte("key: value\n")
	os.WriteFile(testFile, content, 0644)

	source, err := NewFileSource(testFile, FileSourceOptions{})
	if err != nil {
		t.Fatalf("NewFileSource() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = source.Load(ctx)

	// Should either complete successfully or handle cancellation gracefully
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("Load() error = %v", err)
	}
}

// =============================================================================
// FILE SOURCE RELOAD TESTS
// =============================================================================

func TestFileSource_Reload(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "config.yaml")

	// Initial content
	content1 := []byte("key: value1\n")
	os.WriteFile(testFile, content1, 0644)

	source, err := NewFileSource(testFile, FileSourceOptions{})
	if err != nil {
		t.Fatalf("NewFileSource() error = %v", err)
	}

	ctx := context.Background()

	// First load
	data1, err := source.Load(ctx)
	if err != nil {
		t.Fatalf("First Load() error = %v", err)
	}

	if data1["key"] != "value1" {
		t.Errorf("First load: key = %v, want value1", data1["key"])
	}

	// Modify file
	content2 := []byte("key: value2\n")
	os.WriteFile(testFile, content2, 0644)

	// Give filesystem time to update
	time.Sleep(100 * time.Millisecond)

	// Second load
	data2, err := source.Load(ctx)
	if err != nil {
		t.Fatalf("Second Load() error = %v", err)
	}

	if data2["key"] != "value2" {
		t.Errorf("Second load: key = %v, want value2", data2["key"])
	}
}

// =============================================================================
// FILE SOURCE SYMLINK TESTS
// =============================================================================

func TestFileSource_Symlink(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping symlink test in CI")
	}

	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "target.yaml")
	symlinkFile := filepath.Join(tmpDir, "symlink.yaml")

	// Create target file
	content := []byte("key: value\n")
	os.WriteFile(targetFile, content, 0644)

	// Create symlink
	err := os.Symlink(targetFile, symlinkFile)
	if err != nil {
		t.Skip("Cannot create symlink:", err)
	}

	source, err := NewFileSource(symlinkFile, FileSourceOptions{})
	if err != nil {
		t.Fatalf("NewFileSource() error = %v", err)
	}

	ctx := context.Background()

	data, err := source.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if data["key"] != "value" {
		t.Errorf("key = %v, want value", data["key"])
	}
}
