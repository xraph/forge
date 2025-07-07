package plugins

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/xraph/forge/jobs"
	"github.com/xraph/forge/logger"
	"github.com/xraph/forge/router"
)

// Integrator handles integration between plugins and framework components
type Integrator interface {
	// Router integration
	RegisterRoutes(router router.Router) error
	UnregisterRoutes(router router.Router, pluginName string) error

	// Middleware integration
	RegisterMiddleware(router router.Router) error
	UnregisterMiddleware(router router.Router, pluginName string) error

	// Job system integration
	RegisterJobs(jobManager jobs.Processor) error
	UnregisterJobs(jobManager jobs.Processor, pluginName string) error

	// Command integration
	RegisterCommands(rootCmd *cobra.Command) error
	UnregisterCommands(rootCmd *cobra.Command, pluginName string) error

	// Health check integration
	RegisterHealthChecks(healthChecker interface{}) error
	UnregisterHealthChecks(healthChecker interface{}, pluginName string) error

	// Event integration
	SubscribeToEvents(eventBus EventBus) error
	UnsubscribeFromEvents(eventBus EventBus, pluginName string) error

	// Lifecycle management
	OnPluginEnabled(plugin Plugin) error
	OnPluginDisabled(plugin Plugin) error
	OnPluginReloaded(plugin Plugin) error

	// Integration status
	GetIntegrationStatus() IntegrationStatus
	ValidateIntegrations() []IntegrationError
}

// IntegrationStatus represents the status of plugin integrations
type IntegrationStatus struct {
	Routes        int                          `json:"routes"`
	Middleware    int                          `json:"middleware"`
	Jobs          int                          `json:"jobs"`
	Commands      int                          `json:"commands"`
	HealthChecks  int                          `json:"health_checks"`
	EventHandlers int                          `json:"event_handlers"`
	Errors        []IntegrationError           `json:"errors"`
	PluginStatus  map[string]PluginIntegration `json:"plugin_status"`
}

// PluginIntegration represents integration status for a single plugin
type PluginIntegration struct {
	Plugin       string    `json:"plugin"`
	Enabled      bool      `json:"enabled"`
	Routes       []string  `json:"routes"`
	Middleware   []string  `json:"middleware"`
	Jobs         []string  `json:"jobs"`
	Commands     []string  `json:"commands"`
	HealthChecks []string  `json:"health_checks"`
	LastUpdate   time.Time `json:"last_update"`
	Errors       []string  `json:"errors"`
}

