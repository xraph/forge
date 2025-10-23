package storage

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xraph/forge"
)

// LocalBackend implements storage using local filesystem
type LocalBackend struct {
	rootDir string
	baseURL string
	secret  string // For presigned URLs
	logger  forge.Logger
	metrics forge.Metrics
}

// NewLocalBackend creates a new local filesystem backend
func NewLocalBackend(config map[string]interface{}, logger forge.Logger, metrics forge.Metrics) (*LocalBackend, error) {
	rootDir, ok := config["root_dir"].(string)
	if !ok {
		rootDir = "./storage"
	}

	baseURL, _ := config["base_url"].(string)
	if baseURL == "" {
		baseURL = "http://localhost:8080/files"
	}

	secret, _ := config["secret"].(string)
	if secret == "" {
		secret = "default-secret" // In production, generate a secure secret
	}

	// Create root directory if it doesn't exist
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create root directory: %w", err)
	}

	return &LocalBackend{
		rootDir: rootDir,
		baseURL: baseURL,
		secret:  secret,
		logger:  logger,
		metrics: metrics,
	}, nil
}

// Upload uploads a file
func (b *LocalBackend) Upload(ctx context.Context, key string, data io.Reader, opts ...UploadOption) error {
	start := time.Now()

	options := applyUploadOptions(opts...)

	// Validate key
	if strings.Contains(key, "..") {
		return ErrInvalidKey
	}

	// Create full path
	path := filepath.Join(b.rootDir, key)
	dir := filepath.Dir(path)

	// Create directory structure
	if err := os.MkdirAll(dir, 0755); err != nil {
		b.metrics.Counter("storage_upload_errors", "backend", "local").Inc()
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create file
	file, err := os.Create(path)
	if err != nil {
		b.metrics.Counter("storage_upload_errors", "backend", "local").Inc()
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Copy data
	written, err := io.Copy(file, data)
	if err != nil {
		b.metrics.Counter("storage_upload_errors", "backend", "local").Inc()
		return fmt.Errorf("failed to write data: %w", err)
	}

	// Store metadata (if provided)
	if len(options.Metadata) > 0 {
		b.saveMetadata(path, options.Metadata)
	}

	duration := time.Since(start)
	b.metrics.Histogram("storage_upload_duration", "backend", "local").Observe(duration.Seconds())
	b.metrics.Counter("storage_uploads", "backend", "local").Inc()
	b.metrics.Counter("storage_upload_bytes", "backend", "local").Add(float64(written))

	b.logger.Info("file uploaded",
		forge.F("key", key),
		forge.F("size", written),
		forge.F("duration", duration),
	)

	return nil
}

// Download downloads a file
func (b *LocalBackend) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	path := filepath.Join(b.rootDir, key)

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrObjectNotFound
		}
		b.metrics.Counter("storage_download_errors", "backend", "local").Inc()
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	b.metrics.Counter("storage_downloads", "backend", "local").Inc()

	return file, nil
}

// Delete deletes a file
func (b *LocalBackend) Delete(ctx context.Context, key string) error {
	path := filepath.Join(b.rootDir, key)

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return ErrObjectNotFound
		}
		b.metrics.Counter("storage_delete_errors", "backend", "local").Inc()
		return fmt.Errorf("failed to delete file: %w", err)
	}

	// Also delete metadata file if exists
	metaPath := path + ".meta"
	os.Remove(metaPath)

	b.metrics.Counter("storage_deletes", "backend", "local").Inc()

	return nil
}

// List lists files with a prefix
func (b *LocalBackend) List(ctx context.Context, prefix string, opts ...ListOption) ([]Object, error) {
	options := applyListOptions(opts...)

	basePath := filepath.Join(b.rootDir, prefix)
	var objects []Object

	err := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			if !options.Recursive && path != basePath {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip metadata files
		if strings.HasSuffix(path, ".meta") {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(b.rootDir, path)
		if err != nil {
			return err
		}

		// Calculate ETag (MD5)
		etag := b.calculateETag(path)

		objects = append(objects, Object{
			Key:          filepath.ToSlash(relPath),
			Size:         info.Size(),
			LastModified: info.ModTime(),
			ETag:         etag,
			ContentType:  b.getContentType(path),
		})

		// Check limit
		if options.Limit > 0 && len(objects) >= options.Limit {
			return filepath.SkipAll
		}

		return nil
	})

	if err != nil && err != filepath.SkipAll {
		return nil, err
	}

	return objects, nil
}

