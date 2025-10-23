package scaling

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	"github.com/xraph/forge/v0/pkg/streaming"
)

// StreamingCoordinator coordinates multiple streaming service instances
type StreamingCoordinator interface {
	// Lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	IsRunning() bool

	// Instance management
	RegisterInstance(metadata InstanceMetadata) error
	UnregisterInstance() error
	GetInstanceID() string
	UpdateInstanceHealth(health InstanceHealth) error

	// Room coordination
	CreateRoom(ctx context.Context, roomID string, config streaming.RoomConfig) (*RoomAllocation, error)
	DeleteRoom(ctx context.Context, roomID string) error
	GetRoomAllocation(ctx context.Context, roomID string) (*RoomAllocation, error)
	MigrateRoom(ctx context.Context, roomID string, targetInstanceID string) error

	// Load balancing
	GetOptimalInstance(ctx context.Context, criteria AllocationCriteria) (string, error)
	BalanceLoad(ctx context.Context) error
	GetLoadBalancingStats() LoadBalancingStats

	// Message routing
	RouteMessage(ctx context.Context, roomID string, message *streaming.Message) error
	BroadcastToRoom(ctx context.Context, roomID string, message *streaming.Message) error
	BroadcastToAll(ctx context.Context, message *streaming.Message) error

	// Event handling
	OnInstanceJoined(handler InstanceEventHandler)
	OnInstanceLeft(handler InstanceEventHandler)
	OnRoomMigrated(handler RoomMigrationHandler)
	OnLoadRebalanced(handler LoadRebalanceHandler)

	// Health monitoring
	MonitorHealth(ctx context.Context) error
	GetClusterHealth() ClusterHealth

	// Statistics
	GetCoordinationStats() CoordinationStats
}

// RoomAllocation represents how a room is allocated across instances
type RoomAllocation struct {
	RoomID           string    `json:"room_id"`
	PrimaryInstance  string    `json:"primary_instance"`
	ReplicaInstances []string  `json:"replica_instances"`
	CreatedAt        time.Time `json:"created_at"`
	LastMigration    time.Time `json:"last_migration,omitempty"`
	MigrationCount   int       `json:"migration_count"`
	LoadScore        float64   `json:"load_score"`
}

// AllocationCriteria defines criteria for instance allocation
type AllocationCriteria struct {
	Region               string               `json:"region,omitempty"`
	Zone                 string               `json:"zone,omitempty"`
	RequiredCapabilities []string             `json:"required_capabilities,omitempty"`
	Tags                 map[string]string    `json:"tags,omitempty"`
	PreferredInstance    string               `json:"preferred_instance,omitempty"`
	AvoidInstances       []string             `json:"avoid_instances,omitempty"`
	MaxLatency           time.Duration        `json:"max_latency,omitempty"`
	MinResources         ResourceRequirements `json:"min_resources,omitempty"`
}

// ResourceRequirements defines minimum resource requirements
type ResourceRequirements struct {
	CPU         float64 `json:"cpu"`         // CPU cores
	Memory      int64   `json:"memory"`      // Memory in MB
	Connections int     `json:"connections"` // Max connections
	Bandwidth   int64   `json:"bandwidth"`   // Bandwidth in MB/s
}

// LoadBalancingStats represents load balancing statistics
type LoadBalancingStats struct {
	TotalRebalances     int                `json:"total_rebalances"`
	LastRebalance       time.Time          `json:"last_rebalance"`
	RoomMigrations      int                `json:"room_migrations"`
	InstanceLoadScores  map[string]float64 `json:"instance_load_scores"`
	LoadDistribution    map[string]int     `json:"load_distribution"`
	RebalanceEfficiency float64            `json:"rebalance_efficiency"`
	AverageLoadScore    float64            `json:"average_load_score"`
	LoadVariance        float64            `json:"load_variance"`
}

// ClusterHealth represents the overall health of the streaming cluster
type ClusterHealth struct {
	OverallStatus    string                    `json:"overall_status"`
	HealthyInstances int                       `json:"healthy_instances"`
	TotalInstances   int                       `json:"total_instances"`
	InstanceHealth   map[string]InstanceHealth `json:"instance_health"`
	RoomDistribution map[string]int            `json:"room_distribution"`
	LoadBalance      float64                   `json:"load_balance"`
	FailoverCapacity float64                   `json:"failover_capacity"`
	LastHealthCheck  time.Time                 `json:"last_health_check"`
}

