package discovery

import (
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/discovery/backends"
)

// DI container keys for discovery extension services.
const (
	// ServiceKey is the DI key for the discovery service.
	ServiceKey = "discovery"
	// ServiceKeyLegacy is the legacy DI key for the discovery service.
	ServiceKeyLegacy = "discovery.Service"
)

// Config holds service discovery extension configuration.
type Config struct {
	// Enabled determines if service discovery is enabled
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Backend specifies the service discovery backend
	// Options: "consul", "etcd", "kubernetes", "eureka", "memory"
	Backend string `json:"backend" yaml:"backend"`

	// Service configuration for this instance
	Service ServiceConfig `json:"service" yaml:"service"`

	// Health check settings
	HealthCheck HealthCheckConfig `json:"health_check" yaml:"health_check"`

	// Watch settings
	Watch WatchConfig `json:"watch" yaml:"watch"`

	// Consul-specific settings
	Consul ConsulConfig `json:"consul" yaml:"consul"`

	// Etcd-specific settings
	Etcd EtcdConfig `json:"etcd" yaml:"etcd"`

	// Kubernetes-specific settings
	Kubernetes KubernetesConfig `json:"kubernetes" yaml:"kubernetes"`

	// Eureka-specific settings
	Eureka EurekaConfig `json:"eureka" yaml:"eureka"`

	// MDNS-specific settings
	MDNS MDNSConfig `json:"mdns" yaml:"mdns"`

	// FARP (Forge API Gateway Registration Protocol) settings
	FARP FARPConfig `json:"farp" yaml:"farp"`
}

// FARPConfig holds FARP (Forge API Gateway Registration Protocol) configuration.
type FARPConfig struct {
	// Enabled determines if FARP schema registration is enabled
	Enabled bool `json:"enabled" yaml:"enabled"`

	// AutoRegister automatically registers schemas on service startup
	AutoRegister bool `json:"auto_register" yaml:"auto_register"`

	// Strategy for schema storage: "push" (to registry), "pull" (HTTP only), "hybrid"
	Strategy string `json:"strategy" yaml:"strategy"`

	// Schemas to register
	Schemas []FARPSchemaConfig `json:"schemas" yaml:"schemas"`

	// Endpoints configuration
	Endpoints FARPEndpointsConfig `json:"endpoints" yaml:"endpoints"`

	// Capabilities advertised by this service
	Capabilities []string `json:"capabilities" yaml:"capabilities"`
}

// FARPSchemaConfig configures a single schema to register.
type FARPSchemaConfig struct {
	// Type of schema: "openapi", "asyncapi", "grpc", "graphql"
	Type string `json:"type" yaml:"type"`

	// SpecVersion is the schema specification version (e.g., "3.1.0" for OpenAPI)
	SpecVersion string `json:"spec_version" yaml:"spec_version"`

	// Location configuration
	Location FARPLocationConfig `json:"location" yaml:"location"`

	// ContentType is the schema content type
	ContentType string `json:"content_type" yaml:"content_type"`
}

// FARPLocationConfig configures where the schema is located.
type FARPLocationConfig struct {
	// Type: "http", "registry", "inline"
	Type string `json:"type" yaml:"type"`

	// URL for HTTP location type
	URL string `json:"url,omitempty" yaml:"url,omitempty"`

	// RegistryPath for registry location type
	RegistryPath string `json:"registry_path,omitempty" yaml:"registry_path,omitempty"`

	// Headers for HTTP authentication
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
}

// FARPEndpointsConfig configures service endpoints.
type FARPEndpointsConfig struct {
	// Health check endpoint
	Health string `json:"health" yaml:"health"`

	// Metrics endpoint
	Metrics string `json:"metrics,omitempty" yaml:"metrics,omitempty"`

	// OpenAPI spec endpoint
	OpenAPI string `json:"openapi,omitempty" yaml:"openapi,omitempty"`

	// AsyncAPI spec endpoint
	AsyncAPI string `json:"asyncapi,omitempty" yaml:"asyncapi,omitempty"`

	// gRPC reflection enabled
	GRPCReflection bool `json:"grpc_reflection,omitempty" yaml:"grpc_reflection,omitempty"`

	// GraphQL endpoint
	GraphQL string `json:"graphql,omitempty" yaml:"graphql,omitempty"`
}

// ServiceConfig holds service registration configuration.
type ServiceConfig struct {
	// Name is the service name
	Name string `json:"name" yaml:"name"`

	// ID is the unique service instance ID (auto-generated if empty)
	ID string `json:"id" yaml:"id"`

	// Version is the service version
	Version string `json:"version" yaml:"version"`

	// Address is the service address
	Address string `json:"address" yaml:"address"`

	// Port is the service port
	Port int `json:"port" yaml:"port"`

	// Tags are service tags for filtering
	Tags []string `json:"tags" yaml:"tags"`

	// Metadata is arbitrary service metadata
	Metadata map[string]string `json:"metadata" yaml:"metadata"`

	// EnableAutoDeregister enables automatic deregistration on shutdown
	EnableAutoDeregister bool `json:"enable_auto_deregister" yaml:"enable_auto_deregister"`
}

