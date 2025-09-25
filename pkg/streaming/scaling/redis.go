package scaling

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/streaming"
)

// RedisScaler handles Redis-based horizontal scaling for streaming
type RedisScaler interface {
	// Instance management
	RegisterInstance(ctx context.Context, instanceID string, metadata InstanceMetadata) error
	UnregisterInstance(ctx context.Context, instanceID string) error
	GetInstances(ctx context.Context) ([]Instance, error)
	GetInstance(ctx context.Context, instanceID string) (*Instance, error)

	// Message distribution
	PublishMessage(ctx context.Context, roomID string, message *streaming.Message) error
	SubscribeToRoom(ctx context.Context, roomID string, handler MessageHandler) error
	UnsubscribeFromRoom(ctx context.Context, roomID string) error

	// Room coordination
	RegisterRoom(ctx context.Context, roomID string, instanceID string) error
	UnregisterRoom(ctx context.Context, roomID string, instanceID string) error
	GetRoomInstances(ctx context.Context, roomID string) ([]string, error)
	GetInstanceRooms(ctx context.Context, instanceID string) ([]string, error)

	// User presence coordination
	SetUserPresence(ctx context.Context, userID string, presence UserPresence) error
	GetUserPresence(ctx context.Context, userID string) (*UserPresence, error)
	RemoveUserPresence(ctx context.Context, userID string) error
	GetRoomUsers(ctx context.Context, roomID string) ([]UserPresence, error)

	// Connection tracking
	RegisterConnection(ctx context.Context, connectionID string, connection ConnectionInfo) error
	UnregisterConnection(ctx context.Context, connectionID string) error
	GetUserConnections(ctx context.Context, userID string) ([]ConnectionInfo, error)
	GetInstanceConnections(ctx context.Context, instanceID string) ([]ConnectionInfo, error)

	// Health monitoring
	UpdateInstanceHealth(ctx context.Context, instanceID string, health InstanceHealth) error
	GetInstanceHealth(ctx context.Context, instanceID string) (*InstanceHealth, error)
	CleanupStaleInstances(ctx context.Context, timeout time.Duration) error

	// Statistics
	GetScalingStats(ctx context.Context) (*ScalingStats, error)
}

// MessageHandler handles distributed messages
type MessageHandler func(roomID string, message *streaming.Message) error

// Instance represents a streaming service instance
type Instance struct {
	ID           string           `json:"id"`
	Address      string           `json:"address"`
	Metadata     InstanceMetadata `json:"metadata"`
	Health       InstanceHealth   `json:"health"`
	RegisteredAt time.Time        `json:"registered_at"`
	LastSeen     time.Time        `json:"last_seen"`
}

// InstanceMetadata contains metadata about an instance
type InstanceMetadata struct {
	Version      string            `json:"version"`
	Region       string            `json:"region,omitempty"`
	Zone         string            `json:"zone,omitempty"`
	Capabilities []string          `json:"capabilities,omitempty"`
	Tags         map[string]string `json:"tags,omitempty"`
	MaxRooms     int               `json:"max_rooms"`
	MaxConns     int               `json:"max_connections"`
}

// InstanceHealth represents the health status of an instance
type InstanceHealth struct {
	Status          string        `json:"status"` // healthy, unhealthy, degraded
	RoomCount       int           `json:"room_count"`
	ConnectionCount int           `json:"connection_count"`
	MemoryUsage     float64       `json:"memory_usage"`
	CPUUsage        float64       `json:"cpu_usage"`
	LoadAverage     float64       `json:"load_average"`
	LastHeartbeat   time.Time     `json:"last_heartbeat"`
	Uptime          time.Duration `json:"uptime"`
}

// UserPresence represents user presence information
type UserPresence struct {
	UserID     string                   `json:"user_id"`
	Status     streaming.PresenceStatus `json:"status"`
	RoomID     string                   `json:"room_id"`
	InstanceID string                   `json:"instance_id"`
	Metadata   map[string]interface{}   `json:"metadata,omitempty"`
	LastSeen   time.Time                `json:"last_seen"`
}

// ConnectionInfo represents connection information for scaling
type ConnectionInfo struct {
	ID           string                 `json:"id"`
	UserID       string                 `json:"user_id"`
	RoomID       string                 `json:"room_id"`
	InstanceID   string                 `json:"instance_id"`
	Protocol     streaming.ProtocolType `json:"protocol"`
	RemoteAddr   string                 `json:"remote_addr"`
	ConnectedAt  time.Time              `json:"connected_at"`
	LastActivity time.Time              `json:"last_activity"`
}

