package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/logger"
)

// sseHub manages multiple SSE connections
type sseHub struct {
	streams      map[string]SSEStream
	groups       map[string]map[string]bool // group -> stream IDs
	streamGroups map[string]map[string]bool // stream ID -> groups
	register     chan SSEStream
	unregister   chan SSEStream
	broadcast    chan sseMessage
	broadcastTo  chan sseBroadcastMessage
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	logger       logger.Logger
	config       SSEConfig
	stats        sseStats
	started      bool
}

// sseMessage represents an SSE message
type sseMessage struct {
	event SSEEvent
	data  []byte
}

// sseBroadcastMessage represents a targeted SSE broadcast
type sseBroadcastMessage struct {
	streamIDs []string
	groups    []string
	event     SSEEvent
	data      []byte
}

// sseStats represents SSE statistics
type sseStats struct {
	TotalStreams  int64
	ActiveStreams int
	TotalGroups   int
	MessagesSent  int64
	ErrorCount    int64
	LastError     error
	LastErrorTime time.Time
}

// NewSSEHub creates a new SSE hub
func NewSSEHub(config SSEConfig, logger logger.Logger) *sseHub {
	ctx, cancel := context.WithCancel(context.Background())

	return &sseHub{
		streams:      make(map[string]SSEStream),
		groups:       make(map[string]map[string]bool),
		streamGroups: make(map[string]map[string]bool),
		register:     make(chan SSEStream, 256),
		unregister:   make(chan SSEStream, 256),
		broadcast:    make(chan sseMessage, 256),
		broadcastTo:  make(chan sseBroadcastMessage, 256),
		ctx:          ctx,
		cancel:       cancel,
		logger:       logger,
		config:       config,
		stats:        sseStats{},
	}
}

// Start starts the SSE hub
func (h *sseHub) Start(ctx context.Context) error {
	h.mu.Lock()
	if h.started {
		h.mu.Unlock()
		return fmt.Errorf("SSE hub already started")
	}
	h.started = true
	h.mu.Unlock()

	h.logger.Info("Starting SSE hub")
	go h.run()
	return nil
}

// Stop stops the SSE hub
func (h *sseHub) Stop(ctx context.Context) error {
	h.mu.Lock()
	if !h.started {
		h.mu.Unlock()
		return nil
	}
	h.started = false
	h.mu.Unlock()

	h.logger.Info("Stopping SSE hub")
	h.cancel()

	// Close all streams
	h.mu.RLock()
	for _, stream := range h.streams {
		stream.Close()
	}
	h.mu.RUnlock()

	return nil
}

// run is the main hub loop
func (h *sseHub) run() {
	defer func() {
		if r := recover(); r != nil {
			h.logger.Error("SSE hub panic recovered",
				logger.Any("panic", r),
				logger.Stack("stacktrace"),
			)
		}
	}()

	for {
		select {
		case <-h.ctx.Done():
			return

		case stream := <-h.register:
			h.handleRegister(stream)

		case stream := <-h.unregister:
			h.handleUnregister(stream)

		case msg := <-h.broadcast:
			h.handleBroadcast(msg)

		case msg := <-h.broadcastTo:
			h.handleBroadcastTo(msg)
		}
	}
}

// handleRegister handles stream registration
func (h *sseHub) handleRegister(stream SSEStream) {
	h.mu.Lock()
	h.streams[stream.ID()] = stream
	h.streamGroups[stream.ID()] = make(map[string]bool)
	h.stats.TotalStreams++
	h.stats.ActiveStreams++
	h.mu.Unlock()

	h.logger.Debug("SSE stream registered",
		logger.String("stream_id", stream.ID()),
		logger.String("remote_addr", stream.RemoteAddr()),
	)
}

