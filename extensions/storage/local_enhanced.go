package storage

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/errors"
)

// EnhancedLocalBackend implements enhanced local filesystem storage with proper locking and pooling.
type EnhancedLocalBackend struct {
	rootDir    string
	baseURL    string
	secret     string
	logger     forge.Logger
	metrics    forge.Metrics
	validator  *PathValidator
	bufferPool *BufferPool

	// File-level locks to prevent concurrent access issues
	fileLocks sync.Map // map[string]*sync.RWMutex

	// ETag cache to avoid recalculating
	etagCache sync.Map // map[string]string

	// Configuration
	chunkSize     int64
	maxUploadSize int64
}

// EnhancedLocalConfig contains configuration for enhanced local backend.
type EnhancedLocalConfig struct {
	RootDir       string
	BaseURL       string
	Secret        string
	ChunkSize     int64
	MaxUploadSize int64
}

// NewEnhancedLocalBackend creates a new enhanced local filesystem backend.
func NewEnhancedLocalBackend(config map[string]any, logger forge.Logger, metrics forge.Metrics) (*EnhancedLocalBackend, error) {
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
		// Generate a cryptographically secure secret
		secretBytes := make([]byte, 32)
		if _, err := rand.Read(secretBytes); err != nil {
			return nil, fmt.Errorf("failed to generate secret: %w", err)
		}

		secret = hex.EncodeToString(secretBytes)

		logger.Warn("generated random secret for local storage - configure a persistent secret in production")
	}

	chunkSize := int64(5 * 1024 * 1024) // 5MB default
	if cs, ok := config["chunk_size"].(int64); ok && cs > 0 {
		chunkSize = cs
	} else if cs, ok := config["chunk_size"].(int); ok && cs > 0 {
		chunkSize = int64(cs)
	}

	maxUploadSize := int64(5 * 1024 * 1024 * 1024) // 5GB default
	if mus, ok := config["max_upload_size"].(int64); ok && mus > 0 {
		maxUploadSize = mus
	} else if mus, ok := config["max_upload_size"].(int); ok && mus > 0 {
		maxUploadSize = int64(mus)
	}

	// Create root directory if it doesn't exist
	if err := os.MkdirAll(rootDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create root directory: %w", err)
	}

	return &EnhancedLocalBackend{
		rootDir:       rootDir,
		baseURL:       baseURL,
		secret:        secret,
		logger:        logger,
		metrics:       metrics,
		validator:     NewPathValidator(),
		bufferPool:    NewBufferPool(int(chunkSize)),
		chunkSize:     chunkSize,
		maxUploadSize: maxUploadSize,
	}, nil
}

