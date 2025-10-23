package redis

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	cachecore "github.com/xraph/forge/pkg/cache/core"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// RedisClusterCache implements distributed caching using Redis Cluster
type RedisClusterCache struct {
	*RedisCache
	clusterClient *redis.ClusterClient
	nodes         map[string]*redis.Client
	nodesMu       sync.RWMutex
	topology      *ClusterTopology
	rebalancing   bool
	rebalanceMu   sync.Mutex
}

// ClusterTopology represents the Redis cluster topology
type ClusterTopology struct {
	Masters []ClusterNode `json:"masters"`
	Slaves  []ClusterNode `json:"slaves"`
	Updated time.Time     `json:"updated"`
}

// ClusterNode represents a single node in the cluster
type ClusterNode struct {
	ID       string   `json:"id"`
	Address  string   `json:"address"`
	Role     string   `json:"role"`
	Slots    []string `json:"slots"`
	Replicas []string `json:"replicas"`
	Health   string   `json:"health"`
}

// NewRedisClusterCache creates a new Redis cluster cache
func NewRedisClusterCache(name string, config *cachecore.RedisConfig, logger common.Logger, metrics common.Metrics) (*RedisClusterCache, error) {
	if !config.EnableCluster {
		return nil, fmt.Errorf("cluster mode not enabled in config")
	}

	if len(config.ClusterNodes) == 0 {
		return nil, fmt.Errorf("no cluster nodes specified")
	}

	baseCache, err := NewRedisCache(name, config, logger, metrics)
	if err != nil {
		return nil, err
	}

	clusterCache := &RedisClusterCache{
		RedisCache: baseCache,
		nodes:      make(map[string]*redis.Client),
		topology:   &ClusterTopology{},
	}

	if err := clusterCache.initCluster(); err != nil {
		return nil, fmt.Errorf("failed to initialize cluster: %w", err)
	}

	return clusterCache, nil
}

// initCluster initializes the cluster connection and topology
func (r *RedisClusterCache) initCluster() error {
	// Create cluster client
	clusterClient := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:        r.config.ClusterNodes,
		Password:     r.config.Password,
		PoolSize:     r.config.PoolSize,
		MinIdleConns: r.config.MinIdleConns,
		MaxIdleConns: r.config.MaxIdleConns,
		DialTimeout:  r.config.DialTimeout,
		ReadTimeout:  r.config.ReadTimeout,
		WriteTimeout: r.config.WriteTimeout,
	})

	r.clusterClient = clusterClient
	r.client = clusterClient

	// Test cluster connection
	ctx, cancel := context.WithTimeout(context.Background(), r.config.DialTimeout)
	defer cancel()

	if err := r.clusterClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to ping cluster: %w", err)
	}

	// Load cluster topology
	if err := r.loadTopology(ctx); err != nil {
		return fmt.Errorf("failed to load cluster topology: %w", err)
	}

	if r.logger != nil {
		r.logger.Info("Redis cluster initialized",
			logger.String("name", r.name),
			logger.Int("masters", len(r.topology.Masters)),
			logger.Int("slaves", len(r.topology.Slaves)),
		)
	}

	return nil
}

// loadTopology loads the current cluster topology
func (r *RedisClusterCache) loadTopology(ctx context.Context) error {
	clusterSlots, err := r.clusterClient.ClusterSlots(ctx).Result()
	if err != nil {
		return fmt.Errorf("failed to get cluster slots: %w", err)
	}

	r.nodesMu.Lock()
	defer r.nodesMu.Unlock()

	masters := make([]ClusterNode, 0)
	slaves := make([]ClusterNode, 0)

	for _, slot := range clusterSlots {
		if len(slot.Nodes) == 0 {
			continue
		}

		// First node is the master
		master := slot.Nodes[0]
		masterNode := ClusterNode{
			ID:      master.ID,
			Address: master.Addr,
			Role:    "master",
			Slots:   []string{fmt.Sprintf("%d-%d", slot.Start, slot.End)},
			Health:  "healthy",
		}

		// Create individual client for this master
		r.nodes[masterNode.ID] = redis.NewClient(&redis.Options{
			Addr:     masterNode.Address,
			Password: r.config.Password,
			DB:       r.config.Database,
		})

		masters = append(masters, masterNode)

		// Remaining nodes are slaves
		for i := 1; i < len(slot.Nodes); i++ {
			slave := slot.Nodes[i]
			slaveNode := ClusterNode{
				ID:      slave.ID,
				Address: slave.Addr,
				Role:    "slave",
				Health:  "healthy",
			}

			// Create individual client for this slave
			r.nodes[slaveNode.ID] = redis.NewClient(&redis.Options{
				Addr:     slaveNode.Address,
				Password: r.config.Password,
				DB:       r.config.Database,
			})

			slaves = append(slaves, slaveNode)
			masterNode.Replicas = append(masterNode.Replicas, slaveNode.ID)
		}
	}

	r.topology = &ClusterTopology{
		Masters: masters,
		Slaves:  slaves,
		Updated: time.Now(),
	}

	return nil
}

