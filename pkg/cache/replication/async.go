package replication

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// AsyncReplication implements asynchronous cache replication
type AsyncReplication struct {
	nodes            map[string]*AsyncReplicaNode
	replicationSets  map[string]*AsyncReplicationSet
	replicationQueue chan *AsyncOperation
	config           AsyncReplicationConfig
	logger           common.Logger
	metrics          common.Metrics
	mu               sync.RWMutex
	stats            AsyncReplicationStats
	started          bool
	workers          []*AsyncWorker
	stopChan         chan struct{}
}

// AsyncReplicationConfig contains configuration for asynchronous replication
type AsyncReplicationConfig struct {
	ReplicationFactor   int           `yaml:"replication_factor" json:"replication_factor"`
	WorkerCount         int           `yaml:"worker_count" json:"worker_count"`
	QueueSize           int           `yaml:"queue_size" json:"queue_size"`
	BatchSize           int           `yaml:"batch_size" json:"batch_size"`
	FlushInterval       time.Duration `yaml:"flush_interval" json:"flush_interval"`
	MaxRetries          int           `yaml:"max_retries" json:"max_retries"`
	RetryDelay          time.Duration `yaml:"retry_delay" json:"retry_delay"`
	ReplicationDelay    time.Duration `yaml:"replication_delay" json:"replication_delay"`
	EnableBatching      bool          `yaml:"enable_batching" json:"enable_batching"`
	EnableCompression   bool          `yaml:"enable_compression" json:"enable_compression"`
	EnablePriority      bool          `yaml:"enable_priority" json:"enable_priority"`
	ConflictResolution  string        `yaml:"conflict_resolution" json:"conflict_resolution"`
	ConsistencyModel    string        `yaml:"consistency_model" json:"consistency_model"` // eventual, causal
	LagThreshold        time.Duration `yaml:"lag_threshold" json:"lag_threshold"`
	HealthCheckInterval time.Duration `yaml:"health_check_interval" json:"health_check_interval"`
}

// AsyncReplicationStats contains statistics for asynchronous replication
type AsyncReplicationStats struct {
	TotalOperations       int64                      `json:"total_operations"`
	QueuedOperations      int64                      `json:"queued_operations"`
	ProcessedOperations   int64                      `json:"processed_operations"`
	FailedOperations      int64                      `json:"failed_operations"`
	RetriedOperations     int64                      `json:"retried_operations"`
	BatchedOperations     int64                      `json:"batched_operations"`
	AverageReplicationLag time.Duration              `json:"average_replication_lag"`
	MaxReplicationLag     time.Duration              `json:"max_replication_lag"`
	QueueUtilization      float64                    `json:"queue_utilization"`
	WorkerUtilization     float64                    `json:"worker_utilization"`
	ThroughputOpsPerSec   float64                    `json:"throughput_ops_per_sec"`
	NodeStats             map[string]*AsyncNodeStats `json:"node_stats"`
	SetStats              map[string]*AsyncSetStats  `json:"set_stats"`
	WorkerStats           []*WorkerStats             `json:"worker_stats"`
	PerformanceMetrics    *AsyncPerformanceMetrics   `json:"performance_metrics"`
}

// AsyncReplicaNode represents a node in the async replication system
type AsyncReplicaNode struct {
	ID                  string                 `json:"id"`
	Address             string                 `json:"address"`
	Status              NodeStatus             `json:"status"`
	Weight              float64                `json:"weight"`
	Priority            int                    `json:"priority"`
	LastSeen            time.Time              `json:"last_seen"`
	ReplicationLag      time.Duration          `json:"replication_lag"`
	PendingOperations   int64                  `json:"pending_operations"`
	ProcessedOperations int64                  `json:"processed_operations"`
	FailedOperations    int64                  `json:"failed_operations"`
	RetryOperations     int64                  `json:"retry_operations"`
	DataVersion         int64                  `json:"data_version"`
	LastSync            time.Time              `json:"last_sync"`
	Capacity            int64                  `json:"capacity"`
	Used                int64                  `json:"used"`
	ErrorRate           float64                `json:"error_rate"`
	Metadata            map[string]interface{} `json:"metadata"`
	Tags                []string               `json:"tags"`
}

