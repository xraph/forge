package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	plugins "github.com/xraph/forge/pkg/pluginengine/common"
)

// PluginRegistry manages both plugin instances and metadata storage
type PluginRegistry struct {
	config         StoreConfig
	pluginEntries  map[string]*PluginRegistryEntry // Plugin metadata storage
	pluginInstance map[string]plugins.PluginEngine       // Active plugin instances
	versions       map[string][]string
	categories     map[string][]string
	publishers     map[string][]string
	mu             sync.RWMutex
	dataDir        string
	indexFile      string
	logger         common.Logger
	metrics        common.Metrics
	initialized    bool
}

// PluginRegistryEntry represents a plugin entry in the registry
type PluginRegistryEntry struct {
	Info         plugins.PluginEngineInfo         `json:"info"`
	Versions     []PluginVersionInfo        `json:"versions"`
	LocalPath    string                     `json:"local_path"`
	Installed    bool                       `json:"installed"`
	LastUpdated  time.Time                  `json:"last_updated"`
	Dependencies []plugins.PluginEngineDependency `json:"dependencies"`
	Checksums    map[string]string          `json:"checksums"`
	Metadata     map[string]interface{}     `json:"metadata"`
}

// PluginVersionInfo contains version-specific information
type PluginVersionInfo struct {
	Version     string                 `json:"version"`
	ReleaseDate time.Time              `json:"release_date"`
	Checksum    string                 `json:"checksum"`
	Size        int64                  `json:"size"`
	Changes     []string               `json:"changes"`
	Metadata    map[string]interface{} `json:"metadata"`
	Deprecated  bool                   `json:"deprecated"`
	Prerelease  bool                   `json:"prerelease"`
}

// RegistryIndex represents the registry index file
type RegistryIndex struct {
	Version     string                          `json:"version"`
	LastUpdated time.Time                       `json:"last_updated"`
	Plugins     map[string]*PluginRegistryEntry `json:"plugins"`
	Categories  map[string][]string             `json:"categories"`
	Publishers  map[string][]string             `json:"publishers"`
	Stats       plugins.RegistryStats           `json:"stats"`
}

// NewPluginRegistry creates a new plugin registry
func NewPluginRegistry(config StoreConfig, logger common.Logger, metrics common.Metrics) *PluginRegistry {
	dataDir := filepath.Join(config.CacheDirectory, "registry")
	indexFile := filepath.Join(dataDir, "index.json")

	return &PluginRegistry{
		config:         config,
		pluginEntries:  make(map[string]*PluginRegistryEntry),
		pluginInstance: make(map[string]plugins.PluginEngine),
		versions:       make(map[string][]string),
		categories:     make(map[string][]string),
		publishers:     make(map[string][]string),
		dataDir:        dataDir,
		indexFile:      indexFile,
		logger:         logger,
		metrics:        metrics,
	}
}

// Initialize initializes the plugin registry
func (pr *PluginRegistry) Initialize(ctx context.Context) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if pr.initialized {
		return nil
	}

	// Create data directory
	if err := os.MkdirAll(pr.dataDir, 0755); err != nil {
		return fmt.Errorf("failed to create registry data directory: %w", err)
	}

	// Load existing index
	if err := pr.loadIndex(); err != nil {
		pr.logger.Warn("failed to load registry index, starting fresh", logger.Error(err))
	}

	// Scan for local plugins
	if err := pr.scanLocalPlugins(ctx); err != nil {
		pr.logger.Warn("failed to scan local plugins", logger.Error(err))
	}

	pr.initialized = true

	pr.logger.Info("plugin registry initialized",
		logger.String("data_dir", pr.dataDir),
		logger.Int("total_plugins", len(pr.pluginEntries)),
	)

	if pr.metrics != nil {
		pr.metrics.Counter("forge.plugins.registry_initialized").Inc()
		pr.metrics.Gauge("forge.plugins.registry_plugins").Set(float64(len(pr.pluginEntries)))
	}

	return nil
}

