package protocols

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	streamingcore "github.com/xraph/forge/pkg/streaming/core"
)

// LongPollingHandler implements the ProtocolHandler interface for long polling
type LongPollingHandler struct {
	logger      common.Logger
	metrics     common.Metrics
	config      LongPollingConfig
	connections map[string]*LongPollingConnection
	mu          sync.RWMutex
}

// LongPollingConfig contains long polling specific configuration
type LongPollingConfig struct {
	CheckOrigin         bool          `yaml:"check_origin" default:"false"`
	AllowedOrigins      []string      `yaml:"allowed_origins"`
	PollTimeout         time.Duration `yaml:"poll_timeout" default:"30s"`
	MaxPollTimeout      time.Duration `yaml:"max_poll_timeout" default:"60s"`
	MessageBufferSize   int           `yaml:"message_buffer_size" default:"100"`
	ConnectionTTL       time.Duration `yaml:"connection_ttl" default:"300s"`
	CleanupInterval     time.Duration `yaml:"cleanup_interval" default:"60s"`
	MaxMessageQueueSize int           `yaml:"max_message_queue_size" default:"1000"`
	EnableGzip          bool          `yaml:"enable_gzip" default:"true"`
	JSONPCallback       string        `yaml:"jsonp_callback" default:"callback"`
}

// DefaultLongPollingConfig returns default long polling configuration
func DefaultLongPollingConfig() LongPollingConfig {
	return LongPollingConfig{
		CheckOrigin:         false,
		AllowedOrigins:      []string{"*"},
		PollTimeout:         30 * time.Second,
		MaxPollTimeout:      60 * time.Second,
		MessageBufferSize:   100,
		ConnectionTTL:       5 * time.Minute,
		CleanupInterval:     60 * time.Second,
		MaxMessageQueueSize: 1000,
		EnableGzip:          true,
		JSONPCallback:       "callback",
	}
}

// NewLongPollingHandler creates a new long polling protocol handler
func NewLongPollingHandler(logger common.Logger, metrics common.Metrics) streamingcore.ProtocolHandler {
	config := DefaultLongPollingConfig()

	handler := &LongPollingHandler{
		logger:      logger,
		metrics:     metrics,
		config:      config,
		connections: make(map[string]*LongPollingConnection),
	}

	// Start cleanup routine
	go handler.cleanupRoutine()

	return handler
}

// Name returns the protocol handler name
func (h *LongPollingHandler) Name() string {
	return "longpolling"
}

// Priority returns the protocol handler priority
func (h *LongPollingHandler) Priority() int {
	return 60 // Lower priority than WebSocket and SSE
}

// CanHandle checks if the request can be handled by this protocol
func (h *LongPollingHandler) CanHandle(r *http.Request) bool {
	// Check for explicit long polling indicators
	if r.URL.Query().Get("transport") == "polling" ||
		r.URL.Query().Get("transport") == "longpolling" {
		return true
	}

	// Check URL path
	if strings.Contains(r.URL.Path, "/poll") ||
		strings.Contains(r.URL.Path, "/polling") {
		return true
	}

	// Check for JSONP callback (common in long polling)
	if r.URL.Query().Get("callback") != "" ||
		r.URL.Query().Get("jsonp") != "" {
		return true
	}

	// Check Accept header for long polling preference
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "application/json") &&
		!strings.Contains(accept, "text/event-stream") {
		return true
	}

	return false
}

// HandleUpgrade handles the long polling connection setup
func (h *LongPollingHandler) HandleUpgrade(w http.ResponseWriter, r *http.Request) (streamingcore.Connection, error) {
	// Check origin if required
	if h.config.CheckOrigin && !h.checkOrigin(r) {
		return nil, fmt.Errorf("origin not allowed")
	}

	// Extract connection parameters
	userID := r.URL.Query().Get("user_id")
	roomID := r.URL.Query().Get("room_id")
	sessionID := r.URL.Query().Get("session_id")
	connectionID := r.URL.Query().Get("connection_id")

	if sessionID == "" {
		sessionID = generateSessionID()
	}

	if connectionID == "" {
		connectionID = generateConnectionID("poll")
	}

	// Check if this is a poll request for existing connection
	if existingConn := h.getConnection(connectionID); existingConn != nil {
		return h.handlePollRequest(w, r, existingConn)
	}

	// Create new connection
	config := streamingcore.DefaultConnectionConfig()
	conn := h.createConnection(connectionID, userID, roomID, sessionID, config)

	// Handle initial request
	if r.Method == "POST" {
		// Handle message sending
		return h.handleSendRequest(w, r, conn)
	} else {
		// Handle polling request
		return h.handlePollRequest(w, r, conn)
	}
}

