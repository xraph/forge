package replication

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// ConsistencyManager manages different consistency models for cache replication
type ConsistencyManager struct {
	models       map[string]ConsistencyModel
	config       ConsistencyConfig
	vectorClocks map[string]*VectorClock
	logger       common.Logger
	metrics      common.Metrics
	mu           sync.RWMutex
	stats        ConsistencyStats
}

// ConsistencyModel defines the interface for consistency models
type ConsistencyModel interface {
	Name() string
	ValidateRead(ctx context.Context, key string, values map[string]*DataValue) (*DataValue, error)
	ValidateWrite(ctx context.Context, key string, value *DataValue, replicas []string) error
	ResolveConflict(ctx context.Context, key string, conflicts []*DataValue) (*DataValue, error)
	GetRequiredNodes(totalNodes int) (readNodes, writeNodes int)
	GetConsistencyLevel() ConsistencyLevel
}

// ConsistencyLevel represents different consistency levels
type ConsistencyLevel string

const (
	ConsistencyLevelStrong    ConsistencyLevel = "strong"
	ConsistencyLevelEventual  ConsistencyLevel = "eventual"
	ConsistencyLevelCausal    ConsistencyLevel = "causal"
	ConsistencyLevelMonotonic ConsistencyLevel = "monotonic"
	ConsistencyLevelSession   ConsistencyLevel = "session"
	ConsistencyLevelBounded   ConsistencyLevel = "bounded"
)

// ConsistencyConfig contains configuration for consistency management
type ConsistencyConfig struct {
	DefaultModel       string                     `yaml:"default_model" json:"default_model"`
	ModelConfigs       map[string]ModelConfig     `yaml:"model_configs" json:"model_configs"`
	ConflictResolution ConflictResolutionStrategy `yaml:"conflict_resolution" json:"conflict_resolution"`
	MaxClockSkew       time.Duration              `yaml:"max_clock_skew" json:"max_clock_skew"`
	VectorClockSync    bool                       `yaml:"vector_clock_sync" json:"vector_clock_sync"`
	ReadRepairEnabled  bool                       `yaml:"read_repair_enabled" json:"read_repair_enabled"`
	ConsistencyTimeout time.Duration              `yaml:"consistency_timeout" json:"consistency_timeout"`
	ValidationInterval time.Duration              `yaml:"validation_interval" json:"validation_interval"`
	EnableMetrics      bool                       `yaml:"enable_metrics" json:"enable_metrics"`
}

// ModelConfig contains configuration for a specific consistency model
type ModelConfig struct {
	Enabled            bool                       `yaml:"enabled" json:"enabled"`
	ReadQuorum         int                        `yaml:"read_quorum" json:"read_quorum"`
	WriteQuorum        int                        `yaml:"write_quorum" json:"write_quorum"`
	Parameters         map[string]interface{}     `yaml:"parameters" json:"parameters"`
	ConflictResolution ConflictResolutionStrategy `yaml:"conflict_resolution" json:"conflict_resolution"`
	Timeout            time.Duration              `yaml:"timeout" json:"timeout"`
}

// ConflictResolutionStrategy defines different conflict resolution strategies
type ConflictResolutionStrategy string

const (
	ConflictResolutionLastWriteWins ConflictResolutionStrategy = "last_write_wins"
	ConflictResolutionVectorClock   ConflictResolutionStrategy = "vector_clock"
	ConflictResolutionCustom        ConflictResolutionStrategy = "custom"
	ConflictResolutionMerge         ConflictResolutionStrategy = "merge"
	ConflictResolutionManual        ConflictResolutionStrategy = "manual"
)

// ConsistencyStats contains statistics for consistency management
type ConsistencyStats struct {
	TotalValidations      int64                  `json:"total_validations"`
	SuccessfulReads       int64                  `json:"successful_reads"`
	FailedReads           int64                  `json:"failed_reads"`
	ConflictsDetected     int64                  `json:"conflicts_detected"`
	ConflictsResolved     int64                  `json:"conflicts_resolved"`
	ReadRepairs           int64                  `json:"read_repairs"`
	ConsistencyViolations int64                  `json:"consistency_violations"`
	AverageValidationTime time.Duration          `json:"average_validation_time"`
	ModelStats            map[string]*ModelStats `json:"model_stats"`
	ClockStats            *VectorClockStats      `json:"clock_stats"`
}