// Stop stops the plugin registry
func (pr *PluginRegistry) Stop(ctx context.Context) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if !pr.initialized {
		return nil
	}

	// Save index
	if err := pr.saveIndex(); err != nil {
		pr.logger.Error("failed to save registry index", logger.Error(err))
	}

	pr.initialized = false

	pr.logger.Info("plugin registry stopped")

	if pr.metrics != nil {
		pr.metrics.Counter("forge.plugins.registry_stopped").Inc()
	}

	return nil
}

// Register registers a plugin instance in the registry
func (pr *PluginRegistry) Register(plugin plugins.PluginEngine) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if !pr.initialized {
		return fmt.Errorf("plugin registry not initialized")
	}

	pluginID := plugin.ID()

	// Store the plugin instance
	pr.pluginInstance[pluginID] = plugin

	// Create or update plugin entry from instance
	info := plugins.PluginEngineInfo{
		ID:           plugin.ID(),
		Name:         plugin.Name(),
		Version:      plugin.Version(),
		Description:  plugin.Description(),
		Author:       plugin.Author(),
		License:      plugin.License(),
		Type:         plugin.Type(),
		Capabilities: plugin.Capabilities(),
		Dependencies: plugin.Dependencies(),
		ConfigSchema: plugin.ConfigSchema(),
		UpdatedAt:    time.Now(),
	}

	entry := &PluginRegistryEntry{
		Info:         info,
		Versions:     []PluginVersionInfo{},
		LocalPath:    "",
		Installed:    true,
		LastUpdated:  time.Now(),
		Dependencies: plugin.Dependencies(),
		Checksums:    make(map[string]string),
		Metadata:     make(map[string]interface{}),
	}

	// Add version info
	versionInfo := PluginVersionInfo{
		Version:     plugin.Version(),
		ReleaseDate: time.Now(),
		Checksum:    "",
		Size:        0,
		Changes:     []string{},
		Metadata:    make(map[string]interface{}),
		Deprecated:  false,
		Prerelease:  false,
	}
	entry.Versions = append(entry.Versions, versionInfo)

	pr.pluginEntries[pluginID] = entry

	// Update indices
	pr.updateIndices(entry)

	pr.logger.Debug("plugin registered",
		logger.String("plugin_id", pluginID),
		logger.String("version", plugin.Version()),
	)

	if pr.metrics != nil {
		pr.metrics.Counter("forge.plugins.registry_register").Inc()
	}

	return nil
}

// Unregister removes a plugin from the registry
func (pr *PluginRegistry) Unregister(pluginID string) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if !pr.initialized {
		return fmt.Errorf("plugin registry not initialized")
	}

	// Remove plugin instance
	delete(pr.pluginInstance, pluginID)

	// Update entry to mark as uninstalled
	if entry, exists := pr.pluginEntries[pluginID]; exists {
		entry.Installed = false
		entry.LocalPath = ""
	}

	pr.logger.Debug("plugin unregistered",
		logger.String("plugin_id", pluginID),
	)

	if pr.metrics != nil {
		pr.metrics.Counter("forge.plugins.registry_unregister").Inc()
	}

	return nil
}

// Get retrieves a plugin instance by ID
func (pr *PluginRegistry) Get(pluginID string) (plugins.PluginEngine, error) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	if !pr.initialized {
		return nil, fmt.Errorf("plugin registry not initialized")
	}

	plugin, exists := pr.pluginInstance[pluginID]
	if !exists {
		return nil, fmt.Errorf("plugin not found: %s", pluginID)
	}

	if pr.metrics != nil {
		pr.metrics.Counter("forge.plugins.registry_get").Inc()
	}

	return plugin, nil
}

// List returns all registered plugin instances
func (pr *PluginRegistry) List() []plugins.PluginEngine {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	plugins := make([]plugins.PluginEngine, 0, len(pr.pluginInstance))
	for _, plugin := range pr.pluginInstance {
		plugins = append(plugins, plugin)
	}

	return plugins
}

