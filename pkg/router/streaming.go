package router

import (
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/streaming"
)

// StreamingIntegration manages WebSocket and SSE integration with the router
type StreamingIntegration struct {
	router           *ForgeRouter
	streamingManager streaming.StreamingManager
	wsHandlers       map[string]*common.WSHandlerInfo
	sseHandlers      map[string]*common.SSEHandlerInfo
	asyncAPIGen      *AsyncAPIGenerator
	mu               sync.RWMutex
	logger           common.Logger
	metrics          common.Metrics
}

// NewStreamingIntegration creates a new streaming integration
func NewStreamingIntegration(router *ForgeRouter, streamingManager streaming.StreamingManager) *StreamingIntegration {
	si := &StreamingIntegration{
		router:           router,
		streamingManager: streamingManager,
		wsHandlers:       make(map[string]*common.WSHandlerInfo),
		sseHandlers:      make(map[string]*common.SSEHandlerInfo),
		logger:           router.logger,
		metrics:          router.metrics,
	}

	// Initialize AsyncAPI generator
	si.asyncAPIGen = NewAsyncAPIGenerator(router)

	return si
}

// RegisterWebSocket registers a WebSocket handler with proper streaming options
func (si *StreamingIntegration) RegisterWebSocket(path string, handler interface{}, options ...common.StreamingHandlerInfo) error {
	si.mu.Lock()
	defer si.mu.Unlock()

	if si.logger != nil {
		si.logger.Info("Registering WebSocket handler",
			logger.String("path", path),
		)
	}

	// Validate handler signature
	handlerType := reflect.TypeOf(handler)
	if handlerType.Kind() != reflect.Func {
		return common.ErrInvalidConfig("handler", fmt.Errorf("WebSocket handler must be a function"))
	}

	// Create handler info with proper streaming configuration
	info := &common.WSHandlerInfo{
		Path:           path,
		Handler:        handler,
		MessageTypes:   make(map[string]reflect.Type),
		EventTypes:     make(map[string]reflect.Type),
		RegisteredAt:   time.Now(),
		Authentication: false,
	}

	// Apply streaming handler options
	for _, option := range options {
		si.applyStreamingOption(info, option)
	}

	// Extract message types from handler signature
	si.extractMessageTypes(handlerType, info)

	// Create HTTP handler wrapper
	httpHandler := si.createWebSocketWrapper(handler, info)

	// Register with base router
	si.router.adapter.GET(path, httpHandler)

	// Store handler info
	si.wsHandlers[path] = info

	// Generate AsyncAPI documentation
	if err := si.asyncAPIGen.AddWebSocketOperation(path, info); err != nil {
		if si.logger != nil {
			si.logger.Warn("Failed to generate AsyncAPI documentation for WebSocket",
				logger.String("path", path),
				logger.Error(err),
			)
		}
	}

	if si.logger != nil {
		si.logger.Info("WebSocket handler registered",
			logger.String("path", path),
			logger.String("summary", info.Summary),
			logger.Bool("auth", info.Authentication),
		)
	}

	if si.metrics != nil {
		si.metrics.Counter("forge.router.websocket_handlers_registered").Inc()
	}

	return nil
}

// RegisterSSE registers a Server-Sent Events handler with proper streaming options
func (si *StreamingIntegration) RegisterSSE(path string, handler interface{}, options ...common.StreamingHandlerInfo) error {
	si.mu.Lock()
	defer si.mu.Unlock()

	if si.logger != nil {
		si.logger.Info("Registering SSE handler",
			logger.String("path", path),
		)
	}

	// Validate handler signature
	handlerType := reflect.TypeOf(handler)
	if handlerType.Kind() != reflect.Func {
		return common.ErrInvalidConfig("handler", fmt.Errorf("SSE handler must be a function"))
	}

	// Create handler info with proper streaming configuration
	info := &common.SSEHandlerInfo{
		Path:           path,
		Handler:        handler,
		EventTypes:     make(map[string]reflect.Type),
		RegisteredAt:   time.Now(),
		Authentication: false,
	}

	// Apply streaming handler options
	for _, option := range options {
		si.applySSEStreamingOption(info, option)
	}

	// Extract event types from handler signature
	si.extractEventTypes(handlerType, info)

	// Create HTTP handler wrapper
	httpHandler := si.createSSEWrapper(handler, info)

	// Register with base router
	si.router.adapter.GET(path, httpHandler)

	// Store handler info
	si.sseHandlers[path] = info

	// Generate AsyncAPI documentation
	if err := si.asyncAPIGen.AddSSEOperation(path, info); err != nil {
		if si.logger != nil {
			si.logger.Warn("Failed to generate AsyncAPI documentation for SSE",
				logger.String("path", path),
				logger.Error(err),
			)
		}
	}

	if si.logger != nil {
		si.logger.Info("SSE handler registered",
			logger.String("path", path),
			logger.String("summary", info.Summary),
			logger.Bool("auth", info.Authentication),
		)
	}

	if si.metrics != nil {
		si.metrics.Counter("forge.router.sse_handlers_registered").Inc()
	}

	return nil
}

