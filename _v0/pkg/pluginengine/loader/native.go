package loader

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"plugin"
	"strings"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	plugins "github.com/xraph/forge/v0/pkg/pluginengine/common"
)

// NativeLoader loads native Go plugins (using Go's plugin package)
type NativeLoader struct {
	logger      common.Logger
	metrics     common.Metrics
	loadedSOs   map[string]*plugin.Plugin
	pluginCache map[string]plugins.PluginEngine
}

// NativePluginWrapper wraps a native Go plugin to implement the Plugin interface
type NativePluginWrapper struct {
	id           string
	name         string
	version      string
	description  string
	author       string
	license      string
	pluginType   plugins.PluginEngineType
	soPath       string
	goPlugin     *plugin.Plugin
	pluginImpl   plugins.PluginEngine
	capabilities []plugins.PluginEngineCapability
	dependencies []plugins.PluginEngineDependency
	config       interface{}
	metadata     map[string]interface{}
	startTime    time.Time
	metrics      plugins.PluginEngineMetrics
}

func (npw *NativePluginWrapper) Controllers() []common.Controller {
	// TODO implement me
	panic("implement me")
}

// NewNativeLoader creates a new native Go plugin loader
func NewNativeLoader(logger common.Logger, metrics common.Metrics) Loader {
	return &NativeLoader{
		logger:      logger,
		metrics:     metrics,
		loadedSOs:   make(map[string]*plugin.Plugin),
		pluginCache: make(map[string]plugins.PluginEngine),
	}
}

// Name returns the loader name
func (nl *NativeLoader) Name() string {
	return "native-loader"
}

// Type returns the loader type
func (nl *NativeLoader) Type() LoaderType {
	return LoaderTypeNative
}

// SupportedExtensions returns supported file extensions
func (nl *NativeLoader) SupportedExtensions() []string {
	return []string{".so", ".dylib"} // .dll not supported by Go's plugin package
}

// LoadPlugin loads a native Go plugin
func (nl *NativeLoader) LoadPlugin(ctx context.Context, source plugins.PluginEngineSource) (plugins.PluginEngine, error) {
	startTime := time.Now()

	// Validate source
	if err := nl.ValidateSource(source); err != nil {
		return nil, fmt.Errorf("source validation failed: %w", err)
	}

	// Resolve SO path
	soPath, err := nl.resolveSOPath(source)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve SO path: %w", err)
	}

	// Check cache first
	if cached, exists := nl.pluginCache[soPath]; exists {
		return cached, nil
	}

	// Load the shared object
	goPlugin, err := plugin.Open(soPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open native plugin: %w", err)
	}

	// Store the loaded SO
	nl.loadedSOs[soPath] = goPlugin

	// Look for the plugin instance or factory
	pluginImpl, err := nl.extractPluginFromSO(goPlugin, soPath)
	if err != nil {
		return nil, fmt.Errorf("failed to extract plugin from SO: %w", err)
	}

	// Load plugin metadata
	metadata, err := nl.loadNativeMetadata(soPath)
	if err != nil {
		if nl.logger != nil {
			nl.logger.Warn("failed to load native plugin metadata", logger.Error(err))
		}
		// Use default metadata
		metadata = nl.createDefaultMetadata(soPath)
	}

	// Create plugin wrapper
	wrapper := &NativePluginWrapper{
		id:           metadata.ID,
		name:         metadata.Name,
		version:      metadata.Version,
		description:  metadata.Description,
		author:       metadata.Author,
		license:      metadata.License,
		pluginType:   metadata.Type,
		soPath:       soPath,
		goPlugin:     goPlugin,
		pluginImpl:   pluginImpl,
		capabilities: metadata.Capabilities,
		dependencies: metadata.Dependencies,
		metadata:     make(map[string]interface{}),
		startTime:    time.Now(),
		metrics:      plugins.PluginEngineMetrics{},
	}

	// Apply source configuration
	if source.Config != nil {
		if err := wrapper.Configure(source.Config); err != nil {
			return nil, fmt.Errorf("failed to configure native plugin: %w", err)
		}
	}

	// Cache the wrapper
	nl.pluginCache[soPath] = wrapper

	loadTime := time.Since(startTime)

	if nl.logger != nil {
		nl.logger.Info("native plugin loaded",
			logger.String("plugin_id", wrapper.ID()),
			logger.String("so_path", soPath),
			logger.Duration("load_time", loadTime),
		)
	}

	if nl.metrics != nil {
		nl.metrics.Counter("forge.plugins.native.loaded").Inc()
		nl.metrics.Histogram("forge.plugins.native.load_time").Observe(loadTime.Seconds())
	}

	return wrapper, nil
}

