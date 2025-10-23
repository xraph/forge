package lb

import (
	"context"
	"time"
)

// LoadBalancer selects nodes for connections.
type LoadBalancer interface {
	// SelectNode chooses node for new connection
	SelectNode(ctx context.Context, userID string, metadata map[string]any) (*NodeInfo, error)

	// GetNode returns node for existing connection
	GetNode(ctx context.Context, connID string) (*NodeInfo, error)

	// RegisterNode adds node to pool
	RegisterNode(ctx context.Context, node *NodeInfo) error

	// UnregisterNode removes node
	UnregisterNode(ctx context.Context, nodeID string) error

	// Health checks node health
	Health(ctx context.Context, nodeID string) error

	// GetNodes returns all registered nodes
	GetNodes(ctx context.Context) ([]*NodeInfo, error)

	// GetHealthyNodes returns only healthy nodes
	GetHealthyNodes(ctx context.Context) ([]*NodeInfo, error)
}

// NodeInfo represents a server node.
type NodeInfo struct {
	ID       string
	Address  string
	Port     int
	Weight   int
	Metadata map[string]any

	// Geographic info
	Region    string
	Zone      string
	Latitude  float64
	Longitude float64

	// Health info
	Healthy         bool
	LastHealthCheck time.Time
	FailureCount    int

	// Load info
	ConnectionCount int
	CPU             float64
	Memory          float64
}

// LoadBalancerConfig configures load balancing.
type LoadBalancerConfig struct {
	Strategy      Strategy
	HealthCheck   HealthCheckConfig
	StickySession StickySessionConfig
}

// Strategy defines load balancing strategy.
type Strategy string

const (
	StrategyRoundRobin       Strategy = "round_robin"
	StrategyLeastConnections Strategy = "least_connections"
	StrategyConsistentHash   Strategy = "consistent_hash"
	StrategySticky           Strategy = "sticky"
	StrategyWeighted         Strategy = "weighted"
	StrategyGeoProximity     Strategy = "geo_proximity"
)

// HealthCheckConfig configures health checking.
type HealthCheckConfig struct {
	Enabled       bool
	Interval      time.Duration
	Timeout       time.Duration
	FailThreshold int
	PassThreshold int
}

// StickySessionConfig configures sticky sessions.
type StickySessionConfig struct {
	Enabled    bool
	TTL        time.Duration
	CookieName string
}

// DefaultLoadBalancerConfig returns default configuration.
func DefaultLoadBalancerConfig() LoadBalancerConfig {
	return LoadBalancerConfig{
		Strategy: StrategyRoundRobin,
		HealthCheck: HealthCheckConfig{
			Enabled:       true,
			Interval:      10 * time.Second,
			Timeout:       5 * time.Second,
			FailThreshold: 3,
			PassThreshold: 2,
		},
		StickySession: StickySessionConfig{
			Enabled:    true,
			TTL:        time.Hour,
			CookieName: "lb_session",
		},
	}
}

// NodeStore persists node information.
type NodeStore interface {
	// Save stores node info
	Save(ctx context.Context, node *NodeInfo) error

	// Get retrieves node info
	Get(ctx context.Context, nodeID string) (*NodeInfo, error)

	// List lists all nodes
	List(ctx context.Context) ([]*NodeInfo, error)

	// Delete removes node
	Delete(ctx context.Context, nodeID string) error

	// UpdateHealth updates node health status
	UpdateHealth(ctx context.Context, nodeID string, healthy bool) error

	// UpdateConnectionCount updates connection count
	UpdateConnectionCount(ctx context.Context, nodeID string, count int) error
}

// SessionStore persists session affinity.
type SessionStore interface {
	// Set creates session affinity
	Set(ctx context.Context, userID, nodeID string, ttl time.Duration) error

	// Get retrieves session affinity
	Get(ctx context.Context, userID string) (string, error)

	// Delete removes session affinity
	Delete(ctx context.Context, userID string) error
}
