package loader

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	plugins "github.com/xraph/forge/pkg/pluginengine/common"
)

// ScriptLoader loads script-based plugins (Lua, Python, JavaScript, etc.)
type ScriptLoader struct {
	logger       common.Logger
	metrics      common.Metrics
	interpreters map[string]ScriptInterpreter
	scriptCache  map[string]*ScriptPluginWrapper
}

// ScriptInterpreter defines the interface for script interpreters
type ScriptInterpreter interface {
	Name() string
	Extensions() []string
	ExecuteScript(scriptPath string, function string, args ...interface{}) (interface{}, error)
	ValidateScript(scriptPath string) error
	LoadScript(scriptPath string) error
	UnloadScript(scriptPath string) error
}

// ScriptPluginWrapper wraps a script plugin to implement the Plugin interface
type ScriptPluginWrapper struct {
	id           string
	name         string
	version      string
	description  string
	author       string
	license      string
	pluginType   plugins.PluginEngineType
	scriptPath   string
	interpreter  ScriptInterpreter
	capabilities []plugins.PluginEngineCapability
	dependencies []plugins.PluginEngineDependency
	config       interface{}
	metadata     map[string]interface{}
	startTime    time.Time
	metrics      plugins.PluginEngineMetrics
}

// NewScriptLoader creates a new script plugin loader
func NewScriptLoader(logger common.Logger, metrics common.Metrics) Loader {
	sl := &ScriptLoader{
		logger:       logger,
		metrics:      metrics,
		interpreters: make(map[string]ScriptInterpreter),
		scriptCache:  make(map[string]*ScriptPluginWrapper),
	}

	// Register default interpreters
	sl.registerDefaultInterpreters()

	return sl
}

// Name returns the loader name
func (sl *ScriptLoader) Name() string {
	return "script-loader"
}

// Type returns the loader type
func (sl *ScriptLoader) Type() LoaderType {
	return LoaderTypeScript
}

// SupportedExtensions returns supported file extensions
func (sl *ScriptLoader) SupportedExtensions() []string {
	var extensions []string
	for _, interpreter := range sl.interpreters {
		extensions = append(extensions, interpreter.Extensions()...)
	}
	return extensions
}

func (spw *ScriptPluginWrapper) Controllers() []common.Controller {
	return []common.Controller{}
}

// LoadPlugin loads a script plugin
func (sl *ScriptLoader) LoadPlugin(ctx context.Context, source plugins.PluginEngineSource) (plugins.PluginEngine, error) {
	startTime := time.Now()

	// Validate source
	if err := sl.ValidateSource(source); err != nil {
		return nil, fmt.Errorf("source validation failed: %w", err)
	}

	// Resolve script path
	scriptPath, err := sl.resolveScriptPath(source)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve script path: %w", err)
	}

	// Check cache first
	if cached, exists := sl.scriptCache[scriptPath]; exists {
		return cached, nil
	}

	// Determine interpreter
	interpreter, err := sl.getInterpreterForScript(scriptPath)
	if err != nil {
		return nil, fmt.Errorf("failed to determine interpreter: %w", err)
	}

	// Load script metadata
	metadata, err := sl.loadScriptMetadata(scriptPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load script metadata: %w", err)
	}

	// Create plugin wrapper
	wrapper := &ScriptPluginWrapper{
		id:           metadata.ID,
		name:         metadata.Name,
		version:      metadata.Version,
		description:  metadata.Description,
		author:       metadata.Author,
		license:      metadata.License,
		pluginType:   metadata.Type,
		scriptPath:   scriptPath,
		interpreter:  interpreter,
		capabilities: metadata.Capabilities,
		dependencies: metadata.Dependencies,
		metadata:     make(map[string]interface{}),
		startTime:    time.Now(),
		metrics:      plugins.PluginEngineMetrics{},
	}

	// Load script into interpreter
	if err := interpreter.LoadScript(scriptPath); err != nil {
		return nil, fmt.Errorf("failed to load script: %w", err)
	}

	// Initialize plugin
	if err := wrapper.initializeFromScript(); err != nil {
		return nil, fmt.Errorf("failed to initialize script plugin: %w", err)
	}

	// Apply source configuration
	if source.Config != nil {
		if err := wrapper.Configure(source.Config); err != nil {
			return nil, fmt.Errorf("failed to configure script plugin: %w", err)
		}
	}

	// Cache the wrapper
	sl.scriptCache[scriptPath] = wrapper

	loadTime := time.Since(startTime)

	if sl.logger != nil {
		sl.logger.Info("script plugin loaded",
			logger.String("plugin_id", wrapper.ID()),
			logger.String("script_path", scriptPath),
			logger.String("interpreter", interpreter.Name()),
			logger.Duration("load_time", loadTime),
		)
	}

	if sl.metrics != nil {
		sl.metrics.Counter("forge.plugins.script.loaded").Inc()
		sl.metrics.Histogram("forge.plugins.script.load_time").Observe(loadTime.Seconds())
	}

	return wrapper, nil
}