// AsyncReplicationSet represents a set of replica nodes for async replication
type AsyncReplicationSet struct {
	ID                    string        `json:"id"`
	PrimaryNode           string        `json:"primary_node"`
	ReplicaNodes          []string      `json:"replica_nodes"`
	Status                SetStatus     `json:"status"`
	Created               time.Time     `json:"created"`
	LastSync              time.Time     `json:"last_sync"`
	SyncCount             int64         `json:"sync_count"`
	ConflictCount         int64         `json:"conflict_count"`
	AverageReplicationLag time.Duration `json:"average_replication_lag"`
	MaxReplicationLag     time.Duration `json:"max_replication_lag"`
	PendingOperations     int64         `json:"pending_operations"`
	ReplicationMode       string        `json:"replication_mode"` // async, eventually_consistent
	ConsistencyLevel      float64       `json:"consistency_level"`
}

// AsyncOperation represents an asynchronous replication operation
type AsyncOperation struct {
	ID             string                 `json:"id"`
	Type           OperationType          `json:"type"`
	SetID          string                 `json:"set_id"`
	Key            string                 `json:"key"`
	Value          interface{}            `json:"value"`
	TTL            time.Duration          `json:"ttl"`
	Priority       OperationPriority      `json:"priority"`
	Timestamp      time.Time              `json:"timestamp"`
	Version        int64                  `json:"version"`
	Checksum       string                 `json:"checksum"`
	Metadata       map[string]interface{} `json:"metadata"`
	TargetNodes    []string               `json:"target_nodes"`
	CompletedNodes []string               `json:"completed_nodes"`
	FailedNodes    []string               `json:"failed_nodes"`
	Status         OperationStatus        `json:"status"`
	Retries        int                    `json:"retries"`
	MaxRetries     int                    `json:"max_retries"`
	CreatedAt      time.Time              `json:"created_at"`
	ScheduledAt    time.Time              `json:"scheduled_at"`
	StartedAt      time.Time              `json:"started_at"`
	CompletedAt    time.Time              `json:"completed_at"`
	ReplicationLag time.Duration          `json:"replication_lag"`
}

// OperationPriority represents the priority of an operation
type OperationPriority int

const (
	PriorityLow      OperationPriority = 1
	PriorityNormal   OperationPriority = 5
	PriorityHigh     OperationPriority = 10
	PriorityCritical OperationPriority = 15
)

// AsyncWorker represents a worker that processes async operations
type AsyncWorker struct {
	ID          int
	replication *AsyncReplication
	queue       chan *AsyncOperation
	batchQueue  []*AsyncOperation
	stopChan    chan struct{}
	stats       *WorkerStats
	mu          sync.Mutex
}

// WorkerStats contains statistics for a worker
type WorkerStats struct {
	WorkerID            int           `json:"worker_id"`
	ProcessedOperations int64         `json:"processed_operations"`
	FailedOperations    int64         `json:"failed_operations"`
	BatchedOperations   int64         `json:"batched_operations"`
	AverageLatency      time.Duration `json:"average_latency"`
	LastActivity        time.Time     `json:"last_activity"`
	Status              string        `json:"status"`
	QueueSize           int           `json:"queue_size"`
	Utilization         float64       `json:"utilization"`
}

// AsyncNodeStats contains detailed statistics for an async node
type AsyncNodeStats struct {
	NodeID                string        `json:"node_id"`
	Status                string        `json:"status"`
	PendingOperations     int64         `json:"pending_operations"`
	ProcessedOperations   int64         `json:"processed_operations"`
	FailedOperations      int64         `json:"failed_operations"`
	AverageReplicationLag time.Duration `json:"average_replication_lag"`
	LastSync              time.Time     `json:"last_sync"`
	ErrorRate             float64       `json:"error_rate"`
	ThroughputOpsPerSec   float64       `json:"throughput_ops_per_sec"`
}

