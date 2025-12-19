package storage

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/internal/logger"
)

func TestExtension_Implementation(t *testing.T) {
	var _ forge.Extension = (*Extension)(nil)
}

func TestExtension_BasicInfo(t *testing.T) {
	ext := NewExtension(DefaultConfig())

	if ext.Name() != "storage" {
		t.Errorf("expected name 'storage', got %s", ext.Name())
	}

	if ext.Version() != "2.0.0" {
		t.Errorf("expected version '2.0.0', got %s", ext.Version())
	}

	if ext.Description() == "" {
		t.Error("expected non-empty description")
	}

	deps := ext.Dependencies()
	if len(deps) != 0 {
		t.Errorf("expected 0 dependencies, got %d", len(deps))
	}
}

func TestExtension_Lifecycle(t *testing.T) {
	// Create test directory
	testDir := filepath.Join(os.TempDir(), "forge-storage-test")
	defer os.RemoveAll(testDir)

	// Create test app
	app := forge.New(
		forge.WithAppName("test-app"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(logger.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
	)

	// Create extension with local backend
	config := Config{
		Default: "local",
		Backends: map[string]BackendConfig{
			"local": {
				Type: BackendTypeLocal,
				Config: map[string]any{
					"root_dir": testDir,
					"base_url": "http://localhost:8080/files",
				},
			},
		},
		EnablePresignedURLs: true,
		PresignExpiry:       15 * time.Minute,
	}

	ext := NewExtension(config)

	// Register extension
	if err := ext.Register(app); err != nil {
		t.Fatalf("failed to register extension: %v", err)
	}

	// Start extension
	ctx := context.Background()
	if err := ext.Start(ctx); err != nil {
		t.Fatalf("failed to start extension: %v", err)
	}

	// Health check
	if err := ext.Health(ctx); err != nil {
		t.Errorf("health check failed: %v", err)
	}

	// Get storage manager
	manager := MustGetManager(app.Container())
	if manager == nil {
		t.Fatal("storage manager not found")
	}

	// Test upload
	content := []byte("test content")

	err := manager.Upload(ctx, "test/file.txt", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("failed to upload: %v", err)
	}

	// Test download
	reader, err := manager.Download(ctx, "test/file.txt")
	if err != nil {
		t.Fatalf("failed to download: %v", err)
	}
	defer reader.Close()

	downloaded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read downloaded content: %v", err)
	}

	if !bytes.Equal(content, downloaded) {
		t.Errorf("content mismatch: expected %s, got %s", content, downloaded)
	}

	// Stop extension
	if err := ext.Stop(ctx); err != nil {
		t.Errorf("failed to stop extension: %v", err)
	}
}

func TestLocalBackend_Upload(t *testing.T) {
	testDir := filepath.Join(os.TempDir(), "storage-upload-test")
	defer os.RemoveAll(testDir)

	log := logger.NewTestLogger()
	metrics := forge.NewNoOpMetrics()

	backend, err := NewLocalBackend(map[string]any{
		"root_dir": testDir,
		"base_url": "http://localhost:8080/files",
	}, log, metrics)
	if err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	ctx := context.Background()

	// Test upload
	content := []byte("hello world")

	err = backend.Upload(ctx, "test/file.txt", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("failed to upload: %v", err)
	}

	// Verify file exists
	path := filepath.Join(testDir, "test/file.txt")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("file does not exist after upload")
	}

	// Verify content
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	if !bytes.Equal(content, data) {
		t.Errorf("content mismatch: expected %s, got %s", content, data)
	}
}

