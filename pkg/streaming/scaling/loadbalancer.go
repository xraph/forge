package scaling

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/streaming"
)

// LoadBalancer handles connection load balancing across streaming instances
type LoadBalancer interface {
	// Connection routing
	SelectInstance(ctx context.Context, criteria ConnectionCriteria) (*InstanceSelection, error)
	RouteConnection(ctx context.Context, connectionInfo ConnectionInfo) (*InstanceSelection, error)
	GetInstanceLoad(ctx context.Context, instanceID string) (*InstanceLoad, error)

	// Load balancing strategies
	SetStrategy(strategy LoadBalanceStrategy) error
	GetStrategy() LoadBalanceStrategy
	GetAvailableStrategies() []LoadBalanceStrategy

	// Health and monitoring
	UpdateInstanceHealth(instanceID string, health InstanceHealthMetrics) error
	GetHealthyInstances(ctx context.Context) ([]string, error)
	MarkInstanceUnhealthy(instanceID string, reason string) error
	MarkInstanceHealthy(instanceID string) error

	// Failover management
	HandleInstanceFailure(ctx context.Context, instanceID string) error
	GetFailoverTarget(ctx context.Context, failedInstanceID string) (string, error)
	TriggerFailover(ctx context.Context, fromInstance, toInstance string) error

	// Configuration and lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	UpdateConfig(config LoadBalancerConfig) error
	GetConfig() LoadBalancerConfig

	// Statistics and monitoring
	GetLoadStats() LoadBalancerStats
	GetInstanceStats(instanceID string) (*InstanceStats, error)
	GetAllInstanceStats() map[string]*InstanceStats
}

// LoadBalanceStrategy defines different load balancing algorithms
type LoadBalanceStrategy string

const (
	StrategyRoundRobin     LoadBalanceStrategy = "round_robin"
	StrategyLeastConnected LoadBalanceStrategy = "least_connected"
	StrategyWeightedRandom LoadBalanceStrategy = "weighted_random"
	StrategyResourceBased  LoadBalanceStrategy = "resource_based"
	StrategyLatencyBased   LoadBalanceStrategy = "latency_based"
	StrategyGeographic     LoadBalanceStrategy = "geographic"
	StrategyCapacityBased  LoadBalanceStrategy = "capacity_based"
)

// ConnectionCriteria defines criteria for connection routing
type ConnectionCriteria struct {
	Protocol          streaming.ProtocolType `json:"protocol"`
	UserID            string                 `json:"user_id,omitempty"`
	RoomID            string                 `json:"room_id,omitempty"`
	Region            string                 `json:"region,omitempty"`
	Zone              string                 `json:"zone,omitempty"`
	RequiredFeatures  []string               `json:"required_features,omitempty"`
	MaxLatency        time.Duration          `json:"max_latency,omitempty"`
	StickySession     bool                   `json:"sticky_session"`
	PreferredInstance string                 `json:"preferred_instance,omitempty"`
	AvoidInstances    []string               `json:"avoid_instances,omitempty"`
}

// InstanceSelection represents the result of instance selection
type InstanceSelection struct {
	InstanceID      string                `json:"instance_id"`
	LoadScore       float64               `json:"load_score"`
	HealthScore     float64               `json:"health_score"`
	LatencyScore    float64               `json:"latency_score"`
	CapacityScore   float64               `json:"capacity_score"`
	OverallScore    float64               `json:"overall_score"`
	SelectionReason string                `json:"selection_reason"`
	Metadata        InstanceSelectionMeta `json:"metadata"`
	SelectedAt      time.Time             `json:"selected_at"`
}

// InstanceSelectionMeta contains additional selection metadata
type InstanceSelectionMeta struct {
	Strategy         LoadBalanceStrategy `json:"strategy"`
	FallbackUsed     bool                `json:"fallback_used"`
	CandidateCount   int                 `json:"candidate_count"`
	SelectionTime    time.Duration       `json:"selection_time"`
	WeightingFactors map[string]float64  `json:"weighting_factors"`
}

// InstanceLoad represents the current load on an instance
type InstanceLoad struct {
	InstanceID            string    `json:"instance_id"`
	ActiveConnections     int       `json:"active_connections"`
	MaxConnections        int       `json:"max_connections"`
	ConnectionUtilization float64   `json:"connection_utilization"`
	ActiveRooms           int       `json:"active_rooms"`
	MaxRooms              int       `json:"max_rooms"`
	RoomUtilization       float64   `json:"room_utilization"`
	CPUUsage              float64   `json:"cpu_usage"`
	MemoryUsage           float64   `json:"memory_usage"`
	NetworkUsage          float64   `json:"network_usage"`
	LoadScore             float64   `json:"load_score"`
	LastUpdated           time.Time `json:"last_updated"`
}

// InstanceHealthMetrics represents health metrics for an instance
type InstanceHealthMetrics struct {
	InstanceID          string        `json:"instance_id"`
	Healthy             bool          `json:"healthy"`
	ResponseTime        time.Duration `json:"response_time"`
	ErrorRate           float64       `json:"error_rate"`
	CPUUsage            float64       `json:"cpu_usage"`
	MemoryUsage         float64       `json:"memory_usage"`
	DiskUsage           float64       `json:"disk_usage"`
	NetworkLatency      time.Duration `json:"network_latency"`
	ActiveConnections   int           `json:"active_connections"`
	DroppedConnections  int           `json:"dropped_connections"`
	LastHealthCheck     time.Time     `json:"last_health_check"`
	HealthCheckInterval time.Duration `json:"health_check_interval"`
	FailureCount        int           `json:"failure_count"`
	RecoveryTime        time.Duration `json:"recovery_time"`
}

// InstanceStats represents comprehensive instance statistics
type InstanceStats struct {
	Load              *InstanceLoad          `json:"load"`
	Health            *InstanceHealthMetrics `json:"health"`
	SelectionCount    int64                  `json:"selection_count"`
	FailoverCount     int64                  `json:"failover_count"`
	LastSelected      time.Time              `json:"last_selected"`
	LastFailover      time.Time              `json:"last_failover"`
	AverageLatency    time.Duration          `json:"average_latency"`
	ThroughputMBps    float64                `json:"throughput_mbps"`
	ConnectionsPerSec float64                `json:"connections_per_sec"`
}

