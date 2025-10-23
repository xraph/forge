package formats

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	configcore "github.com/xraph/forge/v0/pkg/config/core"
	"github.com/xraph/forge/v0/pkg/logger"
)

// Loader handles loading configuration from various sources
type Loader struct {
	processors   map[string]FormatProcessor
	cache        map[string]*LoadResult
	cacheTTL     time.Duration
	retryCount   int
	retryDelay   time.Duration
	logger       common.Logger
	metrics      common.Metrics
	errorHandler common.ErrorHandler
	mu           sync.RWMutex
}

// LoaderConfig contains configuration for the loader
type LoaderConfig struct {
	CacheTTL     time.Duration
	RetryCount   int
	RetryDelay   time.Duration
	Logger       common.Logger
	Metrics      common.Metrics
	ErrorHandler common.ErrorHandler
}

// LoadResult represents the result of loading configuration
type LoadResult struct {
	Data     map[string]interface{}
	Source   string
	LoadTime time.Time
	Size     int
	Format   string
	Hash     string
	Error    error
	Cached   bool
}

// LoadOptions contains options for loading configuration
type LoadOptions struct {
	UseCache       bool
	ValidateFormat bool
	ExpandEnvVars  bool
	ExpandSecrets  bool
	MergeStrategy  MergeStrategy
	Transformers   []DataTransformer
}

// MergeStrategy defines how to merge configuration data
type MergeStrategy string

const (
	MergeStrategyOverride MergeStrategy = "override" // Override existing values
	MergeStrategyMerge    MergeStrategy = "merge"    // Deep merge objects
	MergeStrategyAppend   MergeStrategy = "append"   // Append to arrays
	MergeStrategyPreserve MergeStrategy = "preserve" // Preserve existing values
)

// DataTransformer transforms configuration data during loading
type DataTransformer interface {
	Name() string
	Transform(data map[string]interface{}) (map[string]interface{}, error)
	Priority() int
}

// FormatProcessor processes specific configuration formats
type FormatProcessor interface {
	Name() string
	Extensions() []string
	Parse(data []byte) (map[string]interface{}, error)
	Validate(data map[string]interface{}) error
}

// NewLoader creates a new configuration loader
func NewLoader(config LoaderConfig) *Loader {
	if config.CacheTTL == 0 {
		config.CacheTTL = 5 * time.Minute
	}
	if config.RetryCount == 0 {
		config.RetryCount = 3
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = time.Second
	}

	loader := &Loader{
		processors:   make(map[string]FormatProcessor),
		cache:        make(map[string]*LoadResult),
		cacheTTL:     config.CacheTTL,
		retryCount:   config.RetryCount,
		retryDelay:   config.RetryDelay,
		logger:       config.Logger,
		metrics:      config.Metrics,
		errorHandler: config.ErrorHandler,
	}

	// Register default format processors
	loader.RegisterProcessor(&YAMLProcessor{})
	loader.RegisterProcessor(&JSONProcessor{})
	loader.RegisterProcessor(&TOMLProcessor{})

	return loader
}

// LoadSource loads configuration from a source
func (l *Loader) LoadSource(ctx context.Context, source configcore.ConfigSource) (map[string]interface{}, error) {
	return l.LoadSourceWithOptions(ctx, source, LoadOptions{
		UseCache:       true,
		ValidateFormat: true,
		ExpandEnvVars:  true,
		ExpandSecrets:  false,
		MergeStrategy:  MergeStrategyMerge,
	})
}

// LoadSourceWithOptions loads configuration from a source with options
func (l *Loader) LoadSourceWithOptions(ctx context.Context, source configcore.ConfigSource, options LoadOptions) (map[string]interface{}, error) {
	sourceName := source.Name()

	// Check cache first if enabled
	if options.UseCache {
		if cached := l.getCachedResult(sourceName); cached != nil {
			if l.logger != nil {
				l.logger.Debug("using cached configuration",
					logger.String("source", sourceName),
					logger.Time("load_time", cached.LoadTime),
				)
			}
			if l.metrics != nil {
				l.metrics.Counter("forge.config.loader.cache_hits").Inc()
			}
			return cached.Data, nil
		}
	}

	// Load with retries
	var result *LoadResult
	var err error

	for attempt := 0; attempt <= l.retryCount; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(l.retryDelay * time.Duration(attempt)):
			}

			if l.logger != nil {
				l.logger.Warn("retrying configuration load",
					logger.String("source", sourceName),
					logger.Int("attempt", attempt),
					logger.Error(err),
				)
			}
		}

		result, err = l.loadSourceOnce(ctx, source, options)
		if err == nil {
			break
		}

		// Don't retry certain types of errors
		if l.isNonRetryableError(err) {
			break
		}
	}

	if err != nil {
		if l.errorHandler != nil {
			l.errorHandler.HandleError(nil, err)
		}
		return nil, common.ErrConfigError(fmt.Sprintf("failed to load source %s after %d attempts", sourceName, l.retryCount+1), err)
	}

	// Cache the result
	if options.UseCache {
		l.cacheResult(sourceName, result)
	}

	if l.metrics != nil {
		l.metrics.Counter("forge.config.loader.loads_success").Inc()
		l.metrics.Histogram("forge.config.loader.load_duration").Observe(time.Since(result.LoadTime).Seconds())
		l.metrics.Gauge("forge.config.loader.data_size", "source", sourceName).Set(float64(result.Size))
	}

	return result.Data, nil
}