// ScalingStats represents scaling statistics
type ScalingStats struct {
	TotalInstances       int            `json:"total_instances"`
	HealthyInstances     int            `json:"healthy_instances"`
	TotalRooms           int            `json:"total_rooms"`
	TotalConnections     int            `json:"total_connections"`
	TotalUsers           int            `json:"total_users"`
	MessageRate          float64        `json:"message_rate"`
	InstanceDistribution map[string]int `json:"instance_distribution"`
	RegionDistribution   map[string]int `json:"region_distribution"`
}

// RedisScalerConfig contains Redis scaler configuration
type RedisScalerConfig struct {
	Address           string        `yaml:"address" default:"localhost:6379"`
	Password          string        `yaml:"password"`
	DB                int           `yaml:"db" default:"0"`
	PoolSize          int           `yaml:"pool_size" default:"10"`
	MinIdleConns      int           `yaml:"min_idle_conns" default:"5"`
	MaxRetries        int           `yaml:"max_retries" default:"3"`
	RetryDelay        time.Duration `yaml:"retry_delay" default:"100ms"`
	DialTimeout       time.Duration `yaml:"dial_timeout" default:"5s"`
	ReadTimeout       time.Duration `yaml:"read_timeout" default:"3s"`
	WriteTimeout      time.Duration `yaml:"write_timeout" default:"3s"`
	PoolTimeout       time.Duration `yaml:"pool_timeout" default:"4s"`
	IdleTimeout       time.Duration `yaml:"idle_timeout" default:"5m"`
	KeyPrefix         string        `yaml:"key_prefix" default:"forge:streaming"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval" default:"30s"`
	InstanceTTL       time.Duration `yaml:"instance_ttl" default:"90s"`
	PresenceTTL       time.Duration `yaml:"presence_ttl" default:"300s"`
	ConnectionTTL     time.Duration `yaml:"connection_ttl" default:"600s"`
	EnableCompression bool          `yaml:"enable_compression" default:"false"`
}

// DefaultRedisScalerConfig returns default Redis scaler configuration
func DefaultRedisScalerConfig() RedisScalerConfig {
	return RedisScalerConfig{
		Address:           "localhost:6379",
		DB:                0,
		PoolSize:          10,
		MinIdleConns:      5,
		MaxRetries:        3,
		RetryDelay:        100 * time.Millisecond,
		DialTimeout:       5 * time.Second,
		ReadTimeout:       3 * time.Second,
		WriteTimeout:      3 * time.Second,
		PoolTimeout:       4 * time.Second,
		IdleTimeout:       5 * time.Minute,
		KeyPrefix:         "forge:streaming",
		HeartbeatInterval: 30 * time.Second,
		InstanceTTL:       90 * time.Second,
		PresenceTTL:       5 * time.Minute,
		ConnectionTTL:     10 * time.Minute,
		EnableCompression: false,
	}
}

// DefaultRedisScaler implements RedisScaler using Redis
type DefaultRedisScaler struct {
	client        *redis.Client
	pubsub        *redis.PubSub
	config        RedisScalerConfig
	instanceID    string
	logger        common.Logger
	metrics       common.Metrics
	subscriptions map[string]MessageHandler
	subMu         sync.RWMutex
	heartbeatStop chan struct{}
	cleanupStop   chan struct{}
	wg            sync.WaitGroup
}