// ListByType returns plugins filtered by type
func (pr *PluginRegistry) ListByType(pluginType plugins.PluginEngineType) []plugins.PluginEngine {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	var filteredPlugins []plugins.PluginEngine
	for _, plugin := range pr.pluginInstance {
		if plugin.Type() == pluginType {
			filteredPlugins = append(filteredPlugins, plugin)
		}
	}

	return filteredPlugins
}

// Search searches for plugins by query string
func (pr *PluginRegistry) Search(query string) []plugins.PluginEngine {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	var results []plugins.PluginEngine
	queryLower := strings.ToLower(query)

	for _, plugin := range pr.pluginInstance {
		if strings.Contains(strings.ToLower(plugin.Name()), queryLower) ||
			strings.Contains(strings.ToLower(plugin.Description()), queryLower) ||
			strings.Contains(strings.ToLower(plugin.ID()), queryLower) {
			results = append(results, plugin)
		}
	}

	return results
}

// GetStats returns registry statistics
func (pr *PluginRegistry) GetStats() plugins.RegistryStats {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	stats := plugins.RegistryStats{
		TotalPlugins:     len(pr.pluginInstance),
		PluginsByType:    make(map[plugins.PluginEngineType]int),
		PluginsByState:   make(map[plugins.PluginEngineState]int),
		LoadedPlugins:    len(pr.pluginInstance),
		ActivePlugins:    0,
		FailedPlugins:    0,
		TotalMemoryUsage: 0,
		AverageCPUUsage:  0,
		LastUpdated:      time.Now(),
	}

	// Count by type
	for _, plugin := range pr.pluginInstance {
		stats.PluginsByType[plugin.Type()]++
		stats.ActivePlugins++
	}

	// Count by state (simplified - all loaded plugins are considered started)
	stats.PluginsByState[plugins.PluginEngineStateStarted] = len(pr.pluginInstance)

	return stats
}

// Metadata management methods (for backwards compatibility)

// SearchMetadata searches for plugins in the registry metadata
func (pr *PluginRegistry) SearchMetadata(ctx context.Context, query PluginQuery) ([]plugins.PluginEngineInfo, error) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	if !pr.initialized {
		return nil, fmt.Errorf("plugin registry not initialized")
	}

	var results []plugins.PluginEngineInfo

	for _, entry := range pr.pluginEntries {
		if pr.matchesQuery(entry, query) {
			results = append(results, entry.Info)
		}
	}

	// Sort results
	pr.sortResults(results, query.Sort)

	pr.logger.Debug("registry metadata search completed",
		logger.String("query", query.Name),
		logger.Int("results", len(results)),
	)

	if pr.metrics != nil {
		pr.metrics.Counter("forge.plugins.registry_search_metadata").Inc()
	}

	return results, nil
}

// GetPluginInfo retrieves plugin information
func (pr *PluginRegistry) GetPluginInfo(ctx context.Context, pluginID string) (*plugins.PluginEngineInfo, error) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	if !pr.initialized {
		return nil, fmt.Errorf("plugin registry not initialized")
	}

	entry, exists := pr.pluginEntries[pluginID]
	if !exists {
		return nil, fmt.Errorf("plugin not found: %s", pluginID)
	}

	if pr.metrics != nil {
		pr.metrics.Counter("forge.plugins.registry_get_info").Inc()
	}

	return &entry.Info, nil
}

// GetVersions retrieves available versions for a plugin
func (pr *PluginRegistry) GetVersions(ctx context.Context, pluginID string) ([]string, error) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	if !pr.initialized {
		return nil, fmt.Errorf("plugin registry not initialized")
	}

	entry, exists := pr.pluginEntries[pluginID]
	if !exists {
		return nil, fmt.Errorf("plugin not found: %s", pluginID)
	}

	versions := make([]string, 0, len(entry.Versions))
	for _, v := range entry.Versions {
		if !v.Deprecated {
			versions = append(versions, v.Version)
		}
	}

	if pr.metrics != nil {
		pr.metrics.Counter("forge.plugins.registry_get_versions").Inc()
	}

	return versions, nil
}

