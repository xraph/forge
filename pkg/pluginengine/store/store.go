package store

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	plugins "github.com/xraph/forge/pkg/pluginengine/common"
)

// PluginStore interface for plugin discovery and management
type PluginStore interface {
	Initialize(ctx context.Context) error
	Stop(ctx context.Context) error
	Validator() *PluginValidator
	Registry() plugins.PluginEngineRegistry
	Search(ctx context.Context, query PluginQuery) ([]plugins.PluginEngineInfo, error)
	Get(ctx context.Context, pluginID string) (*plugins.PluginEngineInfo, error)
	GetVersions(ctx context.Context, pluginID string) ([]string, error)
	GetLatestVersion(ctx context.Context, pluginID string) (string, error)
	Download(ctx context.Context, pluginID, version string) (plugins.PluginEnginePackage, error)
	Publish(ctx context.Context, plugin plugins.PluginEnginePackage) error
	GetStats() PluginStoreStats
}

// PluginStoreImpl implements the PluginStore interface
type PluginStoreImpl struct {
	config      StoreConfig
	registry    *PluginRegistry
	downloader  *PluginDownloader
	validator   *PluginValidator
	cache       *PluginCache
	marketplace *MarketplaceClient
	logger      common.Logger
	metrics     common.Metrics
	mu          sync.RWMutex
	initialized bool
	stats       PluginStoreStats
}

// StoreConfig contains configuration for the plugin store
type StoreConfig struct {
	CacheDirectory      string        `yaml:"cache_directory" json:"cache_directory"`
	MarketplaceURL      string        `yaml:"marketplace_url" json:"marketplace_url"`
	RegistryURL         string        `yaml:"registry_url" json:"registry_url"`
	APIKey              string        `yaml:"api_key" json:"api_key"`
	CacheTimeout        time.Duration `yaml:"cache_timeout" json:"cache_timeout"`
	MaxCacheSize        int64         `yaml:"max_cache_size" json:"max_cache_size"`
	MaxDownloadSize     int64         `yaml:"max_download_size" json:"max_download_size"`
	VerifySignatures    bool          `yaml:"verify_signatures" json:"verify_signatures"`
	TrustedPublishers   []string      `yaml:"trusted_publishers" json:"trusted_publishers"`
	AllowedSources      []string      `yaml:"allowed_sources" json:"allowed_sources"`
	BlockedSources      []string      `yaml:"blocked_sources" json:"blocked_sources"`
	EnableMarketplace   bool          `yaml:"enable_marketplace" json:"enable_marketplace"`
	EnableLocalRegistry bool          `yaml:"enable_local_registry" json:"enable_local_registry"`
	EnableCache         bool          `yaml:"enable_cache" json:"enable_cache"`
	HttpTimeout         time.Duration `yaml:"http_timeout" json:"http_timeout"`
	MaxRetries          int           `yaml:"max_retries" json:"max_retries"`
	RetryBackoff        time.Duration `yaml:"retry_backoff" json:"retry_backoff"`
}

func DefaultConfig() StoreConfig {
	return StoreConfig{
		CacheDirectory:      "plugins/cache",
		MarketplaceURL:      "https://plugins.forge.dev",
		RegistryURL:         "https://registry.forge.dev",
		CacheTimeout:        24 * time.Hour,
		MaxCacheSize:        10 * 1024 * 1024 * 1024, // 10GB
		MaxDownloadSize:     1024 * 1024 * 1024,      // 1GB
		VerifySignatures:    true,
		EnableMarketplace:   true,
		EnableLocalRegistry: true,
		EnableCache:         true,
		HttpTimeout:         30 * time.Second,
		MaxRetries:          3,
		RetryBackoff:        5 * time.Second,
	}
}

// PluginStoreStats contains statistics about the plugin store
type PluginStoreStats struct {
	TotalPlugins        int           `json:"total_plugins"`
	CachedPlugins       int           `json:"cached_plugins"`
	DownloadedPlugins   int           `json:"downloaded_plugins"`
	FailedDownloads     int           `json:"failed_downloads"`
	CacheHits           int64         `json:"cache_hits"`
	CacheMisses         int64         `json:"cache_misses"`
	TotalDownloadSize   int64         `json:"total_download_size"`
	AverageDownloadTime time.Duration `json:"average_download_time"`
	LastUpdateCheck     time.Time     `json:"last_update_check"`
	MarketplaceStatus   string        `json:"marketplace_status"`
	CacheSize           int64         `json:"cache_size"`
}