// CoordinationStats represents coordination statistics
type CoordinationStats struct {
	MessagesRouted         int64         `json:"messages_routed"`
	RoomAllocations        int           `json:"room_allocations"`
	InstanceSwitches       int           `json:"instance_switches"`
	FailoverEvents         int           `json:"failover_events"`
	AverageLatency         time.Duration `json:"average_latency"`
	ThroughputMBps         float64       `json:"throughput_mbps"`
	ErrorRate              float64       `json:"error_rate"`
	CoordinationEfficiency float64       `json:"coordination_efficiency"`
}

// Event handlers
type InstanceEventHandler func(instanceID string, metadata InstanceMetadata)
type RoomMigrationHandler func(roomID string, fromInstance, toInstance string)
type LoadRebalanceHandler func(stats LoadBalancingStats)

// StreamingCoordinatorConfig contains coordinator configuration
type StreamingCoordinatorConfig struct {
	InstanceID                string        `yaml:"instance_id"`
	Region                    string        `yaml:"region"`
	Zone                      string        `yaml:"zone"`
	LoadBalanceInterval       time.Duration `yaml:"load_balance_interval" default:"60s"`
	HealthCheckInterval       time.Duration `yaml:"health_check_interval" default:"30s"`
	FailoverTimeout           time.Duration `yaml:"failover_timeout" default:"30s"`
	MaxRoomsPerInstance       int           `yaml:"max_rooms_per_instance" default:"1000"`
	MaxConnectionsPerInstance int           `yaml:"max_connections_per_instance" default:"10000"`
	LoadBalanceThreshold      float64       `yaml:"load_balance_threshold" default:"0.8"`
	FailoverThreshold         float64       `yaml:"failover_threshold" default:"0.95"`
	ReplicationFactor         int           `yaml:"replication_factor" default:"1"`
	EnableAutoRebalance       bool          `yaml:"enable_auto_rebalance" default:"true"`
	EnableAutoFailover        bool          `yaml:"enable_auto_failover" default:"true"`
	MigrationTimeout          time.Duration `yaml:"migration_timeout" default:"60s"`
	MaxMigrationsPerMinute    int           `yaml:"max_migrations_per_minute" default:"10"`
}

// DefaultStreamingCoordinatorConfig returns default coordinator configuration
func DefaultStreamingCoordinatorConfig() StreamingCoordinatorConfig {
	return StreamingCoordinatorConfig{
		LoadBalanceInterval:       60 * time.Second,
		HealthCheckInterval:       30 * time.Second,
		FailoverTimeout:           30 * time.Second,
		MaxRoomsPerInstance:       1000,
		MaxConnectionsPerInstance: 10000,
		LoadBalanceThreshold:      0.8,
		FailoverThreshold:         0.95,
		ReplicationFactor:         1,
		EnableAutoRebalance:       true,
		EnableAutoFailover:        true,
		MigrationTimeout:          60 * time.Second,
		MaxMigrationsPerMinute:    10,
	}
}

// DefaultStreamingCoordinator implements StreamingCoordinator
type DefaultStreamingCoordinator struct {
	instanceID string
	scaler     RedisScaler
	config     StreamingCoordinatorConfig
	logger     common.Logger
	metrics    common.Metrics

	// State
	running         bool
	roomAllocations map[string]*RoomAllocation
	allocationsMu   sync.RWMutex

	// Event handlers
	instanceJoinedHandler InstanceEventHandler
	instanceLeftHandler   InstanceEventHandler
	roomMigratedHandler   RoomMigrationHandler
	loadRebalancedHandler LoadRebalanceHandler
	handlersMu            sync.RWMutex

	// Background processes
	loadBalanceTicker *time.Ticker
	healthCheckTicker *time.Ticker
	stopCh            chan struct{}
	wg                sync.WaitGroup

	// Statistics
	stats   CoordinationStats
	statsMu sync.RWMutex

	// Load balancing
	lastRebalance    time.Time
	migrationCounter map[time.Time]int
	migrationMu      sync.Mutex
}

// NewDefaultStreamingCoordinator creates a new streaming coordinator
func NewDefaultStreamingCoordinator(
	instanceID string,
	scaler RedisScaler,
	config StreamingCoordinatorConfig,
	logger common.Logger,
	metrics common.Metrics,
) StreamingCoordinator {
	config.InstanceID = instanceID

	return &DefaultStreamingCoordinator{
		instanceID:       instanceID,
		scaler:           scaler,
		config:           config,
		logger:           logger,
		metrics:          metrics,
		roomAllocations:  make(map[string]*RoomAllocation),
		stopCh:           make(chan struct{}),
		migrationCounter: make(map[time.Time]int),
	}
}

