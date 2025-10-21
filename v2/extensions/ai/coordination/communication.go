package coordination

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v2"
	"github.com/xraph/forge/v2/internal/logger"
)

// CommunicationProtocol defines the communication protocol
type CommunicationProtocol string

const (
	CommunicationProtocolDirect      CommunicationProtocol = "direct"      // Direct method calls
	CommunicationProtocolMessage     CommunicationProtocol = "message"     // Message passing
	CommunicationProtocolEventBus    CommunicationProtocol = "event_bus"   // Event-based communication
	CommunicationProtocolDistributed CommunicationProtocol = "distributed" // Distributed messaging
)

// MessageType defines different types of messages
type MessageType string

const (
	MessageTypeRequest      MessageType = "request"      // Request for action
	MessageTypeResponse     MessageType = "response"     // Response to request
	MessageTypeBroadcast    MessageType = "broadcast"    // Broadcast message
	MessageTypeNotification MessageType = "notification" // Notification message
	MessageTypeHeartbeat    MessageType = "heartbeat"    // Heartbeat/ping message
	MessageTypeCommand      MessageType = "command"      // Command message
	MessageTypeStatus       MessageType = "status"       // Status update
	MessageTypeAlert        MessageType = "alert"        // Alert message
)

// MessagePriority defines message priorities
type MessagePriority int

const (
	MessagePriorityLow    MessagePriority = 1
	MessagePriorityNormal MessagePriority = 5
	MessagePriorityHigh   MessagePriority = 8
	MessagePrityCritical  MessagePriority = 10
)

// AgentMessage represents a message between agents
type AgentMessage struct {
	ID         string                 `json:"id"`
	Type       MessageType            `json:"type"`
	From       string                 `json:"from"`
	To         string                 `json:"to"` // Empty for broadcasts
	Subject    string                 `json:"subject"`
	Payload    interface{}            `json:"payload"`
	Priority   MessagePriority        `json:"priority"`
	Timestamp  time.Time              `json:"timestamp"`
	ExpiresAt  time.Time              `json:"expires_at"`
	ResponseTo string                 `json:"response_to,omitempty"`
	Metadata   map[string]interface{} `json:"metadata"`
	Encrypted  bool                   `json:"encrypted"`
	Compressed bool                   `json:"compressed"`
	Retries    int                    `json:"retries"`
	MaxRetries int                    `json:"max_retries"`
}

// MessageHandler defines a function that handles incoming messages
type MessageHandler func(ctx context.Context, message *AgentMessage) (*AgentMessage, error)

// CommunicationConfig contains configuration for agent communication
type CommunicationConfig struct {
	Protocol           CommunicationProtocol `yaml:"protocol" default:"message"`
	MaxRetries         int                   `yaml:"max_retries" default:"3"`
	RetryDelay         time.Duration         `yaml:"retry_delay" default:"1s"`
	MessageTimeout     time.Duration         `yaml:"message_timeout" default:"30s"`
	HeartbeatInterval  time.Duration         `yaml:"heartbeat_interval" default:"30s"`
	MaxMessageSize     int64                 `yaml:"max_message_size" default:"1048576"` // 1MB
	EnableEncryption   bool                  `yaml:"enable_encryption" default:"false"`
	EnableCompression  bool                  `yaml:"enable_compression" default:"false"`
	BufferSize         int                   `yaml:"buffer_size" default:"1000"`
	WorkerCount        int                   `yaml:"worker_count" default:"10"`
	EnableMessageQueue bool                  `yaml:"enable_message_queue" default:"true"`
	QueueCapacity      int                   `yaml:"queue_capacity" default:"10000"`
}

