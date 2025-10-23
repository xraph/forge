package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"plugin"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/logger"
)

// CLIPlugin defines the interface for CLI plugins
type CLIPlugin interface {
	Name() string
	Description() string
	Version() string
	Commands() []*Command
	Middleware() []CLIMiddleware
	Initialize(app CLIApp) error
	Cleanup() error
}

// PluginInfo contains metadata about a plugin
type PluginInfo struct {
	Name         string            `json:"name" yaml:"name"`
	Description  string            `json:"description" yaml:"description"`
	Version      string            `json:"version" yaml:"version"`
	Author       string            `json:"author" yaml:"author"`
	License      string            `json:"license" yaml:"license"`
	Homepage     string            `json:"homepage" yaml:"homepage"`
	Commands     []string          `json:"commands" yaml:"commands"`
	Dependencies []string          `json:"dependencies" yaml:"dependencies"`
	Metadata     map[string]string `json:"metadata" yaml:"metadata"`
	LoadTime     time.Time         `json:"load_time" yaml:"load_time"`
	LoadDuration time.Duration     `json:"load_duration" yaml:"load_duration"`
}

// PluginManager manages CLI plugins
type PluginManager struct {
	plugins    map[string]CLIPlugin
	pluginInfo map[string]*PluginInfo
	registry   *PluginRegistry
	loader     *PluginLoader
	config     PluginManagerConfig
	app        CLIApp
	mutex      sync.RWMutex
}

// PluginManagerConfig contains plugin manager configuration
type PluginManagerConfig struct {
	PluginDirs      []string      `yaml:"plugin_dirs" json:"plugin_dirs"`
	AutoLoad        bool          `yaml:"auto_load" json:"auto_load"`
	PluginTimeout   time.Duration `yaml:"plugin_timeout" json:"plugin_timeout"`
	EnableHotReload bool          `yaml:"enable_hot_reload" json:"enable_hot_reload"`
	AllowUnsafe     bool          `yaml:"allow_unsafe" json:"allow_unsafe"`
}

// NewPluginManager creates a new plugin manager
func NewPluginManager(app CLIApp, config PluginManagerConfig) *PluginManager {
	return &PluginManager{
		plugins:    make(map[string]CLIPlugin),
		pluginInfo: make(map[string]*PluginInfo),
		registry:   NewPluginRegistry(),
		loader:     NewPluginLoader(config.PluginDirs),
		config:     config,
		app:        app,
	}
}

// LoadPlugin loads a plugin by name or path
func (pm *PluginManager) LoadPlugin(nameOrPath string) error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	start := time.Now()

	// Check if already loaded
	if _, exists := pm.plugins[nameOrPath]; exists {
		return fmt.Errorf("plugin '%s' already loaded", nameOrPath)
	}

	// Load the plugin
	plugin, err := pm.loader.Load(nameOrPath)
	if err != nil {
		return fmt.Errorf("failed to load plugin '%s': %w", nameOrPath, err)
	}

	// Initialize plugin
	if err := plugin.Initialize(pm.app); err != nil {
		return fmt.Errorf("failed to initialize plugin '%s': %w", plugin.Name(), err)
	}

	// Store plugin and info
	pm.plugins[plugin.Name()] = plugin
	pm.pluginInfo[plugin.Name()] = &PluginInfo{
		Name:         plugin.Name(),
		Description:  plugin.Description(),
		Version:      plugin.Version(),
		Commands:     pm.extractCommandNames(plugin.Commands()),
		LoadTime:     time.Now(),
		LoadDuration: time.Since(start),
	}

	// Register with registry
	pm.registry.Register(plugin.Name(), pm.pluginInfo[plugin.Name()])

	if pm.app.Logger() != nil {
		pm.app.Logger().Info("plugin loaded",
			logger.String("plugin", plugin.Name()),
			logger.String("version", plugin.Version()),
			logger.Duration("load_duration", time.Since(start)),
			logger.Int("commands", len(plugin.Commands())),
			logger.Int("middleware", len(plugin.Middleware())),
		)
	}

	return nil
}

