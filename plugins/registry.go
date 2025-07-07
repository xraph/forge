package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/core"
	"github.com/xraph/forge/logger"
)

// registry implements the Registry interface
type registry struct {
	mu       sync.RWMutex
	config   RegistryConfig
	plugins  map[string]*PluginMetadata
	versions map[string][]string // plugin name -> versions
	logger   logger.Logger
	client   *http.Client

	// Cache
	cache    map[string]*cacheEntry
	cacheTTL time.Duration

	// Categories and tags
	categories map[string][]string // category -> plugin names
	tags       map[string][]string // tag -> plugin names

	// Statistics
	downloads map[string]int64 // plugin name -> download count
	popular   []string         // ordered by popularity
}

// cacheEntry represents a cached registry entry
type cacheEntry struct {
	data      interface{}
	timestamp time.Time
}

// NewRegistry creates a new plugin registry
func NewRegistry(config RegistryConfig) Registry {
	r := &registry{
		config:     config,
		plugins:    make(map[string]*PluginMetadata),
		versions:   make(map[string][]string),
		cache:      make(map[string]*cacheEntry),
		categories: make(map[string][]string),
		tags:       make(map[string][]string),
		downloads:  make(map[string]int64),
		cacheTTL:   config.TTL,
		logger:     logger.GetGlobalLogger().Named("plugin-registry"),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	// Set default TTL if not configured
	if r.cacheTTL == 0 {
		r.cacheTTL = 24 * time.Hour
	}

	// Initialize based on registry type
	switch config.Type {
	case "local":
		r.initializeLocal()
	case "remote":
		r.initializeRemote()
	case "hybrid":
		r.initializeLocal()
		r.initializeRemote()
	default:
		r.logger.Warn("Unknown registry type, defaulting to local",
			logger.String("type", config.Type),
		)
		r.initializeLocal()
	}

	return r
}

// Register registers a plugin in the registry
func (r *registry) Register(metadata PluginMetadata) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.validateMetadata(metadata); err != nil {
		return fmt.Errorf("metadata validation failed: %w", err)
	}

	name := metadata.Name
	version := metadata.Version

	// Store metadata
	r.plugins[name] = &metadata

	// Update versions
	if versions, exists := r.versions[name]; exists {
		// Check if version already exists
		for _, v := range versions {
			if v == version {
				// Update existing version
				r.logger.Info("Updating existing plugin version",
					logger.String("name", name),
					logger.String("version", version),
				)
				return r.persistMetadata(metadata)
			}
		}
		// Add new version
		r.versions[name] = append(versions, version)
	} else {
		r.versions[name] = []string{version}
	}

	// Sort versions (latest first)
	r.sortVersions(name)

	// Update categories and tags
	r.updateCategories(metadata)
	r.updateTags(metadata)

	// Persist metadata
	if err := r.persistMetadata(metadata); err != nil {
		r.logger.Error("Failed to persist metadata",
			logger.String("name", name),
			logger.Error(err),
		)
		return err
	}

	// Clear related cache entries
	r.invalidateCache(name)

	r.logger.Info("Plugin registered successfully",
		logger.String("name", name),
		logger.String("version", version),
		logger.String("category", metadata.Category),
	)

	return nil
}

// Unregister removes a plugin from the registry
func (r *registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	metadata, exists := r.plugins[name]
	if !exists {
		return fmt.Errorf("%w: %s", ErrPluginNotFound, name)
	}

	// Remove from all data structures
	delete(r.plugins, name)
	delete(r.versions, name)
	delete(r.downloads, name)

	// Update categories and tags
	r.removeFromCategories(metadata)
	r.removeFromTags(metadata)

	// Remove from popular list
	for i, p := range r.popular {
		if p == name {
			r.popular = append(r.popular[:i], r.popular[i+1:]...)
			break
		}
	}

	// Remove persisted data
	if err := r.removePersistedData(name); err != nil {
		r.logger.Error("Failed to remove persisted data",
			logger.String("name", name),
			logger.Error(err),
		)
	}

	// Clear cache
	r.invalidateCache(name)

	r.logger.Info("Plugin unregistered",
		logger.String("name", name),
	)

	return nil
}

