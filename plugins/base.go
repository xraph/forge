package plugins

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/xraph/forge/core"
	"github.com/xraph/forge/jobs"
	"github.com/xraph/forge/logger"
)

// BasePlugin provides a base implementation for plugins
type BasePlugin struct {
	mu          sync.RWMutex
	metadata    PluginMetadata
	container   core.Container
	config      map[string]interface{}
	enabled     bool
	initialized bool
	started     bool
	lastError   error
	status      PluginStatus
	logger      logger.Logger

	// Lifecycle hooks
	onInitialize func(core.Container) error
	onStart      func(context.Context) error
	onStop       func(context.Context) error

	// Plugin definitions
	routes      []RouteDefinition
	jobs        []JobDefinition
	middleware  []MiddlewareDefinition
	commands    []CommandDefinition
	healthCheck []HealthCheckDefinition

	// Configuration
	configSchema  map[string]interface{}
	defaultConfig map[string]interface{}

	// Dependencies
	dependencies []Dependency

	// Statistics
	requestCount int64
	errorCount   int64
	lastUsed     time.Time
	lastErrorAt  time.Time
	loadTime     time.Duration
	startTime    time.Duration
}

// NewBasePlugin creates a new base plugin
func NewBasePlugin(metadata PluginMetadata) *BasePlugin {
	return &BasePlugin{
		metadata:      metadata,
		config:        make(map[string]interface{}),
		configSchema:  make(map[string]interface{}),
		defaultConfig: make(map[string]interface{}),
		status: PluginStatus{
			Enabled:     false,
			Initialized: false,
			Started:     false,
			Health:      string(HealthStatusUnknown),
		},
		logger: logger.GetGlobalLogger().Named(fmt.Sprintf("plugin.%s", metadata.Name)),
	}
}

// Plugin interface implementation

// Name returns the plugin name
func (p *BasePlugin) Name() string {
	return p.metadata.Name
}

// Version returns the plugin version
func (p *BasePlugin) Version() string {
	return p.metadata.Version
}

// Description returns the plugin description
func (p *BasePlugin) Description() string {
	return p.metadata.Description
}

// Author returns the plugin author
func (p *BasePlugin) Author() string {
	return p.metadata.Author
}

// Initialize initializes the plugin
func (p *BasePlugin) Initialize(container core.Container) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.initialized {
		return nil
	}

	start := time.Now()

	p.container = container
	p.logger = p.logger.Named(fmt.Sprintf("plugin.%s", p.metadata.Name))

	// Apply default configuration
	p.applyDefaultConfig()

	// Call custom initialization hook
	if p.onInitialize != nil {
		if err := p.onInitialize(container); err != nil {
			p.lastError = err
			p.lastErrorAt = time.Now()
			p.updateStatus()
			return fmt.Errorf("initialization hook failed: %w", err)
		}
	}

	p.initialized = true
	p.loadTime = time.Since(start)
	p.updateStatus()

	p.logger.Info("Plugin initialized successfully",
		logger.Duration("load_time", p.loadTime),
	)

	return nil
}

// Start starts the plugin
func (p *BasePlugin) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.initialized {
		return fmt.Errorf("plugin not initialized")
	}

	if p.started {
		return nil
	}

	start := time.Now()

	// Call custom start hook
	if p.onStart != nil {
		if err := p.onStart(ctx); err != nil {
			p.lastError = err
			p.lastErrorAt = time.Now()
			p.updateStatus()
			return fmt.Errorf("start hook failed: %w", err)
		}
	}

	p.started = true
	p.startTime = time.Since(start)
	p.updateStatus()

	p.logger.Info("Plugin started successfully",
		logger.Duration("start_time", p.startTime),
	)

	return nil
}

