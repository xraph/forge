package forge

import (
	"context"
	"os"
	"time"
)

// App represents a Forge application with lifecycle management
type App interface {
	// Core components
	Container() Container
	Router() Router
	Config() ConfigManager
	Logger() Logger
	Metrics() Metrics
	HealthManager() HealthManager

	// Lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Run() error // Blocks until shutdown signal

	// Registration
	RegisterService(name string, factory Factory, opts ...RegisterOption) error
	RegisterController(controller Controller) error
	RegisterExtension(ext Extension) error

	// Information
	Name() string
	Version() string
	Environment() string
	StartTime() time.Time
	Uptime() time.Duration

	// Extensions
	Extensions() []Extension
	GetExtension(name string) (Extension, error)
}

// AppConfig configures the application
type AppConfig struct {
	// Basic info
	Name        string
	Version     string
	Description string
	Environment string // "development", "staging", "production"

	// Components
	ConfigManager ConfigManager
	Logger        Logger

	// Router options
	RouterOptions []RouterOption

	// Observability
	MetricsConfig MetricsConfig
	HealthConfig  HealthConfig

	ErrorHandler ErrorHandler

	// Server
	HTTPAddress string        // Default: ":8080"
	HTTPTimeout time.Duration // Default: 30s

	// Shutdown
	ShutdownTimeout time.Duration // Default: 30s
	ShutdownSignals []os.Signal   // Default: SIGINT, SIGTERM

	// Extensions
	Extensions []Extension // Extensions to register with the app
}

// DefaultAppConfig returns a default application configuration
func DefaultAppConfig() AppConfig {
	// Get proper default health config with all fields set
	healthConfig := DefaultHealthConfig()
	healthConfig.CheckInterval = 30 * time.Second
	healthConfig.ReportInterval = 60 * time.Second
	healthConfig.EnableAutoDiscovery = true
	healthConfig.MaxConcurrentChecks = 10
	healthConfig.DefaultTimeout = 5 * time.Second
	healthConfig.EnableSmartAggregation = true
	healthConfig.HistorySize = 100

	return AppConfig{
		Name:            "forge-app",
		Version:         "1.0.0",
		Description:     "Forge Application",
		Environment:     "development",
		HTTPAddress:     ":8080",
		HTTPTimeout:     30 * time.Second,
		ShutdownTimeout: 30 * time.Second,
		ShutdownSignals: nil, // Will use default SIGINT, SIGTERM
		RouterOptions:   nil,
		MetricsConfig:   DefaultMetricsConfig(),
		HealthConfig:    healthConfig,
	}
}

// NewApp creates a new Forge application
func NewApp(config AppConfig) App {
	return newApp(config)
}

// New creates a new Forge application
func New(config AppConfig) App {
	return newApp(config)
}

// AppInfo represents application information returned by /_/info endpoint
type AppInfo struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	Environment string          `json:"environment"`
	StartTime   time.Time       `json:"start_time"`
	Uptime      time.Duration   `json:"uptime"`
	GoVersion   string          `json:"go_version"`
	Services    []string        `json:"services"`
	Routes      int             `json:"routes"`
	Extensions  []ExtensionInfo `json:"extensions,omitempty"`
}