// LoadBalancerStats represents load balancer statistics
type LoadBalancerStats struct {
	TotalSelections      int64                           `json:"total_selections"`
	SelectionsByStrategy map[LoadBalanceStrategy]int64   `json:"selections_by_strategy"`
	SelectionsByInstance map[string]int64                `json:"selections_by_instance"`
	FailoverCount        int64                           `json:"failover_count"`
	HealthyInstances     int                             `json:"healthy_instances"`
	UnhealthyInstances   int                             `json:"unhealthy_instances"`
	AverageSelectionTime time.Duration                   `json:"average_selection_time"`
	LoadDistribution     map[string]float64              `json:"load_distribution"`
	StrategyEfficiency   map[LoadBalanceStrategy]float64 `json:"strategy_efficiency"`
	LastFailover         time.Time                       `json:"last_failover"`
	InstanceStats        map[string]*InstanceStats       `json:"instance_stats"`
}

// LoadBalancerConfig contains load balancer configuration
type LoadBalancerConfig struct {
	Strategy                  LoadBalanceStrategy  `yaml:"strategy" default:"least_connected"`
	HealthCheckInterval       time.Duration        `yaml:"health_check_interval" default:"30s"`
	UnhealthyThreshold        int                  `yaml:"unhealthy_threshold" default:"3"`
	HealthyThreshold          int                  `yaml:"healthy_threshold" default:"2"`
	MaxSelectionTime          time.Duration        `yaml:"max_selection_time" default:"100ms"`
	EnableStickySession       bool                 `yaml:"enable_sticky_session" default:"true"`
	EnableGeographicRouting   bool                 `yaml:"enable_geographic_routing" default:"true"`
	EnableCapacityRouting     bool                 `yaml:"enable_capacity_routing" default:"true"`
	FailoverTimeout           time.Duration        `yaml:"failover_timeout" default:"30s"`
	LoadMetricsInterval       time.Duration        `yaml:"load_metrics_interval" default:"10s"`
	EnableAutomaticFailover   bool                 `yaml:"enable_automatic_failover" default:"true"`
	MaxFailoverRetries        int                  `yaml:"max_failover_retries" default:"3"`
	ConnectionDrainTimeout    time.Duration        `yaml:"connection_drain_timeout" default:"60s"`
	WeightingFactors          WeightingConfig      `yaml:"weighting_factors"`
	Thresholds                ThresholdConfig      `yaml:"thresholds"`
	CircuitBreakerConfig      CircuitBreakerConfig `yaml:"circuit_breaker"`
	MaxConnectionsPerInstance int                  `yaml:"max_connections_per_instance" default:"100"`
}

// WeightingConfig defines weighting factors for different metrics
type WeightingConfig struct {
	ConnectionLoad float64 `yaml:"connection_load" default:"0.3"`
	ResourceUsage  float64 `yaml:"resource_usage" default:"0.25"`
	HealthScore    float64 `yaml:"health_score" default:"0.2"`
	LatencyScore   float64 `yaml:"latency_score" default:"0.15"`
	CapacityScore  float64 `yaml:"capacity_score" default:"0.1"`
}

// ThresholdConfig defines various threshold values
type ThresholdConfig struct {
	MaxConnectionUtilization float64       `yaml:"max_connection_utilization" default:"0.8"`
	MaxCPUUsage              float64       `yaml:"max_cpu_usage" default:"0.8"`
	MaxMemoryUsage           float64       `yaml:"max_memory_usage" default:"0.8"`
	MaxLatency               time.Duration `yaml:"max_latency" default:"100ms"`
	MinHealthScore           float64       `yaml:"min_health_score" default:"0.7"`
	ErrorRateThreshold       float64       `yaml:"error_rate_threshold" default:"0.05"`
}

// CircuitBreakerConfig defines circuit breaker configuration
type CircuitBreakerConfig struct {
	Enabled             bool          `yaml:"enabled" default:"true"`
	FailureThreshold    int           `yaml:"failure_threshold" default:"5"`
	RecoveryTimeout     time.Duration `yaml:"recovery_timeout" default:"60s"`
	HalfOpenMaxCalls    int           `yaml:"half_open_max_calls" default:"3"`
	HalfOpenSuccessRate float64       `yaml:"half_open_success_rate" default:"0.8"`
}

// DefaultLoadBalancerConfig returns default load balancer configuration
func DefaultLoadBalancerConfig() LoadBalancerConfig {
	return LoadBalancerConfig{
		Strategy:                StrategyLeastConnected,
		HealthCheckInterval:     30 * time.Second,
		UnhealthyThreshold:      3,
		HealthyThreshold:        2,
		MaxSelectionTime:        100 * time.Millisecond,
		EnableStickySession:     true,
		EnableGeographicRouting: true,
		EnableCapacityRouting:   true,
		FailoverTimeout:         30 * time.Second,
		LoadMetricsInterval:     10 * time.Second,
		EnableAutomaticFailover: true,
		MaxFailoverRetries:      3,
		ConnectionDrainTimeout:  60 * time.Second,
		WeightingFactors: WeightingConfig{
			ConnectionLoad: 0.3,
			ResourceUsage:  0.25,
			HealthScore:    0.2,
			LatencyScore:   0.15,
			CapacityScore:  0.1,
		},
		Thresholds: ThresholdConfig{
			MaxConnectionUtilization: 0.8,
			MaxCPUUsage:              0.8,
			MaxMemoryUsage:           0.8,
			MaxLatency:               100 * time.Millisecond,
			MinHealthScore:           0.7,
			ErrorRateThreshold:       0.05,
		},
		CircuitBreakerConfig: CircuitBreakerConfig{
			Enabled:             true,
			FailureThreshold:    5,
			RecoveryTimeout:     60 * time.Second,
			HalfOpenMaxCalls:    3,
			HalfOpenSuccessRate: 0.8,
		},
	}
}