// ValidateSource validates a script plugin source
func (sl *ScriptLoader) ValidateSource(source plugins.PluginEngineSource) error {
	if source.Location == "" {
		return fmt.Errorf("script location is required")
	}

	scriptPath, err := sl.resolveScriptPath(source)
	if err != nil {
		return fmt.Errorf("failed to resolve script path: %w", err)
	}

	// Check if file exists
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		return fmt.Errorf("script file does not exist: %s", scriptPath)
	}

	// Get interpreter for validation
	interpreter, err := sl.getInterpreterForScript(scriptPath)
	if err != nil {
		return fmt.Errorf("no interpreter available: %w", err)
	}

	// Validate script syntax
	return interpreter.ValidateScript(scriptPath)
}

// GetMetadata extracts metadata from a script plugin
func (sl *ScriptLoader) GetMetadata(source plugins.PluginEngineSource) (*plugins.PluginEngineInfo, error) {
	scriptPath, err := sl.resolveScriptPath(source)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve script path: %w", err)
	}

	return sl.loadScriptMetadata(scriptPath)
}

// registerDefaultInterpreters registers default script interpreters
func (sl *ScriptLoader) registerDefaultInterpreters() {
	// Register Lua interpreter
	luaInterpreter := NewLuaInterpreter()
	sl.interpreters["lua"] = luaInterpreter

	// Register Python interpreter
	pythonInterpreter := NewPythonInterpreter()
	sl.interpreters["python"] = pythonInterpreter

	// Register JavaScript interpreter (Node.js)
	jsInterpreter := NewJavaScriptInterpreter()
	sl.interpreters["javascript"] = jsInterpreter
}

// resolveScriptPath resolves the full path to the script file
func (sl *ScriptLoader) resolveScriptPath(source plugins.PluginEngineSource) (string, error) {
	switch source.Type {
	case plugins.PluginEngineSourceTypeFile:
		if filepath.IsAbs(source.Location) {
			return source.Location, nil
		}
		return filepath.Abs(source.Location)
	default:
		return "", fmt.Errorf("unsupported source type for scripts: %s", source.Type)
	}
}

// getInterpreterForScript determines the appropriate interpreter for a script
func (sl *ScriptLoader) getInterpreterForScript(scriptPath string) (ScriptInterpreter, error) {
	ext := strings.ToLower(filepath.Ext(scriptPath))

	for _, interpreter := range sl.interpreters {
		for _, supportedExt := range interpreter.Extensions() {
			if ext == supportedExt {
				return interpreter, nil
			}
		}
	}

	return nil, fmt.Errorf("no interpreter found for extension: %s", ext)
}