// applyStreamingOption applies a streaming handler option to WebSocket handler info
func (si *StreamingIntegration) applyStreamingOption(info *common.WSHandlerInfo, option common.StreamingHandlerInfo) {
	if option.Summary != "" {
		info.Summary = option.Summary
	}
	if option.Description != "" {
		info.Description = option.Description
	}
	if len(option.Tags) > 0 {
		info.Tags = append(info.Tags, option.Tags...)
	}
	if option.Authentication {
		info.Authentication = option.Authentication
	}
	if option.Protocol != "" {
		// Protocol is implied for WebSocket, but we can store it for documentation
	}
	if len(option.MessageTypes) > 0 {
		for name, msgType := range option.MessageTypes {
			info.MessageTypes[name] = msgType
		}
	}
	if len(option.EventTypes) > 0 {
		for name, eventType := range option.EventTypes {
			info.EventTypes[name] = eventType
		}
	}
}

// applySSEStreamingOption applies a streaming handler option to SSE handler info
func (si *StreamingIntegration) applySSEStreamingOption(info *common.SSEHandlerInfo, option common.StreamingHandlerInfo) {
	if option.Summary != "" {
		info.Summary = option.Summary
	}
	if option.Description != "" {
		info.Description = option.Description
	}
	if len(option.Tags) > 0 {
		info.Tags = append(info.Tags, option.Tags...)
	}
	if option.Authentication {
		info.Authentication = option.Authentication
	}
	if len(option.EventTypes) > 0 {
		for name, eventType := range option.EventTypes {
			info.EventTypes[name] = eventType
		}
	}
}

// GetAsyncAPISpec returns the AsyncAPI specification
func (si *StreamingIntegration) GetAsyncAPISpec() *common.AsyncAPISpec {
	if si.asyncAPIGen == nil {
		return nil
	}
	return si.asyncAPIGen.GetSpec()
}

// GetAsyncAPISpecJSON returns the AsyncAPI specification as JSON
func (si *StreamingIntegration) GetAsyncAPISpecJSON() ([]byte, error) {
	if si.asyncAPIGen == nil {
		return nil, fmt.Errorf("AsyncAPI generator not initialized")
	}
	return si.asyncAPIGen.GetSpecJSON()
}

// GetAsyncAPISpecYAML returns the AsyncAPI specification as YAML
func (si *StreamingIntegration) GetAsyncAPISpecYAML() ([]byte, error) {
	if si.asyncAPIGen == nil {
		return nil, fmt.Errorf("AsyncAPI generator not initialized")
	}
	return si.asyncAPIGen.GetSpecYAML()
}

// createWebSocketWrapper creates an HTTP handler that upgrades to WebSocket
func (si *StreamingIntegration) createWebSocketWrapper(handler interface{}, info *common.WSHandlerInfo) func(http.ResponseWriter, *http.Request) {
	handlerValue := reflect.ValueOf(handler)

	return func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		// Create Forge context
		forgeCtx := common.NewForgeContext(r.Context(), si.router.container, si.logger, si.metrics, si.router.config)
		forgeCtx = forgeCtx.WithRequest(r).WithResponseWriter(w)

		// Update connection statistics
		si.updateWSStats(info.Path, startTime)

		// Check authentication if required
		if info.Authentication {
			if !si.checkAuthentication(forgeCtx) {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				si.updateWSErrorStats(info.Path, fmt.Errorf("authentication failed"))
				return
			}
		}

		// Create WebSocket connection using streaming manager
		conn, err := si.streamingManager.HandleWebSocket(w, r, streaming.DefaultConnectionConfig())
		if err != nil {
			si.updateWSErrorStats(info.Path, err)
			if si.logger != nil {
				si.logger.Error("Failed to upgrade WebSocket connection",
					logger.String("path", info.Path),
					logger.Error(err),
				)
			}
			return
		}

		// Update connection count
		info.ConnectionCount++
		info.LastConnected = time.Now()

		// Call the handler with the connection
		si.callWebSocketHandler(handlerValue, forgeCtx, conn, info)

		// Record metrics
		latency := time.Since(startTime)
		if si.metrics != nil {
			si.metrics.Counter("forge.router.websocket_connections").Inc()
			si.metrics.Histogram("forge.router.websocket_connection_duration").Observe(latency.Seconds())
		}
	}
}