// HealthCheckConfig holds health check configuration.
type HealthCheckConfig struct {
	// Enabled determines if health checks are enabled
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Interval is how often to perform health checks
	Interval time.Duration `json:"interval" yaml:"interval"`

	// Timeout is the health check timeout
	Timeout time.Duration `json:"timeout" yaml:"timeout"`

	// DeregisterCriticalServiceAfter is when to deregister unhealthy services
	DeregisterCriticalServiceAfter time.Duration `json:"deregister_critical_service_after" yaml:"deregister_critical_service_after"`

	// HTTP health check endpoint (if using HTTP)
	HTTP string `json:"http" yaml:"http"`

	// TCP health check address (if using TCP)
	TCP string `json:"tcp" yaml:"tcp"`

	// gRPC health check address (if using gRPC)
	GRPC string `json:"grpc" yaml:"grpc"`
}

// WatchConfig holds service watch configuration.
type WatchConfig struct {
	// Enabled determines if service watching is enabled
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Services are service names to watch for changes
	Services []string `json:"services" yaml:"services"`

	// Tags are tags to filter services
	Tags []string `json:"tags" yaml:"tags"`

	// OnChange is called when watched services change
	// This is set programmatically, not via config
	OnChange func(services []*ServiceInstance)
}

// ConsulConfig holds Consul-specific configuration.
type ConsulConfig = backends.ConsulConfig

// EtcdConfig holds etcd-specific configuration.
type EtcdConfig = backends.EtcdConfig

// KubernetesConfig holds Kubernetes-specific configuration.
type KubernetesConfig = backends.KubernetesConfig

// EurekaConfig holds Eureka-specific configuration.
type EurekaConfig = backends.EurekaConfig

// MDNSConfig holds mDNS/DNS-SD-specific configuration
// Works natively on macOS (Bonjour), Linux (Avahi), and Windows (DNS-SD).
type MDNSConfig = backends.MDNSConfig

// DefaultConfig returns the default service discovery configuration.
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

// ConfigOption configures the service discovery extension.
type ConfigOption func(*Config)

// WithEnabled sets whether service discovery is enabled.
func WithEnabled(enabled bool) ConfigOption {
	return func(c *Config) {
		c.Enabled = enabled
	}
}

// WithBackend sets the service discovery backend.
func WithBackend(backend string) ConfigOption {
	return func(c *Config) {
		c.Backend = backend
	}
}

// WithService sets the service configuration.
func WithService(service ServiceConfig) ConfigOption {
	return func(c *Config) {
		c.Service = service
	}
}

// WithServiceName sets the service name.
func WithServiceName(name string) ConfigOption {
	return func(c *Config) {
		c.Service.Name = name
	}
}

// WithServiceAddress sets the service address and port.
func WithServiceAddress(address string, port int) ConfigOption {
	return func(c *Config) {
		c.Service.Address = address
		c.Service.Port = port
	}
}

// WithServiceTags sets the service tags.
func WithServiceTags(tags ...string) ConfigOption {
	return func(c *Config) {
		c.Service.Tags = tags
	}
}

// WithServiceMetadata sets the service metadata.
func WithServiceMetadata(metadata map[string]string) ConfigOption {
	return func(c *Config) {
		c.Service.Metadata = metadata
	}
}

// WithHealthCheck sets the health check configuration.
func WithHealthCheck(config HealthCheckConfig) ConfigOption {
	return func(c *Config) {
		c.HealthCheck = config
	}
}

// WithHTTPHealthCheck sets HTTP health check.
func WithHTTPHealthCheck(endpoint string, interval time.Duration) ConfigOption {
	return func(c *Config) {
		c.HealthCheck.Enabled = true
		c.HealthCheck.HTTP = endpoint
		c.HealthCheck.Interval = interval
	}
}

// WithWatch enables service watching.
func WithWatch(services []string, onChange func([]*ServiceInstance)) ConfigOption {
	return func(c *Config) {
		c.Watch.Enabled = true
		c.Watch.Services = services
		c.Watch.OnChange = onChange
	}
}

// WithConsul configures Consul backend.
func WithConsul(address, token string) ConfigOption {
	return func(c *Config) {
		c.Backend = "consul"
		c.Consul.Address = address
		c.Consul.Token = token
	}
}

// WithEtcd configures etcd backend.
func WithEtcd(endpoints []string) ConfigOption {
	return func(c *Config) {
		c.Backend = "etcd"
		c.Etcd.Endpoints = endpoints
	}
}