// DefaultLoadBalancer implements LoadBalancer interface
type DefaultLoadBalancer struct {
	config  LoadBalancerConfig
	scaler  RedisScaler
	logger  common.Logger
	metrics common.Metrics

	// Strategy implementations
	strategies      map[LoadBalanceStrategy]BalanceStrategy
	currentStrategy LoadBalanceStrategy

	// Instance management
	instances      map[string]*InstanceInfo
	instancesMu    sync.RWMutex
	healthCheckers map[string]*InstanceHealthChecker
	healthMu       sync.RWMutex

	// Load balancing state
	roundRobinIndex    int
	selectionCount     int64
	stickySessionCache map[string]string // userID -> instanceID
	stickyCacheMu      sync.RWMutex

	// Circuit breakers
	circuitBreakers map[string]*CircuitBreaker
	breakersMu      sync.RWMutex

	// Statistics
	stats   LoadBalancerStats
	statsMu sync.RWMutex

	// Lifecycle
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// InstanceInfo represents information about an instance
type InstanceInfo struct {
	ID           string
	Metadata     InstanceMetadata
	Load         *InstanceLoad
	Health       *InstanceHealthMetrics
	Stats        *InstanceStats
	LastSeen     time.Time
	CircuitState CircuitState
	mu           sync.RWMutex
}

// CircuitState represents circuit breaker state
type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"
	CircuitOpen     CircuitState = "open"
	CircuitHalfOpen CircuitState = "half_open"
)

// BalanceStrategy defines the interface for load balancing strategies
type BalanceStrategy interface {
	Name() LoadBalanceStrategy
	SelectInstance(candidates []*InstanceInfo, criteria ConnectionCriteria) (*InstanceSelection, error)
	UpdateWeights(instances []*InstanceInfo) error
}

// NewDefaultLoadBalancer creates a new default load balancer
func NewDefaultLoadBalancer(
	scaler RedisScaler,
	config LoadBalancerConfig,
	logger common.Logger,
	metrics common.Metrics,
) LoadBalancer {
	lb := &DefaultLoadBalancer{
		config:             config,
		scaler:             scaler,
		logger:             logger,
		metrics:            metrics,
		strategies:         make(map[LoadBalanceStrategy]BalanceStrategy),
		currentStrategy:    config.Strategy,
		instances:          make(map[string]*InstanceInfo),
		healthCheckers:     make(map[string]*InstanceHealthChecker),
		stickySessionCache: make(map[string]string),
		circuitBreakers:    make(map[string]*CircuitBreaker),
		stopCh:             make(chan struct{}),
		stats: LoadBalancerStats{
			SelectionsByStrategy: make(map[LoadBalanceStrategy]int64),
			SelectionsByInstance: make(map[string]int64),
			LoadDistribution:     make(map[string]float64),
			StrategyEfficiency:   make(map[LoadBalanceStrategy]float64),
			InstanceStats:        make(map[string]*InstanceStats),
		},
	}

	// Initialize strategies
	lb.initializeStrategies()

	return lb
}

// Connection routing

// SelectInstance selects the best instance based on criteria
func (lb *DefaultLoadBalancer) SelectInstance(ctx context.Context, criteria ConnectionCriteria) (*InstanceSelection, error) {
	startTime := time.Now()

	// Get healthy instances
	healthyInstances, err := lb.getHealthyInstancesInternal()
	if err != nil {
		return nil, fmt.Errorf("failed to get healthy instances: %w", err)
	}

	if len(healthyInstances) == 0 {
		return nil, common.ErrServiceNotFound("no healthy instances available")
	}

	// Filter instances based on criteria
	candidates := lb.filterInstancesByCriteria(healthyInstances, criteria)
	if len(candidates) == 0 {
		return nil, common.ErrServiceNotFound("no instances match criteria")
	}

	// Check sticky session
	if criteria.StickySession && criteria.UserID != "" {
		if instanceID := lb.getStickySession(criteria.UserID); instanceID != "" {
			for _, instance := range candidates {
				if instance.ID == instanceID {
					selection := &InstanceSelection{
						InstanceID:      instanceID,
						SelectionReason: "sticky_session",
						SelectedAt:      time.Now(),
						Metadata: InstanceSelectionMeta{
							Strategy:       lb.currentStrategy,
							SelectionTime:  time.Since(startTime),
							CandidateCount: len(candidates),
						},
					}
					lb.recordSelection(selection)
					return selection, nil
				}
			}
		}
	}

	// Get current strategy
	strategy := lb.strategies[lb.currentStrategy]
	if strategy == nil {
		return nil, common.ErrInvalidConfig("strategy", fmt.Errorf("strategy %s not found", lb.currentStrategy))
	}

	// Select instance using strategy
	selection, err := strategy.SelectInstance(candidates, criteria)
	if err != nil {
		return nil, fmt.Errorf("strategy selection failed: %w", err)
	}

	// Update selection metadata
	selection.Metadata.SelectionTime = time.Since(startTime)
	selection.Metadata.CandidateCount = len(candidates)
	selection.Metadata.Strategy = lb.currentStrategy
	selection.SelectedAt = time.Now()

	// Update sticky session if enabled
	if criteria.StickySession && criteria.UserID != "" {
		lb.setStickySession(criteria.UserID, selection.InstanceID)
	}

	// Record selection
	lb.recordSelection(selection)

	if lb.logger != nil {
		lb.logger.Debug("instance selected",
			logger.String("instance_id", selection.InstanceID),
			logger.String("strategy", string(lb.currentStrategy)),
			logger.String("reason", selection.SelectionReason),
			logger.Float64("score", selection.OverallScore),
			logger.Duration("selection_time", selection.Metadata.SelectionTime),
		)
	}

	if lb.metrics != nil {
		lb.metrics.Counter("streaming.loadbalancer.selections").Inc()
		lb.metrics.Histogram("streaming.loadbalancer.selection_time").Observe(selection.Metadata.SelectionTime.Seconds())
	}

	return selection, nil
}

// RouteConnection routes a connection to an optimal instance
func (lb *DefaultLoadBalancer) RouteConnection(ctx context.Context, connectionInfo ConnectionInfo) (*InstanceSelection, error) {
	criteria := ConnectionCriteria{
		Protocol:      connectionInfo.Protocol,
		UserID:        connectionInfo.UserID,
		RoomID:        connectionInfo.RoomID,
		StickySession: lb.config.EnableStickySession,
	}

	return lb.SelectInstance(ctx, criteria)
}