// IntegrationError represents an integration error
type IntegrationError struct {
	Plugin    string    `json:"plugin"`
	Component string    `json:"component"` // route, job, middleware, etc.
	Type      string    `json:"type"`      // registration, conflict, validation
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// integrator implements the Integrator interface
type integrator struct {
	manager Manager
	logger  logger.Logger
	mu      sync.RWMutex

	// Integration tracking
	routes        map[string][]RouteRegistration
	middleware    map[string][]MiddlewareRegistration
	jobs          map[string][]JobRegistration
	commands      map[string][]CommandRegistration
	healthChecks  map[string][]HealthCheckRegistration
	eventHandlers map[string][]EventHandlerRegistration

	// Error tracking
	errors []IntegrationError

	// Configuration
	config IntegratorConfig
}

// Registration types for tracking
type RouteRegistration struct {
	Plugin     string          `json:"plugin"`
	Route      RouteDefinition `json:"route"`
	Registered time.Time       `json:"registered"`
	Active     bool            `json:"active"`
}

type MiddlewareRegistration struct {
	Plugin     string               `json:"plugin"`
	Middleware MiddlewareDefinition `json:"middleware"`
	Registered time.Time            `json:"registered"`
	Active     bool                 `json:"active"`
	Priority   int                  `json:"priority"`
}

type JobRegistration struct {
	Plugin     string        `json:"plugin"`
	Job        JobDefinition `json:"job"`
	Registered time.Time     `json:"registered"`
	Active     bool          `json:"active"`
	JobID      string        `json:"job_id,omitempty"`
}

type CommandRegistration struct {
	Plugin     string            `json:"plugin"`
	Command    CommandDefinition `json:"command"`
	Registered time.Time         `json:"registered"`
	Active     bool              `json:"active"`
}

type HealthCheckRegistration struct {
	Plugin     string                `json:"plugin"`
	Check      HealthCheckDefinition `json:"check"`
	Registered time.Time             `json:"registered"`
	Active     bool                  `json:"active"`
}

type EventHandlerRegistration struct {
	Plugin     string    `json:"plugin"`
	EventType  string    `json:"event_type"`
	HandlerID  string    `json:"handler_id"`
	Registered time.Time `json:"registered"`
	Active     bool      `json:"active"`
}

// IntegratorConfig configures the integrator
type IntegratorConfig struct {
	AutoRegister       bool          `mapstructure:"auto_register" yaml:"auto_register"`
	ValidateOnRegister bool          `mapstructure:"validate_on_register" yaml:"validate_on_register"`
	ConflictResolution string        `mapstructure:"conflict_resolution" yaml:"conflict_resolution"` // error, override, merge
	RoutePrefix        string        `mapstructure:"route_prefix" yaml:"route_prefix"`
	MiddlewarePriority int           `mapstructure:"middleware_priority" yaml:"middleware_priority"`
	HealthCheckTimeout time.Duration `mapstructure:"health_check_timeout" yaml:"health_check_timeout"`
}

// NewIntegrator creates a new plugin integrator
func NewIntegrator(manager Manager, config IntegratorConfig) Integrator {
	if config.HealthCheckTimeout == 0 {
		config.HealthCheckTimeout = 30 * time.Second
	}

	integrator := &integrator{
		manager:       manager,
		logger:        logger.GetGlobalLogger().Named("plugin-integrator"),
		config:        config,
		routes:        make(map[string][]RouteRegistration),
		middleware:    make(map[string][]MiddlewareRegistration),
		jobs:          make(map[string][]JobRegistration),
		commands:      make(map[string][]CommandRegistration),
		healthChecks:  make(map[string][]HealthCheckRegistration),
		eventHandlers: make(map[string][]EventHandlerRegistration),
		errors:        make([]IntegrationError, 0),
	}

	// Subscribe to plugin events for auto-integration
	if config.AutoRegister {
		integrator.setupAutoIntegration()
	}

	return integrator
}

// Router integration

// RegisterRoutes registers all plugin routes with the router
func (i *integrator) RegisterRoutes(r router.Router) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	plugins := i.manager.ListEnabled()

	for _, plugin := range plugins {
		if err := i.registerPluginRoutes(r, plugin); err != nil {
			i.recordError(plugin.Name(), "route", "registration", err.Error())
			i.logger.Error("Failed to register plugin routes",
				logger.String("plugin", plugin.Name()),
				logger.Error(err),
			)
			continue
		}
	}

	i.logger.Info("Plugin routes registered",
		logger.Int("plugins", len(plugins)),
		logger.Int("total_routes", i.getTotalRoutes()),
	)

	return nil
}

// UnregisterRoutes removes all routes for a specific plugin
func (i *integrator) UnregisterRoutes(r router.Router, pluginName string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	registrations, exists := i.routes[pluginName]
	if !exists {
		return nil
	}

	// Mark routes as inactive
	for idx := range registrations {
		registrations[idx].Active = false
	}

	i.routes[pluginName] = registrations

	i.logger.Info("Plugin routes unregistered",
		logger.String("plugin", pluginName),
		logger.Int("routes", len(registrations)),
	)

	return nil
}

func (i *integrator) registerPluginRoutes(r router.Router, plugin Plugin) error {
	pluginName := plugin.Name()
	routes := plugin.Routes()

	var registrations []RouteRegistration

	for _, route := range routes {
		// Apply route prefix if configured
		pattern := route.Pattern
		if i.config.RoutePrefix != "" {
			pattern = strings.TrimSuffix(i.config.RoutePrefix, "/") + "/" + strings.TrimPrefix(pattern, "/")
		}

		// Validate route
		if i.config.ValidateOnRegister {
			if err := i.validateRoute(route); err != nil {
				return fmt.Errorf("route validation failed for %s %s: %w", route.Method, pattern, err)
			}
		}

		// Check for conflicts
		if err := i.checkRouteConflict(route, pluginName); err != nil {
			if i.config.ConflictResolution == "error" {
				return fmt.Errorf("route conflict: %w", err)
			}
			i.logger.Warn("Route conflict detected",
				logger.String("plugin", pluginName),
				logger.String("route", fmt.Sprintf("%s %s", route.Method, pattern)),
				logger.Error(err),
			)
		}

		// Create wrapped handler with plugin context
		wrappedHandler := i.wrapRouteHandler(plugin, route)

		// Register route with router
		switch strings.ToUpper(route.Method) {
		case "GET":
			r.Get(pattern, wrappedHandler)
		case "POST":
			r.Post(pattern, wrappedHandler)
		case "PUT":
			r.Put(pattern, wrappedHandler)
		case "PATCH":
			r.Patch(pattern, wrappedHandler)
		case "DELETE":
			r.Delete(pattern, wrappedHandler)
		case "HEAD":
			r.Head(pattern, wrappedHandler)
		case "OPTIONS":
			r.Options(pattern, wrappedHandler)
		default:
			r.Method(route.Method, pattern, wrappedHandler)
		}

		// Track registration
		registration := RouteRegistration{
			Plugin:     pluginName,
			Route:      route,
			Registered: time.Now(),
			Active:     true,
		}
		registrations = append(registrations, registration)

		i.logger.Debug("Route registered",
			logger.String("plugin", pluginName),
			logger.String("method", route.Method),
			logger.String("pattern", pattern),
		)
	}

	i.routes[pluginName] = registrations
	return nil
}

