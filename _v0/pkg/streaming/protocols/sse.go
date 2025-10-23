package protocols

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	streamingcore "github.com/xraph/forge/v0/pkg/streaming/core"
)

// SSEHandler implements the ProtocolHandler interface for Server-Sent Events
type SSEHandler struct {
	logger  common.Logger
	metrics common.Metrics
	config  SSEConfig
}

// SSEConfig contains SSE-specific configuration
type SSEConfig struct {
	CheckOrigin       bool          `yaml:"check_origin" default:"false"`
	AllowedOrigins    []string      `yaml:"allowed_origins"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval" default:"30s"`
	BufferSize        int           `yaml:"buffer_size" default:"1024"`
	FlushInterval     time.Duration `yaml:"flush_interval" default:"1s"`
	MaxEventSize      int64         `yaml:"max_event_size" default:"32768"`
	EnableCompression bool          `yaml:"enable_compression" default:"true"`
	RetryInterval     time.Duration `yaml:"retry_interval" default:"5s"`
}

// DefaultSSEConfig returns default SSE configuration
func DefaultSSEConfig() SSEConfig {
	return SSEConfig{
		CheckOrigin:       false,
		AllowedOrigins:    []string{"*"},
		HeartbeatInterval: 30 * time.Second,
		BufferSize:        1024,
		FlushInterval:     1 * time.Second,
		MaxEventSize:      32768, // 32KB
		EnableCompression: true,
		RetryInterval:     5 * time.Second,
	}
}

// NewSSEHandler creates a new SSE protocol handler
func NewSSEHandler(logger common.Logger, metrics common.Metrics) streamingcore.ProtocolHandler {
	config := DefaultSSEConfig()

	return &SSEHandler{
		logger:  logger,
		metrics: metrics,
		config:  config,
	}
}

// Name returns the protocol handler name
func (h *SSEHandler) Name() string {
	return "sse"
}

// Priority returns the protocol handler priority
func (h *SSEHandler) Priority() int {
	return 80 // Lower priority than WebSocket
}

// CanHandle checks if the request can be handled by this protocol
func (h *SSEHandler) CanHandle(r *http.Request) bool {
	// Check for SSE accept header
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/event-stream") {
		return true
	}

	// Check for specific SSE headers
	if r.Header.Get("Cache-Control") == "no-cache" &&
		r.Header.Get("Connection") == "keep-alive" {
		return true
	}

	// Check URL path or query parameter
	if strings.Contains(r.URL.Path, "/sse") ||
		r.URL.Query().Get("transport") == "sse" {
		return true
	}

	return false
}

// HandleUpgrade handles the SSE connection setup
func (h *SSEHandler) HandleUpgrade(w http.ResponseWriter, r *http.Request) (streamingcore.Connection, error) {
	// Check origin if required
	if h.config.CheckOrigin && !h.checkOrigin(r) {
		return nil, fmt.Errorf("origin not allowed")
	}

	// Extract connection parameters
	userID := r.URL.Query().Get("user_id")
	roomID := r.URL.Query().Get("room_id")
	sessionID := r.URL.Query().Get("session_id")

	if sessionID == "" {
		sessionID = generateSessionID()
	}

	// Set SSE headers
	h.setSSEHeaders(w)

	// Create connection
	connectionID := generateConnectionID("sse")
	config := streamingcore.DefaultConnectionConfig()

	conn := NewSSEConnection(
		connectionID,
		userID,
		roomID,
		sessionID,
		w,
		r,
		config,
		h.config,
		h.logger,
		h.metrics,
	)

	if h.logger != nil {
		h.logger.Info("SSE connection established",
			logger.String("connection_id", connectionID),
			logger.String("user_id", userID),
			logger.String("room_id", roomID),
			logger.String("remote_addr", r.RemoteAddr),
		)
	}

	if h.metrics != nil {
		h.metrics.Counter("streaming.sse.connections.established").Inc()
	}

	return conn, nil
}

// setSSEHeaders sets the necessary headers for SSE
func (h *SSEHandler) setSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Cache-Control")

	if h.config.EnableCompression {
		w.Header().Set("Content-Encoding", "gzip")
	}
}

// checkOrigin checks if the origin is allowed
func (h *SSEHandler) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // Allow requests without Origin header
	}

	// Check wildcard
	for _, allowed := range h.config.AllowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}

	return false
}

// SSEConnection implements the Connection interface for Server-Sent Events
type SSEConnection struct {
	*BaseConnection
	writer     http.ResponseWriter
	request    *http.Request
	flusher    http.Flusher
	sendCh     chan *streamingcore.Message
	heartbeat  *time.Ticker
	closeOnce  sync.Once
	serializer streamingcore.MessageSerializer
	validator  streamingcore.MessageValidator
	sseConfig  SSEConfig
}