// NewPluginStore creates a new plugin store
func NewPluginStore(config StoreConfig, logger common.Logger, metrics common.Metrics) PluginStore {
	return &PluginStoreImpl{
		config:   config,
		logger:   logger,
		metrics:  metrics,
		stats:    PluginStoreStats{},
		registry: NewPluginRegistry(config, logger, metrics),
	}
}

// Initialize initializes the plugin store
func (ps *PluginStoreImpl) Initialize(ctx context.Context) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.initialized {
		return nil
	}

	// Create cache directory
	if err := os.MkdirAll(ps.config.CacheDirectory, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Initialize registry
	ps.registry = NewPluginRegistry(ps.config, ps.logger, ps.metrics)
	if err := ps.registry.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize registry: %w", err)
	}

	// Initialize downloader
	ps.downloader = NewPluginDownloader(ps.config, ps.logger, ps.metrics)
	if err := ps.downloader.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize downloader: %w", err)
	}

	// Initialize validator
	ps.validator = NewPluginValidator(ps.config, ps.logger, ps.metrics)
	if err := ps.validator.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize validator: %w", err)
	}

	// Initialize cache
	if ps.config.EnableCache {
		ps.cache = NewPluginCache(ps.config, ps.logger, ps.metrics)
		if err := ps.cache.Initialize(ctx); err != nil {
			return fmt.Errorf("failed to initialize cache: %w", err)
		}
	}

	// Initialize marketplace client
	if ps.config.EnableMarketplace {
		ps.marketplace = NewMarketplaceClient(ps.config, ps.logger, ps.metrics)
		if err := ps.marketplace.Initialize(ctx); err != nil {
			return fmt.Errorf("failed to initialize marketplace client: %w", err)
		}
	}

	ps.initialized = true

	ps.logger.Info("plugin store initialized",
		logger.String("cache_directory", ps.config.CacheDirectory),
		logger.String("marketplace_url", ps.config.MarketplaceURL),
		logger.Bool("enable_cache", ps.config.EnableCache),
		logger.Bool("enable_marketplace", ps.config.EnableMarketplace),
	)

	if ps.metrics != nil {
		ps.metrics.Counter("forge.plugins.store_initialized").Inc()
	}

	return nil
}

// Stop stops the plugin store
func (ps *PluginStoreImpl) Stop(ctx context.Context) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if !ps.initialized {
		return nil
	}

	// Stop components
	if ps.cache != nil {
		if err := ps.cache.Stop(ctx); err != nil {
			ps.logger.Error("failed to stop cache", logger.Error(err))
		}
	}

	if ps.marketplace != nil {
		if err := ps.marketplace.Stop(ctx); err != nil {
			ps.logger.Error("failed to stop marketplace client", logger.Error(err))
		}
	}

	if ps.validator != nil {
		if err := ps.validator.Stop(ctx); err != nil {
			ps.logger.Error("failed to stop validator", logger.Error(err))
		}
	}

	if ps.downloader != nil {
		if err := ps.downloader.Stop(ctx); err != nil {
			ps.logger.Error("failed to stop downloader", logger.Error(err))
		}
	}

	if ps.registry != nil {
		if err := ps.registry.Stop(ctx); err != nil {
			ps.logger.Error("failed to stop registry", logger.Error(err))
		}
	}

	ps.initialized = false

	ps.logger.Info("plugin store stopped")

	if ps.metrics != nil {
		ps.metrics.Counter("forge.plugins.store_stopped").Inc()
	}

	return nil
}

func (ps *PluginStoreImpl) Validator() *PluginValidator {
	return ps.validator
}

// Registry returns the plugin registry that implements plugins.PluginEngineRegistry
func (ps *PluginStoreImpl) Registry() plugins.PluginEngineRegistry {
	return ps.registry
}