func TestLocalBackend_Download(t *testing.T) {
	testDir := filepath.Join(os.TempDir(), "storage-download-test")
	defer os.RemoveAll(testDir)

	log := logger.NewTestLogger()
	metrics := forge.NewNoOpMetrics()

	backend, err := NewLocalBackend(map[string]any{
		"root_dir": testDir,
	}, log, metrics)
	if err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	ctx := context.Background()

	// Upload file first
	content := []byte("download test")
	backend.Upload(ctx, "download/file.txt", bytes.NewReader(content))

	// Download file
	reader, err := backend.Download(ctx, "download/file.txt")
	if err != nil {
		t.Fatalf("failed to download: %v", err)
	}
	defer reader.Close()

	// Read content
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	if !bytes.Equal(content, data) {
		t.Errorf("content mismatch: expected %s, got %s", content, data)
	}

	// Test download non-existent file
	_, err = backend.Download(ctx, "nonexistent.txt")
	if !errors.Is(err, ErrObjectNotFound) {
		t.Errorf("expected ErrObjectNotFound, got %v", err)
	}
}

func TestLocalBackend_Delete(t *testing.T) {
	testDir := filepath.Join(os.TempDir(), "storage-delete-test")
	defer os.RemoveAll(testDir)

	log := logger.NewTestLogger()
	metrics := forge.NewNoOpMetrics()

	backend, err := NewLocalBackend(map[string]any{
		"root_dir": testDir,
	}, log, metrics)
	if err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	ctx := context.Background()

	// Upload file
	backend.Upload(ctx, "delete/file.txt", strings.NewReader("delete me"))

	// Verify exists
	exists, err := backend.Exists(ctx, "delete/file.txt")
	if err != nil {
		t.Fatalf("failed to check existence: %v", err)
	}

	if !exists {
		t.Error("file should exist before delete")
	}

	// Delete file
	err = backend.Delete(ctx, "delete/file.txt")
	if err != nil {
		t.Fatalf("failed to delete: %v", err)
	}

	// Verify deleted
	exists, err = backend.Exists(ctx, "delete/file.txt")
	if err != nil {
		t.Fatalf("failed to check existence: %v", err)
	}

	if exists {
		t.Error("file should not exist after delete")
	}

	// Test delete non-existent file
	err = backend.Delete(ctx, "nonexistent.txt")
	if !errors.Is(err, ErrObjectNotFound) {
		t.Errorf("expected ErrObjectNotFound, got %v", err)
	}
}

func TestLocalBackend_List(t *testing.T) {
	testDir := filepath.Join(os.TempDir(), "storage-list-test")
	defer os.RemoveAll(testDir)

	log := logger.NewTestLogger()
	metrics := forge.NewNoOpMetrics()

	backend, err := NewLocalBackend(map[string]any{
		"root_dir": testDir,
	}, log, metrics)
	if err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	ctx := context.Background()

	// Upload multiple files
	files := []string{
		"dir1/file1.txt",
		"dir1/file2.txt",
		"dir1/subdir/file3.txt",
		"dir2/file4.txt",
	}

	for _, key := range files {
		backend.Upload(ctx, key, strings.NewReader("content"))
	}

	// List all files
	objects, err := backend.List(ctx, "", WithRecursive(true))
	if err != nil {
		t.Fatalf("failed to list: %v", err)
	}

	if len(objects) != len(files) {
		t.Errorf("expected %d files, got %d", len(files), len(objects))
	}

	// List with prefix
	objects, err = backend.List(ctx, "dir1", WithRecursive(true))
	if err != nil {
		t.Fatalf("failed to list with prefix: %v", err)
	}

	if len(objects) != 3 {
		t.Errorf("expected 3 files in dir1, got %d", len(objects))
	}

	// List with limit
	objects, err = backend.List(ctx, "", WithLimit(2), WithRecursive(true))
	if err != nil {
		t.Fatalf("failed to list with limit: %v", err)
	}

	if len(objects) > 2 {
		t.Errorf("expected at most 2 files, got %d", len(objects))
	}
}

