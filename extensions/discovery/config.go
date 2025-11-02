package discovery

import (
	"time"

	"github.com/xraph/forge/extensions/discovery/backends"
)

// Config holds service discovery extension configuration
type Config struct {
	// Enabled determines if service discovery is enabled
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Backend specifies the service discovery backend
	// Options: "consul", "etcd", "kubernetes", "eureka", "memory"
	Backend string `yaml:"backend" json:"backend"`

	// Service configuration for this instance
	Service ServiceConfig `yaml:"service" json:"service"`

	// Health check settings
	HealthCheck HealthCheckConfig `yaml:"health_check" json:"health_check"`

	// Watch settings
	Watch WatchConfig `yaml:"watch" json:"watch"`

	// Consul-specific settings
	Consul ConsulConfig `yaml:"consul" json:"consul"`

	// Etcd-specific settings
	Etcd EtcdConfig `yaml:"etcd" json:"etcd"`

	// Kubernetes-specific settings
	Kubernetes KubernetesConfig `yaml:"kubernetes" json:"kubernetes"`

	// Eureka-specific settings
	Eureka EurekaConfig `yaml:"eureka" json:"eureka"`

	// MDNS-specific settings
	MDNS MDNSConfig `yaml:"mdns" json:"mdns"`

	// FARP (Forge API Gateway Registration Protocol) settings
	FARP FARPConfig `yaml:"farp" json:"farp"`
}

// FARPConfig holds FARP (Forge API Gateway Registration Protocol) configuration
type FARPConfig struct {
	// Enabled determines if FARP schema registration is enabled
	Enabled bool `yaml:"enabled" json:"enabled"`

	// AutoRegister automatically registers schemas on service startup
	AutoRegister bool `yaml:"auto_register" json:"auto_register"`

	// Strategy for schema storage: "push" (to registry), "pull" (HTTP only), "hybrid"
	Strategy string `yaml:"strategy" json:"strategy"`

	// Schemas to register
	Schemas []FARPSchemaConfig `yaml:"schemas" json:"schemas"`

	// Endpoints configuration
	Endpoints FARPEndpointsConfig `yaml:"endpoints" json:"endpoints"`

	// Capabilities advertised by this service
	Capabilities []string `yaml:"capabilities" json:"capabilities"`
}

// FARPSchemaConfig configures a single schema to register
type FARPSchemaConfig struct {
	// Type of schema: "openapi", "asyncapi", "grpc", "graphql"
	Type string `yaml:"type" json:"type"`

	// SpecVersion is the schema specification version (e.g., "3.1.0" for OpenAPI)
	SpecVersion string `yaml:"spec_version" json:"spec_version"`

	// Location configuration
	Location FARPLocationConfig `yaml:"location" json:"location"`

	// ContentType is the schema content type
	ContentType string `yaml:"content_type" json:"content_type"`
}

// FARPLocationConfig configures where the schema is located
type FARPLocationConfig struct {
	// Type: "http", "registry", "inline"
	Type string `yaml:"type" json:"type"`

	// URL for HTTP location type
	URL string `yaml:"url,omitempty" json:"url,omitempty"`

	// RegistryPath for registry location type
	RegistryPath string `yaml:"registry_path,omitempty" json:"registry_path,omitempty"`

	// Headers for HTTP authentication
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
}

// FARPEndpointsConfig configures service endpoints
type FARPEndpointsConfig struct {
	// Health check endpoint
	Health string `yaml:"health" json:"health"`

	// Metrics endpoint
	Metrics string `yaml:"metrics,omitempty" json:"metrics,omitempty"`

	// OpenAPI spec endpoint
	OpenAPI string `yaml:"openapi,omitempty" json:"openapi,omitempty"`

	// AsyncAPI spec endpoint
	AsyncAPI string `yaml:"asyncapi,omitempty" json:"asyncapi,omitempty"`

	// gRPC reflection enabled
	GRPCReflection bool `yaml:"grpc_reflection,omitempty" json:"grpc_reflection,omitempty"`

	// GraphQL endpoint
	GraphQL string `yaml:"graphql,omitempty" json:"graphql,omitempty"`
}