// Lifecycle methods

// Start starts the coordinator
func (c *DefaultStreamingCoordinator) Start(ctx context.Context) error {
	if c.running {
		return nil
	}

	// Register this instance
	metadata := InstanceMetadata{
		Version:      "1.0.0", // Should come from build info
		Region:       c.config.Region,
		Zone:         c.config.Zone,
		Capabilities: []string{"websocket", "sse", "polling"},
		MaxRooms:     c.config.MaxRoomsPerInstance,
		MaxConns:     c.config.MaxConnectionsPerInstance,
	}

	if err := c.scaler.RegisterInstance(ctx, c.instanceID, metadata); err != nil {
		return fmt.Errorf("failed to register instance: %w", err)
	}

	// Load existing room allocations
	if err := c.loadRoomAllocations(ctx); err != nil {
		if c.logger != nil {
			c.logger.Warn("failed to load room allocations", logger.Error(err))
		}
	}

	// Start background processes
	c.startBackgroundProcesses()

	c.running = true

	if c.logger != nil {
		c.logger.Info("streaming coordinator started",
			logger.String("instance_id", c.instanceID),
			logger.String("region", c.config.Region),
			logger.String("zone", c.config.Zone),
		)
	}

	if c.metrics != nil {
		c.metrics.Counter("streaming.coordinator.started").Inc()
	}

	return nil
}

// Stop stops the coordinator
func (c *DefaultStreamingCoordinator) Stop(ctx context.Context) error {
	if !c.running {
		return nil
	}

	// Stop background processes
	c.stopBackgroundProcesses()

	// Unregister this instance
	if err := c.scaler.UnregisterInstance(ctx, c.instanceID); err != nil {
		if c.logger != nil {
			c.logger.Warn("failed to unregister instance", logger.Error(err))
		}
	}

	c.running = false

	if c.logger != nil {
		c.logger.Info("streaming coordinator stopped")
	}

	return nil
}

// IsRunning returns true if the coordinator is running
func (c *DefaultStreamingCoordinator) IsRunning() bool {
	return c.running
}

// Instance management

// RegisterInstance registers this instance (called during Start)
func (c *DefaultStreamingCoordinator) RegisterInstance(metadata InstanceMetadata) error {
	ctx := context.Background()
	return c.scaler.RegisterInstance(ctx, c.instanceID, metadata)
}

// UnregisterInstance unregisters this instance
func (c *DefaultStreamingCoordinator) UnregisterInstance() error {
	ctx := context.Background()
	return c.scaler.UnregisterInstance(ctx, c.instanceID)
}

// GetInstanceID returns the instance ID
func (c *DefaultStreamingCoordinator) GetInstanceID() string {
	return c.instanceID
}

// UpdateInstanceHealth updates this instance's health
func (c *DefaultStreamingCoordinator) UpdateInstanceHealth(health InstanceHealth) error {
	ctx := context.Background()
	return c.scaler.UpdateInstanceHealth(ctx, c.instanceID, health)
}

// Room coordination

// CreateRoom creates a room and allocates it to an optimal instance
func (c *DefaultStreamingCoordinator) CreateRoom(ctx context.Context, roomID string, config streaming.RoomConfig) (*RoomAllocation, error) {
	// Check if room already exists
	if allocation := c.getRoomAllocation(roomID); allocation != nil {
		return allocation, nil
	}

	// Find optimal instance for the room
	criteria := AllocationCriteria{
		Region: c.config.Region, // Prefer same region
		Zone:   c.config.Zone,   // Prefer same zone
	}

	instanceID, err := c.GetOptimalInstance(ctx, criteria)
	if err != nil {
		return nil, fmt.Errorf("failed to find optimal instance: %w", err)
	}

	// Create allocation
	allocation := &RoomAllocation{
		RoomID:           roomID,
		PrimaryInstance:  instanceID,
		ReplicaInstances: []string{}, // TODO: Add replica support
		CreatedAt:        time.Now(),
		LoadScore:        0.0,
	}

	// Register room with the scaler
	if err := c.scaler.RegisterRoom(ctx, roomID, instanceID); err != nil {
		return nil, fmt.Errorf("failed to register room: %w", err)
	}

	// Store allocation
	c.setRoomAllocation(roomID, allocation)

	// Update statistics
	c.statsMu.Lock()
	c.stats.RoomAllocations++
	c.statsMu.Unlock()

	if c.logger != nil {
		c.logger.Info("room allocated",
			logger.String("room_id", roomID),
			logger.String("instance_id", instanceID),
		)
	}

	if c.metrics != nil {
		c.metrics.Counter("streaming.coordinator.rooms.allocated").Inc()
	}

	return allocation, nil
}