func TestLocalBackend_Metadata(t *testing.T) {
	testDir := filepath.Join(os.TempDir(), "storage-metadata-test")
	defer os.RemoveAll(testDir)

	log := logger.NewTestLogger()
	metrics := forge.NewNoOpMetrics()

	backend, err := NewLocalBackend(map[string]any{
		"root_dir": testDir,
	}, log, metrics)
	if err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	ctx := context.Background()

	// Upload file with metadata
	content := []byte("metadata test")
	metadata := map[string]string{
		"user_id":  "123",
		"uploaded": time.Now().Format(time.RFC3339),
	}

	err = backend.Upload(ctx, "meta/file.txt", bytes.NewReader(content),
		WithMetadata(metadata))
	if err != nil {
		t.Fatalf("failed to upload: %v", err)
	}

	// Get metadata
	meta, err := backend.Metadata(ctx, "meta/file.txt")
	if err != nil {
		t.Fatalf("failed to get metadata: %v", err)
	}

	if meta.Key != "meta/file.txt" {
		t.Errorf("expected key meta/file.txt, got %s", meta.Key)
	}

	if meta.Size != int64(len(content)) {
		t.Errorf("expected size %d, got %d", len(content), meta.Size)
	}

	if meta.ETag == "" {
		t.Error("expected non-empty ETag")
	}

	// Test metadata non-existent file
	_, err = backend.Metadata(ctx, "nonexistent.txt")
	if !errors.Is(err, ErrObjectNotFound) {
		t.Errorf("expected ErrObjectNotFound, got %v", err)
	}
}

func TestLocalBackend_CopyMove(t *testing.T) {
	testDir := filepath.Join(os.TempDir(), "storage-copymove-test")
	defer os.RemoveAll(testDir)

	log := logger.NewTestLogger()
	metrics := forge.NewNoOpMetrics()

	backend, err := NewLocalBackend(map[string]any{
		"root_dir": testDir,
	}, log, metrics)
	if err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	ctx := context.Background()

	// Upload original file
	content := []byte("copy me")
	backend.Upload(ctx, "original.txt", bytes.NewReader(content))

	// Test Copy
	err = backend.Copy(ctx, "original.txt", "copied.txt")
	if err != nil {
		t.Fatalf("failed to copy: %v", err)
	}

	// Verify both exist
	exists, _ := backend.Exists(ctx, "original.txt")
	if !exists {
		t.Error("original should still exist after copy")
	}

	exists, _ = backend.Exists(ctx, "copied.txt")
	if !exists {
		t.Error("copied file should exist")
	}

	// Test Move
	err = backend.Move(ctx, "copied.txt", "moved.txt")
	if err != nil {
		t.Fatalf("failed to move: %v", err)
	}

	// Verify source deleted, destination exists
	exists, _ = backend.Exists(ctx, "copied.txt")
	if exists {
		t.Error("copied file should not exist after move")
	}

	exists, _ = backend.Exists(ctx, "moved.txt")
	if !exists {
		t.Error("moved file should exist")
	}
}

