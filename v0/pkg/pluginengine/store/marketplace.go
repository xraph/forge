package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	plugins "github.com/xraph/forge/pkg/pluginengine/common"
)

// MarketplaceClient handles communication with the plugin marketplace
type MarketplaceClient struct {
	config      StoreConfig
	httpClient  HTTPClient
	authToken   string
	baseURL     string
	logger      common.Logger
	metrics     common.Metrics
	rateLimiter *RateLimiter
	cache       *MarketplaceCache
	mu          sync.RWMutex
	initialized bool
}

// MarketplaceCache caches marketplace responses
type MarketplaceCache struct {
	searches map[string]*CachedSearch
	plugins  map[string]*CachedPlugin
	versions map[string]*CachedVersions
	mu       sync.RWMutex
	timeout  time.Duration
}

// CachedSearch represents a cached search result
type CachedSearch struct {
	Query     string                     `json:"query"`
	Results   []plugins.PluginEngineInfo `json:"results"`
	CachedAt  time.Time                  `json:"cached_at"`
	ExpiresAt time.Time                  `json:"expires_at"`
}

// CachedPlugin represents a cached plugin info
type CachedPlugin struct {
	PluginID  string                   `json:"plugin_id"`
	Info      plugins.PluginEngineInfo `json:"info"`
	CachedAt  time.Time                `json:"cached_at"`
	ExpiresAt time.Time                `json:"expires_at"`
}