// Stop stops the plugin
func (p *BasePlugin) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.started {
		return nil
	}

	// Call custom stop hook
	if p.onStop != nil {
		if err := p.onStop(ctx); err != nil {
			p.lastError = err
			p.lastErrorAt = time.Now()
			p.logger.Error("Stop hook failed", logger.Error(err))
			// Don't return error for stop, just log it
		}
	}

	p.started = false
	p.updateStatus()

	p.logger.Info("Plugin stopped successfully")

	return nil
}

// Routes returns plugin routes
func (p *BasePlugin) Routes() []RouteDefinition {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.routes
}

// Jobs returns plugin jobs
func (p *BasePlugin) Jobs() []JobDefinition {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.jobs
}

// Middleware returns plugin middleware
func (p *BasePlugin) Middleware() []MiddlewareDefinition {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.middleware
}

// Commands returns plugin commands
func (p *BasePlugin) Commands() []CommandDefinition {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.commands
}

// HealthChecks returns plugin health checks
func (p *BasePlugin) HealthChecks() []HealthCheckDefinition {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.healthCheck
}

// ConfigSchema returns the configuration schema
func (p *BasePlugin) ConfigSchema() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.configSchema
}

// DefaultConfig returns the default configuration
func (p *BasePlugin) DefaultConfig() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.defaultConfig
}

// ValidateConfig validates the provided configuration
func (p *BasePlugin) ValidateConfig(config map[string]interface{}) error {
	// Basic validation against schema
	// In a real implementation, this would use a JSON schema validator
	for key := range config {
		if _, exists := p.configSchema[key]; !exists {
			return fmt.Errorf("unknown configuration key: %s", key)
		}
	}

	// Check required fields
	for key, schema := range p.configSchema {
		if schemaMap, ok := schema.(map[string]interface{}); ok {
			if required, exists := schemaMap["required"]; exists && required.(bool) {
				if _, exists := config[key]; !exists {
					return fmt.Errorf("required configuration key missing: %s", key)
				}
			}
		}
	}

	return nil
}

// Dependencies returns plugin dependencies
func (p *BasePlugin) Dependencies() []Dependency {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.dependencies
}

// IsEnabled returns whether the plugin is enabled
func (p *BasePlugin) IsEnabled() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.enabled
}

// IsInitialized returns whether the plugin is initialized
func (p *BasePlugin) IsInitialized() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.initialized
}

// Status returns the plugin status
func (p *BasePlugin) Status() PluginStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.status
}

// Configuration methods

// SetConfig sets the plugin configuration
func (p *BasePlugin) SetConfig(config map[string]interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.ValidateConfig(config); err != nil {
		return err
	}

	// Merge with defaults
	merged := make(map[string]interface{})
	for k, v := range p.defaultConfig {
		merged[k] = v
	}
	for k, v := range config {
		merged[k] = v
	}

	p.config = merged
	p.updateStatus()

	p.logger.Info("Configuration updated")

	return nil
}

// GetConfig returns the current configuration
func (p *BasePlugin) GetConfig() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	config := make(map[string]interface{})
	for k, v := range p.config {
		config[k] = v
	}
	return config
}

// GetConfigValue returns a specific configuration value
func (p *BasePlugin) GetConfigValue(key string) (interface{}, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	value, exists := p.config[key]
	return value, exists
}

// SetConfigValue sets a specific configuration value
func (p *BasePlugin) SetConfigValue(key string, value interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Validate single key
	if _, exists := p.configSchema[key]; !exists {
		return fmt.Errorf("unknown configuration key: %s", key)
	}

	p.config[key] = value
	p.updateStatus()

	return nil
}

// Builder methods for plugin definition

// SetMetadata sets plugin metadata
func (p *BasePlugin) SetMetadata(metadata PluginMetadata) *BasePlugin {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.metadata = metadata
	return p
}

// SetConfigSchema sets the configuration schema
func (p *BasePlugin) SetConfigSchema(schema map[string]interface{}) *BasePlugin {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.configSchema = schema
	return p
}

