package statemachine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/consensus/storage"
)

// MemoryStateMachine implements an in-memory state machine
type MemoryStateMachine struct {
	state          interface{}
	appliedEntries int64
	lastApplied    uint64
	snapshotCount  int64
	lastSnapshot   time.Time
	startTime      time.Time
	errorCount     int64
	lastError      string
	operationCount int64
	totalLatency   time.Duration
	observers      []StateMachineObserver
	mu             sync.RWMutex
}

// MemoryStateMachineFactory creates memory state machines
type MemoryStateMachineFactory struct{}

// NewMemoryStateMachineFactory creates a new memory state machine factory
func NewMemoryStateMachineFactory() *MemoryStateMachineFactory {
	return &MemoryStateMachineFactory{}
}

// Create creates a new memory state machine
func (f *MemoryStateMachineFactory) Create(config StateMachineConfig) (StateMachine, error) {
	sm := &MemoryStateMachine{
		state:          config.InitialState,
		appliedEntries: 0,
		lastApplied:    0,
		snapshotCount:  0,
		startTime:      time.Now(),
		errorCount:     0,
		operationCount: 0,
		totalLatency:   0,
		observers:      make([]StateMachineObserver, 0),
	}

	// Initialize with empty map if no initial state provided
	if sm.state == nil {
		sm.state = make(map[string]interface{})
	}

	return sm, nil
}

// Name returns the factory name
func (f *MemoryStateMachineFactory) Name() string {
	return StateMachineTypeMemory
}

// Version returns the factory version
func (f *MemoryStateMachineFactory) Version() string {
	return "1.0.0"
}

// Apply applies a log entry to the state machine
func (sm *MemoryStateMachine) Apply(ctx context.Context, entry storage.LogEntry) error {
	start := time.Now()
	defer func() {
		sm.mu.Lock()
		sm.operationCount++
		sm.totalLatency += time.Since(start)
		sm.mu.Unlock()
	}()

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Skip if already applied
	if entry.Index <= sm.lastApplied {
		return nil
	}

	oldState := sm.copyState()

	// Apply the entry based on its type
	switch entry.Type {
	case storage.EntryTypeApplication:
		if err := sm.applyApplicationEntry(entry); err != nil {
			sm.errorCount++
			sm.lastError = err.Error()
			return err
		}
	case storage.EntryTypeConfiguration:
		if err := sm.applyConfigurationEntry(entry); err != nil {
			sm.errorCount++
			sm.lastError = err.Error()
			return err
		}
	case storage.EntryTypeNoOp:
		// No-op entries don't change state
	default:
		err := fmt.Errorf("unknown entry type: %s", entry.Type)
		sm.errorCount++
		sm.lastError = err.Error()
		return err
	}

	sm.appliedEntries++
	sm.lastApplied = entry.Index

	// Notify observers
	for _, observer := range sm.observers {
		if err := observer.OnStateChange(oldState, sm.state); err != nil {
			// Log error but don't fail the operation
			sm.errorCount++
			sm.lastError = err.Error()
		}
	}

	return nil
}

// applyApplicationEntry applies an application log entry
func (sm *MemoryStateMachine) applyApplicationEntry(entry storage.LogEntry) error {
	var cmd Command
	if err := json.Unmarshal(entry.Data, &cmd); err != nil {
		return fmt.Errorf("failed to unmarshal command: %w", err)
	}

	// Apply the command to the state
	switch cmd.Type {
	case "set":
		return sm.applySetCommand(cmd)
	case "delete":
		return sm.applyDeleteCommand(cmd)
	case "clear":
		return sm.applyClearCommand(cmd)
	default:
		return fmt.Errorf("unknown command type: %s", cmd.Type)
	}
}

// applyConfigurationEntry applies a configuration log entry
func (sm *MemoryStateMachine) applyConfigurationEntry(entry storage.LogEntry) error {
	// Configuration entries don't change application state in memory state machine
	return nil
}

// applySetCommand applies a set command
func (sm *MemoryStateMachine) applySetCommand(cmd Command) error {
	data, ok := cmd.Data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid set command data")
	}

	key, ok := data["key"].(string)
	if !ok {
		return fmt.Errorf("invalid key in set command")
	}

	value := data["value"]

	// Ensure state is a map
	stateMap, ok := sm.state.(map[string]interface{})
	if !ok {
		stateMap = make(map[string]interface{})
		sm.state = stateMap
	}

	stateMap[key] = value
	return nil
}