// UnloadPlugin unloads a plugin by name
func (pm *PluginManager) UnloadPlugin(name string) error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	plugin, exists := pm.plugins[name]
	if !exists {
		return fmt.Errorf("plugin '%s' not found", name)
	}

	// Cleanup plugin
	if err := plugin.Cleanup(); err != nil {
		return fmt.Errorf("failed to cleanup plugin '%s': %w", name, err)
	}

	// Remove from maps
	delete(pm.plugins, name)
	delete(pm.pluginInfo, name)

	// Unregister from registry
	pm.registry.Unregister(name)

	if pm.app.Logger() != nil {
		pm.app.Logger().Info("plugin unloaded", logger.String("plugin", name))
	}

	return nil
}

// GetPlugin returns a plugin by name
func (pm *PluginManager) GetPlugin(name string) (CLIPlugin, error) {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	plugin, exists := pm.plugins[name]
	if !exists {
		return nil, fmt.Errorf("plugin '%s' not found", name)
	}

	return plugin, nil
}

// ListPlugins returns all loaded plugins
func (pm *PluginManager) ListPlugins() map[string]*PluginInfo {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	result := make(map[string]*PluginInfo)
	for name, info := range pm.pluginInfo {
		result[name] = info
	}

	return result
}

// LoadAllPlugins loads all plugins from configured directories
func (pm *PluginManager) LoadAllPlugins() error {
	if !pm.config.AutoLoad {
		return nil
	}

	plugins, err := pm.loader.Discover()
	if err != nil {
		return fmt.Errorf("failed to discover plugins: %w", err)
	}

	for _, pluginPath := range plugins {
		if err := pm.LoadPlugin(pluginPath); err != nil {
			if pm.app.Logger() != nil {
				pm.app.Logger().Warn("failed to load plugin",
					logger.String("path", pluginPath),
					logger.Error(err),
				)
			}
		}
	}

	return nil
}

// extractCommandNames extracts command names from commands
func (pm *PluginManager) extractCommandNames(commands []*Command) []string {
	names := make([]string, len(commands))
	for i, cmd := range commands {
		names[i] = cmd.Use
	}
	return names
}

// PluginRegistry maintains a registry of available plugins
type PluginRegistry struct {
	plugins map[string]*PluginInfo
	mutex   sync.RWMutex
}

// NewPluginRegistry creates a new plugin registry
func NewPluginRegistry() *PluginRegistry {
	return &PluginRegistry{
		plugins: make(map[string]*PluginInfo),
	}
}

// Register registers a plugin in the registry
func (pr *PluginRegistry) Register(name string, info *PluginInfo) {
	pr.mutex.Lock()
	defer pr.mutex.Unlock()
	pr.plugins[name] = info
}

// Unregister removes a plugin from the registry
func (pr *PluginRegistry) Unregister(name string) {
	pr.mutex.Lock()
	defer pr.mutex.Unlock()
	delete(pr.plugins, name)
}

// Get returns plugin info by name
func (pr *PluginRegistry) Get(name string) (*PluginInfo, error) {
	pr.mutex.RLock()
	defer pr.mutex.RUnlock()

	info, exists := pr.plugins[name]
	if !exists {
		return nil, fmt.Errorf("plugin '%s' not found in registry", name)
	}

	return info, nil
}

// List returns all registered plugins
func (pr *PluginRegistry) List() map[string]*PluginInfo {
	pr.mutex.RLock()
	defer pr.mutex.RUnlock()

	result := make(map[string]*PluginInfo)
	for name, info := range pr.plugins {
		result[name] = info
	}

	return result
}

// PluginLoader loads plugins from files
type PluginLoader struct {
	searchPaths []string
	cache       map[string]CLIPlugin
	mutex       sync.RWMutex
}

// NewPluginLoader creates a new plugin loader
func NewPluginLoader(searchPaths []string) *PluginLoader {
	return &PluginLoader{
		searchPaths: searchPaths,
		cache:       make(map[string]CLIPlugin),
	}
}

