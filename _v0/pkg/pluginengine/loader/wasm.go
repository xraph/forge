package loader

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	plugins "github.com/xraph/forge/v0/pkg/pluginengine/common"
)

// WASMLoader loads WebAssembly plugins
type WASMLoader struct {
	logger  common.Logger
	metrics common.Metrics
	runtime WASMRuntime
	modules map[string]*WASMModule
}

// WASMRuntime defines the interface for WebAssembly runtimes
type WASMRuntime interface {
	Name() string
	LoadModule(wasmPath string) (*WASMModule, error)
	UnloadModule(moduleID string) error
	CallFunction(moduleID, functionName string, args ...interface{}) (interface{}, error)
	ValidateModule(wasmPath string) error
	GetModuleExports(moduleID string) ([]string, error)
	GetModuleImports(moduleID string) ([]string, error)
}

// WASMModule represents a loaded WebAssembly module
type WASMModule struct {
	ID       string
	Path     string
	Exports  []string
	Imports  []string
	Instance interface{} // Runtime-specific instance
	Metadata map[string]interface{}
}

// WASMPluginWrapper wraps a WASM module to implement the Plugin interface
type WASMPluginWrapper struct {
	id           string
	name         string
	version      string
	description  string
	author       string
	license      string
	pluginType   plugins.PluginEngineType
	wasmPath     string
	module       *WASMModule
	runtime      WASMRuntime
	capabilities []plugins.PluginEngineCapability
	dependencies []plugins.PluginEngineDependency
	config       interface{}
	metadata     map[string]interface{}
	startTime    time.Time
	metrics      plugins.PluginEngineMetrics
}

// NewWASMLoader creates a new WebAssembly plugin loader
func NewWASMLoader(logger common.Logger, metrics common.Metrics) Loader {
	wl := &WASMLoader{
		logger:  logger,
		metrics: metrics,
		modules: make(map[string]*WASMModule),
	}

	// Initialize WASM runtime
	wl.runtime = NewWasmTimeRuntime() // or other runtime

	return wl
}

// Name returns the loader name
func (wl *WASMLoader) Name() string {
	return "wasm-loader"
}

// Type returns the loader type
func (wl *WASMLoader) Type() LoaderType {
	return LoaderTypeWASM
}

// SupportedExtensions returns supported file extensions
func (wl *WASMLoader) SupportedExtensions() []string {
	return []string{".wasm", ".wat"}
}

// LoadPlugin loads a WebAssembly plugin
func (wl *WASMLoader) LoadPlugin(ctx context.Context, source plugins.PluginEngineSource) (plugins.PluginEngine, error) {
	startTime := time.Now()

	// Validate source
	if err := wl.ValidateSource(source); err != nil {
		return nil, fmt.Errorf("source validation failed: %w", err)
	}

	// Resolve WASM path
	wasmPath, err := wl.resolveWASMPath(source)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve WASM path: %w", err)
	}

	// Load WASM module
	module, err := wl.runtime.LoadModule(wasmPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load WASM module: %w", err)
	}

	// Load plugin metadata
	metadata, err := wl.loadWASMMetadata(wasmPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load WASM metadata: %w", err)
	}

	// Create plugin wrapper
	wrapper := &WASMPluginWrapper{
		id:           metadata.ID,
		name:         metadata.Name,
		version:      metadata.Version,
		description:  metadata.Description,
		author:       metadata.Author,
		license:      metadata.License,
		pluginType:   metadata.Type,
		wasmPath:     wasmPath,
		module:       module,
		runtime:      wl.runtime,
		capabilities: metadata.Capabilities,
		dependencies: metadata.Dependencies,
		metadata:     make(map[string]interface{}),
		startTime:    time.Now(),
		metrics:      plugins.PluginEngineMetrics{},
	}

	// Store module reference
	wl.modules[module.ID] = module

	// Initialize plugin from WASM
	if err := wrapper.initializeFromWASM(); err != nil {
		return nil, fmt.Errorf("failed to initialize WASM plugin: %w", err)
	}

	// Apply source configuration
	if source.Config != nil {
		if err := wrapper.Configure(source.Config); err != nil {
			return nil, fmt.Errorf("failed to configure WASM plugin: %w", err)
		}
	}

	loadTime := time.Since(startTime)

	if wl.logger != nil {
		wl.logger.Info("WASM plugin loaded",
			logger.String("plugin_id", wrapper.ID()),
			logger.String("wasm_path", wasmPath),
			logger.String("runtime", wl.runtime.Name()),
			logger.Duration("load_time", loadTime),
			logger.String("exports", strings.Join(module.Exports, ", ")),
		)
	}

	if wl.metrics != nil {
		wl.metrics.Counter("forge.plugins.wasm.loaded").Inc()
		wl.metrics.Histogram("forge.plugins.wasm.load_time").Observe(loadTime.Seconds())
	}

	return wrapper, nil
}