// CachedVersions represents cached version list
type CachedVersions struct {
	PluginID  string    `json:"plugin_id"`
	Versions  []string  `json:"versions"`
	CachedAt  time.Time `json:"cached_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// MarketplaceResponse represents a marketplace API response
type MarketplaceResponse struct {
	Success bool                   `json:"success"`
	Data    interface{}            `json:"data"`
	Error   string                 `json:"error,omitempty"`
	Meta    map[string]interface{} `json:"meta,omitempty"`
}

// SearchResponse represents search results from marketplace
type SearchResponse struct {
	Plugins    []plugins.PluginEngineInfo `json:"plugins"`
	Total      int                        `json:"total"`
	Page       int                        `json:"page"`
	PageSize   int                        `json:"page_size"`
	TotalPages int                        `json:"total_pages"`
}

// RateLimiter implements rate limiting for marketplace API
type RateLimiter struct {
	requests    map[string][]time.Time
	maxRequests int
	window      time.Duration
	mu          sync.RWMutex
}

// NewMarketplaceClient creates a new marketplace client
func NewMarketplaceClient(config StoreConfig, logger common.Logger, metrics common.Metrics) *MarketplaceClient {
	return &MarketplaceClient{
		config:     config,
		httpClient: NewDefaultHTTPClient(config.HttpTimeout),
		baseURL:    config.MarketplaceURL,
		logger:     logger,
		metrics:    metrics,
		rateLimiter: &RateLimiter{
			requests:    make(map[string][]time.Time),
			maxRequests: 100, // 100 requests per minute
			window:      time.Minute,
		},
		cache: &MarketplaceCache{
			searches: make(map[string]*CachedSearch),
			plugins:  make(map[string]*CachedPlugin),
			versions: make(map[string]*CachedVersions),
			timeout:  15 * time.Minute,
		},
	}
}

// Initialize initializes the marketplace client
func (mc *MarketplaceClient) Initialize(ctx context.Context) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if mc.initialized {
		return nil
	}

	// Authenticate with marketplace if API key is provided
	if mc.config.APIKey != "" {
		if err := mc.authenticate(ctx); err != nil {
			return fmt.Errorf("failed to authenticate with marketplace: %w", err)
		}
	}

	// Test connectivity
	if err := mc.testConnectivity(ctx); err != nil {
		return fmt.Errorf("failed to connect to marketplace: %w", err)
	}

	mc.initialized = true

	mc.logger.Info("marketplace client initialized",
		logger.String("base_url", mc.baseURL),
		logger.Bool("authenticated", mc.authToken != ""),
	)

	if mc.metrics != nil {
		mc.metrics.Counter("forge.plugins.marketplace_initialized").Inc()
	}

	return nil
}

// Stop stops the marketplace client
func (mc *MarketplaceClient) Stop(ctx context.Context) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if !mc.initialized {
		return nil
	}

	mc.initialized = false

	mc.logger.Info("marketplace client stopped")

	if mc.metrics != nil {
		mc.metrics.Counter("forge.plugins.marketplace_stopped").Inc()
	}

	return nil
}

// Search searches for plugins in the marketplace
func (mc *MarketplaceClient) Search(ctx context.Context, query PluginQuery) ([]plugins.PluginEngineInfo, error) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if !mc.initialized {
		return nil, fmt.Errorf("marketplace client not initialized")
	}

	// Check cache first
	cacheKey := mc.buildSearchCacheKey(query)
	if cached := mc.getCachedSearch(cacheKey); cached != nil {
		mc.logger.Debug("returning cached search results",
			logger.String("query", query.Name),
			logger.Int("results", len(cached.Results)),
		)

		if mc.metrics != nil {
			mc.metrics.Counter("forge.plugins.marketplace_cache_hit").Inc()
		}

		return cached.Results, nil
	}

	// Rate limit check
	if !mc.rateLimiter.Allow("search") {
		return nil, fmt.Errorf("rate limit exceeded")
	}

	// Build search URL
	searchURL := fmt.Sprintf("%s/api/v1/plugins/search", mc.baseURL)

	// Add query parameters
	params := url.Values{}
	if query.Name != "" {
		params.Add("q", query.Name)
	}
	if query.Type != "" {
		params.Add("type", string(query.Type))
	}
	if query.Author != "" {
		params.Add("author", query.Author)
	}
	if query.Category != "" {
		params.Add("category", query.Category)
	}
	if query.MinRating > 0 {
		params.Add("min_rating", strconv.FormatFloat(query.MinRating, 'f', 2, 64))
	}
	if len(query.Tags) > 0 {
		params.Add("tags", strings.Join(query.Tags, ","))
	}
	if query.Sort != "" {
		params.Add("sort", string(query.Sort))
	}
	if query.Limit > 0 {
		params.Add("limit", strconv.Itoa(query.Limit))
	}
	if query.Offset > 0 {
		params.Add("offset", strconv.Itoa(query.Offset))
	}

	if len(params) > 0 {
		searchURL += "?" + params.Encode()
	}

	// Make request
	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create search request: %w", err)
	}

	mc.addAuthHeaders(req)

	resp, err := mc.httpClient.Do(req)
	if err != nil {
		if mc.metrics != nil {
			mc.metrics.Counter("forge.plugins.marketplace_search_failed").Inc()
		}
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if mc.metrics != nil {
			mc.metrics.Counter("forge.plugins.marketplace_search_failed").Inc()
		}
		return nil, fmt.Errorf("search request failed with status: %d", resp.StatusCode)
	}

	// Parse response
	var marketplaceResp MarketplaceResponse
	if err := json.NewDecoder(resp.Body).Decode(&marketplaceResp); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	if !marketplaceResp.Success {
		return nil, fmt.Errorf("search failed: %s", marketplaceResp.Error)
	}

	// Parse search results
	searchData, err := json.Marshal(marketplaceResp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal search data: %w", err)
	}

	var searchResp SearchResponse
	if err := json.Unmarshal(searchData, &searchResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal search response: %w", err)
	}

	// Cache results
	mc.setCachedSearch(cacheKey, &CachedSearch{
		Query:     cacheKey,
		Results:   searchResp.Plugins,
		CachedAt:  time.Now(),
		ExpiresAt: time.Now().Add(mc.cache.timeout),
	})

	mc.logger.Debug("marketplace search completed",
		logger.String("query", query.Name),
		logger.Int("results", len(searchResp.Plugins)),
		logger.Int("total", searchResp.Total),
	)

	if mc.metrics != nil {
		mc.metrics.Counter("forge.plugins.marketplace_search_success").Inc()
		mc.metrics.Histogram("forge.plugins.marketplace_search_results").Observe(float64(len(searchResp.Plugins)))
	}

	return searchResp.Plugins, nil
}

// GetPluginInfo retrieves plugin information from marketplace
func (mc *MarketplaceClient) GetPluginInfo(ctx context.Context, pluginID string) (*plugins.PluginEngineInfo, error) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if !mc.initialized {
		return nil, fmt.Errorf("marketplace client not initialized")
	}

	// Check cache first
	if cached := mc.getCachedPlugin(pluginID); cached != nil {
		mc.logger.Debug("returning cached plugin info",
			logger.String("plugin_id", pluginID),
		)

		if mc.metrics != nil {
			mc.metrics.Counter("forge.plugins.marketplace_cache_hit").Inc()
		}

		return &cached.Info, nil
	}

	// Rate limit check
	if !mc.rateLimiter.Allow("get_plugin") {
		return nil, fmt.Errorf("rate limit exceeded")
	}

	// Build URL
	pluginURL := fmt.Sprintf("%s/api/v1/plugins/%s", mc.baseURL, pluginID)

	// Make request
	req, err := http.NewRequestWithContext(ctx, "GET", pluginURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create plugin request: %w", err)
	}

	mc.addAuthHeaders(req)

	resp, err := mc.httpClient.Do(req)
	if err != nil {
		if mc.metrics != nil {
			mc.metrics.Counter("forge.plugins.marketplace_get_plugin_failed").Inc()
		}
		return nil, fmt.Errorf("plugin request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("plugin not found: %s", pluginID)
	}

	if resp.StatusCode != http.StatusOK {
		if mc.metrics != nil {
			mc.metrics.Counter("forge.plugins.marketplace_get_plugin_failed").Inc()
		}
		return nil, fmt.Errorf("plugin request failed with status: %d", resp.StatusCode)
	}

	// Parse response
	var marketplaceResp MarketplaceResponse
	if err := json.NewDecoder(resp.Body).Decode(&marketplaceResp); err != nil {
		return nil, fmt.Errorf("failed to decode plugin response: %w", err)
	}

	if !marketplaceResp.Success {
		return nil, fmt.Errorf("get plugin failed: %s", marketplaceResp.Error)
	}

	// Parse plugin info
	pluginData, err := json.Marshal(marketplaceResp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal plugin data: %w", err)
	}

	var pluginInfo plugins.PluginEngineInfo
	if err := json.Unmarshal(pluginData, &pluginInfo); err != nil {
		return nil, fmt.Errorf("failed to unmarshal plugin info: %w", err)
	}

	// Cache result
	mc.setCachedPlugin(pluginID, &CachedPlugin{
		PluginID:  pluginID,
		Info:      pluginInfo,
		CachedAt:  time.Now(),
		ExpiresAt: time.Now().Add(mc.cache.timeout),
	})

	mc.logger.Debug("plugin info retrieved from marketplace",
		logger.String("plugin_id", pluginID),
		logger.String("plugin_name", pluginInfo.Name),
		logger.String("version", pluginInfo.Version),
	)

	if mc.metrics != nil {
		mc.metrics.Counter("forge.plugins.marketplace_get_plugin_success").Inc()
	}

	return &pluginInfo, nil
}

// GetVersions retrieves available versions for a plugin
func (mc *MarketplaceClient) GetVersions(ctx context.Context, pluginID string) ([]string, error) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if !mc.initialized {
		return nil, fmt.Errorf("marketplace client not initialized")
	}

	// Check cache first
	if cached := mc.getCachedVersions(pluginID); cached != nil {
		mc.logger.Debug("returning cached versions",
			logger.String("plugin_id", pluginID),
			logger.Int("versions", len(cached.Versions)),
		)

		if mc.metrics != nil {
			mc.metrics.Counter("forge.plugins.marketplace_cache_hit").Inc()
		}

		return cached.Versions, nil
	}

	// Rate limit check
	if !mc.rateLimiter.Allow("get_versions") {
		return nil, fmt.Errorf("rate limit exceeded")
	}

	// Build URL
	versionsURL := fmt.Sprintf("%s/api/v1/plugins/%s/versions", mc.baseURL, pluginID)

	// Make request
	req, err := http.NewRequestWithContext(ctx, "GET", versionsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create versions request: %w", err)
	}

	mc.addAuthHeaders(req)

	resp, err := mc.httpClient.Do(req)
	if err != nil {
		if mc.metrics != nil {
			mc.metrics.Counter("forge.plugins.marketplace_get_versions_failed").Inc()
		}
		return nil, fmt.Errorf("versions request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("plugin not found: %s", pluginID)
	}

	if resp.StatusCode != http.StatusOK {
		if mc.metrics != nil {
			mc.metrics.Counter("forge.plugins.marketplace_get_versions_failed").Inc()
		}
		return nil, fmt.Errorf("versions request failed with status: %d", resp.StatusCode)
	}

	// Parse response
	var marketplaceResp MarketplaceResponse
	if err := json.NewDecoder(resp.Body).Decode(&marketplaceResp); err != nil {
		return nil, fmt.Errorf("failed to decode versions response: %w", err)
	}

	if !marketplaceResp.Success {
		return nil, fmt.Errorf("get versions failed: %s", marketplaceResp.Error)
	}

	// Parse versions
	versionsData, err := json.Marshal(marketplaceResp.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal versions data: %w", err)
	}

	var versions []string
	if err := json.Unmarshal(versionsData, &versions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal versions: %w", err)
	}

	// Cache result
	mc.setCachedVersions(pluginID, &CachedVersions{
		PluginID:  pluginID,
		Versions:  versions,
		CachedAt:  time.Now(),
		ExpiresAt: time.Now().Add(mc.cache.timeout),
	})

	mc.logger.Debug("versions retrieved from marketplace",
		logger.String("plugin_id", pluginID),
		logger.Int("versions", len(versions)),
	)

	if mc.metrics != nil {
		mc.metrics.Counter("forge.plugins.marketplace_get_versions_success").Inc()
	}

	return versions, nil
}

// Download downloads a plugin package from marketplace
func (mc *MarketplaceClient) Download(ctx context.Context, pluginID, version string) (plugins.PluginEnginePackage, error) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if !mc.initialized {
		return plugins.PluginEnginePackage{}, fmt.Errorf("marketplace client not initialized")
	}

	// Rate limit check
	if !mc.rateLimiter.Allow("download") {
		return plugins.PluginEnginePackage{}, fmt.Errorf("rate limit exceeded")
	}

	// Build download URL
	downloadURL := fmt.Sprintf("%s/api/v1/plugins/%s/versions/%s/download", mc.baseURL, pluginID, version)

	// Make request
	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return plugins.PluginEnginePackage{}, fmt.Errorf("failed to create download request: %w", err)
	}

	mc.addAuthHeaders(req)

	resp, err := mc.httpClient.Do(req)
	if err != nil {
		if mc.metrics != nil {
			mc.metrics.Counter("forge.plugins.marketplace_download_failed").Inc()
		}
		return plugins.PluginEnginePackage{}, fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return plugins.PluginEnginePackage{}, fmt.Errorf("plugin version not found: %s@%s", pluginID, version)
	}

	if resp.StatusCode != http.StatusOK {
		if mc.metrics != nil {
			mc.metrics.Counter("forge.plugins.marketplace_download_failed").Inc()
		}
		return plugins.PluginEnginePackage{}, fmt.Errorf("download request failed with status: %d", resp.StatusCode)
	}

	// Parse response as plugin package
	var marketplaceResp MarketplaceResponse
	if err := json.NewDecoder(resp.Body).Decode(&marketplaceResp); err != nil {
		return plugins.PluginEnginePackage{}, fmt.Errorf("failed to decode download response: %w", err)
	}

	if !marketplaceResp.Success {
		return plugins.PluginEnginePackage{}, fmt.Errorf("download failed: %s", marketplaceResp.Error)
	}

	// Parse plugin package
	packageData, err := json.Marshal(marketplaceResp.Data)
	if err != nil {
		return plugins.PluginEnginePackage{}, fmt.Errorf("failed to marshal package data: %w", err)
	}

	var pkg plugins.PluginEnginePackage
	if err := json.Unmarshal(packageData, &pkg); err != nil {
		return plugins.PluginEnginePackage{}, fmt.Errorf("failed to unmarshal plugin package: %w", err)
	}

	mc.logger.Debug("plugin package downloaded from marketplace",
		logger.String("plugin_id", pluginID),
		logger.String("version", version),
		logger.Int("size", len(pkg.Binary)),
	)

	if mc.metrics != nil {
		mc.metrics.Counter("forge.plugins.marketplace_download_success").Inc()
		mc.metrics.Histogram("forge.plugins.marketplace_download_size").Observe(float64(len(pkg.Binary)))
	}

	return pkg, nil
}

// Publish publishes a plugin to the marketplace
func (mc *MarketplaceClient) Publish(ctx context.Context, pkg plugins.PluginEnginePackage) error {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if !mc.initialized {
		return fmt.Errorf("marketplace client not initialized")
	}

	if mc.authToken == "" {
		return fmt.Errorf("authentication required for publishing")
	}

	// Rate limit check
	if !mc.rateLimiter.Allow("publish") {
		return fmt.Errorf("rate limit exceeded")
	}

	// Build publish URL
	publishURL := fmt.Sprintf("%s/api/v1/plugins", mc.baseURL)

	// Prepare request body
	reqBody, err := json.Marshal(pkg)
	if err != nil {
		return fmt.Errorf("failed to marshal plugin package: %w", err)
	}

	// Make request
	req, err := http.NewRequestWithContext(ctx, "POST", publishURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create publish request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	mc.addAuthHeaders(req)

	resp, err := mc.httpClient.Do(req)
	if err != nil {
		if mc.metrics != nil {
			mc.metrics.Counter("forge.plugins.marketplace_publish_failed").Inc()
		}
		return fmt.Errorf("publish request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		if mc.metrics != nil {
			mc.metrics.Counter("forge.plugins.marketplace_publish_failed").Inc()
		}
		return fmt.Errorf("publish request failed with status: %d", resp.StatusCode)
	}

	// Parse response
	var marketplaceResp MarketplaceResponse
	if err := json.NewDecoder(resp.Body).Decode(&marketplaceResp); err != nil {
		return fmt.Errorf("failed to decode publish response: %w", err)
	}

	if !marketplaceResp.Success {
		return fmt.Errorf("publish failed: %s", marketplaceResp.Error)
	}

	mc.logger.Info("plugin published to marketplace",
		logger.String("plugin_id", pkg.Info.ID),
		logger.String("version", pkg.Info.Version),
	)

	if mc.metrics != nil {
		mc.metrics.Counter("forge.plugins.marketplace_publish_success").Inc()
	}

	return nil
}

// GetStatus returns the marketplace status
func (mc *MarketplaceClient) GetStatus() string {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if !mc.initialized {
		return "not_initialized"
	}

	// Test connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mc.testConnectivity(ctx); err != nil {
		return "offline"
	}

	return "online"
}

// Helper methods

// authenticate authenticates with the marketplace
func (mc *MarketplaceClient) authenticate(ctx context.Context) error {
	authURL := fmt.Sprintf("%s/api/v1/auth/login", mc.baseURL)

	authReq := map[string]string{
		"api_key": mc.config.APIKey,
	}

	reqBody, err := json.Marshal(authReq)
	if err != nil {
		return fmt.Errorf("failed to marshal auth request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", authURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create auth request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := mc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("auth request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("authentication failed with status: %d", resp.StatusCode)
	}

	var authResp struct {
		Success bool   `json:"success"`
		Token   string `json:"token"`
		Error   string `json:"error,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return fmt.Errorf("failed to decode auth response: %w", err)
	}

	if !authResp.Success {
		return fmt.Errorf("authentication failed: %s", authResp.Error)
	}

	mc.authToken = authResp.Token
	return nil
}