// Search searches for plugins
func (ps *PluginStoreImpl) Search(ctx context.Context, query PluginQuery) ([]plugins.PluginEngineInfo, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if !ps.initialized {
		return nil, fmt.Errorf("plugin store not initialized")
	}

	var results []plugins.PluginEngineInfo
	var err error

	// Search in marketplace if enabled
	if ps.config.EnableMarketplace && ps.marketplace != nil {
		marketplaceResults, err := ps.marketplace.Search(ctx, query)
		if err != nil {
			ps.logger.Warn("marketplace search failed", logger.Error(err))
		} else {
			results = append(results, marketplaceResults...)
		}
	}

	// Search in local registry if enabled
	if ps.config.EnableLocalRegistry && ps.registry != nil {
		registryResults, err := ps.registry.SearchMetadata(ctx, query)
		if err != nil {
			ps.logger.Warn("registry search failed", logger.Error(err))
		} else {
			results = append(results, registryResults...)
		}
	}

	// Apply filters
	results = ps.filterResults(results, query)

	// Sort results
	results = ps.sortResults(results, query.Sort)

	// Apply pagination
	if query.Limit > 0 {
		start := query.Offset
		end := start + query.Limit
		if start >= len(results) {
			results = []plugins.PluginEngineInfo{}
		} else if end > len(results) {
			results = results[start:]
		} else {
			results = results[start:end]
		}
	}

	ps.logger.Debug("plugin search completed",
		logger.String("query", query.Name),
		logger.Int("results", len(results)),
	)

	if ps.metrics != nil {
		ps.metrics.Counter("forge.plugins.search_performed").Inc()
		ps.metrics.Histogram("forge.plugins.search_results").Observe(float64(len(results)))
	}

	return results, err
}

// Get retrieves plugin information
func (ps *PluginStoreImpl) Get(ctx context.Context, pluginID string) (*plugins.PluginEngineInfo, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if !ps.initialized {
		return nil, fmt.Errorf("plugin store not initialized")
	}

	// Check cache first
	if ps.cache != nil {
		if info, found := ps.cache.GetPluginInfo(pluginID); found {
			ps.stats.CacheHits++
			return info, nil
		}
		ps.stats.CacheMisses++
	}

	var info *plugins.PluginEngineInfo
	var err error

	// Try marketplace first
	if ps.config.EnableMarketplace && ps.marketplace != nil {
		info, err = ps.marketplace.GetPluginInfo(ctx, pluginID)
		if err == nil {
			// Cache the result
			if ps.cache != nil {
				ps.cache.SetPluginInfo(pluginID, info)
			}
			return info, nil
		}
	}

	// Try local registry
	if ps.config.EnableLocalRegistry && ps.registry != nil {
		info, err = ps.registry.GetPluginInfo(ctx, pluginID)
		if err == nil {
			// Cache the result
			if ps.cache != nil {
				ps.cache.SetPluginInfo(pluginID, info)
			}
			return info, nil
		}
	}

	if ps.metrics != nil {
		ps.metrics.Counter("forge.plugins.get_plugin_info").Inc()
	}

	return nil, fmt.Errorf("plugin not found: %s", pluginID)
}

// GetVersions retrieves available versions for a plugin
func (ps *PluginStoreImpl) GetVersions(ctx context.Context, pluginID string) ([]string, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if !ps.initialized {
		return nil, fmt.Errorf("plugin store not initialized")
	}

	var versions []string
	var err error

	// Try marketplace first
	if ps.config.EnableMarketplace && ps.marketplace != nil {
		versions, err = ps.marketplace.GetVersions(ctx, pluginID)
		if err == nil {
			return versions, nil
		}
	}

	// Try local registry
	if ps.config.EnableLocalRegistry && ps.registry != nil {
		versions, err = ps.registry.GetVersions(ctx, pluginID)
		if err == nil {
			return versions, nil
		}
	}

	if ps.metrics != nil {
		ps.metrics.Counter("forge.plugins.get_versions").Inc()
	}

	return nil, fmt.Errorf("versions not found for plugin: %s", pluginID)
}

// GetLatestVersion retrieves the latest version for a plugin
func (ps *PluginStoreImpl) GetLatestVersion(ctx context.Context, pluginID string) (string, error) {
	versions, err := ps.GetVersions(ctx, pluginID)
	if err != nil {
		return "", err
	}

	if len(versions) == 0 {
		return "", fmt.Errorf("no versions available for plugin: %s", pluginID)
	}

	// Return the first version (assuming they're sorted with latest first)
	return versions[0], nil
}