// ValidateSource validates a WASM plugin source
func (wl *WASMLoader) ValidateSource(source plugins.PluginEngineSource) error {
	if source.Location == "" {
		return fmt.Errorf("WASM location is required")
	}

	wasmPath, err := wl.resolveWASMPath(source)
	if err != nil {
		return fmt.Errorf("failed to resolve WASM path: %w", err)
	}

	// Check if file exists
	if _, err := os.Stat(wasmPath); os.IsNotExist(err) {
		return fmt.Errorf("WASM file does not exist: %s", wasmPath)
	}

	// Validate WASM module
	return wl.runtime.ValidateModule(wasmPath)
}

// GetMetadata extracts metadata from a WASM plugin
func (wl *WASMLoader) GetMetadata(source plugins.PluginEngineSource) (*plugins.PluginEngineInfo, error) {
	wasmPath, err := wl.resolveWASMPath(source)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve WASM path: %w", err)
	}

	return wl.loadWASMMetadata(wasmPath)
}

// resolveWASMPath resolves the full path to the WASM file
func (wl *WASMLoader) resolveWASMPath(source plugins.PluginEngineSource) (string, error) {
	switch source.Type {
	case plugins.PluginEngineSourceTypeFile:
		if filepath.IsAbs(source.Location) {
			return source.Location, nil
		}
		return filepath.Abs(source.Location)
	default:
		return "", fmt.Errorf("unsupported source type for WASM: %s", source.Type)
	}
}

// loadWASMMetadata loads metadata from a WASM file or accompanying JSON
func (wl *WASMLoader) loadWASMMetadata(wasmPath string) (*plugins.PluginEngineInfo, error) {
	// Try to load from accompanying .json file first
	metadataPath := strings.TrimSuffix(wasmPath, filepath.Ext(wasmPath)) + ".json"
	if _, err := os.Stat(metadataPath); err == nil {
		return wl.loadMetadataFromJSON(metadataPath)
	}

	// Extract basic metadata from WASM file
	return wl.extractMetadataFromWASM(wasmPath)
}

