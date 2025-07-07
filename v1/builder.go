package forge

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/xraph/forge/core"
	"github.com/xraph/forge/database"
	"github.com/xraph/forge/jobs"
	"github.com/xraph/forge/logger"
	"github.com/xraph/forge/observability"
	"github.com/xraph/forge/plugins"
	"github.com/xraph/forge/router"
)

// builder implements the Builder interface with fluent API
type builder struct {
	// Core configuration
	name        string
	version     string
	environment string
	config      core.Config
	logger      logger.Logger

	// Component configurations
	databases   []database.Config
	middlewares []func(http.Handler) http.Handler
	groups      []router.Group
	plugins     []plugins.Plugin

	// Feature configurations
	tracingConfig       *observability.TracingConfig
	metricsConfig       *observability.MetricsConfig
	healthConfig        *observability.HealthConfig
	loggingConfig       *logger.LoggingConfig
	jobsConfig          *jobs.Config
	routerConfig        *router.Config
	serverConfig        *core.ServerConfig
	corsConfig          *router.CORSConfig
	rateLimitConfig     *router.RateLimitConfig
	documentationConfig *router.DocumentationConfig

	// Feature flags
	enableJobs          bool
	enableCache         bool
	enableTracing       bool
	enableMetrics       bool
	enableLogging       bool
	enableHealthChecks  bool
	enablePlugins       bool
	enableWebSocket     bool
	enableSSE           bool
	enableHotReload     bool
	enableAutoMigration bool
	enableDocumentation bool

	// Mode flags
	developmentMode bool
	testingMode     bool
	productionMode  bool

	// Build options
	skipValidation bool
	autoStart      bool
}

// NewApplication creates a new application builder
func NewApplication(name string) Builder {
	return &builder{
		name:               name,
		version:            "1.0.0",
		environment:        "development",
		config:             core.NewConfig(),
		enableHealthChecks: true,
		enableMetrics:      true,
		enableLogging:      true,
	}
}

// New creates a new application builder (alias for NewApplication)
func New(name string) Builder {
	return NewApplication(name)
}

// Configuration methods

func (b *builder) WithConfig(config core.Config) Builder {
	b.config = config
	return b
}

func (b *builder) WithConfigFile(path string) Builder {
	config, err := core.LoadConfigFromFile(path)
	if err != nil {
		panic(fmt.Sprintf("failed to load config from %s: %v", path, err))
	}
	b.config = config
	return b
}

func (b *builder) WithConfigFromEnv() Builder {
	config, err := core.LoadConfig()
	if err != nil {
		panic(fmt.Sprintf("failed to load config from environment: %v", err))
	}
	b.config = config
	return b
}

func (b *builder) WithConfigFromBytes(data []byte) Builder {
	config, err := core.LoadConfigFromBytes(data)
	if err != nil {
		panic(fmt.Sprintf("failed to load config from bytes: %v", err))
	}
	b.config = config
	return b
}

// Database configuration methods

func (b *builder) WithDatabase(config database.Config) Builder {
	b.databases = append(b.databases, config)
	return b
}

func (b *builder) WithSQL(name string, config database.SQLConfig) Builder {
	dbConfig := database.Config{
		Name: name,
		Type: "sql",
		SQL:  &config,
	}
	return b.WithDatabase(dbConfig)
}

func (b *builder) WithNoSQL(name string, config database.NoSQLConfig) Builder {
	dbConfig := database.Config{
		Name:  name,
		Type:  "nosql",
		NoSQL: &config,
	}
	return b.WithDatabase(dbConfig)
}

func (b *builder) WithCache(name string, config database.CacheConfig) Builder {
	dbConfig := database.Config{
		Name:  name,
		Type:  "cache",
		Cache: &config,
	}
	return b.WithDatabase(dbConfig)
}

// Database convenience methods

func (b *builder) WithPostgreSQL(name, dsn string) Builder {
	config := database.SQLConfig{
		Driver: "postgres",
		DSN:    dsn,
	}
	return b.WithSQL(name, config)
}