// Load loads a plugin by name or path
func (pl *PluginLoader) Load(nameOrPath string) (CLIPlugin, error) {
	pl.mutex.Lock()
	defer pl.mutex.Unlock()

	// Check cache first
	if cached, exists := pl.cache[nameOrPath]; exists {
		return cached, nil
	}

	// Determine if it's a path or name
	var pluginPath string
	if filepath.IsAbs(nameOrPath) || filepath.Ext(nameOrPath) == ".so" {
		pluginPath = nameOrPath
	} else {
		// Search for plugin in search paths
		found, err := pl.findPlugin(nameOrPath)
		if err != nil {
			return nil, err
		}
		pluginPath = found
	}

	// Load the plugin
	loadedPlugin, err := pl.loadFromFile(pluginPath)
	if err != nil {
		return nil, err
	}

	// Cache the plugin
	pl.cache[nameOrPath] = loadedPlugin

	return loadedPlugin, nil
}

// Discover discovers all available plugins in search paths
func (pl *PluginLoader) Discover() ([]string, error) {
	var plugins []string

	for _, searchPath := range pl.searchPaths {
		matches, err := filepath.Glob(filepath.Join(searchPath, "*.so"))
		if err != nil {
			continue
		}
		plugins = append(plugins, matches...)
	}

	return plugins, nil
}

// findPlugin finds a plugin by name in search paths
func (pl *PluginLoader) findPlugin(name string) (string, error) {
	for _, searchPath := range pl.searchPaths {
		pluginPath := filepath.Join(searchPath, name+".so")
		if _, err := os.Stat(pluginPath); err == nil {
			return pluginPath, nil
		}
	}

	return "", fmt.Errorf("plugin '%s' not found in search paths", name)
}

// loadFromFile loads a plugin from a shared library file
func (pl *PluginLoader) loadFromFile(path string) (CLIPlugin, error) {
	// Load the plugin
	p, err := plugin.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open plugin '%s': %w", path, err)
	}

	// Look for the plugin constructor
	sym, err := p.Lookup("NewPlugin")
	if err != nil {
		return nil, fmt.Errorf("plugin '%s' does not export NewPlugin function: %w", path, err)
	}

	// Verify the constructor signature
	constructor, ok := sym.(func() CLIPlugin)
	if !ok {
		return nil, fmt.Errorf("plugin '%s' NewPlugin function has invalid signature", path)
	}

	// Create the plugin instance
	pluginInstance := constructor()
	if pluginInstance == nil {
		return nil, fmt.Errorf("plugin '%s' NewPlugin returned nil", path)
	}

	return pluginInstance, nil
}

// BasePlugin provides a base implementation for plugins
type BasePlugin struct {
	name        string
	description string
	version     string
	commands    []*Command
	middleware  []CLIMiddleware
	initialized bool
}

// NewBasePlugin creates a new base plugin
func NewBasePlugin(name, description, version string) *BasePlugin {
	return &BasePlugin{
		name:        name,
		description: description,
		version:     version,
		commands:    make([]*Command, 0),
		middleware:  make([]CLIMiddleware, 0),
	}
}

// Name returns the plugin name
func (bp *BasePlugin) Name() string {
	return bp.name
}

// Description returns the plugin description
func (bp *BasePlugin) Description() string {
	return bp.description
}

// Version returns the plugin version
func (bp *BasePlugin) Version() string {
	return bp.version
}

// Commands returns the plugin commands
func (bp *BasePlugin) Commands() []*Command {
	return bp.commands
}

// Middleware returns the plugin middleware
func (bp *BasePlugin) Middleware() []CLIMiddleware {
	return bp.middleware
}

// AddCommand adds a command to the plugin
func (bp *BasePlugin) AddCommand(cmd *Command) {
	bp.commands = append(bp.commands, cmd)
}

// AddMiddleware adds middleware to the plugin
func (bp *BasePlugin) AddMiddleware(mw CLIMiddleware) {
	bp.middleware = append(bp.middleware, mw)
}

// Initialize initializes the plugin
func (bp *BasePlugin) Initialize(app CLIApp) error {
	if bp.initialized {
		return fmt.Errorf("plugin '%s' already initialized", bp.name)
	}

	bp.initialized = true
	return nil
}

// Cleanup cleans up the plugin
func (bp *BasePlugin) Cleanup() error {
	bp.initialized = false
	return nil
}

// IsInitialized returns whether the plugin is initialized
func (bp *BasePlugin) IsInitialized() bool {
	return bp.initialized
}

