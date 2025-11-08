package backends

import (
	"context"
	"slices"
	"time"
)

// Backend defines the interface for service discovery backends.
type Backend interface {
	// Name returns the backend name
	Name() string

	// Initialize initializes the backend
	Initialize(ctx context.Context) error

	// Register registers a service instance
	Register(ctx context.Context, instance *ServiceInstance) error

	// Deregister deregisters a service instance
	Deregister(ctx context.Context, serviceID string) error

	// Discover discovers service instances by name
	Discover(ctx context.Context, serviceName string) ([]*ServiceInstance, error)

	// DiscoverWithTags discovers service instances by name and tags
	DiscoverWithTags(ctx context.Context, serviceName string, tags []string) ([]*ServiceInstance, error)

	// Watch watches for changes to a service
	Watch(ctx context.Context, serviceName string, onChange func([]*ServiceInstance)) error

	// ListServices lists all registered services
	ListServices(ctx context.Context) ([]string, error)

	// Health checks backend health
	Health(ctx context.Context) error

	// Close closes the backend
	Close() error
}

// ServiceInstance represents a registered service instance.
type ServiceInstance struct {
	// ID is the unique service instance ID
	ID string `json:"id"`

	// Name is the service name
	Name string `json:"name"`

	// Version is the service version
	Version string `json:"version"`

	// Address is the service address (IP or hostname)
	Address string `json:"address"`

	// Port is the service port
	Port int `json:"port"`

	// Tags are service tags for filtering
	Tags []string `json:"tags"`

	// Metadata is arbitrary service metadata
	Metadata map[string]string `json:"metadata"`

	// Status is the service health status
	Status HealthStatus `json:"status"`

	// LastHeartbeat is the timestamp of the last heartbeat
	LastHeartbeat int64 `json:"last_heartbeat"`
}

// HealthStatus represents service health status.
type HealthStatus string

const (
	// HealthStatusPassing indicates the service is healthy.
	HealthStatusPassing HealthStatus = "passing"

	// HealthStatusWarning indicates the service has warnings.
	HealthStatusWarning HealthStatus = "warning"

	// HealthStatusCritical indicates the service is unhealthy.
	HealthStatusCritical HealthStatus = "critical"

	// HealthStatusUnknown indicates the health status is unknown.
	HealthStatusUnknown HealthStatus = "unknown"
)

// URL returns the full URL for the service instance.
func (si *ServiceInstance) URL(scheme string) string {
	if scheme == "" {
		scheme = "http"
	}

	return scheme + "://" + si.Address + ":" + string(rune(si.Port))
}

// HasTag checks if the service has a specific tag.
func (si *ServiceInstance) HasTag(tag string) bool {
	return slices.Contains(si.Tags, tag)
}

// HasAllTags checks if the service has all specified tags.
func (si *ServiceInstance) HasAllTags(tags []string) bool {
	for _, tag := range tags {
		if !si.HasTag(tag) {
			return false
		}
	}

	return true
}

// IsHealthy checks if the service is healthy.
func (si *ServiceInstance) IsHealthy() bool {
	return si.Status == HealthStatusPassing
}

// GetMetadata retrieves metadata by key.
func (si *ServiceInstance) GetMetadata(key string) (string, bool) {
	val, ok := si.Metadata[key]

	return val, ok
}

// ConsulConfig holds Consul-specific configuration.
type ConsulConfig struct {
	// Address is the Consul agent address
	Address string `json:"address" yaml:"address"`

	// Token is the Consul ACL token
	Token string `json:"token" yaml:"token"`

	// Datacenter is the Consul datacenter
	Datacenter string `json:"datacenter" yaml:"datacenter"`

	// Namespace is the Consul namespace (Enterprise)
	Namespace string `json:"namespace" yaml:"namespace"`

	// TLS settings
	TLSEnabled bool   `json:"tls_enabled" yaml:"tls_enabled"`
	TLSCAFile  string `json:"tls_ca_file" yaml:"tls_ca_file"`
	TLSCert    string `json:"tls_cert"    yaml:"tls_cert"`
	TLSKey     string `json:"tls_key"     yaml:"tls_key"`
}