// NewDefaultRedisScaler creates a new Redis-based scaler
func NewDefaultRedisScaler(
	instanceID string,
	config RedisScalerConfig,
	logger common.Logger,
	metrics common.Metrics,
) (RedisScaler, error) {
	// Create Redis client
	client := redis.NewClient(&redis.Options{
		Addr:         config.Address,
		Password:     config.Password,
		DB:           config.DB,
		PoolSize:     config.PoolSize,
		MinIdleConns: config.MinIdleConns,
		MaxRetries:   config.MaxRetries,
		// RetryDelay:   config.RetryDelay,
		DialTimeout:  config.DialTimeout,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
		PoolTimeout:  config.PoolTimeout,
		// IdleTimeout:  config.IdleTimeout,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	scaler := &DefaultRedisScaler{
		client:        client,
		config:        config,
		instanceID:    instanceID,
		logger:        logger,
		metrics:       metrics,
		subscriptions: make(map[string]MessageHandler),
		heartbeatStop: make(chan struct{}),
		cleanupStop:   make(chan struct{}),
	}

	// OnStart background processes
	scaler.startHeartbeat()
	scaler.startCleanup()

	return scaler, nil
}

// Instance management

// RegisterInstance registers this instance in Redis
func (r *DefaultRedisScaler) RegisterInstance(ctx context.Context, instanceID string, metadata InstanceMetadata) error {
	instance := Instance{
		ID:           instanceID,
		Metadata:     metadata,
		RegisteredAt: time.Now(),
		LastSeen:     time.Now(),
	}

	data, err := json.Marshal(instance)
	if err != nil {
		return fmt.Errorf("failed to marshal instance data: %w", err)
	}

	key := r.instanceKey(instanceID)
	if err := r.client.Set(ctx, key, data, r.config.InstanceTTL).Err(); err != nil {
		return fmt.Errorf("failed to register instance in Redis: %w", err)
	}

	// Add to instances set
	instancesKey := r.key("instances")
	if err := r.client.SAdd(ctx, instancesKey, instanceID).Err(); err != nil {
		return fmt.Errorf("failed to add instance to set: %w", err)
	}

	if r.logger != nil {
		r.logger.Info("instance registered in Redis",
			logger.String("instance_id", instanceID),
			logger.String("version", metadata.Version),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("streaming.scaling.instances.registered").Inc()
	}

	return nil
}

// UnregisterInstance removes this instance from Redis
func (r *DefaultRedisScaler) UnregisterInstance(ctx context.Context, instanceID string) error {
	// Remove instance data
	key := r.instanceKey(instanceID)
	if err := r.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to remove instance from Redis: %w", err)
	}

	// Remove from instances set
	instancesKey := r.key("instances")
	if err := r.client.SRem(ctx, instancesKey, instanceID).Err(); err != nil {
		return fmt.Errorf("failed to remove instance from set: %w", err)
	}

	// Cleanup instance-specific data
	if err := r.cleanupInstanceData(ctx, instanceID); err != nil {
		if r.logger != nil {
			r.logger.Warn("failed to cleanup instance data",
				logger.String("instance_id", instanceID),
				logger.Error(err),
			)
		}
	}

	if r.logger != nil {
		r.logger.Info("instance unregistered from Redis",
			logger.String("instance_id", instanceID),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("streaming.scaling.instances.unregistered").Inc()
	}

	return nil
}

// GetInstances returns all registered instances
func (r *DefaultRedisScaler) GetInstances(ctx context.Context) ([]Instance, error) {
	instancesKey := r.key("instances")
	instanceIDs, err := r.client.SMembers(ctx, instancesKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get instance IDs: %w", err)
	}

	var instances []Instance
	for _, instanceID := range instanceIDs {
		instance, err := r.GetInstance(ctx, instanceID)
		if err != nil {
			if r.logger != nil {
				r.logger.Warn("failed to get instance data",
					logger.String("instance_id", instanceID),
					logger.Error(err),
				)
			}
			continue
		}
		if instance != nil {
			instances = append(instances, *instance)
		}
	}

	return instances, nil
}

// GetInstance returns a specific instance
func (r *DefaultRedisScaler) GetInstance(ctx context.Context, instanceID string) (*Instance, error) {
	key := r.instanceKey(instanceID)
	data, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // Instance not found
		}
		return nil, fmt.Errorf("failed to get instance from Redis: %w", err)
	}

	var instance Instance
	if err := json.Unmarshal([]byte(data), &instance); err != nil {
		return nil, fmt.Errorf("failed to unmarshal instance data: %w", err)
	}

	return &instance, nil
}

// Message distribution

// PublishMessage publishes a message to a room channel
func (r *DefaultRedisScaler) PublishMessage(ctx context.Context, roomID string, message *streaming.Message) error {
	data, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	channel := r.roomChannelKey(roomID)
	if err := r.client.Publish(ctx, channel, data).Err(); err != nil {
		return fmt.Errorf("failed to publish message to Redis: %w", err)
	}

	if r.metrics != nil {
		r.metrics.Counter("streaming.scaling.messages.published").Inc()
	}

	return nil
}