// ModelStats contains statistics for a specific consistency model
type ModelStats struct {
	ModelName       string        `json:"model_name"`
	UsageCount      int64         `json:"usage_count"`
	SuccessRate     float64       `json:"success_rate"`
	AverageLatency  time.Duration `json:"average_latency"`
	ConflictRate    float64       `json:"conflict_rate"`
	ReadRepairCount int64         `json:"read_repair_count"`
}

// DataValue represents a value with consistency metadata
type DataValue struct {
	Value       interface{}            `json:"value"`
	Version     int64                  `json:"version"`
	Timestamp   time.Time              `json:"timestamp"`
	NodeID      string                 `json:"node_id"`
	VectorClock *VectorClock           `json:"vector_clock"`
	Checksum    string                 `json:"checksum"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// VectorClock implements vector clock for causal consistency
type VectorClock struct {
	Clocks map[string]int64 `json:"clocks"`
	mu     sync.RWMutex
}

// VectorClockStats contains statistics for vector clocks
type VectorClockStats struct {
	TotalClocks      int64         `json:"total_clocks"`
	ClockUpdates     int64         `json:"clock_updates"`
	ClockSyncs       int64         `json:"clock_syncs"`
	ConflictCount    int64         `json:"conflict_count"`
	AverageClockSize int           `json:"average_clock_size"`
	MaxClockSkew     time.Duration `json:"max_clock_skew"`
}

// NewConsistencyManager creates a new consistency manager
func NewConsistencyManager(config ConsistencyConfig, logger common.Logger, metrics common.Metrics) *ConsistencyManager {
	cm := &ConsistencyManager{
		models:       make(map[string]ConsistencyModel),
		config:       config,
		vectorClocks: make(map[string]*VectorClock),
		logger:       logger,
		metrics:      metrics,
		stats: ConsistencyStats{
			ModelStats: make(map[string]*ModelStats),
			ClockStats: &VectorClockStats{},
		},
	}

	// Initialize consistency models
	cm.initializeModels()

	return cm
}

// RegisterModel registers a consistency model
func (cm *ConsistencyManager) RegisterModel(model ConsistencyModel) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	name := model.Name()
	if _, exists := cm.models[name]; exists {
		return fmt.Errorf("consistency model %s already registered", name)
	}

	cm.models[name] = model
	cm.stats.ModelStats[name] = &ModelStats{
		ModelName: name,
	}

	cm.logger.Info("consistency model registered",
		logger.String("model", name),
		logger.String("level", string(model.GetConsistencyLevel())),
	)

	return nil
}

// ValidateRead validates a read operation according to the specified consistency model
func (cm *ConsistencyManager) ValidateRead(ctx context.Context, modelName, key string, values map[string]*DataValue) (*DataValue, error) {
	start := time.Now()
	defer func() {
		cm.updateValidationStats(modelName, time.Since(start))
	}()

	cm.mu.RLock()
	model, exists := cm.models[modelName]
	if !exists {
		cm.mu.RUnlock()
		return nil, fmt.Errorf("consistency model %s not found", modelName)
	}
	cm.mu.RUnlock()

	cm.stats.TotalValidations++

	// Validate read using the specified model
	value, err := model.ValidateRead(ctx, key, values)
	if err != nil {
		cm.stats.FailedReads++
		if modelStats, exists := cm.stats.ModelStats[modelName]; exists {
			modelStats.SuccessRate = float64(cm.stats.SuccessfulReads) / float64(cm.stats.TotalValidations)
		}
		return nil, err
	}

	cm.stats.SuccessfulReads++

	// Perform read repair if inconsistencies detected
	if cm.config.ReadRepairEnabled && cm.hasInconsistencies(values) {
		go cm.performReadRepair(ctx, key, value, values)
	}

	// Update model statistics
	if modelStats, exists := cm.stats.ModelStats[modelName]; exists {
		modelStats.UsageCount++
		modelStats.SuccessRate = float64(cm.stats.SuccessfulReads) / float64(cm.stats.TotalValidations)
		if modelStats.AverageLatency == 0 {
			modelStats.AverageLatency = time.Since(start)
		} else {
			modelStats.AverageLatency = (modelStats.AverageLatency + time.Since(start)) / 2
		}
	}

	return value, nil
}

// ValidateWrite validates a write operation according to the specified consistency model
func (cm *ConsistencyManager) ValidateWrite(ctx context.Context, modelName, key string, value *DataValue, replicas []string) error {
	cm.mu.RLock()
	model, exists := cm.models[modelName]
	if !exists {
		cm.mu.RUnlock()
		return fmt.Errorf("consistency model %s not found", modelName)
	}
	cm.mu.RUnlock()

	// Update vector clock if causal consistency is enabled
	if cm.config.VectorClockSync {
		cm.updateVectorClock(key, value.NodeID)
		value.VectorClock = cm.getVectorClock(key)
	}

	return model.ValidateWrite(ctx, key, value, replicas)
}

// ResolveConflict resolves conflicts using the specified consistency model
func (cm *ConsistencyManager) ResolveConflict(ctx context.Context, modelName, key string, conflicts []*DataValue) (*DataValue, error) {
	start := time.Now()

	cm.mu.RLock()
	model, exists := cm.models[modelName]
	if !exists {
		cm.mu.RUnlock()
		return nil, fmt.Errorf("consistency model %s not found", modelName)
	}
	cm.mu.RUnlock()

	cm.stats.ConflictsDetected++

	value, err := model.ResolveConflict(ctx, key, conflicts)
	if err != nil {
		return nil, err
	}

	cm.stats.ConflictsResolved++

	// Update model statistics
	if modelStats, exists := cm.stats.ModelStats[modelName]; exists {
		modelStats.ConflictRate = float64(cm.stats.ConflictsDetected) / float64(modelStats.UsageCount)
	}

	cm.logger.Debug("conflict resolved",
		logger.String("model", modelName),
		logger.String("key", key),
		logger.Int("conflicts", len(conflicts)),
		logger.Duration("resolution_time", time.Since(start)),
	)

	return value, nil
}

// GetRequiredNodes returns the required number of nodes for read and write operations
func (cm *ConsistencyManager) GetRequiredNodes(modelName string, totalNodes int) (int, int, error) {
	cm.mu.RLock()
	model, exists := cm.models[modelName]
	if !exists {
		cm.mu.RUnlock()
		return 0, 0, fmt.Errorf("consistency model %s not found", modelName)
	}
	cm.mu.RUnlock()

	readNodes, writeNodes := model.GetRequiredNodes(totalNodes)
	return readNodes, writeNodes, nil
}

// GetStats returns consistency management statistics
func (cm *ConsistencyManager) GetStats() ConsistencyStats {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.stats
}

// Vector Clock implementation

// NewVectorClock creates a new vector clock
func NewVectorClock() *VectorClock {
	return &VectorClock{
		Clocks: make(map[string]int64),
	}
}

// Update updates the vector clock for a node
func (vc *VectorClock) Update(nodeID string) {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	vc.Clocks[nodeID]++
}

// Merge merges another vector clock into this one
func (vc *VectorClock) Merge(other *VectorClock) {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	if other == nil {
		return
	}

	other.mu.RLock()
	defer other.mu.RUnlock()

	for nodeID, clock := range other.Clocks {
		if vc.Clocks[nodeID] < clock {
			vc.Clocks[nodeID] = clock
		}
	}
}

// Compare compares two vector clocks
func (vc *VectorClock) Compare(other *VectorClock) ClockRelation {
	if other == nil {
		return ClockConcurrent
	}

	vc.mu.RLock()
	defer vc.mu.RUnlock()

	other.mu.RLock()
	defer other.mu.RUnlock()

	allLessOrEqual := true
	allGreaterOrEqual := true
	hasLess := false
	hasGreater := false

	// Get all node IDs from both clocks
	allNodes := make(map[string]bool)
	for nodeID := range vc.Clocks {
		allNodes[nodeID] = true
	}
	for nodeID := range other.Clocks {
		allNodes[nodeID] = true
	}

	for nodeID := range allNodes {
		thisClock := vc.Clocks[nodeID]
		otherClock := other.Clocks[nodeID]

		if thisClock < otherClock {
			allGreaterOrEqual = false
			hasLess = true
		} else if thisClock > otherClock {
			allLessOrEqual = false
			hasGreater = true
		}
	}

	if allLessOrEqual && !hasGreater {
		if hasLess {
			return ClockBefore
		}
		return ClockEqual
	}

	if allGreaterOrEqual && !hasLess {
		return ClockAfter
	}

	return ClockConcurrent
}

// Copy creates a copy of the vector clock
func (vc *VectorClock) Copy() *VectorClock {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	copy := NewVectorClock()
	for nodeID, clock := range vc.Clocks {
		copy.Clocks[nodeID] = clock
	}

	return copy
}

// ClockRelation represents the relationship between two vector clocks
type ClockRelation int

const (
	ClockBefore     ClockRelation = iota // This clock is before the other
	ClockAfter                           // This clock is after the other
	ClockEqual                           // Clocks are equal
	ClockConcurrent                      // Clocks are concurrent (no causal relationship)
)

// Consistency Model Implementations

// StrongConsistency implements strong consistency model
type StrongConsistency struct {
	config ModelConfig
}

func NewStrongConsistency(config ModelConfig) *StrongConsistency {
	return &StrongConsistency{config: config}
}

func (sc *StrongConsistency) Name() string {
	return "strong"
}

func (sc *StrongConsistency) GetConsistencyLevel() ConsistencyLevel {
	return ConsistencyLevelStrong
}

func (sc *StrongConsistency) ValidateRead(ctx context.Context, key string, values map[string]*DataValue) (*DataValue, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("no values provided for validation")
	}

	// For strong consistency, all replicas must have the same value
	var firstValue *DataValue
	for _, value := range values {
		if firstValue == nil {
			firstValue = value
			continue
		}

		if firstValue.Version != value.Version || firstValue.Checksum != value.Checksum {
			return nil, fmt.Errorf("strong consistency violation: inconsistent values detected")
		}
	}

	return firstValue, nil
}

func (sc *StrongConsistency) ValidateWrite(ctx context.Context, key string, value *DataValue, replicas []string) error {
	// Strong consistency requires all replicas to acknowledge the write
	requiredAcks := len(replicas)
	if requiredAcks < sc.config.WriteQuorum {
		return fmt.Errorf("insufficient replicas for strong consistency")
	}

	return nil
}

func (sc *StrongConsistency) ResolveConflict(ctx context.Context, key string, conflicts []*DataValue) (*DataValue, error) {
	// In strong consistency, conflicts should not occur
	// If they do, choose the value with the highest version
	if len(conflicts) == 0 {
		return nil, fmt.Errorf("no conflicts to resolve")
	}

	latest := conflicts[0]
	for _, conflict := range conflicts[1:] {
		if conflict.Version > latest.Version {
			latest = conflict
		}
	}

	return latest, nil
}

func (sc *StrongConsistency) GetRequiredNodes(totalNodes int) (int, int) {
	// Strong consistency requires all nodes
	return totalNodes, totalNodes
}

// EventualConsistency implements eventual consistency model
type EventualConsistency struct {
	config ModelConfig
}

func NewEventualConsistency(config ModelConfig) *EventualConsistency {
	return &EventualConsistency{config: config}
}

func (ec *EventualConsistency) Name() string {
	return "eventual"
}

func (ec *EventualConsistency) GetConsistencyLevel() ConsistencyLevel {
	return ConsistencyLevelEventual
}

func (ec *EventualConsistency) ValidateRead(ctx context.Context, key string, values map[string]*DataValue) (*DataValue, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("no values provided for validation")
	}

	// For eventual consistency, return the value with the highest version
	var latest *DataValue
	for _, value := range values {
		if latest == nil || value.Version > latest.Version {
			latest = value
		}
	}

	return latest, nil
}

func (ec *EventualConsistency) ValidateWrite(ctx context.Context, key string, value *DataValue, replicas []string) error {
	// Eventual consistency allows writes as long as some replicas are available
	availableReplicas := len(replicas)
	if availableReplicas < 1 {
		return fmt.Errorf("no replicas available for write")
	}

	return nil
}

func (ec *EventualConsistency) ResolveConflict(ctx context.Context, key string, conflicts []*DataValue) (*DataValue, error) {
	// Use last-write-wins conflict resolution
	if len(conflicts) == 0 {
		return nil, fmt.Errorf("no conflicts to resolve")
	}

	latest := conflicts[0]
	for _, conflict := range conflicts[1:] {
		if conflict.Timestamp.After(latest.Timestamp) {
			latest = conflict
		}
	}

	return latest, nil
}

func (ec *EventualConsistency) GetRequiredNodes(totalNodes int) (int, int) {
	// Eventual consistency can work with any number of nodes
	readQuorum := 1
	writeQuorum := 1

	if ec.config.ReadQuorum > 0 {
		readQuorum = ec.config.ReadQuorum
	}
	if ec.config.WriteQuorum > 0 {
		writeQuorum = ec.config.WriteQuorum
	}

	return readQuorum, writeQuorum
}

// Helper methods

func (cm *ConsistencyManager) initializeModels() {
	// Initialize default consistency models
	if config, exists := cm.config.ModelConfigs["strong"]; exists && config.Enabled {
		cm.RegisterModel(NewStrongConsistency(config))
	}

	if config, exists := cm.config.ModelConfigs["eventual"]; exists && config.Enabled {
		cm.RegisterModel(NewEventualConsistency(config))
	}
}

func (cm *ConsistencyManager) updateVectorClock(key, nodeID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, exists := cm.vectorClocks[key]; !exists {
		cm.vectorClocks[key] = NewVectorClock()
	}

	cm.vectorClocks[key].Update(nodeID)
	cm.stats.ClockStats.ClockUpdates++
}

func (cm *ConsistencyManager) getVectorClock(key string) *VectorClock {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if clock, exists := cm.vectorClocks[key]; exists {
		return clock.Copy()
	}

	return NewVectorClock()
}

func (cm *ConsistencyManager) hasInconsistencies(values map[string]*DataValue) bool {
	if len(values) <= 1 {
		return false
	}

	var firstValue *DataValue
	for _, value := range values {
		if firstValue == nil {
			firstValue = value
			continue
		}

		if firstValue.Version != value.Version || firstValue.Checksum != value.Checksum {
			return true
		}
	}

	return false
}

func (cm *ConsistencyManager) performReadRepair(ctx context.Context, key string, correctValue *DataValue, allValues map[string]*DataValue) {
	cm.stats.ReadRepairs++

	// Identify nodes that need repair
	var repairNodes []string
	for nodeID, value := range allValues {
		if value.Version != correctValue.Version || value.Checksum != correctValue.Checksum {
			repairNodes = append(repairNodes, nodeID)
		}
	}

	if len(repairNodes) > 0 {
		cm.logger.Debug("performing read repair",
			logger.String("key", key),
			logger.Int("repair_nodes", len(repairNodes)),
			logger.Int64("correct_version", correctValue.Version),
		)

		// Update model statistics
		for _, modelStats := range cm.stats.ModelStats {
			modelStats.ReadRepairCount++
		}
	}

	// In a real implementation, this would trigger repair operations to update the inconsistent nodes
}

func (cm *ConsistencyManager) updateValidationStats(modelName string, duration time.Duration) {
	if cm.stats.AverageValidationTime == 0 {
		cm.stats.AverageValidationTime = duration
	} else {
		cm.stats.AverageValidationTime = (cm.stats.AverageValidationTime + duration) / 2
	}

	if modelStats, exists := cm.stats.ModelStats[modelName]; exists {
		if modelStats.AverageLatency == 0 {
			modelStats.AverageLatency = duration
		} else {
			modelStats.AverageLatency = (modelStats.AverageLatency + duration) / 2
		}
	}
}
