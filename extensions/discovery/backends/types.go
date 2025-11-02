package backends

import (
	"context"
	"time"
)

// Backend defines the interface for service discovery backends
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

// ServiceInstance represents a registered service instance
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

// HealthStatus represents service health status
type HealthStatus string

const (
	// HealthStatusPassing indicates the service is healthy
	HealthStatusPassing HealthStatus = "passing"

	// HealthStatusWarning indicates the service has warnings
	HealthStatusWarning HealthStatus = "warning"

	// HealthStatusCritical indicates the service is unhealthy
	HealthStatusCritical HealthStatus = "critical"

	// HealthStatusUnknown indicates the health status is unknown
	HealthStatusUnknown HealthStatus = "unknown"
)

// URL returns the full URL for the service instance
func (si *ServiceInstance) URL(scheme string) string {
	if scheme == "" {
		scheme = "http"
	}
	return scheme + "://" + si.Address + ":" + string(rune(si.Port))
}

// HasTag checks if the service has a specific tag
func (si *ServiceInstance) HasTag(tag string) bool {
	for _, t := range si.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// HasAllTags checks if the service has all specified tags
func (si *ServiceInstance) HasAllTags(tags []string) bool {
	for _, tag := range tags {
		if !si.HasTag(tag) {
			return false
		}
	}
	return true
}

// IsHealthy checks if the service is healthy
func (si *ServiceInstance) IsHealthy() bool {
	return si.Status == HealthStatusPassing
}

// GetMetadata retrieves metadata by key
func (si *ServiceInstance) GetMetadata(key string) (string, bool) {
	val, ok := si.Metadata[key]
	return val, ok
}

// ConsulConfig holds Consul-specific configuration
type ConsulConfig struct {
	// Address is the Consul agent address
	Address string `yaml:"address" json:"address"`

	// Token is the Consul ACL token
	Token string `yaml:"token" json:"token"`

	// Datacenter is the Consul datacenter
	Datacenter string `yaml:"datacenter" json:"datacenter"`

	// Namespace is the Consul namespace (Enterprise)
	Namespace string `yaml:"namespace" json:"namespace"`

	// TLS settings
	TLSEnabled bool   `yaml:"tls_enabled" json:"tls_enabled"`
	TLSCAFile  string `yaml:"tls_ca_file" json:"tls_ca_file"`
	TLSCert    string `yaml:"tls_cert" json:"tls_cert"`
	TLSKey     string `yaml:"tls_key" json:"tls_key"`
}

// EtcdConfig holds etcd-specific configuration
type EtcdConfig struct {
	// Endpoints are the etcd endpoints
	Endpoints []string `yaml:"endpoints" json:"endpoints"`

	// Username for etcd authentication
	Username string `yaml:"username" json:"username"`

	// Password for etcd authentication
	Password string `yaml:"password" json:"password"`

	// DialTimeout is the timeout for dialing etcd
	DialTimeout time.Duration `yaml:"dial_timeout" json:"dial_timeout"`

	// KeyPrefix is the etcd key prefix for services
	KeyPrefix string `yaml:"key_prefix" json:"key_prefix"`

	// TLS settings
	TLSEnabled bool   `yaml:"tls_enabled" json:"tls_enabled"`
	TLSCAFile  string `yaml:"tls_ca_file" json:"tls_ca_file"`
	TLSCert    string `yaml:"tls_cert" json:"tls_cert"`
	TLSKey     string `yaml:"tls_key" json:"tls_key"`
}

// KubernetesConfig holds Kubernetes-specific configuration
type KubernetesConfig struct {
	// Namespace is the Kubernetes namespace
	Namespace string `yaml:"namespace" json:"namespace"`

	// InCluster determines if running in-cluster
	InCluster bool `yaml:"in_cluster" json:"in_cluster"`

	// KubeconfigPath is the path to kubeconfig (if not in-cluster)
	KubeconfigPath string `yaml:"kubeconfig_path" json:"kubeconfig_path"`

	// ServiceType is the service type to watch (ClusterIP, NodePort, LoadBalancer)
	ServiceType string `yaml:"service_type" json:"service_type"`

	// LabelSelector is the label selector for filtering services
	LabelSelector string `yaml:"label_selector" json:"label_selector"`
}

// EurekaConfig holds Eureka-specific configuration
type EurekaConfig struct {
	// URLs are the Eureka server URLs
	URLs []string `yaml:"urls" json:"urls"`

	// RegistryFetchInterval is how often to fetch the registry
	RegistryFetchInterval time.Duration `yaml:"registry_fetch_interval" json:"registry_fetch_interval"`

	// InstanceInfoReplicationInterval is how often to replicate instance info
	InstanceInfoReplicationInterval time.Duration `yaml:"instance_info_replication_interval" json:"instance_info_replication_interval"`
}

// MDNSConfig holds mDNS/DNS-SD-specific configuration
type MDNSConfig struct {
	// Domain is the mDNS domain (default: "local.")
	Domain string `yaml:"domain" json:"domain"`

	// Interface is the network interface to use (empty for all interfaces)
	Interface string `yaml:"interface" json:"interface"`

	// IPv6 enables IPv6 support
	IPv6 bool `yaml:"ipv6" json:"ipv6"`

	// BrowseTimeout is the timeout for browsing services
	BrowseTimeout time.Duration `yaml:"browse_timeout" json:"browse_timeout"`

	// TTL is the time-to-live for service records (default: 120 seconds)
	TTL uint32 `yaml:"ttl" json:"ttl"`
}