// SetDefaultConfig sets the default configuration
func (p *BasePlugin) SetDefaultConfig(config map[string]interface{}) *BasePlugin {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.defaultConfig = config
	return p
}

// SetDependencies sets plugin dependencies
func (p *BasePlugin) SetDependencies(deps []Dependency) *BasePlugin {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dependencies = deps
	return p
}

// AddDependency adds a plugin dependency
func (p *BasePlugin) AddDependency(dep Dependency) *BasePlugin {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dependencies = append(p.dependencies, dep)
	return p
}

// Lifecycle hooks

// OnInitialize sets the initialization hook
func (p *BasePlugin) OnInitialize(hook func(core.Container) error) *BasePlugin {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onInitialize = hook
	return p
}

// OnStart sets the start hook
func (p *BasePlugin) OnStart(hook func(context.Context) error) *BasePlugin {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onStart = hook
	return p
}

// OnStop sets the stop hook
func (p *BasePlugin) OnStop(hook func(context.Context) error) *BasePlugin {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onStop = hook
	return p
}

// Route registration

// AddRoute adds a route definition
func (p *BasePlugin) AddRoute(route RouteDefinition) *BasePlugin {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.routes = append(p.routes, route)
	return p
}

// AddRoutes adds multiple route definitions
func (p *BasePlugin) AddRoutes(routes []RouteDefinition) *BasePlugin {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.routes = append(p.routes, routes...)
	return p
}

// GET adds a GET route
func (p *BasePlugin) GET(pattern string, handler http.HandlerFunc) *BasePlugin {
	return p.AddRoute(RouteDefinition{
		Method:  "GET",
		Pattern: pattern,
		Handler: handler,
	})
}

// POST adds a POST route
func (p *BasePlugin) POST(pattern string, handler http.HandlerFunc) *BasePlugin {
	return p.AddRoute(RouteDefinition{
		Method:  "POST",
		Pattern: pattern,
		Handler: handler,
	})
}

// PUT adds a PUT route
func (p *BasePlugin) PUT(pattern string, handler http.HandlerFunc) *BasePlugin {
	return p.AddRoute(RouteDefinition{
		Method:  "PUT",
		Pattern: pattern,
		Handler: handler,
	})
}

// DELETE adds a DELETE route
func (p *BasePlugin) DELETE(pattern string, handler http.HandlerFunc) *BasePlugin {
	return p.AddRoute(RouteDefinition{
		Method:  "DELETE",
		Pattern: pattern,
		Handler: handler,
	})
}

// Job registration

// AddJob adds a job definition
func (p *BasePlugin) AddJob(job JobDefinition) *BasePlugin {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.jobs = append(p.jobs, job)
	return p
}

// AddJobs adds multiple job definitions
func (p *BasePlugin) AddJobs(jobs []JobDefinition) *BasePlugin {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.jobs = append(p.jobs, jobs...)
	return p
}

// AddScheduledJob adds a scheduled job
func (p *BasePlugin) AddScheduledJob(name, schedule string, handler jobs.JobHandlerFunc) *BasePlugin {
	return p.AddJob(JobDefinition{
		Name:     name,
		Type:     "scheduled",
		Handler:  handler,
		Schedule: schedule,
	})
}

// Middleware registration

// AddMiddleware adds a middleware definition
func (p *BasePlugin) AddMiddleware(middleware MiddlewareDefinition) *BasePlugin {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.middleware = append(p.middleware, middleware)
	return p
}

// AddMiddlewareFunc adds a middleware function
func (p *BasePlugin) AddMiddlewareFunc(name string, handler func(http.Handler) http.Handler) *BasePlugin {
	return p.AddMiddleware(MiddlewareDefinition{
		Name:    name,
		Handler: handler,
	})
}

// Command registration

// AddCommand adds a command definition
func (p *BasePlugin) AddCommand(command CommandDefinition) *BasePlugin {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.commands = append(p.commands, command)
	return p
}