// Upload uploads a file with proper locking and validation.
func (b *EnhancedLocalBackend) Upload(ctx context.Context, key string, data io.Reader, opts ...UploadOption) error {
	start := time.Now()

	// Validate key
	if err := b.validator.ValidateKey(key); err != nil {
		return fmt.Errorf("invalid key: %w", err)
	}

	options := applyUploadOptions(opts...)

	// Validate content type
	if err := b.validator.ValidateContentType(options.ContentType); err != nil {
		return err
	}

	// Validate metadata
	if err := ValidateMetadata(options.Metadata); err != nil {
		return err
	}

	// Create full path
	path := filepath.Join(b.rootDir, key)

	// Get file lock (write lock)
	lock := b.getFileLock(path)

	lock.Lock()
	defer lock.Unlock()

	// Create directory structure
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		b.metrics.Counter("storage_upload_errors", "backend", "local_enhanced", "error", "mkdir").Inc()

		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create temporary file first (atomic write)
	tempPath := path + ".tmp." + b.generateRandomSuffix()

	tempFile, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0640)
	if err != nil {
		b.metrics.Counter("storage_upload_errors", "backend", "local_enhanced", "error", "create").Inc()

		return fmt.Errorf("failed to create temp file: %w", err)
	}

	// Ensure cleanup on error
	var uploadErr error

	defer func() {
		tempFile.Close()

		if uploadErr != nil {
			os.Remove(tempPath)
		}
	}()

	// Copy data using buffer pool for efficient I/O
	buf := b.bufferPool.Get()
	defer b.bufferPool.Put(buf)

	written, uploadErr := b.copyWithLimit(tempFile, data, b.maxUploadSize, buf)
	if uploadErr != nil {
		b.metrics.Counter("storage_upload_errors", "backend", "local_enhanced", "error", "write").Inc()

		return fmt.Errorf("failed to write data: %w", uploadErr)
	}

	// Sync to disk
	if uploadErr = tempFile.Sync(); uploadErr != nil {
		b.metrics.Counter("storage_upload_errors", "backend", "local_enhanced", "error", "sync").Inc()

		return fmt.Errorf("failed to sync file: %w", uploadErr)
	}

	tempFile.Close()

	// Atomic rename
	if uploadErr = os.Rename(tempPath, path); uploadErr != nil {
		b.metrics.Counter("storage_upload_errors", "backend", "local_enhanced", "error", "rename").Inc()

		return fmt.Errorf("failed to rename file: %w", uploadErr)
	}

	// Store metadata (if provided)
	if len(options.Metadata) > 0 {
		if err := b.saveMetadata(path, options.Metadata); err != nil {
			b.logger.Warn("failed to save metadata",
				forge.F("key", key),
				forge.F("error", err.Error()),
			)
		}
	}

	// Invalidate ETag cache
	b.etagCache.Delete(path)

	duration := time.Since(start)
	b.metrics.Histogram("storage_upload_duration", "backend", "local_enhanced").Observe(duration.Seconds())
	b.metrics.Counter("storage_uploads", "backend", "local_enhanced").Inc()
	b.metrics.Counter("storage_upload_bytes", "backend", "local_enhanced").Add(float64(written))

	b.logger.Info("file uploaded",
		forge.F("key", key),
		forge.F("size", written),
		forge.F("duration", duration),
	)

	return nil
}

// Download downloads a file with proper locking.
func (b *EnhancedLocalBackend) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	// Validate key
	if err := b.validator.ValidateKey(key); err != nil {
		return nil, fmt.Errorf("invalid key: %w", err)
	}

	path := filepath.Join(b.rootDir, key)

	// Get file lock (read lock)
	lock := b.getFileLock(path)
	lock.RLock()

	// Open file
	file, err := os.Open(path)
	if err != nil {
		lock.RUnlock()

		if os.IsNotExist(err) {
			return nil, ErrObjectNotFound
		}

		b.metrics.Counter("storage_download_errors", "backend", "local_enhanced").Inc()

		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	b.metrics.Counter("storage_downloads", "backend", "local_enhanced").Inc()

	// Wrap with a reader that releases the lock when closed
	return &lockedReadCloser{
		ReadCloser: file,
		lock:       lock,
	}, nil
}

// Delete deletes a file with proper locking.
func (b *EnhancedLocalBackend) Delete(ctx context.Context, key string) error {
	// Validate key
	if err := b.validator.ValidateKey(key); err != nil {
		return fmt.Errorf("invalid key: %w", err)
	}

	path := filepath.Join(b.rootDir, key)

	// Get file lock (write lock)
	lock := b.getFileLock(path)

	lock.Lock()
	defer lock.Unlock()

	// Delete file
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return ErrObjectNotFound
		}

		b.metrics.Counter("storage_delete_errors", "backend", "local_enhanced").Inc()

		return fmt.Errorf("failed to delete file: %w", err)
	}

	// Also delete metadata file if exists
	metaPath := path + ".meta"
	os.Remove(metaPath)

	// Invalidate cache
	b.etagCache.Delete(path)
	b.fileLocks.Delete(path)

	b.metrics.Counter("storage_deletes", "backend", "local_enhanced").Inc()

	return nil
}