// testConnectivity tests connectivity to the marketplace
func (mc *MarketplaceClient) testConnectivity(ctx context.Context) error {
	healthURL := fmt.Sprintf("%s/api/v1/health", mc.baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create health request: %w", err)
	}

	resp, err := mc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("health request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("marketplace health check failed with status: %d", resp.StatusCode)
	}

	return nil
}

// addAuthHeaders adds authentication headers to requests
func (mc *MarketplaceClient) addAuthHeaders(req *http.Request) {
	if mc.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+mc.authToken)
	}
	req.Header.Set("User-Agent", "Forge-Plugin-Store/1.0")
}

// buildSearchCacheKey builds a cache key for search results
func (mc *MarketplaceClient) buildSearchCacheKey(query PluginQuery) string {
	key := fmt.Sprintf("search:%s:%s:%s:%s:%.2f:%s:%s:%d:%d",
		query.Name,
		query.Type,
		query.Author,
		query.Category,
		query.MinRating,
		strings.Join(query.Tags, ","),
		query.Sort,
		query.Limit,
		query.Offset,
	)
	return key
}

// Cache methods

func (mc *MarketplaceClient) getCachedSearch(key string) *CachedSearch {
	mc.cache.mu.RLock()
	defer mc.cache.mu.RUnlock()

	cached, exists := mc.cache.searches[key]
	if !exists {
		return nil
	}

	if time.Now().After(cached.ExpiresAt) {
		return nil
	}

	return cached
}

