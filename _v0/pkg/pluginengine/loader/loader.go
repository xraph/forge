package loader

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	plugins "github.com/xraph/forge/v0/pkg/pluginengine/common"
)

// PluginLoader defines the interface for loading plugins
type PluginLoader interface {
	LoadPlugin(ctx context.Context, source plugins.PluginEngineSource) (plugins.PluginEngine, error)
	UnloadPlugin(ctx context.Context, pluginID string) error
	GetLoader(loaderType LoaderType) (Loader, error)
	RegisterLoader(loaderType LoaderType, loader Loader) error
	GetSupportedTypes() []LoaderType
	ValidateSource(source plugins.PluginEngineSource) error
}

// LoaderType defines the type of plugin loader
type LoaderType string

const (
	LoaderTypeBinary LoaderType = "binary"
	LoaderTypeScript LoaderType = "script"
	LoaderTypeWASM   LoaderType = "wasm"
	LoaderTypeNative LoaderType = "native"
)

// Loader defines the interface for specific plugin loaders
type Loader interface {
	Name() string
	Type() LoaderType
	SupportedExtensions() []string
	LoadPlugin(ctx context.Context, source plugins.PluginEngineSource) (plugins.PluginEngine, error)
	ValidateSource(source plugins.PluginEngineSource) error
	GetMetadata(source plugins.PluginEngineSource) (*plugins.PluginEngineInfo, error)
}

// PluginLoaderImpl implements the PluginLoader interface
type PluginLoaderImpl struct {
	loaders map[LoaderType]Loader
	logger  common.Logger
	metrics common.Metrics
	mu      sync.RWMutex
}

// NewPluginLoader creates a new plugin loader
func NewPluginLoader(logger common.Logger, metrics common.Metrics) PluginLoader {
	pl := &PluginLoaderImpl{
		loaders: make(map[LoaderType]Loader),
		logger:  logger,
		metrics: metrics,
	}

	// Register default loaders
	pl.registerDefaultLoaders()

	return pl
}

// registerDefaultLoaders registers the default plugin loaders
func (pl *PluginLoaderImpl) registerDefaultLoaders() {
	// Register binary loader
	binaryLoader := NewBinaryLoader(pl.logger, pl.metrics)
	pl.loaders[LoaderTypeBinary] = binaryLoader

	// Register script loader
	scriptLoader := NewScriptLoader(pl.logger, pl.metrics)
	pl.loaders[LoaderTypeScript] = scriptLoader

	// Register WASM loader
	wasmLoader := NewWASMLoader(pl.logger, pl.metrics)
	pl.loaders[LoaderTypeWASM] = wasmLoader

	// Register native Go loader
	nativeLoader := NewNativeLoader(pl.logger, pl.metrics)
	pl.loaders[LoaderTypeNative] = nativeLoader
}

// LoadPlugin loads a plugin from the given source
func (pl *PluginLoaderImpl) LoadPlugin(ctx context.Context, source plugins.PluginEngineSource) (plugins.PluginEngine, error) {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	// Validate source
	if err := pl.ValidateSource(source); err != nil {
		return nil, fmt.Errorf("source validation failed: %w", err)
	}

	// Determine loader type based on source
	loaderType, err := pl.determineLoaderType(source)
	if err != nil {
		return nil, fmt.Errorf("failed to determine loader type: %w", err)
	}

	// Get appropriate loader
	loader, exists := pl.loaders[loaderType]
	if !exists {
		return nil, fmt.Errorf("no loader available for type: %s", loaderType)
	}

	// Load plugin
	plugin, err := loader.LoadPlugin(ctx, source)
	if err != nil {
		if pl.metrics != nil {
			pl.metrics.Counter("forge.plugins.load_failed", "type", string(loaderType)).Inc()
		}
		return nil, fmt.Errorf("failed to load plugin with %s loader: %w", loaderType, err)
	}

	if pl.logger != nil {
		pl.logger.Info("plugin loaded successfully",
			logger.String("plugin_id", plugin.ID()),
			logger.String("loader_type", string(loaderType)),
			logger.String("source_location", source.Location),
		)
	}

	if pl.metrics != nil {
		pl.metrics.Counter("forge.plugins.loaded", "type", string(loaderType)).Inc()
	}

	return plugin, nil
}