// NewSSEConnection creates a new SSE connection
func NewSSEConnection(
	id, userID, roomID, sessionID string,
	w http.ResponseWriter,
	r *http.Request,
	config streamingcore.ConnectionConfig,
	sseConfig SSEConfig,
	l common.Logger,
	metrics common.Metrics,
) streamingcore.Connection {
	base := NewBaseConnection(id, userID, roomID, sessionID, streamingcore.ProtocolSSE, config, l)

	// Check if ResponseWriter supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		// This shouldn't happen with standard HTTP servers, but handle gracefully
		l.Warn("ResponseWriter does not support flushing",
			logger.String("connection_id", id))
	}

	conn := &SSEConnection{
		BaseConnection: base.(*BaseConnection),
		writer:         w,
		request:        r,
		flusher:        flusher,
		sendCh:         make(chan *streamingcore.Message, config.BufferSize),
		serializer:     streamingcore.NewJSONMessageSerializer(),
		validator:      streamingcore.NewDefaultMessageValidator(),
		sseConfig:      sseConfig,
	}

	// Set remote address and user agent
	conn.SetRemoteAddr(r.RemoteAddr)
	conn.SetUserAgent(r.UserAgent())

	// Start event loop
	go conn.eventLoop()

	// Start heartbeat if enabled
	if sseConfig.HeartbeatInterval > 0 {
		conn.startHeartbeat()
	}

	// Mark as connected
	conn.setState(streamingcore.ConnectionStateConnected)

	return conn
}

// Send sends a message through the SSE connection
func (c *SSEConnection) Send(ctx context.Context, message *streamingcore.Message) error {
	if !c.IsAlive() {
		return fmt.Errorf("connection is not alive")
	}

	// Validate message
	if err := c.validator.Validate(message); err != nil {
		return fmt.Errorf("message validation failed: %w", err)
	}

	select {
	case c.sendCh <- message:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.Context().Done():
		return fmt.Errorf("connection closed")
	default:
		return fmt.Errorf("send buffer full")
	}
}

// SendEvent sends a message through the SSE connection
func (c *SSEConnection) SendEvent(ctx context.Context, message streamingcore.Event) error {
	return c.Send(ctx, message.ToMessage())
}

// Close closes the SSE connection
func (c *SSEConnection) Close(ctx context.Context) error {
	c.closeOnce.Do(func() {
		c.setState(streamingcore.ConnectionStateClosing)

		// Stop heartbeat
		if c.heartbeat != nil {
			c.heartbeat.Stop()
		}

		// Close send channel
		close(c.sendCh)

		// Send close event
		c.writeSSEEvent("close", "connection closed", "")

		c.setState(streamingcore.ConnectionStateClosed)
		c.handleClose("normal_closure")
	})

	return nil
}

// eventLoop handles the main event loop for SSE
func (c *SSEConnection) eventLoop() {
	defer func() {
		if r := recover(); r != nil {
			c.handleError(fmt.Errorf("event loop panic: %v", r))
		}
		c.Close(context.Background())
	}()

	// Send initial connection event
	c.writeSSEEvent("connected", "connection established", c.ID())

	for {
		select {
		case message, ok := <-c.sendCh:
			if !ok {
				// Channel closed
				return
			}

			if err := c.writeMessage(message); err != nil {
				c.handleError(fmt.Errorf("failed to write message: %w", err))
				return
			}

		case <-c.Context().Done():
			return
		}
	}
}

// writeMessage writes a message as an SSE event
func (c *SSEConnection) writeMessage(message *streamingcore.Message) error {
	// Serialize message
	data, err := c.serializer.Serialize(message)
	if err != nil {
		return fmt.Errorf("failed to serialize message: %w", err)
	}

	// Check size limit
	if int64(len(data)) > c.sseConfig.MaxEventSize {
		return fmt.Errorf("message too large: %d bytes (max: %d)", len(data), c.sseConfig.MaxEventSize)
	}

	// Write SSE event
	eventType := string(message.Type)
	eventData := string(data)
	eventID := message.ID

	if err := c.writeSSEEvent(eventType, eventData, eventID); err != nil {
		return err
	}

	// Update statistics
	c.updateStats(1, 0, int64(len(data)), 0, 0)

	return nil
}