// ServiceConfig holds service registration configuration
type ServiceConfig struct {
	// Name is the service name
	Name string `yaml:"name" json:"name"`

	// ID is the unique service instance ID (auto-generated if empty)
	ID string `yaml:"id" json:"id"`

	// Version is the service version
	Version string `yaml:"version" json:"version"`

	// Address is the service address
	Address string `yaml:"address" json:"address"`

	// Port is the service port
	Port int `yaml:"port" json:"port"`

	// Tags are service tags for filtering
	Tags []string `yaml:"tags" json:"tags"`

	// Metadata is arbitrary service metadata
	Metadata map[string]string `yaml:"metadata" json:"metadata"`

	// EnableAutoDeregister enables automatic deregistration on shutdown
	EnableAutoDeregister bool `yaml:"enable_auto_deregister" json:"enable_auto_deregister"`
}

// HealthCheckConfig holds health check configuration
type HealthCheckConfig struct {
	// Enabled determines if health checks are enabled
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Interval is how often to perform health checks
	Interval time.Duration `yaml:"interval" json:"interval"`

	// Timeout is the health check timeout
	Timeout time.Duration `yaml:"timeout" json:"timeout"`

	// DeregisterCriticalServiceAfter is when to deregister unhealthy services
	DeregisterCriticalServiceAfter time.Duration `yaml:"deregister_critical_service_after" json:"deregister_critical_service_after"`

	// HTTP health check endpoint (if using HTTP)
	HTTP string `yaml:"http" json:"http"`

	// TCP health check address (if using TCP)
	TCP string `yaml:"tcp" json:"tcp"`

	// gRPC health check address (if using gRPC)
	GRPC string `yaml:"grpc" json:"grpc"`
}

// WatchConfig holds service watch configuration
type WatchConfig struct {
	// Enabled determines if service watching is enabled
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Services are service names to watch for changes
	Services []string `yaml:"services" json:"services"`

	// Tags are tags to filter services
	Tags []string `yaml:"tags" json:"tags"`

	// OnChange is called when watched services change
	// This is set programmatically, not via config
	OnChange func(services []*ServiceInstance)
}

// ConsulConfig holds Consul-specific configuration
type ConsulConfig = backends.ConsulConfig

// EtcdConfig holds etcd-specific configuration
type EtcdConfig = backends.EtcdConfig

// KubernetesConfig holds Kubernetes-specific configuration
type KubernetesConfig = backends.KubernetesConfig

// EurekaConfig holds Eureka-specific configuration
type EurekaConfig = backends.EurekaConfig

// MDNSConfig holds mDNS/DNS-SD-specific configuration
// Works natively on macOS (Bonjour), Linux (Avahi), and Windows (DNS-SD)
type MDNSConfig = backends.MDNSConfig

// DefaultConfig returns the default service discovery configuration
func DefaultConfig() Config {
	return Config{
		Enabled: true,
		Backend: "memory",
		Service: ServiceConfig{
			Tags:                 []string{},
			Metadata:             make(map[string]string),
			EnableAutoDeregister: true,
		},
		HealthCheck: HealthCheckConfig{
			Enabled:                        true,
			Interval:                       10 * time.Second,
			Timeout:                        5 * time.Second,
			DeregisterCriticalServiceAfter: 1 * time.Minute,
		},
		Watch: WatchConfig{
			Enabled:  false,
			Services: []string{},
			Tags:     []string{},
		},
		Consul: ConsulConfig{
			Address: "127.0.0.1:8500",
		},
		Etcd: EtcdConfig{
			Endpoints:   []string{"127.0.0.1:2379"},
			DialTimeout: 5 * time.Second,
			KeyPrefix:   "/services",
		},
		Kubernetes: KubernetesConfig{
			Namespace:   "default",
			InCluster:   true,
			ServiceType: "ClusterIP",
		},
		Eureka: EurekaConfig{
			RegistryFetchInterval:           30 * time.Second,
			InstanceInfoReplicationInterval: 30 * time.Second,
		},
		MDNS: MDNSConfig{
			Domain:        "local.",
			IPv6:          false,
			BrowseTimeout: 3 * time.Second,
			TTL:           120,
		},
		FARP: FARPConfig{
			Enabled:      false,
			AutoRegister: true,
			Strategy:     "push",
			Schemas:      []FARPSchemaConfig{},
			Endpoints: FARPEndpointsConfig{
				Health: "/health",
			},
			Capabilities: []string{},
		},
	}
}