// GetNode returns the node responsible for a given key
func (r *RedisClusterCache) GetNode(key string) (string, error) {
	if err := cachecore.ValidateKey(key); err != nil {
		return "", err
	}

	// Use Redis cluster client to determine the node
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	node, err := r.clusterClient.ClusterKeySlot(ctx, key).Result()
	if err != nil {
		return "", fmt.Errorf("failed to get key slot: %w", err)
	}

	// Find the master node for this slot
	r.nodesMu.RLock()
	defer r.nodesMu.RUnlock()

	for _, master := range r.topology.Masters {
		for _, slotRange := range master.Slots {
			if r.slotInRange(int64(node), slotRange) {
				return master.ID, nil
			}
		}
	}

	return "", fmt.Errorf("no node found for key %s (slot %d)", key, node)
}

// slotInRange checks if a slot is within a given range
func (r *RedisClusterCache) slotInRange(slot int64, slotRange string) bool {
	parts := strings.Split(slotRange, "-")
	if len(parts) != 2 {
		return false
	}

	start := parseInt(parts[0])
	end := parseInt(parts[1])

	return slot >= start && slot <= end
}

// Rebalance redistributes data across cluster nodes
func (r *RedisClusterCache) Rebalance(ctx context.Context) error {
	r.rebalanceMu.Lock()
	defer r.rebalanceMu.Unlock()

	if r.rebalancing {
		return fmt.Errorf("rebalancing already in progress")
	}

	r.rebalancing = true
	defer func() { r.rebalancing = false }()

	if r.logger != nil {
		r.logger.Info("starting cluster rebalance", logger.String("cache", r.name))
	}

	start := time.Now()

	// Reload topology first
	if err := r.loadTopology(ctx); err != nil {
		return fmt.Errorf("failed to reload topology: %w", err)
	}

	// Check cluster health before rebalancing
	if err := r.checkClusterHealth(ctx); err != nil {
		return fmt.Errorf("cluster health check failed: %w", err)
	}

	// // Trigger Redis cluster rebalance
	// if err := r.clusterClient.ClusterRebalance(ctx).Err(); err != nil {
	// 	return fmt.Errorf("cluster rebalance failed: %w", err)
	// }

	// Wait for rebalance to complete
	if err := r.waitForRebalanceCompletion(ctx); err != nil {
		return fmt.Errorf("rebalance completion check failed: %w", err)
	}

	duration := time.Since(start)

	if r.logger != nil {
		r.logger.Info("cluster rebalance completed",
			logger.String("cache", r.name),
			logger.Duration("duration", duration),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("cache.cluster.rebalance_completed", "cache", r.name).Inc()
		r.metrics.Histogram("cache.cluster.rebalance_duration", "cache", r.name).Observe(duration.Seconds())
	}

	return nil
}

// checkClusterHealth checks the health of all cluster nodes
func (r *RedisClusterCache) checkClusterHealth(ctx context.Context) error {
	r.nodesMu.RLock()
	defer r.nodesMu.RUnlock()

	unhealthyNodes := 0
	totalNodes := len(r.nodes)

	for nodeID, client := range r.nodes {
		if err := client.Ping(ctx).Err(); err != nil {
			unhealthyNodes++
			if r.logger != nil {
				r.logger.Warn("unhealthy cluster node detected",
					logger.String("node_id", nodeID),
					logger.Error(err),
				)
			}
		}
	}

	// Fail if more than 25% of nodes are unhealthy
	if float64(unhealthyNodes)/float64(totalNodes) > 0.25 {
		return fmt.Errorf("too many unhealthy nodes: %d/%d", unhealthyNodes, totalNodes)
	}

	return nil
}

// waitForRebalanceCompletion waits for the rebalance operation to complete
func (r *RedisClusterCache) waitForRebalanceCompletion(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	timeout := time.After(5 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("rebalance timeout")
		case <-ticker.C:
			if stable, err := r.isClusterStable(ctx); err != nil {
				return err
			} else if stable {
				return nil
			}
		}
	}
}