// handleUnregister handles stream unregistration
func (h *sseHub) handleUnregister(stream SSEStream) {
	h.mu.Lock()
	defer h.mu.Unlock()

	streamID := stream.ID()
	if _, exists := h.streams[streamID]; !exists {
		return
	}

	// Remove from all groups
	if groups, exists := h.streamGroups[streamID]; exists {
		for group := range groups {
			if h.groups[group] != nil {
				delete(h.groups[group], streamID)
				if len(h.groups[group]) == 0 {
					delete(h.groups, group)
				}
			}
		}
		delete(h.streamGroups, streamID)
	}

	// Remove stream
	delete(h.streams, streamID)
	h.stats.ActiveStreams--

	h.logger.Debug("SSE stream unregistered",
		logger.String("stream_id", streamID),
	)
}

// handleBroadcast handles broadcasting to all streams
func (h *sseHub) handleBroadcast(msg sseMessage) {
	h.mu.RLock()
	streams := make([]SSEStream, 0, len(h.streams))
	for _, stream := range h.streams {
		if !stream.IsClosed() {
			streams = append(streams, stream)
		}
	}
	h.mu.RUnlock()

	for _, stream := range streams {
		if err := stream.Send(msg.event); err != nil {
			h.logger.Error("Failed to broadcast SSE message",
				logger.Error(err),
				logger.String("stream_id", stream.ID()),
			)
			h.stats.ErrorCount++
			h.stats.LastError = err
			h.stats.LastErrorTime = time.Now()
		} else {
			h.stats.MessagesSent++
		}
	}
}

// handleBroadcastTo handles targeted broadcasting
func (h *sseHub) handleBroadcastTo(msg sseBroadcastMessage) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	targetStreams := make(map[string]SSEStream)

	// Add streams by ID
	for _, streamID := range msg.streamIDs {
		if stream, exists := h.streams[streamID]; exists && !stream.IsClosed() {
			targetStreams[streamID] = stream
		}
	}

	// Add streams by group
	for _, group := range msg.groups {
		if groupStreams, exists := h.groups[group]; exists {
			for streamID := range groupStreams {
				if stream, exists := h.streams[streamID]; exists && !stream.IsClosed() {
					targetStreams[streamID] = stream
				}
			}
		}
	}

	// Send to target streams
	for _, stream := range targetStreams {
		if err := stream.Send(msg.event); err != nil {
			h.logger.Error("Failed to send targeted SSE message",
				logger.Error(err),
				logger.String("stream_id", stream.ID()),
			)
			h.stats.ErrorCount++
			h.stats.LastError = err
			h.stats.LastErrorTime = time.Now()
		} else {
			h.stats.MessagesSent++
		}
	}
}

// Enhanced SSE stream implementation
type sseStream struct {
	writer         http.ResponseWriter
	flusher        http.Flusher
	id             string
	lastEventID    string
	ctx            context.Context
	cancel         context.CancelFunc
	logger         logger.Logger
	hub            *sseHub
	remoteAddr     string
	headers        http.Header
	closed         bool
	mu             sync.RWMutex
	keepAlive      time.Duration
	keepAliveTimer *time.Timer

	// Configuration
	defaultRetry int
	config       SSEConfig
}

// NewSSEStream creates a new enhanced SSE stream
func NewSSEStream(w http.ResponseWriter, req *http.Request, config SSEConfig, hub *sseHub, logger logger.Logger) SSEStream {
	ctx, cancel := context.WithCancel(req.Context())

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Set CORS headers if configured
	for _, origin := range config.AllowedOrigins {
		if origin == "*" || origin == req.Header.Get("Origin") {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			break
		}
	}

	// Set custom headers
	for key, value := range config.Headers {
		w.Header().Set(key, value)
	}

	// Get Last-Event-ID if present
	lastEventID := req.Header.Get("Last-Event-ID")
	if lastEventID == "" {
		lastEventID = req.URL.Query().Get("lastEventId")
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		logger.Error("ResponseWriter does not support flushing")
	}

	stream := &sseStream{
		writer:       w,
		flusher:      flusher,
		id:           generateConnectionID(),
		lastEventID:  lastEventID,
		ctx:          ctx,
		cancel:       cancel,
		logger:       logger,
		hub:          hub,
		remoteAddr:   req.RemoteAddr,
		headers:      req.Header,
		keepAlive:    config.KeepAliveInterval,
		defaultRetry: config.DefaultRetry,
		config:       config,
	}

	// Start keep-alive if configured
	if stream.keepAlive > 0 {
		stream.startKeepAlive()
	}

	// Register with hub
	if hub != nil {
		hub.register <- stream
	}

	return stream
}