// CommunicationStats contains statistics about agent communication
type CommunicationStats struct {
	TotalMessagesSent      int64                     `json:"total_messages_sent"`
	TotalMessagesReceived  int64                     `json:"total_messages_received"`
	TotalMessagesProcessed int64                     `json:"total_messages_processed"`
	MessagesInQueue        int64                     `json:"messages_in_queue"`
	MessagesByType         map[MessageType]int64     `json:"messages_by_type"`
	MessagesByPriority     map[MessagePriority]int64 `json:"messages_by_priority"`
	AverageLatency         time.Duration             `json:"average_latency"`
	FailedMessages         int64                     `json:"failed_messages"`
	Retransmissions        int64                     `json:"retransmissions"`
	ActiveConnections      int                       `json:"active_connections"`
	AgentStats             map[string]AgentCommStats `json:"agent_stats"`
	LastUpdated            time.Time                 `json:"last_updated"`
}

// AgentCommStats contains communication statistics for a specific agent
type AgentCommStats struct {
	AgentID           string        `json:"agent_id"`
	MessagesSent      int64         `json:"messages_sent"`
	MessagesReceived  int64         `json:"messages_received"`
	AverageLatency    time.Duration `json:"average_latency"`
	LastCommunication time.Time     `json:"last_communication"`
	IsOnline          bool          `json:"is_online"`
	FailureCount      int64         `json:"failure_count"`
}

// CommunicationManager manages inter-agent communication
type CommunicationManager struct {
	config        CommunicationConfig
	protocol      CommunicationProtocol
	agents        map[string]*AgentCommunicator
	messageQueue  chan *QueuedMessage
	handlers      map[MessageType]MessageHandler
	subscriptions map[string][]string // agent_id -> message types
	stats         CommunicationStats
	logger        forge.Logger
	started       bool
	mu            sync.RWMutex
}

// QueuedMessage represents a message in the processing queue
type QueuedMessage struct {
	Message   *AgentMessage
	Timestamp time.Time
	Retries   int
}

// AgentCommunicator handles communication for a specific agent
type AgentCommunicator struct {
	agentID       string
	manager       *CommunicationManager
	messageBuffer chan *AgentMessage
	handlers      map[MessageType]MessageHandler
	stats         AgentCommStats
	lastHeartbeat time.Time
	isOnline      bool
	mu            sync.RWMutex
}

// NewCommunicationManager creates a new communication manager
func NewCommunicationManager(protocol CommunicationProtocol, logger logger.Logger) *CommunicationManager {
	return &CommunicationManager{
		config: CommunicationConfig{
			Protocol:          protocol,
			MaxRetries:        3,
			RetryDelay:        time.Second,
			MessageTimeout:    30 * time.Second,
			HeartbeatInterval: 30 * time.Second,
			MaxMessageSize:    1024 * 1024, // 1MB
			BufferSize:        1000,
			WorkerCount:       10,
			QueueCapacity:     10000,
		},
		protocol:      protocol,
		agents:        make(map[string]*AgentCommunicator),
		messageQueue:  make(chan *QueuedMessage, 10000),
		handlers:      make(map[MessageType]MessageHandler),
		subscriptions: make(map[string][]string),
		stats: CommunicationStats{
			MessagesByType:     make(map[MessageType]int64),
			MessagesByPriority: make(map[MessagePriority]int64),
			AgentStats:         make(map[string]AgentCommStats),
		},
		logger: logger,
	}
}

// Start starts the communication manager
func (cm *CommunicationManager) Start(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.started {
		return fmt.Errorf("communication manager already started")
	}

	// Start message processing workers
	for i := 0; i < cm.config.WorkerCount; i++ {
		go cm.messageWorker(ctx)
	}

	// Start heartbeat monitor
	go cm.heartbeatMonitor(ctx)

	// Start statistics collection
	go cm.statisticsCollector(ctx)

	cm.started = true

	if cm.logger != nil {
		cm.logger.Info("communication manager started",
			logger.String("protocol", string(cm.protocol)),
			logger.Int("workers", cm.config.WorkerCount),
			logger.Duration("heartbeat_interval", cm.config.HeartbeatInterval),
		)
	}

	return nil
}