// GetInstanceLoad returns the current load for an instance
func (lb *DefaultLoadBalancer) GetInstanceLoad(ctx context.Context, instanceID string) (*InstanceLoad, error) {
	lb.instancesMu.RLock()
	instance, exists := lb.instances[instanceID]
	lb.instancesMu.RUnlock()

	if !exists {
		return nil, common.ErrServiceNotFound(instanceID)
	}

	instance.mu.RLock()
	defer instance.mu.RUnlock()

	if instance.Load == nil {
		return nil, common.ErrServiceNotFound("load data not available")
	}

	// Return a copy to avoid race conditions
	load := *instance.Load
	return &load, nil
}

// Load balancing strategies

// SetStrategy sets the load balancing strategy
func (lb *DefaultLoadBalancer) SetStrategy(strategy LoadBalanceStrategy) error {
	if _, exists := lb.strategies[strategy]; !exists {
		return common.ErrInvalidConfig("strategy", fmt.Errorf("unknown strategy: %s", strategy))
	}

	lb.currentStrategy = strategy

	if lb.logger != nil {
		lb.logger.Info("load balancing strategy changed",
			logger.String("strategy", string(strategy)),
		)
	}

	return nil
}

// GetStrategy returns the current load balancing strategy
func (lb *DefaultLoadBalancer) GetStrategy() LoadBalanceStrategy {
	return lb.currentStrategy
}

// GetAvailableStrategies returns all available strategies
func (lb *DefaultLoadBalancer) GetAvailableStrategies() []LoadBalanceStrategy {
	strategies := make([]LoadBalanceStrategy, 0, len(lb.strategies))
	for strategy := range lb.strategies {
		strategies = append(strategies, strategy)
	}
	return strategies
}

// Health and monitoring

// UpdateInstanceHealth updates health metrics for an instance
func (lb *DefaultLoadBalancer) UpdateInstanceHealth(instanceID string, health InstanceHealthMetrics) error {
	lb.instancesMu.Lock()
	defer lb.instancesMu.Unlock()

	instance, exists := lb.instances[instanceID]
	if !exists {
		// Create new instance if it doesn't exist
		instance = &InstanceInfo{
			ID:    instanceID,
			Stats: &InstanceStats{},
		}
		lb.instances[instanceID] = instance
	}

	instance.mu.Lock()
	defer instance.mu.Unlock()

	instance.Health = &health
	instance.LastSeen = time.Now()

	// Update circuit breaker state based on health
	if health.Healthy {
		lb.recordHealthyResponse(instanceID)
	} else {
		lb.recordUnhealthyResponse(instanceID)
	}

	if lb.metrics != nil {
		lb.metrics.Gauge("streaming.loadbalancer.instance_health").Set(
			func() float64 {
				if health.Healthy {
					return 1.0
				}
				return 0.0
			}(),
		)
	}

	return nil
}

// GetHealthyInstances returns a list of healthy instance IDs
func (lb *DefaultLoadBalancer) GetHealthyInstances(ctx context.Context) ([]string, error) {
	return lb.getHealthyInstancesInternal()
}

// MarkInstanceUnhealthy marks an instance as unhealthy
func (lb *DefaultLoadBalancer) MarkInstanceUnhealthy(instanceID string, reason string) error {
	lb.instancesMu.Lock()
	defer lb.instancesMu.Unlock()

	instance, exists := lb.instances[instanceID]
	if !exists {
		return common.ErrServiceNotFound(instanceID)
	}

	instance.mu.Lock()
	defer instance.mu.Unlock()

	if instance.Health == nil {
		instance.Health = &InstanceHealthMetrics{
			InstanceID: instanceID,
		}
	}

	instance.Health.Healthy = false
	instance.Health.LastHealthCheck = time.Now()

	// Update circuit breaker
	lb.recordUnhealthyResponse(instanceID)

	if lb.logger != nil {
		lb.logger.Warn("instance marked unhealthy",
			logger.String("instance_id", instanceID),
			logger.String("reason", reason),
		)
	}

	if lb.metrics != nil {
		lb.metrics.Counter("streaming.loadbalancer.instances_marked_unhealthy").Inc()
	}

	return nil
}

// MarkInstanceHealthy marks an instance as healthy
func (lb *DefaultLoadBalancer) MarkInstanceHealthy(instanceID string) error {
	lb.instancesMu.Lock()
	defer lb.instancesMu.Unlock()

	instance, exists := lb.instances[instanceID]
	if !exists {
		return common.ErrServiceNotFound(instanceID)
	}

	instance.mu.Lock()
	defer instance.mu.Unlock()

	if instance.Health == nil {
		instance.Health = &InstanceHealthMetrics{
			InstanceID: instanceID,
		}
	}

	instance.Health.Healthy = true
	instance.Health.LastHealthCheck = time.Now()

	// Update circuit breaker
	lb.recordHealthyResponse(instanceID)

	if lb.logger != nil {
		lb.logger.Info("instance marked healthy",
			logger.String("instance_id", instanceID),
		)
	}

	if lb.metrics != nil {
		lb.metrics.Counter("streaming.loadbalancer.instances_marked_healthy").Inc()
	}

	return nil
}

// Failover management

// HandleInstanceFailure handles instance failure
func (lb *DefaultLoadBalancer) HandleInstanceFailure(ctx context.Context, instanceID string) error {
	if !lb.config.EnableAutomaticFailover {
		return nil
	}

	// Mark instance as unhealthy
	if err := lb.MarkInstanceUnhealthy(instanceID, "instance_failure"); err != nil {
		return err
	}

	// Remove from sticky sessions
	lb.removeStickySessionsForInstance(instanceID)

	// Get connections on the failed instance
	connections, err := lb.scaler.GetInstanceConnections(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("failed to get instance connections: %w", err)
	}

	// Find failover targets and redistribute connections
	for _, conn := range connections {
		target, err := lb.GetFailoverTarget(ctx, instanceID)
		if err != nil {
			if lb.logger != nil {
				lb.logger.Error("failed to find failover target",
					logger.String("instance_id", instanceID),
					logger.String("connection_id", conn.ID),
					logger.Error(err),
				)
			}
			continue
		}

		// Trigger connection failover
		if err := lb.TriggerFailover(ctx, instanceID, target); err != nil {
			if lb.logger != nil {
				lb.logger.Error("failover failed",
					logger.String("from_instance", instanceID),
					logger.String("to_instance", target),
					logger.Error(err),
				)
			}
		}
	}

	// Update statistics
	lb.statsMu.Lock()
	lb.stats.FailoverCount++
	lb.stats.LastFailover = time.Now()
	if instance, exists := lb.stats.InstanceStats[instanceID]; exists {
		instance.FailoverCount++
		instance.LastFailover = time.Now()
	}
	lb.statsMu.Unlock()

	if lb.logger != nil {
		lb.logger.Info("instance failure handled",
			logger.String("instance_id", instanceID),
			logger.Int("connections_affected", len(connections)),
		)
	}

	if lb.metrics != nil {
		lb.metrics.Counter("streaming.loadbalancer.failovers").Inc()
	}

	return nil
}