// createConnection creates a new long polling connection
func (h *LongPollingHandler) createConnection(
	id, userID, roomID, sessionID string,
	config streamingcore.ConnectionConfig,
) *LongPollingConnection {
	h.mu.Lock()
	defer h.mu.Unlock()

	conn := NewLongPollingConnection(
		id, userID, roomID, sessionID,
		config, h.config, h.logger, h.metrics,
	)

	h.connections[id] = conn

	if h.logger != nil {
		h.logger.Info("long polling connection created",
			logger.String("connection_id", id),
			logger.String("user_id", userID),
			logger.String("room_id", roomID),
		)
	}

	if h.metrics != nil {
		h.metrics.Counter("streaming.longpolling.connections.created").Inc()
	}

	return conn
}

// getConnection gets an existing connection
func (h *LongPollingHandler) getConnection(connectionID string) *LongPollingConnection {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.connections[connectionID]
}

// removeConnection removes a connection
func (h *LongPollingHandler) removeConnection(connectionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.connections, connectionID)
}

// handlePollRequest handles a polling request
func (h *LongPollingHandler) handlePollRequest(w http.ResponseWriter, r *http.Request, conn *LongPollingConnection) (streamingcore.Connection, error) {
	// Parse timeout from request
	timeout := h.config.PollTimeout
	if timeoutStr := r.URL.Query().Get("timeout"); timeoutStr != "" {
		if parsedTimeout, err := time.ParseDuration(timeoutStr + "s"); err == nil {
			if parsedTimeout <= h.config.MaxPollTimeout {
				timeout = parsedTimeout
			}
		}
	}

	// Parse last message ID for message continuity
	lastMessageID := r.URL.Query().Get("last_message_id")

	// Set appropriate headers
	h.setPollingHeaders(w, r)

	// Wait for messages or timeout
	messages, err := conn.PollMessages(r.Context(), timeout, lastMessageID)
	if err != nil {
		return nil, fmt.Errorf("failed to poll messages: %w", err)
	}

	// Send response
	if err := h.sendPollResponse(w, r, messages); err != nil {
		return nil, fmt.Errorf("failed to send poll response: %w", err)
	}

	return conn, nil
}

// handleSendRequest handles a message sending request
func (h *LongPollingHandler) handleSendRequest(w http.ResponseWriter, r *http.Request, conn *LongPollingConnection) (streamingcore.Connection, error) {
	// Parse message from request body
	var messageData map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&messageData); err != nil {
		return nil, fmt.Errorf("failed to decode message: %w", err)
	}

	// Create message
	message := &streamingcore.Message{
		ID:        generateMessageID(),
		Type:      streamingcore.MessageType(getStringFromMap(messageData, "type", "text")),
		From:      conn.UserID(),
		RoomID:    conn.RoomID(),
		Data:      messageData["data"],
		Timestamp: time.Now(),
	}

	// Validate and queue message for processing
	if err := conn.QueueOutgoingMessage(message); err != nil {
		return nil, fmt.Errorf("failed to queue message: %w", err)
	}

	// Send acknowledgment
	response := map[string]interface{}{
		"status":     "ok",
		"message_id": message.ID,
		"timestamp":  message.Timestamp.Format(time.RFC3339),
	}

	h.setPollingHeaders(w, r)
	w.WriteHeader(http.StatusOK)
	return conn, json.NewEncoder(w).Encode(response)
}

// setPollingHeaders sets appropriate headers for polling responses
func (h *LongPollingHandler) setPollingHeaders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	// CORS headers
	if origin := r.Header.Get("Origin"); origin != "" && h.isOriginAllowed(origin) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	}

	// Compression
	if h.config.EnableGzip && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
	}
}

// sendPollResponse sends the polling response
func (h *LongPollingHandler) sendPollResponse(w http.ResponseWriter, r *http.Request, messages []*streamingcore.Message) error {
	response := map[string]interface{}{
		"messages":  messages,
		"timestamp": time.Now().Format(time.RFC3339),
	}

	// Handle JSONP if callback is specified
	if callback := r.URL.Query().Get("callback"); callback != "" {
		w.Header().Set("Content-Type", "application/javascript")
		jsonData, err := json.Marshal(response)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "%s(%s);", callback, string(jsonData))
		return err
	}

	return json.NewEncoder(w).Encode(response)
}

// checkOrigin checks if the origin is allowed
func (h *LongPollingHandler) checkOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	return h.isOriginAllowed(origin)
}

// isOriginAllowed checks if an origin is in the allowed list
func (h *LongPollingHandler) isOriginAllowed(origin string) bool {
	if origin == "" {
		return true
	}

	for _, allowed := range h.config.AllowedOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}
	return false
}