// Stop stops the communication manager
func (cm *CommunicationManager) Stop(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.started {
		return fmt.Errorf("communication manager not started")
	}

	// Stop all agent communicators
	for _, agent := range cm.agents {
		agent.stop()
	}

	// Close message queue
	close(cm.messageQueue)

	cm.started = false

	if cm.logger != nil {
		cm.logger.Info("communication manager stopped")
	}

	return nil
}

// RegisterAgent registers an agent for communication
func (cm *CommunicationManager) RegisterAgent(agentID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.started {
		return fmt.Errorf("communication manager not started")
	}

	if _, exists := cm.agents[agentID]; exists {
		return fmt.Errorf("agent %s already registered", agentID)
	}

	agent := &AgentCommunicator{
		agentID:       agentID,
		manager:       cm,
		messageBuffer: make(chan *AgentMessage, cm.config.BufferSize),
		handlers:      make(map[MessageType]MessageHandler),
		stats: AgentCommStats{
			AgentID:  agentID,
			IsOnline: true,
		},
		lastHeartbeat: time.Now(),
		isOnline:      true,
	}

	cm.agents[agentID] = agent
	cm.stats.AgentStats[agentID] = agent.stats
	cm.stats.ActiveConnections++

	// Start agent message processing
	go agent.processMessages()

	if cm.logger != nil {
		cm.logger.Info("agent registered for communication",
			logger.String("agent_id", agentID),
		)
	}

	return nil
}

// UnregisterAgent unregisters an agent from communication
func (cm *CommunicationManager) UnregisterAgent(agentID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	agent, exists := cm.agents[agentID]
	if !exists {
		return fmt.Errorf("agent %s not found", agentID)
	}

	agent.stop()
	delete(cm.agents, agentID)
	delete(cm.stats.AgentStats, agentID)
	delete(cm.subscriptions, agentID)
	cm.stats.ActiveConnections--

	if cm.logger != nil {
		cm.logger.Info("agent unregistered from communication",
			logger.String("agent_id", agentID),
		)
	}

	return nil
}

// SendMessage sends a message from one agent to another
func (cm *CommunicationManager) SendMessage(ctx context.Context, message *AgentMessage) error {
	if !cm.started {
		return fmt.Errorf("communication manager not started")
	}

	// Validate message
	if err := cm.validateMessage(message); err != nil {
		return fmt.Errorf("invalid message: %w", err)
	}

	// Set message ID and timestamp if not set
	if message.ID == "" {
		message.ID = cm.generateMessageID()
	}
	if message.Timestamp.IsZero() {
		message.Timestamp = time.Now()
	}
	if message.ExpiresAt.IsZero() {
		message.ExpiresAt = time.Now().Add(cm.config.MessageTimeout)
	}
	if message.MaxRetries == 0 {
		message.MaxRetries = cm.config.MaxRetries
	}

	// Queue message for processing
	select {
	case cm.messageQueue <- &QueuedMessage{
		Message:   message,
		Timestamp: time.Now(),
		Retries:   0,
	}:
		cm.mu.Lock()
		cm.stats.TotalMessagesSent++
		cm.stats.MessagesByType[message.Type]++
		cm.stats.MessagesByPriority[message.Priority]++
		cm.mu.Unlock()

		if cm.logger != nil {
			cm.logger.Debug("message queued for delivery",
				logger.String("message_id", message.ID),
				logger.String("from", message.From),
				logger.String("to", message.To),
				logger.String("type", string(message.Type)),
			)
		}

		return nil

	case <-ctx.Done():
		return ctx.Err()

	default:
		return fmt.Errorf("message queue is full")
	}
}