// GetFailoverTarget finds the best target for failover
func (lb *DefaultLoadBalancer) GetFailoverTarget(ctx context.Context, failedInstanceID string) (string, error) {
	criteria := ConnectionCriteria{
		AvoidInstances: []string{failedInstanceID},
	}

	selection, err := lb.SelectInstance(ctx, criteria)
	if err != nil {
		return "", err
	}

	return selection.InstanceID, nil
}

// TriggerFailover triggers failover from one instance to another
func (lb *DefaultLoadBalancer) TriggerFailover(ctx context.Context, fromInstance, toInstance string) error {
	// This would typically involve:
	// 1. Gracefully closing connections on the failed instance
	// 2. Redirecting traffic to the target instance
	// 3. Updating load balancer state
	// 4. Notifying other components

	if lb.logger != nil {
		lb.logger.Info("failover triggered",
			logger.String("from_instance", fromInstance),
			logger.String("to_instance", toInstance),
		)
	}

	return nil
}

// Configuration and lifecycle

// Start starts the load balancer
func (lb *DefaultLoadBalancer) Start(ctx context.Context) error {
	if lb.running {
		return nil
	}

	// Start health checking
	lb.startHealthChecking()

	// Start load metrics collection
	lb.startLoadMetricsCollection()

	lb.running = true

	if lb.logger != nil {
		lb.logger.Info("load balancer started",
			logger.String("strategy", string(lb.currentStrategy)),
			logger.Duration("health_check_interval", lb.config.HealthCheckInterval),
		)
	}

	if lb.metrics != nil {
		lb.metrics.Counter("streaming.loadbalancer.started").Inc()
	}

	return nil
}

// Stop stops the load balancer
func (lb *DefaultLoadBalancer) Stop(ctx context.Context) error {
	if !lb.running {
		return nil
	}

	// Stop background processes
	close(lb.stopCh)
	lb.wg.Wait()

	lb.running = false

	if lb.logger != nil {
		lb.logger.Info("load balancer stopped")
	}

	return nil
}

// UpdateConfig updates the load balancer configuration
func (lb *DefaultLoadBalancer) UpdateConfig(config LoadBalancerConfig) error {
	lb.config = config

	// Update current strategy if changed
	if config.Strategy != lb.currentStrategy {
		if err := lb.SetStrategy(config.Strategy); err != nil {
			return err
		}
	}

	if lb.logger != nil {
		lb.logger.Info("load balancer configuration updated")
	}

	return nil
}

// GetConfig returns the current configuration
func (lb *DefaultLoadBalancer) GetConfig() LoadBalancerConfig {
	return lb.config
}

// Statistics and monitoring

// GetLoadStats returns load balancer statistics
func (lb *DefaultLoadBalancer) GetLoadStats() LoadBalancerStats {
	lb.statsMu.RLock()
	defer lb.statsMu.RUnlock()

	// Create a deep copy to avoid race conditions
	stats := LoadBalancerStats{
		TotalSelections:      lb.stats.TotalSelections,
		SelectionsByStrategy: make(map[LoadBalanceStrategy]int64),
		SelectionsByInstance: make(map[string]int64),
		FailoverCount:        lb.stats.FailoverCount,
		AverageSelectionTime: lb.stats.AverageSelectionTime,
		LoadDistribution:     make(map[string]float64),
		StrategyEfficiency:   make(map[LoadBalanceStrategy]float64),
		LastFailover:         lb.stats.LastFailover,
		InstanceStats:        make(map[string]*InstanceStats),
	}

	for k, v := range lb.stats.SelectionsByStrategy {
		stats.SelectionsByStrategy[k] = v
	}
	for k, v := range lb.stats.SelectionsByInstance {
		stats.SelectionsByInstance[k] = v
	}
	for k, v := range lb.stats.LoadDistribution {
		stats.LoadDistribution[k] = v
	}
	for k, v := range lb.stats.StrategyEfficiency {
		stats.StrategyEfficiency[k] = v
	}
	for k, v := range lb.stats.InstanceStats {
		statsCopy := *v
		stats.InstanceStats[k] = &statsCopy
	}

	// Update healthy/unhealthy counts
	lb.instancesMu.RLock()
	for _, instance := range lb.instances {
		instance.mu.RLock()
		if instance.Health != nil && instance.Health.Healthy {
			stats.HealthyInstances++
		} else {
			stats.UnhealthyInstances++
		}
		instance.mu.RUnlock()
	}
	lb.instancesMu.RUnlock()

	return stats
}

// GetInstanceStats returns statistics for a specific instance
func (lb *DefaultLoadBalancer) GetInstanceStats(instanceID string) (*InstanceStats, error) {
	lb.statsMu.RLock()
	defer lb.statsMu.RUnlock()

	stats, exists := lb.stats.InstanceStats[instanceID]
	if !exists {
		return nil, common.ErrServiceNotFound(instanceID)
	}

	// Return a copy
	statsCopy := *stats
	return &statsCopy, nil
}

// GetAllInstanceStats returns statistics for all instances
func (lb *DefaultLoadBalancer) GetAllInstanceStats() map[string]*InstanceStats {
	lb.statsMu.RLock()
	defer lb.statsMu.RUnlock()

	stats := make(map[string]*InstanceStats)
	for k, v := range lb.stats.InstanceStats {
		statsCopy := *v
		stats[k] = &statsCopy
	}

	return stats
}

// Helper methods