// Download downloads a plugin package from the registry
func (pr *PluginRegistry) Download(ctx context.Context, pluginID, version string) (plugins.PluginEnginePackage, error) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	if !pr.initialized {
		return plugins.PluginEnginePackage{}, fmt.Errorf("plugin registry not initialized")
	}

	entry, exists := pr.pluginEntries[pluginID]
	if !exists {
		return plugins.PluginEnginePackage{}, fmt.Errorf("plugin not found: %s", pluginID)
	}

	// Find version
	var versionInfo *PluginVersionInfo
	for _, v := range entry.Versions {
		if v.Version == version {
			versionInfo = &v
			break
		}
	}

	if versionInfo == nil {
		return plugins.PluginEnginePackage{}, fmt.Errorf("version not found: %s", version)
	}

	// Load package from local storage
	packagePath := filepath.Join(pr.dataDir, "packages", pluginID, fmt.Sprintf("%s.zip", version))
	packageData, err := os.ReadFile(packagePath)
	if err != nil {
		return plugins.PluginEnginePackage{}, fmt.Errorf("failed to read package: %w", err)
	}

	// Create package
	pkg := plugins.PluginEnginePackage{
		Info:     entry.Info,
		Binary:   packageData,
		Checksum: versionInfo.Checksum,
	}

	// Load additional files if they exist
	configPath := filepath.Join(pr.dataDir, "configs", pluginID, fmt.Sprintf("%s.json", version))
	if configData, err := os.ReadFile(configPath); err == nil {
		pkg.Config = configData
	}

	docsPath := filepath.Join(pr.dataDir, "docs", pluginID, fmt.Sprintf("%s.md", version))
	if docsData, err := os.ReadFile(docsPath); err == nil {
		pkg.Docs = docsData
	}

	pr.logger.Debug("plugin package downloaded from registry",
		logger.String("plugin_id", pluginID),
		logger.String("version", version),
		logger.Int("size", len(packageData)),
	)

	if pr.metrics != nil {
		pr.metrics.Counter("forge.plugins.registry_download").Inc()
		pr.metrics.Histogram("forge.plugins.registry_download_size").Observe(float64(len(packageData)))
	}

	return pkg, nil
}

// Publish publishes a plugin to the registry
func (pr *PluginRegistry) Publish(ctx context.Context, pkg plugins.PluginEnginePackage) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if !pr.initialized {
		return fmt.Errorf("plugin registry not initialized")
	}

	pluginID := pkg.Info.ID
	version := pkg.Info.Version

	// Create or update plugin entry
	entry, exists := pr.pluginEntries[pluginID]
	if !exists {
		entry = &PluginRegistryEntry{
			Info:         pkg.Info,
			Versions:     []PluginVersionInfo{},
			LocalPath:    "",
			Installed:    false,
			LastUpdated:  time.Now(),
			Dependencies: pkg.Info.Dependencies,
			Checksums:    make(map[string]string),
			Metadata:     make(map[string]interface{}),
		}
		pr.pluginEntries[pluginID] = entry
	}

	// Add version info
	versionInfo := PluginVersionInfo{
		Version:     version,
		ReleaseDate: time.Now(),
		Checksum:    pkg.Checksum,
		Size:        int64(len(pkg.Binary)),
		Changes:     []string{},
		Metadata:    make(map[string]interface{}),
		Deprecated:  false,
		Prerelease:  false,
	}

	// Check if version already exists
	versionExists := false
	for i, v := range entry.Versions {
		if v.Version == version {
			entry.Versions[i] = versionInfo
			versionExists = true
			break
		}
	}

	if !versionExists {
		entry.Versions = append(entry.Versions, versionInfo)
	}

	// Sort versions
	sort.Slice(entry.Versions, func(i, j int) bool {
		return entry.Versions[i].ReleaseDate.After(entry.Versions[j].ReleaseDate)
	})

	// Update entry
	entry.LastUpdated = time.Now()
	entry.Checksums[version] = pkg.Checksum

	// Save package files
	if err := pr.savePackageFiles(pluginID, version, pkg); err != nil {
		return fmt.Errorf("failed to save package files: %w", err)
	}

	// Update indices
	pr.updateIndices(entry)

	// Save index
	if err := pr.saveIndex(); err != nil {
		pr.logger.Error("failed to save registry index", logger.Error(err))
	}

	pr.logger.Info("plugin published to registry",
		logger.String("plugin_id", pluginID),
		logger.String("version", version),
		logger.Int("total_versions", len(entry.Versions)),
	)

	if pr.metrics != nil {
		pr.metrics.Counter("forge.plugins.registry_publish").Inc()
		pr.metrics.Gauge("forge.plugins.registry_plugins").Set(float64(len(pr.pluginEntries)))
	}

	return nil
}

