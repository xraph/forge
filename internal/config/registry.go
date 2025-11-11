package config

import (
	"fmt"
	"maps"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/internal/config/sources"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

// SourceRegistryImpl implements the SourceRegistry interface.
type SourceRegistryImpl struct {
	sources  map[string]ConfigSource
	metadata map[string]*SourceMetadata
	logger   logger.Logger
	metrics  shared.Metrics
	mu       sync.RWMutex
}

// NewSourceRegistry creates a new source registry.
func NewSourceRegistry(logger logger.Logger) SourceRegistry {
	return &SourceRegistryImpl{
		sources:  make(map[string]ConfigSource),
		metadata: make(map[string]*SourceMetadata),
		logger:   logger,
	}
}

// RegisterSource registers a configuration source.
func (sr *SourceRegistryImpl) RegisterSource(source ConfigSource) error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	if source == nil {
		return ErrValidationError("source", errors.New("source cannot be nil"))
	}

	sourceName := source.Name()
	if sourceName == "" {
		return ErrValidationError("source_name", errors.New("source name cannot be empty"))
	}

	// Check if source already exists
	if _, exists := sr.sources[sourceName]; exists {
		return fmt.Errorf("source already exists: %s", sourceName)
	}

	// Register the source
	sr.sources[sourceName] = source

	// Create metadata
	metadata := &SourceMetadata{
		Name:         sourceName,
		Priority:     source.Priority(),
		Type:         source.GetType(),
		LastLoaded:   time.Time{}, // Will be updated on first load
		LastModified: time.Time{}, // Will be updated when source changes
		IsWatching:   false,       // Will be updated when watching starts
		KeyCount:     0,           // Will be updated on load
		ErrorCount:   0,
		Properties:   make(map[string]any),
	}

	sr.metadata[sourceName] = metadata

	if sr.logger != nil {
		sr.logger.Info("configuration source registered",
			logger.String("source", sourceName),
			logger.String("type", metadata.Type),
			logger.Int("priority", source.Priority()),
			logger.Bool("watchable", source.IsWatchable()),
			logger.Bool("supports_secrets", source.SupportsSecrets()),
		)
	}

	if sr.metrics != nil {
		sr.metrics.Counter("config.sources_registered").Inc()
		sr.metrics.Gauge("config.sources_count").Set(float64(len(sr.sources)))
	}

	return nil
}

// UnregisterSource unregisters a configuration source.
func (sr *SourceRegistryImpl) UnregisterSource(name string) error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	source, exists := sr.sources[name]
	if !exists {
		return fmt.Errorf("source not found: %s", name)
	}

	// Stop watching if the source is being watched
	if source.IsWatchable() {
		if err := source.StopWatch(); err != nil {
			if sr.logger != nil {
				sr.logger.Warn("failed to stop watching source during unregistration",
					logger.String("source", name),
					logger.Error(err),
				)
			}
		}
	}

	// Remove from registry
	delete(sr.sources, name)
	delete(sr.metadata, name)

	if sr.logger != nil {
		sr.logger.Info("configuration source unregistered",
			logger.String("source", name),
		)
	}

	if sr.metrics != nil {
		sr.metrics.Counter("config.sources_unregistered").Inc()
		sr.metrics.Gauge("config.sources_count").Set(float64(len(sr.sources)))
	}

	return nil
}

// GetSource retrieves a configuration source by name.
func (sr *SourceRegistryImpl) GetSource(name string) (ConfigSource, error) {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	source, exists := sr.sources[name]
	if !exists {
		return nil, fmt.Errorf("source not found: %s", name)
	}

	return source, nil
}

// GetSources returns all registered sources ordered by priority.
func (sr *SourceRegistryImpl) GetSources() []ConfigSource {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	// Collect sources with their priorities
	type sourceWithPriority struct {
		source   ConfigSource
		priority int
	}

	var sourcesWithPriority []sourceWithPriority
	for _, source := range sr.sources {
		sourcesWithPriority = append(sourcesWithPriority, sourceWithPriority{
			source:   source,
			priority: source.Priority(),
		})
	}

	// Sort by priority (higher priority first), then by name for stability
	sort.Slice(sourcesWithPriority, func(i, j int) bool {
		if sourcesWithPriority[i].priority != sourcesWithPriority[j].priority {
			return sourcesWithPriority[i].priority > sourcesWithPriority[j].priority
		}

		return sourcesWithPriority[i].source.Name() < sourcesWithPriority[j].source.Name()
	})

	// Extract sorted sources
	result := make([]ConfigSource, len(sourcesWithPriority))
	for i, swp := range sourcesWithPriority {
		result[i] = swp.source
	}

	return result
}

// GetSourceMetadata returns metadata for a source.
func (sr *SourceRegistryImpl) GetSourceMetadata(name string) (*SourceMetadata, error) {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	metadata, exists := sr.metadata[name]
	if !exists {
		return nil, fmt.Errorf("source not found: %s", name)
	}

	// Return a copy to prevent external modification
	metadataCopy := *metadata

	metadataCopy.Properties = make(map[string]any)
	maps.Copy(metadataCopy.Properties, metadata.Properties)

	return &metadataCopy, nil
}

// GetAllMetadata returns metadata for all sources.
func (sr *SourceRegistryImpl) GetAllMetadata() map[string]*SourceMetadata {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	result := make(map[string]*SourceMetadata)

	for name, metadata := range sr.metadata {
		// Return copies to prevent external modification
		metadataCopy := *metadata

		metadataCopy.Properties = make(map[string]any)
		maps.Copy(metadataCopy.Properties, metadata.Properties)

		result[name] = &metadataCopy
	}

	return result
}