// DeleteRoom deletes a room allocation
func (c *DefaultStreamingCoordinator) DeleteRoom(ctx context.Context, roomID string) error {
	allocation := c.getRoomAllocation(roomID)
	if allocation == nil {
		return common.ErrServiceNotFound(roomID)
	}

	// Unregister from all instances
	if err := c.scaler.UnregisterRoom(ctx, roomID, allocation.PrimaryInstance); err != nil {
		return fmt.Errorf("failed to unregister room: %w", err)
	}

	for _, instanceID := range allocation.ReplicaInstances {
		c.scaler.UnregisterRoom(ctx, roomID, instanceID)
	}

	// Remove allocation
	c.removeRoomAllocation(roomID)

	if c.logger != nil {
		c.logger.Info("room deallocated",
			logger.String("room_id", roomID),
			logger.String("instance_id", allocation.PrimaryInstance),
		)
	}

	if c.metrics != nil {
		c.metrics.Counter("streaming.coordinator.rooms.deallocated").Inc()
	}

	return nil
}

// GetRoomAllocation returns the allocation for a room
func (c *DefaultStreamingCoordinator) GetRoomAllocation(ctx context.Context, roomID string) (*RoomAllocation, error) {
	allocation := c.getRoomAllocation(roomID)
	if allocation == nil {
		return nil, common.ErrServiceNotFound(roomID)
	}
	return allocation, nil
}

// MigrateRoom migrates a room to a different instance
func (c *DefaultStreamingCoordinator) MigrateRoom(ctx context.Context, roomID string, targetInstanceID string) error {
	allocation := c.getRoomAllocation(roomID)
	if allocation == nil {
		return common.ErrServiceNotFound(roomID)
	}

	if allocation.PrimaryInstance == targetInstanceID {
		return nil // Already on target instance
	}

	// Check migration rate limit
	if !c.canMigrate() {
		return fmt.Errorf("migration rate limit exceeded")
	}

	oldInstanceID := allocation.PrimaryInstance

	// Update allocation
	allocation.PrimaryInstance = targetInstanceID
	allocation.LastMigration = time.Now()
	allocation.MigrationCount++

	// Update in scaler
	if err := c.scaler.UnregisterRoom(ctx, roomID, oldInstanceID); err != nil {
		return fmt.Errorf("failed to unregister room from old instance: %w", err)
	}

	if err := c.scaler.RegisterRoom(ctx, roomID, targetInstanceID); err != nil {
		// Rollback
		c.scaler.RegisterRoom(ctx, roomID, oldInstanceID)
		allocation.PrimaryInstance = oldInstanceID
		return fmt.Errorf("failed to register room on new instance: %w", err)
	}

	// Record migration
	c.recordMigration()

	// Notify handlers
	c.handlersMu.RLock()
	handler := c.roomMigratedHandler
	c.handlersMu.RUnlock()

	if handler != nil {
		go handler(roomID, oldInstanceID, targetInstanceID)
	}

	// Update statistics
	c.statsMu.Lock()
	c.stats.InstanceSwitches++
	c.statsMu.Unlock()

	if c.logger != nil {
		c.logger.Info("room migrated",
			logger.String("room_id", roomID),
			logger.String("from_instance", oldInstanceID),
			logger.String("to_instance", targetInstanceID),
		)
	}

	if c.metrics != nil {
		c.metrics.Counter("streaming.coordinator.rooms.migrated").Inc()
	}

	return nil
}

// Load balancing

// GetOptimalInstance finds the optimal instance for allocation
func (c *DefaultStreamingCoordinator) GetOptimalInstance(ctx context.Context, criteria AllocationCriteria) (string, error) {
	instances, err := c.scaler.GetInstances(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get instances: %w", err)
	}

	if len(instances) == 0 {
		return "", fmt.Errorf("no available instances")
	}

	// Filter instances based on criteria
	candidates := c.filterInstances(instances, criteria)
	if len(candidates) == 0 {
		return "", fmt.Errorf("no instances match criteria")
	}

	// Score instances and select the best one
	scores := make(map[string]float64)
	for _, instance := range candidates {
		score, err := c.calculateInstanceScore(ctx, instance.ID)
		if err != nil {
			continue
		}
		scores[instance.ID] = score
	}

	if len(scores) == 0 {
		return "", fmt.Errorf("failed to score any instances")
	}

	// Select instance with lowest score (best)
	var bestInstance string
	var bestScore float64 = math.Inf(1)

	for instanceID, score := range scores {
		if score < bestScore {
			bestScore = score
			bestInstance = instanceID
		}
	}

	return bestInstance, nil
}