// RegisterLocalPlugin registers a locally installed plugin
func (pr *PluginRegistry) RegisterLocalPlugin(ctx context.Context, pluginInfo plugins.PluginEngineInfo, localPath string) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if !pr.initialized {
		return fmt.Errorf("plugin registry not initialized")
	}

	pluginID := pluginInfo.ID

	// Create or update plugin entry
	entry, exists := pr.pluginEntries[pluginID]
	if !exists {
		entry = &PluginRegistryEntry{
			Info:         pluginInfo,
			Versions:     []PluginVersionInfo{},
			LocalPath:    localPath,
			Installed:    true,
			LastUpdated:  time.Now(),
			Dependencies: pluginInfo.Dependencies,
			Checksums:    make(map[string]string),
			Metadata:     make(map[string]interface{}),
		}
		pr.pluginEntries[pluginID] = entry
	} else {
		entry.LocalPath = localPath
		entry.Installed = true
		entry.LastUpdated = time.Now()
	}

	// Add version info if not exists
	versionExists := false
	for _, v := range entry.Versions {
		if v.Version == pluginInfo.Version {
			versionExists = true
			break
		}
	}

	if !versionExists {
		versionInfo := PluginVersionInfo{
			Version:     pluginInfo.Version,
			ReleaseDate: time.Now(),
			Checksum:    "",
			Size:        0,
			Changes:     []string{},
			Metadata:    make(map[string]interface{}),
			Deprecated:  false,
			Prerelease:  false,
		}
		entry.Versions = append(entry.Versions, versionInfo)
	}

	// Update indices
	pr.updateIndices(entry)

	pr.logger.Debug("local plugin registered",
		logger.String("plugin_id", pluginID),
		logger.String("version", pluginInfo.Version),
		logger.String("local_path", localPath),
	)

	if pr.metrics != nil {
		pr.metrics.Counter("forge.plugins.registry_register_local").Inc()
	}

	return nil
}

// UnregisterLocalPlugin unregisters a locally installed plugin
func (pr *PluginRegistry) UnregisterLocalPlugin(ctx context.Context, pluginID string) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if !pr.initialized {
		return fmt.Errorf("plugin registry not initialized")
	}

	entry, exists := pr.pluginEntries[pluginID]
	if !exists {
		return fmt.Errorf("plugin not found: %s", pluginID)
	}

	entry.Installed = false
	entry.LocalPath = ""

	pr.logger.Debug("local plugin unregistered",
		logger.String("plugin_id", pluginID),
	)

	if pr.metrics != nil {
		pr.metrics.Counter("forge.plugins.registry_unregister_local").Inc()
	}

	return nil
}

// GetCategories returns all available plugin categories
func (pr *PluginRegistry) GetCategories() []string {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	var categories []string
	for category := range pr.categories {
		categories = append(categories, category)
	}

	sort.Strings(categories)
	return categories
}

