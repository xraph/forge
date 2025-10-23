package storage

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/metrics"
)

func TestStorageManager_Start(t *testing.T) {
	config := DefaultConfig()
	testLogger := logger.NewTestLogger()
	testMetrics := metrics.NewMockMetricsCollector()

	manager := NewStorageManager(config, testLogger, testMetrics)

	ctx := context.Background()
	err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// Verify default backend is set
	if manager.defaultBackend == nil {
		t.Error("Default backend not set")
	}

	// Verify health checker is initialized
	if manager.healthChecker == nil {
		t.Error("Health checker not initialized")
	}
}

func TestStorageManager_Upload_Download(t *testing.T) {
	// This is an integration test that requires actual filesystem access
	// Skip if not in integration test mode
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := DefaultConfig()
	config.UseEnhancedBackend = true
	testLogger := logger.NewTestLogger()
	testMetrics := metrics.NewMockMetricsCollector()

	manager := NewStorageManager(config, testLogger, testMetrics)

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// Test upload
	testKey := "test/file.txt"
	testData := "Hello, World!"
	reader := strings.NewReader(testData)

	err := manager.Upload(ctx, testKey, reader, WithContentType("text/plain"))
	if err != nil {
		t.Fatalf("Failed to upload: %v", err)
	}

	// Test download
	rc, err := manager.Download(ctx, testKey)
	if err != nil {
		t.Fatalf("Failed to download: %v", err)
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("Failed to read downloaded data: %v", err)
	}

	if string(data) != testData {
		t.Errorf("Expected data '%s', got '%s'", testData, string(data))
	}

	// Test exists
	exists, err := manager.Exists(ctx, testKey)
	if err != nil {
		t.Fatalf("Failed to check exists: %v", err)
	}
	if !exists {
		t.Error("File should exist")
	}

	// Test metadata
	metadata, err := manager.Metadata(ctx, testKey)
	if err != nil {
		t.Fatalf("Failed to get metadata: %v", err)
	}
	if metadata.Size != int64(len(testData)) {
		t.Errorf("Expected size %d, got %d", len(testData), metadata.Size)
	}

	// Test delete
	err = manager.Delete(ctx, testKey)
	if err != nil {
		t.Fatalf("Failed to delete: %v", err)
	}

	// Verify deleted
	exists, err = manager.Exists(ctx, testKey)
	if err != nil {
		t.Fatalf("Failed to check exists after delete: %v", err)
	}
	if exists {
		t.Error("File should not exist after delete")
	}
}

func TestStorageManager_List(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := DefaultConfig()
	config.UseEnhancedBackend = true
	testLogger := logger.NewTestLogger()
	testMetrics := metrics.NewMockMetricsCollector()

	manager := NewStorageManager(config, testLogger, testMetrics)

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// Upload multiple files
	files := []string{"list_test/file1.txt", "list_test/file2.txt", "list_test/sub/file3.txt"}
	for _, file := range files {
		err := manager.Upload(ctx, file, strings.NewReader("test"), WithContentType("text/plain"))
		if err != nil {
			t.Fatalf("Failed to upload %s: %v", file, err)
		}
	}

	// Test list with prefix
	objects, err := manager.List(ctx, "list_test/", WithRecursive(true))
	if err != nil {
		t.Fatalf("Failed to list: %v", err)
	}

	if len(objects) < len(files) {
		t.Errorf("Expected at least %d objects, got %d", len(files), len(objects))
	}

	// Cleanup
	for _, file := range files {
		manager.Delete(ctx, file)
	}
}

func TestStorageManager_Copy_Move(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := DefaultConfig()
	config.UseEnhancedBackend = true
	testLogger := logger.NewTestLogger()
	testMetrics := metrics.NewMockMetricsCollector()

	manager := NewStorageManager(config, testLogger, testMetrics)

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// Upload source file
	srcKey := "copy_test/source.txt"
	testData := "test data for copy"
	err := manager.Upload(ctx, srcKey, strings.NewReader(testData), WithContentType("text/plain"))
	if err != nil {
		t.Fatalf("Failed to upload source: %v", err)
	}

	// Test copy
	copyKey := "copy_test/copied.txt"
	err = manager.Copy(ctx, srcKey, copyKey)
	if err != nil {
		t.Fatalf("Failed to copy: %v", err)
	}

	// Verify both exist
	srcExists, _ := manager.Exists(ctx, srcKey)
	copyExists, _ := manager.Exists(ctx, copyKey)

	if !srcExists || !copyExists {
		t.Error("Both source and copy should exist after copy")
	}

	// Test move
	moveKey := "copy_test/moved.txt"
	err = manager.Move(ctx, copyKey, moveKey)
	if err != nil {
		t.Fatalf("Failed to move: %v", err)
	}

	// Verify source gone, destination exists
	copyExists, _ = manager.Exists(ctx, copyKey)
	moveExists, _ := manager.Exists(ctx, moveKey)

	if copyExists {
		t.Error("Copy source should not exist after move")
	}
	if !moveExists {
		t.Error("Move destination should exist after move")
	}

	// Cleanup
	manager.Delete(ctx, srcKey)
	manager.Delete(ctx, moveKey)
}