// BalanceLoad performs load balancing across instances
func (c *DefaultStreamingCoordinator) BalanceLoad(ctx context.Context) error {
	if !c.config.EnableAutoRebalance {
		return nil
	}

	instances, err := c.scaler.GetInstances(ctx)
	if err != nil {
		return fmt.Errorf("failed to get instances: %w", err)
	}

	// Calculate load scores for all instances
	loadScores := make(map[string]float64)
	for _, instance := range instances {
		score, err := c.calculateInstanceScore(ctx, instance.ID)
		if err != nil {
			continue
		}
		loadScores[instance.ID] = score
	}

	// Find overloaded instances
	overloadedInstances := make([]string, 0)
	underloadedInstances := make([]string, 0)

	for instanceID, score := range loadScores {
		if score > c.config.LoadBalanceThreshold {
			overloadedInstances = append(overloadedInstances, instanceID)
		} else if score < c.config.LoadBalanceThreshold*0.5 {
			underloadedInstances = append(underloadedInstances, instanceID)
		}
	}

	if len(overloadedInstances) == 0 || len(underloadedInstances) == 0 {
		return nil // No balancing needed
	}

	// Migrate rooms from overloaded to underloaded instances
	migrations := 0
	for _, overloadedInstance := range overloadedInstances {
		rooms, err := c.scaler.GetInstanceRooms(ctx, overloadedInstance)
		if err != nil {
			continue
		}

		// Sort rooms by some criteria (e.g., least active)
		// For now, just take the first few rooms
		for i, roomID := range rooms {
			if i >= c.config.MaxMigrationsPerMinute || migrations >= c.config.MaxMigrationsPerMinute {
				break
			}

			// Find best target instance
			targetInstance := c.selectBestTarget(underloadedInstances, loadScores)
			if targetInstance == "" {
				continue
			}

			// Migrate room
			if err := c.MigrateRoom(ctx, roomID, targetInstance); err != nil {
				if c.logger != nil {
					c.logger.Warn("failed to migrate room during load balancing",
						logger.String("room_id", roomID),
						logger.String("from", overloadedInstance),
						logger.String("to", targetInstance),
						logger.Error(err),
					)
				}
				continue
			}

			migrations++
		}
	}

	// Update statistics
	stats := LoadBalancingStats{
		TotalRebalances:    1,
		LastRebalance:      time.Now(),
		RoomMigrations:     migrations,
		InstanceLoadScores: loadScores,
	}

	c.lastRebalance = time.Now()

	// Notify handlers
	c.handlersMu.RLock()
	handler := c.loadRebalancedHandler
	c.handlersMu.RUnlock()

	if handler != nil {
		go handler(stats)
	}

	if c.logger != nil {
		c.logger.Info("load balancing completed",
			logger.Int("migrations", migrations),
			logger.Int("overloaded_instances", len(overloadedInstances)),
			logger.Int("underloaded_instances", len(underloadedInstances)),
		)
	}

	if c.metrics != nil {
		c.metrics.Counter("streaming.coordinator.load_balances").Inc()
		c.metrics.Histogram("streaming.coordinator.migrations_per_balance").Observe(float64(migrations))
	}

	return nil
}

// GetLoadBalancingStats returns load balancing statistics
func (c *DefaultStreamingCoordinator) GetLoadBalancingStats() LoadBalancingStats {
	// Implementation would collect and return actual stats
	return LoadBalancingStats{
		LastRebalance: c.lastRebalance,
	}
}

// Message routing

// RouteMessage routes a message to the appropriate instance
func (c *DefaultStreamingCoordinator) RouteMessage(ctx context.Context, roomID string, message *streaming.Message) error {
	allocation := c.getRoomAllocation(roomID)
	if allocation == nil {
		return common.ErrServiceNotFound(fmt.Sprintf("room %s not allocated", roomID))
	}

	// Route to primary instance via Redis
	if err := c.scaler.PublishMessage(ctx, roomID, message); err != nil {
		return fmt.Errorf("failed to route message: %w", err)
	}

	// Update statistics
	c.statsMu.Lock()
	c.stats.MessagesRouted++
	c.statsMu.Unlock()

	if c.metrics != nil {
		c.metrics.Counter("streaming.coordinator.messages.routed").Inc()
	}

	return nil
}