// AsyncSetStats contains detailed statistics for an async replication set
type AsyncSetStats struct {
	SetID                 string        `json:"set_id"`
	Status                string        `json:"status"`
	PendingOperations     int64         `json:"pending_operations"`
	AverageReplicationLag time.Duration `json:"average_replication_lag"`
	MaxReplicationLag     time.Duration `json:"max_replication_lag"`
	SyncCount             int64         `json:"sync_count"`
	ConflictCount         int64         `json:"conflict_count"`
	LastSync              time.Time     `json:"last_sync"`
	ConsistencyLevel      float64       `json:"consistency_level"`
}

// AsyncPerformanceMetrics contains performance metrics for async replication
type AsyncPerformanceMetrics struct {
	AverageLatency        time.Duration `json:"average_latency"`
	P50Latency            time.Duration `json:"p50_latency"`
	P95Latency            time.Duration `json:"p95_latency"`
	P99Latency            time.Duration `json:"p99_latency"`
	ThroughputOpsPerSec   float64       `json:"throughput_ops_per_sec"`
	QueueUtilization      float64       `json:"queue_utilization"`
	WorkerUtilization     float64       `json:"worker_utilization"`
	ErrorRate             float64       `json:"error_rate"`
	ReplicationEfficiency float64       `json:"replication_efficiency"`
}

// NewAsyncReplication creates a new asynchronous replication instance
func NewAsyncReplication(config AsyncReplicationConfig, logger common.Logger, metrics common.Metrics) *AsyncReplication {
	queueSize := config.QueueSize
	if queueSize <= 0 {
		queueSize = 10000
	}

	return &AsyncReplication{
		nodes:            make(map[string]*AsyncReplicaNode),
		replicationSets:  make(map[string]*AsyncReplicationSet),
		replicationQueue: make(chan *AsyncOperation, queueSize),
		config:           config,
		logger:           logger,
		metrics:          metrics,
		stats: AsyncReplicationStats{
			NodeStats:          make(map[string]*AsyncNodeStats),
			SetStats:           make(map[string]*AsyncSetStats),
			PerformanceMetrics: &AsyncPerformanceMetrics{},
		},
		stopChan: make(chan struct{}),
	}
}