// GetPublishers returns all available plugin publishers
func (pr *PluginRegistry) GetPublishers() []string {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	var publishers []string
	for publisher := range pr.publishers {
		publishers = append(publishers, publisher)
	}

	sort.Strings(publishers)
	return publishers
}

// Helper methods

func (pr *PluginRegistry) loadIndex() error {
	if _, err := os.Stat(pr.indexFile); os.IsNotExist(err) {
		return nil // No index file exists yet
	}

	data, err := os.ReadFile(pr.indexFile)
	if err != nil {
		return fmt.Errorf("failed to read index file: %w", err)
	}

	var index RegistryIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return fmt.Errorf("failed to unmarshal index: %w", err)
	}

	pr.pluginEntries = index.Plugins
	pr.categories = index.Categories
	pr.publishers = index.Publishers

	// Build versions map
	pr.versions = make(map[string][]string)
	for pluginID, entry := range pr.pluginEntries {
		versions := make([]string, 0, len(entry.Versions))
		for _, v := range entry.Versions {
			versions = append(versions, v.Version)
		}
		pr.versions[pluginID] = versions
	}

	return nil
}

func (pr *PluginRegistry) saveIndex() error {
	index := RegistryIndex{
		Version:     "1.0",
		LastUpdated: time.Now(),
		Plugins:     pr.pluginEntries,
		Categories:  pr.categories,
		Publishers:  pr.publishers,
		Stats:       pr.GetStats(),
	}

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal index: %w", err)
	}

	if err := os.WriteFile(pr.indexFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write index file: %w", err)
	}

	return nil
}

func (pr *PluginRegistry) scanLocalPlugins(ctx context.Context) error {
	pluginDir := filepath.Join(pr.config.CacheDirectory, "plugins")
	if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
		return nil // No plugins directory exists
	}

	return filepath.Walk(pluginDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(path, ".json") {
			// Try to load plugin info
			data, err := os.ReadFile(path)
			if err != nil {
				return nil // Skip files that can't be read
			}

			var pluginInfo plugins.PluginEngineInfo
			if err := json.Unmarshal(data, &pluginInfo); err != nil {
				return nil // Skip files that can't be parsed
			}

			// Register the plugin
			if err := pr.RegisterLocalPlugin(ctx, pluginInfo, path); err != nil {
				pr.logger.Warn("failed to register local plugin",
					logger.String("path", path),
					logger.Error(err),
				)
			}
		}

		return nil
	})
}

