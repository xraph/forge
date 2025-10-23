package statemachine

import (
	"context"
	"time"

	"github.com/xraph/forge/pkg/consensus/storage"
)

// StateMachine defines the interface for applying log entries to application state
type StateMachine interface {
	// Apply applies a log entry to the state machine
	Apply(ctx context.Context, entry storage.LogEntry) error

	// GetState returns the current state of the state machine
	GetState() interface{}

	// CreateSnapshot creates a snapshot of the current state
	CreateSnapshot() (*storage.Snapshot, error)

	// RestoreSnapshot restores the state machine from a snapshot
	RestoreSnapshot(snapshot *storage.Snapshot) error

	// Reset resets the state machine to initial state
	Reset() error

	// Size returns the approximate size of the state machine in bytes
	Size() int64

	// HealthCheck performs a health check on the state machine
	HealthCheck(ctx context.Context) error

	// GetStats returns statistics about the state machine
	GetStats() StateMachineStats

	// Close closes the state machine and releases resources
	Close(ctx context.Context) error
}

// StateMachineStats contains statistics about the state machine
type StateMachineStats struct {
	AppliedEntries   int64         `json:"applied_entries"`
	LastApplied      uint64        `json:"last_applied"`
	SnapshotCount    int64         `json:"snapshot_count"`
	LastSnapshot     time.Time     `json:"last_snapshot"`
	StateSize        int64         `json:"state_size"`
	Uptime           time.Duration `json:"uptime"`
	ErrorCount       int64         `json:"error_count"`
	LastError        string        `json:"last_error,omitempty"`
	OperationsPerSec float64       `json:"operations_per_sec"`
	AverageLatency   time.Duration `json:"average_latency"`
}

// StateMachineFactory creates state machines
type StateMachineFactory interface {
	// Create creates a new state machine instance
	Create(config StateMachineConfig) (StateMachine, error)

	// Name returns the name of the state machine type
	Name() string

	// Version returns the version of the state machine implementation
	Version() string
}

// StateMachineConfig contains configuration for state machines
type StateMachineConfig struct {
	Type         string                 `json:"type"`
	InitialState interface{}            `json:"initial_state"`
	Options      map[string]interface{} `json:"options"`
	Persistent   bool                   `json:"persistent"`
	StoragePath  string                 `json:"storage_path"`
}

// ApplicationStateMachine extends StateMachine with application-specific methods
type ApplicationStateMachine interface {
	StateMachine

	// Command executes a command on the state machine
	Command(ctx context.Context, cmd Command) (interface{}, error)

	// Query performs a read-only query on the state machine
	Query(ctx context.Context, query Query) (interface{}, error)

	// RegisterHandler registers a command handler
	RegisterHandler(cmdType string, handler CommandHandler) error

	// UnregisterHandler unregisters a command handler
	UnregisterHandler(cmdType string) error

	// GetHandlers returns all registered command handlers
	GetHandlers() map[string]CommandHandler
}

// Command represents a command to be executed on the state machine
type Command struct {
	Type      string                 `json:"type"`
	Data      interface{}            `json:"data"`
	Metadata  map[string]interface{} `json:"metadata"`
	Timestamp time.Time              `json:"timestamp"`
}

// Query represents a read-only query on the state machine
type Query struct {
	Type      string                 `json:"type"`
	Data      interface{}            `json:"data"`
	Metadata  map[string]interface{} `json:"metadata"`
	Timestamp time.Time              `json:"timestamp"`
}

// CommandHandler handles commands for the state machine
type CommandHandler func(ctx context.Context, state interface{}, cmd Command) (interface{}, error)

// QueryHandler handles queries for the state machine
type QueryHandler func(ctx context.Context, state interface{}, query Query) (interface{}, error)

// StateMachineObserver observes state machine events
type StateMachineObserver interface {
	// OnStateChange is called when the state machine state changes
	OnStateChange(oldState, newState interface{}) error

	// OnSnapshot is called when a snapshot is created
	OnSnapshot(snapshot *storage.Snapshot) error

	// OnRestore is called when a snapshot is restored
	OnRestore(snapshot *storage.Snapshot) error

	// OnError is called when an error occurs
	OnError(err error)
}

// StateMachineBuilder builds state machines with fluent API
type StateMachineBuilder struct {
	config    StateMachineConfig
	observers []StateMachineObserver
	handlers  map[string]CommandHandler
}