func (i *integrator) wrapRouteHandler(plugin Plugin, route RouteDefinition) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Add plugin context
		ctx := context.WithValue(r.Context(), "plugin", plugin.Name())
		ctx = context.WithValue(ctx, "plugin_route", route.Name)
		r = r.WithContext(ctx)

		// Track request metrics
		start := time.Now()
		defer func() {
			duration := time.Since(start)
			i.logger.Debug("Plugin route handled",
				logger.String("plugin", plugin.Name()),
				logger.String("method", r.Method),
				logger.String("path", r.URL.Path),
				logger.Duration("duration", duration),
			)

			// Update plugin statistics if available
			if basePlugin, ok := plugin.(*BasePlugin); ok {
				basePlugin.IncrementRequestCount()
			}
		}()

		// Handle panics
		defer func() {
			if rec := recover(); rec != nil {
				i.logger.Error("Plugin route panicked",
					logger.String("plugin", plugin.Name()),
					logger.String("route", route.Name),
					logger.Any("panic", rec),
				)

				if basePlugin, ok := plugin.(*BasePlugin); ok {
					basePlugin.IncrementErrorCount()
				}

				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()

		// Call original handler
		route.Handler(w, r)
	}
}

// Middleware integration

// RegisterMiddleware registers all plugin middleware with the router
func (i *integrator) RegisterMiddleware(r router.Router) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	plugins := i.manager.ListEnabled()
	var allMiddleware []MiddlewareRegistration

	// Collect all middleware
	for _, plugin := range plugins {
		pluginName := plugin.Name()
		middlewares := plugin.Middleware()

		for _, mw := range middlewares {
			registration := MiddlewareRegistration{
				Plugin:     pluginName,
				Middleware: mw,
				Registered: time.Now(),
				Active:     true,
				Priority:   mw.Priority,
			}
			allMiddleware = append(allMiddleware, registration)
		}
	}

	// Sort by priority (higher priority first)
	sort.Slice(allMiddleware, func(i, j int) bool {
		return allMiddleware[i].Priority > allMiddleware[j].Priority
	})

	// Register middleware in priority order
	for _, registration := range allMiddleware {
		wrappedHandler := i.wrapMiddleware(registration.Plugin, registration.Middleware)
		r.Use(wrappedHandler)

		i.logger.Debug("Middleware registered",
			logger.String("plugin", registration.Plugin),
			logger.String("middleware", registration.Middleware.Name),
			logger.Int("priority", registration.Priority),
		)
	}

	// Store registrations grouped by plugin
	for _, registration := range allMiddleware {
		i.middleware[registration.Plugin] = append(i.middleware[registration.Plugin], registration)
	}

	i.logger.Info("Plugin middleware registered",
		logger.Int("total_middleware", len(allMiddleware)),
	)

	return nil
}

// UnregisterMiddleware removes middleware for a specific plugin
func (i *integrator) UnregisterMiddleware(r router.Router, pluginName string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	registrations, exists := i.middleware[pluginName]
	if !exists {
		return nil
	}

	// Mark middleware as inactive
	for idx := range registrations {
		registrations[idx].Active = false
	}

	i.middleware[pluginName] = registrations

	i.logger.Info("Plugin middleware unregistered",
		logger.String("plugin", pluginName),
		logger.Int("middleware", len(registrations)),
	)

	return nil
}