func TestStorageManager_PresignedURLs(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := DefaultConfig()
	config.EnablePresignedURLs = true
	config.UseEnhancedBackend = true
	logger := &mockLogger{}
	metrics := &mockMetrics{}

	manager := NewStorageManager(config, logger, metrics)

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	testKey := "presign_test/file.txt"

	// Test presign upload
	uploadURL, err := manager.PresignUpload(ctx, testKey, 15*time.Minute)
	if err != nil {
		t.Fatalf("Failed to presign upload: %v", err)
	}

	if uploadURL == "" {
		t.Error("Expected non-empty upload URL")
	}

	// Upload file for download test
	err = manager.Upload(ctx, testKey, strings.NewReader("test"), WithContentType("text/plain"))
	if err != nil {
		t.Fatalf("Failed to upload: %v", err)
	}

	// Test presign download
	downloadURL, err := manager.PresignDownload(ctx, testKey, 15*time.Minute)
	if err != nil {
		t.Fatalf("Failed to presign download: %v", err)
	}

	if downloadURL == "" {
		t.Error("Expected non-empty download URL")
	}

	// Cleanup
	manager.Delete(ctx, testKey)
}

func TestStorageManager_Health(t *testing.T) {
	config := DefaultConfig()
	logger := &mockLogger{}
	metrics := &mockMetrics{}

	manager := NewStorageManager(config, logger, metrics)

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// Test basic health check
	err := manager.Health(ctx)
	if err != nil {
		t.Errorf("Health check failed: %v", err)
	}

	// Test detailed health check
	health, err := manager.HealthDetailed(ctx, false)
	if err != nil {
		t.Fatalf("Detailed health check failed: %v", err)
	}

	if !health.Healthy {
		t.Error("Expected healthy status")
	}

	if health.BackendCount == 0 {
		t.Error("Expected at least one backend")
	}

	if health.HealthyCount != health.BackendCount {
		t.Errorf("Expected all backends healthy, got %d/%d", health.HealthyCount, health.BackendCount)
	}
}

func TestStorageManager_Backend(t *testing.T) {
	config := DefaultConfig()
	logger := &mockLogger{}
	metrics := &mockMetrics{}

	manager := NewStorageManager(config, logger, metrics)

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// Test get existing backend
	backend := manager.Backend("local")
	if backend == nil {
		t.Error("Expected local backend to exist")
	}

	// Test get non-existent backend
	backend = manager.Backend("nonexistent")
	if backend != nil {
		t.Error("Expected nil for non-existent backend")
	}
}

func TestStorageManager_InvalidConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{
			name: "no backends",
			config: Config{
				Default:  "local",
				Backends: map[string]BackendConfig{},
			},
		},
		{
			name: "invalid default",
			config: Config{
				Default: "nonexistent",
				Backends: map[string]BackendConfig{
					"local": {Type: "local"},
				},
			},
		},
		{
			name: "invalid backend type",
			config: Config{
				Default: "invalid",
				Backends: map[string]BackendConfig{
					"invalid": {Type: "invalid_type"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &mockLogger{}
			metrics := &mockMetrics{}

			// Validate should fail
			err := tt.config.Validate()
			if err == nil && tt.name != "invalid backend type" {
				t.Error("Expected validation to fail")
			}

			manager := NewStorageManager(tt.config, logger, metrics)
			ctx := context.Background()

			// Start should fail
			err = manager.Start(ctx)
			if err == nil {
				t.Error("Expected start to fail with invalid config")
			}
		})
	}
}

func BenchmarkStorageManager_Upload(b *testing.B) {
	config := DefaultConfig()
	config.UseEnhancedBackend = true
	testLogger := logger.NewTestLogger()
	testMetrics := metrics.NewMockMetricsCollector()

	manager := NewStorageManager(config, testLogger, testMetrics)

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		b.Fatalf("Failed to start manager: %v", err)
	}

	testData := strings.NewReader("benchmark test data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testData.Seek(0, 0)
		key := "bench/test.txt"
		manager.Upload(ctx, key, testData, WithContentType("text/plain"))
	}

	// Cleanup
	manager.Delete(ctx, "bench/test.txt")
}

func BenchmarkStorageManager_Download(b *testing.B) {
	config := DefaultConfig()
	config.UseEnhancedBackend = true
	testLogger := logger.NewTestLogger()
	testMetrics := metrics.NewMockMetricsCollector()

	manager := NewStorageManager(config, testLogger, testMetrics)

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		b.Fatalf("Failed to start manager: %v", err)
	}

	// Upload test file
	testKey := "bench/download.txt"
	testData := strings.NewReader("benchmark test data")
	if err := manager.Upload(ctx, testKey, testData, WithContentType("text/plain")); err != nil {
		b.Fatalf("Failed to upload test file: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rc, err := manager.Download(ctx, testKey)
		if err != nil {
			b.Fatalf("Failed to download: %v", err)
		}
		io.ReadAll(rc)
		rc.Close()
	}

	// Cleanup
	manager.Delete(ctx, testKey)
}