func (b *builder) WithMySQL(name, dsn string) Builder {
	config := database.SQLConfig{
		Driver: "mysql",
		DSN:    dsn,
	}
	return b.WithSQL(name, config)
}

func (b *builder) WithSQLite(name, path string) Builder {
	config := database.SQLConfig{
		Driver: "sqlite",
		DSN:    path,
	}
	return b.WithSQL(name, config)
}

func (b *builder) WithMongoDB(name, uri string) Builder {
	config := database.NoSQLConfig{
		Driver: "mongodb",
		URL:    uri,
	}
	return b.WithNoSQL(name, config)
}

func (b *builder) WithRedis(name, addr string) Builder {
	config := database.CacheConfig{
		Driver: "redis",
		Host:   strings.Split(addr, ":")[0],
	}
	if len(strings.Split(addr, ":")) > 1 {
		config.Port = 6379 // default, should parse from addr
	}
	return b.WithCache(name, config)
}

func (b *builder) WithMemoryCache(name string) Builder {
	config := database.CacheConfig{
		Driver: "memory",
	}
	return b.WithCache(name, config)
}

// Observability methods

func (b *builder) WithTracing(config observability.TracingConfig) Builder {
	b.tracingConfig = &config
	b.enableTracing = true
	return b
}

func (b *builder) WithMetrics(config observability.MetricsConfig) Builder {
	b.metricsConfig = &config
	b.enableMetrics = true
	return b
}

func (b *builder) WithHealth(config observability.HealthConfig) Builder {
	b.healthConfig = &config
	b.enableHealthChecks = true
	return b
}

func (b *builder) WithLogging(config logger.LoggingConfig) Builder {
	b.loggingConfig = &config
	return b
}

// Feature methods

func (b *builder) WithJobs(config jobs.Config) Builder {
	b.jobsConfig = &config
	b.enableJobs = true
	return b
}

func (b *builder) WithPlugins(plugins ...plugins.Plugin) Builder {
	b.plugins = append(b.plugins, plugins...)
	b.enablePlugins = true
	return b
}

func (b *builder) WithPlugin(plugin plugins.Plugin) Builder {
	b.plugins = append(b.plugins, plugin)
	b.enablePlugins = true
	return b
}

// HTTP and routing methods

func (b *builder) WithMiddleware(middleware ...func(http.Handler) http.Handler) Builder {
	b.middlewares = append(b.middlewares, middleware...)
	return b
}

func (b *builder) WithGroups(groups ...router.Group) Builder {
	b.groups = append(b.groups, groups...)
	return b
}

func (b *builder) WithGroup(group router.Group) Builder {
	b.groups = append(b.groups, group)
	return b
}

func (b *builder) WithRouter(config router.Config) Builder {
	b.routerConfig = &config
	return b
}

func (b *builder) WithCORS(config router.CORSConfig) Builder {
	b.corsConfig = &config
	return b
}

func (b *builder) WithRateLimit(config router.RateLimitConfig) Builder {
	b.rateLimitConfig = &config
	return b
}

// Documentation Configuration Methods

func (b *builder) WithDocumentation(config router.DocumentationConfig) Builder {
	b.documentationConfig = &config
	b.enableDocumentation = true
	return b
}

func (b *builder) WithOpenAPI(title, version, description string) Builder {
	if b.documentationConfig == nil {
		b.documentationConfig = &router.DocumentationConfig{}
	}

	b.documentationConfig.Title = title
	b.documentationConfig.Version = version
	b.documentationConfig.Description = description
	b.documentationConfig.EnableAutoGeneration = true
	b.enableDocumentation = true

	return b
}

func (b *builder) WithSwaggerUI(path string) Builder {
	if b.documentationConfig == nil {
		b.documentationConfig = &router.DocumentationConfig{}
	}

	b.documentationConfig.EnableSwaggerUI = true
	b.documentationConfig.SwaggerUIPath = path
	b.enableDocumentation = true

	return b
}