func (i *integrator) wrapMiddleware(pluginName string, mw MiddlewareDefinition) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Add plugin context
			ctx := context.WithValue(r.Context(), "middleware_plugin", pluginName)
			ctx = context.WithValue(ctx, "middleware_name", mw.Name)
			r = r.WithContext(ctx)

			// Handle panics
			defer func() {
				if rec := recover(); rec != nil {
					i.logger.Error("Plugin middleware panicked",
						logger.String("plugin", pluginName),
						logger.String("middleware", mw.Name),
						logger.Any("panic", rec),
					)
					next.ServeHTTP(w, r) // Continue with next handler
				}
			}()

			// Apply middleware
			mw.Handler(next).ServeHTTP(w, r)
		})
	}
}

// Job system integration

// RegisterJobs registers all plugin jobs with the job manager
func (i *integrator) RegisterJobs(jobManager jobs.Processor) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	plugins := i.manager.ListEnabled()

	for _, plugin := range plugins {
		if err := i.registerPluginJobs(jobManager, plugin); err != nil {
			i.recordError(plugin.Name(), "job", "registration", err.Error())
			i.logger.Error("Failed to register plugin jobs",
				logger.String("plugin", plugin.Name()),
				logger.Error(err),
			)
			continue
		}
	}

	i.logger.Info("Plugin jobs registered",
		logger.Int("plugins", len(plugins)),
		logger.Int("total_jobs", i.getTotalJobs()),
	)

	return nil
}

// UnregisterJobs removes all jobs for a specific plugin
func (i *integrator) UnregisterJobs(jobManager jobs.Processor, pluginName string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	registrations, exists := i.jobs[pluginName]
	if !exists {
		return nil
	}

	// Remove jobs from job manager and mark as inactive
	for idx := range registrations {
		if registrations[idx].JobID != "" {
			if err := jobManager.DeleteJob(context.Background(), registrations[idx].JobID); err != nil {
				i.logger.Warn("Failed to remove job",
					logger.String("plugin", pluginName),
					logger.String("job", registrations[idx].Job.Name),
					logger.Error(err),
				)
			}
		}
		registrations[idx].Active = false
	}

	i.jobs[pluginName] = registrations

	i.logger.Info("Plugin jobs unregistered",
		logger.String("plugin", pluginName),
		logger.Int("jobs", len(registrations)),
	)

	return nil
}

func (i *integrator) registerPluginJobs(jobManager jobs.Processor, plugin Plugin) error {
	pluginName := plugin.Name()
	jobDefs := plugin.Jobs()
	ctx := context.Background()

	var registrations []JobRegistration

	for _, jobDef := range jobDefs {
		// Wrap job handler with plugin context
		wrappedHandler := i.wrapJobHandler(plugin, &jobDef)

		// Register job handler
		err := jobManager.RegisterHandler(jobDef.Type, wrappedHandler)
		if err != nil {
			return fmt.Errorf("failed to register job %s: %w", jobDef.Name, err)
		}

		// Create job configuration
		job := jobs.Job{
			ID:         fmt.Sprintf("%s.%s", pluginName, jobDef.Name),
			Type:       jobDef.Type,
			Queue:      jobDef.Queue,
			Priority:   jobDef.Priority,
			MaxRetries: jobDef.MaxRetries,
			Timeout:    jobDef.Timeout,
		}

		// Register job
		err = jobManager.Enqueue(ctx, job)
		if err != nil {
			return fmt.Errorf("failed to register job %s: %w", jobDef.Name, err)
		}

		// Schedule job if it has a schedule
		if jobDef.Schedule != "" {
			// if err := jobManager.EnqueueAt(job.ID, jobDef.Schedule); err != nil {
			// 	i.logger.Warn("Failed to schedule job",
			// 		logger.String("plugin", pluginName),
			// 		logger.String("job", jobDef.Name),
			// 		logger.String("schedule", jobDef.Schedule),
			// 		logger.Error(err),
			// 	)
			// }
		}

		// Track registration
		registration := JobRegistration{
			Plugin:     pluginName,
			Job:        jobDef,
			Registered: time.Now(),
			Active:     true,
			JobID:      job.ID,
		}
		registrations = append(registrations, registration)

		i.logger.Debug("Job registered",
			logger.String("plugin", pluginName),
			logger.String("job", jobDef.Name),
			logger.String("job_id", job.ID),
		)
	}

	i.jobs[pluginName] = registrations
	return nil
}