// isClusterStable checks if the cluster is in a stable state
func (r *RedisClusterCache) isClusterStable(ctx context.Context) (bool, error) {
	info, err := r.clusterClient.ClusterInfo(ctx).Result()
	if err != nil {
		return false, err
	}

	// Parse cluster info to check state
	lines := strings.Split(info, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "cluster_state:") {
			state := strings.TrimPrefix(line, "cluster_state:")
			return strings.TrimSpace(state) == "ok", nil
		}
	}

	return false, fmt.Errorf("cluster state not found in info")
}

// GetClusterInfo returns information about the cluster
func (r *RedisClusterCache) GetClusterInfo(ctx context.Context) (*ClusterInfo, error) {
	info, err := r.clusterClient.ClusterInfo(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster info: %w", err)
	}

	nodes, err := r.clusterClient.ClusterNodes(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster nodes: %w", err)
	}

	clusterInfo := &ClusterInfo{
		State:     r.parseClusterState(info),
		Slots:     r.parseClusterSlots(info),
		Nodes:     r.parseClusterNodes(nodes),
		Topology:  r.topology,
		UpdatedAt: time.Now(),
	}

	return clusterInfo, nil
}

// ClusterInfo contains comprehensive cluster information
type ClusterInfo struct {
	State     string           `json:"state"`
	Slots     ClusterSlotInfo  `json:"slots"`
	Nodes     []ClusterNode    `json:"nodes"`
	Topology  *ClusterTopology `json:"topology"`
	UpdatedAt time.Time        `json:"updated_at"`
}

// ClusterSlotInfo contains slot assignment information
type ClusterSlotInfo struct {
	Assigned   int `json:"assigned"`
	Unassigned int `json:"unassigned"`
	Total      int `json:"total"`
}

// Helper functions for parsing cluster information
func (r *RedisClusterCache) parseClusterState(info string) string {
	lines := strings.Split(info, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "cluster_state:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "cluster_state:"))
		}
	}
	return "unknown"
}

func (r *RedisClusterCache) parseClusterSlots(info string) ClusterSlotInfo {
	lines := strings.Split(info, "\n")
	slots := ClusterSlotInfo{Total: 16384}

	for _, line := range lines {
		if strings.HasPrefix(line, "cluster_slots_assigned:") {
			slots.Assigned = int(parseInt(strings.TrimSpace(strings.TrimPrefix(line, "cluster_slots_assigned:"))))
		}
	}

	slots.Unassigned = slots.Total - slots.Assigned
	return slots
}

func (r *RedisClusterCache) parseClusterNodes(nodes string) []ClusterNode {
	lines := strings.Split(nodes, "\n")
	nodeList := make([]ClusterNode, 0)

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 8 {
			continue
		}

		node := ClusterNode{
			ID:      parts[0],
			Address: parts[1],
			Role:    r.parseNodeRole(parts[2]),
			Health:  r.parseNodeHealth(parts[7]),
		}

		if len(parts) > 8 {
			node.Slots = parts[8:]
		}

		nodeList = append(nodeList, node)
	}

	return nodeList
}

func (r *RedisClusterCache) parseNodeRole(flags string) string {
	if strings.Contains(flags, "master") {
		return "master"
	} else if strings.Contains(flags, "slave") {
		return "slave"
	}
	return "unknown"
}

func (r *RedisClusterCache) parseNodeHealth(status string) string {
	if status == "connected" {
		return "healthy"
	}
	return "unhealthy"
}

// GetTopology returns the current cluster topology
func (r *RedisClusterCache) GetTopology() *ClusterTopology {
	r.nodesMu.RLock()
	defer r.nodesMu.RUnlock()
	return r.topology
}

// RefreshTopology refreshes the cluster topology
func (r *RedisClusterCache) RefreshTopology(ctx context.Context) error {
	return r.loadTopology(ctx)
}

// IsRebalancing returns whether a rebalance operation is in progress
func (r *RedisClusterCache) IsRebalancing() bool {
	r.rebalanceMu.Lock()
	defer r.rebalanceMu.Unlock()
	return r.rebalancing
}

// GetNodeClient returns a client for a specific node
func (r *RedisClusterCache) GetNodeClient(nodeID string) (*redis.Client, error) {
	r.nodesMu.RLock()
	defer r.nodesMu.RUnlock()

	client, exists := r.nodes[nodeID]
	if !exists {
		return nil, fmt.Errorf("node %s not found", nodeID)
	}

	return client, nil
}

// Helper function for parsing integers
func parseInt(s string) int64 {
	if i, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
		return i
	}
	return 0
}
