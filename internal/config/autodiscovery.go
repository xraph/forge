package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xraph/forge/internal/config/sources"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

// F is a helper function to create logger fields
func F(key string, value interface{}) logger.Field {
	return logger.Any(key, value)
}

// AutoDiscoveryConfig configures automatic config file discovery
type AutoDiscoveryConfig struct {
	// AppName is the application name to look for in app-scoped configs
	// If provided, will look for "apps.{AppName}" section in config
	AppName string

	// SearchPaths are directories to search for config files
	// Defaults to current directory and parent directories
	SearchPaths []string

	// ConfigNames are the config file names to search for
	// Defaults to ["config.yaml", "config.yml"]
	ConfigNames []string

	// LocalConfigNames are the local override config file names
	// Defaults to ["config.local.yaml", "config.local.yml"]
	LocalConfigNames []string

	// MaxDepth is the maximum number of parent directories to search
	// Defaults to 5
	MaxDepth int

	// RequireBase determines if base config file is required
	// Defaults to false
	RequireBase bool

	// RequireLocal determines if local config file is required
	// Defaults to false
	RequireLocal bool

	// EnableAppScoping enables app-scoped config extraction
	// If true and AppName is set, will extract "apps.{AppName}" section
	// Defaults to true
	EnableAppScoping bool

	// Logger for discovery operations
	Logger logger.Logger

	// ErrorHandler for error handling
	ErrorHandler shared.ErrorHandler
}

// AutoDiscoveryResult contains the result of config discovery
type AutoDiscoveryResult struct {
	// BaseConfigPath is the path to the base config file
	BaseConfigPath string

	// LocalConfigPath is the path to the local config file
	LocalConfigPath string

	// WorkingDirectory is the directory where configs were found
	WorkingDirectory string

	// IsMonorepo indicates if this is a monorepo layout
	IsMonorepo bool

	// AppName is the app name for app-scoped configs
	AppName string
}

// DefaultAutoDiscoveryConfig returns default auto-discovery configuration
func DefaultAutoDiscoveryConfig() AutoDiscoveryConfig {
	return AutoDiscoveryConfig{
		ConfigNames:      []string{"config.yaml", "config.yml"},
		LocalConfigNames: []string{"config.local.yaml", "config.local.yml"},
		MaxDepth:         5,
		RequireBase:      false,
		RequireLocal:     false,
		EnableAppScoping: true,
	}
}

// DiscoverAndLoadConfigs automatically discovers and loads config files
func DiscoverAndLoadConfigs(cfg AutoDiscoveryConfig) (ConfigManager, *AutoDiscoveryResult, error) {
	// Apply defaults
	if len(cfg.ConfigNames) == 0 {
		cfg.ConfigNames = []string{"config.yaml", "config.yml"}
	}
	if len(cfg.LocalConfigNames) == 0 {
		cfg.LocalConfigNames = []string{"config.local.yaml", "config.local.yml"}
	}
	if cfg.MaxDepth == 0 {
		cfg.MaxDepth = 5
	}
	if len(cfg.SearchPaths) == 0 {
		// Default to current directory
		cwd, err := os.Getwd()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get current directory: %w", err)
		}
		cfg.SearchPaths = []string{cwd}
	}

	// Discover config files
	result, err := discoverConfigFiles(cfg)
	if err != nil {
		return nil, nil, err
	}

	// Create config manager
	manager := NewManager(ManagerConfig{
		Logger:       cfg.Logger,
		ErrorHandler: cfg.ErrorHandler,
	})

	// Load base config if found
	if result.BaseConfigPath != "" {
		source, err := sources.NewFileSource(result.BaseConfigPath, sources.FileSourceOptions{
			Name:          "config.base",
			Priority:      100,
			WatchEnabled:  true,
			ExpandEnvVars: true,
			RequireFile:   cfg.RequireBase,
			Logger:        cfg.Logger,
			ErrorHandler:  cfg.ErrorHandler,
		})
		if err != nil {
			if cfg.RequireBase {
				return nil, nil, fmt.Errorf("failed to create base config source: %w", err)
			}
		} else {
			if err := manager.LoadFrom(source); err != nil {
				if cfg.RequireBase {
					return nil, nil, fmt.Errorf("failed to load base config: %w", err)
				}
			}
		}
	} else if cfg.RequireBase {
		return nil, nil, fmt.Errorf("base config file required but not found")
	}

	// Load local config if found (higher priority - overrides base)
	if result.LocalConfigPath != "" {
		source, err := sources.NewFileSource(result.LocalConfigPath, sources.FileSourceOptions{
			Name:          "config.local",
			Priority:      200, // Higher priority than base
			WatchEnabled:  true,
			ExpandEnvVars: true,
			RequireFile:   cfg.RequireLocal,
			Logger:        cfg.Logger,
			ErrorHandler:  cfg.ErrorHandler,
		})
		if err != nil {
			if cfg.RequireLocal {
				return nil, nil, fmt.Errorf("failed to create local config source: %w", err)
			}
		} else {
			if err := manager.LoadFrom(source); err != nil {
				if cfg.RequireLocal {
					return nil, nil, fmt.Errorf("failed to load local config: %w", err)
				}
			}
		}
	} else if cfg.RequireLocal {
		return nil, nil, fmt.Errorf("local config file required but not found")
	}

	// Extract app-scoped config if enabled and AppName is provided
	// We need to do this BEFORE merging sources to maintain proper priority
	if cfg.EnableAppScoping && cfg.AppName != "" {
		// Get the source data before merging
		if mgr, ok := manager.(*Manager); ok {
			if err := extractAppScopedWithPriority(mgr, cfg.AppName); err != nil {
				if cfg.Logger != nil {
					cfg.Logger.Debug("app-scoped config not found, using global config",
						F("app", cfg.AppName),
					)
				}
			}
		}
	}

	return manager, result, nil
}