func (i *integrator) wrapJobHandler(plugin Plugin, jobDef *JobDefinition) jobs.JobHandler {
	return newPluginJobHandler(plugin, jobDef, i.logger)
}

// Command integration

// RegisterCommands registers all plugin commands with the root command
func (i *integrator) RegisterCommands(rootCmd *cobra.Command) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	plugins := i.manager.ListEnabled()

	for _, plugin := range plugins {
		if err := i.registerPluginCommands(rootCmd, plugin); err != nil {
			i.recordError(plugin.Name(), "command", "registration", err.Error())
			i.logger.Error("Failed to register plugin commands",
				logger.String("plugin", plugin.Name()),
				logger.Error(err),
			)
			continue
		}
	}

	i.logger.Info("Plugin commands registered",
		logger.Int("plugins", len(plugins)),
		logger.Int("total_commands", i.getTotalCommands()),
	)

	return nil
}

// UnregisterCommands removes all commands for a specific plugin
func (i *integrator) UnregisterCommands(rootCmd *cobra.Command, pluginName string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	registrations, exists := i.commands[pluginName]
	if !exists {
		return nil
	}

	// Remove commands from root command
	for _, registration := range registrations {
		i.removeCommand(rootCmd, registration.Command.Name)
	}

	// Mark as inactive
	for idx := range registrations {
		registrations[idx].Active = false
	}

	i.commands[pluginName] = registrations

	i.logger.Info("Plugin commands unregistered",
		logger.String("plugin", pluginName),
		logger.Int("commands", len(registrations)),
	)

	return nil
}

func (i *integrator) registerPluginCommands(rootCmd *cobra.Command, plugin Plugin) error {
	pluginName := plugin.Name()
	commands := plugin.Commands()

	var registrations []CommandRegistration

	for _, cmdDef := range commands {
		// Create cobra command
		cobraCmd := &cobra.Command{
			Use:     cmdDef.Name,
			Short:   cmdDef.Description,
			Long:    cmdDef.Usage,
			Hidden:  cmdDef.Hidden,
			Aliases: cmdDef.Aliases,
			RunE:    i.wrapCommandHandler(plugin, cmdDef),
		}

		// Add flags
		for _, flag := range cmdDef.Flags {
			i.addFlag(cobraCmd, flag)
		}

		// Add subcommands
		for _, subCmd := range cmdDef.Subcommands {
			subCobraCmd := &cobra.Command{
				Use:     subCmd.Name,
				Short:   subCmd.Description,
				Long:    subCmd.Usage,
				Hidden:  subCmd.Hidden,
				Aliases: subCmd.Aliases,
				RunE:    i.wrapCommandHandler(plugin, subCmd),
			}

			for _, flag := range subCmd.Flags {
				i.addFlag(subCobraCmd, flag)
			}

			cobraCmd.AddCommand(subCobraCmd)
		}

		// Add to root command
		rootCmd.AddCommand(cobraCmd)

		// Track registration
		registration := CommandRegistration{
			Plugin:     pluginName,
			Command:    cmdDef,
			Registered: time.Now(),
			Active:     true,
		}
		registrations = append(registrations, registration)

		i.logger.Debug("Command registered",
			logger.String("plugin", pluginName),
			logger.String("command", cmdDef.Name),
		)
	}

	i.commands[pluginName] = registrations
	return nil
}

func (i *integrator) wrapCommandHandler(plugin Plugin, cmdDef CommandDefinition) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		// Create context with plugin information
		ctx := context.WithValue(context.Background(), "plugin", plugin.Name())
		ctx = context.WithValue(ctx, "command", cmdDef.Name)

		// Track command execution
		start := time.Now()
		defer func() {
			duration := time.Since(start)
			i.logger.Debug("Plugin command executed",
				logger.String("plugin", plugin.Name()),
				logger.String("command", cmdDef.Name),
				logger.Duration("duration", duration),
			)
		}()

		// Execute command
		return cmdDef.Handler.Execute(ctx, args)
	}
}