// NewStateMachineBuilder creates a new state machine builder
func NewStateMachineBuilder() *StateMachineBuilder {
	return &StateMachineBuilder{
		config: StateMachineConfig{
			Options: make(map[string]interface{}),
		},
		observers: make([]StateMachineObserver, 0),
		handlers:  make(map[string]CommandHandler),
	}
}

// WithType sets the state machine type
func (b *StateMachineBuilder) WithType(smType string) *StateMachineBuilder {
	b.config.Type = smType
	return b
}

// WithInitialState sets the initial state
func (b *StateMachineBuilder) WithInitialState(state interface{}) *StateMachineBuilder {
	b.config.InitialState = state
	return b
}

// WithPersistence enables persistence
func (b *StateMachineBuilder) WithPersistence(persistent bool, storagePath string) *StateMachineBuilder {
	b.config.Persistent = persistent
	b.config.StoragePath = storagePath
	return b
}

// WithOption sets an option
func (b *StateMachineBuilder) WithOption(key string, value interface{}) *StateMachineBuilder {
	b.config.Options[key] = value
	return b
}

// WithObserver adds an observer
func (b *StateMachineBuilder) WithObserver(observer StateMachineObserver) *StateMachineBuilder {
	b.observers = append(b.observers, observer)
	return b
}

// WithHandler adds a command handler
func (b *StateMachineBuilder) WithHandler(cmdType string, handler CommandHandler) *StateMachineBuilder {
	b.handlers[cmdType] = handler
	return b
}

// Build builds the state machine
func (b *StateMachineBuilder) Build(factory StateMachineFactory) (StateMachine, error) {
	sm, err := factory.Create(b.config)
	if err != nil {
		return nil, err
	}

	// Add observers if the state machine supports them
	if observable, ok := sm.(interface {
		AddObserver(StateMachineObserver)
	}); ok {
		for _, observer := range b.observers {
			observable.AddObserver(observer)
		}
	}

	// Add handlers if the state machine supports them
	if appSM, ok := sm.(ApplicationStateMachine); ok {
		for cmdType, handler := range b.handlers {
			if err := appSM.RegisterHandler(cmdType, handler); err != nil {
				return nil, err
			}
		}
	}

	return sm, nil
}

// Common state machine types
const (
	StateMachineTypeMemory      = "memory"
	StateMachineTypePersistent  = "persistent"
	StateMachineTypeApplication = "application"
)

// StateMachineRegistry manages state machine factories
type StateMachineRegistry struct {
	factories map[string]StateMachineFactory
}

// NewStateMachineRegistry creates a new state machine registry
func NewStateMachineRegistry() *StateMachineRegistry {
	return &StateMachineRegistry{
		factories: make(map[string]StateMachineFactory),
	}
}

// Register registers a state machine factory
func (r *StateMachineRegistry) Register(factory StateMachineFactory) error {
	r.factories[factory.Name()] = factory
	return nil
}

// Create creates a state machine by type
func (r *StateMachineRegistry) Create(config StateMachineConfig) (StateMachine, error) {
	factory, exists := r.factories[config.Type]
	if !exists {
		return nil, &StateMachineError{
			Code:    "factory_not_found",
			Message: "state machine factory not found: " + config.Type,
		}
	}

	return factory.Create(config)
}

// GetFactory returns a factory by name
func (r *StateMachineRegistry) GetFactory(name string) (StateMachineFactory, bool) {
	factory, exists := r.factories[name]
	return factory, exists
}

// GetFactories returns all registered factories
func (r *StateMachineRegistry) GetFactories() map[string]StateMachineFactory {
	factories := make(map[string]StateMachineFactory)
	for name, factory := range r.factories {
		factories[name] = factory
	}
	return factories
}

// StateMachineError represents a state machine error
type StateMachineError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Cause   error  `json:"cause,omitempty"`
}

func (e *StateMachineError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *StateMachineError) Unwrap() error {
	return e.Cause
}

// Common error codes
const (
	ErrCodeInvalidState    = "invalid_state"
	ErrCodeInvalidCommand  = "invalid_command"
	ErrCodeHandlerNotFound = "handler_not_found"
	ErrCodeSnapshotFailed  = "snapshot_failed"
	ErrCodeRestoreFailed   = "restore_failed"
	ErrCodeFactoryNotFound = "factory_not_found"
)
