package statemachine

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// ApplicationStateMachineImpl implements an application-specific state machine
type ApplicationStateMachineImpl struct {
	*PersistentStateMachine
	handlers      map[string]CommandHandler
	queryHandlers map[string]QueryHandler
	middleware    []StateMachineMiddleware
	validators    []CommandValidator
	transformers  []StateTransformer
	metrics       ApplicationMetrics
	logger        common.Logger
	handlersMu    sync.RWMutex
}

// ApplicationStateMachineFactory creates application state machines
type ApplicationStateMachineFactory struct {
	logger  common.Logger
	metrics common.Metrics
}

// NewApplicationStateMachineFactory creates a new application state machine factory
func NewApplicationStateMachineFactory(logger common.Logger, metrics common.Metrics) *ApplicationStateMachineFactory {
	return &ApplicationStateMachineFactory{
		logger:  logger,
		metrics: metrics,
	}
}

// Create creates a new application state machine
func (f *ApplicationStateMachineFactory) Create(config StateMachineConfig) (StateMachine, error) {
	// Create persistent state machine first
	persistentFactory := NewPersistentStateMachineFactory(f.logger, f.metrics)
	persistentSM, err := persistentFactory.Create(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create persistent state machine: %w", err)
	}

	appSM := &ApplicationStateMachineImpl{
		PersistentStateMachine: persistentSM.(*PersistentStateMachine),
		handlers:               make(map[string]CommandHandler),
		queryHandlers:          make(map[string]QueryHandler),
		middleware:             make([]StateMachineMiddleware, 0),
		validators:             make([]CommandValidator, 0),
		transformers:           make([]StateTransformer, 0),
		metrics:                NewApplicationMetrics(f.metrics),
		logger:                 f.logger,
	}

	// Register default command handlers
	appSM.registerDefaultHandlers()

	// Apply configuration
	if err := appSM.applyConfig(config); err != nil {
		return nil, fmt.Errorf("failed to apply configuration: %w", err)
	}

	return appSM, nil
}

// Name returns the factory name
func (f *ApplicationStateMachineFactory) Name() string {
	return StateMachineTypeApplication
}

// Version returns the factory version
func (f *ApplicationStateMachineFactory) Version() string {
	return "1.0.0"
}

// Command executes a command on the state machine
func (asm *ApplicationStateMachineImpl) Command(ctx context.Context, cmd Command) (interface{}, error) {
	start := time.Now()
	defer func() {
		asm.metrics.RecordCommandLatency(cmd.Type, time.Since(start))
	}()

	asm.metrics.IncrementCommands(cmd.Type)

	// Validate command
	if err := asm.validateCommand(cmd); err != nil {
		asm.metrics.IncrementCommandErrors(cmd.Type)
		return nil, fmt.Errorf("command validation failed: %w", err)
	}

	// Apply middleware
	for _, middleware := range asm.middleware {
		if err := middleware.PreCommand(ctx, cmd); err != nil {
			asm.metrics.IncrementCommandErrors(cmd.Type)
			return nil, fmt.Errorf("middleware pre-command failed: %w", err)
		}
	}

	// Get handler
	asm.handlersMu.RLock()
	handler, exists := asm.handlers[cmd.Type]
	asm.handlersMu.RUnlock()

	if !exists {
		asm.metrics.IncrementCommandErrors(cmd.Type)
		return nil, &StateMachineError{
			Code:    ErrCodeHandlerNotFound,
			Message: fmt.Sprintf("command handler not found for type: %s", cmd.Type),
		}
	}

	// Execute command
	state := asm.PersistentStateMachine.GetState()
	result, err := handler(ctx, state, cmd)
	if err != nil {
		asm.metrics.IncrementCommandErrors(cmd.Type)

		// Apply middleware error handling
		for _, middleware := range asm.middleware {
			middleware.OnError(ctx, cmd, err)
		}

		return nil, fmt.Errorf("command execution failed: %w", err)
	}

	// Apply middleware post-processing
	for _, middleware := range asm.middleware {
		if err := middleware.PostCommand(ctx, cmd, result); err != nil {
			asm.metrics.IncrementCommandErrors(cmd.Type)
			return nil, fmt.Errorf("middleware post-command failed: %w", err)
		}
	}

	return result, nil
}