// WithKubernetes configures Kubernetes backend.
func WithKubernetes(namespace string, inCluster bool) ConfigOption {
	return func(c *Config) {
		c.Backend = "kubernetes"
		c.Kubernetes.Namespace = namespace
		c.Kubernetes.InCluster = inCluster
	}
}

// WithEureka configures Eureka backend.
func WithEureka(urls []string) ConfigOption {
	return func(c *Config) {
		c.Backend = "eureka"
		c.Eureka.URLs = urls
	}
}

// WithMDNS configures mDNS backend.
func WithMDNS(domain string) ConfigOption {
	return func(c *Config) {
		c.Backend = "mdns"
		if domain != "" {
			c.MDNS.Domain = domain
		}
	}
}

// WithFARP sets the full FARP configuration.
func WithFARP(farp FARPConfig) ConfigOption {
	return func(c *Config) { c.FARP = farp }
}

// WithFARPEnabled sets whether FARP schema registration is enabled.
func WithFARPEnabled(enabled bool) ConfigOption {
	return func(c *Config) { c.FARP.Enabled = enabled }
}

// WithFARPAutoRegister sets whether schemas are automatically registered on startup.
func WithFARPAutoRegister(autoRegister bool) ConfigOption {
	return func(c *Config) { c.FARP.AutoRegister = autoRegister }
}

// WithFARPStrategy sets the FARP registration strategy (push, pull, or hybrid).
func WithFARPStrategy(strategy string) ConfigOption {
	return func(c *Config) { c.FARP.Strategy = strategy }
}

// WithFARPSchemas sets the FARP schema configurations.
func WithFARPSchemas(schemas ...FARPSchemaConfig) ConfigOption {
	return func(c *Config) { c.FARP.Schemas = schemas }
}

// WithFARPEndpoints sets the FARP endpoint configuration.
func WithFARPEndpoints(endpoints FARPEndpointsConfig) ConfigOption {
	return func(c *Config) { c.FARP.Endpoints = endpoints }
}

// WithFARPCapabilities sets the FARP capability tags.
func WithFARPCapabilities(capabilities ...string) ConfigOption {
	return func(c *Config) { c.FARP.Capabilities = capabilities }
}

// WithMDNSServiceType sets the mDNS service type for registration.
func WithMDNSServiceType(serviceType string) ConfigOption {
	return func(c *Config) { c.MDNS.ServiceType = serviceType }
}

// WithMDNSServiceTypes sets the mDNS service types to browse for discovery.
func WithMDNSServiceTypes(types ...string) ConfigOption {
	return func(c *Config) { c.MDNS.ServiceTypes = types }
}

// WithMDNSWatchInterval sets the mDNS watch polling interval.
func WithMDNSWatchInterval(d time.Duration) ConfigOption {
	return func(c *Config) { c.MDNS.WatchInterval = d }
}

// WithMDNSBrowseTimeout sets the mDNS browse operation timeout.
func WithMDNSBrowseTimeout(d time.Duration) ConfigOption {
	return func(c *Config) { c.MDNS.BrowseTimeout = d }
}

// WithMDNSTTL sets the mDNS DNS record time-to-live.
func WithMDNSTTL(ttl uint32) ConfigOption {
	return func(c *Config) { c.MDNS.TTL = ttl }
}

// WithMDNSInterface sets the network interface for mDNS operations.
func WithMDNSInterface(iface string) ConfigOption {
	return func(c *Config) { c.MDNS.Interface = iface }
}

// WithMDNSIPv6 sets whether mDNS uses IPv6.
func WithMDNSIPv6(enabled bool) ConfigOption {
	return func(c *Config) { c.MDNS.IPv6 = enabled }
}

// WithServiceID sets the unique service instance identifier.
func WithServiceID(id string) ConfigOption {
	return func(c *Config) { c.Service.ID = id }
}

// WithServiceVersion sets the service version string.
func WithServiceVersion(version string) ConfigOption {
	return func(c *Config) { c.Service.Version = version }
}

// WithAutoDeregister sets whether the service is automatically deregistered on shutdown.
func WithAutoDeregister(enabled bool) ConfigOption {
	return func(c *Config) { c.Service.EnableAutoDeregister = enabled }
}

// WithConfig sets the complete config.
func WithConfig(config Config) ConfigOption {
	return func(c *Config) {
		*c = config
	}
}

// WithAppConfig sets the app config for auto-detecting service info
// This allows the extension to read Name, Version, and HTTPAddress from the app config
// when Service config is not fully provided.
func WithAppConfig(appConfig forge.AppConfig) ConfigOption {
	return func(c *Config) {
		// Store in metadata for later extraction in Register
		if c.Service.Metadata == nil {
			c.Service.Metadata = make(map[string]string)
		}

		c.Service.Metadata["_app_config_name"] = appConfig.Name
		c.Service.Metadata["_app_config_version"] = appConfig.Version
		c.Service.Metadata["_app_config_http_address"] = appConfig.HTTPAddress
	}
}