// loadMetadataFromJSON loads plugin metadata from JSON file
func (wl *WASMLoader) loadMetadataFromJSON(path string) (*plugins.PluginEngineInfo, error) {
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

// extractMetadataFromWASM extracts basic metadata from WASM file
func (wl *WASMLoader) extractMetadataFromWASM(wasmPath string) (*plugins.PluginEngineInfo, error) {
	baseName := filepath.Base(wasmPath)
	nameWithoutExt := strings.TrimSuffix(baseName, filepath.Ext(baseName))

	return &plugins.PluginEngineInfo{
		ID:          nameWithoutExt,
		Name:        nameWithoutExt,
		Version:     "1.0.0",
		Type:        plugins.PluginEngineTypeUtility,
		Description: fmt.Sprintf("WASM plugin: %s", baseName),
		Author:      "unknown",
		License:     "unknown",
	}, nil
}

// WASMPluginWrapper implementation of Plugin interface

func (wpw *WASMPluginWrapper) ID() string                     { return wpw.id }
func (wpw *WASMPluginWrapper) Name() string                   { return wpw.name }
func (wpw *WASMPluginWrapper) Version() string                { return wpw.version }
func (wpw *WASMPluginWrapper) Description() string            { return wpw.description }
func (wpw *WASMPluginWrapper) Author() string                 { return wpw.author }
func (wpw *WASMPluginWrapper) License() string                { return wpw.license }
func (wpw *WASMPluginWrapper) Type() plugins.PluginEngineType { return wpw.pluginType }
func (wpw *WASMPluginWrapper) Capabilities() []plugins.PluginEngineCapability {
	return wpw.capabilities
}
func (wpw *WASMPluginWrapper) Dependencies() []plugins.PluginEngineDependency {
	return wpw.dependencies
}

func (wpw *WASMPluginWrapper) Initialize(ctx context.Context, container common.Container) error {
	// Call WASM initialize function if available
	if wpw.hasFunction("initialize") {
		_, err := wpw.runtime.CallFunction(wpw.module.ID, "initialize", container)
		return err
	}
	return nil
}

func (wpw *WASMPluginWrapper) OnStart(ctx context.Context) error {
	if wpw.hasFunction("start") {
		_, err := wpw.runtime.CallFunction(wpw.module.ID, "start")
		return err
	}
	return nil
}

func (wpw *WASMPluginWrapper) OnStop(ctx context.Context) error {
	if wpw.hasFunction("stop") {
		_, err := wpw.runtime.CallFunction(wpw.module.ID, "stop")
		return err
	}
	return nil
}

func (wpw *WASMPluginWrapper) Cleanup(ctx context.Context) error {
	return wpw.runtime.UnloadModule(wpw.module.ID)
}

func (wpw *WASMPluginWrapper) Configure(config interface{}) error {
	wpw.config = config
	if wpw.hasFunction("configure") {
		_, err := wpw.runtime.CallFunction(wpw.module.ID, "configure", config)
		return err
	}
	return nil
}

func (wpw *WASMPluginWrapper) GetConfig() interface{} {
	return wpw.config
}

func (wpw *WASMPluginWrapper) HealthCheck(ctx context.Context) error {
	if wpw.hasFunction("health_check") {
		result, err := wpw.runtime.CallFunction(wpw.module.ID, "health_check")
		if err != nil {
			return fmt.Errorf("WASM health check failed: %w", err)
		}
		if healthy, ok := result.(bool); ok && !healthy {
			return fmt.Errorf("WASM plugin reported unhealthy")
		}
	}
	return nil
}

func (wpw *WASMPluginWrapper) GetMetrics() plugins.PluginEngineMetrics {
	wpw.metrics.Uptime = time.Since(wpw.startTime)
	return wpw.metrics
}

func (wpw *WASMPluginWrapper) Middleware() []any {
	return []any{}
}

func (wpw *WASMPluginWrapper) Controllers() []common.Controller {
	return []common.Controller{}
}

func (wpw *WASMPluginWrapper) ConfigureRoutes(r common.Router) error {
	return nil
}

func (wpw *WASMPluginWrapper) Services() []common.ServiceDefinition {
	return []common.ServiceDefinition{}
}

func (wpw *WASMPluginWrapper) Commands() []plugins.CLICommand {
	return []plugins.CLICommand{}
}

func (wpw *WASMPluginWrapper) Hooks() []plugins.Hook {
	return []plugins.Hook{}
}

func (wpw *WASMPluginWrapper) ConfigSchema() plugins.ConfigSchema {
	return plugins.ConfigSchema{}
}

func (wpw *WASMPluginWrapper) hasFunction(name string) bool {
	for _, export := range wpw.module.Exports {
		if export == name {
			return true
		}
	}
	return false
}

func (wpw *WASMPluginWrapper) initializeFromWASM() error {
	// Initialize plugin metadata from WASM module
	return nil
}

// Basic WASM runtime implementation using a hypothetical wasmtime-go binding

// WasmTimeRuntime implements WASMRuntime using Wasmtime
type WasmTimeRuntime struct {
	modules map[string]*WASMModule
}

// NewWasmTimeRuntime creates a new Wasmtime-based WASM runtime
func NewWasmTimeRuntime() WASMRuntime {
	return &WasmTimeRuntime{
		modules: make(map[string]*WASMModule),
	}
}

func (wtr *WasmTimeRuntime) Name() string {
	return "wasmtime"
}

func (wtr *WasmTimeRuntime) LoadModule(wasmPath string) (*WASMModule, error) {
	// Read WASM file
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read WASM file: %w", err)
	}

	// Validate WASM header
	if len(wasmBytes) < 8 || string(wasmBytes[:4]) != "\x00asm" {
		return nil, fmt.Errorf("invalid WASM file format")
	}

	// Create module ID
	moduleID := fmt.Sprintf("module_%s_%d", filepath.Base(wasmPath), time.Now().UnixNano())

	// In a real implementation, you would use wasmtime-go or similar:
	// engine := wasmtime.NewEngine()
	// module, err := wasmtime.NewModule(engine, wasmBytes)
	// if err != nil {
	//     return nil, err
	// }
	// store := wasmtime.NewStore(engine)
	// instance, err := wasmtime.NewInstance(store, module, []wasmtime.AsExtern{})

	// For now, create a mock module
	module := &WASMModule{
		ID:       moduleID,
		Path:     wasmPath,
		Exports:  []string{"initialize", "start", "stop", "configure", "health_check"}, // Mock exports
		Imports:  []string{},                                                           // Mock imports
		Instance: nil,                                                                  // Would contain the actual WASM instance
		Metadata: make(map[string]interface{}),
	}

	wtr.modules[moduleID] = module
	return module, nil
}