// List lists files with a prefix.
func (b *EnhancedLocalBackend) List(ctx context.Context, prefix string, opts ...ListOption) ([]Object, error) {
	options := applyListOptions(opts...)

	// Validate prefix if provided
	if prefix != "" {
		cleaned := b.validator.SanitizeKey(prefix)
		if cleaned != prefix {
			prefix = cleaned
		}
	}

	basePath := filepath.Join(b.rootDir, prefix)

	var (
		objects []Object
		mu      sync.Mutex
	)

	err := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

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
		if filepath.Ext(path) == ".meta" || filepath.Ext(path) == ".tmp" {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(b.rootDir, path)
		if err != nil {
			return err
		}

		// Calculate ETag with caching
		etag := b.getCachedETag(path)

		mu.Lock()

		objects = append(objects, Object{
			Key:          filepath.ToSlash(relPath),
			Size:         info.Size(),
			LastModified: info.ModTime(),
			ETag:         etag,
			ContentType:  b.getContentType(path),
		})

		// Check limit
		limitReached := options.Limit > 0 && len(objects) >= options.Limit

		mu.Unlock()

		if limitReached {
			return filepath.SkipAll
		}

		return nil
	})

	if err != nil && !errors.Is(err, filepath.SkipAll) {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	return objects, nil
}

// Metadata retrieves object metadata.
func (b *EnhancedLocalBackend) Metadata(ctx context.Context, key string) (*ObjectMetadata, error) {
	// Validate key
	if err := b.validator.ValidateKey(key); err != nil {
		return nil, fmt.Errorf("invalid key: %w", err)
	}

	path := filepath.Join(b.rootDir, key)

	// Get file lock (read lock)
	lock := b.getFileLock(path)

	lock.RLock()
	defer lock.RUnlock()

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrObjectNotFound
		}

		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	metadata := &ObjectMetadata{
		Key:          key,
		Size:         info.Size(),
		LastModified: info.ModTime(),
		ETag:         b.getCachedETag(path),
		ContentType:  b.getContentType(path),
		Metadata:     b.loadMetadata(path),
	}

	return metadata, nil
}

// Exists checks if an object exists.
func (b *EnhancedLocalBackend) Exists(ctx context.Context, key string) (bool, error) {
	// Validate key
	if err := b.validator.ValidateKey(key); err != nil {
		return false, fmt.Errorf("invalid key: %w", err)
	}

	path := filepath.Join(b.rootDir, key)

	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}

		return false, fmt.Errorf("failed to check existence: %w", err)
	}

	return true, nil
}

// Copy copies a file with proper locking.
func (b *EnhancedLocalBackend) Copy(ctx context.Context, srcKey, dstKey string) error {
	// Validate keys
	if err := b.validator.ValidateKey(srcKey); err != nil {
		return fmt.Errorf("invalid source key: %w", err)
	}

	if err := b.validator.ValidateKey(dstKey); err != nil {
		return fmt.Errorf("invalid destination key: %w", err)
	}

	srcPath := filepath.Join(b.rootDir, srcKey)
	dstPath := filepath.Join(b.rootDir, dstKey)

	// Get locks (read source, write dest)
	srcLock := b.getFileLock(srcPath)
	dstLock := b.getFileLock(dstPath)

	srcLock.RLock()
	defer srcLock.RUnlock()

	dstLock.Lock()
	defer dstLock.Unlock()

	// Create destination directory
	dstDir := filepath.Dir(dstPath)
	if err := os.MkdirAll(dstDir, 0750); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Copy file
	src, err := os.Open(srcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrObjectNotFound
		}

		return fmt.Errorf("failed to open source: %w", err)
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0640)
	if err != nil {
		return fmt.Errorf("failed to create destination: %w", err)
	}
	defer dst.Close()

	// Use buffer pool for efficient copy
	buf := b.bufferPool.Get()
	defer b.bufferPool.Put(buf)

	if _, err := io.CopyBuffer(dst, src, buf); err != nil {
		return fmt.Errorf("failed to copy data: %w", err)
	}

	// Sync to disk
	if err := dst.Sync(); err != nil {
		return fmt.Errorf("failed to sync destination: %w", err)
	}

	// Invalidate destination ETag cache
	b.etagCache.Delete(dstPath)

	return nil
}