// Basic stream methods
func (s *sseStream) ID() string               { return s.id }
func (s *sseStream) LastEventID() string      { return s.lastEventID }
func (s *sseStream) RemoteAddr() string       { return s.remoteAddr }
func (s *sseStream) Headers() http.Header     { return s.headers }
func (s *sseStream) Context() context.Context { return s.ctx }

func (s *sseStream) IsClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

// Send sends an SSE event
func (s *sseStream) Send(event SSEEvent) error {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return fmt.Errorf("stream closed")
	}
	s.mu.RUnlock()

	// Format event
	eventData := s.formatEvent(event)

	// Write event
	if _, err := s.writer.Write(eventData); err != nil {
		s.logger.Error("Failed to write SSE event",
			logger.Error(err),
			logger.String("stream_id", s.id),
		)
		s.Close()
		return err
	}

	// Flush if possible
	if s.flusher != nil {
		s.flusher.Flush()
	}

	// Update last event ID if provided
	if event.ID != "" {
		s.lastEventID = event.ID
	}

	return nil
}

// SendData sends data as an SSE event
func (s *sseStream) SendData(data string) error {
	return s.Send(SSEEvent{Data: data})
}

// SendJSON sends JSON data as an SSE event
func (s *sseStream) SendJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return s.SendData(string(data))
}

// SendEvent sends a named event with data
func (s *sseStream) SendEvent(name, data string) error {
	return s.Send(SSEEvent{Event: name, Data: data})
}

// SendComment sends a comment (ignored by clients)
func (s *sseStream) SendComment(comment string) error {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return fmt.Errorf("stream closed")
	}
	s.mu.RUnlock()

	// Format comment
	commentData := fmt.Sprintf(": %s\n", comment)

	// Write comment
	if _, err := s.writer.Write([]byte(commentData)); err != nil {
		s.logger.Error("Failed to write SSE comment",
			logger.Error(err),
			logger.String("stream_id", s.id),
		)
		s.Close()
		return err
	}

	// Flush if possible
	if s.flusher != nil {
		s.flusher.Flush()
	}

	return nil
}

// Close closes the SSE stream
func (s *sseStream) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	s.cancel()

	// Stop keep-alive timer
	if s.keepAliveTimer != nil {
		s.keepAliveTimer.Stop()
	}

	// Unregister from hub
	if s.hub != nil {
		s.hub.unregister <- s
	}

	s.logger.Debug("SSE stream closed", logger.String("stream_id", s.id))
	return nil
}

// SetRetry sets the retry time for the stream
func (s *sseStream) SetRetry(milliseconds int) error {
	return s.Send(SSEEvent{Retry: milliseconds})
}

// SetKeepAlive sets the keep-alive interval
func (s *sseStream) SetKeepAlive(interval time.Duration) {
	s.mu.Lock()
	s.keepAlive = interval
	s.mu.Unlock()

	if interval > 0 {
		s.startKeepAlive()
	} else {
		s.stopKeepAlive()
	}
}

// formatEvent formats an SSE event according to the specification
func (s *sseStream) formatEvent(event SSEEvent) []byte {
	var eventData strings.Builder

	// Event ID
	if event.ID != "" {
		eventData.WriteString(fmt.Sprintf("id: %s\n", event.ID))
	}

	// Event type
	if event.Event != "" {
		eventData.WriteString(fmt.Sprintf("event: %s\n", event.Event))
	}

	// Data (can be multiline)
	if event.Data != "" {
		dataLines := strings.Split(event.Data, "\n")
		for _, line := range dataLines {
			eventData.WriteString(fmt.Sprintf("data: %s\n", line))
		}
	}

	// Retry
	if event.Retry > 0 {
		eventData.WriteString(fmt.Sprintf("retry: %d\n", event.Retry))
	}

	// End event with empty line
	eventData.WriteString("\n")

	return []byte(eventData.String())
}