// Start starts the asynchronous replication
func (ar *AsyncReplication) Start(ctx context.Context) error {
	ar.mu.Lock()
	defer ar.mu.Unlock()

	if ar.started {
		return fmt.Errorf("async replication already started")
	}

	// Validate configuration
	if err := ar.validateConfig(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// OnStart workers
	workerCount := ar.config.WorkerCount
	if workerCount <= 0 {
		workerCount = 5
	}

	ar.workers = make([]*AsyncWorker, workerCount)
	for i := 0; i < workerCount; i++ {
		worker := &AsyncWorker{
			ID:          i,
			replication: ar,
			queue:       make(chan *AsyncOperation, 100),
			batchQueue:  make([]*AsyncOperation, 0, ar.config.BatchSize),
			stopChan:    make(chan struct{}),
			stats: &WorkerStats{
				WorkerID:     i,
				Status:       "active",
				LastActivity: time.Now(),
			},
		}
		ar.workers[i] = worker
		ar.stats.WorkerStats = append(ar.stats.WorkerStats, worker.stats)
		go worker.start(ctx)
	}

	// OnStart operation distributor
	go ar.distributeOperations(ctx)

	// OnStart health checker
	if ar.config.HealthCheckInterval > 0 {
		go ar.healthCheckLoop(ctx)
	}

	// OnStart metrics collector
	go ar.metricsLoop(ctx)

	ar.started = true

	ar.logger.Info("asynchronous replication started",
		logger.Int("worker_count", workerCount),
		logger.Int("queue_size", ar.config.QueueSize),
		logger.Bool("batching_enabled", ar.config.EnableBatching),
	)

	if ar.metrics != nil {
		ar.metrics.Counter("forge.cache.replication.async.started").Inc()
	}

	return nil
}

// Stop stops the asynchronous replication
func (ar *AsyncReplication) Stop(ctx context.Context) error {
	ar.mu.Lock()
	defer ar.mu.Unlock()

	if !ar.started {
		return fmt.Errorf("async replication not started")
	}

	// Signal shutdown
	close(ar.stopChan)

	// OnStop all workers
	for _, worker := range ar.workers {
		worker.stop()
	}

	// Wait for workers to finish processing
	time.Sleep(time.Second)

	ar.started = false

	ar.logger.Info("asynchronous replication stopped")

	if ar.metrics != nil {
		ar.metrics.Counter("forge.cache.replication.async.stopped").Inc()
	}

	return nil
}

// AddNode adds a replica node
func (ar *AsyncReplication) AddNode(node *AsyncReplicaNode) error {
	ar.mu.Lock()
	defer ar.mu.Unlock()

	if _, exists := ar.nodes[node.ID]; exists {
		return fmt.Errorf("node %s already exists", node.ID)
	}

	node.Status = NodeStatusOnline
	node.LastSeen = time.Now()
	node.LastSync = time.Now()
	ar.nodes[node.ID] = node
	ar.stats.NodeStats[node.ID] = &AsyncNodeStats{
		NodeID:              node.ID,
		Status:              string(node.Status),
		PendingOperations:   0,
		ProcessedOperations: 0,
		FailedOperations:    0,
		LastSync:            time.Now(),
	}

	ar.logger.Info("async replica node added",
		logger.String("node_id", node.ID),
		logger.String("address", node.Address),
		logger.Int("priority", node.Priority),
	)

	if ar.metrics != nil {
		ar.metrics.Counter("forge.cache.replication.async.nodes.added").Inc()
		ar.metrics.Gauge("forge.cache.replication.async.nodes.total").Set(float64(len(ar.nodes)))
	}

	return nil
}

// QueueOperation queues an operation for asynchronous replication
func (ar *AsyncReplication) QueueOperation(setID, key string, value interface{}, ttl time.Duration, priority OperationPriority) error {
	if !ar.started {
		return fmt.Errorf("async replication not started")
	}

	operation := &AsyncOperation{
		ID:         ar.generateOperationID(),
		Type:       OperationTypeSet,
		SetID:      setID,
		Key:        key,
		Value:      value,
		TTL:        ttl,
		Priority:   priority,
		Timestamp:  time.Now(),
		Version:    ar.getNextVersion(),
		Status:     OperationStatusPending,
		MaxRetries: ar.config.MaxRetries,
		CreatedAt:  time.Now(),
	}

	// Add replication delay if configured
	if ar.config.ReplicationDelay > 0 {
		operation.ScheduledAt = time.Now().Add(ar.config.ReplicationDelay)
	} else {
		operation.ScheduledAt = time.Now()
	}

	// Get target nodes for this operation
	if set, exists := ar.replicationSets[setID]; exists {
		operation.TargetNodes = append([]string{set.PrimaryNode}, set.ReplicaNodes...)
	} else {
		return fmt.Errorf("replication set %s not found", setID)
	}

	// Queue the operation
	select {
	case ar.replicationQueue <- operation:
		ar.stats.QueuedOperations++
		ar.stats.TotalOperations++

		ar.logger.Debug("operation queued for async replication",
			logger.String("operation_id", operation.ID),
			logger.String("set_id", setID),
			logger.String("key", key),
			logger.Int("priority", int(priority)),
		)

		if ar.metrics != nil {
			ar.metrics.Counter("forge.cache.replication.async.operations.queued").Inc()
			ar.metrics.Gauge("forge.cache.replication.async.queue.size").Set(float64(len(ar.replicationQueue)))
		}

		return nil
	default:
		ar.stats.FailedOperations++
		return fmt.Errorf("replication queue is full")
	}
}

// GetStats returns asynchronous replication statistics
func (ar *AsyncReplication) GetStats() AsyncReplicationStats {
	ar.mu.RLock()
	defer ar.mu.RUnlock()

	// Update queue utilization
	ar.stats.QueueUtilization = float64(len(ar.replicationQueue)) / float64(cap(ar.replicationQueue))

	// Update worker utilization
	var totalUtilization float64
	for _, worker := range ar.workers {
		worker.mu.Lock()
		totalUtilization += worker.stats.Utilization
		worker.mu.Unlock()
	}
	if len(ar.workers) > 0 {
		ar.stats.WorkerUtilization = totalUtilization / float64(len(ar.workers))
	}

	return ar.stats
}

// HealthCheck performs health check on async replication system
func (ar *AsyncReplication) HealthCheck(ctx context.Context) error {
	ar.mu.RLock()
	defer ar.mu.RUnlock()

	if !ar.started {
		return fmt.Errorf("async replication not started")
	}

	// Check queue utilization
	if ar.stats.QueueUtilization > 0.9 {
		return fmt.Errorf("replication queue utilization too high: %.2f", ar.stats.QueueUtilization)
	}

	// Check replication lag
	if ar.stats.MaxReplicationLag > ar.config.LagThreshold {
		return fmt.Errorf("replication lag too high: %v", ar.stats.MaxReplicationLag)
	}

	// Check worker health
	activeWorkers := 0
	for _, worker := range ar.workers {
		if worker.stats.Status == "active" {
			activeWorkers++
		}
	}

	if activeWorkers < len(ar.workers)/2 {
		return fmt.Errorf("too many workers inactive: %d/%d", activeWorkers, len(ar.workers))
	}

	return nil
}

// Worker implementation

func (w *AsyncWorker) start(ctx context.Context) {
	w.stats.Status = "active"
	ticker := time.NewTicker(w.replication.config.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopChan:
			w.stats.Status = "stopped"
			return
		case operation := <-w.queue:
			w.processOperation(ctx, operation)
		case <-ticker.C:
			if w.replication.config.EnableBatching {
				w.processBatch(ctx)
			}
		}
	}
}