// AddCommands adds multiple command definitions
func (p *BasePlugin) AddCommands(commands []CommandDefinition) *BasePlugin {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.commands = append(p.commands, commands...)
	return p
}

// Health check registration

// AddHealthCheck adds a health check definition
func (p *BasePlugin) AddHealthCheck(check HealthCheckDefinition) *BasePlugin {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.healthCheck = append(p.healthCheck, check)
	return p
}

// AddHealthCheckFunc adds a health check function
func (p *BasePlugin) AddHealthCheckFunc(name string, handler HealthCheckHandler) *BasePlugin {
	return p.AddHealthCheck(HealthCheckDefinition{
		Name:    name,
		Handler: handler,
	})
}

// Utility methods

// GetLogger returns the plugin logger
func (p *BasePlugin) GetLogger() logger.Logger {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.logger
}

// GetContainer returns the plugin container
func (p *BasePlugin) GetContainer() core.Container {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.container
}

// IncrementRequestCount increments the request counter
func (p *BasePlugin) IncrementRequestCount() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requestCount++
	p.lastUsed = time.Now()
	p.updateStatus()
}

// IncrementErrorCount increments the error counter
func (p *BasePlugin) IncrementErrorCount() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.errorCount++
	p.lastErrorAt = time.Now()
	p.updateStatus()
}

// SetLastError sets the last error
func (p *BasePlugin) SetLastError(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastError = err
	p.lastErrorAt = time.Now()
	p.updateStatus()
}

// GetMetadata returns plugin metadata
func (p *BasePlugin) GetMetadata() PluginMetadata {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.metadata
}

// GetStatistics returns plugin statistics
func (p *BasePlugin) GetStatistics() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return map[string]interface{}{
		"request_count": p.requestCount,
		"error_count":   p.errorCount,
		"last_used":     p.lastUsed,
		"last_error":    p.lastErrorAt,
		"load_time":     p.loadTime,
		"start_time":    p.startTime,
		"enabled":       p.enabled,
		"initialized":   p.initialized,
		"started":       p.started,
	}
}

// Private methods

func (p *BasePlugin) applyDefaultConfig() {
	if len(p.defaultConfig) == 0 {
		return
	}

	// Apply defaults for missing keys
	for key, value := range p.defaultConfig {
		if _, exists := p.config[key]; !exists {
			p.config[key] = value
		}
	}
}

func (p *BasePlugin) updateStatus() {
	status := PluginStatus{
		Enabled:     p.enabled,
		Initialized: p.initialized,
		Started:     p.started,
		LoadTime:    p.loadTime,
		StartTime:   p.startTime,
		Health:      string(HealthStatusUnknown),
	}

	// Determine health status
	if p.lastError != nil {
		status.Error = p.lastError.Error()
		status.Health = string(HealthStatusDown)
		status.LastError = p.lastErrorAt
	} else if p.started {
		status.Health = string(HealthStatusUp)
	} else if p.initialized {
		status.Health = string(HealthStatusWarning)
	}

	// Add statistics
	status.RequestCount = p.requestCount
	status.ErrorCount = p.errorCount
	status.LastUsed = p.lastUsed

	// Add metadata
	status.Metadata = map[string]interface{}{
		"version":     p.metadata.Version,
		"author":      p.metadata.Author,
		"description": p.metadata.Description,
		"category":    p.metadata.Category,
	}

	p.status = status
}

// Specialized plugin types

// HTTPPlugin provides HTTP-specific functionality
type HTTPPlugin struct {
	*BasePlugin
	prefix string
}

// NewHTTPPlugin creates a new HTTP plugin
func NewHTTPPlugin(metadata PluginMetadata, prefix string) *HTTPPlugin {
	return &HTTPPlugin{
		BasePlugin: NewBasePlugin(metadata),
		prefix:     prefix,
	}
}