// cleanupRoutine periodically cleans up expired connections
func (h *LongPollingHandler) cleanupRoutine() {
	ticker := time.NewTicker(h.config.CleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		h.cleanupExpiredConnections()
	}
}

// cleanupExpiredConnections removes expired connections
func (h *LongPollingHandler) cleanupExpiredConnections() {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	var expiredConnections []string

	for id, conn := range h.connections {
		if now.Sub(conn.GetLastActivity()) > h.config.ConnectionTTL {
			expiredConnections = append(expiredConnections, id)
		}
	}

	for _, id := range expiredConnections {
		if conn := h.connections[id]; conn != nil {
			conn.Close(context.Background())
		}
		delete(h.connections, id)

		if h.logger != nil {
			h.logger.Debug("expired long polling connection removed",
				logger.String("connection_id", id),
			)
		}
	}

	if len(expiredConnections) > 0 && h.metrics != nil {
		h.metrics.Counter("streaming.longpolling.connections.expired").Add(float64(len(expiredConnections)))
	}
}

// LongPollingConnection implements the Connection interface for long polling
type LongPollingConnection struct {
	*BaseConnection
	messageQueue  chan *streamingcore.Message
	outgoingQueue chan *streamingcore.Message
	pollRequests  chan *pollRequest
	lastActivity  time.Time
	lastMessageID string
	serializer    streamingcore.MessageSerializer
	validator     streamingcore.MessageValidator
	pollingConfig LongPollingConfig
	closeOnce     sync.Once
	activePoll    *pollRequest
	activePollMu  sync.RWMutex
}

// pollRequest represents a polling request
type pollRequest struct {
	ctx           context.Context
	responseCh    chan []*streamingcore.Message
	timeout       time.Duration
	lastMessageID string
}

// NewLongPollingConnection creates a new long polling connection
func NewLongPollingConnection(
	id, userID, roomID, sessionID string,
	config streamingcore.ConnectionConfig,
	pollingConfig LongPollingConfig,
	logger common.Logger,
	metrics common.Metrics,
) *LongPollingConnection {
	base := NewBaseConnection(id, userID, roomID, sessionID, streamingcore.ProtocolLongPolling, config, logger)

	conn := &LongPollingConnection{
		BaseConnection: base.(*BaseConnection),
		messageQueue:   make(chan *streamingcore.Message, pollingConfig.MessageBufferSize),
		outgoingQueue:  make(chan *streamingcore.Message, pollingConfig.MessageBufferSize),
		pollRequests:   make(chan *pollRequest, 1),
		lastActivity:   time.Now(),
		serializer:     streamingcore.NewJSONMessageSerializer(),
		validator:      streamingcore.NewDefaultMessageValidator(),
		pollingConfig:  pollingConfig,
	}

	// Start message handler
	go conn.messageHandler()

	// Mark as connected
	conn.setState(streamingcore.ConnectionStateConnected)

	return conn
}

// Send sends a message through the long polling connection
func (c *LongPollingConnection) Send(ctx context.Context, message *streamingcore.Message) error {
	if !c.IsAlive() {
		return fmt.Errorf("connection is not alive")
	}

	// Validate message
	if err := c.validator.Validate(message); err != nil {
		return fmt.Errorf("message validation failed: %w", err)
	}

	select {
	case c.messageQueue <- message:
		c.updateActivity()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.Context().Done():
		return fmt.Errorf("connection closed")
	default:
		return fmt.Errorf("message queue full")
	}
}

// SendEvent sends a message through the long polling connection
func (c *LongPollingConnection) SendEvent(ctx context.Context, message streamingcore.Event) error {
	return c.Send(ctx, message.ToMessage())
}

// Close closes the long polling connection
func (c *LongPollingConnection) Close(ctx context.Context) error {
	c.closeOnce.Do(func() {
		c.setState(streamingcore.ConnectionStateClosing)

		// Close channels
		close(c.messageQueue)
		close(c.outgoingQueue)
		close(c.pollRequests)

		// Cancel any active poll
		c.activePollMu.Lock()
		if c.activePoll != nil && c.activePoll.responseCh != nil {
			close(c.activePoll.responseCh)
		}
		c.activePollMu.Unlock()

		c.setState(streamingcore.ConnectionStateClosed)
		c.handleClose("normal_closure")
	})

	return nil
}