func TestLocalBackend_PresignedURLs(t *testing.T) {
	testDir := filepath.Join(os.TempDir(), "storage-presign-test")
	defer os.RemoveAll(testDir)

	log := logger.NewTestLogger()
	metrics := forge.NewNoOpMetrics()

	backend, err := NewLocalBackend(map[string]any{
		"root_dir": testDir,
		"base_url": "http://localhost:8080/files",
	}, log, metrics)
	if err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	ctx := context.Background()

	// Test presigned upload URL
	uploadURL, err := backend.PresignUpload(ctx, "upload/file.txt", 15*time.Minute)
	if err != nil {
		t.Fatalf("failed to generate presigned upload URL: %v", err)
	}

	if uploadURL == "" {
		t.Error("expected non-empty upload URL")
	}

	if !strings.Contains(uploadURL, "http://localhost:8080/files") {
		t.Error("upload URL should contain base URL")
	}

	if !strings.Contains(uploadURL, "token=") {
		t.Error("upload URL should contain token")
	}

	if !strings.Contains(uploadURL, "expires=") {
		t.Error("upload URL should contain expiry")
	}

	// Upload file for download test
	backend.Upload(ctx, "download/file.txt", strings.NewReader("download"))

	// Test presigned download URL
	downloadURL, err := backend.PresignDownload(ctx, "download/file.txt", 1*time.Hour)
	if err != nil {
		t.Fatalf("failed to generate presigned download URL: %v", err)
	}

	if downloadURL == "" {
		t.Error("expected non-empty download URL")
	}

	if !strings.Contains(downloadURL, "http://localhost:8080/files") {
		t.Error("download URL should contain base URL")
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name:    "empty config",
			config:  Config{},
			wantErr: true,
		},
		{
			name: "valid config",
			config: Config{
				Default: "local",
				Backends: map[string]BackendConfig{
					"local": {
						Type:   BackendTypeLocal,
						Config: map[string]any{},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "no default backend",
			config: Config{
				Backends: map[string]BackendConfig{
					"local": {
						Type: "local",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "default backend not found",
			config: Config{
				Default: "s3",
				Backends: map[string]BackendConfig{
					"local": {
						Type: "local",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "missing backend type",
			config: Config{
				Default: "local",
				Backends: map[string]BackendConfig{
					"local": {
						Config: map[string]any{},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if len(config.Backends) == 0 {
		t.Error("default config should have at least one backend")
	}

	if config.Default != "local" {
		t.Error("default config should use local backend")
	}

	// Should be valid
	if err := config.Validate(); err != nil {
		t.Errorf("default config should be valid: %v", err)
	}
}

func TestStorageManager_MultipleBackends(t *testing.T) {
	testDir1 := filepath.Join(os.TempDir(), "storage-backend1")
	testDir2 := filepath.Join(os.TempDir(), "storage-backend2")

	defer func() {
		os.RemoveAll(testDir1)
		os.RemoveAll(testDir2)
	}()

	log := logger.NewTestLogger()
	metrics := forge.NewNoOpMetrics()

	config := Config{
		Default: "backend1",
		Backends: map[string]BackendConfig{
			"backend1": {
				Type: "local",
				Config: map[string]any{
					"root_dir": testDir1,
				},
			},
			"backend2": {
				Type: "local",
				Config: map[string]any{
					"root_dir": testDir2,
				},
			},
		},
	}

	manager := NewStorageManager(config, log, metrics)

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("failed to start manager: %v", err)
	}

	// Upload to default backend
	content := []byte("default backend")
	if err := manager.Upload(ctx, "test.txt", bytes.NewReader(content)); err != nil {
		t.Fatalf("failed to upload to default: %v", err)
	}

	// Upload to specific backend
	backend2 := manager.Backend("backend2")
	if backend2 == nil {
		t.Fatal("backend2 not found")
	}

	content2 := []byte("backend2")
	if err := backend2.Upload(ctx, "test.txt", bytes.NewReader(content2)); err != nil {
		t.Fatalf("failed to upload to backend2: %v", err)
	}

	// Verify files are in different locations
	path1 := filepath.Join(testDir1, "test.txt")
	path2 := filepath.Join(testDir2, "test.txt")

	data1, _ := os.ReadFile(path1)
	data2, _ := os.ReadFile(path2)

	if bytes.Equal(data1, data2) {
		t.Error("files in different backends should have different content")
	}
}

func TestUploadOptions(t *testing.T) {
	options := applyUploadOptions(
		WithContentType("image/jpeg"),
		WithMetadata(map[string]string{"user": "123"}),
		WithACL("public-read"),
	)

	if options.ContentType != "image/jpeg" {
		t.Errorf("expected content type image/jpeg, got %s", options.ContentType)
	}

	if options.Metadata["user"] != "123" {
		t.Error("expected user metadata")
	}

	if options.ACL != "public-read" {
		t.Errorf("expected ACL public-read, got %s", options.ACL)
	}
}

func TestListOptions(t *testing.T) {
	options := applyListOptions(
		WithLimit(50),
		WithMarker("abc"),
		WithRecursive(true),
	)

	if options.Limit != 50 {
		t.Errorf("expected limit 50, got %d", options.Limit)
	}

	if options.Marker != "abc" {
		t.Errorf("expected marker abc, got %s", options.Marker)
	}

	if !options.Recursive {
		t.Error("expected recursive true")
	}
}