// Metadata retrieves object metadata
func (b *LocalBackend) Metadata(ctx context.Context, key string) (*ObjectMetadata, error) {
	path := filepath.Join(b.rootDir, key)

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrObjectNotFound
		}
		return nil, err
	}

	metadata := &ObjectMetadata{
		Key:          key,
		Size:         info.Size(),
		LastModified: info.ModTime(),
		ETag:         b.calculateETag(path),
		ContentType:  b.getContentType(path),
		Metadata:     b.loadMetadata(path),
	}

	return metadata, nil
}

// Exists checks if an object exists
func (b *LocalBackend) Exists(ctx context.Context, key string) (bool, error) {
	path := filepath.Join(b.rootDir, key)
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Copy copies a file
func (b *LocalBackend) Copy(ctx context.Context, srcKey, dstKey string) error {
	srcPath := filepath.Join(b.rootDir, srcKey)
	dstPath := filepath.Join(b.rootDir, dstKey)

	// Create destination directory
	dstDir := filepath.Dir(dstPath)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}

	// Copy file
	src, err := os.Open(srcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrObjectNotFound
		}
		return err
	}
	defer src.Close()

	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}

	return nil
}

// Move moves a file
func (b *LocalBackend) Move(ctx context.Context, srcKey, dstKey string) error {
	srcPath := filepath.Join(b.rootDir, srcKey)
	dstPath := filepath.Join(b.rootDir, dstKey)

	// Create destination directory
	dstDir := filepath.Dir(dstPath)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}

	// Rename (move) file
	if err := os.Rename(srcPath, dstPath); err != nil {
		return err
	}

	return nil
}

// PresignUpload generates a presigned URL for upload
func (b *LocalBackend) PresignUpload(ctx context.Context, key string, expiry time.Duration) (string, error) {
	token := b.generateSignedToken(key, expiry)
	expires := time.Now().Add(expiry).Unix()

	url := fmt.Sprintf("%s/%s?token=%s&expires=%d&action=upload",
		b.baseURL, key, token, expires)

	b.metrics.Counter("storage_presigned_urls", "backend", "local", "type", "upload").Inc()

	return url, nil
}

// PresignDownload generates a presigned URL for download
func (b *LocalBackend) PresignDownload(ctx context.Context, key string, expiry time.Duration) (string, error) {
	token := b.generateSignedToken(key, expiry)
	expires := time.Now().Add(expiry).Unix()

	url := fmt.Sprintf("%s/%s?token=%s&expires=%d",
		b.baseURL, key, token, expires)

	b.metrics.Counter("storage_presigned_urls", "backend", "local", "type", "download").Inc()

	return url, nil
}

// Helper: Calculate ETag (MD5 hash)
func (b *LocalBackend) calculateETag(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return ""
	}

	return hex.EncodeToString(hash.Sum(nil))
}

// Helper: Get content type from file extension
func (b *LocalBackend) getContentType(path string) string {
	ext := filepath.Ext(path)
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".pdf":
		return "application/pdf"
	case ".txt":
		return "text/plain"
	case ".html":
		return "text/html"
	case ".json":
		return "application/json"
	default:
		return "application/octet-stream"
	}
}

// Helper: Generate signed token for presigned URLs
func (b *LocalBackend) generateSignedToken(key string, expiry time.Duration) string {
	expires := time.Now().Add(expiry).Unix()
	data := fmt.Sprintf("%s:%d:%s", key, expires, b.secret)

	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// Helper: Save metadata to file
func (b *LocalBackend) saveMetadata(path string, metadata map[string]string) error {
	if len(metadata) == 0 {
		return nil
	}

	metaPath := path + ".meta"
	file, err := os.Create(metaPath)
	if err != nil {
		return err
	}
	defer file.Close()

	for key, value := range metadata {
		fmt.Fprintf(file, "%s=%s\n", key, value)
	}

	return nil
}

// Helper: Load metadata from file
func (b *LocalBackend) loadMetadata(path string) map[string]string {
	metadata := make(map[string]string)

	metaPath := path + ".meta"
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return metadata
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			metadata[parts[0]] = parts[1]
		}
	}

	return metadata
}