func (w *AsyncWorker) stop() {
	close(w.stopChan)
}

func (w *AsyncWorker) processOperation(ctx context.Context, operation *AsyncOperation) {
	w.mu.Lock()
	w.stats.LastActivity = time.Now()
	w.mu.Unlock()

	start := time.Now()

	// Check if operation should be delayed
	if time.Now().Before(operation.ScheduledAt) {
		// Reschedule the operation
		go func() {
			time.Sleep(time.Until(operation.ScheduledAt))
			w.queue <- operation
		}()
		return
	}

	operation.StartedAt = time.Now()
	operation.Status = OperationStatusInProgress

	// Add to batch if batching is enabled
	if w.replication.config.EnableBatching {
		w.mu.Lock()
		w.batchQueue = append(w.batchQueue, operation)
		shouldFlush := len(w.batchQueue) >= w.replication.config.BatchSize
		w.mu.Unlock()

		if shouldFlush {
			w.processBatch(ctx)
		}
		return
	}

	// Process single operation
	success := w.replicateToNodes(ctx, []*AsyncOperation{operation})

	duration := time.Since(start)
	w.updateStats(success, duration)

	if success {
		operation.Status = OperationStatusSuccess
		operation.CompletedAt = time.Now()
		operation.ReplicationLag = operation.CompletedAt.Sub(operation.CreatedAt)
		w.replication.stats.ProcessedOperations++
	} else {
		w.handleFailedOperation(operation)
	}
}

func (w *AsyncWorker) processBatch(ctx context.Context) {
	w.mu.Lock()
	if len(w.batchQueue) == 0 {
		w.mu.Unlock()
		return
	}

	batch := make([]*AsyncOperation, len(w.batchQueue))
	copy(batch, w.batchQueue)
	w.batchQueue = w.batchQueue[:0] // Clear the batch queue
	w.mu.Unlock()

	start := time.Now()
	success := w.replicateToNodes(ctx, batch)
	duration := time.Since(start)

	w.mu.Lock()
	w.stats.BatchedOperations += int64(len(batch))
	w.mu.Unlock()

	w.updateStats(success, duration)

	for _, operation := range batch {
		if success {
			operation.Status = OperationStatusSuccess
			operation.CompletedAt = time.Now()
			operation.ReplicationLag = operation.CompletedAt.Sub(operation.CreatedAt)
			w.replication.stats.ProcessedOperations++
		} else {
			w.handleFailedOperation(operation)
		}
	}

	w.replication.stats.BatchedOperations += int64(len(batch))
}