func (b *builder) WithReDoc(path string) Builder {
	if b.documentationConfig == nil {
		b.documentationConfig = &router.DocumentationConfig{}
	}

	b.documentationConfig.EnableReDoc = true
	b.documentationConfig.ReDocPath = path
	b.enableDocumentation = true

	return b
}

func (b *builder) WithAsyncAPI(config router.AsyncAPIConfig) Builder {
	if b.documentationConfig == nil {
		b.documentationConfig = &router.DocumentationConfig{}
	}

	b.documentationConfig.AsyncAPI = config
	b.documentationConfig.EnableAsyncAPIUI = true
	b.enableDocumentation = true

	return b
}

func (b *builder) WithPostmanCollection(path string) Builder {
	if b.documentationConfig == nil {
		b.documentationConfig = &router.DocumentationConfig{}
	}

	b.documentationConfig.EnablePostman = true
	b.documentationConfig.PostmanPath = path
	b.enableDocumentation = true

	return b
}

func (b *builder) WithAPIDocumentation() Builder {
	return b.EnableDocumentation()
}

// Server methods

func (b *builder) WithServer(config core.ServerConfig) Builder {
	b.serverConfig = &config
	return b
}

func (b *builder) WithTLS(certFile, keyFile string) Builder {
	if b.serverConfig == nil {
		b.serverConfig = &core.ServerConfig{}
	}
	b.serverConfig.TLS.Enabled = true
	b.serverConfig.TLS.CertFile = certFile
	b.serverConfig.TLS.KeyFile = keyFile
	return b
}

func (b *builder) WithHost(host string) Builder {
	if b.serverConfig == nil {
		b.serverConfig = &core.ServerConfig{}
	}
	b.serverConfig.Host = host
	return b
}

func (b *builder) WithPort(port int) Builder {
	if b.serverConfig == nil {
		b.serverConfig = &core.ServerConfig{}
	}
	b.serverConfig.Port = port
	return b
}

// Feature toggle methods

func (b *builder) EnableJobs() Builder {
	b.enableJobs = true
	return b
}

func (b *builder) EnableCache() Builder {
	b.enableCache = true
	return b
}

func (b *builder) EnableTracing() Builder {
	b.enableTracing = true
	return b
}

func (b *builder) EnableMetrics() Builder {
	b.enableMetrics = true
	return b
}

func (b *builder) EnableHealthChecks() Builder {
	b.enableHealthChecks = true
	return b
}

func (b *builder) EnablePlugins() Builder {
	b.enablePlugins = true
	return b
}

func (b *builder) EnableWebSocket() Builder {
	b.enableWebSocket = true
	return b
}

func (b *builder) EnableSSE() Builder {
	b.enableSSE = true
	return b
}

func (b *builder) EnableHotReload() Builder {
	b.enableHotReload = true
	return b
}

func (b *builder) EnableDocumentation() Builder {
	if b.documentationConfig == nil {
		b.documentationConfig = b.getDefaultDocumentationConfig()
	}
	b.enableDocumentation = true
	return b
}

// Disable methods

func (b *builder) DisableJobs() Builder {
	b.enableJobs = false
	return b
}

func (b *builder) DisableCache() Builder {
	b.enableCache = false
	return b
}

func (b *builder) DisableTracing() Builder {
	b.enableTracing = false
	return b
}

func (b *builder) DisableMetrics() Builder {
	b.enableMetrics = false
	return b
}

func (b *builder) DisableHealthChecks() Builder {
	b.enableHealthChecks = false
	return b
}

func (b *builder) DisablePlugins() Builder {
	b.enablePlugins = false
	return b
}

func (b *builder) DisableDocumentation() Builder {
	b.enableDocumentation = false
	return b
}

// Mode methods

func (b *builder) WithDevelopmentMode() Builder {
	b.developmentMode = true
	b.environment = "development"
	b.enableHotReload = true
	b.enableMetrics = true
	b.enableHealthChecks = true
	b.enableDocumentation = true // Enable docs in development
	return b
}

func (b *builder) WithTestingMode() Builder {
	b.testingMode = true
	b.environment = "testing"
	b.enableMetrics = false
	b.enableTracing = false
	b.enableDocumentation = false // Disable docs in testing
	return b
}