// SubscribeToRoom subscribes to a room's message channel
func (r *DefaultRedisScaler) SubscribeToRoom(ctx context.Context, roomID string, handler MessageHandler) error {
	r.subMu.Lock()
	defer r.subMu.Unlock()

	// Check if already subscribed
	if _, exists := r.subscriptions[roomID]; exists {
		return fmt.Errorf("already subscribed to room: %s", roomID)
	}

	// Create subscription if first room
	if len(r.subscriptions) == 0 {
		r.pubsub = r.client.Subscribe(ctx)
	}

	// Subscribe to channel
	channel := r.roomChannelKey(roomID)
	if err := r.pubsub.Subscribe(ctx, channel); err != nil {
		return fmt.Errorf("failed to subscribe to room channel: %w", err)
	}

	// Store handler
	r.subscriptions[roomID] = handler

	// OnStart message processing if first subscription
	if len(r.subscriptions) == 1 {
		r.wg.Add(1)
		go r.processSubscriptions()
	}

	if r.logger != nil {
		r.logger.Debug("subscribed to room channel",
			logger.String("room_id", roomID),
			logger.String("channel", channel),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("streaming.scaling.subscriptions.added").Inc()
	}

	return nil
}

// UnsubscribeFromRoom unsubscribes from a room's message channel
func (r *DefaultRedisScaler) UnsubscribeFromRoom(ctx context.Context, roomID string) error {
	r.subMu.Lock()
	defer r.subMu.Unlock()

	// Check if subscribed
	if _, exists := r.subscriptions[roomID]; !exists {
		return fmt.Errorf("not subscribed to room: %s", roomID)
	}

	// Unsubscribe from channel
	channel := r.roomChannelKey(roomID)
	if r.pubsub != nil {
		if err := r.pubsub.Unsubscribe(ctx, channel); err != nil {
			return fmt.Errorf("failed to unsubscribe from room channel: %w", err)
		}
	}

	// Remove handler
	delete(r.subscriptions, roomID)

	// Close pubsub if no more subscriptions
	if len(r.subscriptions) == 0 && r.pubsub != nil {
		r.pubsub.Close()
		r.pubsub = nil
	}

	if r.logger != nil {
		r.logger.Debug("unsubscribed from room channel",
			logger.String("room_id", roomID),
			logger.String("channel", channel),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("streaming.scaling.subscriptions.removed").Inc()
	}

	return nil
}

// Room coordination

// RegisterRoom registers a room on this instance
func (r *DefaultRedisScaler) RegisterRoom(ctx context.Context, roomID string, instanceID string) error {
	// Add room to instance's rooms set
	instanceRoomsKey := r.instanceRoomsKey(instanceID)
	if err := r.client.SAdd(ctx, instanceRoomsKey, roomID).Err(); err != nil {
		return fmt.Errorf("failed to add room to instance set: %w", err)
	}

	// Add instance to room's instances set
	roomInstancesKey := r.roomInstancesKey(roomID)
	if err := r.client.SAdd(ctx, roomInstancesKey, instanceID).Err(); err != nil {
		return fmt.Errorf("failed to add instance to room set: %w", err)
	}

	// Set TTL for cleanup
	r.client.Expire(ctx, instanceRoomsKey, r.config.InstanceTTL)
	r.client.Expire(ctx, roomInstancesKey, r.config.InstanceTTL)

	if r.metrics != nil {
		r.metrics.Counter("streaming.scaling.rooms.registered").Inc()
	}

	return nil
}

// UnregisterRoom unregisters a room from this instance
func (r *DefaultRedisScaler) UnregisterRoom(ctx context.Context, roomID string, instanceID string) error {
	// Remove room from instance's rooms set
	instanceRoomsKey := r.instanceRoomsKey(instanceID)
	if err := r.client.SRem(ctx, instanceRoomsKey, roomID).Err(); err != nil {
		return fmt.Errorf("failed to remove room from instance set: %w", err)
	}

	// Remove instance from room's instances set
	roomInstancesKey := r.roomInstancesKey(roomID)
	if err := r.client.SRem(ctx, roomInstancesKey, instanceID).Err(); err != nil {
		return fmt.Errorf("failed to remove instance from room set: %w", err)
	}

	if r.metrics != nil {
		r.metrics.Counter("streaming.scaling.rooms.unregistered").Inc()
	}

	return nil
}

// GetRoomInstances returns all instances hosting a room
func (r *DefaultRedisScaler) GetRoomInstances(ctx context.Context, roomID string) ([]string, error) {
	roomInstancesKey := r.roomInstancesKey(roomID)
	instances, err := r.client.SMembers(ctx, roomInstancesKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get room instances: %w", err)
	}
	return instances, nil
}

// GetInstanceRooms returns all rooms hosted by an instance
func (r *DefaultRedisScaler) GetInstanceRooms(ctx context.Context, instanceID string) ([]string, error) {
	instanceRoomsKey := r.instanceRoomsKey(instanceID)
	rooms, err := r.client.SMembers(ctx, instanceRoomsKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get instance rooms: %w", err)
	}
	return rooms, nil
}

// User presence coordination

// SetUserPresence sets user presence information
func (r *DefaultRedisScaler) SetUserPresence(ctx context.Context, userID string, presence UserPresence) error {
	data, err := json.Marshal(presence)
	if err != nil {
		return fmt.Errorf("failed to marshal presence data: %w", err)
	}

	key := r.userPresenceKey(userID)
	if err := r.client.Set(ctx, key, data, r.config.PresenceTTL).Err(); err != nil {
		return fmt.Errorf("failed to set user presence: %w", err)
	}

	// Add to room users set if in a room
	if presence.RoomID != "" {
		roomUsersKey := r.roomUsersKey(presence.RoomID)
		if err := r.client.SAdd(ctx, roomUsersKey, userID).Err(); err != nil {
			return fmt.Errorf("failed to add user to room set: %w", err)
		}
		r.client.Expire(ctx, roomUsersKey, r.config.PresenceTTL)
	}

	if r.metrics != nil {
		r.metrics.Counter("streaming.scaling.presence.updated").Inc()
	}

	return nil
}

// GetUserPresence gets user presence information
func (r *DefaultRedisScaler) GetUserPresence(ctx context.Context, userID string) (*UserPresence, error) {
	key := r.userPresenceKey(userID)
	data, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // Presence not found
		}
		return nil, fmt.Errorf("failed to get user presence: %w", err)
	}

	var presence UserPresence
	if err := json.Unmarshal([]byte(data), &presence); err != nil {
		return nil, fmt.Errorf("failed to unmarshal presence data: %w", err)
	}

	return &presence, nil
}