// UnloadPlugin unloads a plugin
func (pl *PluginLoaderImpl) UnloadPlugin(ctx context.Context, pluginID string) error {
	// Plugin unloading is typically handled by the plugin manager
	// This method can be used for cleanup tasks specific to loaders

	if pl.logger != nil {
		pl.logger.Info("plugin unloaded", logger.String("plugin_id", pluginID))
	}

	if pl.metrics != nil {
		pl.metrics.Counter("forge.plugins.unloaded").Inc()
	}

	return nil
}

// GetLoader returns a specific loader by type
func (pl *PluginLoaderImpl) GetLoader(loaderType LoaderType) (Loader, error) {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	loader, exists := pl.loaders[loaderType]
	if !exists {
		return nil, fmt.Errorf("loader not found: %s", loaderType)
	}

	return loader, nil
}

// RegisterLoader registers a new plugin loader
func (pl *PluginLoaderImpl) RegisterLoader(loaderType LoaderType, loader Loader) error {
	pl.mu.Lock()
	defer pl.mu.Unlock()

	if _, exists := pl.loaders[loaderType]; exists {
		return fmt.Errorf("loader already registered: %s", loaderType)
	}

	pl.loaders[loaderType] = loader

	if pl.logger != nil {
		pl.logger.Info("plugin loader registered",
			logger.String("loader_type", string(loaderType)),
			logger.String("loader_name", loader.Name()),
		)
	}

	return nil
}

// GetSupportedTypes returns all supported loader types
func (pl *PluginLoaderImpl) GetSupportedTypes() []LoaderType {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	types := make([]LoaderType, 0, len(pl.loaders))
	for loaderType := range pl.loaders {
		types = append(types, loaderType)
	}

	return types
}

// ValidateSource validates a plugin source
func (pl *PluginLoaderImpl) ValidateSource(source plugins.PluginEngineSource) error {
	if source.Location == "" {
		return fmt.Errorf("plugin source location is required")
	}

	// Determine loader type and validate with specific loader
	loaderType, err := pl.determineLoaderType(source)
	if err != nil {
		return err
	}

	loader, exists := pl.loaders[loaderType]
	if !exists {
		return fmt.Errorf("no loader available for type: %s", loaderType)
	}

	return loader.ValidateSource(source)
}

// determineLoaderType determines the appropriate loader type for a source
func (pl *PluginLoaderImpl) determineLoaderType(source plugins.PluginEngineSource) (LoaderType, error) {
	location := strings.ToLower(source.Location)
	ext := filepath.Ext(location)

	// Check each loader's supported extensions
	for loaderType, loader := range pl.loaders {
		for _, supportedExt := range loader.SupportedExtensions() {
			if ext == supportedExt {
				return loaderType, nil
			}
		}
	}

	// Special cases based on source type
	switch source.Type {
	case plugins.PluginEngineSourceTypeFile:
		// Default to binary for file sources
		return LoaderTypeBinary, nil
	case plugins.PluginEngineSourceTypeURL:
		// Try to determine from URL extension
		if strings.Contains(location, ".wasm") {
			return LoaderTypeWASM, nil
		}
		if strings.Contains(location, ".so") || strings.Contains(location, ".dll") || strings.Contains(location, ".dylib") {
			return LoaderTypeNative, nil
		}
		return LoaderTypeBinary, nil
	case plugins.PluginEngineSourceTypeMarketplace, plugins.PluginEngineSourceTypeRegistry:
		// Default to binary for marketplace/registry sources
		return LoaderTypeBinary, nil
	default:
		return LoaderTypeBinary, nil
	}
}

// LoaderStats contains statistics about plugin loading
type LoaderStats struct {
	LoaderType      LoaderType `json:"loader_type"`
	LoaderName      string     `json:"loader_name"`
	PluginsLoaded   int64      `json:"plugins_loaded"`
	LoadFailures    int64      `json:"load_failures"`
	LoadSuccessRate float64    `json:"load_success_rate"`
	AverageLoadTime float64    `json:"average_load_time"`
}

// GetStats returns loader statistics
func (pl *PluginLoaderImpl) GetStats() map[LoaderType]LoaderStats {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	stats := make(map[LoaderType]LoaderStats)
	for loaderType, loader := range pl.loaders {
		// In a real implementation, these stats would be tracked
		stats[loaderType] = LoaderStats{
			LoaderType:      loaderType,
			LoaderName:      loader.Name(),
			PluginsLoaded:   0, // Would be tracked from metrics
			LoadFailures:    0, // Would be tracked from metrics
			LoadSuccessRate: 0, // Calculated from above
			AverageLoadTime: 0, // Would be tracked from metrics
		}
	}

	return stats
}