// Query performs a read-only query on the state machine
func (asm *ApplicationStateMachineImpl) Query(ctx context.Context, query Query) (interface{}, error) {
	start := time.Now()
	defer func() {
		asm.metrics.RecordQueryLatency(query.Type, time.Since(start))
	}()

	asm.metrics.IncrementQueries(query.Type)

	// Get query handler
	asm.handlersMu.RLock()
	handler, exists := asm.queryHandlers[query.Type]
	asm.handlersMu.RUnlock()

	if !exists {
		asm.metrics.IncrementQueryErrors(query.Type)
		return nil, &StateMachineError{
			Code:    ErrCodeHandlerNotFound,
			Message: fmt.Sprintf("query handler not found for type: %s", query.Type),
		}
	}

	// Execute query
	state := asm.PersistentStateMachine.GetState()
	result, err := handler(ctx, state, query)
	if err != nil {
		asm.metrics.IncrementQueryErrors(query.Type)
		return nil, fmt.Errorf("query execution failed: %w", err)
	}

	return result, nil
}

// RegisterHandler registers a command handler
func (asm *ApplicationStateMachineImpl) RegisterHandler(cmdType string, handler CommandHandler) error {
	asm.handlersMu.Lock()
	defer asm.handlersMu.Unlock()

	if _, exists := asm.handlers[cmdType]; exists {
		return fmt.Errorf("command handler already exists for type: %s", cmdType)
	}

	asm.handlers[cmdType] = handler

	if asm.logger != nil {
		asm.logger.Info("registered command handler",
			logger.String("type", cmdType),
		)
	}

	return nil
}

// UnregisterHandler unregisters a command handler
func (asm *ApplicationStateMachineImpl) UnregisterHandler(cmdType string) error {
	asm.handlersMu.Lock()
	defer asm.handlersMu.Unlock()

	if _, exists := asm.handlers[cmdType]; !exists {
		return fmt.Errorf("command handler not found for type: %s", cmdType)
	}

	delete(asm.handlers, cmdType)

	if asm.logger != nil {
		asm.logger.Info("unregistered command handler",
			logger.String("type", cmdType),
		)
	}

	return nil
}

// GetHandlers returns all registered command handlers
func (asm *ApplicationStateMachineImpl) GetHandlers() map[string]CommandHandler {
	asm.handlersMu.RLock()
	defer asm.handlersMu.RUnlock()

	handlers := make(map[string]CommandHandler)
	for cmdType, handler := range asm.handlers {
		handlers[cmdType] = handler
	}

	return handlers
}

// RegisterQueryHandler registers a query handler
func (asm *ApplicationStateMachineImpl) RegisterQueryHandler(queryType string, handler QueryHandler) error {
	asm.handlersMu.Lock()
	defer asm.handlersMu.Unlock()

	if _, exists := asm.queryHandlers[queryType]; exists {
		return fmt.Errorf("query handler already exists for type: %s", queryType)
	}

	asm.queryHandlers[queryType] = handler

	if asm.logger != nil {
		asm.logger.Info("registered query handler",
			logger.String("type", queryType),
		)
	}

	return nil
}

// UnregisterQueryHandler unregisters a query handler
func (asm *ApplicationStateMachineImpl) UnregisterQueryHandler(queryType string) error {
	asm.handlersMu.Lock()
	defer asm.handlersMu.Unlock()

	if _, exists := asm.queryHandlers[queryType]; !exists {
		return fmt.Errorf("query handler not found for type: %s", queryType)
	}

	delete(asm.queryHandlers, queryType)

	if asm.logger != nil {
		asm.logger.Info("unregistered query handler",
			logger.String("type", queryType),
		)
	}

	return nil
}

// AddMiddleware adds middleware to the state machine
func (asm *ApplicationStateMachineImpl) AddMiddleware(middleware StateMachineMiddleware) {
	asm.middleware = append(asm.middleware, middleware)
}

// AddValidator adds a command validator
func (asm *ApplicationStateMachineImpl) AddValidator(validator CommandValidator) {
	asm.validators = append(asm.validators, validator)
}