// startKeepAlive starts the keep-alive mechanism
func (s *sseStream) startKeepAlive() {
	s.stopKeepAlive() // Stop existing timer

	if s.keepAlive <= 0 {
		return
	}

	s.keepAliveTimer = time.AfterFunc(s.keepAlive, func() {
		if err := s.SendComment("keep-alive"); err != nil {
			s.logger.Error("Failed to send keep-alive",
				logger.Error(err),
				logger.String("stream_id", s.id),
			)
			s.Close()
			return
		}

		// Schedule next keep-alive
		s.startKeepAlive()
	})
}

// stopKeepAlive stops the keep-alive mechanism
func (s *sseStream) stopKeepAlive() {
	if s.keepAliveTimer != nil {
		s.keepAliveTimer.Stop()
		s.keepAliveTimer = nil
	}
}

// SSE handler wrapper
type sseHandlerWrapper struct {
	handler SSEHandler
	config  SSEConfig
	hub     *sseHub
	logger  logger.Logger
}

// NewSSEHandlerWrapper creates a new SSE handler wrapper
func NewSSEHandlerWrapper(handler SSEHandler, config SSEConfig, logger logger.Logger) http.HandlerFunc {
	hub := NewSSEHub(config, logger)
	hub.Start(context.Background())

	wrapper := &sseHandlerWrapper{
		handler: handler,
		config:  config,
		hub:     hub,
		logger:  logger,
	}

	return wrapper.ServeHTTP
}

// ServeHTTP implements http.HandlerFunc
func (h *sseHandlerWrapper) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Check if request accepts SSE
	if !strings.Contains(req.Header.Get("Accept"), "text/event-stream") {
		http.Error(w, "SSE not supported", http.StatusNotAcceptable)
		return
	}

	// Create SSE stream
	stream := NewSSEStream(w, req, h.config, h.hub, h.logger)
	defer stream.Close()

	// Set default retry if configured
	if h.config.DefaultRetry > 0 {
		if err := stream.SetRetry(h.config.DefaultRetry); err != nil {
			h.logger.Error("Failed to set default retry",
				logger.Error(err),
				logger.String("stream_id", stream.ID()),
			)
		}
	}

	// Handle SSE connection
	if err := h.handler.HandleSSE(req.Context(), stream); err != nil {
		h.logger.Error("SSE handler error",
			logger.Error(err),
			logger.String("stream_id", stream.ID()),
		)
	}
}

// SSE broadcasting utilities
func (h *sseHub) BroadcastEvent(event SSEEvent) error {
	if !h.started {
		return fmt.Errorf("hub not started")
	}

	msg := sseMessage{event: event}

	select {
	case h.broadcast <- msg:
		return nil
	case <-time.After(time.Second):
		return fmt.Errorf("broadcast timeout")
	}
}

func (h *sseHub) BroadcastData(data string) error {
	return h.BroadcastEvent(SSEEvent{Data: data})
}

func (h *sseHub) BroadcastJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return h.BroadcastData(string(data))
}

func (h *sseHub) BroadcastToStreams(streamIDs []string, event SSEEvent) error {
	if !h.started {
		return fmt.Errorf("hub not started")
	}

	msg := sseBroadcastMessage{
		streamIDs: streamIDs,
		event:     event,
	}

	select {
	case h.broadcastTo <- msg:
		return nil
	case <-time.After(time.Second):
		return fmt.Errorf("broadcast timeout")
	}
}

func (h *sseHub) BroadcastToGroups(groups []string, event SSEEvent) error {
	if !h.started {
		return fmt.Errorf("hub not started")
	}

	msg := sseBroadcastMessage{
		groups: groups,
		event:  event,
	}

	select {
	case h.broadcastTo <- msg:
		return nil
	case <-time.After(time.Second):
		return fmt.Errorf("broadcast timeout")
	}
}