func (w *AsyncWorker) replicateToNodes(ctx context.Context, operations []*AsyncOperation) bool {
	// Simulate replication to nodes
	// In a real implementation, this would send operations to cache nodes

	successCount := 0
	totalOperations := len(operations)

	for _, operation := range operations {
		for _, nodeID := range operation.TargetNodes {
			if w.replicateToNode(ctx, nodeID, operation) {
				operation.CompletedNodes = append(operation.CompletedNodes, nodeID)
				successCount++
			} else {
				operation.FailedNodes = append(operation.FailedNodes, nodeID)
			}
		}
	}

	// Consider operation successful if majority of nodes succeeded
	return float64(successCount) >= float64(totalOperations*len(operations[0].TargetNodes))*0.5
}

func (w *AsyncWorker) replicateToNode(ctx context.Context, nodeID string, operation *AsyncOperation) bool {
	// Simulate replication to a single node
	// In a real implementation, this would send the operation to the specific cache node

	node, exists := w.replication.nodes[nodeID]
	if !exists {
		return false
	}

	// Simulate network latency
	time.Sleep(time.Millisecond * 5)

	// Simulate success/failure based on node status
	if node.Status == NodeStatusOnline {
		// Update node statistics
		node.ProcessedOperations++
		node.LastSync = time.Now()

		// Update node stats in replication stats
		if nodeStats, exists := w.replication.stats.NodeStats[nodeID]; exists {
			nodeStats.ProcessedOperations++
			nodeStats.LastSync = time.Now()
		}

		return true
	}

	node.FailedOperations++
	if nodeStats, exists := w.replication.stats.NodeStats[nodeID]; exists {
		nodeStats.FailedOperations++
	}

	return false
}

func (w *AsyncWorker) handleFailedOperation(operation *AsyncOperation) {
	operation.Retries++

	if operation.Retries < operation.MaxRetries {
		// Retry the operation
		go func() {
			time.Sleep(w.replication.config.RetryDelay)
			w.queue <- operation
		}()
		w.replication.stats.RetriedOperations++
	} else {
		// Mark as permanently failed
		operation.Status = OperationStatusFailed
		operation.CompletedAt = time.Now()
		w.replication.stats.FailedOperations++
	}
}

func (w *AsyncWorker) updateStats(success bool, duration time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if success {
		w.stats.ProcessedOperations++
	} else {
		w.stats.FailedOperations++
	}

	// Update average latency
	if w.stats.AverageLatency == 0 {
		w.stats.AverageLatency = duration
	} else {
		w.stats.AverageLatency = (w.stats.AverageLatency + duration) / 2
	}

	// Update utilization (simplified calculation)
	w.stats.Utilization = float64(len(w.queue)) / float64(cap(w.queue))
	w.stats.QueueSize = len(w.queue)
}

// Helper methods

func (ar *AsyncReplication) distributeOperations(ctx context.Context) {
	for {
		select {
		case <-ar.stopChan:
			return
		case operation := <-ar.replicationQueue:
			// Distribute operation to least busy worker based on priority
			worker := ar.selectWorker(operation)
			select {
			case worker.queue <- operation:
				// Operation distributed successfully
			default:
				// Worker queue is full, put back to main queue or handle differently
				go func() {
					time.Sleep(time.Millisecond * 100)
					ar.replicationQueue <- operation
				}()
			}
		}
	}
}

