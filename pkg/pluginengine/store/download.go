package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	json "github.com/json-iterator/go"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	plugins "github.com/xraph/forge/pkg/pluginengine/common"
)

// PluginDownloader handles plugin download and caching
type PluginDownloader struct {
	config      StoreConfig
	cache       *DownloadCache
	httpClient  HTTPClient
	logger      common.Logger
	metrics     common.Metrics
	mu          sync.RWMutex
	initialized bool
}

// DownloadCache manages downloaded plugin packages
type DownloadCache struct {
	cacheDir     string
	maxSize      int64
	currentSize  int64
	entries      map[string]*CacheEntry
	mu           sync.RWMutex
	cleanupTimer *time.Timer
}

// CacheEntry represents a cached plugin package
type CacheEntry struct {
	PluginID    string    `json:"plugin_id"`
	Version     string    `json:"version"`
	FilePath    string    `json:"file_path"`
	Size        int64     `json:"size"`
	Checksum    string    `json:"checksum"`
	CachedAt    time.Time `json:"cached_at"`
	LastAccess  time.Time `json:"last_access"`
	AccessCount int64     `json:"access_count"`
}

// DownloadProgress tracks download progress
type DownloadProgress struct {
	PluginID       string        `json:"plugin_id"`
	Version        string        `json:"version"`
	TotalSize      int64         `json:"total_size"`
	DownloadedSize int64         `json:"downloaded_size"`
	Progress       float64       `json:"progress"`
	Speed          int64         `json:"speed"`
	ETA            time.Duration `json:"eta"`
	StartTime      time.Time     `json:"start_time"`
}

// DownloadStats contains download statistics
type DownloadStats struct {
	TotalDownloads       int64         `json:"total_downloads"`
	SuccessfulDownloads  int64         `json:"successful_downloads"`
	FailedDownloads      int64         `json:"failed_downloads"`
	TotalBytesDownloaded int64         `json:"total_bytes_downloaded"`
	AverageDownloadTime  time.Duration `json:"average_download_time"`
	CacheHitRate         float64       `json:"cache_hit_rate"`
	CacheSize            int64         `json:"cache_size"`
}

// NewPluginDownloader creates a new plugin downloader
func NewPluginDownloader(config StoreConfig, logger common.Logger, metrics common.Metrics) *PluginDownloader {
	return &PluginDownloader{
		config:     config,
		httpClient: NewDefaultHTTPClient(config.HttpTimeout),
		logger:     logger,
		metrics:    metrics,
	}
}

// Initialize initializes the plugin downloader
func (pd *PluginDownloader) Initialize(ctx context.Context) error {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if pd.initialized {
		return nil
	}

	// Create cache directory
	cacheDir := filepath.Join(pd.config.CacheDirectory, "downloads")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Initialize cache
	pd.cache = &DownloadCache{
		cacheDir:    cacheDir,
		maxSize:     pd.config.MaxCacheSize,
		currentSize: 0,
		entries:     make(map[string]*CacheEntry),
	}

	// Load existing cache entries
	if err := pd.loadCacheIndex(); err != nil {
		pd.logger.Warn("failed to load cache index", logger.Error(err))
	}

	// Start cleanup timer
	pd.cache.cleanupTimer = time.AfterFunc(time.Hour, pd.cleanupCache)

	pd.initialized = true

	pd.logger.Info("plugin downloader initialized",
		logger.String("cache_dir", cacheDir),
		logger.Int64("max_cache_size", pd.config.MaxCacheSize),
		logger.Int("cached_entries", len(pd.cache.entries)),
	)

	if pd.metrics != nil {
		pd.metrics.Counter("forge.plugins.downloader_initialized").Inc()
	}

	return nil
}

// Stop stops the plugin downloader
func (pd *PluginDownloader) Stop(ctx context.Context) error {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if !pd.initialized {
		return nil
	}

	// Stop cleanup timer
	if pd.cache.cleanupTimer != nil {
		pd.cache.cleanupTimer.Stop()
	}

	// Save cache index
	if err := pd.saveCacheIndex(); err != nil {
		pd.logger.Error("failed to save cache index", logger.Error(err))
	}

	pd.initialized = false

	pd.logger.Info("plugin downloader stopped")

	if pd.metrics != nil {
		pd.metrics.Counter("forge.plugins.downloader_stopped").Inc()
	}

	return nil
}