// BroadcastMessage broadcasts a message to all registered agents
func (cm *CommunicationManager) BroadcastMessage(ctx context.Context, message *AgentMessage) error {
	if !cm.started {
		return fmt.Errorf("communication manager not started")
	}

	message.To = "" // Empty means broadcast

	cm.mu.RLock()
	agents := make([]string, 0, len(cm.agents))
	for agentID := range cm.agents {
		if agentID != message.From { // Don't send to sender
			agents = append(agents, agentID)
		}
	}
	cm.mu.RUnlock()

	// Send to each agent
	for _, agentID := range agents {
		msgCopy := *message
		msgCopy.To = agentID
		msgCopy.ID = cm.generateMessageID() // Each copy gets unique ID

		if err := cm.SendMessage(ctx, &msgCopy); err != nil {
			if cm.logger != nil {
				cm.logger.Error("failed to broadcast message to agent",
					logger.String("agent_id", agentID),
					logger.Error(err),
				)
			}
		}
	}

	return nil
}

// SubscribeToMessages subscribes an agent to specific message types
func (cm *CommunicationManager) SubscribeToMessages(agentID string, messageTypes []string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.started {
		return fmt.Errorf("communication manager not started")
	}

	if _, exists := cm.agents[agentID]; !exists {
		return fmt.Errorf("agent %s not registered", agentID)
	}

	cm.subscriptions[agentID] = messageTypes

	if cm.logger != nil {
		cm.logger.Info("agent subscribed to message types",
			logger.String("agent_id", agentID),
			logger.Any("message_types", messageTypes),
		)
	}

	return nil
}

// RegisterMessageHandler registers a global message handler
func (cm *CommunicationManager) RegisterMessageHandler(messageType MessageType, handler MessageHandler) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.handlers[messageType] = handler

	if cm.logger != nil {
		cm.logger.Info("message handler registered",
			logger.String("message_type", string(messageType)),
		)
	}

	return nil
}

// GetStats returns communication statistics
func (cm *CommunicationManager) GetStats() CommunicationStats {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Update queue size
	cm.stats.MessagesInQueue = int64(len(cm.messageQueue))
	cm.stats.LastUpdated = time.Now()

	return cm.stats
}

// Message processing workers

func (cm *CommunicationManager) messageWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case queuedMsg := <-cm.messageQueue:
			if queuedMsg == nil {
				return // Channel closed
			}
			cm.processQueuedMessage(ctx, queuedMsg)
		}
	}
}

func (cm *CommunicationManager) processQueuedMessage(ctx context.Context, queuedMsg *QueuedMessage) {
	message := queuedMsg.Message

	// Check if message has expired
	if time.Now().After(message.ExpiresAt) {
		if cm.logger != nil {
			cm.logger.Warn("message expired",
				logger.String("message_id", message.ID),
			)
		}
		return
	}

	// Find target agent(s)
	if message.To == "" {
		// Broadcast message - this shouldn't happen here as broadcasts are handled in BroadcastMessage
		return
	}

	cm.mu.RLock()
	targetAgent, exists := cm.agents[message.To]
	cm.mu.RUnlock()

	if !exists {
		if cm.logger != nil {
			cm.logger.Error("target agent not found",
				logger.String("message_id", message.ID),
				logger.String("target_agent", message.To),
			)
		}
		cm.mu.Lock()
		cm.stats.FailedMessages++
		cm.mu.Unlock()
		return
	}

	// Deliver message to target agent
	if err := targetAgent.deliverMessage(ctx, message); err != nil {
		// Handle delivery failure
		queuedMsg.Retries++

		if queuedMsg.Retries < message.MaxRetries {
			// Retry after delay
			go func() {
				time.Sleep(cm.config.RetryDelay * time.Duration(queuedMsg.Retries))
				select {
				case cm.messageQueue <- queuedMsg:
					cm.mu.Lock()
					cm.stats.Retransmissions++
					cm.mu.Unlock()
				case <-ctx.Done():
				}
			}()
		} else {
			if cm.logger != nil {
				cm.logger.Error("message delivery failed after max retries",
					logger.String("message_id", message.ID),
					logger.String("target_agent", message.To),
					logger.Error(err),
				)
			}
			cm.mu.Lock()
			cm.stats.FailedMessages++
			cm.mu.Unlock()
		}
	}
}

func (cm *CommunicationManager) heartbeatMonitor(ctx context.Context) {
	ticker := time.NewTicker(cm.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cm.checkAgentHealth()
		}
	}
}