// BroadcastToRoom broadcasts a message to all instances hosting a room
func (c *DefaultStreamingCoordinator) BroadcastToRoom(ctx context.Context, roomID string, message *streaming.Message) error {
	return c.scaler.PublishMessage(ctx, roomID, message)
}

// BroadcastToAll broadcasts a message to all instances
func (c *DefaultStreamingCoordinator) BroadcastToAll(ctx context.Context, message *streaming.Message) error {
	instances, err := c.scaler.GetInstances(ctx)
	if err != nil {
		return fmt.Errorf("failed to get instances: %w", err)
	}

	// Broadcast to all instances (implementation would use a global channel)
	for _, instance := range instances {
		// This is a simplified implementation
		// In practice, you'd use a global broadcast mechanism
		_ = instance
	}

	return nil
}

// Event handling

// OnInstanceJoined sets the instance joined handler
func (c *DefaultStreamingCoordinator) OnInstanceJoined(handler InstanceEventHandler) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.instanceJoinedHandler = handler
}

// OnInstanceLeft sets the instance left handler
func (c *DefaultStreamingCoordinator) OnInstanceLeft(handler InstanceEventHandler) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.instanceLeftHandler = handler
}

// OnRoomMigrated sets the room migrated handler
func (c *DefaultStreamingCoordinator) OnRoomMigrated(handler RoomMigrationHandler) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.roomMigratedHandler = handler
}

// OnLoadRebalanced sets the load rebalanced handler
func (c *DefaultStreamingCoordinator) OnLoadRebalanced(handler LoadRebalanceHandler) {
	c.handlersMu.Lock()
	defer c.handlersMu.Unlock()
	c.loadRebalancedHandler = handler
}

// Health monitoring

// MonitorHealth monitors the health of all instances
func (c *DefaultStreamingCoordinator) MonitorHealth(ctx context.Context) error {
	instances, err := c.scaler.GetInstances(ctx)
	if err != nil {
		return fmt.Errorf("failed to get instances: %w", err)
	}

	unhealthyInstances := make([]string, 0)

	for _, instance := range instances {
		health, err := c.scaler.GetInstanceHealth(ctx, instance.ID)
		if err != nil || health == nil {
			unhealthyInstances = append(unhealthyInstances, instance.ID)
			continue
		}

		// Check if instance is unhealthy
		if health.Status != "healthy" {
			unhealthyInstances = append(unhealthyInstances, instance.ID)

			// Trigger failover if enabled
			if c.config.EnableAutoFailover {
				c.handleInstanceFailure(ctx, instance.ID)
			}
		}
	}

	if len(unhealthyInstances) > 0 && c.logger != nil {
		c.logger.Warn("unhealthy instances detected",
			logger.String("instances", fmt.Sprintf("%v", unhealthyInstances)),
		)
	}

	return nil
}

// GetClusterHealth returns the overall cluster health
func (c *DefaultStreamingCoordinator) GetClusterHealth() ClusterHealth {
	ctx := context.Background()
	instances, err := c.scaler.GetInstances(ctx)
	if err != nil {
		return ClusterHealth{
			OverallStatus:   "unknown",
			LastHealthCheck: time.Now(),
		}
	}

	healthyCount := 0
	instanceHealth := make(map[string]InstanceHealth)

	for _, instance := range instances {
		health, err := c.scaler.GetInstanceHealth(ctx, instance.ID)
		if err != nil || health == nil {
			instanceHealth[instance.ID] = InstanceHealth{Status: "unknown"}
			continue
		}

		instanceHealth[instance.ID] = *health
		if health.Status == "healthy" {
			healthyCount++
		}
	}

	overallStatus := "healthy"
	if healthyCount == 0 {
		overallStatus = "critical"
	} else if float64(healthyCount)/float64(len(instances)) < 0.5 {
		overallStatus = "degraded"
	}

	return ClusterHealth{
		OverallStatus:    overallStatus,
		HealthyInstances: healthyCount,
		TotalInstances:   len(instances),
		InstanceHealth:   instanceHealth,
		LastHealthCheck:  time.Now(),
	}
}