// Download downloads a plugin package
func (ps *PluginStoreImpl) Download(ctx context.Context, pluginID, version string) (plugins.PluginEnginePackage, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if !ps.initialized {
		return plugins.PluginEnginePackage{}, fmt.Errorf("plugin store not initialized")
	}

	// Check cache first
	if ps.cache != nil {
		if pkg, found := ps.cache.GetPackage(pluginID, version); found {
			ps.stats.CacheHits++
			return pkg, nil
		}
		ps.stats.CacheMisses++
	}

	startTime := time.Now()
	defer func() {
		downloadTime := time.Since(startTime)
		ps.stats.AverageDownloadTime = (ps.stats.AverageDownloadTime + downloadTime) / 2
	}()

	var pkg plugins.PluginEnginePackage
	var err error

	// Try marketplace first
	if ps.config.EnableMarketplace && ps.marketplace != nil {
		pkg, err = ps.marketplace.Download(ctx, pluginID, version)
		if err == nil {
			// Validate package
			if err := ps.validator.ValidatePackage(ctx, pkg); err != nil {
				ps.stats.FailedDownloads++
				return plugins.PluginEnginePackage{}, fmt.Errorf("package validation failed: %w", err)
			}

			// Cache the package
			if ps.cache != nil {
				ps.cache.SetPackage(pluginID, version, pkg)
			}

			ps.stats.DownloadedPlugins++
			ps.stats.TotalDownloadSize += int64(len(pkg.Binary))

			ps.logger.Info("plugin package downloaded",
				logger.String("plugin_id", pluginID),
				logger.String("version", version),
				logger.Int("size", len(pkg.Binary)),
			)

			if ps.metrics != nil {
				ps.metrics.Counter("forge.plugins.download_success").Inc()
				ps.metrics.Histogram("forge.plugins.download_size").Observe(float64(len(pkg.Binary)))
			}

			return pkg, nil
		}
	}

	// Try local registry
	if ps.config.EnableLocalRegistry && ps.registry != nil {
		pkg, err = ps.registry.Download(ctx, pluginID, version)
		if err == nil {
			// Validate package
			if err := ps.validator.ValidatePackage(ctx, pkg); err != nil {
				ps.stats.FailedDownloads++
				return plugins.PluginEnginePackage{}, fmt.Errorf("package validation failed: %w", err)
			}

			// Cache the package
			if ps.cache != nil {
				ps.cache.SetPackage(pluginID, version, pkg)
			}

			ps.stats.DownloadedPlugins++
			ps.stats.TotalDownloadSize += int64(len(pkg.Binary))

			return pkg, nil
		}
	}

	ps.stats.FailedDownloads++

	if ps.metrics != nil {
		ps.metrics.Counter("forge.plugins.download_failed").Inc()
	}

	return plugins.PluginEnginePackage{}, fmt.Errorf("failed to download plugin %s version %s", pluginID, version)
}

// Publish publishes a plugin package
func (ps *PluginStoreImpl) Publish(ctx context.Context, plugin plugins.PluginEnginePackage) error {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if !ps.initialized {
		return fmt.Errorf("plugin store not initialized")
	}

	// Validate package
	if err := ps.validator.ValidatePackage(ctx, plugin); err != nil {
		return fmt.Errorf("package validation failed: %w", err)
	}

	// Publish to marketplace if enabled
	if ps.config.EnableMarketplace && ps.marketplace != nil {
		if err := ps.marketplace.Publish(ctx, plugin); err != nil {
			return fmt.Errorf("failed to publish to marketplace: %w", err)
		}
	}

	// Publish to local registry if enabled
	if ps.config.EnableLocalRegistry && ps.registry != nil {
		if err := ps.registry.Publish(ctx, plugin); err != nil {
			return fmt.Errorf("failed to publish to local registry: %w", err)
		}
	}

	ps.logger.Info("plugin published",
		logger.String("plugin_id", plugin.Info.ID),
		logger.String("version", plugin.Info.Version),
	)

	if ps.metrics != nil {
		ps.metrics.Counter("forge.plugins.publish_success").Inc()
	}

	return nil
}