func (pr *PluginRegistry) matchesQuery(entry *PluginRegistryEntry, query PluginQuery) bool {
	// Name search
	if query.Name != "" {
		if !strings.Contains(strings.ToLower(entry.Info.Name), strings.ToLower(query.Name)) &&
			!strings.Contains(strings.ToLower(entry.Info.Description), strings.ToLower(query.Name)) {
			return false
		}
	}

	// Type filter
	if query.Type != "" && entry.Info.Type != query.Type {
		return false
	}

	// Author filter
	if query.Author != "" && entry.Info.Author != query.Author {
		return false
	}

	// Category filter
	if query.Category != "" && entry.Info.Category != query.Category {
		return false
	}

	// Rating filter
	if query.MinRating > 0 && entry.Info.Rating < query.MinRating {
		return false
	}

	// Tags filter
	if len(query.Tags) > 0 {
		for _, tag := range query.Tags {
			found := false
			for _, entryTag := range entry.Info.Tags {
				if strings.EqualFold(entryTag, tag) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}

	// Compatibility filter
	if query.Compatible != "" {
		found := false
		for _, compat := range entry.Info.Compatibility {
			if compat == query.Compatible {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

func (pr *PluginRegistry) sortResults(results []plugins.PluginEngineInfo, sortOrder PluginSortOrder) {
	switch sortOrder {
	case SortByName:
		sort.Slice(results, func(i, j int) bool {
			return results[i].Name < results[j].Name
		})
	case SortByRating:
		sort.Slice(results, func(i, j int) bool {
			return results[i].Rating > results[j].Rating
		})
	case SortByDownloads:
		sort.Slice(results, func(i, j int) bool {
			return results[i].Downloads > results[j].Downloads
		})
	case SortByRecent:
		sort.Slice(results, func(i, j int) bool {
			return results[i].UpdatedAt.After(results[j].UpdatedAt)
		})
	default:
		// Default to relevance (no sorting)
	}
}

func (pr *PluginRegistry) updateIndices(entry *PluginRegistryEntry) {
	// Update categories
	if entry.Info.Category != "" {
		if _, exists := pr.categories[entry.Info.Category]; !exists {
			pr.categories[entry.Info.Category] = []string{}
		}

		// Add plugin to category if not already present
		found := false
		for _, pluginID := range pr.categories[entry.Info.Category] {
			if pluginID == entry.Info.ID {
				found = true
				break
			}
		}
		if !found {
			pr.categories[entry.Info.Category] = append(pr.categories[entry.Info.Category], entry.Info.ID)
		}
	}

	// Update publishers
	if entry.Info.Author != "" {
		if _, exists := pr.publishers[entry.Info.Author]; !exists {
			pr.publishers[entry.Info.Author] = []string{}
		}

		// Add plugin to publisher if not already present
		found := false
		for _, pluginID := range pr.publishers[entry.Info.Author] {
			if pluginID == entry.Info.ID {
				found = true
				break
			}
		}
		if !found {
			pr.publishers[entry.Info.Author] = append(pr.publishers[entry.Info.Author], entry.Info.ID)
		}
	}

	// Update versions
	versions := make([]string, 0, len(entry.Versions))
	for _, v := range entry.Versions {
		versions = append(versions, v.Version)
	}
	pr.versions[entry.Info.ID] = versions
}

func (pr *PluginRegistry) savePackageFiles(pluginID, version string, pkg plugins.PluginEnginePackage) error {
	// Save binary package
	packageDir := filepath.Join(pr.dataDir, "packages", pluginID)
	if err := os.MkdirAll(packageDir, 0755); err != nil {
		return fmt.Errorf("failed to create package directory: %w", err)
	}

	packagePath := filepath.Join(packageDir, fmt.Sprintf("%s.zip", version))
	if err := os.WriteFile(packagePath, pkg.Binary, 0644); err != nil {
		return fmt.Errorf("failed to write package file: %w", err)
	}

	// Save config if present
	if len(pkg.Config) > 0 {
		configDir := filepath.Join(pr.dataDir, "configs", pluginID)
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}

		configPath := filepath.Join(configDir, fmt.Sprintf("%s.json", version))
		if err := os.WriteFile(configPath, pkg.Config, 0644); err != nil {
			return fmt.Errorf("failed to write config file: %w", err)
		}
	}

	// Save docs if present
	if len(pkg.Docs) > 0 {
		docsDir := filepath.Join(pr.dataDir, "docs", pluginID)
		if err := os.MkdirAll(docsDir, 0755); err != nil {
			return fmt.Errorf("failed to create docs directory: %w", err)
		}

		docsPath := filepath.Join(docsDir, fmt.Sprintf("%s.md", version))
		if err := os.WriteFile(docsPath, pkg.Docs, 0644); err != nil {
			return fmt.Errorf("failed to write docs file: %w", err)
		}
	}

	// Save assets if present
	if len(pkg.Assets) > 0 {
		assetsDir := filepath.Join(pr.dataDir, "assets", pluginID, version)
		if err := os.MkdirAll(assetsDir, 0755); err != nil {
			return fmt.Errorf("failed to create assets directory: %w", err)
		}

		for filename, data := range pkg.Assets {
			assetPath := filepath.Join(assetsDir, filename)
			if err := os.WriteFile(assetPath, data, 0644); err != nil {
				return fmt.Errorf("failed to write asset file %s: %w", filename, err)
			}
		}
	}

	return nil
}
