package statemachine

import (
	"encoding/json"
	"fmt"
	"maps"
	"sync"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// MemoryStateMachine implements a simple in-memory state machine.
type MemoryStateMachine struct {
	state  map[string]any
	mu     sync.RWMutex
	logger forge.Logger
}

// MemoryStateMachineConfig contains configuration for memory state machine.
type MemoryStateMachineConfig struct {
	InitialCapacity int
}

// NewMemoryStateMachine creates a new memory state machine.
func NewMemoryStateMachine(config MemoryStateMachineConfig, logger forge.Logger) *MemoryStateMachine {
	capacity := config.InitialCapacity
	if capacity == 0 {
		capacity = 1000
	}

	return &MemoryStateMachine{
		state:  make(map[string]any, capacity),
		logger: logger,
	}
}

// Apply applies a log entry to the state machine.
func (sm *MemoryStateMachine) Apply(entry internal.LogEntry) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	switch entry.Type {
	case internal.EntryNormal:
		// Parse command from entry data
		var cmd internal.Command
		if err := json.Unmarshal(entry.Data, &cmd); err != nil {
			return fmt.Errorf("failed to unmarshal command: %w", err)
		}

		return sm.applyCommand(cmd)

	case internal.EntryConfig:
		// Configuration changes are handled by the Raft layer
		sm.logger.Debug("config entry applied",
			forge.F("index", entry.Index),
		)

		return nil

	case internal.EntryNoop:
		// No-op entries don't change state
		sm.logger.Debug("no-op entry applied",
			forge.F("index", entry.Index),
		)

		return nil

	case internal.EntryBarrier:
		// Barrier entries are for read consistency
		sm.logger.Debug("barrier entry applied",
			forge.F("index", entry.Index),
		)

		return nil

	default:
		return fmt.Errorf("unknown entry type: %d", entry.Type)
	}
}

// applyCommand applies a command to the state machine.
func (sm *MemoryStateMachine) applyCommand(cmd internal.Command) error {
	switch cmd.Type {
	case "set":
		key, ok := cmd.Payload["key"].(string)
		if !ok {
			return errors.New("invalid key in set command")
		}

		value := cmd.Payload["value"]
		sm.state[key] = value

		sm.logger.Debug("applied set command",
			forge.F("key", key),
		)

	case "delete":
		key, ok := cmd.Payload["key"].(string)
		if !ok {
			return errors.New("invalid key in delete command")
		}

		delete(sm.state, key)

		sm.logger.Debug("applied delete command",
			forge.F("key", key),
		)

	case "increment":
		key, ok := cmd.Payload["key"].(string)
		if !ok {
			return errors.New("invalid key in increment command")
		}

		current, exists := sm.state[key]
		if !exists {
			sm.state[key] = 1
		} else {
			if num, ok := current.(float64); ok {
				sm.state[key] = num + 1
			} else if num, ok := current.(int); ok {
				sm.state[key] = num + 1
			} else {
				return errors.New("cannot increment non-numeric value")
			}
		}

		sm.logger.Debug("applied increment command",
			forge.F("key", key),
		)

	default:
		return fmt.Errorf("unknown command type: %s", cmd.Type)
	}

	return nil
}

// Snapshot creates a snapshot of the current state.
func (sm *MemoryStateMachine) Snapshot() (*internal.Snapshot, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Serialize the state
	data, err := json.Marshal(sm.state)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal state: %w", err)
	}

	snapshot := &internal.Snapshot{
		Data: data,
		Size: int64(len(data)),
	}

	sm.logger.Info("snapshot created",
		forge.F("size", snapshot.Size),
	)

	return snapshot, nil
}

// Restore restores the state machine from a snapshot.
func (sm *MemoryStateMachine) Restore(snapshot *internal.Snapshot) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Deserialize the state
	newState := make(map[string]any)
	if err := json.Unmarshal(snapshot.Data, &newState); err != nil {
		return fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	sm.state = newState

	sm.logger.Info("snapshot restored",
		forge.F("index", snapshot.Index),
		forge.F("term", snapshot.Term),
		forge.F("size", snapshot.Size),
	)

	return nil
}

// Query performs a read-only query.
func (sm *MemoryStateMachine) Query(query any) (any, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Parse query
	q, ok := query.(map[string]any)
	if !ok {
		return nil, errors.New("invalid query format")
	}

	queryType, ok := q["type"].(string)
	if !ok {
		return nil, errors.New("query type not specified")
	}

	switch queryType {
	case "get":
		key, ok := q["key"].(string)
		if !ok {
			return nil, errors.New("invalid key in get query")
		}

		value, exists := sm.state[key]
		if !exists {
			return nil, fmt.Errorf("key not found: %s", key)
		}

		return value, nil

	case "list":
		return sm.state, nil

	case "size":
		return len(sm.state), nil

	default:
		return nil, fmt.Errorf("unknown query type: %s", queryType)
	}
}

// GetState returns a copy of the current state (for testing).
func (sm *MemoryStateMachine) GetState() map[string]any {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Return a shallow copy
	stateCopy := make(map[string]any, len(sm.state))
	maps.Copy(stateCopy, sm.state)

	return stateCopy
}

// Clear clears the state machine (for testing).
func (sm *MemoryStateMachine) Clear() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.state = make(map[string]any)
	sm.logger.Info("state machine cleared")
}