func (b *builder) WithProductionMode() Builder {
	b.productionMode = true
	b.environment = "production"
	b.enableHotReload = false
	// Documentation can be enabled in production for internal APIs
	return b
}

func (b *builder) WithHotReload() Builder {
	b.enableHotReload = true
	return b
}

func (b *builder) WithAutoMigration() Builder {
	b.enableAutoMigration = true
	return b
}

// Build methods

func (b *builder) Build() (Application, error) {
	start := time.Now()

	app := &application{
		name:        b.name,
		version:     b.version,
		environment: b.environment,
		config:      b.config,
		testingMode: b.testingMode,
		hotReload:   b.enableHotReload,
		startTime:   start,
	}

	// Initialize logger first
	if err := b.initializeLogger(app); err != nil {
		return nil, fmt.Errorf("failed to initialize logger: %w", err)
	}

	app.logger.Info("Building application",
		logger.String("name", app.name),
		logger.String("version", app.version),
		logger.String("environment", app.environment),
	)

	// Initialize container
	app.container = core.NewContainer(app.logger)

	// Register core services
	_ = app.container.Register(ComponentConfig, app.config)
	_ = app.container.Register(ComponentLogger, app.logger)

	// Initialize components in order
	if err := b.initializeDatabases(app); err != nil {
		return nil, fmt.Errorf("failed to initialize databases: %w", err)
	}

	if err := b.initializeObservability(app); err != nil {
		return nil, fmt.Errorf("failed to initialize observability: %w", err)
	}

	if err := b.initializeJobs(app); err != nil {
		return nil, fmt.Errorf("failed to initialize jobs: %w", err)
	}

	if err := b.initializeRouter(app); err != nil {
		return nil, fmt.Errorf("failed to initialize router: %w", err)
	}

	if err := b.initializeDocumentation(app); err != nil {
		return nil, fmt.Errorf("failed to initialize documentation: %w", err)
	}

	if err := b.initializePlugins(app); err != nil {
		return nil, fmt.Errorf("failed to initialize plugins: %w", err)
	}

	if err := b.initializeServer(app); err != nil {
		return nil, fmt.Errorf("failed to initialize server: %w", err)
	}

	if err := b.initializeServices(app); err != nil {
		return nil, fmt.Errorf("failed to initialize services: %w", err)
	}

	// Run validation unless skipped
	if !b.skipValidation {
		if err := b.validateConfiguration(app); err != nil {
			return nil, fmt.Errorf("configuration validation failed: %w", err)
		}
	}

	// Auto-migration if enabled
	if b.enableAutoMigration {
		if err := b.runAutoMigrations(app); err != nil {
			app.logger.Warn("Auto-migration failed", logger.Error(err))
		}
	}

	app.startDuration = time.Since(start)
	app.logger.Info("Application built successfully",
		logger.Duration("build_duration", app.startDuration),
		logger.String("go_version", runtime.Version()),
		logger.Int("num_cpu", runtime.NumCPU()),
	)

	return app, nil
}

func (b *builder) MustBuild() Application {
	app, err := b.Build()
	if err != nil {
		panic(fmt.Sprintf("failed to build application: %v", err))
	}
	return app
}

func (b *builder) BuildAndStart(ctx context.Context) (Application, error) {
	app, err := b.Build()
	if err != nil {
		return nil, err
	}

	if err := app.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start application: %w", err)
	}

	return app, nil
}

func (b *builder) MustBuildAndStart(ctx context.Context) Application {
	app, err := b.BuildAndStart(ctx)
	if err != nil {
		panic(fmt.Sprintf("failed to build and start application: %v", err))
	}
	return app
}

// Documentation initialization

