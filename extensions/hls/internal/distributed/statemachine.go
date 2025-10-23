package distributed

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus"
)

// HLSStateMachine implements the state machine for distributed HLS operations
type HLSStateMachine struct {
	logger forge.Logger

	// State storage
	streams map[string]*StreamState
	nodes   map[string]*NodeState
	mu      sync.RWMutex

	// Stats
	appliedIndex uint64
	appliedTerm  uint64
}

// NewHLSStateMachine creates a new HLS state machine
func NewHLSStateMachine(logger forge.Logger) *HLSStateMachine {
	return &HLSStateMachine{
		logger:  logger,
		streams: make(map[string]*StreamState),
		nodes:   make(map[string]*NodeState),
	}
}

// Apply applies a log entry to the state machine
func (sm *HLSStateMachine) Apply(entry *consensus.LogEntry) interface{} {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Update applied index
	sm.appliedIndex = entry.Index
	sm.appliedTerm = entry.Term

	// Decode command
	var cmd consensus.Command
	if err := json.Unmarshal(entry.Data, &cmd); err != nil {
		sm.logger.Error("failed to unmarshal command",
			forge.F("error", err),
			forge.F("index", entry.Index),
		)
		return fmt.Errorf("failed to unmarshal command: %w", err)
	}

	// Handle command based on type
	switch cmd.Type {
	case CommandCreateStream:
		return sm.applyCreateStream(cmd)
	case CommandDeleteStream:
		return sm.applyDeleteStream(cmd)
	case CommandUpdateStream:
		return sm.applyUpdateStream(cmd)
	case CommandStartStream:
		return sm.applyStartStream(cmd)
	case CommandStopStream:
		return sm.applyStopStream(cmd)
	case CommandUpdateNode:
		return sm.applyUpdateNode(cmd)
	case CommandStreamOwner:
		return sm.applyStreamOwner(cmd)
	default:
		sm.logger.Warn("unknown command type",
			forge.F("type", cmd.Type),
			forge.F("index", entry.Index),
		)
		return fmt.Errorf("unknown command type: %s", cmd.Type)
	}
}

// applyCreateStream applies a create stream command
func (sm *HLSStateMachine) applyCreateStream(cmd consensus.Command) interface{} {
	streamID, ok := cmd.Payload["stream_id"].(string)
	if !ok {
		return fmt.Errorf("invalid stream_id in payload")
	}

	dataBytes, ok := cmd.Payload["data"].([]byte)
	if !ok {
		// Try to marshal the data if it's not bytes
		if data, ok := cmd.Payload["data"].(string); ok {
			dataBytes = []byte(data)
		} else {
			return fmt.Errorf("invalid data in payload")
		}
	}

	var state StreamState
	if err := json.Unmarshal(dataBytes, &state); err != nil {
		return fmt.Errorf("failed to unmarshal stream state: %w", err)
	}

	// Store stream state
	sm.streams[streamID] = &state

	sm.logger.Info("stream created in state machine",
		forge.F("stream_id", streamID),
		forge.F("owner", state.OwnerNode),
	)

	return nil
}

// applyDeleteStream applies a delete stream command
func (sm *HLSStateMachine) applyDeleteStream(cmd consensus.Command) interface{} {
	streamID, ok := cmd.Payload["stream_id"].(string)
	if !ok {
		return fmt.Errorf("invalid stream_id in payload")
	}

	delete(sm.streams, streamID)

	sm.logger.Info("stream deleted from state machine",
		forge.F("stream_id", streamID),
	)

	return nil
}

// applyUpdateStream applies an update stream command
func (sm *HLSStateMachine) applyUpdateStream(cmd consensus.Command) interface{} {
	streamID, ok := cmd.Payload["stream_id"].(string)
	if !ok {
		return fmt.Errorf("invalid stream_id in payload")
	}

	dataBytes, ok := cmd.Payload["data"].([]byte)
	if !ok {
		if data, ok := cmd.Payload["data"].(string); ok {
			dataBytes = []byte(data)
		} else {
			return fmt.Errorf("invalid data in payload")
		}
	}

	var state StreamState
	if err := json.Unmarshal(dataBytes, &state); err != nil {
		return fmt.Errorf("failed to unmarshal stream state: %w", err)
	}

	// Update existing stream
	sm.streams[streamID] = &state

	sm.logger.Info("stream updated in state machine",
		forge.F("stream_id", streamID),
	)

	return nil
}

// applyStartStream applies a start stream command
func (sm *HLSStateMachine) applyStartStream(cmd consensus.Command) interface{} {
	streamID, ok := cmd.Payload["stream_id"].(string)
	if !ok {
		return fmt.Errorf("invalid stream_id in payload")
	}

	if stream, exists := sm.streams[streamID]; exists {
		stream.Status = "started"
		stream.UpdatedAt = time.Now()
	}

	sm.logger.Info("stream started in state machine",
		forge.F("stream_id", streamID),
	)

	return nil
}