func (cm *CommunicationManager) checkAgentHealth() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	now := time.Now()
	timeout := cm.config.HeartbeatInterval * 3 // 3x heartbeat interval

	for agentID, agent := range cm.agents {
		agent.mu.Lock()
		if now.Sub(agent.lastHeartbeat) > timeout {
			if agent.isOnline {
				agent.isOnline = false
				agent.stats.IsOnline = false

				if cm.logger != nil {
					cm.logger.Warn("agent went offline",
						logger.String("agent_id", agentID),
						logger.Duration("last_heartbeat", now.Sub(agent.lastHeartbeat)),
					)
				}
			}
		}
		agent.mu.Unlock()
	}
}

func (cm *CommunicationManager) statisticsCollector(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cm.updateStatistics()
		}
	}
}

func (cm *CommunicationManager) updateStatistics() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Update agent statistics
	for agentID, agent := range cm.agents {
		agent.mu.RLock()
		cm.stats.AgentStats[agentID] = agent.stats
		agent.mu.RUnlock()
	}

	// Calculate average latency
	totalLatency := time.Duration(0)
	totalMessages := int64(0)
	for _, agentStats := range cm.stats.AgentStats {
		if agentStats.MessagesSent > 0 {
			totalLatency += agentStats.AverageLatency * time.Duration(agentStats.MessagesSent)
			totalMessages += agentStats.MessagesSent
		}
	}

	if totalMessages > 0 {
		cm.stats.AverageLatency = totalLatency / time.Duration(totalMessages)
	}
}

// Helper methods

func (cm *CommunicationManager) validateMessage(message *AgentMessage) error {
	if message == nil {
		return fmt.Errorf("message is nil")
	}
	if message.From == "" {
		return fmt.Errorf("message sender is required")
	}
	if message.Type == "" {
		return fmt.Errorf("message type is required")
	}
	if message.Subject == "" {
		return fmt.Errorf("message subject is required")
	}

	// Validate message size
	if data, err := json.Marshal(message); err == nil {
		if int64(len(data)) > cm.config.MaxMessageSize {
			return fmt.Errorf("message size exceeds limit: %d bytes", len(data))
		}
	}

	return nil
}

func (cm *CommunicationManager) generateMessageID() string {
	return fmt.Sprintf("msg-%d", time.Now().UnixNano())
}

// AgentCommunicator methods

func (ac *AgentCommunicator) deliverMessage(ctx context.Context, message *AgentMessage) error {
	select {
	case ac.messageBuffer <- message:
		ac.mu.Lock()
		ac.stats.MessagesReceived++
		ac.stats.LastCommunication = time.Now()
		ac.mu.Unlock()

		ac.manager.mu.Lock()
		ac.manager.stats.TotalMessagesReceived++
		ac.manager.mu.Unlock()

		return nil

	case <-ctx.Done():
		return ctx.Err()

	default:
		return fmt.Errorf("agent message buffer is full")
	}
}

func (ac *AgentCommunicator) processMessages() {
	for message := range ac.messageBuffer {
		ac.handleMessage(message)
	}
}