// Move moves a file with proper locking.
func (b *EnhancedLocalBackend) Move(ctx context.Context, srcKey, dstKey string) error {
	// Validate keys
	if err := b.validator.ValidateKey(srcKey); err != nil {
		return fmt.Errorf("invalid source key: %w", err)
	}

	if err := b.validator.ValidateKey(dstKey); err != nil {
		return fmt.Errorf("invalid destination key: %w", err)
	}

	srcPath := filepath.Join(b.rootDir, srcKey)
	dstPath := filepath.Join(b.rootDir, dstKey)

	// Get locks (write both)
	srcLock := b.getFileLock(srcPath)
	dstLock := b.getFileLock(dstPath)

	srcLock.Lock()
	defer srcLock.Unlock()

	dstLock.Lock()
	defer dstLock.Unlock()

	// Create destination directory
	dstDir := filepath.Dir(dstPath)
	if err := os.MkdirAll(dstDir, 0750); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Rename (move) file
	if err := os.Rename(srcPath, dstPath); err != nil {
		return fmt.Errorf("failed to move file: %w", err)
	}

	// Move metadata file if exists
	srcMetaPath := srcPath + ".meta"

	dstMetaPath := dstPath + ".meta"
	if _, err := os.Stat(srcMetaPath); err == nil {
		os.Rename(srcMetaPath, dstMetaPath)
	}

	// Update caches
	b.etagCache.Delete(srcPath)
	b.etagCache.Delete(dstPath)
	b.fileLocks.Delete(srcPath)

	return nil
}

// PresignUpload generates a presigned URL for upload.
func (b *EnhancedLocalBackend) PresignUpload(ctx context.Context, key string, expiry time.Duration) (string, error) {
	// Validate key
	if err := b.validator.ValidateKey(key); err != nil {
		return "", fmt.Errorf("invalid key: %w", err)
	}

	token := b.generateSignedToken(key, expiry)
	expires := time.Now().Add(expiry).Unix()

	url := fmt.Sprintf("%s/%s?token=%s&expires=%d&action=upload",
		b.baseURL, key, token, expires)

	b.metrics.Counter("storage_presigned_urls", "backend", "local_enhanced", "type", "upload").Inc()

	return url, nil
}

// PresignDownload generates a presigned URL for download.
func (b *EnhancedLocalBackend) PresignDownload(ctx context.Context, key string, expiry time.Duration) (string, error) {
	// Validate key
	if err := b.validator.ValidateKey(key); err != nil {
		return "", fmt.Errorf("invalid key: %w", err)
	}

	token := b.generateSignedToken(key, expiry)
	expires := time.Now().Add(expiry).Unix()

	url := fmt.Sprintf("%s/%s?token=%s&expires=%d",
		b.baseURL, key, token, expires)

	b.metrics.Counter("storage_presigned_urls", "backend", "local_enhanced", "type", "download").Inc()

	return url, nil
}

// Helper: Get or create file lock.
func (b *EnhancedLocalBackend) getFileLock(path string) *sync.RWMutex {
	lock, _ := b.fileLocks.LoadOrStore(path, &sync.RWMutex{})

	return lock.(*sync.RWMutex)
}

// Helper: Copy with size limit.
func (b *EnhancedLocalBackend) copyWithLimit(dst io.Writer, src io.Reader, limit int64, buf []byte) (int64, error) {
	limitedReader := io.LimitReader(src, limit+1)

	written, err := io.CopyBuffer(dst, limitedReader, buf)
	if err != nil {
		return written, err
	}

	if written > limit {
		return written, ErrFileTooLarge
	}

	return written, nil
}