func (b *builder) initializeDocumentation(app *application) error {
	if !b.enableDocumentation {
		return nil
	}

	docConfig := b.documentationConfig
	if docConfig == nil {
		docConfig = b.getDefaultDocumentationConfig()
	}

	// Ensure basic configuration is set
	if docConfig.Title == "" {
		docConfig.Title = app.name + " API"
	}
	if docConfig.Version == "" {
		docConfig.Version = app.version
	}
	if docConfig.Description == "" {
		docConfig.Description = fmt.Sprintf("API documentation for %s", app.name)
	}

	// Set up default servers if not configured
	if len(docConfig.Servers) == 0 {
		port := 8080
		if b.serverConfig != nil && b.serverConfig.Port != 0 {
			port = b.serverConfig.Port
		}

		protocol := "http"
		if b.serverConfig != nil && b.serverConfig.TLS.Enabled {
			protocol = "https"
		}

		docConfig.Servers = []router.ServerInfo{
			{
				URL:         fmt.Sprintf("%s://localhost:%d", protocol, port),
				Description: "Development server",
			},
		}
	}

	// Enable the documentation on the router
	app.router.EnableDocumentation(*docConfig)

	app.logger.Info("Documentation system enabled",
		logger.Bool("swagger_ui", docConfig.EnableSwaggerUI),
		logger.Bool("redoc", docConfig.EnableReDoc),
		logger.Bool("asyncapi_ui", docConfig.EnableAsyncAPIUI),
		logger.Bool("postman", docConfig.EnablePostman),
		logger.String("swagger_path", docConfig.SwaggerUIPath),
		logger.String("redoc_path", docConfig.ReDocPath),
	)

	return nil
}

func (b *builder) getDefaultDocumentationConfig() *router.DocumentationConfig {
	config := router.DefaultDocumentationConfig()

	// Customize based on application name and environment
	config.Title = b.name + " API"
	config.Version = b.version
	config.Description = fmt.Sprintf("API documentation for %s", b.name)

	// Adjust paths based on environment
	if b.developmentMode {
		config.EnableSwaggerUI = true
		config.EnableReDoc = true
		config.EnableAsyncAPIUI = true
		config.EnablePostman = true
		config.SwaggerUIPath = "/docs"
		config.ReDocPath = "/redoc"
		config.AsyncAPIUIPath = "/asyncapi"
		config.PostmanPath = "/postman.json"
	} else if b.productionMode {
		// More conservative defaults for production
		config.EnableSwaggerUI = false
		config.EnableReDoc = true
		config.EnableAsyncAPIUI = false
		config.EnablePostman = false
		config.ReDocPath = "/api-docs"
	}

	return &config
}

// Initialization methods (continuing from previous implementation)

func (b *builder) initializeLogger(app *application) error {
	if b.loggingConfig != nil {
		app.logger = logger.NewLogger(*b.loggingConfig)
	} else {
		// Use configuration from main config
		loggingConfig := logger.LoggingConfig{
			Level:       app.config.GetString("logging.level"),
			Format:      app.config.GetString("logging.format"),
			Environment: app.environment,
			Output:      app.config.GetString("logging.output"),
		}

		if loggingConfig.Level == "" {
			if b.developmentMode {
				loggingConfig.Level = "debug"
			} else {
				loggingConfig.Level = "info"
			}
		}

		if loggingConfig.Format == "" {
			if b.productionMode {
				loggingConfig.Format = "json"
			} else {
				loggingConfig.Format = "console"
			}
		}

		app.logger = logger.NewLogger(loggingConfig)
	}

	logger.SetGlobalLogger(app.logger)
	return nil
}

func (b *builder) initializeDatabases(app *application) error {
	for _, dbConfig := range b.databases {
		if err := b.registerDatabase(app, dbConfig); err != nil {
			return fmt.Errorf("failed to register database %s: %w", dbConfig.Name, err)
		}
	}

	// Register default in-memory cache if caching is enabled but no cache configured
	if b.enableCache && !b.hasCacheConfigured() {
		defaultCache := database.Config{
			Name: "default",
			Type: "cache",
			Cache: &database.CacheConfig{
				Driver: "memory",
			},
		}
		if err := b.registerDatabase(app, defaultCache); err != nil {
			return fmt.Errorf("failed to register default cache: %w", err)
		}
	}

	return nil
}