// createSSEWrapper creates an HTTP handler for Server-Sent Events
func (si *StreamingIntegration) createSSEWrapper(handler interface{}, info *common.SSEHandlerInfo) func(http.ResponseWriter, *http.Request) {
	handlerValue := reflect.ValueOf(handler)

	return func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()

		// Create Forge context
		forgeCtx := common.NewForgeContext(r.Context(), si.router.container, si.logger, si.metrics, si.router.config)
		forgeCtx = forgeCtx.WithRequest(r).WithResponseWriter(w)

		// Update connection statistics
		si.updateSSEStats(info.Path, startTime)

		// Check authentication if required
		if info.Authentication {
			if !si.checkAuthentication(forgeCtx) {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				si.updateSSEErrorStats(info.Path, fmt.Errorf("authentication failed"))
				return
			}
		}

		// Create SSE connection using streaming manager
		conn, err := si.streamingManager.HandleSSE(w, r, streaming.DefaultConnectionConfig())
		if err != nil {
			si.updateSSEErrorStats(info.Path, err)
			if si.logger != nil {
				si.logger.Error("Failed to create SSE connection",
					logger.String("path", info.Path),
					logger.Error(err),
				)
			}
			return
		}

		// Update connection count
		info.ConnectionCount++
		info.LastConnected = time.Now()

		// Call the handler with the connection
		si.callSSEHandler(handlerValue, forgeCtx, conn, info)

		// Record metrics
		latency := time.Since(startTime)
		if si.metrics != nil {
			si.metrics.Counter("forge.router.sse_connections").Inc()
			si.metrics.Histogram("forge.router.sse_connection_duration").Observe(latency.Seconds())
		}
	}
}

// callWebSocketHandler calls the WebSocket handler with proper signature
func (si *StreamingIntegration) callWebSocketHandler(handlerValue reflect.Value, ctx common.Context, conn streaming.Connection, info *common.WSHandlerInfo) {
	defer func() {
		if r := recover(); r != nil {
			info.ErrorCount++
			info.LastError = fmt.Errorf("panic in WebSocket handler: %v", r)
			if si.logger != nil {
				si.logger.Error("Panic in WebSocket handler",
					logger.String("path", info.Path),
					logger.String("panic", fmt.Sprintf("%v", r)),
				)
			}
		}
	}()

	// Handle different handler signatures:
	// func(ctx Context, conn Connection) error
	// func(ctx Context, conn Connection, message *Message) error
	// func(conn Connection) error

	handlerType := handlerValue.Type()
	numIn := handlerType.NumIn()

	var args []reflect.Value

	switch numIn {
	case 1:
		// func(conn Connection) error
		args = []reflect.Value{reflect.ValueOf(conn)}
	case 2:
		// func(ctx Context, conn Connection) error
		args = []reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(conn)}
	default:
		info.ErrorCount++
		info.LastError = fmt.Errorf("invalid WebSocket handler signature")
		if si.logger != nil {
			si.logger.Error("Invalid WebSocket handler signature",
				logger.String("path", info.Path),
				logger.Int("params", numIn),
			)
		}
		return
	}

	// Call the handler
	results := handlerValue.Call(args)

	// Handle error result if present
	if len(results) > 0 && !results[0].IsNil() {
		if err, ok := results[0].Interface().(error); ok {
			info.ErrorCount++
			info.LastError = err
			if si.logger != nil {
				si.logger.Error("WebSocket handler error",
					logger.String("path", info.Path),
					logger.Error(err),
				)
			}
		}
	}
}

// callSSEHandler calls the SSE handler with proper signature
func (si *StreamingIntegration) callSSEHandler(handlerValue reflect.Value, ctx common.Context, conn streaming.Connection, info *common.SSEHandlerInfo) {
	defer func() {
		if r := recover(); r != nil {
			info.ErrorCount++
			info.LastError = fmt.Errorf("panic in SSE handler: %v", r)
			if si.logger != nil {
				si.logger.Error("Panic in SSE handler",
					logger.String("path", info.Path),
					logger.String("panic", fmt.Sprintf("%v", r)),
				)
			}
		}
	}()

	// Handle different handler signatures similar to WebSocket
	handlerType := handlerValue.Type()
	numIn := handlerType.NumIn()

	var args []reflect.Value

	switch numIn {
	case 1:
		// func(conn Connection) error
		args = []reflect.Value{reflect.ValueOf(conn)}
	case 2:
		// func(ctx Context, conn Connection) error
		args = []reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(conn)}
	default:
		info.ErrorCount++
		info.LastError = fmt.Errorf("invalid SSE handler signature")
		if si.logger != nil {
			si.logger.Error("Invalid SSE handler signature",
				logger.String("path", info.Path),
				logger.Int("params", numIn),
			)
		}
		return
	}

	// Call the handler
	results := handlerValue.Call(args)

	// Handle error result if present
	if len(results) > 0 && !results[0].IsNil() {
		if err, ok := results[0].Interface().(error); ok {
			info.ErrorCount++
			info.LastError = err
			if si.logger != nil {
				si.logger.Error("SSE handler error",
					logger.String("path", info.Path),
					logger.Error(err),
				)
			}
		}
	}
}