// Search searches for plugins based on query
func (r *registry) Search(query string) []PluginMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if query == "" {
		return r.listAll()
	}

	// Check cache first
	cacheKey := "search:" + query
	if entry, exists := r.cache[cacheKey]; exists && time.Since(entry.timestamp) < r.cacheTTL {
		if results, ok := entry.data.([]PluginMetadata); ok {
			return results
		}
	}

	query = strings.ToLower(query)
	var results []PluginMetadata

	for _, metadata := range r.plugins {
		if r.matchesQuery(metadata, query) {
			results = append(results, *metadata)
		}
	}

	// Sort by relevance (name matches first, then description)
	sort.Slice(results, func(i, j int) bool {
		nameMatchI := strings.Contains(strings.ToLower(results[i].Name), query)
		nameMatchJ := strings.Contains(strings.ToLower(results[j].Name), query)

		if nameMatchI && !nameMatchJ {
			return true
		}
		if !nameMatchI && nameMatchJ {
			return false
		}

		// Secondary sort by downloads
		downloadsI := r.downloads[results[i].Name]
		downloadsJ := r.downloads[results[j].Name]
		return downloadsI > downloadsJ
	})

	// Cache results
	r.cache[cacheKey] = &cacheEntry{
		data:      results,
		timestamp: time.Now(),
	}

	r.logger.Debug("Search completed",
		logger.String("query", query),
		logger.Int("results", len(results)),
	)

	return results
}

// List returns all plugins
func (r *registry) List() []PluginMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.listAll()
}

// Get returns a specific plugin by name
func (r *registry) Get(name string) (*PluginMetadata, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	metadata, exists := r.plugins[name]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrPluginNotFound, name)
	}

	// Create a copy to avoid external modification
	result := *metadata
	return &result, nil
}

// ListVersions returns all versions for a plugin
func (r *registry) ListVersions(name string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	versions, exists := r.versions[name]
	if !exists {
		return []string{}
	}

	// Return a copy
	result := make([]string, len(versions))
	copy(result, versions)
	return result
}

// GetLatestVersion returns the latest version for a plugin
func (r *registry) GetLatestVersion(name string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	versions, exists := r.versions[name]
	if !exists || len(versions) == 0 {
		return "", fmt.Errorf("%w: %s", ErrPluginNotFound, name)
	}

	// First version in the sorted list is the latest
	return versions[0], nil
}

// ListCategories returns all available categories
func (r *registry) ListCategories() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	categories := make([]string, 0, len(r.categories))
	for category := range r.categories {
		categories = append(categories, category)
	}

	sort.Strings(categories)
	return categories
}

// GetByCategory returns plugins in a specific category
func (r *registry) GetByCategory(category string) []PluginMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()

	pluginNames, exists := r.categories[category]
	if !exists {
		return []PluginMetadata{}
	}

	var results []PluginMetadata
	for _, name := range pluginNames {
		if metadata, exists := r.plugins[name]; exists {
			results = append(results, *metadata)
		}
	}

	// Sort by popularity
	sort.Slice(results, func(i, j int) bool {
		downloadsI := r.downloads[results[i].Name]
		downloadsJ := r.downloads[results[j].Name]
		return downloadsI > downloadsJ
	})

	return results
}

// GetPopular returns popular plugins
func (r *registry) GetPopular(limit int) []PluginMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.popular) == 0 {
		r.updatePopular()
	}

	var results []PluginMetadata
	count := limit
	if count > len(r.popular) {
		count = len(r.popular)
	}

	for i := 0; i < count; i++ {
		name := r.popular[i]
		if metadata, exists := r.plugins[name]; exists {
			results = append(results, *metadata)
		}
	}

	return results
}

// GetRecommended returns recommended plugins
func (r *registry) GetRecommended() []PluginMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// For now, return popular plugins with high ratings
	// In a real implementation, this could use ML algorithms
	popular := r.GetPopular(10)

	// Filter for well-maintained plugins
	var recommended []PluginMetadata
	for _, plugin := range popular {
		// Check if plugin is well-maintained (updated recently)
		if time.Since(plugin.UpdatedAt) < 365*24*time.Hour {
			recommended = append(recommended, plugin)
		}
	}

	return recommended
}

// Private helper methods

func (r *registry) initializeLocal() {
	if r.config.CacheDir == "" {
		r.config.CacheDir = filepath.Join(os.TempDir(), "forge-registry")
	}

	// Create cache directory
	if err := os.MkdirAll(r.config.CacheDir, 0755); err != nil {
		r.logger.Error("Failed to create cache directory",
			logger.String("dir", r.config.CacheDir),
			logger.Error(err),
		)
		return
	}

	// Load existing metadata
	r.loadLocalMetadata()

	r.logger.Info("Local registry initialized",
		logger.String("cache_dir", r.config.CacheDir),
	)
}