// discoverConfigFiles searches for config files in the specified paths
func discoverConfigFiles(cfg AutoDiscoveryConfig) (*AutoDiscoveryResult, error) {
	result := &AutoDiscoveryResult{
		AppName: cfg.AppName,
	}

	// Search in each path
	for _, searchPath := range cfg.SearchPaths {
		// Clean and normalize path
		searchPath = filepath.Clean(searchPath)

		// Try to find configs in this path and parent directories
		found, err := searchInPathHierarchy(searchPath, cfg, result)
		if err != nil {
			continue
		}
		if found {
			return result, nil
		}
	}

	// If we didn't find anything and it's not required, return empty result
	if !cfg.RequireBase && !cfg.RequireLocal {
		return result, nil
	}

	return nil, fmt.Errorf("config files not found in search paths")
}

// searchInPathHierarchy searches for config files in a path and its parents
func searchInPathHierarchy(startPath string, cfg AutoDiscoveryConfig, result *AutoDiscoveryResult) (bool, error) {
	currentPath := startPath
	depth := 0

	for {
		// Check if we've exceeded max depth
		if depth >= cfg.MaxDepth {
			break
		}

		// Look for base config files
		for _, configName := range cfg.ConfigNames {
			configPath := filepath.Join(currentPath, configName)
			if fileExists(configPath) {
				result.BaseConfigPath = configPath
				result.WorkingDirectory = currentPath
				break
			}
		}

		// Look for local config files
		for _, localName := range cfg.LocalConfigNames {
			localPath := filepath.Join(currentPath, localName)
			if fileExists(localPath) {
				result.LocalConfigPath = localPath
				if result.WorkingDirectory == "" {
					result.WorkingDirectory = currentPath
				}
			}
		}

		// Check if this looks like a monorepo (has apps/ directory)
		appsDir := filepath.Join(currentPath, "apps")
		if dirExists(appsDir) {
			result.IsMonorepo = true
		}

		// If we found at least one config, we're done
		if result.BaseConfigPath != "" || result.LocalConfigPath != "" {
			return true, nil
		}

		// Move to parent directory
		parentPath := filepath.Dir(currentPath)
		if parentPath == currentPath {
			// Reached root
			break
		}
		currentPath = parentPath
		depth++
	}

	return false, nil
}