// UpdateSourceMetadata updates metadata for a source (internal use).
func (sr *SourceRegistryImpl) UpdateSourceMetadata(name string, updater func(*SourceMetadata)) error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	metadata, exists := sr.metadata[name]
	if !exists {
		return fmt.Errorf("source not found: %s", name)
	}

	updater(metadata)

	return nil
}

// UpdateLoadStats updates load statistics for a source.
func (sr *SourceRegistryImpl) UpdateLoadStats(name string, keyCount int, err error) error {
	return sr.UpdateSourceMetadata(name, func(metadata *SourceMetadata) {
		metadata.LastLoaded = time.Now()

		metadata.KeyCount = keyCount
		if err != nil {
			metadata.ErrorCount++
			metadata.LastError = err.Error()
		} else {
			metadata.LastError = ""
		}
	})
}

// UpdateWatchStatus updates watch status for a source.
func (sr *SourceRegistryImpl) UpdateWatchStatus(name string, isWatching bool) error {
	return sr.UpdateSourceMetadata(name, func(metadata *SourceMetadata) {
		metadata.IsWatching = isWatching
	})
}

// UpdateModifiedTime updates the last modified time for a source.
func (sr *SourceRegistryImpl) UpdateModifiedTime(name string, modifiedTime time.Time) error {
	return sr.UpdateSourceMetadata(name, func(metadata *SourceMetadata) {
		metadata.LastModified = modifiedTime
	})
}

// GetSourcesByType returns sources of a specific type.
func (sr *SourceRegistryImpl) GetSourcesByType(sourceType string) []ConfigSource {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	var result []ConfigSource

	for _, source := range sr.sources {
		if sr.getSourceType(source) == sourceType {
			result = append(result, source)
		}
	}

	return result
}

// GetWatchableSources returns all sources that support watching.
func (sr *SourceRegistryImpl) GetWatchableSources() []ConfigSource {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	var result []ConfigSource

	for _, source := range sr.sources {
		if source.IsWatchable() {
			result = append(result, source)
		}
	}

	return result
}

// GetSourcesWithSecrets returns all sources that support secrets.
func (sr *SourceRegistryImpl) GetSourcesWithSecrets() []ConfigSource {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	var result []ConfigSource

	for _, source := range sr.sources {
		if source.SupportsSecrets() {
			result = append(result, source)
		}
	}

	return result
}

// ValidateSourceDependencies validates that all source dependencies are met.
func (sr *SourceRegistryImpl) ValidateSourceDependencies() error {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	// For now, we don't have source dependencies
	// This could be extended in the future to support complex dependency graphs
	return nil
}

// GetSourceStats returns statistics about the registry.
func (sr *SourceRegistryImpl) GetSourceStats() map[string]any {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	stats := map[string]any{
		"total_sources":     len(sr.sources),
		"watchable_sources": 0,
		"secret_sources":    0,
		"types":             make(map[string]int),
		"priorities":        make(map[int]int),
	}

	watchableCount := 0
	secretCount := 0
	types := make(map[string]int)
	priorities := make(map[int]int)

	for _, source := range sr.sources {
		if source.IsWatchable() {
			watchableCount++
		}

		if source.SupportsSecrets() {
			secretCount++
		}

		sourceType := sr.getSourceType(source)
		types[sourceType]++

		priority := source.Priority()
		priorities[priority]++
	}

	stats["watchable_sources"] = watchableCount
	stats["secret_sources"] = secretCount
	stats["types"] = types
	stats["priorities"] = priorities

	return stats
}

// getSourceType determines the type of a configuration source.
func (sr *SourceRegistryImpl) getSourceType(source ConfigSource) string {
	// Use GetType() method first
	if sourceType := source.GetType(); sourceType != "" && sourceType != "unknown" {
		return sourceType
	}

	// Fall back to type assertion for known types
	switch source.(type) {
	case *sources.FileSource:
		return "file"
	case *sources.EnvSource:
		return "environment"
	case *sources.ConsulSource:
		return "consul"
	case *sources.K8sSource:
		return "kubernetes"
	default:
		return "unknown"
	}
}

// Clear removes all registered sources.
func (sr *SourceRegistryImpl) Clear() error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	// Stop watching for all sources
	for name, source := range sr.sources {
		if source.IsWatchable() {
			if err := source.StopWatch(); err != nil {
				if sr.logger != nil {
					sr.logger.Warn("failed to stop watching source during clear",
						logger.String("source", name),
						logger.Error(err),
					)
				}
			}
		}
	}

	// Clear all data
	sr.sources = make(map[string]ConfigSource)
	sr.metadata = make(map[string]*SourceMetadata)

	if sr.logger != nil {
		sr.logger.Info("all configuration sources cleared")
	}

	if sr.metrics != nil {
		sr.metrics.Counter("config.sources_cleared").Inc()
		sr.metrics.Gauge("config.sources_count").Set(0)
	}

	return nil
}

// IsEmpty returns true if no sources are registered.
func (sr *SourceRegistryImpl) IsEmpty() bool {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	return len(sr.sources) == 0
}

// GetSourceNames returns the names of all registered sources.
func (sr *SourceRegistryImpl) GetSourceNames() []string {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	names := make([]string, 0, len(sr.sources))
	for name := range sr.sources {
		names = append(names, name)
	}

	return names
}

// HasSource returns true if a source with the given name is registered.
func (sr *SourceRegistryImpl) HasSource(name string) bool {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	_, exists := sr.sources[name]

	return exists
}