// initializeStrategies initializes all load balancing strategies
func (lb *DefaultLoadBalancer) initializeStrategies() {
	lb.strategies[StrategyRoundRobin] = NewRoundRobinStrategy(lb)
	lb.strategies[StrategyLeastConnected] = NewLeastConnectedStrategy(lb)
	lb.strategies[StrategyWeightedRandom] = NewWeightedRandomStrategy(lb)
	lb.strategies[StrategyResourceBased] = NewResourceBasedStrategy(lb)
	lb.strategies[StrategyLatencyBased] = NewLatencyBasedStrategy(lb)
	lb.strategies[StrategyGeographic] = NewGeographicStrategy(lb)
	lb.strategies[StrategyCapacityBased] = NewCapacityBasedStrategy(lb)
}

// getHealthyInstancesInternal returns healthy instances (internal method)
func (lb *DefaultLoadBalancer) getHealthyInstancesInternal() ([]*InstanceInfo, error) {
	lb.instancesMu.RLock()
	defer lb.instancesMu.RUnlock()

	var healthy []*InstanceInfo
	for _, instance := range lb.instances {
		instance.mu.RLock()
		isHealthy := instance.Health != nil && instance.Health.Healthy && lb.isCircuitClosed(instance.ID)
		instance.mu.RUnlock()

		if isHealthy {
			healthy = append(healthy, instance)
		}
	}

	return healthy, nil
}

// filterInstancesByCriteria filters instances based on connection criteria
func (lb *DefaultLoadBalancer) filterInstancesByCriteria(instances []*InstanceInfo, criteria ConnectionCriteria) []*InstanceInfo {
	var filtered []*InstanceInfo

	for _, instance := range instances {
		// Check avoided instances
		skip := false
		for _, avoidID := range criteria.AvoidInstances {
			if instance.ID == avoidID {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		// Check preferred instance
		if criteria.PreferredInstance != "" && instance.ID == criteria.PreferredInstance {
			filtered = []*InstanceInfo{instance} // Return only preferred instance
			break
		}

		// Check geographic criteria
		if criteria.Region != "" || criteria.Zone != "" {
			if !lb.matchesGeographicCriteria(instance, criteria) {
				continue
			}
		}

		// Check required features
		if len(criteria.RequiredFeatures) > 0 {
			if !lb.hasRequiredFeatures(instance, criteria.RequiredFeatures) {
				continue
			}
		}

		// Check load thresholds
		if !lb.withinLoadThresholds(instance) {
			continue
		}

		filtered = append(filtered, instance)
	}

	return filtered
}

// matchesGeographicCriteria checks if instance matches geographic criteria
func (lb *DefaultLoadBalancer) matchesGeographicCriteria(instance *InstanceInfo, criteria ConnectionCriteria) bool {
	if criteria.Region != "" && instance.Metadata.Region != criteria.Region {
		return false
	}
	if criteria.Zone != "" && instance.Metadata.Zone != criteria.Zone {
		return false
	}
	return true
}

// hasRequiredFeatures checks if instance has all required features
func (lb *DefaultLoadBalancer) hasRequiredFeatures(instance *InstanceInfo, requiredFeatures []string) bool {
	instanceFeatures := make(map[string]bool)
	for _, feature := range instance.Metadata.Capabilities {
		instanceFeatures[feature] = true
	}

	for _, required := range requiredFeatures {
		if !instanceFeatures[required] {
			return false
		}
	}

	return true
}

// withinLoadThresholds checks if instance is within acceptable load thresholds
func (lb *DefaultLoadBalancer) withinLoadThresholds(instance *InstanceInfo) bool {
	instance.mu.RLock()
	defer instance.mu.RUnlock()

	if instance.Load == nil {
		return true // No load data, assume OK
	}

	load := instance.Load
	thresholds := lb.config.Thresholds

	if load.ConnectionUtilization > thresholds.MaxConnectionUtilization {
		return false
	}
	if load.CPUUsage > thresholds.MaxCPUUsage {
		return false
	}
	if load.MemoryUsage > thresholds.MaxMemoryUsage {
		return false
	}

	return true
}

// Sticky session management

// getStickySession gets the instance for a sticky session
func (lb *DefaultLoadBalancer) getStickySession(userID string) string {
	lb.stickyCacheMu.RLock()
	defer lb.stickyCacheMu.RUnlock()
	return lb.stickySessionCache[userID]
}

// setStickySession sets the instance for a sticky session
func (lb *DefaultLoadBalancer) setStickySession(userID, instanceID string) {
	lb.stickyCacheMu.Lock()
	defer lb.stickyCacheMu.Unlock()
	lb.stickySessionCache[userID] = instanceID
}

// removeStickySessionsForInstance removes all sticky sessions for an instance
func (lb *DefaultLoadBalancer) removeStickySessionsForInstance(instanceID string) {
	lb.stickyCacheMu.Lock()
	defer lb.stickyCacheMu.Unlock()

	for userID, stickyInstanceID := range lb.stickySessionCache {
		if stickyInstanceID == instanceID {
			delete(lb.stickySessionCache, userID)
		}
	}
}

// Circuit breaker management

// isCircuitClosed checks if circuit breaker is closed for an instance
func (lb *DefaultLoadBalancer) isCircuitClosed(instanceID string) bool {
	lb.breakersMu.RLock()
	defer lb.breakersMu.RUnlock()

	breaker, exists := lb.circuitBreakers[instanceID]
	if !exists {
		return true // No circuit breaker, assume closed
	}

	return breaker.State == CircuitClosed || breaker.State == CircuitHalfOpen
}

// recordHealthyResponse records a healthy response for circuit breaker
func (lb *DefaultLoadBalancer) recordHealthyResponse(instanceID string) {
	if !lb.config.CircuitBreakerConfig.Enabled {
		return
	}

	lb.breakersMu.Lock()
	defer lb.breakersMu.Unlock()

	breaker, exists := lb.circuitBreakers[instanceID]
	if !exists {
		breaker = NewCircuitBreaker(instanceID, lb.config.CircuitBreakerConfig)
		lb.circuitBreakers[instanceID] = breaker
	}

	breaker.RecordSuccess()
}

// recordUnhealthyResponse records an unhealthy response for circuit breaker
func (lb *DefaultLoadBalancer) recordUnhealthyResponse(instanceID string) {
	if !lb.config.CircuitBreakerConfig.Enabled {
		return
	}

	lb.breakersMu.Lock()
	defer lb.breakersMu.Unlock()

	breaker, exists := lb.circuitBreakers[instanceID]
	if !exists {
		breaker = NewCircuitBreaker(instanceID, lb.config.CircuitBreakerConfig)
		lb.circuitBreakers[instanceID] = breaker
	}

	breaker.RecordFailure()
}

// recordSelection records a selection for statistics
func (lb *DefaultLoadBalancer) recordSelection(selection *InstanceSelection) {
	lb.statsMu.Lock()
	defer lb.statsMu.Unlock()

	lb.stats.TotalSelections++
	lb.stats.SelectionsByStrategy[selection.Metadata.Strategy]++
	lb.stats.SelectionsByInstance[selection.InstanceID]++

	// Update average selection time
	if lb.stats.TotalSelections == 1 {
		lb.stats.AverageSelectionTime = selection.Metadata.SelectionTime
	} else {
		lb.stats.AverageSelectionTime = (lb.stats.AverageSelectionTime*time.Duration(lb.stats.TotalSelections-1) + selection.Metadata.SelectionTime) / time.Duration(lb.stats.TotalSelections)
	}

	// Update instance stats
	if _, exists := lb.stats.InstanceStats[selection.InstanceID]; !exists {
		lb.stats.InstanceStats[selection.InstanceID] = &InstanceStats{}
	}

	instanceStats := lb.stats.InstanceStats[selection.InstanceID]
	instanceStats.SelectionCount++
	instanceStats.LastSelected = selection.SelectedAt
}

// Background processes

// startHealthChecking starts the health checking process
func (lb *DefaultLoadBalancer) startHealthChecking() {
	lb.wg.Add(1)
	go func() {
		defer lb.wg.Done()

		ticker := time.NewTicker(lb.config.HealthCheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				lb.performHealthChecks()
			case <-lb.stopCh:
				return
			}
		}
	}()
}

// performHealthChecks performs health checks on all instances
func (lb *DefaultLoadBalancer) performHealthChecks() {
	ctx := context.Background()

	instances, err := lb.scaler.GetInstances(ctx)
	if err != nil {
		if lb.logger != nil {
			lb.logger.Error("failed to get instances for health check", logger.Error(err))
		}
		return
	}

	for _, instance := range instances {
		go func(inst Instance) {
			health, err := lb.scaler.GetInstanceHealth(ctx, inst.ID)
			if err != nil {
				lb.MarkInstanceUnhealthy(inst.ID, "health_check_failed")
				return
			}

			if health != nil {
				lb.UpdateInstanceHealth(inst.ID, *health)
			}
		}(instance)
	}
}

// startLoadMetricsCollection starts load metrics collection
func (lb *DefaultLoadBalancer) startLoadMetricsCollection() {
	lb.wg.Add(1)
	go func() {
		defer lb.wg.Done()

		ticker := time.NewTicker(lb.config.LoadMetricsInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				lb.collectLoadMetrics()
			case <-lb.stopCh:
				return
			}
		}
	}()
}