// EtcdConfig holds etcd-specific configuration.
type EtcdConfig struct {
	// Endpoints are the etcd endpoints
	Endpoints []string `json:"endpoints" yaml:"endpoints"`

	// Username for etcd authentication
	Username string `json:"username" yaml:"username"`

	// Password for etcd authentication
	Password string `json:"password" yaml:"password"`

	// DialTimeout is the timeout for dialing etcd
	DialTimeout time.Duration `json:"dial_timeout" yaml:"dial_timeout"`

	// KeyPrefix is the etcd key prefix for services
	KeyPrefix string `json:"key_prefix" yaml:"key_prefix"`

	// TLS settings
	TLSEnabled bool   `json:"tls_enabled" yaml:"tls_enabled"`
	TLSCAFile  string `json:"tls_ca_file" yaml:"tls_ca_file"`
	TLSCert    string `json:"tls_cert"    yaml:"tls_cert"`
	TLSKey     string `json:"tls_key"     yaml:"tls_key"`
}

// KubernetesConfig holds Kubernetes-specific configuration.
type KubernetesConfig struct {
	// Namespace is the Kubernetes namespace
	Namespace string `json:"namespace" yaml:"namespace"`

	// InCluster determines if running in-cluster
	InCluster bool `json:"in_cluster" yaml:"in_cluster"`

	// KubeconfigPath is the path to kubeconfig (if not in-cluster)
	KubeconfigPath string `json:"kubeconfig_path" yaml:"kubeconfig_path"`

	// ServiceType is the service type to watch (ClusterIP, NodePort, LoadBalancer)
	ServiceType string `json:"service_type" yaml:"service_type"`

	// LabelSelector is the label selector for filtering services
	LabelSelector string `json:"label_selector" yaml:"label_selector"`
}

// EurekaConfig holds Eureka-specific configuration.
type EurekaConfig struct {
	// URLs are the Eureka server URLs
	URLs []string `json:"urls" yaml:"urls"`

	// RegistryFetchInterval is how often to fetch the registry
	RegistryFetchInterval time.Duration `json:"registry_fetch_interval" yaml:"registry_fetch_interval"`

	// InstanceInfoReplicationInterval is how often to replicate instance info
	InstanceInfoReplicationInterval time.Duration `json:"instance_info_replication_interval" yaml:"instance_info_replication_interval"`
}

// MDNSConfig holds mDNS/DNS-SD-specific configuration.
type MDNSConfig struct {
	// Domain is the mDNS domain (default: "local.")
	Domain string `json:"domain" yaml:"domain"`

	// ServiceType is the mDNS service type for registration (e.g., "_octopus._tcp", "_farp._tcp")
	// If empty, defaults to "_{service-name}._tcp"
	// This is used when registering a single service
	ServiceType string `json:"service_type" yaml:"service_type"`

	// ServiceTypes is a list of service types to discover (gateway/mesh use case)
	// Example: ["_octopus._tcp", "_farp._tcp", "_http._tcp"]
	// Used for discovering multiple service types simultaneously
	ServiceTypes []string `json:"service_types" yaml:"service_types"`

	// WatchInterval is how often to poll for service changes (default: 30s)
	WatchInterval time.Duration `json:"watch_interval" yaml:"watch_interval"`

	// Interface is the network interface to use (empty for all interfaces)
	Interface string `json:"interface" yaml:"interface"`

	// IPv6 enables IPv6 support
	IPv6 bool `json:"ipv6" yaml:"ipv6"`

	// BrowseTimeout is the timeout for browsing services (default: 3s)
	BrowseTimeout time.Duration `json:"browse_timeout" yaml:"browse_timeout"`

	// TTL is the time-to-live for service records (default: 120 seconds)
	TTL uint32 `json:"ttl" yaml:"ttl"`
}