// Download downloads a plugin package
func (pd *PluginDownloader) Download(ctx context.Context, source plugins.PluginEngineSource, progressCallback func(DownloadProgress)) (plugins.PluginEnginePackage, error) {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	if !pd.initialized {
		return plugins.PluginEnginePackage{}, fmt.Errorf("plugin downloader not initialized")
	}

	pluginID := source.Location
	version := source.Version

	// Check cache first
	if cached, found := pd.getCachedPackage(pluginID, version); found {
		pd.logger.Debug("plugin package found in cache",
			logger.String("plugin_id", pluginID),
			logger.String("version", version),
		)

		if pd.metrics != nil {
			pd.metrics.Counter("forge.plugins.cache_hit").Inc()
		}

		return cached, nil
	}

	if pd.metrics != nil {
		pd.metrics.Counter("forge.plugins.cache_miss").Inc()
	}

	// Download from source
	startTime := time.Now()
	pkg, err := pd.downloadFromSource(ctx, source, progressCallback)
	if err != nil {
		if pd.metrics != nil {
			pd.metrics.Counter("forge.plugins.download_failed").Inc()
		}
		return plugins.PluginEnginePackage{}, err
	}

	// Cache the package
	if err := pd.cachePackage(pluginID, version, pkg); err != nil {
		pd.logger.Warn("failed to cache plugin package",
			logger.String("plugin_id", pluginID),
			logger.String("version", version),
			logger.Error(err),
		)
	}

	downloadTime := time.Since(startTime)
	pd.logger.Info("plugin package downloaded",
		logger.String("plugin_id", pluginID),
		logger.String("version", version),
		logger.Int("size", len(pkg.Binary)),
		logger.Duration("download_time", downloadTime),
	)

	if pd.metrics != nil {
		pd.metrics.Counter("forge.plugins.download_success").Inc()
		pd.metrics.Histogram("forge.plugins.download_time").Observe(downloadTime.Seconds())
		pd.metrics.Histogram("forge.plugins.download_size").Observe(float64(len(pkg.Binary)))
	}

	return pkg, nil
}

// downloadFromSource downloads plugin from the specified source
func (pd *PluginDownloader) downloadFromSource(ctx context.Context, source plugins.PluginEngineSource, progressCallback func(DownloadProgress)) (plugins.PluginEnginePackage, error) {
	switch source.Type {
	case plugins.PluginEngineSourceTypeURL:
		return pd.downloadFromURL(ctx, source.Location, source.Version, progressCallback)
	case plugins.PluginEngineSourceTypeFile:
		return pd.downloadFromFile(ctx, source.Location, source.Version)
	case plugins.PluginEngineSourceTypeMarketplace:
		return pd.downloadFromMarketplace(ctx, source.Location, source.Version, progressCallback)
	default:
		return plugins.PluginEnginePackage{}, fmt.Errorf("unsupported source type: %s", source.Type)
	}
}

// downloadFromURL downloads plugin from URL
func (pd *PluginDownloader) downloadFromURL(ctx context.Context, url, version string, progressCallback func(DownloadProgress)) (plugins.PluginEnginePackage, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return plugins.PluginEnginePackage{}, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := pd.httpClient.Do(req)
	if err != nil {
		return plugins.PluginEnginePackage{}, fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return plugins.PluginEnginePackage{}, fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	// Get content length for progress tracking
	contentLength := resp.ContentLength
	if contentLength > pd.config.MaxDownloadSize {
		return plugins.PluginEnginePackage{}, fmt.Errorf("download size exceeds limit: %d > %d", contentLength, pd.config.MaxDownloadSize)
	}

	// Download with progress tracking
	var downloadedSize int64
	var buffer []byte
	startTime := time.Now()

	// Create progress reader
	progressReader := &progressReader{
		reader:           resp.Body,
		total:            contentLength,
		downloaded:       &downloadedSize,
		progressCallback: progressCallback,
		pluginID:         url, // Use URL as plugin ID for now
		version:          version,
		startTime:        startTime,
	}

	// Read entire response
	buffer, err = io.ReadAll(progressReader)
	if err != nil {
		return plugins.PluginEnginePackage{}, fmt.Errorf("failed to read response: %w", err)
	}

	// Calculate checksum
	hasher := sha256.New()
	hasher.Write(buffer)
	checksum := hex.EncodeToString(hasher.Sum(nil))

	// Create plugin package
	pkg := plugins.PluginEnginePackage{
		Binary:   buffer,
		Checksum: checksum,
	}

	return pkg, nil
}

// downloadFromFile loads plugin from local file
func (pd *PluginDownloader) downloadFromFile(ctx context.Context, filePath, version string) (plugins.PluginEnginePackage, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return plugins.PluginEnginePackage{}, fmt.Errorf("failed to read file: %w", err)
	}

	// Calculate checksum
	hasher := sha256.New()
	hasher.Write(data)
	checksum := hex.EncodeToString(hasher.Sum(nil))

	pkg := plugins.PluginEnginePackage{
		Binary:   data,
		Checksum: checksum,
	}

	return pkg, nil
}