// FunctionalPlugin allows creating plugins from functions
type FunctionalPlugin struct {
	*BasePlugin
	initFunc    func(app CLIApp) error
	cleanupFunc func() error
}

// NewFunctionalPlugin creates a plugin from functions
func NewFunctionalPlugin(name, description, version string, initFunc func(app CLIApp) error, cleanupFunc func() error) *FunctionalPlugin {
	return &FunctionalPlugin{
		BasePlugin:  NewBasePlugin(name, description, version),
		initFunc:    initFunc,
		cleanupFunc: cleanupFunc,
	}
}

// Initialize initializes the functional plugin
func (fp *FunctionalPlugin) Initialize(app CLIApp) error {
	if err := fp.BasePlugin.Initialize(app); err != nil {
		return err
	}

	if fp.initFunc != nil {
		return fp.initFunc(app)
	}

	return nil
}

// Cleanup cleans up the functional plugin
func (fp *FunctionalPlugin) Cleanup() error {
	if fp.cleanupFunc != nil {
		if err := fp.cleanupFunc(); err != nil {
			return err
		}
	}

	return fp.BasePlugin.Cleanup()
}

// PluginBuilder helps build plugins
type PluginBuilder struct {
	plugin *BasePlugin
}

// NewPluginBuilder creates a new plugin builder
func NewPluginBuilder(name, description, version string) *PluginBuilder {
	return &PluginBuilder{
		plugin: NewBasePlugin(name, description, version),
	}
}

// WithCommand adds a command to the plugin
func (pb *PluginBuilder) WithCommand(cmd *Command) *PluginBuilder {
	pb.plugin.AddCommand(cmd)
	return pb
}

// WithMiddleware adds middleware to the plugin
func (pb *PluginBuilder) WithMiddleware(mw CLIMiddleware) *PluginBuilder {
	pb.plugin.AddMiddleware(mw)
	return pb
}

// Build builds the plugin
func (pb *PluginBuilder) Build() CLIPlugin {
	return pb.plugin
}

// Example database plugin implementation
type DatabasePlugin struct {
	*BasePlugin
	app CLIApp
}

// NewDatabasePlugin creates a new database plugin
func NewDatabasePlugin() *DatabasePlugin {
	plugin := &DatabasePlugin{
		BasePlugin: NewBasePlugin("database", "Database management commands", "1.0.0"),
	}

	plugin.setupCommands()
	return plugin
}

// setupCommands sets up the database commands
func (dp *DatabasePlugin) setupCommands() {
	// Migration command
	migrateCmd := NewCommand("db migrate", "Run database migrations").
		WithLong("Run all pending database migrations").
		WithService((*MigrationService)(nil))

	migrateCmd.Run = func(ctx CLIContext, args []string) error {
		var migrationService MigrationService
		if err := ctx.Resolve(&migrationService); err != nil {
			return err
		}

		spinner := ctx.Spinner("Running migrations...")
		defer spinner.Stop()

		err := migrationService.Migrate(context.Background())
		if err != nil {
			ctx.ErrorMsg("Migration failed")
			return err
		}

		ctx.Success("Migrations completed successfully")
		return nil
	}

	// Status command
	statusCmd := NewCommand("db status", "Show migration status").
		WithLong("Show the status of all database migrations").
		WithService((*MigrationService)(nil)).
		WithFlags(
			StringFlag("output", "o", "Output format (json, yaml, table)", false).WithDefault("table"),
		)

	statusCmd.Run = func(ctx CLIContext, args []string) error {
		var migrationService MigrationService
		if err := ctx.Resolve(&migrationService); err != nil {
			return err
		}

		status, err := migrationService.GetStatus(context.Background())
		if err != nil {
			return err
		}

		return ctx.OutputData(status)
	}

	dp.AddCommand(migrateCmd)
	dp.AddCommand(statusCmd)
}

// Initialize initializes the database plugin
func (dp *DatabasePlugin) Initialize(app CLIApp) error {
	if err := dp.BasePlugin.Initialize(app); err != nil {
		return err
	}

	dp.app = app
	return nil
}

// MigrationService interface for dependency injection
type MigrationService interface {
	Migrate(ctx context.Context) error
	GetStatus(ctx context.Context) (interface{}, error)
}