func (b *builder) registerDatabase(app *application, config database.Config) error {
	switch config.Type {
	case "sql":
		if config.SQL == nil {
			return fmt.Errorf("SQL config is required for SQL database")
		}
		app.container.RegisterSingleton("database:"+config.Name, func() interface{} {
			db, err := database.NewSQLDatabase(*config.SQL)
			if err != nil {
				app.logger.Fatal("Failed to create SQL database",
					logger.String("name", config.Name),
					logger.Error(err),
				)
			}
			return db
		})

	case "nosql":
		if config.NoSQL == nil {
			return fmt.Errorf("NoSQL config is required for NoSQL database")
		}
		app.container.RegisterSingleton("nosql:"+config.Name, func() interface{} {
			db, err := database.NewNoSQLDatabase(*config.NoSQL)
			if err != nil {
				app.logger.Fatal("Failed to create NoSQL database",
					logger.String("name", config.Name),
					logger.Error(err),
				)
			}
			return db
		})

	case "cache":
		if config.Cache == nil {
			return fmt.Errorf("Cache config is required for cache")
		}
		app.container.RegisterSingleton("cache:"+config.Name, func() interface{} {
			cache, err := database.NewCache(*config.Cache)
			if err != nil {
				app.logger.Fatal("Failed to create cache",
					logger.String("name", config.Name),
					logger.Error(err),
				)
			}
			return cache
		})

	default:
		return fmt.Errorf("unknown database type: %s", config.Type)
	}

	app.logger.Debug("Database registered",
		logger.String("name", config.Name),
		logger.String("type", config.Type),
	)

	return nil
}

func (b *builder) initializeObservability(app *application) error {
	// Initialize tracing
	if b.enableTracing {
		tracingConfig := b.tracingConfig
		if tracingConfig == nil {
			tracingConfig = &observability.TracingConfig{
				Enabled:     app.config.GetBool("observability.tracing.enabled"),
				ServiceName: app.name,
				Endpoint:    app.config.GetString("observability.tracing.endpoint"),
				SampleRate:  app.config.GetFloat64("observability.tracing.sample_rate"),
			}
		}

		if tracingConfig.ServiceName == "" {
			tracingConfig.ServiceName = app.name
		}

		tracer, err := observability.NewTracer(*tracingConfig)
		if err != nil {
			return fmt.Errorf("failed to create tracer: %w", err)
		}

		_ = app.container.Register(ComponentTracer, tracer)
	}

	// Initialize metrics
	if b.enableMetrics {
		metricsConfig := b.metricsConfig
		if metricsConfig == nil {
			metricsConfig = &observability.MetricsConfig{
				Enabled:     true,
				ServiceName: app.name,
				Port:        app.config.GetInt("observability.metrics.port"),
				Path:        app.config.GetString("observability.metrics.path"),
			}
		}

		if metricsConfig.ServiceName == "" {
			metricsConfig.ServiceName = app.name
		}

		metrics, err := observability.NewMetrics(*metricsConfig)
		if err != nil {
			return fmt.Errorf("failed to create metrics: %w", err)
		}
		_ = app.container.Register(ComponentMetrics, metrics)

		// Create application metrics wrapper
		app.metrics = NewApplicationMetrics(metrics, app.logger)
	}

	// Initialize health checks
	if b.enableHealthChecks {
		healthConfig := b.healthConfig
		if healthConfig == nil {
			healthConfig = &observability.HealthConfig{
				Enabled: true,
				Path:    "/health",
				Timeout: 30 * time.Second,
			}
		}

		health := observability.NewHealth(*healthConfig)
		_ = app.container.Register(ComponentHealth, health)
	}

	return nil
}

func (b *builder) initializeJobs(app *application) error {
	if !b.enableJobs {
		return nil
	}

	jobsConfig := b.jobsConfig
	if jobsConfig == nil {
		jobsConfig = &jobs.Config{
			Enabled:     true,
			Backend:     "memory",
			Concurrency: runtime.NumCPU() * 2,
		}
	}

	processorFactory := jobs.NewProcessorFactory(app.logger)
	processor, err := processorFactory.CreateProcessor(*jobsConfig)
	if err != nil {
		return fmt.Errorf("failed to create job processor: %w", err)
	}

	_ = app.container.Register(ComponentJobs, processor)
	return nil
}