// GetStats returns plugin store statistics
func (ps *PluginStoreImpl) GetStats() PluginStoreStats {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	ps.stats.LastUpdateCheck = time.Now()

	if ps.cache != nil {
		ps.stats.CacheSize = ps.cache.GetSize()
	}

	if ps.marketplace != nil {
		ps.stats.MarketplaceStatus = ps.marketplace.GetStatus()
	}

	return ps.stats
}

// Helper methods

func (ps *PluginStoreImpl) filterResults(results []plugins.PluginEngineInfo, query PluginQuery) []plugins.PluginEngineInfo {
	var filtered []plugins.PluginEngineInfo

	for _, result := range results {
		// Apply filters
		if query.Type != "" && result.Type != query.Type {
			continue
		}

		if query.Author != "" && result.Author != query.Author {
			continue
		}

		if query.Category != "" && result.Category != query.Category {
			continue
		}

		if query.MinRating > 0 && result.Rating < query.MinRating {
			continue
		}

		if len(query.Tags) > 0 {
			hasAllTags := true
			for _, tag := range query.Tags {
				found := false
				for _, resultTag := range result.Tags {
					if resultTag == tag {
						found = true
						break
					}
				}
				if !found {
					hasAllTags = false
					break
				}
			}
			if !hasAllTags {
				continue
			}
		}

		filtered = append(filtered, result)
	}

	return filtered
}

func (ps *PluginStoreImpl) sortResults(results []plugins.PluginEngineInfo, sortOrder PluginSortOrder) []plugins.PluginEngineInfo {
	// Implement sorting logic based on sort order
	// For now, return as-is
	return results
}

// PluginSortOrder defines sort order for plugin search results
type PluginSortOrder string

const (
	SortByRelevance  PluginSortOrder = "relevance"
	SortByPopularity PluginSortOrder = "popularity"
	SortByRating     PluginSortOrder = "rating"
	SortByRecent     PluginSortOrder = "recent"
	SortByName       PluginSortOrder = "name"
	SortByDownloads  PluginSortOrder = "downloads"
)

// PluginQuery defines search query parameters
type PluginQuery struct {
	Name       string                   `json:"name,omitempty"`
	Type       plugins.PluginEngineType `json:"type,omitempty"`
	Author     string                   `json:"author,omitempty"`
	Tags       []string                 `json:"tags,omitempty"`
	Category   string                   `json:"category,omitempty"`
	MinRating  float64                  `json:"min_rating,omitempty"`
	Compatible string                   `json:"compatible,omitempty"` // Framework version
	Filters    map[string]interface{}   `json:"filters,omitempty"`
	Sort       PluginSortOrder          `json:"sort"`
	Limit      int                      `json:"limit"`
	Offset     int                      `json:"offset"`
}

// HTTPClient interface for making HTTP requests
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// DefaultHTTPClient provides a default HTTP client implementation
type DefaultHTTPClient struct {
	client *http.Client
}

// NewDefaultHTTPClient creates a new default HTTP client
func NewDefaultHTTPClient(timeout time.Duration) *DefaultHTTPClient {
	return &DefaultHTTPClient{
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// Do executes an HTTP request
func (c *DefaultHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return c.client.Do(req)
}

// Helper function to create HTTP request with proper headers
func (ps *PluginStoreImpl) createHTTPRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "Forge-Plugin-Store/1.0")
	req.Header.Set("Content-Type", "application/json")

	if ps.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+ps.config.APIKey)
	}

	return req, nil
}

// Helper function to parse JSON response
func (ps *PluginStoreImpl) parseJSONResponse(resp *http.Response, v interface{}) error {
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(v)
}

// Helper function to save file to cache
func (ps *PluginStoreImpl) saveToCacheFile(pluginID, version string, data []byte) error {
	cacheDir := filepath.Join(ps.config.CacheDirectory, pluginID)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}

	filename := filepath.Join(cacheDir, fmt.Sprintf("%s.zip", version))
	return os.WriteFile(filename, data, 0644)
}

// Helper function to load from cache file
func (ps *PluginStoreImpl) loadFromCacheFile(pluginID, version string) ([]byte, error) {
	filename := filepath.Join(ps.config.CacheDirectory, pluginID, fmt.Sprintf("%s.zip", version))
	return os.ReadFile(filename)
}