func (ac *AgentCommunicator) handleMessage(message *AgentMessage) {
	startTime := time.Now()

	// Update heartbeat if this is a heartbeat message
	if message.Type == MessageTypeHeartbeat {
		ac.mu.Lock()
		ac.lastHeartbeat = time.Now()
		ac.isOnline = true
		ac.stats.IsOnline = true
		ac.mu.Unlock()
		return
	}

	// Find appropriate handler
	var handler MessageHandler

	// Check agent-specific handlers first
	ac.mu.RLock()
	if h, exists := ac.handlers[message.Type]; exists {
		handler = h
	}
	ac.mu.RUnlock()

	// Fall back to global handlers
	if handler == nil {
		ac.manager.mu.RLock()
		if h, exists := ac.manager.handlers[message.Type]; exists {
			handler = h
		}
		ac.manager.mu.RUnlock()
	}

	if handler == nil {
		if ac.manager.logger != nil {
			ac.manager.logger.Warn("no handler found for message type",
				logger.String("agent_id", ac.agentID),
				logger.String("message_type", string(message.Type)),
				logger.String("message_id", message.ID),
			)
		}
		return
	}

	// Process message
	ctx := context.Background()
	response, err := handler(ctx, message)

	// Update statistics
	latency := time.Since(startTime)
	ac.mu.Lock()
	ac.stats.AverageLatency = (ac.stats.AverageLatency + latency) / 2
	if err != nil {
		ac.stats.FailureCount++
	}
	ac.mu.Unlock()

	if err != nil {
		if ac.manager.logger != nil {
			ac.manager.logger.Error("message handling failed",
				logger.String("agent_id", ac.agentID),
				logger.String("message_id", message.ID),
				logger.Error(err),
			)
		}
		return
	}

	// Send response if provided
	if response != nil {
		response.From = ac.agentID
		response.To = message.From
		response.ResponseTo = message.ID
		response.Type = MessageTypeResponse

		if err := ac.manager.SendMessage(ctx, response); err != nil {
			if ac.manager.logger != nil {
				ac.manager.logger.Error("failed to send response",
					logger.String("agent_id", ac.agentID),
					logger.String("message_id", message.ID),
					logger.Error(err),
				)
			}
		}
	}

	if ac.manager.logger != nil {
		ac.manager.logger.Debug("message processed successfully",
			logger.String("agent_id", ac.agentID),
			logger.String("message_id", message.ID),
			logger.Duration("latency", latency),
		)
	}
}

func (ac *AgentCommunicator) registerHandler(messageType MessageType, handler MessageHandler) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.handlers[messageType] = handler
}

func (ac *AgentCommunicator) stop() {
	close(ac.messageBuffer)
}

// SendHeartbeat sends a heartbeat message
func (ac *AgentCommunicator) SendHeartbeat(ctx context.Context) error {
	heartbeat := &AgentMessage{
		Type:      MessageTypeHeartbeat,
		From:      ac.agentID,
		Subject:   "heartbeat",
		Payload:   map[string]interface{}{"timestamp": time.Now()},
		Priority:  MessagePriorityLow,
		Timestamp: time.Now(),
	}

	return ac.manager.BroadcastMessage(ctx, heartbeat)
}

// Utility functions for creating common message types

// CreateRequestMessage creates a request message
func CreateRequestMessage(from, to, subject string, payload interface{}) *AgentMessage {
	return &AgentMessage{
		Type:     MessageTypeRequest,
		From:     from,
		To:       to,
		Subject:  subject,
		Payload:  payload,
		Priority: MessagePriorityNormal,
		Metadata: make(map[string]interface{}),
	}
}

// CreateBroadcastMessage creates a broadcast message
func CreateBroadcastMessage(from, subject string, payload interface{}) *AgentMessage {
	return &AgentMessage{
		Type:     MessageTypeBroadcast,
		From:     from,
		To:       "", // Empty for broadcast
		Subject:  subject,
		Payload:  payload,
		Priority: MessagePriorityNormal,
		Metadata: make(map[string]interface{}),
	}
}

// CreateAlertMessage creates an alert message
func CreateAlertMessage(from, subject string, payload interface{}) *AgentMessage {
	return &AgentMessage{
		Type:     MessageTypeAlert,
		From:     from,
		Subject:  subject,
		Payload:  payload,
		Priority: MessagePrityCritical,
		Metadata: make(map[string]interface{}),
	}
}

// CreateCommandMessage creates a command message
func CreateCommandMessage(from, to, command string, parameters map[string]interface{}) *AgentMessage {
	return &AgentMessage{
		Type:     MessageTypeCommand,
		From:     from,
		To:       to,
		Subject:  command,
		Payload:  parameters,
		Priority: MessagePriorityHigh,
		Metadata: make(map[string]interface{}),
	}
}