// applyDeleteCommand applies a delete command
func (sm *MemoryStateMachine) applyDeleteCommand(cmd Command) error {
	data, ok := cmd.Data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid delete command data")
	}

	key, ok := data["key"].(string)
	if !ok {
		return fmt.Errorf("invalid key in delete command")
	}

	// Ensure state is a map
	stateMap, ok := sm.state.(map[string]interface{})
	if !ok {
		return nil // Nothing to delete
	}

	delete(stateMap, key)
	return nil
}

// applyClearCommand applies a clear command
func (sm *MemoryStateMachine) applyClearCommand(cmd Command) error {
	sm.state = make(map[string]interface{})
	return nil
}

// GetState returns the current state
func (sm *MemoryStateMachine) GetState() interface{} {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.copyState()
}

// copyState creates a deep copy of the current state
func (sm *MemoryStateMachine) copyState() interface{} {
	if sm.state == nil {
		return nil
	}

	// For simple types, return as is
	switch v := sm.state.(type) {
	case map[string]interface{}:
		copy := make(map[string]interface{})
		for k, val := range v {
			copy[k] = val
		}
		return copy
	default:
		// For complex types, use JSON marshaling/unmarshaling
		data, err := json.Marshal(sm.state)
		if err != nil {
			return sm.state // Return original if copy fails
		}
		var copy interface{}
		if err := json.Unmarshal(data, &copy); err != nil {
			return sm.state // Return original if copy fails
		}
		return copy
	}
}

// CreateSnapshot creates a snapshot of the current state
func (sm *MemoryStateMachine) CreateSnapshot() (*storage.Snapshot, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	data, err := json.Marshal(sm.state)
	if err != nil {
		sm.errorCount++
		sm.lastError = err.Error()
		return nil, fmt.Errorf("failed to marshal state: %w", err)
	}

	snapshot := &storage.Snapshot{
		Index: sm.lastApplied,
		Term:  0, // This would need to be provided by the caller
		Data:  data,
		Metadata: map[string]interface{}{
			"applied_entries": sm.appliedEntries,
			"state_size":      len(data),
			"created_at":      time.Now(),
		},
		Timestamp: time.Now(),
		Size:      int64(len(data)),
	}

	sm.snapshotCount++
	sm.lastSnapshot = time.Now()

	// Notify observers
	for _, observer := range sm.observers {
		if err := observer.OnSnapshot(snapshot); err != nil {
			sm.errorCount++
			sm.lastError = err.Error()
		}
	}

	return snapshot, nil
}

// RestoreSnapshot restores the state machine from a snapshot
func (sm *MemoryStateMachine) RestoreSnapshot(snapshot *storage.Snapshot) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var state interface{}
	if err := json.Unmarshal(snapshot.Data, &state); err != nil {
		sm.errorCount++
		sm.lastError = err.Error()
		return fmt.Errorf("failed to unmarshal snapshot data: %w", err)
	}

	oldState := sm.copyState()
	sm.state = state
	sm.lastApplied = snapshot.Index

	// Update metadata if available
	if metadata := snapshot.Metadata; metadata != nil {
		if appliedEntries, ok := metadata["applied_entries"].(float64); ok {
			sm.appliedEntries = int64(appliedEntries)
		}
	}

	// Notify observers
	for _, observer := range sm.observers {
		if err := observer.OnRestore(snapshot); err != nil {
			sm.errorCount++
			sm.lastError = err.Error()
		}
		if err := observer.OnStateChange(oldState, sm.state); err != nil {
			sm.errorCount++
			sm.lastError = err.Error()
		}
	}

	return nil
}

// Reset resets the state machine to initial state
func (sm *MemoryStateMachine) Reset() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	oldState := sm.copyState()
	sm.state = make(map[string]interface{})
	sm.appliedEntries = 0
	sm.lastApplied = 0
	sm.snapshotCount = 0
	sm.lastSnapshot = time.Time{}
	sm.errorCount = 0
	sm.lastError = ""
	sm.operationCount = 0
	sm.totalLatency = 0

	// Notify observers
	for _, observer := range sm.observers {
		if err := observer.OnStateChange(oldState, sm.state); err != nil {
			sm.errorCount++
			sm.lastError = err.Error()
		}
	}

	return nil
}