// GetCoordinationStats returns coordination statistics
func (c *DefaultStreamingCoordinator) GetCoordinationStats() CoordinationStats {
	c.statsMu.RLock()
	defer c.statsMu.RUnlock()
	return c.stats
}

// Helper methods

// filterInstances filters instances based on criteria
func (c *DefaultStreamingCoordinator) filterInstances(instances []Instance, criteria AllocationCriteria) []Instance {
	var filtered []Instance

	for _, instance := range instances {
		// Check region
		if criteria.Region != "" && instance.Metadata.Region != criteria.Region {
			continue
		}

		// Check zone
		if criteria.Zone != "" && instance.Metadata.Zone != criteria.Zone {
			continue
		}

		// Check capabilities
		if len(criteria.RequiredCapabilities) > 0 {
			hasAllCapabilities := true
			for _, required := range criteria.RequiredCapabilities {
				found := false
				for _, capability := range instance.Metadata.Capabilities {
					if capability == required {
						found = true
						break
					}
				}
				if !found {
					hasAllCapabilities = false
					break
				}
			}
			if !hasAllCapabilities {
				continue
			}
		}

		// Check tags
		if len(criteria.Tags) > 0 {
			hasAllTags := true
			for key, value := range criteria.Tags {
				if instanceValue, exists := instance.Metadata.Tags[key]; !exists || instanceValue != value {
					hasAllTags = false
					break
				}
			}
			if !hasAllTags {
				continue
			}
		}

		// Check avoided instances
		isAvoided := false
		for _, avoidedID := range criteria.AvoidInstances {
			if instance.ID == avoidedID {
				isAvoided = true
				break
			}
		}
		if isAvoided {
			continue
		}

		filtered = append(filtered, instance)
	}

	return filtered
}

// calculateInstanceScore calculates a load score for an instance
func (c *DefaultStreamingCoordinator) calculateInstanceScore(ctx context.Context, instanceID string) (float64, error) {
	// Get instance health
	health, err := c.scaler.GetInstanceHealth(ctx, instanceID)
	if err != nil {
		return math.Inf(1), err // Infinite score for unhealthy instances
	}

	if health == nil || health.Status != "healthy" {
		return math.Inf(1), nil
	}

	// Get room count
	rooms, err := c.scaler.GetInstanceRooms(ctx, instanceID)
	if err != nil {
		return math.Inf(1), err
	}

	// Get connection count
	connections, err := c.scaler.GetInstanceConnections(ctx, instanceID)
	if err != nil {
		return math.Inf(1), err
	}

	// Calculate score based on multiple factors
	roomScore := float64(len(rooms)) / float64(c.config.MaxRoomsPerInstance)
	connectionScore := float64(len(connections)) / float64(c.config.MaxConnectionsPerInstance)
	cpuScore := health.CPUUsage / 100.0
	memoryScore := health.MemoryUsage / 100.0

	// Weighted average
	score := (roomScore*0.3 + connectionScore*0.3 + cpuScore*0.2 + memoryScore*0.2)

	return score, nil
}

// selectBestTarget selects the best target instance for migration
func (c *DefaultStreamingCoordinator) selectBestTarget(candidates []string, loadScores map[string]float64) string {
	if len(candidates) == 0 {
		return ""
	}

	var bestInstance string
	var bestScore float64 = math.Inf(1)

	for _, instanceID := range candidates {
		if score, exists := loadScores[instanceID]; exists && score < bestScore {
			bestScore = score
			bestInstance = instanceID
		}
	}

	return bestInstance
}

// canMigrate checks if a migration is allowed based on rate limits
func (c *DefaultStreamingCoordinator) canMigrate() bool {
	c.migrationMu.Lock()
	defer c.migrationMu.Unlock()

	now := time.Now()
	cutoff := now.Add(-time.Minute)

	// Clean up old entries
	for timestamp := range c.migrationCounter {
		if timestamp.Before(cutoff) {
			delete(c.migrationCounter, timestamp)
		}
	}

	// Count migrations in the last minute
	totalMigrations := 0
	for _, count := range c.migrationCounter {
		totalMigrations += count
	}

	return totalMigrations < c.config.MaxMigrationsPerMinute
}

// recordMigration records a migration for rate limiting
func (c *DefaultStreamingCoordinator) recordMigration() {
	c.migrationMu.Lock()
	defer c.migrationMu.Unlock()

	now := time.Now().Truncate(time.Minute)
	c.migrationCounter[now]++
}