func (r *registry) initializeRemote() {
	if r.config.URL == "" {
		r.logger.Warn("Remote registry URL not configured")
		return
	}

	// Configure HTTP client with auth if provided
	if len(r.config.Auth) > 0 {
		// Add authentication headers
		// This is a simplified implementation
	}

	// Sync with remote registry
	go r.syncWithRemote()

	r.logger.Info("Remote registry initialized",
		logger.String("url", r.config.URL),
	)
}

func (r *registry) loadLocalMetadata() {
	indexPath := filepath.Join(r.config.CacheDir, "index.json")

	data, err := os.ReadFile(indexPath)
	if err != nil {
		if !os.IsNotExist(err) {
			r.logger.Error("Failed to read registry index",
				logger.String("path", indexPath),
				logger.Error(err),
			)
		}
		return
	}

	var index struct {
		Plugins    map[string]*PluginMetadata `json:"plugins"`
		Versions   map[string][]string        `json:"versions"`
		Downloads  map[string]int64           `json:"downloads"`
		Categories map[string][]string        `json:"categories"`
		Tags       map[string][]string        `json:"tags"`
		Popular    []string                   `json:"popular"`
	}

	if err := json.Unmarshal(data, &index); err != nil {
		r.logger.Error("Failed to parse registry index",
			logger.Error(err),
		)
		return
	}

	// Load data
	r.plugins = index.Plugins
	r.versions = index.Versions
	r.downloads = index.Downloads
	r.categories = index.Categories
	r.tags = index.Tags
	r.popular = index.Popular

	// Initialize empty maps if nil
	if r.plugins == nil {
		r.plugins = make(map[string]*PluginMetadata)
	}
	if r.versions == nil {
		r.versions = make(map[string][]string)
	}
	if r.downloads == nil {
		r.downloads = make(map[string]int64)
	}
	if r.categories == nil {
		r.categories = make(map[string][]string)
	}
	if r.tags == nil {
		r.tags = make(map[string][]string)
	}

	r.logger.Info("Loaded local registry metadata",
		logger.Int("plugins", len(r.plugins)),
	)
}

func (r *registry) saveLocalMetadata() error {
	indexPath := filepath.Join(r.config.CacheDir, "index.json")

	index := struct {
		Plugins    map[string]*PluginMetadata `json:"plugins"`
		Versions   map[string][]string        `json:"versions"`
		Downloads  map[string]int64           `json:"downloads"`
		Categories map[string][]string        `json:"categories"`
		Tags       map[string][]string        `json:"tags"`
		Popular    []string                   `json:"popular"`
		UpdatedAt  time.Time                  `json:"updated_at"`
	}{
		Plugins:    r.plugins,
		Versions:   r.versions,
		Downloads:  r.downloads,
		Categories: r.categories,
		Tags:       r.tags,
		Popular:    r.popular,
		UpdatedAt:  time.Now(),
	}

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal index: %w", err)
	}

	if err := os.WriteFile(indexPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write index: %w", err)
	}

	return nil
}