func (b *builder) initializeRouter(app *application) error {
	routerConfig := b.routerConfig
	if routerConfig == nil {
		routerConfig = &router.Config{
			Logger: app.logger,
		}
	}

	app.router = router.NewRouter(*routerConfig)

	// Add default middleware stack
	b.addDefaultMiddleware(app)

	// Add custom middlewares
	for _, middleware := range b.middlewares {
		app.router.Use(middleware)
	}

	// Add CORS if configured
	if b.corsConfig != nil {
		app.router.EnableCORS(*b.corsConfig)
	}

	// Add rate limiting if configured
	if b.rateLimitConfig != nil {
		app.router.EnableRateLimit(*b.rateLimitConfig)
	}

	// Register groups
	for _, group := range b.groups {
		group.Register(app.router)
	}

	// Add default routes
	b.addDefaultRoutes(app)

	return app.container.Register(ComponentRouter, app.router)
}

func (b *builder) initializePlugins(app *application) error {
	if !b.enablePlugins {
		return nil
	}

	var err error

	// Create plugin manager
	app.pluginSystem, err = plugins.NewSystemWithDefaults()
	if err != nil {
		return fmt.Errorf("failed to create plugin system: %w", err)
	}

	// Register plugins
	for _, plugin := range b.plugins {
		if err := app.pluginSystem.Manager().Register(plugin); err != nil {
			return fmt.Errorf("failed to register plugin %s: %w", plugin.Name(), err)
		}
	}

	// Initialize plugins with container
	if err := app.pluginSystem.Initialize(app.container); err != nil {
		return fmt.Errorf("failed to initialize plugins: %w", err)
	}

	_ = app.container.Register(ComponentPluginsSystem, app.pluginSystem)
	app.pluginManager = app.pluginSystem.Manager()
	_ = app.container.Register(ComponentPlugins, app.pluginManager)
	return nil
}

func (b *builder) initializeServer(app *application) error {
	serverConfig := b.serverConfig
	if serverConfig == nil {
		serverConfig = &core.ServerConfig{
			Host:             app.config.GetString("server.host"),
			Port:             app.config.GetInt("server.port"),
			ReadTimeout:      app.config.GetDuration("server.read_timeout"),
			WriteTimeout:     app.config.GetDuration("server.write_timeout"),
			IdleTimeout:      app.config.GetDuration("server.idle_timeout"),
			ShutdownTimeout:  app.config.GetDuration("server.shutdown_timeout"),
			EnableHTTP2:      app.config.GetBool("server.enable_http2"),
			GracefulShutdown: app.config.GetBool("server.graceful_shutdown"),
		}

		// Set defaults if not configured
		if serverConfig.Host == "" {
			serverConfig.Host = "0.0.0.0"
		}
		if serverConfig.Port == 0 {
			serverConfig.Port = 8080
		}
		if serverConfig.ReadTimeout == 0 {
			serverConfig.ReadTimeout = 30 * time.Second
		}
		if serverConfig.WriteTimeout == 0 {
			serverConfig.WriteTimeout = 30 * time.Second
		}
		if serverConfig.ShutdownTimeout == 0 {
			serverConfig.ShutdownTimeout = 30 * time.Second
		}
	}

	app.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", serverConfig.Host, serverConfig.Port),
		Handler:      app.router.Handler(),
		ReadTimeout:  serverConfig.ReadTimeout,
		WriteTimeout: serverConfig.WriteTimeout,
		IdleTimeout:  serverConfig.IdleTimeout,
	}

	return nil
}

func (b *builder) initializeServices(app *application) error {
	// Initialize event bus
	app.eventBus = NewEventBus(app.logger)
	_ = app.container.Register(ComponentEventBus, app.eventBus)

	// Initialize service manager
	app.serviceManager = NewServiceManager(app.container, app.logger)
	_ = app.container.Register(ComponentServiceManager, app.serviceManager)

	// Initialize development server if in development mode
	if b.developmentMode && b.enableHotReload {
		app.devServer = NewDevelopmentServer(app, app.logger)
	}

	return nil
}