// RemoveUserPresence removes user presence information
func (r *DefaultRedisScaler) RemoveUserPresence(ctx context.Context, userID string) error {
	// Get current presence to remove from room set
	presence, err := r.GetUserPresence(ctx, userID)
	if err != nil {
		return err
	}

	// Remove from room users set if applicable
	if presence != nil && presence.RoomID != "" {
		roomUsersKey := r.roomUsersKey(presence.RoomID)
		r.client.SRem(ctx, roomUsersKey, userID)
	}

	// Remove presence data
	key := r.userPresenceKey(userID)
	if err := r.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to remove user presence: %w", err)
	}

	if r.metrics != nil {
		r.metrics.Counter("streaming.scaling.presence.removed").Inc()
	}

	return nil
}

// GetRoomUsers returns all users in a room
func (r *DefaultRedisScaler) GetRoomUsers(ctx context.Context, roomID string) ([]UserPresence, error) {
	roomUsersKey := r.roomUsersKey(roomID)
	userIDs, err := r.client.SMembers(ctx, roomUsersKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get room users: %w", err)
	}

	var users []UserPresence
	for _, userID := range userIDs {
		presence, err := r.GetUserPresence(ctx, userID)
		if err != nil {
			continue // Skip users with missing presence
		}
		if presence != nil {
			users = append(users, *presence)
		}
	}

	return users, nil
}

// Connection tracking

