package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	plugins "github.com/xraph/forge/v0/pkg/pluginengine/common"
)

// PluginCache manages caching of plugin information and packages
type PluginCache struct {
	config        StoreConfig
	cacheDir      string
	infoCache     map[string]*CachedPluginInfo
	packageCache  map[string]*CachedPluginPackage
	mu            sync.RWMutex
	logger        common.Logger
	metrics       common.Metrics
	initialized   bool
	totalSize     int64
	lastCleanup   time.Time
	cleanupTicker *time.Ticker
}

// CachedPluginInfo represents cached plugin information
type CachedPluginInfo struct {
	Info        *plugins.PluginEngineInfo `json:"info"`
	CachedAt    time.Time                 `json:"cached_at"`
	ExpiresAt   time.Time                 `json:"expires_at"`
	AccessCount int64                     `json:"access_count"`
	LastAccess  time.Time                 `json:"last_access"`
}

// CachedPluginPackage represents cached plugin package
type CachedPluginPackage struct {
	Package     plugins.PluginEnginePackage `json:"package"`
	CachedAt    time.Time                   `json:"cached_at"`
	ExpiresAt   time.Time                   `json:"expires_at"`
	Size        int64                       `json:"size"`
	AccessCount int64                       `json:"access_count"`
	LastAccess  time.Time                   `json:"last_access"`
	FilePath    string                      `json:"file_path"`
}

// CacheStats contains cache statistics
type CacheStats struct {
	TotalItems     int       `json:"total_items"`
	InfoItems      int       `json:"info_items"`
	PackageItems   int       `json:"package_items"`
	TotalSize      int64     `json:"total_size"`
	HitRate        float64   `json:"hit_rate"`
	MissRate       float64   `json:"miss_rate"`
	LastCleanup    time.Time `json:"last_cleanup"`
	CacheDirectory string    `json:"cache_directory"`
	MaxSize        int64     `json:"max_size"`
	CurrentSize    int64     `json:"current_size"`
	EvictionCount  int64     `json:"eviction_count"`
	AccessCount    int64     `json:"access_count"`
	HitCount       int64     `json:"hit_count"`
	MissCount      int64     `json:"miss_count"`
}

// NewPluginCache creates a new plugin cache
func NewPluginCache(config StoreConfig, logger common.Logger, metrics common.Metrics) *PluginCache {
	cacheDir := filepath.Join(config.CacheDirectory, "cache")

	return &PluginCache{
		config:       config,
		cacheDir:     cacheDir,
		infoCache:    make(map[string]*CachedPluginInfo),
		packageCache: make(map[string]*CachedPluginPackage),
		logger:       logger,
		metrics:      metrics,
		lastCleanup:  time.Now(),
	}
}

// Initialize initializes the plugin cache
func (pc *PluginCache) Initialize(ctx context.Context) error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if pc.initialized {
		return nil
	}

	// Create cache directory
	if err := os.MkdirAll(pc.cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Load existing cache entries
	if err := pc.loadCacheIndex(); err != nil {
		pc.logger.Warn("failed to load cache index, starting fresh", logger.Error(err))
	}

	// Start cleanup routine
	pc.cleanupTicker = time.NewTicker(time.Hour)
	go pc.cleanupRoutine(ctx)

	pc.initialized = true

	pc.logger.Info("plugin cache initialized",
		logger.String("cache_dir", pc.cacheDir),
		logger.Int("info_items", len(pc.infoCache)),
		logger.Int("package_items", len(pc.packageCache)),
		logger.Int64("total_size", pc.totalSize),
	)

	if pc.metrics != nil {
		pc.metrics.Counter("forge.plugins.cache_initialized").Inc()
		pc.metrics.Gauge("forge.plugins.cache_size").Set(float64(pc.totalSize))
	}

	return nil
}