// downloadFromMarketplace downloads plugin from marketplace
func (pd *PluginDownloader) downloadFromMarketplace(ctx context.Context, pluginID, version string, progressCallback func(DownloadProgress)) (plugins.PluginEnginePackage, error) {
	// Construct marketplace URL
	marketplaceURL := fmt.Sprintf("%s/plugins/%s/versions/%s/download", pd.config.MarketplaceURL, pluginID, version)

	req, err := http.NewRequestWithContext(ctx, "GET", marketplaceURL, nil)
	if err != nil {
		return plugins.PluginEnginePackage{}, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication if available
	if pd.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+pd.config.APIKey)
	}

	resp, err := pd.httpClient.Do(req)
	if err != nil {
		return plugins.PluginEnginePackage{}, fmt.Errorf("failed to download from marketplace: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return plugins.PluginEnginePackage{}, fmt.Errorf("marketplace download failed with status: %d", resp.StatusCode)
	}

	// Download with progress tracking
	var downloadedSize int64
	contentLength := resp.ContentLength
	startTime := time.Now()

	progressReader := &progressReader{
		reader:           resp.Body,
		total:            contentLength,
		downloaded:       &downloadedSize,
		progressCallback: progressCallback,
		pluginID:         pluginID,
		version:          version,
		startTime:        startTime,
	}

	// Read response
	buffer, err := io.ReadAll(progressReader)
	if err != nil {
		return plugins.PluginEnginePackage{}, fmt.Errorf("failed to read response: %w", err)
	}

	// Calculate checksum
	hasher := sha256.New()
	hasher.Write(buffer)
	checksum := hex.EncodeToString(hasher.Sum(nil))

	pkg := plugins.PluginEnginePackage{
		Binary:   buffer,
		Checksum: checksum,
	}

	return pkg, nil
}

// getCachedPackage retrieves a cached plugin package
func (pd *PluginDownloader) getCachedPackage(pluginID, version string) (plugins.PluginEnginePackage, bool) {
	pd.cache.mu.RLock()
	defer pd.cache.mu.RUnlock()

	cacheKey := fmt.Sprintf("%s@%s", pluginID, version)
	entry, exists := pd.cache.entries[cacheKey]
	if !exists {
		return plugins.PluginEnginePackage{}, false
	}

	// Check if cache entry is still valid
	if time.Since(entry.CachedAt) > pd.config.CacheTimeout {
		return plugins.PluginEnginePackage{}, false
	}

	// Check if file still exists
	if _, err := os.Stat(entry.FilePath); os.IsNotExist(err) {
		return plugins.PluginEnginePackage{}, false
	}

	// Load cached package
	data, err := os.ReadFile(entry.FilePath)
	if err != nil {
		return plugins.PluginEnginePackage{}, false
	}

	// Verify checksum
	hasher := sha256.New()
	hasher.Write(data)
	checksum := hex.EncodeToString(hasher.Sum(nil))

	if checksum != entry.Checksum {
		return plugins.PluginEnginePackage{}, false
	}

	// Update access stats
	pd.cache.mu.RUnlock()
	pd.cache.mu.Lock()
	entry.LastAccess = time.Now()
	entry.AccessCount++
	pd.cache.mu.Unlock()
	pd.cache.mu.RLock()

	pkg := plugins.PluginEnginePackage{
		Binary:   data,
		Checksum: checksum,
	}

	return pkg, true
}