// ValidateSource validates a native plugin source
func (nl *NativeLoader) ValidateSource(source plugins.PluginEngineSource) error {
	if source.Location == "" {
		return fmt.Errorf("native plugin location is required")
	}

	soPath, err := nl.resolveSOPath(source)
	if err != nil {
		return fmt.Errorf("failed to resolve SO path: %w", err)
	}

	// Check if file exists
	if _, err := os.Stat(soPath); os.IsNotExist(err) {
		return fmt.Errorf("native plugin file does not exist: %s", soPath)
	}

	// Check if it's a valid shared object
	return nl.validateSharedObject(soPath)
}

// GetMetadata extracts metadata from a native plugin
func (nl *NativeLoader) GetMetadata(source plugins.PluginEngineSource) (*plugins.PluginEngineInfo, error) {
	soPath, err := nl.resolveSOPath(source)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve SO path: %w", err)
	}

	metadata, err := nl.loadNativeMetadata(soPath)
	if err != nil {
		// Return default metadata if unable to load
		metadata = nl.createDefaultMetadata(soPath)
	}

	return metadata, nil
}

// resolveSOPath resolves the full path to the shared object file
func (nl *NativeLoader) resolveSOPath(source plugins.PluginEngineSource) (string, error) {
	switch source.Type {
	case plugins.PluginEngineSourceTypeFile:
		if filepath.IsAbs(source.Location) {
			return source.Location, nil
		}
		return filepath.Abs(source.Location)
	default:
		return "", fmt.Errorf("unsupported source type for native plugins: %s", source.Type)
	}
}

// validateSharedObject performs basic validation on the shared object
func (nl *NativeLoader) validateSharedObject(soPath string) error {
	// Try to open the plugin to validate it
	testPlugin, err := plugin.Open(soPath)
	if err != nil {
		return fmt.Errorf("invalid shared object: %w", err)
	}

	// Look for required symbols
	if _, err := testPlugin.Lookup("Plugin"); err != nil {
		if _, err := testPlugin.Lookup("NewPlugin"); err != nil {
			return fmt.Errorf("required symbol 'Plugin' or 'NewPlugin' not found in shared object")
		}
	}

	return nil
}

// extractPluginFromSO extracts the plugin implementation from the shared object
func (nl *NativeLoader) extractPluginFromSO(goPlugin *plugin.Plugin, soPath string) (plugins.PluginEngine, error) {
	// Strategy 1: Look for a Plugin variable
	if symbol, err := goPlugin.Lookup("Plugin"); err == nil {
		if pluginImpl, ok := symbol.(plugins.PluginEngine); ok {
			return pluginImpl, nil
		}
		if pluginPtr, ok := symbol.(*plugins.PluginEngine); ok && pluginPtr != nil {
			return *pluginPtr, nil
		}
	}

	// Strategy 2: Look for a NewPlugin factory function
	if symbol, err := goPlugin.Lookup("NewPlugin"); err == nil {
		if factory, ok := symbol.(func() plugins.PluginEngine); ok {
			return factory(), nil
		}
		if factoryPtr, ok := symbol.(*func() plugins.PluginEngine); ok && factoryPtr != nil {
			return (*factoryPtr)(), nil
		}
	}

	// Strategy 3: Look for other common factory function names
	factoryNames := []string{"CreatePlugin", "GetPlugin", "InitPlugin"}
	for _, name := range factoryNames {
		if symbol, err := goPlugin.Lookup(name); err == nil {
			if factory, ok := symbol.(func() plugins.PluginEngine); ok {
				return factory(), nil
			}
		}
	}

	return nil, fmt.Errorf("no valid plugin implementation or factory found in %s", soPath)
}

// loadNativeMetadata loads metadata from a native plugin
func (nl *NativeLoader) loadNativeMetadata(soPath string) (*plugins.PluginEngineInfo, error) {
	// Try to load from accompanying .json file first
	metadataPath := strings.TrimSuffix(soPath, filepath.Ext(soPath)) + ".json"
	if _, err := os.Stat(metadataPath); err == nil {
		return nl.loadMetadataFromJSON(metadataPath)
	}

	// Try to extract from the shared object itself
	return nl.extractMetadataFromSO(soPath)
}