// loadSourceOnce performs a single load attempt
func (l *Loader) loadSourceOnce(ctx context.Context, source configcore.ConfigSource, options LoadOptions) (*LoadResult, error) {
	startTime := time.Now()
	sourceName := source.Name()

	if l.logger != nil {
		l.logger.Debug("loading configuration source",
			logger.String("source", sourceName),
		)
	}

	// Load raw data from source
	rawData, err := source.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load from source: %w", err)
	}

	// Convert raw data to standardized format
	data, err := l.processRawData(rawData, options)
	if err != nil {
		return nil, fmt.Errorf("failed to process raw data: %w", err)
	}

	// Apply transformers
	for _, transformer := range options.Transformers {
		transformed, err := transformer.Transform(data)
		if err != nil {
			return nil, fmt.Errorf("transformer %s failed: %w", transformer.Name(), err)
		}
		data = transformed
	}

	// Expand environment variables if enabled
	if options.ExpandEnvVars {
		data = l.expandEnvironmentVariables(data)
	}

	// Expand secrets if enabled
	if options.ExpandSecrets && source.SupportsSecrets() {
		expandedData, err := l.expandSecrets(ctx, data, source)
		if err != nil {
			return nil, fmt.Errorf("failed to expand secrets: %w", err)
		}
		data = expandedData
	}

	result := &LoadResult{
		Data:     data,
		Source:   sourceName,
		LoadTime: startTime,
		Size:     l.calculateDataSize(data),
		Hash:     l.calculateDataHash(data),
	}

	if l.logger != nil {
		l.logger.Info("configuration source loaded successfully",
			logger.String("source", sourceName),
			logger.Int("keys", len(data)),
			logger.Int("size", result.Size),
			logger.Duration("duration", time.Since(startTime)),
		)
	}

	return result, nil
}

// processRawData processes raw configuration data
func (l *Loader) processRawData(rawData map[string]interface{}, options LoadOptions) (map[string]interface{}, error) {
	// For now, assume data is already in the correct format
	// In the future, this could detect format and apply appropriate processor
	return rawData, nil
}

// expandEnvironmentVariables expands environment variables in configuration values
func (l *Loader) expandEnvironmentVariables(data map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range data {
		result[key] = l.expandValue(value)
	}

	return result
}

// expandValue recursively expands environment variables in a value
func (l *Loader) expandValue(value interface{}) interface{} {
	switch v := value.(type) {
	case string:
		return expandEnvVars(v)
	case map[string]interface{}:
		return l.expandEnvironmentVariables(v)
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = l.expandValue(item)
		}
		return result
	default:
		return value
	}
}

// expandSecrets expands secret references in configuration
func (l *Loader) expandSecrets(ctx context.Context, data map[string]interface{}, source configcore.ConfigSource) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	for key, value := range data {
		expandedValue, err := l.expandSecretValue(ctx, value, source)
		if err != nil {
			return nil, fmt.Errorf("failed to expand secret for key %s: %w", key, err)
		}
		result[key] = expandedValue
	}

	return result, nil
}

// expandSecretValue recursively expands secret references in a value
func (l *Loader) expandSecretValue(ctx context.Context, value interface{}, source configcore.ConfigSource) (interface{}, error) {
	switch v := value.(type) {
	case string:
		if isSecretReference(v) {
			secretKey := extractSecretKey(v)
			return source.GetSecret(ctx, secretKey)
		}
		return v, nil
	case map[string]interface{}:
		return l.expandSecrets(ctx, v, source)
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			expandedItem, err := l.expandSecretValue(ctx, item, source)
			if err != nil {
				return nil, err
			}
			result[i] = expandedItem
		}
		return result, nil
	default:
		return value, nil
	}
}

// RegisterProcessor registers a format processor
func (l *Loader) RegisterProcessor(processor FormatProcessor) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.processors[processor.Name()] = processor

	if l.logger != nil {
		l.logger.Debug("format processor registered",
			logger.String("processor", processor.Name()),
			logger.String("extensions", fmt.Sprintf("%v", processor.Extensions())),
		)
	}
}

// GetProcessor returns a format processor by name
func (l *Loader) GetProcessor(name string) (FormatProcessor, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	processor, exists := l.processors[name]
	return processor, exists
}

// GetProcessorByExtension returns a format processor by file extension
func (l *Loader) GetProcessorByExtension(extension string) (FormatProcessor, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	for _, processor := range l.processors {
		for _, ext := range processor.Extensions() {
			if ext == extension {
				return processor, true
			}
		}
	}

	return nil, false
}