// Stop stops the plugin cache
func (pc *PluginCache) Stop(ctx context.Context) error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if !pc.initialized {
		return nil
	}

	// Stop cleanup routine
	if pc.cleanupTicker != nil {
		pc.cleanupTicker.Stop()
	}

	// Save cache index
	if err := pc.saveCacheIndex(); err != nil {
		pc.logger.Error("failed to save cache index", logger.Error(err))
	}

	pc.initialized = false

	pc.logger.Info("plugin cache stopped")

	if pc.metrics != nil {
		pc.metrics.Counter("forge.plugins.cache_stopped").Inc()
	}

	return nil
}

// GetPluginInfo retrieves cached plugin information
func (pc *PluginCache) GetPluginInfo(pluginID string) (*plugins.PluginEngineInfo, bool) {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	cached, exists := pc.infoCache[pluginID]
	if !exists {
		if pc.metrics != nil {
			pc.metrics.Counter("forge.plugins.cache_miss").Inc()
		}
		return nil, false
	}

	// Check if expired
	if time.Now().After(cached.ExpiresAt) {
		// Remove expired entry
		delete(pc.infoCache, pluginID)
		if pc.metrics != nil {
			pc.metrics.Counter("forge.plugins.cache_miss").Inc()
			pc.metrics.Counter("forge.plugins.cache_expired").Inc()
		}
		return nil, false
	}

	// Update access stats
	cached.AccessCount++
	cached.LastAccess = time.Now()

	if pc.metrics != nil {
		pc.metrics.Counter("forge.plugins.cache_hit").Inc()
	}

	return cached.Info, true
}

// SetPluginInfo caches plugin information
func (pc *PluginCache) SetPluginInfo(pluginID string, info *plugins.PluginEngineInfo) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	cached := &CachedPluginInfo{
		Info:        info,
		CachedAt:    time.Now(),
		ExpiresAt:   time.Now().Add(pc.config.CacheTimeout),
		AccessCount: 0,
		LastAccess:  time.Now(),
	}

	pc.infoCache[pluginID] = cached

	pc.logger.Debug("plugin info cached",
		logger.String("plugin_id", pluginID),
		logger.String("version", info.Version),
	)

	if pc.metrics != nil {
		pc.metrics.Counter("forge.plugins.info_cached").Inc()
	}
}

// GetPackage retrieves cached plugin package
func (pc *PluginCache) GetPackage(pluginID, version string) (plugins.PluginEnginePackage, bool) {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	cacheKey := fmt.Sprintf("%s:%s", pluginID, version)
	cached, exists := pc.packageCache[cacheKey]
	if !exists {
		if pc.metrics != nil {
			pc.metrics.Counter("forge.plugins.package_cache_miss").Inc()
		}
		return plugins.PluginEnginePackage{}, false
	}

	// Check if expired
	if time.Now().After(cached.ExpiresAt) {
		// Remove expired entry
		delete(pc.packageCache, cacheKey)
		pc.totalSize -= cached.Size

		// Remove file if it exists
		if cached.FilePath != "" {
			os.Remove(cached.FilePath)
		}

		if pc.metrics != nil {
			pc.metrics.Counter("forge.plugins.package_cache_miss").Inc()
			pc.metrics.Counter("forge.plugins.package_cache_expired").Inc()
		}
		return plugins.PluginEnginePackage{}, false
	}

	// Update access stats
	cached.AccessCount++
	cached.LastAccess = time.Now()

	// Load package from file if needed
	if len(cached.Package.Binary) == 0 && cached.FilePath != "" {
		if data, err := os.ReadFile(cached.FilePath); err == nil {
			cached.Package.Binary = data
		}
	}

	if pc.metrics != nil {
		pc.metrics.Counter("forge.plugins.package_cache_hit").Inc()
	}

	return cached.Package, true
}