// extractAppScopedConfig extracts app-scoped configuration from the manager
// Looks for config under "apps.{appName}" and promotes it to root level
func extractAppScopedConfig(manager ConfigManager, appName string) error {
	// Try to get app-scoped config
	appConfigKey := fmt.Sprintf("apps.%s", appName)
	appConfig := manager.GetSection(appConfigKey)
	
	if appConfig == nil || len(appConfig) == 0 {
		return fmt.Errorf("app-scoped config not found for app: %s", appName)
	}

	// Get all current settings
	allSettings := manager.GetAllSettings()

	// Merge app config with global config using deep merge
	// Global settings are base, app-specific settings override
	mergedConfig := make(map[string]interface{})
	
	// Start with global settings (excluding apps section)
	for key, value := range allSettings {
		if key != "apps" {
			mergedConfig[key] = deepCopyValue(value)
		}
	}

	// Deep merge app-specific settings over global
	deepMergeMapRecursive(mergedConfig, appConfig)

	// Clear and reload with merged config
	if mgr, ok := manager.(*Manager); ok {
		mgr.mu.Lock()
		mgr.data = mergedConfig
		mgr.mu.Unlock()
	}

	return nil
}

// extractAppScopedWithPriority extracts app-scoped config respecting source priorities
// This ensures that local global overrides take precedence over base app-scoped settings
func extractAppScopedWithPriority(mgr *Manager, appName string) error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	
	// Build a priority-aware merged config
	// We need to extract app-scoped from each source separately, then merge by priority
	
	type sourceData struct {
		priority int
		data     map[string]interface{}
		appData  map[string]interface{}
	}
	
	sources := mgr.registry.GetSources()
	sourceDataList := make([]sourceData, 0, len(sources))
	
	// Load each source and extract its app-scoped section
	for _, source := range sources {
		data, err := mgr.loader.LoadSource(context.Background(), source)
		if err != nil {
			continue
		}
		
		// Extract app-scoped section if it exists
		var appData map[string]interface{}
		if appsSection, ok := data["apps"].(map[string]interface{}); ok {
			if appSection, ok := appsSection[appName].(map[string]interface{}); ok {
				appData = appSection
			}
		}
		
		// Remove apps section from global data
		globalData := make(map[string]interface{})
		for k, v := range data {
			if k != "apps" {
				globalData[k] = deepCopyValue(v)
			}
		}
		
		sourceDataList = append(sourceDataList, sourceData{
			priority: source.Priority(),
			data:     globalData,
			appData:  appData,
		})
	}
	
	// Sort by priority
	for i := 0; i < len(sourceDataList); i++ {
		for j := i + 1; j < len(sourceDataList); j++ {
			if sourceDataList[i].priority > sourceDataList[j].priority {
				sourceDataList[i], sourceDataList[j] = sourceDataList[j], sourceDataList[i]
			}
		}
	}
	
	// Merge in priority order:
	// 1. Start with lowest priority global
	// 2. Merge same-priority app-scoped over global
	// 3. Merge next priority global
	// 4. Merge next priority app-scoped
	// etc.
	
	mergedConfig := make(map[string]interface{})
	
	for _, sd := range sourceDataList {
		// First merge global from this source
		deepMergeMapRecursive(mergedConfig, sd.data)
		
		// Then merge app-scoped from this source (app overrides global at same priority)
		if sd.appData != nil {
			deepMergeMapRecursive(mergedConfig, sd.appData)
		}
	}
	
	mgr.data = mergedConfig
	return nil
}

// deepMergeMapRecursive merges source into target recursively
// Values from source override values in target
func deepMergeMapRecursive(target, source map[string]interface{}) {
	for key, sourceValue := range source {
		if targetValue, exists := target[key]; exists {
			// Both are maps - merge recursively
			if targetMap, ok := targetValue.(map[string]interface{}); ok {
				if sourceMap, ok := sourceValue.(map[string]interface{}); ok {
					deepMergeMapRecursive(targetMap, sourceMap)
					continue
				}
			}
		}
		// For non-map values or new keys, source overrides
		target[key] = deepCopyValue(sourceValue)
	}
}

// deepCopyValue creates a deep copy of a value
func deepCopyValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		copied := make(map[string]interface{})
		for k, val := range v {
			copied[k] = deepCopyValue(val)
		}
		return copied
	case []interface{}:
		copied := make([]interface{}, len(v))
		for i, val := range v {
			copied[i] = deepCopyValue(val)
		}
		return copied
	default:
		return v
	}
}