// cachePackage caches a plugin package
func (pd *PluginDownloader) cachePackage(pluginID, version string, pkg plugins.PluginEnginePackage) error {
	pd.cache.mu.Lock()
	defer pd.cache.mu.Unlock()

	// Check if we have space
	packageSize := int64(len(pkg.Binary))
	if pd.cache.currentSize+packageSize > pd.cache.maxSize {
		// Clean up old entries
		if err := pd.cleanupOldEntries(packageSize); err != nil {
			return fmt.Errorf("failed to cleanup cache: %w", err)
		}
	}

	// Save to file
	cacheKey := fmt.Sprintf("%s@%s", pluginID, version)
	filePath := filepath.Join(pd.cache.cacheDir, fmt.Sprintf("%s.zip", cacheKey))

	if err := os.WriteFile(filePath, pkg.Binary, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	// Create cache entry
	entry := &CacheEntry{
		PluginID:    pluginID,
		Version:     version,
		FilePath:    filePath,
		Size:        packageSize,
		Checksum:    pkg.Checksum,
		CachedAt:    time.Now(),
		LastAccess:  time.Now(),
		AccessCount: 1,
	}

	pd.cache.entries[cacheKey] = entry
	pd.cache.currentSize += packageSize

	return nil
}

// cleanupOldEntries removes old cache entries to make space
func (pd *PluginDownloader) cleanupOldEntries(requiredSpace int64) error {
	// Sort entries by last access time
	var entries []*CacheEntry
	for _, entry := range pd.cache.entries {
		entries = append(entries, entry)
	}

	// Sort by last access time (oldest first)
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[i].LastAccess.After(entries[j].LastAccess) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	// Remove oldest entries until we have enough space
	spaceFreed := int64(0)
	for _, entry := range entries {
		if spaceFreed >= requiredSpace {
			break
		}

		// Remove file
		if err := os.Remove(entry.FilePath); err != nil {
			continue
		}

		// Remove from cache
		cacheKey := fmt.Sprintf("%s@%s", entry.PluginID, entry.Version)
		delete(pd.cache.entries, cacheKey)
		pd.cache.currentSize -= entry.Size
		spaceFreed += entry.Size
	}

	return nil
}

// cleanupCache performs periodic cache cleanup
func (pd *PluginDownloader) cleanupCache() {
	pd.cache.mu.Lock()
	defer pd.cache.mu.Unlock()

	// Remove expired entries
	for key, entry := range pd.cache.entries {
		if time.Since(entry.CachedAt) > pd.config.CacheTimeout {
			// Remove file
			if err := os.Remove(entry.FilePath); err != nil {
				pd.logger.Warn("failed to remove expired cache file",
					logger.String("file", entry.FilePath),
					logger.Error(err),
				)
			}

			// Remove from cache
			delete(pd.cache.entries, key)
			pd.cache.currentSize -= entry.Size
		}
	}

	// Schedule next cleanup
	pd.cache.cleanupTimer = time.AfterFunc(time.Hour, pd.cleanupCache)
}

// loadCacheIndex loads the cache index from disk
func (pd *PluginDownloader) loadCacheIndex() error {
	indexPath := filepath.Join(pd.cache.cacheDir, "index.json")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		return nil
	}

	data, err := os.ReadFile(indexPath)
	if err != nil {
		return fmt.Errorf("failed to read cache index: %w", err)
	}

	var entries map[string]*CacheEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("failed to unmarshal cache index: %w", err)
	}

	pd.cache.entries = entries

	// Calculate current size
	pd.cache.currentSize = 0
	for _, entry := range entries {
		pd.cache.currentSize += entry.Size
	}

	return nil
}

// saveCacheIndex saves the cache index to disk
func (pd *PluginDownloader) saveCacheIndex() error {
	indexPath := filepath.Join(pd.cache.cacheDir, "index.json")

	data, err := json.MarshalIndent(pd.cache.entries, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache index: %w", err)
	}

	if err := os.WriteFile(indexPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache index: %w", err)
	}

	return nil
}

// GetStats returns download statistics
func (pd *PluginDownloader) GetStats() DownloadStats {
	pd.cache.mu.RLock()
	defer pd.cache.mu.RUnlock()

	var totalAccesses int64
	var cacheHits int64

	for _, entry := range pd.cache.entries {
		totalAccesses += entry.AccessCount
		if entry.AccessCount > 1 {
			cacheHits += entry.AccessCount - 1
		}
	}

	cacheHitRate := 0.0
	if totalAccesses > 0 {
		cacheHitRate = float64(cacheHits) / float64(totalAccesses)
	}

	return DownloadStats{
		CacheHitRate: cacheHitRate,
		CacheSize:    pd.cache.currentSize,
	}
}

// progressReader wraps an io.Reader to track download progress
type progressReader struct {
	reader           io.Reader
	total            int64
	downloaded       *int64
	progressCallback func(DownloadProgress)
	pluginID         string
	version          string
	startTime        time.Time
	lastUpdate       time.Time
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	*pr.downloaded += int64(n)

	// Update progress every 100ms
	if time.Since(pr.lastUpdate) > 100*time.Millisecond {
		pr.updateProgress()
		pr.lastUpdate = time.Now()
	}

	return n, err
}

func (pr *progressReader) updateProgress() {
	if pr.progressCallback == nil {
		return
	}

	elapsed := time.Since(pr.startTime)
	progress := float64(*pr.downloaded) / float64(pr.total) * 100
	speed := int64(float64(*pr.downloaded) / elapsed.Seconds())

	eta := time.Duration(0)
	if speed > 0 {
		remaining := pr.total - *pr.downloaded
		eta = time.Duration(remaining/speed) * time.Second
	}

	pr.progressCallback(DownloadProgress{
		PluginID:       pr.pluginID,
		Version:        pr.version,
		TotalSize:      pr.total,
		DownloadedSize: *pr.downloaded,
		Progress:       progress,
		Speed:          speed,
		ETA:            eta,
		StartTime:      pr.startTime,
	})
}