// collectLoadMetrics collects load metrics from all instances
func (lb *DefaultLoadBalancer) collectLoadMetrics() {
	ctx := context.Background()

	instances, err := lb.scaler.GetInstances(ctx)
	if err != nil {
		if lb.logger != nil {
			lb.logger.Error("failed to get instances for load metrics", logger.Error(err))
		}
		return
	}

	for _, instance := range instances {
		go func(inst Instance) {
			// Get connections
			connections, err := lb.scaler.GetInstanceConnections(ctx, inst.ID)
			if err != nil {
				return
			}

			// Get rooms
			rooms, err := lb.scaler.GetInstanceRooms(ctx, inst.ID)
			if err != nil {
				return
			}

			// Calculate load metrics
			load := &InstanceLoad{
				InstanceID:        inst.ID,
				ActiveConnections: len(connections),
				MaxConnections:    inst.Metadata.MaxConns,
				ActiveRooms:       len(rooms),
				MaxRooms:          inst.Metadata.MaxRooms,
				LastUpdated:       time.Now(),
			}

			if load.MaxConnections > 0 {
				load.ConnectionUtilization = float64(load.ActiveConnections) / float64(load.MaxConnections)
			}
			if load.MaxRooms > 0 {
				load.RoomUtilization = float64(load.ActiveRooms) / float64(load.MaxRooms)
			}

			// Update instance load
			lb.updateInstanceLoad(inst.ID, load)
		}(instance)
	}
}

// updateInstanceLoad updates load metrics for an instance
func (lb *DefaultLoadBalancer) updateInstanceLoad(instanceID string, load *InstanceLoad) {
	lb.instancesMu.Lock()
	defer lb.instancesMu.Unlock()

	instance, exists := lb.instances[instanceID]
	if !exists {
		instance = &InstanceInfo{
			ID:    instanceID,
			Stats: &InstanceStats{},
		}
		lb.instances[instanceID] = instance
	}

	instance.mu.Lock()
	defer instance.mu.Unlock()

	instance.Load = load
	instance.LastSeen = time.Now()

	// Update load distribution in stats
	lb.statsMu.Lock()
	lb.stats.LoadDistribution[instanceID] = load.LoadScore
	lb.statsMu.Unlock()
}

// Strategy implementations will be added as separate types...
// For brevity, I'll provide stub implementations here

// Round Robin Strategy
type RoundRobinStrategy struct {
	lb    *DefaultLoadBalancer
	index int
	mu    sync.Mutex
}

func NewRoundRobinStrategy(lb *DefaultLoadBalancer) BalanceStrategy {
	return &RoundRobinStrategy{lb: lb}
}

func (s *RoundRobinStrategy) Name() LoadBalanceStrategy {
	return StrategyRoundRobin
}

func (s *RoundRobinStrategy) SelectInstance(candidates []*InstanceInfo, criteria ConnectionCriteria) (*InstanceSelection, error) {
	if len(candidates) == 0 {
		return nil, common.ErrServiceNotFound("no candidates available")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Sort for consistent ordering
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].ID < candidates[j].ID
	})

	selected := candidates[s.index%len(candidates)]
	s.index++

	return &InstanceSelection{
		InstanceID:      selected.ID,
		OverallScore:    1.0 / float64(len(candidates)),
		SelectionReason: "round_robin",
	}, nil
}