func (i *integrator) addFlag(cmd *cobra.Command, flag FlagDefinition) {
	switch flag.Type {
	case "string":
		defaultVal := ""
		if flag.Default != nil {
			if val, ok := flag.Default.(string); ok {
				defaultVal = val
			}
		}
		cmd.Flags().String(flag.Name, defaultVal, flag.Description)
		if flag.Short != "" {
			cmd.Flags().StringP(flag.Name, flag.Short, defaultVal, flag.Description)
		}

	case "int":
		defaultVal := 0
		if flag.Default != nil {
			if val, ok := flag.Default.(int); ok {
				defaultVal = val
			}
		}
		cmd.Flags().Int(flag.Name, defaultVal, flag.Description)
		if flag.Short != "" {
			cmd.Flags().IntP(flag.Name, flag.Short, defaultVal, flag.Description)
		}

	case "bool":
		defaultVal := false
		if flag.Default != nil {
			if val, ok := flag.Default.(bool); ok {
				defaultVal = val
			}
		}
		cmd.Flags().Bool(flag.Name, defaultVal, flag.Description)
		if flag.Short != "" {
			cmd.Flags().BoolP(flag.Name, flag.Short, defaultVal, flag.Description)
		}
	}

	if flag.Required {
		cmd.MarkFlagRequired(flag.Name)
	}
}

func (i *integrator) removeCommand(rootCmd *cobra.Command, commandName string) {
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == commandName {
			rootCmd.RemoveCommand(cmd)
			break
		}
	}
}

// Health check integration

// RegisterHealthChecks registers all plugin health checks
func (i *integrator) RegisterHealthChecks(healthChecker interface{}) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	plugins := i.manager.ListEnabled()

	for _, plugin := range plugins {
		if err := i.registerPluginHealthChecks(healthChecker, plugin); err != nil {
			i.recordError(plugin.Name(), "health_check", "registration", err.Error())
			i.logger.Error("Failed to register plugin health checks",
				logger.String("plugin", plugin.Name()),
				logger.Error(err),
			)
			continue
		}
	}

	i.logger.Info("Plugin health checks registered",
		logger.Int("plugins", len(plugins)),
		logger.Int("total_checks", i.getTotalHealthChecks()),
	)

	return nil
}

// UnregisterHealthChecks removes health checks for a specific plugin
func (i *integrator) UnregisterHealthChecks(healthChecker interface{}, pluginName string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	registrations, exists := i.healthChecks[pluginName]
	if !exists {
		return nil
	}

	// Mark as inactive (actual removal would depend on health checker interface)
	for idx := range registrations {
		registrations[idx].Active = false
	}

	i.healthChecks[pluginName] = registrations

	i.logger.Info("Plugin health checks unregistered",
		logger.String("plugin", pluginName),
		logger.Int("checks", len(registrations)),
	)

	return nil
}

func (i *integrator) registerPluginHealthChecks(healthChecker interface{}, plugin Plugin) error {
	pluginName := plugin.Name()
	checks := plugin.HealthChecks()

	var registrations []HealthCheckRegistration

	for _, check := range checks {
		// Create wrapped health check
		wrappedCheck := i.wrapHealthCheck(plugin, check)
		fmt.Printf("%+v\n", wrappedCheck)

		// Register with health checker (this would depend on the actual health checker interface)
		// For now, just track the registration
		registration := HealthCheckRegistration{
			Plugin:     pluginName,
			Check:      check,
			Registered: time.Now(),
			Active:     true,
		}
		registrations = append(registrations, registration)

		i.logger.Debug("Health check registered",
			logger.String("plugin", pluginName),
			logger.String("check", check.Name),
		)
	}

	i.healthChecks[pluginName] = registrations
	return nil
}

func (i *integrator) wrapHealthCheck(plugin Plugin, check HealthCheckDefinition) interface{} {
	// This would return a health check compatible with the health checker interface
	return func(ctx context.Context) error {
		// Add timeout
		if check.Timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, check.Timeout)
			defer cancel()
		}

		// Add plugin context
		ctx = context.WithValue(ctx, "plugin", plugin.Name())
		ctx = context.WithValue(ctx, "health_check", check.Name)

		// Execute health check
		return check.Handler.Check(ctx)
	}
}

// Event integration

// SubscribeToEvents subscribes plugins to relevant events
func (i *integrator) SubscribeToEvents(eventBus EventBus) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	// Subscribe to plugin lifecycle events
	eventBus.Subscribe(EventTypePluginEnabled, &pluginEventHandler{integrator: i, eventType: "enabled"})
	eventBus.Subscribe(EventTypePluginDisabled, &pluginEventHandler{integrator: i, eventType: "disabled"})
	eventBus.Subscribe(EventTypePluginReloaded, &pluginEventHandler{integrator: i, eventType: "reloaded"})

	i.logger.Info("Subscribed to plugin lifecycle events")
	return nil
}

