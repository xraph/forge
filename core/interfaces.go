package core

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/xraph/forge/database"
	"github.com/xraph/forge/jobs"
	"github.com/xraph/forge/observability"
)

// Container represents the dependency injection container
type Container interface {
	// Registration
	Register(name string, instance interface{}) error
	RegisterSingleton(name string, factory func() interface{}) error
	RegisterScoped(name string, factory func() interface{}) error

	// Resolution
	Resolve(name string) (interface{}, error)
	MustResolve(name string) interface{}
	ResolveInto(target interface{}) error

	// Database access
	SQL(name ...string) database.SQLDatabase
	NoSQL(name ...string) database.NoSQLDatabase
	Cache(name ...string) database.Cache

	// Services
	Jobs() jobs.Processor
	Metrics() observability.Metrics
	Tracer() observability.Tracer
	Health() observability.Health

	// Lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// Config represents application configuration
type Config interface {
	// Basic access
	Get(key string) interface{}
	GetString(key string) string
	GetInt(key string) int
	GetFloat64(key string) float64
	GetBool(key string) bool
	GetDuration(key string) time.Duration
	GetStringSlice(key string) []string

	// Nested configuration
	Sub(key string) Config

	// Validation
	Validate() error

	// Environment
	Environment() string
	IsDevelopment() bool
	IsProduction() bool

	// Watchers
	Watch(callback func(Config)) error
	OnChange(key string, callback func(interface{})) error

	AllKeys() []string
	All() map[string]interface{}
}

// Middleware represents HTTP middleware
type Middleware func(http.Handler) http.Handler

// LifecycleManager manages component lifecycle
type LifecycleManager interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	RegisterStartup(name string, fn func(context.Context) error)
	RegisterShutdown(name string, fn func(context.Context) error)
}

// HealthChecker provides health checking capabilities
type HealthChecker interface {
	AddCheck(name string, check func(context.Context) error)
	RemoveCheck(name string)
	Check(ctx context.Context) map[string]error
	IsHealthy(ctx context.Context) bool
}

// ConfigValidator validates configuration
type ConfigValidator interface {
	ValidateConfig(config Config) error
}

// ServiceRegistry manages service registration and discovery
type ServiceRegistry interface {
	Register(name string, service interface{}) error
	Unregister(name string) error
	Get(name string) (interface{}, bool)
	List() map[string]interface{}
}

// ComponentInitializer initializes framework components
type ComponentInitializer interface {
	Initialize(ctx context.Context, container Container) error
	Priority() int
	Name() string
}

// ServerConfig represents HTTP server configuration
type ServerConfig struct {
	Host             string        `mapstructure:"host" yaml:"host" env:"FORGE_SERVER_HOST"`
	Port             int           `mapstructure:"port" yaml:"port" env:"FORGE_SERVER_PORT"`
	ReadTimeout      time.Duration `mapstructure:"read_timeout" yaml:"read_timeout" env:"FORGE_SERVER_READ_TIMEOUT"`
	WriteTimeout     time.Duration `mapstructure:"write_timeout" yaml:"write_timeout" env:"FORGE_SERVER_WRITE_TIMEOUT"`
	IdleTimeout      time.Duration `mapstructure:"idle_timeout" yaml:"idle_timeout" env:"FORGE_SERVER_IDLE_TIMEOUT"`
	ShutdownTimeout  time.Duration `mapstructure:"shutdown_timeout" yaml:"shutdown_timeout" env:"FORGE_SERVER_SHUTDOWN_TIMEOUT"`
	EnableHTTP2      bool          `mapstructure:"enable_http2" yaml:"enable_http2" env:"FORGE_SERVER_ENABLE_HTTP2"`
	GracefulShutdown bool          `mapstructure:"graceful_shutdown" yaml:"graceful_shutdown" env:"FORGE_SERVER_GRACEFUL_SHUTDOWN"`
	TLS              TLSConfig     `mapstructure:"tls" yaml:"tls"`
}

// TLSConfig represents TLS configuration
type TLSConfig struct {
	Enabled  bool   `mapstructure:"enabled" yaml:"enabled" env:"FORGE_TLS_ENABLED"`
	CertFile string `mapstructure:"cert_file" yaml:"cert_file" env:"FORGE_TLS_CERT_FILE"`
	KeyFile  string `mapstructure:"key_file" yaml:"key_file" env:"FORGE_TLS_KEY_FILE"`
}

// AppConfig represents core application configuration
type AppConfig struct {
	Name        string `mapstructure:"name" yaml:"name" env:"FORGE_APP_NAME"`
	Version     string `mapstructure:"version" yaml:"version" env:"FORGE_APP_VERSION"`
	Environment string `mapstructure:"environment" yaml:"environment" env:"FORGE_ENVIRONMENT"`
	Debug       bool   `mapstructure:"debug" yaml:"debug" env:"FORGE_DEBUG"`
	BaseURL     string `mapstructure:"base_url" yaml:"base_url" env:"FORGE_BASE_URL"`
}

// Error types
var (
	ErrComponentNotFound    = fmt.Errorf("component not found")
	ErrInvalidConfiguration = fmt.Errorf("invalid configuration")
	ErrDependencyNotFound   = fmt.Errorf("dependency not found")
	ErrCircularDependency   = fmt.Errorf("circular dependency detected")
	ErrServiceAlreadyExists = fmt.Errorf("service already exists")
)

// Constants for component names
const (
	ComponentDatabase       = "database"
	ComponentCache          = "cache"
	ComponentLogger         = "logger"
	ComponentConfig         = "config"
	ComponentApp            = "app"
	ComponentMetrics        = "metrics"
	ComponentTracer         = "tracer"
	ComponentHealth         = "health"
	ComponentJobs           = "jobs"
	ComponentPlugins        = "plugin"
	ComponentRouter         = "router"
	ComponentPluginsSystem  = "pluginSystem"
	ComponentEventBus       = "eventbus"
	ComponentServiceManager = "serviceManager"
)