// loadMetadataFromJSON loads plugin metadata from JSON file
func (nl *NativeLoader) loadMetadataFromJSON(path string) (*plugins.PluginEngineInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var info plugins.PluginEngineInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}

	return &info, nil
}

// extractMetadataFromSO attempts to extract metadata from the shared object
func (nl *NativeLoader) extractMetadataFromSO(soPath string) (*plugins.PluginEngineInfo, error) {
	goPlugin, err := plugin.Open(soPath)
	if err != nil {
		return nil, err
	}

	// Look for metadata symbols
	if symbol, err := goPlugin.Lookup("PluginInfo"); err == nil {
		if info, ok := symbol.(*plugins.PluginEngineInfo); ok {
			return info, nil
		}
		if info, ok := symbol.(plugins.PluginEngineInfo); ok {
			return &info, nil
		}
	}

	// Look for metadata function
	if symbol, err := goPlugin.Lookup("GetPluginInfo"); err == nil {
		if infoFunc, ok := symbol.(func() *plugins.PluginEngineInfo); ok {
			return infoFunc(), nil
		}
	}

	// Return default metadata if nothing found
	return nl.createDefaultMetadata(soPath), nil
}

// createDefaultMetadata creates default metadata for a native plugin
func (nl *NativeLoader) createDefaultMetadata(soPath string) *plugins.PluginEngineInfo {
	baseName := filepath.Base(soPath)
	nameWithoutExt := strings.TrimSuffix(baseName, filepath.Ext(baseName))

	return &plugins.PluginEngineInfo{
		ID:          nameWithoutExt,
		Name:        nameWithoutExt,
		Version:     "1.0.0",
		Type:        plugins.PluginEngineTypeUtility,
		Description: fmt.Sprintf("Native Go plugin: %s", baseName),
		Author:      "unknown",
		License:     "unknown",
	}
}

// NativePluginWrapper implementation of Plugin interface

func (npw *NativePluginWrapper) ID() string                     { return npw.id }
func (npw *NativePluginWrapper) Name() string                   { return npw.name }
func (npw *NativePluginWrapper) Version() string                { return npw.version }
func (npw *NativePluginWrapper) Description() string            { return npw.description }
func (npw *NativePluginWrapper) Author() string                 { return npw.author }
func (npw *NativePluginWrapper) License() string                { return npw.license }
func (npw *NativePluginWrapper) Type() plugins.PluginEngineType { return npw.pluginType }
func (npw *NativePluginWrapper) Capabilities() []plugins.PluginEngineCapability {
	return npw.capabilities
}
func (npw *NativePluginWrapper) Dependencies() []plugins.PluginEngineDependency {
	return npw.dependencies
}

func (npw *NativePluginWrapper) Initialize(ctx context.Context, container common.Container) error {
	return npw.pluginImpl.Initialize(ctx, container)
}

func (npw *NativePluginWrapper) OnStart(ctx context.Context) error {
	return npw.pluginImpl.OnStart(ctx)
}

func (npw *NativePluginWrapper) OnStop(ctx context.Context) error {
	return npw.pluginImpl.OnStop(ctx)
}

func (npw *NativePluginWrapper) Cleanup(ctx context.Context) error {
	return npw.pluginImpl.Cleanup(ctx)
}

func (npw *NativePluginWrapper) Configure(config interface{}) error {
	npw.config = config
	return npw.pluginImpl.Configure(config)
}

func (npw *NativePluginWrapper) GetConfig() interface{} {
	return npw.config
}

func (npw *NativePluginWrapper) HealthCheck(ctx context.Context) error {
	return npw.pluginImpl.HealthCheck(ctx)
}

func (npw *NativePluginWrapper) GetMetrics() plugins.PluginEngineMetrics {
	npw.metrics.Uptime = time.Since(npw.startTime)

	// Merge with underlying plugin metrics if available
	if npw.pluginImpl != nil {
		implMetrics := npw.pluginImpl.GetMetrics()
		npw.metrics.CallCount = implMetrics.CallCount
		npw.metrics.ErrorCount = implMetrics.ErrorCount
		npw.metrics.AverageLatency = implMetrics.AverageLatency
		npw.metrics.LastExecuted = implMetrics.LastExecuted
		npw.metrics.MemoryUsage = implMetrics.MemoryUsage
		npw.metrics.CPUUsage = implMetrics.CPUUsage
		npw.metrics.HealthScore = implMetrics.HealthScore
	}

	return npw.metrics
}