// applyStopStream applies a stop stream command
func (sm *HLSStateMachine) applyStopStream(cmd consensus.Command) interface{} {
	streamID, ok := cmd.Payload["stream_id"].(string)
	if !ok {
		return fmt.Errorf("invalid stream_id in payload")
	}

	if stream, exists := sm.streams[streamID]; exists {
		stream.Status = "stopped"
		stream.UpdatedAt = time.Now()
	}

	sm.logger.Info("stream stopped in state machine",
		forge.F("stream_id", streamID),
	)

	return nil
}

// applyUpdateNode applies an update node command
func (sm *HLSStateMachine) applyUpdateNode(cmd consensus.Command) interface{} {
	nodeID, ok := cmd.Payload["node_id"].(string)
	if !ok {
		return fmt.Errorf("invalid node_id in payload")
	}

	dataBytes, ok := cmd.Payload["data"].([]byte)
	if !ok {
		if data, ok := cmd.Payload["data"].(string); ok {
			dataBytes = []byte(data)
		} else {
			return fmt.Errorf("invalid data in payload")
		}
	}

	var state NodeState
	if err := json.Unmarshal(dataBytes, &state); err != nil {
		return fmt.Errorf("failed to unmarshal node state: %w", err)
	}

	// Update node state
	sm.nodes[nodeID] = &state

	return nil
}

// applyStreamOwner applies a stream owner change command
func (sm *HLSStateMachine) applyStreamOwner(cmd consensus.Command) interface{} {
	streamID, ok := cmd.Payload["stream_id"].(string)
	if !ok {
		return fmt.Errorf("invalid stream_id in payload")
	}

	newOwner, ok := cmd.Payload["owner"].(string)
	if !ok {
		return fmt.Errorf("invalid owner in payload")
	}

	if stream, exists := sm.streams[streamID]; exists {
		stream.OwnerNode = newOwner
		stream.UpdatedAt = time.Now()
	}

	sm.logger.Info("stream ownership transferred",
		forge.F("stream_id", streamID),
		forge.F("new_owner", newOwner),
	)

	return nil
}

// Snapshot creates a snapshot of the current state
func (sm *HLSStateMachine) Snapshot() ([]byte, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	snapshot := struct {
		Streams      map[string]*StreamState `json:"streams"`
		Nodes        map[string]*NodeState   `json:"nodes"`
		AppliedIndex uint64                  `json:"applied_index"`
		AppliedTerm  uint64                  `json:"applied_term"`
	}{
		Streams:      sm.streams,
		Nodes:        sm.nodes,
		AppliedIndex: sm.appliedIndex,
		AppliedTerm:  sm.appliedTerm,
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	sm.logger.Info("state machine snapshot created",
		forge.F("streams", len(sm.streams)),
		forge.F("nodes", len(sm.nodes)),
		forge.F("index", sm.appliedIndex),
	)

	return data, nil
}

// Restore restores state from a snapshot
func (sm *HLSStateMachine) Restore(data []byte) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var snapshot struct {
		Streams      map[string]*StreamState `json:"streams"`
		Nodes        map[string]*NodeState   `json:"nodes"`
		AppliedIndex uint64                  `json:"applied_index"`
		AppliedTerm  uint64                  `json:"applied_term"`
	}

	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	// Restore state
	sm.streams = snapshot.Streams
	sm.nodes = snapshot.Nodes
	sm.appliedIndex = snapshot.AppliedIndex
	sm.appliedTerm = snapshot.AppliedTerm

	sm.logger.Info("state machine restored from snapshot",
		forge.F("streams", len(sm.streams)),
		forge.F("nodes", len(sm.nodes)),
		forge.F("index", sm.appliedIndex),
	)

	return nil
}

// Query performs a read-only query on the state machine
func (sm *HLSStateMachine) Query(query interface{}) (interface{}, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Handle different query types
	switch q := query.(type) {
	case string:
		// Query by stream key
		if stream, exists := sm.streams[q]; exists {
			return stream, nil
		}
		return nil, fmt.Errorf("stream not found: %s", q)

	case map[string]interface{}:
		// Structured query
		queryType, ok := q["type"].(string)
		if !ok {
			return nil, fmt.Errorf("query type not specified")
		}

		switch queryType {
		case "get_stream":
			streamID, ok := q["stream_id"].(string)
			if !ok {
				return nil, fmt.Errorf("stream_id not specified")
			}
			if stream, exists := sm.streams[streamID]; exists {
				return stream, nil
			}
			return nil, fmt.Errorf("stream not found: %s", streamID)

		case "list_streams":
			return sm.streams, nil

		case "get_node":
			nodeID, ok := q["node_id"].(string)
			if !ok {
				return nil, fmt.Errorf("node_id not specified")
			}
			if node, exists := sm.nodes[nodeID]; exists {
				return node, nil
			}
			return nil, fmt.Errorf("node not found: %s", nodeID)

		case "list_nodes":
			return sm.nodes, nil

		default:
			return nil, fmt.Errorf("unknown query type: %s", queryType)
		}

	default:
		return nil, fmt.Errorf("unsupported query type: %T", query)
	}
}

// GetStats returns state machine statistics
func (sm *HLSStateMachine) GetStats() map[string]interface{} {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	return map[string]interface{}{
		"streams_count": len(sm.streams),
		"nodes_count":   len(sm.nodes),
		"applied_index": sm.appliedIndex,
		"applied_term":  sm.appliedTerm,
	}
}