// Group management
func (h *sseHub) JoinGroup(streamID, group string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.streams[streamID]; !exists {
		return fmt.Errorf("stream not found: %s", streamID)
	}

	// Add to group
	if h.groups[group] == nil {
		h.groups[group] = make(map[string]bool)
	}
	h.groups[group][streamID] = true

	// Add to stream groups
	h.streamGroups[streamID][group] = true

	h.logger.Debug("Stream joined group",
		logger.String("stream_id", streamID),
		logger.String("group", group),
	)

	return nil
}

func (h *sseHub) LeaveGroup(streamID, group string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.streams[streamID]; !exists {
		return fmt.Errorf("stream not found: %s", streamID)
	}

	// Remove from group
	if h.groups[group] != nil {
		delete(h.groups[group], streamID)
		if len(h.groups[group]) == 0 {
			delete(h.groups, group)
		}
	}

	// Remove from stream groups
	delete(h.streamGroups[streamID], group)

	h.logger.Debug("Stream left group",
		logger.String("stream_id", streamID),
		logger.String("group", group),
	)

	return nil
}

func (h *sseHub) GetStreamGroups(streamID string) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	groups := make([]string, 0)
	if streamGroups, exists := h.streamGroups[streamID]; exists {
		for group := range streamGroups {
			groups = append(groups, group)
		}
	}

	return groups
}

func (h *sseHub) GetGroupMembers(group string) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	members := make([]string, 0)
	if groupMembers, exists := h.groups[group]; exists {
		for streamID := range groupMembers {
			members = append(members, streamID)
		}
	}

	return members
}

// Statistics
func (h *sseHub) StreamCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.streams)
}

func (h *sseHub) GroupCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.groups)
}

func (h *sseHub) Stats() sseStats {
	h.mu.RLock()
	defer h.mu.RUnlock()

	stats := h.stats
	stats.ActiveStreams = len(h.streams)
	stats.TotalGroups = len(h.groups)

	return stats
}

// Default configurations
func DefaultSSEConfig() SSEConfig {
	return SSEConfig{
		MaxConnections:    1000,
		ConnectionTimeout: 0, // No timeout
		KeepAliveInterval: 30 * time.Second,
		DefaultRetry:      3000, // 3 seconds
		AllowedOrigins:    []string{"*"},
		Headers: map[string]string{
			"X-Accel-Buffering": "no", // Disable nginx buffering
		},
	}
}

// Utility functions for SSE event creation
func NewSSEEvent(data string) SSEEvent {
	return SSEEvent{Data: data}
}

func NewSSEEventWithID(id, data string) SSEEvent {
	return SSEEvent{ID: id, Data: data}
}

func NewSSENamedEvent(name, data string) SSEEvent {
	return SSEEvent{Event: name, Data: data}
}

func NewSSEJSONEvent(v interface{}) (SSEEvent, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return SSEEvent{}, fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return SSEEvent{Data: string(data)}, nil
}

func NewSSERetryEvent(milliseconds int) SSEEvent {
	return SSEEvent{Retry: milliseconds}
}

// Simple SSE handler implementation
type simpleSSEHandler struct {
	handlerFunc func(context.Context, SSEStream) error
}

func (h *simpleSSEHandler) HandleSSE(ctx context.Context, stream SSEStream) error {
	return h.handlerFunc(ctx, stream)
}

// NewSimpleSSEHandler creates a simple SSE handler from a function
func NewSimpleSSEHandler(handlerFunc func(context.Context, SSEStream) error) SSEHandler {
	return &simpleSSEHandler{handlerFunc: handlerFunc}
}

// SSE middleware for adding CORS and security headers
func SSEMiddleware(config SSEConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Set SSE headers
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")

			// Set CORS headers
			origin := r.Header.Get("Origin")
			for _, allowedOrigin := range config.AllowedOrigins {
				if allowedOrigin == "*" || allowedOrigin == origin {
					w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
					break
				}
			}

			// Set custom headers
			for key, value := range config.Headers {
				w.Header().Set(key, value)
			}

			next.ServeHTTP(w, r)
		})
	}
}