// AutoLoadConfigManager automatically discovers and loads config files
// This is a convenience function that uses default settings
func AutoLoadConfigManager(appName string, logger logger.Logger) (ConfigManager, error) {
	cfg := DefaultAutoDiscoveryConfig()
	cfg.AppName = appName
	cfg.Logger = logger

	manager, _, err := DiscoverAndLoadConfigs(cfg)
	return manager, err
}

// LoadConfigWithAppScope loads config with app-scoped extraction
// This is the recommended way to load configs in a monorepo environment
func LoadConfigWithAppScope(appName string, logger logger.Logger, errorHandler shared.ErrorHandler) (ConfigManager, error) {
	cfg := DefaultAutoDiscoveryConfig()
	cfg.AppName = appName
	cfg.EnableAppScoping = true
	cfg.Logger = logger
	cfg.ErrorHandler = errorHandler

	manager, result, err := DiscoverAndLoadConfigs(cfg)
	if err != nil {
		return nil, err
	}

	// Log discovery results
	if logger != nil {
		if result.BaseConfigPath != "" {
			logger.Info("discovered base config",
				F("path", result.BaseConfigPath),
			)
		}
		if result.LocalConfigPath != "" {
			logger.Info("discovered local config",
				F("path", result.LocalConfigPath),
			)
		}
		if result.IsMonorepo {
			logger.Info("detected monorepo layout",
				F("app", appName),
			)
		}
	}

	return manager, nil
}

// Helper functions

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// LoadConfigFromPaths is a helper that loads config from explicit paths
// Useful when you know exactly where your config files are
func LoadConfigFromPaths(basePath, localPath, appName string, logger logger.Logger) (ConfigManager, error) {
	manager := NewManager(ManagerConfig{
		Logger: logger,
	})

	// Load base config if provided
	if basePath != "" && fileExists(basePath) {
		source, err := sources.NewFileSource(basePath, sources.FileSourceOptions{
			Name:          "config.base",
			Priority:      100,
			WatchEnabled:  true,
			ExpandEnvVars: true,
			Logger:        logger,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create base config source: %w", err)
		}
		if err := manager.LoadFrom(source); err != nil {
			return nil, fmt.Errorf("failed to load base config: %w", err)
		}
	}

	// Load local config if provided (overrides base)
	if localPath != "" && fileExists(localPath) {
		source, err := sources.NewFileSource(localPath, sources.FileSourceOptions{
			Name:          "config.local",
			Priority:      200,
			WatchEnabled:  true,
			ExpandEnvVars: true,
			Logger:        logger,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create local config source: %w", err)
		}
		if err := manager.LoadFrom(source); err != nil {
			return nil, fmt.Errorf("failed to load local config: %w", err)
		}
	}

	// Extract app-scoped config if app name provided
	if appName != "" {
		if err := extractAppScopedConfig(manager, appName); err != nil {
			// Log but don't fail - app scoping is optional
			if logger != nil {
				logger.Debug("app-scoped config not found, using global config",
					F("app", appName),
				)
			}
		}
	}

	return manager, nil
}

// GetConfigSearchInfo returns information about where configs would be searched
// Useful for debugging config loading issues
func GetConfigSearchInfo(appName string) string {
	cwd, _ := os.Getwd()
	cfg := DefaultAutoDiscoveryConfig()
	cfg.AppName = appName
	
	var info strings.Builder
	info.WriteString(fmt.Sprintf("Config Search Information for app '%s':\n", appName))
	info.WriteString(fmt.Sprintf("  Working Directory: %s\n", cwd))
	info.WriteString(fmt.Sprintf("  Base Config Names: %v\n", cfg.ConfigNames))
	info.WriteString(fmt.Sprintf("  Local Config Names: %v\n", cfg.LocalConfigNames))
	info.WriteString(fmt.Sprintf("  Max Search Depth: %d parent directories\n", cfg.MaxDepth))
	info.WriteString(fmt.Sprintf("  App Scoping: %v\n", cfg.EnableAppScoping))
	
	if cfg.EnableAppScoping && appName != "" {
		info.WriteString(fmt.Sprintf("  App-Scoped Key: apps.%s\n", appName))
	}

	return info.String()
}