func (wtr *WasmTimeRuntime) UnloadModule(moduleID string) error {
	delete(wtr.modules, moduleID)
	return nil
}

func (wtr *WasmTimeRuntime) CallFunction(moduleID, functionName string, args ...interface{}) (interface{}, error) {
	module, exists := wtr.modules[moduleID]
	if !exists {
		return nil, fmt.Errorf("module not found: %s", moduleID)
	}

	// Check if function exists in exports
	found := false
	for _, export := range module.Exports {
		if export == functionName {
			found = true
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("function not found: %s", functionName)
	}

	// In a real implementation, you would call the actual WASM function:
	// function := instance.GetFunc(store, functionName)
	// if function == nil {
	//     return nil, fmt.Errorf("function not found: %s", functionName)
	// }
	// result, err := function.Call(store, args...)
	// return result, err

	// Mock implementation
	return nil, nil
}

func (wtr *WasmTimeRuntime) ValidateModule(wasmPath string) error {
	// Read and validate WASM file
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return fmt.Errorf("failed to read WASM file: %w", err)
	}

	// Check WASM magic number
	if len(wasmBytes) < 8 {
		return fmt.Errorf("file too small to be a valid WASM module")
	}

	if string(wasmBytes[:4]) != "\x00asm" {
		return fmt.Errorf("invalid WASM magic number")
	}

	// Check version (should be 1)
	version := uint32(wasmBytes[4]) | uint32(wasmBytes[5])<<8 | uint32(wasmBytes[6])<<16 | uint32(wasmBytes[7])<<24
	if version != 1 {
		return fmt.Errorf("unsupported WASM version: %d", version)
	}

	return nil
}

func (wtr *WasmTimeRuntime) GetModuleExports(moduleID string) ([]string, error) {
	module, exists := wtr.modules[moduleID]
	if !exists {
		return nil, fmt.Errorf("module not found: %s", moduleID)
	}
	return module.Exports, nil
}

func (wtr *WasmTimeRuntime) GetModuleImports(moduleID string) ([]string, error) {
	module, exists := wtr.modules[moduleID]
	if !exists {
		return nil, fmt.Errorf("module not found: %s", moduleID)
	}
	return module.Imports, nil
}