// RegisterRoutes returns all routes with prefix
func (p *HTTPPlugin) RegisterRoutes() []RouteDefinition {
	routes := p.Routes()

	// Apply prefix to all routes
	for i := range routes {
		if p.prefix != "" && routes[i].Pattern != "" {
			routes[i].Pattern = p.prefix + routes[i].Pattern
		}
	}

	return routes
}

// RegisterMiddleware returns all middleware
func (p *HTTPPlugin) RegisterMiddleware() []MiddlewareDefinition {
	return p.Middleware()
}

// JobPlugin provides job-specific functionality
type JobPlugin struct {
	*BasePlugin
	queue string
}

// NewJobPlugin creates a new job plugin
func NewJobPlugin(metadata PluginMetadata, queue string) *JobPlugin {
	return &JobPlugin{
		BasePlugin: NewBasePlugin(metadata),
		queue:      queue,
	}
}

// RegisterJobs returns all jobs with queue
func (p *JobPlugin) RegisterJobs() []JobDefinition {
	jobs := p.Jobs()

	// Apply queue to all jobs
	for i := range jobs {
		if p.queue != "" && jobs[i].Queue == "" {
			jobs[i].Queue = p.queue
		}
	}

	return jobs
}

// RegisterSchedules returns scheduled jobs
func (p *JobPlugin) RegisterSchedules() []ScheduleDefinition {
	var schedules []ScheduleDefinition

	for _, job := range p.Jobs() {
		if job.Schedule != "" {
			schedules = append(schedules, ScheduleDefinition{
				Name:     job.Name,
				Schedule: job.Schedule,
				Job:      job,
				Enabled:  true,
			})
		}
	}

	return schedules
}

// Configuration builder helpers

// ConfigField represents a configuration field definition
type ConfigField struct {
	Type        string      `json:"type"`
	Description string      `json:"description"`
	Default     interface{} `json:"default,omitempty"`
	Required    bool        `json:"required,omitempty"`
	Enum        []string    `json:"enum,omitempty"`
	Minimum     *float64    `json:"minimum,omitempty"`
	Maximum     *float64    `json:"maximum,omitempty"`
}

// AddConfigField adds a configuration field to the schema
func (p *BasePlugin) AddConfigField(name string, field ConfigField) *BasePlugin {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.configSchema == nil {
		p.configSchema = make(map[string]interface{})
	}

	p.configSchema[name] = map[string]interface{}{
		"type":        field.Type,
		"description": field.Description,
		"required":    field.Required,
	}

	if field.Default != nil {
		if p.defaultConfig == nil {
			p.defaultConfig = make(map[string]interface{})
		}
		p.defaultConfig[name] = field.Default
	}

	if len(field.Enum) > 0 {
		p.configSchema[name].(map[string]interface{})["enum"] = field.Enum
	}

	if field.Minimum != nil {
		p.configSchema[name].(map[string]interface{})["minimum"] = *field.Minimum
	}

	if field.Maximum != nil {
		p.configSchema[name].(map[string]interface{})["maximum"] = *field.Maximum
	}

	return p
}

// AddStringConfig adds a string configuration field
func (p *BasePlugin) AddStringConfig(name, description string, defaultValue string, required bool) *BasePlugin {
	return p.AddConfigField(name, ConfigField{
		Type:        "string",
		Description: description,
		Default:     defaultValue,
		Required:    required,
	})
}

// AddIntConfig adds an integer configuration field
func (p *BasePlugin) AddIntConfig(name, description string, defaultValue int, required bool) *BasePlugin {
	return p.AddConfigField(name, ConfigField{
		Type:        "integer",
		Description: description,
		Default:     defaultValue,
		Required:    required,
	})
}

// AddBoolConfig adds a boolean configuration field
func (p *BasePlugin) AddBoolConfig(name, description string, defaultValue bool, required bool) *BasePlugin {
	return p.AddConfigField(name, ConfigField{
		Type:        "boolean",
		Description: description,
		Default:     defaultValue,
		Required:    required,
	})
}