// ConfigOption configures the service discovery extension
type ConfigOption func(*Config)

// WithEnabled sets whether service discovery is enabled
func WithEnabled(enabled bool) ConfigOption {
	return func(c *Config) {
		c.Enabled = enabled
	}
}

// WithBackend sets the service discovery backend
func WithBackend(backend string) ConfigOption {
	return func(c *Config) {
		c.Backend = backend
	}
}

// WithService sets the service configuration
func WithService(service ServiceConfig) ConfigOption {
	return func(c *Config) {
		c.Service = service
	}
}

// WithServiceName sets the service name
func WithServiceName(name string) ConfigOption {
	return func(c *Config) {
		c.Service.Name = name
	}
}

// WithServiceAddress sets the service address and port
func WithServiceAddress(address string, port int) ConfigOption {
	return func(c *Config) {
		c.Service.Address = address
		c.Service.Port = port
	}
}

// WithServiceTags sets the service tags
func WithServiceTags(tags ...string) ConfigOption {
	return func(c *Config) {
		c.Service.Tags = tags
	}
}

// WithServiceMetadata sets the service metadata
func WithServiceMetadata(metadata map[string]string) ConfigOption {
	return func(c *Config) {
		c.Service.Metadata = metadata
	}
}

// WithHealthCheck sets the health check configuration
func WithHealthCheck(config HealthCheckConfig) ConfigOption {
	return func(c *Config) {
		c.HealthCheck = config
	}
}

// WithHTTPHealthCheck sets HTTP health check
func WithHTTPHealthCheck(endpoint string, interval time.Duration) ConfigOption {
	return func(c *Config) {
		c.HealthCheck.Enabled = true
		c.HealthCheck.HTTP = endpoint
		c.HealthCheck.Interval = interval
	}
}

// WithWatch enables service watching
func WithWatch(services []string, onChange func([]*ServiceInstance)) ConfigOption {
	return func(c *Config) {
		c.Watch.Enabled = true
		c.Watch.Services = services
		c.Watch.OnChange = onChange
	}
}

// WithConsul configures Consul backend
func WithConsul(address, token string) ConfigOption {
	return func(c *Config) {
		c.Backend = "consul"
		c.Consul.Address = address
		c.Consul.Token = token
	}
}

// WithEtcd configures etcd backend
func WithEtcd(endpoints []string) ConfigOption {
	return func(c *Config) {
		c.Backend = "etcd"
		c.Etcd.Endpoints = endpoints
	}
}

// WithKubernetes configures Kubernetes backend
func WithKubernetes(namespace string, inCluster bool) ConfigOption {
	return func(c *Config) {
		c.Backend = "kubernetes"
		c.Kubernetes.Namespace = namespace
		c.Kubernetes.InCluster = inCluster
	}
}

// WithEureka configures Eureka backend
func WithEureka(urls []string) ConfigOption {
	return func(c *Config) {
		c.Backend = "eureka"
		c.Eureka.URLs = urls
	}
}

// WithMDNS configures mDNS backend
func WithMDNS(domain string) ConfigOption {
	return func(c *Config) {
		c.Backend = "mdns"
		if domain != "" {
			c.MDNS.Domain = domain
		}
	}
}

// WithConfig sets the complete config
func WithConfig(config Config) ConfigOption {
	return func(c *Config) {
		*c = config
	}
}