// RegisterConnection registers a connection
func (r *DefaultRedisScaler) RegisterConnection(ctx context.Context, connectionID string, connection ConnectionInfo) error {
	data, err := json.Marshal(connection)
	if err != nil {
		return fmt.Errorf("failed to marshal connection data: %w", err)
	}

	key := r.connectionKey(connectionID)
	if err := r.client.Set(ctx, key, data, r.config.ConnectionTTL).Err(); err != nil {
		return fmt.Errorf("failed to register connection: %w", err)
	}

	// Add to user connections set
	userConnectionsKey := r.userConnectionsKey(connection.UserID)
	if err := r.client.SAdd(ctx, userConnectionsKey, connectionID).Err(); err != nil {
		return fmt.Errorf("failed to add connection to user set: %w", err)
	}

	// Add to instance connections set
	instanceConnectionsKey := r.instanceConnectionsKey(connection.InstanceID)
	if err := r.client.SAdd(ctx, instanceConnectionsKey, connectionID).Err(); err != nil {
		return fmt.Errorf("failed to add connection to instance set: %w", err)
	}

	// Set TTLs
	r.client.Expire(ctx, userConnectionsKey, r.config.ConnectionTTL)
	r.client.Expire(ctx, instanceConnectionsKey, r.config.ConnectionTTL)

	if r.metrics != nil {
		r.metrics.Counter("streaming.scaling.connections.registered").Inc()
	}

	return nil
}

// UnregisterConnection unregisters a connection
func (r *DefaultRedisScaler) UnregisterConnection(ctx context.Context, connectionID string) error {
	// Get connection data first
	key := r.connectionKey(connectionID)
	data, err := r.client.Get(ctx, key).Result()
	if err != nil && err != redis.Nil {
		return fmt.Errorf("failed to get connection data: %w", err)
	}

	if err != redis.Nil {
		var connection ConnectionInfo
		if json.Unmarshal([]byte(data), &connection) == nil {
			// Remove from user connections set
			userConnectionsKey := r.userConnectionsKey(connection.UserID)
			r.client.SRem(ctx, userConnectionsKey, connectionID)

			// Remove from instance connections set
			instanceConnectionsKey := r.instanceConnectionsKey(connection.InstanceID)
			r.client.SRem(ctx, instanceConnectionsKey, connectionID)
		}
	}

	// Remove connection data
	if err := r.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to unregister connection: %w", err)
	}

	if r.metrics != nil {
		r.metrics.Counter("streaming.scaling.connections.unregistered").Inc()
	}

	return nil
}

// GetUserConnections returns all connections for a user
func (r *DefaultRedisScaler) GetUserConnections(ctx context.Context, userID string) ([]ConnectionInfo, error) {
	userConnectionsKey := r.userConnectionsKey(userID)
	connectionIDs, err := r.client.SMembers(ctx, userConnectionsKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get user connections: %w", err)
	}

	var connections []ConnectionInfo
	for _, connectionID := range connectionIDs {
		key := r.connectionKey(connectionID)
		data, err := r.client.Get(ctx, key).Result()
		if err != nil {
			continue // Skip missing connections
		}

		var connection ConnectionInfo
		if err := json.Unmarshal([]byte(data), &connection); err != nil {
			continue // Skip invalid connections
		}

		connections = append(connections, connection)
	}

	return connections, nil
}

// GetInstanceConnections returns all connections for an instance
func (r *DefaultRedisScaler) GetInstanceConnections(ctx context.Context, instanceID string) ([]ConnectionInfo, error) {
	instanceConnectionsKey := r.instanceConnectionsKey(instanceID)
	connectionIDs, err := r.client.SMembers(ctx, instanceConnectionsKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get instance connections: %w", err)
	}

	var connections []ConnectionInfo
	for _, connectionID := range connectionIDs {
		key := r.connectionKey(connectionID)
		data, err := r.client.Get(ctx, key).Result()
		if err != nil {
			continue // Skip missing connections
		}

		var connection ConnectionInfo
		if err := json.Unmarshal([]byte(data), &connection); err != nil {
			continue // Skip invalid connections
		}

		connections = append(connections, connection)
	}

	return connections, nil
}

// Health monitoring

// UpdateInstanceHealth updates instance health information
func (r *DefaultRedisScaler) UpdateInstanceHealth(ctx context.Context, instanceID string, health InstanceHealth) error {
	data, err := json.Marshal(health)
	if err != nil {
		return fmt.Errorf("failed to marshal health data: %w", err)
	}

	key := r.instanceHealthKey(instanceID)
	if err := r.client.Set(ctx, key, data, r.config.InstanceTTL).Err(); err != nil {
		return fmt.Errorf("failed to update instance health: %w", err)
	}

	return nil
}

// GetInstanceHealth gets instance health information
func (r *DefaultRedisScaler) GetInstanceHealth(ctx context.Context, instanceID string) (*InstanceHealth, error) {
	key := r.instanceHealthKey(instanceID)
	data, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // Health not found
		}
		return nil, fmt.Errorf("failed to get instance health: %w", err)
	}

	var health InstanceHealth
	if err := json.Unmarshal([]byte(data), &health); err != nil {
		return nil, fmt.Errorf("failed to unmarshal health data: %w", err)
	}

	return &health, nil
}