func (npw *NativePluginWrapper) Middleware() []any {
	if npw.pluginImpl != nil {
		return npw.pluginImpl.Middleware()
	}
	return []any{}
}

func (npw *NativePluginWrapper) ConfigureRoutes(r common.Router) error {
	if npw.pluginImpl != nil {
		return npw.pluginImpl.ConfigureRoutes(r)
	}
	return nil
}

func (npw *NativePluginWrapper) Services() []common.ServiceDefinition {
	if npw.pluginImpl != nil {
		return npw.pluginImpl.Services()
	}
	return []common.ServiceDefinition{}
}

func (npw *NativePluginWrapper) Commands() []plugins.CLICommand {
	if npw.pluginImpl != nil {
		return npw.pluginImpl.Commands()
	}
	return []plugins.CLICommand{}
}

func (npw *NativePluginWrapper) Hooks() []plugins.Hook {
	if npw.pluginImpl != nil {
		return npw.pluginImpl.Hooks()
	}
	return []plugins.Hook{}
}

func (npw *NativePluginWrapper) ConfigSchema() plugins.ConfigSchema {
	if npw.pluginImpl != nil {
		return npw.pluginImpl.ConfigSchema()
	}
	return plugins.ConfigSchema{}
}

// GetNativePlugin returns the underlying native plugin implementation
func (npw *NativePluginWrapper) GetNativePlugin() plugins.PluginEngine {
	return npw.pluginImpl
}

// GetGoPlugin returns the underlying Go plugin object
func (npw *NativePluginWrapper) GetGoPlugin() *plugin.Plugin {
	return npw.goPlugin
}

// GetSOPath returns the path to the shared object file
func (npw *NativePluginWrapper) GetSOPath() string {
	return npw.soPath
}

// NativePluginBuilder helps build native plugins
type NativePluginBuilder struct {
	sourceDir   string
	outputDir   string
	buildFlags  []string
	ldFlags     []string
	environment map[string]string
}

// NewNativePluginBuilder creates a new native plugin builder
func NewNativePluginBuilder() *NativePluginBuilder {
	return &NativePluginBuilder{
		buildFlags:  []string{},
		ldFlags:     []string{},
		environment: make(map[string]string),
	}
}

// SetSourceDir sets the source directory
func (npb *NativePluginBuilder) SetSourceDir(dir string) *NativePluginBuilder {
	npb.sourceDir = dir
	return npb
}

// SetOutputDir sets the output directory
func (npb *NativePluginBuilder) SetOutputDir(dir string) *NativePluginBuilder {
	npb.outputDir = dir
	return npb
}

// AddBuildFlag adds a build flag
func (npb *NativePluginBuilder) AddBuildFlag(flag string) *NativePluginBuilder {
	npb.buildFlags = append(npb.buildFlags, flag)
	return npb
}

// AddLDFlag adds an LD flag
func (npb *NativePluginBuilder) AddLDFlag(flag string) *NativePluginBuilder {
	npb.ldFlags = append(npb.ldFlags, flag)
	return npb
}

// SetEnvironment sets an environment variable
func (npb *NativePluginBuilder) SetEnvironment(key, value string) *NativePluginBuilder {
	npb.environment[key] = value
	return npb
}

// Build compiles the native plugin
func (npb *NativePluginBuilder) Build(pluginName string) (string, error) {
	if npb.sourceDir == "" {
		return "", fmt.Errorf("source directory not set")
	}

	if npb.outputDir == "" {
		npb.outputDir = "."
	}

	outputPath := filepath.Join(npb.outputDir, pluginName+".so")

	// Build command arguments
	args := []string{"build", "-buildmode=plugin"}
	args = append(args, npb.buildFlags...)

	if len(npb.ldFlags) > 0 {
		args = append(args, "-ldflags", strings.Join(npb.ldFlags, " "))
	}

	args = append(args, "-o", outputPath, npb.sourceDir)

	// Execute build command
	cmd := exec.Command("go", args...)

	// Set environment variables
	for key, value := range npb.environment {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build failed: %w, output: %s", err, string(output))
	}

	return outputPath, nil
}