// handleInstanceFailure handles instance failure
func (c *DefaultStreamingCoordinator) handleInstanceFailure(ctx context.Context, instanceID string) {
	if c.logger != nil {
		c.logger.Warn("handling instance failure",
			logger.String("instance_id", instanceID),
		)
	}

	// Get rooms on the failed instance
	rooms, err := c.scaler.GetInstanceRooms(ctx, instanceID)
	if err != nil {
		return
	}

	// Migrate rooms to healthy instances
	for _, roomID := range rooms {
		// Find a healthy target instance
		criteria := AllocationCriteria{
			AvoidInstances: []string{instanceID},
		}

		targetInstance, err := c.GetOptimalInstance(ctx, criteria)
		if err != nil {
			continue
		}

		// Migrate room
		c.MigrateRoom(ctx, roomID, targetInstance)
	}

	// Update statistics
	c.statsMu.Lock()
	c.stats.FailoverEvents++
	c.statsMu.Unlock()

	if c.metrics != nil {
		c.metrics.Counter("streaming.coordinator.failovers").Inc()
	}
}

// Room allocation management

func (c *DefaultStreamingCoordinator) getRoomAllocation(roomID string) *RoomAllocation {
	c.allocationsMu.RLock()
	defer c.allocationsMu.RUnlock()
	return c.roomAllocations[roomID]
}

func (c *DefaultStreamingCoordinator) setRoomAllocation(roomID string, allocation *RoomAllocation) {
	c.allocationsMu.Lock()
	defer c.allocationsMu.Unlock()
	c.roomAllocations[roomID] = allocation
}

func (c *DefaultStreamingCoordinator) removeRoomAllocation(roomID string) {
	c.allocationsMu.Lock()
	defer c.allocationsMu.Unlock()
	delete(c.roomAllocations, roomID)
}

// loadRoomAllocations loads room allocations from the scaler
func (c *DefaultStreamingCoordinator) loadRoomAllocations(ctx context.Context) error {
	// This would load existing allocations from persistent storage
	// For now, we'll rebuild from instance data

	instances, err := c.scaler.GetInstances(ctx)
	if err != nil {
		return err
	}

	for _, instance := range instances {
		rooms, err := c.scaler.GetInstanceRooms(ctx, instance.ID)
		if err != nil {
			continue
		}

		for _, roomID := range rooms {
			if c.getRoomAllocation(roomID) == nil {
				allocation := &RoomAllocation{
					RoomID:          roomID,
					PrimaryInstance: instance.ID,
					CreatedAt:       time.Now(),
				}
				c.setRoomAllocation(roomID, allocation)
			}
		}
	}

	return nil
}

// Background processes

// startBackgroundProcesses starts background processes
func (c *DefaultStreamingCoordinator) startBackgroundProcesses() {
	// Load balancing
	if c.config.EnableAutoRebalance {
		c.loadBalanceTicker = time.NewTicker(c.config.LoadBalanceInterval)
		c.wg.Add(1)
		go c.loadBalanceLoop()
	}

	// Health checking
	c.healthCheckTicker = time.NewTicker(c.config.HealthCheckInterval)
	c.wg.Add(1)
	go c.healthCheckLoop()
}

// stopBackgroundProcesses stops background processes
func (c *DefaultStreamingCoordinator) stopBackgroundProcesses() {
	close(c.stopCh)

	if c.loadBalanceTicker != nil {
		c.loadBalanceTicker.Stop()
	}

	if c.healthCheckTicker != nil {
		c.healthCheckTicker.Stop()
	}

	c.wg.Wait()
}

// loadBalanceLoop runs the load balancing loop
func (c *DefaultStreamingCoordinator) loadBalanceLoop() {
	defer c.wg.Done()

	for {
		select {
		case <-c.loadBalanceTicker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := c.BalanceLoad(ctx); err != nil {
				if c.logger != nil {
					c.logger.Error("load balancing failed", logger.Error(err))
				}
			}
			cancel()

		case <-c.stopCh:
			return
		}
	}
}

// healthCheckLoop runs the health checking loop
func (c *DefaultStreamingCoordinator) healthCheckLoop() {
	defer c.wg.Done()

	for {
		select {
		case <-c.healthCheckTicker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := c.MonitorHealth(ctx); err != nil {
				if c.logger != nil {
					c.logger.Error("health monitoring failed", logger.Error(err))
				}
			}
			cancel()

		case <-c.stopCh:
			return
		}
	}
}