// SetPackage caches plugin package
func (pc *PluginCache) SetPackage(pluginID, version string, pkg plugins.PluginEnginePackage) error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	cacheKey := fmt.Sprintf("%s:%s", pluginID, version)
	packageSize := int64(len(pkg.Binary))

	// Check if adding this package would exceed cache limit
	if pc.totalSize+packageSize > pc.config.MaxCacheSize {
		if err := pc.evictOldPackages(packageSize); err != nil {
			return fmt.Errorf("failed to evict old packages: %w", err)
		}
	}

	// Save package to file
	packageDir := filepath.Join(pc.cacheDir, "packages", pluginID)
	if err := os.MkdirAll(packageDir, 0755); err != nil {
		return fmt.Errorf("failed to create package directory: %w", err)
	}

	packagePath := filepath.Join(packageDir, fmt.Sprintf("%s.zip", version))
	if err := os.WriteFile(packagePath, pkg.Binary, 0644); err != nil {
		return fmt.Errorf("failed to write package file: %w", err)
	}

	// Create cached entry (without binary data to save memory)
	cached := &CachedPluginPackage{
		Package: plugins.PluginEnginePackage{
			Info:     pkg.Info,
			Config:   pkg.Config,
			Docs:     pkg.Docs,
			Assets:   pkg.Assets,
			Checksum: pkg.Checksum,
			// Binary is loaded on demand from file
		},
		CachedAt:    time.Now(),
		ExpiresAt:   time.Now().Add(pc.config.CacheTimeout),
		Size:        packageSize,
		AccessCount: 0,
		LastAccess:  time.Now(),
		FilePath:    packagePath,
	}

	pc.packageCache[cacheKey] = cached
	pc.totalSize += packageSize

	pc.logger.Debug("plugin package cached",
		logger.String("plugin_id", pluginID),
		logger.String("version", version),
		logger.Int64("size", packageSize),
	)

	if pc.metrics != nil {
		pc.metrics.Counter("forge.plugins.package_cached").Inc()
		pc.metrics.Gauge("forge.plugins.cache_size").Set(float64(pc.totalSize))
	}

	return nil
}

// Clear clears all cached items
func (pc *PluginCache) Clear() error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	// Remove all package files
	for _, cached := range pc.packageCache {
		if cached.FilePath != "" {
			os.Remove(cached.FilePath)
		}
	}

	// Clear in-memory caches
	pc.infoCache = make(map[string]*CachedPluginInfo)
	pc.packageCache = make(map[string]*CachedPluginPackage)
	pc.totalSize = 0

	pc.logger.Info("plugin cache cleared")

	if pc.metrics != nil {
		pc.metrics.Counter("forge.plugins.cache_cleared").Inc()
		pc.metrics.Gauge("forge.plugins.cache_size").Set(0)
	}

	return nil
}

// GetSize returns the current cache size
func (pc *PluginCache) GetSize() int64 {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	return pc.totalSize
}

// GetStats returns cache statistics
func (pc *PluginCache) GetStats() CacheStats {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	var hitCount, missCount, accessCount int64

	// Calculate stats from cached items
	for _, cached := range pc.infoCache {
		accessCount += cached.AccessCount
	}

	for _, cached := range pc.packageCache {
		accessCount += cached.AccessCount
	}

	hitRate := 0.0
	missRate := 0.0

	if accessCount > 0 {
		hitRate = float64(hitCount) / float64(accessCount)
		missRate = 1.0 - hitRate
	}

	return CacheStats{
		TotalItems:     len(pc.infoCache) + len(pc.packageCache),
		InfoItems:      len(pc.infoCache),
		PackageItems:   len(pc.packageCache),
		TotalSize:      pc.totalSize,
		HitRate:        hitRate,
		MissRate:       missRate,
		LastCleanup:    pc.lastCleanup,
		CacheDirectory: pc.cacheDir,
		MaxSize:        pc.config.MaxCacheSize,
		CurrentSize:    pc.totalSize,
		AccessCount:    accessCount,
		HitCount:       hitCount,
		MissCount:      missCount,
	}
}

// Private helper methods