// UnsubscribeFromEvents unsubscribes from events for a specific plugin
func (i *integrator) UnsubscribeFromEvents(eventBus EventBus, pluginName string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	registrations, exists := i.eventHandlers[pluginName]
	if !exists {
		return nil
	}

	// Mark as inactive
	for idx := range registrations {
		registrations[idx].Active = false
	}

	i.eventHandlers[pluginName] = registrations

	i.logger.Info("Plugin event handlers unregistered",
		logger.String("plugin", pluginName),
		logger.Int("handlers", len(registrations)),
	)

	return nil
}

// Lifecycle management

// OnPluginEnabled handles plugin enabled events
func (i *integrator) OnPluginEnabled(plugin Plugin) error {
	i.logger.Info("Plugin enabled, integrating",
		logger.String("plugin", plugin.Name()),
	)

	// Auto-integrate if configured
	if i.config.AutoRegister {
		// This would trigger re-registration of routes, jobs, etc.
		// For now, just log
		i.logger.Debug("Auto-integration not fully implemented")
	}

	return nil
}

// OnPluginDisabled handles plugin disabled events
func (i *integrator) OnPluginDisabled(plugin Plugin) error {
	i.logger.Info("Plugin disabled, removing integrations",
		logger.String("plugin", plugin.Name()),
	)

	// Remove integrations
	// This would call UnregisterRoutes, UnregisterJobs, etc.
	// For now, just log
	i.logger.Debug("Auto-removal not fully implemented")

	return nil
}

// OnPluginReloaded handles plugin reloaded events
func (i *integrator) OnPluginReloaded(plugin Plugin) error {
	i.logger.Info("Plugin reloaded, re-integrating",
		logger.String("plugin", plugin.Name()),
	)

	// Re-integrate plugin
	// This would remove old integrations and add new ones
	// For now, just log
	i.logger.Debug("Auto-reintegration not fully implemented")

	return nil
}

// Status and validation

// GetIntegrationStatus returns the current integration status
func (i *integrator) GetIntegrationStatus() IntegrationStatus {
	i.mu.RLock()
	defer i.mu.RUnlock()

	status := IntegrationStatus{
		Routes:        i.getTotalRoutes(),
		Middleware:    i.getTotalMiddleware(),
		Jobs:          i.getTotalJobs(),
		Commands:      i.getTotalCommands(),
		HealthChecks:  i.getTotalHealthChecks(),
		EventHandlers: i.getTotalEventHandlers(),
		Errors:        make([]IntegrationError, len(i.errors)),
		PluginStatus:  make(map[string]PluginIntegration),
	}

	copy(status.Errors, i.errors)

	// Build plugin status
	for pluginName := range i.routes {
		plugin, err := i.manager.Get(pluginName)
		enabled := err == nil && plugin.IsEnabled()

		integration := PluginIntegration{
			Plugin:       pluginName,
			Enabled:      enabled,
			Routes:       make([]string, 0),
			Middleware:   make([]string, 0),
			Jobs:         make([]string, 0),
			Commands:     make([]string, 0),
			HealthChecks: make([]string, 0),
			LastUpdate:   time.Now(),
			Errors:       make([]string, 0),
		}

		// Collect route names
		for _, reg := range i.routes[pluginName] {
			if reg.Active {
				integration.Routes = append(integration.Routes, reg.Route.Name)
			}
		}

		// Collect middleware names
		for _, reg := range i.middleware[pluginName] {
			if reg.Active {
				integration.Middleware = append(integration.Middleware, reg.Middleware.Name)
			}
		}

		// Collect job names
		for _, reg := range i.jobs[pluginName] {
			if reg.Active {
				integration.Jobs = append(integration.Jobs, reg.Job.Name)
			}
		}

		// Collect command names
		for _, reg := range i.commands[pluginName] {
			if reg.Active {
				integration.Commands = append(integration.Commands, reg.Command.Name)
			}
		}

		// Collect health check names
		for _, reg := range i.healthChecks[pluginName] {
			if reg.Active {
				integration.HealthChecks = append(integration.HealthChecks, reg.Check.Name)
			}
		}

		// Collect errors
		for _, err := range i.errors {
			if err.Plugin == pluginName {
				integration.Errors = append(integration.Errors, err.Message)
			}
		}

		status.PluginStatus[pluginName] = integration
	}

	return status
}