// AddTransformer adds a state transformer
func (asm *ApplicationStateMachineImpl) AddTransformer(transformer StateTransformer) {
	asm.transformers = append(asm.transformers, transformer)
}

// GetMetrics returns application metrics
func (asm *ApplicationStateMachineImpl) GetMetrics() ApplicationMetrics {
	return asm.metrics
}

// applyConfig applies configuration to the state machine
func (asm *ApplicationStateMachineImpl) applyConfig(config StateMachineConfig) error {
	// Apply handlers from config
	if handlers, ok := config.Options["handlers"].(map[string]interface{}); ok {
		for cmdType, handlerConfig := range handlers {
			if err := asm.registerHandlerFromConfig(cmdType, handlerConfig); err != nil {
				return fmt.Errorf("failed to register handler %s: %w", cmdType, err)
			}
		}
	}

	// Apply middleware from config
	if middleware, ok := config.Options["middleware"].([]interface{}); ok {
		for _, mwConfig := range middleware {
			if err := asm.addMiddlewareFromConfig(mwConfig); err != nil {
				return fmt.Errorf("failed to add middleware: %w", err)
			}
		}
	}

	return nil
}

// registerDefaultHandlers registers default command handlers
func (asm *ApplicationStateMachineImpl) registerDefaultHandlers() {
	// Register default set command handler
	asm.RegisterHandler("set", func(ctx context.Context, state interface{}, cmd Command) (interface{}, error) {
		data, ok := cmd.Data.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid set command data")
		}

		key, ok := data["key"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid key in set command")
		}

		value := data["value"]

		// Apply to underlying state machine
		if err := asm.PersistentStateMachine.Set(key, value); err != nil {
			return nil, err
		}

		return map[string]interface{}{
			"key":     key,
			"value":   value,
			"success": true,
		}, nil
	})

	// Register default delete command handler
	asm.RegisterHandler("delete", func(ctx context.Context, state interface{}, cmd Command) (interface{}, error) {
		data, ok := cmd.Data.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid delete command data")
		}

		key, ok := data["key"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid key in delete command")
		}

		// Apply to underlying state machine
		if err := asm.PersistentStateMachine.Delete(key); err != nil {
			return nil, err
		}

		return map[string]interface{}{
			"key":     key,
			"success": true,
		}, nil
	})

	// Register default get query handler
	asm.RegisterQueryHandler("get", func(ctx context.Context, state interface{}, query Query) (interface{}, error) {
		data, ok := query.Data.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid get query data")
		}

		key, ok := data["key"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid key in get query")
		}

		value, exists := asm.PersistentStateMachine.Get(key)
		return map[string]interface{}{
			"key":    key,
			"value":  value,
			"exists": exists,
		}, nil
	})

	// Register default list query handler
	asm.RegisterQueryHandler("list", func(ctx context.Context, state interface{}, query Query) (interface{}, error) {
		keys := asm.PersistentStateMachine.Keys()
		count := asm.PersistentStateMachine.Count()

		return map[string]interface{}{
			"keys":  keys,
			"count": count,
		}, nil
	})
}

// validateCommand validates a command
func (asm *ApplicationStateMachineImpl) validateCommand(cmd Command) error {
	for _, validator := range asm.validators {
		if err := validator.Validate(cmd); err != nil {
			return err
		}
	}
	return nil
}

// registerHandlerFromConfig registers a handler from configuration
func (asm *ApplicationStateMachineImpl) registerHandlerFromConfig(cmdType string, config interface{}) error {
	// This is a simplified implementation
	// In practice, you would create handlers based on configuration
	return nil
}

// addMiddlewareFromConfig adds middleware from configuration
func (asm *ApplicationStateMachineImpl) addMiddlewareFromConfig(config interface{}) error {
	// This is a simplified implementation
	// In practice, you would create middleware based on configuration
	return nil
}

// Interfaces and types for application state machine

// StateMachineMiddleware provides middleware functionality
type StateMachineMiddleware interface {
	PreCommand(ctx context.Context, cmd Command) error
	PostCommand(ctx context.Context, cmd Command, result interface{}) error
	OnError(ctx context.Context, cmd Command, err error)
}