func (pc *PluginCache) loadCacheIndex() error {
	indexFile := filepath.Join(pc.cacheDir, "index.json")
	if _, err := os.Stat(indexFile); os.IsNotExist(err) {
		return nil // No index file exists yet
	}

	data, err := os.ReadFile(indexFile)
	if err != nil {
		return fmt.Errorf("failed to read cache index: %w", err)
	}

	var index struct {
		InfoCache    map[string]*CachedPluginInfo    `json:"info_cache"`
		PackageCache map[string]*CachedPluginPackage `json:"package_cache"`
		TotalSize    int64                           `json:"total_size"`
	}

	if err := json.Unmarshal(data, &index); err != nil {
		return fmt.Errorf("failed to unmarshal cache index: %w", err)
	}

	pc.infoCache = index.InfoCache
	pc.packageCache = index.PackageCache
	pc.totalSize = index.TotalSize

	// Verify package files exist
	for key, cached := range pc.packageCache {
		if cached.FilePath != "" {
			if _, err := os.Stat(cached.FilePath); os.IsNotExist(err) {
				delete(pc.packageCache, key)
				pc.totalSize -= cached.Size
			}
		}
	}

	return nil
}

func (pc *PluginCache) saveCacheIndex() error {
	indexFile := filepath.Join(pc.cacheDir, "index.json")

	index := struct {
		InfoCache    map[string]*CachedPluginInfo    `json:"info_cache"`
		PackageCache map[string]*CachedPluginPackage `json:"package_cache"`
		TotalSize    int64                           `json:"total_size"`
		SavedAt      time.Time                       `json:"saved_at"`
	}{
		InfoCache:    pc.infoCache,
		PackageCache: pc.packageCache,
		TotalSize:    pc.totalSize,
		SavedAt:      time.Now(),
	}

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache index: %w", err)
	}

	if err := os.WriteFile(indexFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache index: %w", err)
	}

	return nil
}

func (pc *PluginCache) evictOldPackages(requiredSize int64) error {
	// Sort packages by last access time (oldest first)
	type packageEntry struct {
		key    string
		cached *CachedPluginPackage
	}

	var packages []packageEntry
	for key, cached := range pc.packageCache {
		packages = append(packages, packageEntry{key, cached})
	}

	// Sort by last access time
	for i := 0; i < len(packages)-1; i++ {
		for j := i + 1; j < len(packages); j++ {
			if packages[i].cached.LastAccess.After(packages[j].cached.LastAccess) {
				packages[i], packages[j] = packages[j], packages[i]
			}
		}
	}

	// Evict oldest packages until we have enough space
	spaceFreed := int64(0)
	evicted := 0

	for _, pkg := range packages {
		if pc.totalSize-spaceFreed+requiredSize <= pc.config.MaxCacheSize {
			break
		}

		// Remove package
		if pkg.cached.FilePath != "" {
			os.Remove(pkg.cached.FilePath)
		}

		delete(pc.packageCache, pkg.key)
		spaceFreed += pkg.cached.Size
		evicted++
	}

	pc.totalSize -= spaceFreed

	pc.logger.Debug("evicted old packages",
		logger.Int("evicted_count", evicted),
		logger.Int64("space_freed", spaceFreed),
	)

	if pc.metrics != nil {
		pc.metrics.Counter("forge.plugins.cache_evictions").Add(float64(evicted))
	}

	return nil
}

func (pc *PluginCache) cleanupRoutine(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-pc.cleanupTicker.C:
			pc.cleanup()
		}
	}
}

func (pc *PluginCache) cleanup() {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	now := time.Now()
	expired := 0

	// Clean up expired info cache entries
	for key, cached := range pc.infoCache {
		if now.After(cached.ExpiresAt) {
			delete(pc.infoCache, key)
			expired++
		}
	}

	// Clean up expired package cache entries
	for key, cached := range pc.packageCache {
		if now.After(cached.ExpiresAt) {
			if cached.FilePath != "" {
				os.Remove(cached.FilePath)
			}
			delete(pc.packageCache, key)
			pc.totalSize -= cached.Size
			expired++
		}
	}

	pc.lastCleanup = now

	if expired > 0 {
		pc.logger.Debug("cleaned up expired cache entries",
			logger.Int("expired_count", expired),
		)

		if pc.metrics != nil {
			pc.metrics.Counter("forge.plugins.cache_cleanup_expired").Add(float64(expired))
		}
	}
}