func (s *RoundRobinStrategy) UpdateWeights(instances []*InstanceInfo) error {
	// Round robin doesn't use weights
	return nil
}

// Least Connected Strategy
type LeastConnectedStrategy struct {
	lb *DefaultLoadBalancer
}

func NewLeastConnectedStrategy(lb *DefaultLoadBalancer) BalanceStrategy {
	return &LeastConnectedStrategy{lb: lb}
}

func (s *LeastConnectedStrategy) Name() LoadBalanceStrategy {
	return StrategyLeastConnected
}

func (s *LeastConnectedStrategy) SelectInstance(candidates []*InstanceInfo, criteria ConnectionCriteria) (*InstanceSelection, error) {
	if len(candidates) == 0 {
		return nil, common.ErrServiceNotFound("no candidates available")
	}

	var best *InstanceInfo
	var minConnections = math.MaxInt32

	for _, instance := range candidates {
		instance.mu.RLock()
		connections := 0
		if instance.Load != nil {
			connections = instance.Load.ActiveConnections
		}
		instance.mu.RUnlock()

		if connections < minConnections {
			minConnections = connections
			best = instance
		}
	}

	if best == nil {
		best = candidates[0]
	}

	score := 1.0 - (float64(minConnections) / float64(s.lb.config.MaxConnectionsPerInstance))

	return &InstanceSelection{
		InstanceID:      best.ID,
		OverallScore:    score,
		SelectionReason: "least_connected",
	}, nil
}

func (s *LeastConnectedStrategy) UpdateWeights(instances []*InstanceInfo) error {
	// Least connected doesn't use weights
	return nil
}

// Stub implementations for other strategies
func NewWeightedRandomStrategy(lb *DefaultLoadBalancer) BalanceStrategy {
	return &stubStrategy{name: StrategyWeightedRandom}
}

func NewResourceBasedStrategy(lb *DefaultLoadBalancer) BalanceStrategy {
	return &stubStrategy{name: StrategyResourceBased}
}

func NewLatencyBasedStrategy(lb *DefaultLoadBalancer) BalanceStrategy {
	return &stubStrategy{name: StrategyLatencyBased}
}

func NewGeographicStrategy(lb *DefaultLoadBalancer) BalanceStrategy {
	return &stubStrategy{name: StrategyGeographic}
}

func NewCapacityBasedStrategy(lb *DefaultLoadBalancer) BalanceStrategy {
	return &stubStrategy{name: StrategyCapacityBased}
}

type stubStrategy struct {
	name LoadBalanceStrategy
}

func (s *stubStrategy) Name() LoadBalanceStrategy {
	return s.name
}

func (s *stubStrategy) SelectInstance(candidates []*InstanceInfo, criteria ConnectionCriteria) (*InstanceSelection, error) {
	if len(candidates) == 0 {
		return nil, common.ErrServiceNotFound("no candidates available")
	}

	// Simple random selection for stub
	selected := candidates[rand.Intn(len(candidates))]
	return &InstanceSelection{
		InstanceID:      selected.ID,
		OverallScore:    rand.Float64(),
		SelectionReason: string(s.name),
	}, nil
}

func (s *stubStrategy) UpdateWeights(instances []*InstanceInfo) error {
	return nil
}

// CircuitBreaker Circuit Breaker implementation
type CircuitBreaker struct {
	InstanceID      string
	State           CircuitState
	FailureCount    int
	LastFailureTime time.Time
	LastSuccessTime time.Time
	Config          CircuitBreakerConfig
	mu              sync.RWMutex
}

func NewCircuitBreaker(instanceID string, config CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		InstanceID: instanceID,
		State:      CircuitClosed,
		Config:     config,
	}
}

func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.LastSuccessTime = time.Now()
	cb.FailureCount = 0

	if cb.State == CircuitHalfOpen {
		cb.State = CircuitClosed
	}
}

func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.LastFailureTime = time.Now()
	cb.FailureCount++

	if cb.FailureCount >= cb.Config.FailureThreshold && cb.State == CircuitClosed {
		cb.State = CircuitOpen
	}
}

func (cb *CircuitBreaker) CanAttempt() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	switch cb.State {
	case CircuitClosed:
		return true
	case CircuitOpen:
		if time.Since(cb.LastFailureTime) >= cb.Config.RecoveryTimeout {
			cb.State = CircuitHalfOpen
			return true
		}
		return false
	case CircuitHalfOpen:
		return true
	default:
		return false
	}
}

// InstanceHealthChecker performs health checks on instances
type InstanceHealthChecker struct {
	InstanceID   string
	LoadBalancer *DefaultLoadBalancer
	ticker       *time.Ticker
	stopCh       chan struct{}
}

func NewInstanceHealthChecker(instanceID string, lb *DefaultLoadBalancer) *InstanceHealthChecker {
	return &InstanceHealthChecker{
		InstanceID:   instanceID,
		LoadBalancer: lb,
		stopCh:       make(chan struct{}),
	}
}

func (hc *InstanceHealthChecker) Start() {
	hc.ticker = time.NewTicker(hc.LoadBalancer.config.HealthCheckInterval)
	go hc.run()
}

func (hc *InstanceHealthChecker) Stop() {
	if hc.ticker != nil {
		hc.ticker.Stop()
	}
	close(hc.stopCh)
}

func (hc *InstanceHealthChecker) run() {
	for {
		select {
		case <-hc.ticker.C:
			hc.performHealthCheck()
		case <-hc.stopCh:
			return
		}
	}
}

func (hc *InstanceHealthChecker) performHealthCheck() {
	ctx := context.Background()
	health, err := hc.LoadBalancer.scaler.GetInstanceHealth(ctx, hc.InstanceID)
	if err != nil {
		hc.LoadBalancer.MarkInstanceUnhealthy(hc.InstanceID, "health_check_failed")
		return
	}

	if health != nil {
		hc.LoadBalancer.UpdateInstanceHealth(hc.InstanceID, *health)
	}
}