// CommandValidator validates commands
type CommandValidator interface {
	Validate(cmd Command) error
}

// StateTransformer transforms state during operations
type StateTransformer interface {
	Transform(oldState, newState interface{}) interface{}
}

// ApplicationMetrics tracks application-specific metrics
type ApplicationMetrics interface {
	IncrementCommands(cmdType string)
	IncrementQueries(queryType string)
	IncrementCommandErrors(cmdType string)
	IncrementQueryErrors(queryType string)
	RecordCommandLatency(cmdType string, duration time.Duration)
	RecordQueryLatency(queryType string, duration time.Duration)
	GetCommandStats() map[string]CommandStats
	GetQueryStats() map[string]QueryStats
}

// CommandStats contains statistics for a command type
type CommandStats struct {
	Type           string        `json:"type"`
	Count          int64         `json:"count"`
	ErrorCount     int64         `json:"error_count"`
	TotalLatency   time.Duration `json:"total_latency"`
	AverageLatency time.Duration `json:"average_latency"`
	MinLatency     time.Duration `json:"min_latency"`
	MaxLatency     time.Duration `json:"max_latency"`
	LastExecuted   time.Time     `json:"last_executed"`
}

// QueryStats contains statistics for a query type
type QueryStats struct {
	Type           string        `json:"type"`
	Count          int64         `json:"count"`
	ErrorCount     int64         `json:"error_count"`
	TotalLatency   time.Duration `json:"total_latency"`
	AverageLatency time.Duration `json:"average_latency"`
	MinLatency     time.Duration `json:"min_latency"`
	MaxLatency     time.Duration `json:"max_latency"`
	LastExecuted   time.Time     `json:"last_executed"`
}

// applicationMetrics implements ApplicationMetrics
type applicationMetrics struct {
	commandStats map[string]*CommandStats
	queryStats   map[string]*QueryStats
	metrics      common.Metrics
	mu           sync.RWMutex
}

// NewApplicationMetrics creates new application metrics
func NewApplicationMetrics(metrics common.Metrics) ApplicationMetrics {
	return &applicationMetrics{
		commandStats: make(map[string]*CommandStats),
		queryStats:   make(map[string]*QueryStats),
		metrics:      metrics,
	}
}

// IncrementCommands increments command count
func (am *applicationMetrics) IncrementCommands(cmdType string) {
	am.mu.Lock()
	defer am.mu.Unlock()

	if stats, exists := am.commandStats[cmdType]; exists {
		stats.Count++
		stats.LastExecuted = time.Now()
	} else {
		am.commandStats[cmdType] = &CommandStats{
			Type:         cmdType,
			Count:        1,
			LastExecuted: time.Now(),
			MinLatency:   time.Hour,
		}
	}

	if am.metrics != nil {
		am.metrics.Counter("forge.consensus.statemachine.commands", "type", cmdType).Inc()
	}
}

// IncrementQueries increments query count
func (am *applicationMetrics) IncrementQueries(queryType string) {
	am.mu.Lock()
	defer am.mu.Unlock()

	if stats, exists := am.queryStats[queryType]; exists {
		stats.Count++
		stats.LastExecuted = time.Now()
	} else {
		am.queryStats[queryType] = &QueryStats{
			Type:         queryType,
			Count:        1,
			LastExecuted: time.Now(),
			MinLatency:   time.Hour,
		}
	}

	if am.metrics != nil {
		am.metrics.Counter("forge.consensus.statemachine.queries", "type", queryType).Inc()
	}
}

// IncrementCommandErrors increments command error count
func (am *applicationMetrics) IncrementCommandErrors(cmdType string) {
	am.mu.Lock()
	defer am.mu.Unlock()

	if stats, exists := am.commandStats[cmdType]; exists {
		stats.ErrorCount++
	}

	if am.metrics != nil {
		am.metrics.Counter("forge.consensus.statemachine.command_errors", "type", cmdType).Inc()
	}
}

// IncrementQueryErrors increments query error count
func (am *applicationMetrics) IncrementQueryErrors(queryType string) {
	am.mu.Lock()
	defer am.mu.Unlock()

	if stats, exists := am.queryStats[queryType]; exists {
		stats.ErrorCount++
	}

	if am.metrics != nil {
		am.metrics.Counter("forge.consensus.statemachine.query_errors", "type", queryType).Inc()
	}
}