// Size returns the approximate size of the state machine in bytes
func (sm *MemoryStateMachine) Size() int64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	data, err := json.Marshal(sm.state)
	if err != nil {
		return 0
	}

	return int64(len(data))
}

// HealthCheck performs a health check on the state machine
func (sm *MemoryStateMachine) HealthCheck(ctx context.Context) error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Check if state is valid
	if sm.state == nil {
		return fmt.Errorf("state machine state is nil")
	}

	// Check error rate
	if sm.operationCount > 0 {
		errorRate := float64(sm.errorCount) / float64(sm.operationCount)
		if errorRate > 0.1 { // 10% error rate threshold
			return fmt.Errorf("high error rate: %.2f%%", errorRate*100)
		}
	}

	return nil
}

// GetStats returns statistics about the state machine
func (sm *MemoryStateMachine) GetStats() StateMachineStats {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var averageLatency time.Duration
	if sm.operationCount > 0 {
		averageLatency = sm.totalLatency / time.Duration(sm.operationCount)
	}

	var operationsPerSec float64
	uptime := time.Since(sm.startTime)
	if uptime > 0 {
		operationsPerSec = float64(sm.operationCount) / uptime.Seconds()
	}

	return StateMachineStats{
		AppliedEntries:   sm.appliedEntries,
		LastApplied:      sm.lastApplied,
		SnapshotCount:    sm.snapshotCount,
		LastSnapshot:     sm.lastSnapshot,
		StateSize:        sm.Size(),
		Uptime:           uptime,
		ErrorCount:       sm.errorCount,
		LastError:        sm.lastError,
		OperationsPerSec: operationsPerSec,
		AverageLatency:   averageLatency,
	}
}

// Close closes the state machine and releases resources
func (sm *MemoryStateMachine) Close(ctx context.Context) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Clear observers
	sm.observers = nil

	// Clear state
	sm.state = nil

	return nil
}

// AddObserver adds an observer to the state machine
func (sm *MemoryStateMachine) AddObserver(observer StateMachineObserver) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.observers = append(sm.observers, observer)
}

// RemoveObserver removes an observer from the state machine
func (sm *MemoryStateMachine) RemoveObserver(observer StateMachineObserver) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for i, obs := range sm.observers {
		if obs == observer {
			sm.observers = append(sm.observers[:i], sm.observers[i+1:]...)
			break
		}
	}
}

// GetObservers returns all observers
func (sm *MemoryStateMachine) GetObservers() []StateMachineObserver {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	observers := make([]StateMachineObserver, len(sm.observers))
	copy(observers, sm.observers)
	return observers
}

// Helper methods for command operations

// Set sets a key-value pair in the state (for testing/utility)
func (sm *MemoryStateMachine) Set(key string, value interface{}) error {
	cmd := Command{
		Type: "set",
		Data: map[string]interface{}{
			"key":   key,
			"value": value,
		},
		Timestamp: time.Now(),
	}

	return sm.applySetCommand(cmd)
}

// Get gets a value by key from the state (for testing/utility)
func (sm *MemoryStateMachine) Get(key string) (interface{}, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stateMap, ok := sm.state.(map[string]interface{})
	if !ok {
		return nil, false
	}

	value, exists := stateMap[key]
	return value, exists
}

// Delete deletes a key from the state (for testing/utility)
func (sm *MemoryStateMachine) Delete(key string) error {
	cmd := Command{
		Type: "delete",
		Data: map[string]interface{}{
			"key": key,
		},
		Timestamp: time.Now(),
	}

	return sm.applyDeleteCommand(cmd)
}

// Clear clears all state (for testing/utility)
func (sm *MemoryStateMachine) Clear() error {
	cmd := Command{
		Type:      "clear",
		Data:      nil,
		Timestamp: time.Now(),
	}

	return sm.applyClearCommand(cmd)
}

// Keys returns all keys in the state (for testing/utility)
func (sm *MemoryStateMachine) Keys() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stateMap, ok := sm.state.(map[string]interface{})
	if !ok {
		return nil
	}

	keys := make([]string, 0, len(stateMap))
	for key := range stateMap {
		keys = append(keys, key)
	}

	return keys
}

// Count returns the number of items in the state (for testing/utility)
func (sm *MemoryStateMachine) Count() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stateMap, ok := sm.state.(map[string]interface{})
	if !ok {
		return 0
	}

	return len(stateMap)
}