// CleanupStaleInstances removes instances that haven't been seen for a while
func (r *DefaultRedisScaler) CleanupStaleInstances(ctx context.Context, timeout time.Duration) error {
	instances, err := r.GetInstances(ctx)
	if err != nil {
		return fmt.Errorf("failed to get instances for cleanup: %w", err)
	}

	now := time.Now()
	var staleInstances []string

	for _, instance := range instances {
		if now.Sub(instance.LastSeen) > timeout {
			staleInstances = append(staleInstances, instance.ID)
		}
	}

	for _, instanceID := range staleInstances {
		if err := r.UnregisterInstance(ctx, instanceID); err != nil {
			if r.logger != nil {
				r.logger.Warn("failed to cleanup stale instance",
					logger.String("instance_id", instanceID),
					logger.Error(err),
				)
			}
		}
	}

	if len(staleInstances) > 0 && r.logger != nil {
		r.logger.Info("cleaned up stale instances",
			logger.Int("count", len(staleInstances)),
			logger.String("instances", fmt.Sprintf("%v", staleInstances)),
		)
	}

	return nil
}

// GetScalingStats returns scaling statistics
func (r *DefaultRedisScaler) GetScalingStats(ctx context.Context) (*ScalingStats, error) {
	instances, err := r.GetInstances(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get instances: %w", err)
	}

	stats := &ScalingStats{
		InstanceDistribution: make(map[string]int),
		RegionDistribution:   make(map[string]int),
	}

	healthyInstances := 0
	totalRooms := 0
	totalConnections := 0

	for _, instance := range instances {
		stats.TotalInstances++

		// Count healthy instances
		health, _ := r.GetInstanceHealth(ctx, instance.ID)
		if health != nil && health.Status == "healthy" {
			healthyInstances++
		}

		// Count rooms and connections
		rooms, _ := r.GetInstanceRooms(ctx, instance.ID)
		connections, _ := r.GetInstanceConnections(ctx, instance.ID)

		totalRooms += len(rooms)
		totalConnections += len(connections)

		// Distribution by instance
		stats.InstanceDistribution[instance.ID] = len(connections)

		// Distribution by region
		if instance.Metadata.Region != "" {
			stats.RegionDistribution[instance.Metadata.Region]++
		}
	}

	stats.HealthyInstances = healthyInstances
	stats.TotalRooms = totalRooms
	stats.TotalConnections = totalConnections

	// TODO: Calculate message rate and user count
	// These would require additional tracking

	return stats, nil
}

// Background processes

// startHeartbeat starts the heartbeat process
func (r *DefaultRedisScaler) startHeartbeat() {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()

		ticker := time.NewTicker(r.config.HeartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				r.sendHeartbeat()
			case <-r.heartbeatStop:
				return
			}
		}
	}()
}

// sendHeartbeat sends a heartbeat to maintain instance registration
func (r *DefaultRedisScaler) sendHeartbeat() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Update instance last seen time
	key := r.instanceKey(r.instanceID)
	instance, err := r.GetInstance(ctx, r.instanceID)
	if err != nil || instance == nil {
		return
	}

	instance.LastSeen = time.Now()
	data, err := json.Marshal(instance)
	if err != nil {
		return
	}

	r.client.Set(ctx, key, data, r.config.InstanceTTL)

	if r.metrics != nil {
		r.metrics.Counter("streaming.scaling.heartbeats.sent").Inc()
	}
}

// startCleanup starts the cleanup process
func (r *DefaultRedisScaler) startCleanup() {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()

		ticker := time.NewTicker(r.config.InstanceTTL / 2)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				r.CleanupStaleInstances(ctx, r.config.InstanceTTL*2)
				cancel()
			case <-r.cleanupStop:
				return
			}
		}
	}()
}