// writeSSEEvent writes an SSE event to the response
func (c *SSEConnection) writeSSEEvent(eventType, data, id string) error {
	// Build SSE event format
	var event strings.Builder

	if id != "" {
		event.WriteString("id: ")
		event.WriteString(id)
		event.WriteString("\n")
	}

	if eventType != "" {
		event.WriteString("event: ")
		event.WriteString(eventType)
		event.WriteString("\n")
	}

	// Handle multi-line data
	lines := strings.Split(data, "\n")
	for _, line := range lines {
		event.WriteString("data: ")
		event.WriteString(line)
		event.WriteString("\n")
	}

	// Add retry directive if configured
	if c.sseConfig.RetryInterval > 0 {
		event.WriteString("retry: ")
		event.WriteString(fmt.Sprintf("%.0f", c.sseConfig.RetryInterval.Seconds()*1000))
		event.WriteString("\n")
	}

	// End event with double newline
	event.WriteString("\n")

	// Write to response
	if _, err := c.writer.Write([]byte(event.String())); err != nil {
		return fmt.Errorf("failed to write SSE event: %w", err)
	}

	// Flush if flusher is available
	if c.flusher != nil {
		c.flusher.Flush()
	}

	return nil
}

// startHeartbeat starts the heartbeat mechanism
func (c *SSEConnection) startHeartbeat() {
	c.heartbeat = time.NewTicker(c.sseConfig.HeartbeatInterval)

	go func() {
		defer c.heartbeat.Stop()

		for {
			select {
			case <-c.heartbeat.C:
				if err := c.sendHeartbeat(); err != nil {
					c.handleError(fmt.Errorf("heartbeat failed: %w", err))
					return
				}

			case <-c.Context().Done():
				return
			}
		}
	}()
}

// sendHeartbeat sends a heartbeat event
func (c *SSEConnection) sendHeartbeat() error {
	timestamp := time.Now().Format(time.RFC3339)
	return c.writeSSEEvent("heartbeat", timestamp, "")
}

// OnMessage SSE doesn't support receiving messages from client, so these are no-ops
func (c *SSEConnection) OnMessage(handler streamingcore.MessageHandler) {
	// SSE is unidirectional, so we don't handle incoming messages
}

// Additional helper methods specific to SSE

// SendComment sends a comment to keep the connection alive
func (c *SSEConnection) SendComment(comment string) error {
	data := ": " + comment + "\n\n"

	if _, err := c.writer.Write([]byte(data)); err != nil {
		return fmt.Errorf("failed to write SSE comment: %w", err)
	}

	if c.flusher != nil {
		c.flusher.Flush()
	}

	return nil
}

// SendRetry sends a retry directive to the client
func (c *SSEConnection) SendRetry(retryMs int) error {
	data := fmt.Sprintf("retry: %d\n\n", retryMs)

	if _, err := c.writer.Write([]byte(data)); err != nil {
		return fmt.Errorf("failed to write SSE retry: %w", err)
	}

	if c.flusher != nil {
		c.flusher.Flush()
	}

	return nil
}

// GetLastEventID gets the last event ID from the request headers
func (c *SSEConnection) GetLastEventID() string {
	return c.request.Header.Get("Last-Event-ID")
}

// SSE Helper functions

// SSEEventBuilder helps build SSE events
type SSEEventBuilder struct {
	id       string
	event    string
	data     []string
	retry    int
	comments []string
}

// NewSSEEventBuilder creates a new SSE event builder
func NewSSEEventBuilder() *SSEEventBuilder {
	return &SSEEventBuilder{
		data:     make([]string, 0),
		comments: make([]string, 0),
	}
}

// ID sets the event ID
func (b *SSEEventBuilder) ID(id string) *SSEEventBuilder {
	b.id = id
	return b
}

// Event sets the event type
func (b *SSEEventBuilder) Event(event string) *SSEEventBuilder {
	b.event = event
	return b
}

// Data adds a data line
func (b *SSEEventBuilder) Data(data string) *SSEEventBuilder {
	b.data = append(b.data, data)
	return b
}

// Retry sets the retry interval in milliseconds
func (b *SSEEventBuilder) Retry(ms int) *SSEEventBuilder {
	b.retry = ms
	return b
}

// Comment adds a comment line
func (b *SSEEventBuilder) Comment(comment string) *SSEEventBuilder {
	b.comments = append(b.comments, comment)
	return b
}

// Build builds the SSE event string
func (b *SSEEventBuilder) Build() string {
	var event strings.Builder

	// Add comments
	for _, comment := range b.comments {
		event.WriteString(": ")
		event.WriteString(comment)
		event.WriteString("\n")
	}

	// Add ID
	if b.id != "" {
		event.WriteString("id: ")
		event.WriteString(b.id)
		event.WriteString("\n")
	}

	// Add event type
	if b.event != "" {
		event.WriteString("event: ")
		event.WriteString(b.event)
		event.WriteString("\n")
	}

	// Add data lines
	for _, data := range b.data {
		event.WriteString("data: ")
		event.WriteString(data)
		event.WriteString("\n")
	}

	// Add retry
	if b.retry > 0 {
		event.WriteString("retry: ")
		event.WriteString(fmt.Sprintf("%d", b.retry))
		event.WriteString("\n")
	}

	// End with double newline
	event.WriteString("\n")

	return event.String()
}