// Helper methods for extracting types and generating documentation
func (si *StreamingIntegration) extractMessageTypes(handlerType reflect.Type, info *common.WSHandlerInfo) {
	// Analyze the handler signature to extract message types
	// This is a simplified implementation - in practice, you'd want more sophisticated type analysis

	// Look for message parameter types
	for i := 0; i < handlerType.NumIn(); i++ {
		paramType := handlerType.In(i)

		// Skip context and connection parameters
		if si.isContextType(paramType) || si.isConnectionType(paramType) {
			continue
		}

		// This could be a message type
		typeName := si.getTypeName(paramType)
		if typeName != "" {
			info.MessageTypes[typeName] = paramType
		}
	}

	// Default message type if none found
	if len(info.MessageTypes) == 0 {
		info.MessageTypes["default"] = reflect.TypeOf(common.WSMessage{})
	}
}

func (si *StreamingIntegration) extractEventTypes(handlerType reflect.Type, info *common.SSEHandlerInfo) {
	// Analyze the handler signature to extract event types
	// This is a simplified implementation

	// Look for event parameter types
	for i := 0; i < handlerType.NumIn(); i++ {
		paramType := handlerType.In(i)

		// Skip context and connection parameters
		if si.isContextType(paramType) || si.isConnectionType(paramType) {
			continue
		}

		// This could be an event type
		typeName := si.getTypeName(paramType)
		if typeName != "" {
			info.EventTypes[typeName] = paramType
		}
	}

	// Default event type if none found
	if len(info.EventTypes) == 0 {
		info.EventTypes["default"] = reflect.TypeOf(common.SSEEvent{})
	}
}

func (si *StreamingIntegration) isContextType(t reflect.Type) bool {
	contextType := reflect.TypeOf((*common.Context)(nil)).Elem()
	return t == contextType || t.Implements(contextType)
}

func (si *StreamingIntegration) isConnectionType(t reflect.Type) bool {
	connectionType := reflect.TypeOf((*streaming.Connection)(nil)).Elem()
	return t == connectionType || t.Implements(connectionType)
}

func (si *StreamingIntegration) getTypeName(t reflect.Type) string {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Name() != "" {
		return t.Name()
	}

	return t.String()
}

func (si *StreamingIntegration) checkAuthentication(ctx common.Context) bool {
	// Basic authentication check - this should be enhanced based on your auth system
	return ctx.IsAuthenticated()
}

// Statistics update methods
func (si *StreamingIntegration) updateWSStats(path string, startTime time.Time) {
	si.mu.Lock()
	defer si.mu.Unlock()
	if info, exists := si.wsHandlers[path]; exists {
		info.CallCount++
		info.LastConnected = startTime
	}
}

func (si *StreamingIntegration) updateWSErrorStats(path string, err error) {
	si.mu.Lock()
	defer si.mu.Unlock()
	if info, exists := si.wsHandlers[path]; exists {
		info.ErrorCount++
		info.LastError = err
	}
}

func (si *StreamingIntegration) updateSSEStats(path string, startTime time.Time) {
	si.mu.Lock()
	defer si.mu.Unlock()
	if info, exists := si.sseHandlers[path]; exists {
		info.CallCount++
		info.LastConnected = startTime
	}
}

func (si *StreamingIntegration) updateSSEErrorStats(path string, err error) {
	si.mu.Lock()
	defer si.mu.Unlock()
	if info, exists := si.sseHandlers[path]; exists {
		info.ErrorCount++
		info.LastError = err
	}
}

// GetWebSocketHandlers returns WebSocket handler statistics
func (si *StreamingIntegration) GetWebSocketHandlers() map[string]*common.WSHandlerInfo {
	si.mu.RLock()
	defer si.mu.RUnlock()

	handlers := make(map[string]*common.WSHandlerInfo)
	for path, info := range si.wsHandlers {
		handlers[path] = info
	}
	return handlers
}

// GetSSEHandlers returns SSE handler statistics
func (si *StreamingIntegration) GetSSEHandlers() map[string]*common.SSEHandlerInfo {
	si.mu.RLock()
	defer si.mu.RUnlock()

	handlers := make(map[string]*common.SSEHandlerInfo)
	for path, info := range si.sseHandlers {
		handlers[path] = info
	}
	return handlers
}