// RecordCommandLatency records command execution latency
func (am *applicationMetrics) RecordCommandLatency(cmdType string, duration time.Duration) {
	am.mu.Lock()
	defer am.mu.Unlock()

	if stats, exists := am.commandStats[cmdType]; exists {
		stats.TotalLatency += duration
		stats.AverageLatency = stats.TotalLatency / time.Duration(stats.Count)

		if duration < stats.MinLatency {
			stats.MinLatency = duration
		}
		if duration > stats.MaxLatency {
			stats.MaxLatency = duration
		}
	}

	if am.metrics != nil {
		am.metrics.Histogram("forge.consensus.statemachine.command_duration", "type", cmdType).Observe(duration.Seconds())
	}
}

// RecordQueryLatency records query execution latency
func (am *applicationMetrics) RecordQueryLatency(queryType string, duration time.Duration) {
	am.mu.Lock()
	defer am.mu.Unlock()

	if stats, exists := am.queryStats[queryType]; exists {
		stats.TotalLatency += duration
		stats.AverageLatency = stats.TotalLatency / time.Duration(stats.Count)

		if duration < stats.MinLatency {
			stats.MinLatency = duration
		}
		if duration > stats.MaxLatency {
			stats.MaxLatency = duration
		}
	}

	if am.metrics != nil {
		am.metrics.Histogram("forge.consensus.statemachine.query_duration", "type", queryType).Observe(duration.Seconds())
	}
}

// GetCommandStats returns command statistics
func (am *applicationMetrics) GetCommandStats() map[string]CommandStats {
	am.mu.RLock()
	defer am.mu.RUnlock()

	stats := make(map[string]CommandStats)
	for cmdType, cmdStats := range am.commandStats {
		stats[cmdType] = *cmdStats
	}

	return stats
}

// GetQueryStats returns query statistics
func (am *applicationMetrics) GetQueryStats() map[string]QueryStats {
	am.mu.RLock()
	defer am.mu.RUnlock()

	stats := make(map[string]QueryStats)
	for queryType, queryStats := range am.queryStats {
		stats[queryType] = *queryStats
	}

	return stats
}

// Basic middleware implementations

// LoggingMiddleware logs command execution
type LoggingMiddleware struct {
	logger common.Logger
}

// NewLoggingMiddleware creates a new logging middleware
func NewLoggingMiddleware(logger common.Logger) *LoggingMiddleware {
	return &LoggingMiddleware{logger: logger}
}

// PreCommand logs before command execution
func (lm *LoggingMiddleware) PreCommand(ctx context.Context, cmd Command) error {
	if lm.logger != nil {
		lm.logger.Info("executing command",
			logger.String("type", cmd.Type),
			logger.Time("timestamp", cmd.Timestamp),
		)
	}
	return nil
}

// PostCommand logs after command execution
func (lm *LoggingMiddleware) PostCommand(ctx context.Context, cmd Command, result interface{}) error {
	if lm.logger != nil {
		lm.logger.Info("command executed successfully",
			logger.String("type", cmd.Type),
			logger.String("result_type", reflect.TypeOf(result).String()),
		)
	}
	return nil
}

// OnError logs command errors
func (lm *LoggingMiddleware) OnError(ctx context.Context, cmd Command, err error) {
	if lm.logger != nil {
		lm.logger.Error("command execution failed",
			logger.String("type", cmd.Type),
			logger.Error(err),
		)
	}
}

// BasicCommandValidator validates basic command structure
type BasicCommandValidator struct{}

// NewBasicCommandValidator creates a new basic command validator
func NewBasicCommandValidator() *BasicCommandValidator {
	return &BasicCommandValidator{}
}

// Validate validates a command
func (bcv *BasicCommandValidator) Validate(cmd Command) error {
	if cmd.Type == "" {
		return fmt.Errorf("command type is required")
	}

	if cmd.Timestamp.IsZero() {
		return fmt.Errorf("command timestamp is required")
	}

	return nil
}