// ClearCache clears the loader cache
func (l *Loader) ClearCache() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.cache = make(map[string]*LoadResult)

	if l.logger != nil {
		l.logger.Debug("loader cache cleared")
	}

	if l.metrics != nil {
		l.metrics.Counter("forge.config.loader.cache_cleared").Inc()
	}
}

// GetCacheStats returns cache statistics
func (l *Loader) GetCacheStats() map[string]interface{} {
	l.mu.RLock()
	defer l.mu.RUnlock()

	var totalSize int
	var oldestEntry time.Time
	var newestEntry time.Time

	for _, result := range l.cache {
		totalSize += result.Size
		if oldestEntry.IsZero() || result.LoadTime.Before(oldestEntry) {
			oldestEntry = result.LoadTime
		}
		if newestEntry.IsZero() || result.LoadTime.After(newestEntry) {
			newestEntry = result.LoadTime
		}
	}

	return map[string]interface{}{
		"entries":      len(l.cache),
		"total_size":   totalSize,
		"oldest_entry": oldestEntry,
		"newest_entry": newestEntry,
		"cache_ttl":    l.cacheTTL,
	}
}

// getCachedResult returns a cached result if valid
func (l *Loader) getCachedResult(sourceName string) *LoadResult {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result, exists := l.cache[sourceName]
	if !exists {
		return nil
	}

	// Check if cache is still valid
	if time.Since(result.LoadTime) > l.cacheTTL {
		// Cache expired
		delete(l.cache, sourceName)
		return nil
	}

	// Mark as cached for metrics
	cachedResult := *result
	cachedResult.Cached = true
	return &cachedResult
}

// cacheResult caches a load result
func (l *Loader) cacheResult(sourceName string, result *LoadResult) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.cache[sourceName] = result

	if l.metrics != nil {
		l.metrics.Gauge("forge.config.loader.cache_entries").Set(float64(len(l.cache)))
	}
}

// isNonRetryableError checks if an error should not be retried
func (l *Loader) isNonRetryableError(err error) bool {
	// Add logic to identify non-retryable errors
	// For example: validation errors, syntax errors, etc.
	if forgeErr, ok := err.(*common.ForgeError); ok {
		switch forgeErr.Code {
		case common.ErrCodeValidationError, common.ErrCodeInvalidConfig:
			return true
		}
	}
	return false
}

// calculateDataSize calculates the approximate size of configuration data
func (l *Loader) calculateDataSize(data map[string]interface{}) int {
	// Simple approximation - in practice, you might want to use JSON marshaling
	// or a more sophisticated size calculation
	size := 0
	for key, value := range data {
		size += len(key)
		size += l.calculateValueSize(value)
	}
	return size
}

// calculateValueSize calculates the size of a configuration value
func (l *Loader) calculateValueSize(value interface{}) int {
	switch v := value.(type) {
	case string:
		return len(v)
	case map[string]interface{}:
		return l.calculateDataSize(v)
	case []interface{}:
		size := 0
		for _, item := range v {
			size += l.calculateValueSize(item)
		}
		return size
	default:
		return len(fmt.Sprintf("%v", value))
	}
}

// calculateDataHash calculates a hash of the configuration data
func (l *Loader) calculateDataHash(data map[string]interface{}) string {
	// Simple hash based on string representation
	// In practice, you might want to use a proper hash function
	return fmt.Sprintf("%x", fmt.Sprintf("%v", data))
}

// Helper functions for environment variable and secret expansion

// expandEnvVars expands environment variables in a string
func expandEnvVars(s string) string {
	// Simple implementation - in practice, you might want to use
	// os.ExpandEnv or a more sophisticated expansion
	return s
}

// isSecretReference checks if a string is a secret reference
func isSecretReference(s string) bool {
	// Check for secret reference patterns like ${secret:key} or $SECRET_KEY
	return strings.HasPrefix(s, "${secret:") && strings.HasSuffix(s, "}")
}

// extractSecretKey extracts the secret key from a reference
func extractSecretKey(s string) string {
	// Extract key from ${secret:key} format
	if strings.HasPrefix(s, "${secret:") && strings.HasSuffix(s, "}") {
		return s[9 : len(s)-1] // Remove "${secret:" and "}"
	}
	return s
}

// Basic transformer implementations

// EnvVarTransformer transforms environment variable references
type EnvVarTransformer struct{}

func (t *EnvVarTransformer) Name() string  { return "env-vars" }
func (t *EnvVarTransformer) Priority() int { return 10 }

func (t *EnvVarTransformer) Transform(data map[string]interface{}) (map[string]interface{}, error) {
	// Implementation would expand environment variables
	return data, nil
}

// SecretsTransformer transforms secret references
type SecretsTransformer struct {
	secretsManager configcore.SecretsManager
}

func (t *SecretsTransformer) Name() string  { return "secrets" }
func (t *SecretsTransformer) Priority() int { return 20 }

func (t *SecretsTransformer) Transform(data map[string]interface{}) (map[string]interface{}, error) {
	// Implementation would expand secret references
	return data, nil
}