// loadScriptMetadata loads metadata from a script file or accompanying JSON
func (sl *ScriptLoader) loadScriptMetadata(scriptPath string) (*plugins.PluginEngineInfo, error) {
	// Try to load from accompanying .json file first
	metadataPath := strings.TrimSuffix(scriptPath, filepath.Ext(scriptPath)) + ".json"
	if _, err := os.Stat(metadataPath); err == nil {
		return sl.loadMetadataFromJSON(metadataPath)
	}

	// Try to extract from script comments/docstrings
	return sl.extractMetadataFromScript(scriptPath)
}

// loadMetadataFromJSON loads plugin metadata from JSON file
func (sl *ScriptLoader) loadMetadataFromJSON(path string) (*plugins.PluginEngineInfo, error) {
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

// extractMetadataFromScript extracts metadata from script file
func (sl *ScriptLoader) extractMetadataFromScript(scriptPath string) (*plugins.PluginEngineInfo, error) {
	// This is a simplified implementation
	// In practice, you would parse script comments or docstrings for metadata

	baseName := filepath.Base(scriptPath)
	nameWithoutExt := strings.TrimSuffix(baseName, filepath.Ext(baseName))

	return &plugins.PluginEngineInfo{
		ID:          nameWithoutExt,
		Name:        nameWithoutExt,
		Version:     "1.0.0",
		Type:        plugins.PluginEngineTypeUtility,
		Description: fmt.Sprintf("Script plugin: %s", baseName),
		Author:      "unknown",
		License:     "unknown",
	}, nil
}

// ScriptPluginWrapper implementation of Plugin interface

func (spw *ScriptPluginWrapper) ID() string                               { return spw.id }
func (spw *ScriptPluginWrapper) Name() string                             { return spw.name }
func (spw *ScriptPluginWrapper) Version() string                          { return spw.version }
func (spw *ScriptPluginWrapper) Description() string                      { return spw.description }
func (spw *ScriptPluginWrapper) Author() string                           { return spw.author }
func (spw *ScriptPluginWrapper) License() string                          { return spw.license }
func (spw *ScriptPluginWrapper) Type() plugins.PluginEngineType                 { return spw.pluginType }
func (spw *ScriptPluginWrapper) Capabilities() []plugins.PluginEngineCapability { return spw.capabilities }
func (spw *ScriptPluginWrapper) Dependencies() []plugins.PluginEngineDependency { return spw.dependencies }

func (spw *ScriptPluginWrapper) Initialize(ctx context.Context, container common.Container) error {
	// Call script initialize function if available
	_, err := spw.interpreter.ExecuteScript(spw.scriptPath, "initialize", container)
	return err
}

func (spw *ScriptPluginWrapper) OnStart(ctx context.Context) error {
	_, err := spw.interpreter.ExecuteScript(spw.scriptPath, "start")
	return err
}

func (spw *ScriptPluginWrapper) OnStop(ctx context.Context) error {
	_, err := spw.interpreter.ExecuteScript(spw.scriptPath, "stop")
	return err
}

func (spw *ScriptPluginWrapper) Cleanup(ctx context.Context) error {
	return spw.interpreter.UnloadScript(spw.scriptPath)
}

func (spw *ScriptPluginWrapper) Configure(config interface{}) error {
	spw.config = config
	_, err := spw.interpreter.ExecuteScript(spw.scriptPath, "configure", config)
	return err
}

func (spw *ScriptPluginWrapper) GetConfig() interface{} {
	return spw.config
}

func (spw *ScriptPluginWrapper) HealthCheck(ctx context.Context) error {
	_, err := spw.interpreter.ExecuteScript(spw.scriptPath, "health_check")
	if err != nil {
		return fmt.Errorf("script health check failed: %w", err)
	}
	return nil
}

func (spw *ScriptPluginWrapper) GetMetrics() plugins.PluginEngineMetrics {
	spw.metrics.Uptime = time.Since(spw.startTime)
	return spw.metrics
}

func (spw *ScriptPluginWrapper) Middleware() []any {
	return []any{}
}

func (spw *ScriptPluginWrapper) ConfigureRoutes(r common.Router) error {
	return nil
}

func (spw *ScriptPluginWrapper) Services() []common.ServiceDefinition {
	return []common.ServiceDefinition{}
}

func (spw *ScriptPluginWrapper) Commands() []plugins.CLICommand {
	return []plugins.CLICommand{}
}

func (spw *ScriptPluginWrapper) Hooks() []plugins.Hook {
	return []plugins.Hook{}
}

func (spw *ScriptPluginWrapper) ConfigSchema() plugins.ConfigSchema {
	return plugins.ConfigSchema{}
}

func (spw *ScriptPluginWrapper) initializeFromScript() error {
	// Initialize plugin metadata from script
	return nil
}

// Basic interpreter implementations

// LuaInterpreter implements ScriptInterpreter for Lua scripts
type LuaInterpreter struct{}

func NewLuaInterpreter() ScriptInterpreter {
	return &LuaInterpreter{}
}

func (li *LuaInterpreter) Name() string {
	return "lua"
}

func (li *LuaInterpreter) Extensions() []string {
	return []string{".lua"}
}

func (li *LuaInterpreter) ExecuteScript(scriptPath, function string, args ...interface{}) (interface{}, error) {
	// Placeholder implementation
	// In a real implementation, you would use a Lua library like gopher-lua
	cmd := exec.Command("lua", scriptPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("lua execution failed: %w, output: %s", err, string(output))
	}
	return string(output), nil
}

func (li *LuaInterpreter) ValidateScript(scriptPath string) error {
	cmd := exec.Command("lua", "-p", scriptPath)
	return cmd.Run()
}

func (li *LuaInterpreter) LoadScript(scriptPath string) error {
	return nil // Placeholder
}

func (li *LuaInterpreter) UnloadScript(scriptPath string) error {
	return nil // Placeholder
}

// PythonInterpreter implements ScriptInterpreter for Python scripts
type PythonInterpreter struct{}

func NewPythonInterpreter() ScriptInterpreter {
	return &PythonInterpreter{}
}

func (pi *PythonInterpreter) Name() string {
	return "python"
}

func (pi *PythonInterpreter) Extensions() []string {
	return []string{".py", ".python"}
}

func (pi *PythonInterpreter) ExecuteScript(scriptPath, function string, args ...interface{}) (interface{}, error) {
	cmd := exec.Command("python3", scriptPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("python execution failed: %w, output: %s", err, string(output))
	}
	return string(output), nil
}

func (pi *PythonInterpreter) ValidateScript(scriptPath string) error {
	cmd := exec.Command("python3", "-m", "py_compile", scriptPath)
	return cmd.Run()
}

func (pi *PythonInterpreter) LoadScript(scriptPath string) error {
	return nil
}

func (pi *PythonInterpreter) UnloadScript(scriptPath string) error {
	return nil
}

// JavaScriptInterpreter implements ScriptInterpreter for JavaScript/Node.js
type JavaScriptInterpreter struct{}

func NewJavaScriptInterpreter() ScriptInterpreter {
	return &JavaScriptInterpreter{}
}

func (ji *JavaScriptInterpreter) Name() string {
	return "javascript"
}

func (ji *JavaScriptInterpreter) Extensions() []string {
	return []string{".js", ".mjs"}
}

func (ji *JavaScriptInterpreter) ExecuteScript(scriptPath, function string, args ...interface{}) (interface{}, error) {
	cmd := exec.Command("node", scriptPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("node execution failed: %w, output: %s", err, string(output))
	}
	return string(output), nil
}

func (ji *JavaScriptInterpreter) ValidateScript(scriptPath string) error {
	cmd := exec.Command("node", "--check", scriptPath)
	return cmd.Run()
}

func (ji *JavaScriptInterpreter) LoadScript(scriptPath string) error {
	return nil
}

func (ji *JavaScriptInterpreter) UnloadScript(scriptPath string) error {
	return nil
}