// ValidateIntegrations validates all current integrations
func (i *integrator) ValidateIntegrations() []IntegrationError {
	i.mu.RLock()
	defer i.mu.RUnlock()

	var validationErrors []IntegrationError

	// Validate routes
	for pluginName, registrations := range i.routes {
		for _, reg := range registrations {
			if reg.Active {
				if err := i.validateRoute(reg.Route); err != nil {
					validationErrors = append(validationErrors, IntegrationError{
						Plugin:    pluginName,
						Component: "route",
						Type:      "validation",
						Message:   err.Error(),
						Timestamp: time.Now(),
					})
				}
			}
		}
	}

	// Add other validation logic for jobs, commands, etc.

	return validationErrors
}

// Private helper methods

func (i *integrator) setupAutoIntegration() {
	// Subscribe to plugin manager events
	i.manager.OnPluginEnabled(func(plugin Plugin) {
		i.OnPluginEnabled(plugin)
	})

	i.manager.OnPluginDisabled(func(plugin Plugin) {
		i.OnPluginDisabled(plugin)
	})
}

func (i *integrator) validateRoute(route RouteDefinition) error {
	if route.Method == "" {
		return fmt.Errorf("route method is required")
	}

	if route.Pattern == "" {
		return fmt.Errorf("route pattern is required")
	}

	if route.Handler == nil {
		return fmt.Errorf("route handler is required")
	}

	return nil
}

func (i *integrator) checkRouteConflict(route RouteDefinition, pluginName string) error {
	// Check for route conflicts across all plugins
	for otherPlugin, registrations := range i.routes {
		if otherPlugin == pluginName {
			continue
		}

		for _, reg := range registrations {
			if reg.Active && reg.Route.Method == route.Method && reg.Route.Pattern == route.Pattern {
				return fmt.Errorf("route conflict with plugin %s: %s %s", otherPlugin, route.Method, route.Pattern)
			}
		}
	}

	return nil
}

func (i *integrator) recordError(plugin, component, errorType, message string) {
	error := IntegrationError{
		Plugin:    plugin,
		Component: component,
		Type:      errorType,
		Message:   message,
		Timestamp: time.Now(),
	}

	i.errors = append(i.errors, error)

	// Keep only last 100 errors
	if len(i.errors) > 100 {
		i.errors = i.errors[1:]
	}
}

func (i *integrator) getTotalRoutes() int {
	total := 0
	for _, registrations := range i.routes {
		for _, reg := range registrations {
			if reg.Active {
				total++
			}
		}
	}
	return total
}

func (i *integrator) getTotalMiddleware() int {
	total := 0
	for _, registrations := range i.middleware {
		for _, reg := range registrations {
			if reg.Active {
				total++
			}
		}
	}
	return total
}

func (i *integrator) getTotalJobs() int {
	total := 0
	for _, registrations := range i.jobs {
		for _, reg := range registrations {
			if reg.Active {
				total++
			}
		}
	}
	return total
}

func (i *integrator) getTotalCommands() int {
	total := 0
	for _, registrations := range i.commands {
		for _, reg := range registrations {
			if reg.Active {
				total++
			}
		}
	}
	return total
}

func (i *integrator) getTotalHealthChecks() int {
	total := 0
	for _, registrations := range i.healthChecks {
		for _, reg := range registrations {
			if reg.Active {
				total++
			}
		}
	}
	return total
}

func (i *integrator) getTotalEventHandlers() int {
	total := 0
	for _, registrations := range i.eventHandlers {
		for _, reg := range registrations {
			if reg.Active {
				total++
			}
		}
	}
	return total
}

// Event handler for plugin lifecycle events
type pluginEventHandler struct {
	integrator *integrator
	eventType  string
}

func (h *pluginEventHandler) Handle(ctx context.Context, event Event) error {
	pluginName := event.Plugin

	plugin, err := h.integrator.manager.Get(pluginName)
	if err != nil {
		return err
	}

	switch h.eventType {
	case "enabled":
		return h.integrator.OnPluginEnabled(plugin)
	case "disabled":
		return h.integrator.OnPluginDisabled(plugin)
	case "reloaded":
		return h.integrator.OnPluginReloaded(plugin)
	}

	return nil
}