// Helper: Get cached ETag or calculate.
func (b *EnhancedLocalBackend) getCachedETag(path string) string {
	// Check cache
	if etag, ok := b.etagCache.Load(path); ok {
		return etag.(string)
	}

	// Calculate and cache
	etag := b.calculateETag(path)
	if etag != "" {
		b.etagCache.Store(path, etag)
	}

	return etag
}

// Helper: Calculate ETag (MD5 hash).
func (b *EnhancedLocalBackend) calculateETag(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	hash := md5.New()

	buf := b.bufferPool.Get()
	defer b.bufferPool.Put(buf)

	if _, err := io.CopyBuffer(hash, file, buf); err != nil {
		return ""
	}

	return hex.EncodeToString(hash.Sum(nil))
}

// Helper: Get content type from file extension.
func (b *EnhancedLocalBackend) getContentType(path string) string {
	ext := filepath.Ext(path)
	contentTypes := map[string]string{
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".png":  "image/png",
		".gif":  "image/gif",
		".webp": "image/webp",
		".svg":  "image/svg+xml",
		".pdf":  "application/pdf",
		".txt":  "text/plain",
		".html": "text/html",
		".htm":  "text/html",
		".json": "application/json",
		".xml":  "application/xml",
		".css":  "text/css",
		".js":   "application/javascript",
		".zip":  "application/zip",
		".tar":  "application/x-tar",
		".gz":   "application/gzip",
		".mp4":  "video/mp4",
		".mp3":  "audio/mpeg",
		".wav":  "audio/wav",
	}

	if contentType, ok := contentTypes[ext]; ok {
		return contentType
	}

	return "application/octet-stream"
}

// Helper: Generate signed token for presigned URLs.
func (b *EnhancedLocalBackend) generateSignedToken(key string, expiry time.Duration) string {
	expires := time.Now().Add(expiry).Unix()
	data := fmt.Sprintf("%s:%d:%s", key, expires, b.secret)

	hash := sha256.Sum256([]byte(data))

	return hex.EncodeToString(hash[:])
}

// Helper: Generate random suffix for temp files.
func (b *EnhancedLocalBackend) generateRandomSuffix() string {
	bytes := make([]byte, 8)
	rand.Read(bytes)

	return hex.EncodeToString(bytes)
}

// Helper: Save metadata to file.
func (b *EnhancedLocalBackend) saveMetadata(path string, metadata map[string]string) error {
	if len(metadata) == 0 {
		return nil
	}

	metaPath := path + ".meta"

	file, err := os.OpenFile(metaPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0640)
	if err != nil {
		return fmt.Errorf("failed to create metadata file: %w", err)
	}
	defer file.Close()

	for key, value := range metadata {
		if _, err := fmt.Fprintf(file, "%s=%s\n", key, value); err != nil {
			return fmt.Errorf("failed to write metadata: %w", err)
		}
	}

	return file.Sync()
}

// Helper: Load metadata from file.
func (b *EnhancedLocalBackend) loadMetadata(path string) map[string]string {
	metadata := make(map[string]string)

	metaPath := path + ".meta"

	data, err := os.ReadFile(metaPath)
	if err != nil {
		return metadata
	}

	// Parse simple key=value format
	lines := string(data)
	for i := 0; i < len(lines); {
		// Find next line
		eol := i
		for eol < len(lines) && lines[eol] != '\n' {
			eol++
		}

		line := lines[i:eol]
		if len(line) > 0 {
			// Find separator
			sep := -1

			for j := range len(line) {
				if line[j] == '=' {
					sep = j

					break
				}
			}

			if sep > 0 && sep < len(line)-1 {
				key := line[:sep]
				value := line[sep+1:]
				metadata[key] = value
			}
		}

		i = eol + 1
	}

	return metadata
}

// lockedReadCloser wraps a ReadCloser with a lock that is released on Close.
type lockedReadCloser struct {
	io.ReadCloser

	lock *sync.RWMutex
}

func (lrc *lockedReadCloser) Close() error {
	err := lrc.ReadCloser.Close()
	lrc.lock.RUnlock()

	return err
}