// PollMessages waits for messages or times out
func (c *LongPollingConnection) PollMessages(ctx context.Context, timeout time.Duration, lastMessageID string) ([]*streamingcore.Message, error) {
	if !c.IsAlive() {
		return nil, fmt.Errorf("connection is not alive")
	}

	c.updateActivity()

	// Create poll request
	pollReq := &pollRequest{
		ctx:           ctx,
		responseCh:    make(chan []*streamingcore.Message, 1),
		timeout:       timeout,
		lastMessageID: lastMessageID,
	}

	// Set as active poll
	c.activePollMu.Lock()
	c.activePoll = pollReq
	c.activePollMu.Unlock()

	// Send poll request
	select {
	case c.pollRequests <- pollReq:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.Context().Done():
		return nil, fmt.Errorf("connection closed")
	}

	// Wait for response or timeout
	select {
	case messages := <-pollReq.responseCh:
		return messages, nil
	case <-time.After(timeout):
		return []*streamingcore.Message{}, nil // Empty response on timeout
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.Context().Done():
		return nil, fmt.Errorf("connection closed")
	}
}

// QueueOutgoingMessage queues a message for sending to other connections
func (c *LongPollingConnection) QueueOutgoingMessage(message *streamingcore.Message) error {
	if !c.IsAlive() {
		return fmt.Errorf("connection is not alive")
	}

	select {
	case c.outgoingQueue <- message:
		c.updateActivity()
		// Trigger message handler if needed
		if err := c.handleMessage(message); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("outgoing queue full")
	}
}

// GetLastActivity returns the last activity time
func (c *LongPollingConnection) GetLastActivity() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastActivity
}

// updateActivity updates the last activity time
func (c *LongPollingConnection) updateActivity() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastActivity = time.Now()
}

// messageHandler handles the message processing loop
func (c *LongPollingConnection) messageHandler() {
	defer func() {
		if r := recover(); r != nil {
			c.handleError(fmt.Errorf("message handler panic: %v", r))
		}
	}()

	var messageBuffer []*streamingcore.Message

	for {
		select {
		case message, ok := <-c.messageQueue:
			if !ok {
				return
			}

			// Add to buffer
			messageBuffer = append(messageBuffer, message)
			c.lastMessageID = message.ID

			// Check if we have an active poll to respond to
			c.activePollMu.RLock()
			activePoll := c.activePoll
			c.activePollMu.RUnlock()

			if activePoll != nil {
				// Filter messages based on lastMessageID in poll request
				var filteredMessages []*streamingcore.Message
				if activePoll.lastMessageID == "" {
					filteredMessages = messageBuffer
				} else {
					// Find messages after the last message ID
					found := false
					for _, msg := range messageBuffer {
						if found {
							filteredMessages = append(filteredMessages, msg)
						} else if msg.ID == activePoll.lastMessageID {
							found = true
						}
					}
					if !found {
						filteredMessages = messageBuffer
					}
				}

				// Send response if we have messages
				if len(filteredMessages) > 0 {
					select {
					case activePoll.responseCh <- filteredMessages:
						// Clear the buffer and active poll
						messageBuffer = messageBuffer[:0]
						c.activePollMu.Lock()
						c.activePoll = nil
						c.activePollMu.Unlock()
					default:
					}
				}
			}

			// Limit buffer size
			if len(messageBuffer) > c.pollingConfig.MaxMessageQueueSize {
				// Remove oldest messages
				excess := len(messageBuffer) - c.pollingConfig.MaxMessageQueueSize
				messageBuffer = messageBuffer[excess:]
			}

		case pollReq, ok := <-c.pollRequests:
			if !ok {
				return
			}

			// If we have buffered messages, send them immediately
			if len(messageBuffer) > 0 {
				var filteredMessages []*streamingcore.Message
				if pollReq.lastMessageID == "" {
					filteredMessages = messageBuffer
				} else {
					// Find messages after the last message ID
					found := false
					for _, msg := range messageBuffer {
						if found {
							filteredMessages = append(filteredMessages, msg)
						} else if msg.ID == pollReq.lastMessageID {
							found = true
						}
					}
					if !found {
						filteredMessages = messageBuffer
					}
				}

				if len(filteredMessages) > 0 {
					select {
					case pollReq.responseCh <- filteredMessages:
						messageBuffer = messageBuffer[:0]
					default:
					}
					continue
				}
			}

			// No messages available, wait for new ones
			c.activePollMu.Lock()
			c.activePoll = pollReq
			c.activePollMu.Unlock()

		case <-c.Context().Done():
			return
		}
	}
}

// Helper functions

// getStringFromMap safely gets a string value from a map
func getStringFromMap(m map[string]interface{}, key, defaultValue string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return defaultValue
}

// generateMessageID generates a unique message ID
func generateMessageID() string {
	return fmt.Sprintf("msg-%d-%s", time.Now().UnixNano(), randomString(8))
}