func (r *registry) syncWithRemote() {
	ctx := context.Background()
	ticker := time.NewTicker(1 * time.Hour) // Sync every hour
	defer ticker.Stop()

	// Initial sync
	r.performRemoteSync(ctx)

	for {
		select {
		case <-ticker.C:
			r.performRemoteSync(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (r *registry) performRemoteSync(ctx context.Context) {
	r.logger.Debug("Starting remote sync",
		logger.String("url", r.config.URL),
	)

	req, err := http.NewRequestWithContext(ctx, "GET", r.config.URL+"/plugins", nil)
	if err != nil {
		r.logger.Error("Failed to create sync request", logger.Error(err))
		return
	}

	// Add authentication headers
	for key, value := range r.config.Auth {
		req.Header.Set(key, value)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		r.logger.Error("Failed to sync with remote registry", logger.Error(err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		r.logger.Error("Remote sync failed",
			logger.Int("status", resp.StatusCode),
		)
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		r.logger.Error("Failed to read sync response", logger.Error(err))
		return
	}

	var remotePlugins []PluginMetadata
	if err := json.Unmarshal(body, &remotePlugins); err != nil {
		r.logger.Error("Failed to parse sync response", logger.Error(err))
		return
	}

	// Merge remote plugins
	r.mu.Lock()
	updated := 0
	for _, plugin := range remotePlugins {
		if existing, exists := r.plugins[plugin.Name]; exists {
			// Check if remote version is newer
			if plugin.UpdatedAt.After(existing.UpdatedAt) {
				r.plugins[plugin.Name] = &plugin
				r.updateVersions(plugin)
				r.updateCategories(plugin)
				r.updateTags(plugin)
				updated++
			}
		} else {
			// New plugin
			r.plugins[plugin.Name] = &plugin
			r.updateVersions(plugin)
			r.updateCategories(plugin)
			r.updateTags(plugin)
			updated++
		}
	}
	r.mu.Unlock()

	// Save updated metadata
	if updated > 0 {
		if err := r.saveLocalMetadata(); err != nil {
			r.logger.Error("Failed to save synced metadata", logger.Error(err))
		}
	}

	r.logger.Info("Remote sync completed",
		logger.Int("updated", updated),
		logger.Int("total", len(remotePlugins)),
	)
}

func (r *registry) validateMetadata(metadata PluginMetadata) error {
	if metadata.Name == "" {
		return fmt.Errorf("plugin name is required")
	}

	if metadata.Version == "" {
		return fmt.Errorf("plugin version is required")
	}

	if metadata.Author == "" {
		return fmt.Errorf("plugin author is required")
	}

	if metadata.ForgeVersion == "" {
		return fmt.Errorf("forge version is required")
	}

	// Validate version format (basic check)
	if !strings.Contains(metadata.Version, ".") {
		return fmt.Errorf("invalid version format: %s", metadata.Version)
	}

	return nil
}

func (r *registry) matchesQuery(metadata *PluginMetadata, query string) bool {
	query = strings.ToLower(query)

	// Check name
	if strings.Contains(strings.ToLower(metadata.Name), query) {
		return true
	}

	// Check description
	if strings.Contains(strings.ToLower(metadata.Description), query) {
		return true
	}

	// Check keywords
	for _, keyword := range metadata.Keywords {
		if strings.Contains(strings.ToLower(keyword), query) {
			return true
		}
	}

	// Check tags
	for _, tag := range metadata.Tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}

	// Check category
	if strings.Contains(strings.ToLower(metadata.Category), query) {
		return true
	}

	// Check author
	if strings.Contains(strings.ToLower(metadata.Author), query) {
		return true
	}

	return false
}

func (r *registry) listAll() []PluginMetadata {
	var results []PluginMetadata
	for _, metadata := range r.plugins {
		results = append(results, *metadata)
	}

	// Sort by popularity
	sort.Slice(results, func(i, j int) bool {
		downloadsI := r.downloads[results[i].Name]
		downloadsJ := r.downloads[results[j].Name]
		return downloadsI > downloadsJ
	})

	return results
}

func (r *registry) updateVersions(metadata PluginMetadata) {
	name := metadata.Name
	version := metadata.Version

	if versions, exists := r.versions[name]; exists {
		// Check if version already exists
		for _, v := range versions {
			if v == version {
				return
			}
		}
		r.versions[name] = append(versions, version)
	} else {
		r.versions[name] = []string{version}
	}

	r.sortVersions(name)
}

func (r *registry) sortVersions(name string) {
	versions := r.versions[name]
	if len(versions) <= 1 {
		return
	}

	// Simple version sorting (latest first)
	// In a real implementation, use semantic versioning
	sort.Slice(versions, func(i, j int) bool {
		return versions[i] > versions[j]
	})

	r.versions[name] = versions
}

func (r *registry) updateCategories(metadata PluginMetadata) {
	if metadata.Category == "" {
		return
	}

	category := metadata.Category
	name := metadata.Name

	if plugins, exists := r.categories[category]; exists {
		// Check if already exists
		for _, p := range plugins {
			if p == name {
				return
			}
		}
		r.categories[category] = append(plugins, name)
	} else {
		r.categories[category] = []string{name}
	}
}

func (r *registry) updateTags(metadata PluginMetadata) {
	name := metadata.Name

	for _, tag := range metadata.Tags {
		if plugins, exists := r.tags[tag]; exists {
			// Check if already exists
			found := false
			for _, p := range plugins {
				if p == name {
					found = true
					break
				}
			}
			if !found {
				r.tags[tag] = append(plugins, name)
			}
		} else {
			r.tags[tag] = []string{name}
		}
	}
}

func (r *registry) removeFromCategories(metadata *PluginMetadata) {
	if metadata.Category == "" {
		return
	}

	category := metadata.Category
	name := metadata.Name

	if plugins, exists := r.categories[category]; exists {
		for i, p := range plugins {
			if p == name {
				r.categories[category] = append(plugins[:i], plugins[i+1:]...)
				break
			}
		}

		// Remove category if empty
		if len(r.categories[category]) == 0 {
			delete(r.categories, category)
		}
	}
}

func (r *registry) removeFromTags(metadata *PluginMetadata) {
	name := metadata.Name

	for _, tag := range metadata.Tags {
		if plugins, exists := r.tags[tag]; exists {
			for i, p := range plugins {
				if p == name {
					r.tags[tag] = append(plugins[:i], plugins[i+1:]...)
					break
				}
			}

			// Remove tag if empty
			if len(r.tags[tag]) == 0 {
				delete(r.tags, tag)
			}
		}
	}
}

func (r *registry) updatePopular() {
	// Sort plugins by download count
	type pluginDownload struct {
		name      string
		downloads int64
	}

	var plugins []pluginDownload
	for name, downloads := range r.downloads {
		plugins = append(plugins, pluginDownload{
			name:      name,
			downloads: downloads,
		})
	}

	sort.Slice(plugins, func(i, j int) bool {
		return plugins[i].downloads > plugins[j].downloads
	})

	// Update popular list
	r.popular = make([]string, len(plugins))
	for i, p := range plugins {
		r.popular[i] = p.name
	}
}

func (r *registry) persistMetadata(metadata PluginMetadata) error {
	// Save individual plugin metadata file
	pluginDir := filepath.Join(r.config.CacheDir, "plugins", metadata.Name)
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		return fmt.Errorf("failed to create plugin directory: %w", err)
	}

	metadataPath := filepath.Join(pluginDir, "metadata.json")
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	// Save registry index
	return r.saveLocalMetadata()
}

func (r *registry) removePersistedData(name string) error {
	pluginDir := filepath.Join(r.config.CacheDir, "plugins", name)
	if err := os.RemoveAll(pluginDir); err != nil {
		return fmt.Errorf("failed to remove plugin directory: %w", err)
	}

	return r.saveLocalMetadata()
}

func (r *registry) invalidateCache(pattern string) {
	for key := range r.cache {
		if strings.Contains(key, pattern) {
			delete(r.cache, key)
		}
	}
}

// Public utility methods

// IncrementDownloads increments the download count for a plugin
func (r *registry) IncrementDownloads(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.downloads[name]++

	// Periodically update popular list
	if r.downloads[name]%100 == 0 {
		r.updatePopular()
	}
}

// GetDownloadCount returns the download count for a plugin
func (r *registry) GetDownloadCount(name string) int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.downloads[name]
}

// GetStatistics returns registry statistics
func (r *registry) GetStatistics() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	totalDownloads := int64(0)
	for _, downloads := range r.downloads {
		totalDownloads += downloads
	}

	return map[string]interface{}{
		"total_plugins":   len(r.plugins),
		"total_versions":  r.getTotalVersions(),
		"total_downloads": totalDownloads,
		"categories":      len(r.categories),
		"tags":            len(r.tags),
		"cache_entries":   len(r.cache),
		"registry_type":   r.config.Type,
	}
}

func (r *registry) getTotalVersions() int {
	total := 0
	for _, versions := range r.versions {
		total += len(versions)
	}
	return total
}

// ClearCache clears the registry cache
func (r *registry) ClearCache() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.cache = make(map[string]*cacheEntry)
	r.logger.Info("Registry cache cleared")
}

// Refresh forces a refresh of the registry
func (r *registry) Refresh() error {
	r.ClearCache()

	if r.config.Type == "remote" || r.config.Type == "hybrid" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		r.performRemoteSync(ctx)
	}

	r.logger.Info("Registry refreshed")
	return nil
}

// Sandboxed plugin wrapper
type sandboxedPlugin struct {
	Plugin
	sandbox Sandbox
	logger  logger.Logger
}

func (sp *sandboxedPlugin) Initialize(container core.Container) error {
	sp.logger.Debug("Sandboxed plugin initializing",
		logger.String("name", sp.Plugin.Name()),
	)

	return sp.sandbox.Execute(sp.Plugin, func() error {
		return sp.Plugin.Initialize(container)
	})
}

func (sp *sandboxedPlugin) Start(ctx context.Context) error {
	sp.logger.Debug("Sandboxed plugin starting",
		logger.String("name", sp.Plugin.Name()),
	)

	return sp.sandbox.Execute(sp.Plugin, func() error {
		return sp.Plugin.Start(ctx)
	})
}

func (sp *sandboxedPlugin) Stop(ctx context.Context) error {
	sp.logger.Debug("Sandboxed plugin stopping",
		logger.String("name", sp.Plugin.Name()),
	)

	return sp.sandbox.Execute(sp.Plugin, func() error {
		return sp.Plugin.Stop(ctx)
	})
}