func (mc *MarketplaceClient) setCachedSearch(key string, search *CachedSearch) {
	mc.cache.mu.Lock()
	defer mc.cache.mu.Unlock()

	mc.cache.searches[key] = search
}

func (mc *MarketplaceClient) getCachedPlugin(pluginID string) *CachedPlugin {
	mc.cache.mu.RLock()
	defer mc.cache.mu.RUnlock()

	cached, exists := mc.cache.plugins[pluginID]
	if !exists {
		return nil
	}

	if time.Now().After(cached.ExpiresAt) {
		return nil
	}

	return cached
}

func (mc *MarketplaceClient) setCachedPlugin(pluginID string, plugin *CachedPlugin) {
	mc.cache.mu.Lock()
	defer mc.cache.mu.Unlock()

	mc.cache.plugins[pluginID] = plugin
}

func (mc *MarketplaceClient) getCachedVersions(pluginID string) *CachedVersions {
	mc.cache.mu.RLock()
	defer mc.cache.mu.RUnlock()

	cached, exists := mc.cache.versions[pluginID]
	if !exists {
		return nil
	}

	if time.Now().After(cached.ExpiresAt) {
		return nil
	}

	return cached
}

func (mc *MarketplaceClient) setCachedVersions(pluginID string, versions *CachedVersions) {
	mc.cache.mu.Lock()
	defer mc.cache.mu.Unlock()

	mc.cache.versions[pluginID] = versions
}

// Rate limiter methods

func (rl *RateLimiter) Allow(operation string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Clean up old requests
	if requests, exists := rl.requests[operation]; exists {
		cutoff := now.Add(-rl.window)
		validRequests := []time.Time{}

		for _, reqTime := range requests {
			if reqTime.After(cutoff) {
				validRequests = append(validRequests, reqTime)
			}
		}

		rl.requests[operation] = validRequests
	}

	// Check if we can make another request
	if len(rl.requests[operation]) >= rl.maxRequests {
		return false
	}

	// Add current request
	rl.requests[operation] = append(rl.requests[operation], now)
	return true
}