func (ar *AsyncReplication) selectWorker(operation *AsyncOperation) *AsyncWorker {
	// Simple round-robin selection (can be enhanced with load balancing)
	// For priority operations, select least busy worker
	if operation.Priority >= PriorityHigh {
		minQueueSize := len(ar.workers[0].queue)
		selectedWorker := ar.workers[0]

		for _, worker := range ar.workers[1:] {
			if len(worker.queue) < minQueueSize {
				minQueueSize = len(worker.queue)
				selectedWorker = worker
			}
		}
		return selectedWorker
	}

	// For normal priority, use round-robin
	workerIndex := int(ar.stats.TotalOperations) % len(ar.workers)
	return ar.workers[workerIndex]
}

func (ar *AsyncReplication) healthCheckLoop(ctx context.Context) {
	ticker := time.NewTicker(ar.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ar.stopChan:
			return
		case <-ticker.C:
			ar.performHealthCheck(ctx)
		}
	}
}

func (ar *AsyncReplication) performHealthCheck(ctx context.Context) {
	// Check node health
	for nodeID, node := range ar.nodes {
		// Simulate health check
		if time.Since(node.LastSeen) > ar.config.HealthCheckInterval*3 {
			node.Status = NodeStatusOffline
		} else if node.ErrorRate > 0.5 {
			node.Status = NodeStatusDegraded
		} else {
			node.Status = NodeStatusOnline
		}

		// Update error rate
		if node.ProcessedOperations > 0 {
			node.ErrorRate = float64(node.FailedOperations) / float64(node.ProcessedOperations)
		}

		// Update node stats
		if nodeStats, exists := ar.stats.NodeStats[nodeID]; exists {
			nodeStats.Status = string(node.Status)
			nodeStats.ErrorRate = node.ErrorRate
		}
	}
}

func (ar *AsyncReplication) metricsLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ar.stopChan:
			return
		case <-ticker.C:
			ar.updateMetrics()
		}
	}
}

func (ar *AsyncReplication) updateMetrics() {
	// Update performance metrics
	if ar.metrics != nil {
		ar.metrics.Gauge("forge.cache.replication.async.queue.size").Set(float64(len(ar.replicationQueue)))
		ar.metrics.Gauge("forge.cache.replication.async.queue.utilization").Set(ar.stats.QueueUtilization)
		ar.metrics.Gauge("forge.cache.replication.async.worker.utilization").Set(ar.stats.WorkerUtilization)
		ar.metrics.Counter("forge.cache.replication.async.operations.total").Add(float64(ar.stats.TotalOperations))
		ar.metrics.Counter("forge.cache.replication.async.operations.processed").Add(float64(ar.stats.ProcessedOperations))
		ar.metrics.Counter("forge.cache.replication.async.operations.failed").Add(float64(ar.stats.FailedOperations))
	}

	// Update replication lag metrics
	var totalLag time.Duration
	var maxLag time.Duration
	lagCount := 0

	for _, nodeStats := range ar.stats.NodeStats {
		if nodeStats.AverageReplicationLag > 0 {
			totalLag += nodeStats.AverageReplicationLag
			lagCount++
			if nodeStats.AverageReplicationLag > maxLag {
				maxLag = nodeStats.AverageReplicationLag
			}
		}
	}

	if lagCount > 0 {
		ar.stats.AverageReplicationLag = totalLag / time.Duration(lagCount)
	}
	ar.stats.MaxReplicationLag = maxLag
}

func (ar *AsyncReplication) validateConfig() error {
	if ar.config.ReplicationFactor < 1 {
		return fmt.Errorf("replication factor must be at least 1")
	}

	if ar.config.WorkerCount < 1 {
		return fmt.Errorf("worker count must be at least 1")
	}

	if ar.config.QueueSize < 1 {
		return fmt.Errorf("queue size must be at least 1")
	}

	if ar.config.BatchSize < 1 {
		ar.config.BatchSize = 10
	}

	if ar.config.FlushInterval <= 0 {
		ar.config.FlushInterval = time.Second
	}

	return nil
}

func (ar *AsyncReplication) generateOperationID() string {
	return fmt.Sprintf("async-op-%d", time.Now().UnixNano())
}

func (ar *AsyncReplication) getNextVersion() int64 {
	return time.Now().UnixNano()
}