// processSubscriptions processes incoming messages from subscriptions
func (r *DefaultRedisScaler) processSubscriptions() {
	defer r.wg.Done()

	for msg := range r.pubsub.Channel() {
		// Extract room ID from channel name
		roomID := r.extractRoomIDFromChannel(msg.Channel)
		if roomID == "" {
			continue
		}

		// Get handler for this room
		r.subMu.RLock()
		handler := r.subscriptions[roomID]
		r.subMu.RUnlock()

		if handler == nil {
			continue
		}

		// Unmarshal message
		var message streaming.Message
		if err := json.Unmarshal([]byte(msg.Payload), &message); err != nil {
			if r.logger != nil {
				r.logger.Warn("failed to unmarshal distributed message",
					logger.String("room_id", roomID),
					logger.Error(err),
				)
			}
			continue
		}

		// Handle message
		if err := handler(roomID, &message); err != nil {
			if r.logger != nil {
				r.logger.Error("failed to handle distributed message",
					logger.String("room_id", roomID),
					logger.Error(err),
				)
			}
		}

		if r.metrics != nil {
			r.metrics.Counter("streaming.scaling.messages.received").Inc()
		}
	}
}

// cleanupInstanceData cleans up data associated with an instance
func (r *DefaultRedisScaler) cleanupInstanceData(ctx context.Context, instanceID string) error {
	// Get and cleanup instance rooms
	rooms, err := r.GetInstanceRooms(ctx, instanceID)
	if err == nil {
		for _, roomID := range rooms {
			r.UnregisterRoom(ctx, roomID, instanceID)
		}
	}

	// Get and cleanup instance connections
	connections, err := r.GetInstanceConnections(ctx, instanceID)
	if err == nil {
		for _, connection := range connections {
			r.UnregisterConnection(ctx, connection.ID)
		}
	}

	// Remove instance-specific keys
	keys := []string{
		r.instanceRoomsKey(instanceID),
		r.instanceConnectionsKey(instanceID),
		r.instanceHealthKey(instanceID),
	}

	for _, key := range keys {
		r.client.Del(ctx, key)
	}

	return nil
}

// Key generation methods

func (r *DefaultRedisScaler) key(suffix string) string {
	return fmt.Sprintf("%s:%s", r.config.KeyPrefix, suffix)
}

func (r *DefaultRedisScaler) instanceKey(instanceID string) string {
	return r.key(fmt.Sprintf("instance:%s", instanceID))
}

func (r *DefaultRedisScaler) instanceHealthKey(instanceID string) string {
	return r.key(fmt.Sprintf("instance:%s:health", instanceID))
}

func (r *DefaultRedisScaler) instanceRoomsKey(instanceID string) string {
	return r.key(fmt.Sprintf("instance:%s:rooms", instanceID))
}

func (r *DefaultRedisScaler) instanceConnectionsKey(instanceID string) string {
	return r.key(fmt.Sprintf("instance:%s:connections", instanceID))
}

func (r *DefaultRedisScaler) roomChannelKey(roomID string) string {
	return r.key(fmt.Sprintf("room:%s:messages", roomID))
}

func (r *DefaultRedisScaler) roomInstancesKey(roomID string) string {
	return r.key(fmt.Sprintf("room:%s:instances", roomID))
}

func (r *DefaultRedisScaler) roomUsersKey(roomID string) string {
	return r.key(fmt.Sprintf("room:%s:users", roomID))
}

func (r *DefaultRedisScaler) userPresenceKey(userID string) string {
	return r.key(fmt.Sprintf("user:%s:presence", userID))
}

func (r *DefaultRedisScaler) userConnectionsKey(userID string) string {
	return r.key(fmt.Sprintf("user:%s:connections", userID))
}

func (r *DefaultRedisScaler) connectionKey(connectionID string) string {
	return r.key(fmt.Sprintf("connection:%s", connectionID))
}

func (r *DefaultRedisScaler) extractRoomIDFromChannel(channel string) string {
	prefix := r.key("room:")
	suffix := ":messages"

	if !strings.HasPrefix(channel, prefix) || !strings.HasSuffix(channel, suffix) {
		return ""
	}

	roomID := strings.TrimPrefix(channel, prefix)
	roomID = strings.TrimSuffix(roomID, suffix)

	return roomID
}

// Close closes the Redis scaler
func (r *DefaultRedisScaler) Close() error {
	// OnStop background processes
	close(r.heartbeatStop)
	close(r.cleanupStop)

	// Close pubsub if exists
	if r.pubsub != nil {
		r.pubsub.Close()
	}

	// Wait for all goroutines to finish
	r.wg.Wait()

	// Close Redis client
	return r.client.Close()
}