// Helper methods for builder

func (b *builder) hasCacheConfigured() bool {
	for _, db := range b.databases {
		if db.Type == "cache" {
			return true
		}
	}
	return false
}

func (b *builder) addDefaultMiddleware(app *application) {
	// Add recovery middleware
	app.router.Use(RecoveryMiddleware(app.logger))

	// Add request ID middleware
	app.router.Use(RequestIDMiddleware())
}

func (b *builder) addDefaultRoutes(app *application) {
	// Add health check routes if enabled
	if b.enableHealthChecks {
		app.router.Health("/health", func(ctx context.Context) error {
			return app.Healthy(ctx)
		})

		app.router.Get("/ready", func(w http.ResponseWriter, r *http.Request) {
			if err := app.Ready(r.Context()); err != nil {
				http.Error(w, "Not ready", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		})
	}

	// Add metrics endpoint if enabled
	if b.enableMetrics {
		app.router.Metrics("/metrics")
	}

	// Add development routes if in development mode
	if b.developmentMode {
		app.router.Get("/debug/routes", func(w http.ResponseWriter, r *http.Request) {
			// Return route information
		})

		app.router.Get("/debug/config", func(w http.ResponseWriter, r *http.Request) {
			// Return sanitized configuration
		})
	}
}

func (b *builder) validateConfiguration(app *application) error {
	if err := app.config.Validate(); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	// Additional application-specific validation
	if app.name == "" {
		return fmt.Errorf("application name cannot be empty")
	}

	return nil
}

func (b *builder) runAutoMigrations(app *application) error {
	app.logger.Info("Running auto-migrations")

	// Run migrations for each configured SQL database
	for _, dbConfig := range b.databases {
		if dbConfig.Type == "sql" && dbConfig.SQL != nil && dbConfig.SQL.AutoMigrate {
			db := app.Database(dbConfig.Name)
			if db != nil {
				// Run migrations (implementation would depend on migration system)
				app.logger.Debug("Running migrations for database", logger.String("name", dbConfig.Name))
			}
		}
	}

	return nil
}

// Convenience functions for creating specific types of applications

// NewWebApplication creates a web application with common defaults
func NewWebApplication(name string) Builder {
	return New(name).
		EnableMetrics().
		EnableHealthChecks().
		EnableTracing().
		EnableDocumentation().
		WithDevelopmentMode()
}

// NewAPIApplication creates an API application with common defaults
func NewAPIApplication(name string) Builder {
	return New(name).
		EnableMetrics().
		EnableHealthChecks().
		EnableTracing().
		EnableJobs().
		EnableDocumentation().
		WithCORS(router.CORSConfig{
			AllowedOrigins: []string{"*"},
			AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowedHeaders: []string{"*"},
		}).
		WithSwaggerUI("/docs").
		WithReDoc("/redoc")
}

// NewMicroservice creates a microservice with common defaults
func NewMicroservice(name string) Builder {
	return New(name).
		EnableMetrics().
		EnableHealthChecks().
		EnableTracing().
		EnableJobs().
		EnablePlugins().
		EnableDocumentation().
		WithProductionMode()
}

// NewTestApplication creates a test application
func NewTestApplication(name string) Builder {
	return New(name).
		WithTestingMode().
		DisableMetrics().
		DisableTracing().
		DisableDocumentation().
		WithMemoryCache("default")
}

// Environment detection helpers

func IsDevelopment(env string) bool {
	return strings.ToLower(env) == "development" || strings.ToLower(env) == "dev"
}

func IsProduction(env string) bool {
	return strings.ToLower(env) == "production" || strings.ToLower(env) == "prod"
}

func IsStaging(env string) bool {
	return strings.ToLower(env) == "staging" || strings.ToLower(env) == "stage"
}

func IsTesting(env string) bool {
	return strings.ToLower(env) == "testing" || strings.ToLower(env) == "test"
}
