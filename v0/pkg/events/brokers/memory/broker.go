package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	eventscore "github.com/xraph/forge/pkg/events/core"
	"github.com/xraph/forge/pkg/logger"
)

// MemoryBroker implements MessageBroker interface using in-memory channels
type MemoryBroker struct {
	name          string
	subscriptions map[string][]eventscore.EventHandler
	topics        map[string]chan eventscore.Event
	config        MemoryBrokerConfig
	logger        common.Logger
	metrics       common.Metrics
	connected     bool
	mu            sync.RWMutex
	wg            sync.WaitGroup
	ctx           context.Context
	cancel        context.CancelFunc
	stats         *MemoryBrokerStats
}

// MemoryBrokerStats contains statistics for the memory broker
type MemoryBrokerStats struct {
	TopicsCount       int           `json:"topics_count"`
	SubscribersCount  int           `json:"subscribers_count"`
	MessagesPublished int64         `json:"messages_published"`
	MessagesDelivered int64         `json:"messages_delivered"`
	MessagesFailed    int64         `json:"messages_failed"`
	AverageLatency    time.Duration `json:"average_latency"`
	Connected         bool          `json:"connected"`
	StartedAt         time.Time     `json:"started_at"`
}

// NewMemoryBroker creates a new memory broker
func NewMemoryBroker(logger common.Logger, metrics common.Metrics) *MemoryBroker {
	config := DefaultMemoryBrokerConfig()

	return &MemoryBroker{
		name:          "memory",
		subscriptions: make(map[string][]eventscore.EventHandler),
		topics:        make(map[string]chan eventscore.Event),
		config:        config,
		logger:        logger,
		metrics:       metrics,
		connected:     false,
		stats: &MemoryBrokerStats{
			Connected: false,
		},
	}
}

// NewMemoryBrokerWithConfig creates a new memory broker with custom configuration
func NewMemoryBrokerWithConfig(config MemoryBrokerConfig, logger common.Logger, metrics common.Metrics) eventscore.MessageBroker {
	return &MemoryBroker{
		name:          "memory",
		subscriptions: make(map[string][]eventscore.EventHandler),
		topics:        make(map[string]chan eventscore.Event),
		config:        config,
		logger:        logger,
		metrics:       metrics,
		connected:     false,
		stats: &MemoryBrokerStats{
			Connected: false,
		},
	}
}

// Connect implements MessageBroker
func (mb *MemoryBroker) Connect(ctx context.Context, config interface{}) error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if mb.connected {
		return fmt.Errorf("memory broker already connected")
	}

	// Update config if provided
	if config != nil {
		if typedConfig, ok := config.(MemoryBrokerConfig); ok {
			mb.config = typedConfig
		} else if configMap, ok := config.(map[string]interface{}); ok {
			if err := mb.updateConfigFromMap(configMap); err != nil {
				return fmt.Errorf("failed to update config: %w", err)
			}
		}
	}

	mb.ctx, mb.cancel = context.WithCancel(ctx)
	mb.connected = true
	mb.stats.Connected = true
	mb.stats.StartedAt = time.Now()

	if mb.logger != nil {
		mb.logger.Info("memory broker connected",
			logger.String("broker", mb.name),
			logger.Int("buffer_size", mb.config.BufferSize),
			logger.Int("max_topics", mb.config.MaxTopics),
		)
	}

	if mb.metrics != nil {
		mb.metrics.Counter("forge.events.broker.connected", "broker", mb.name).Inc()
	}

	return nil
}

// Publish implements MessageBroker
func (mb *MemoryBroker) Publish(ctx context.Context, topic string, event eventscore.Event) error {
	mb.mu.RLock()
	if !mb.connected {
		mb.mu.RUnlock()
		return fmt.Errorf("memory broker not connected")
	}

	// Get or create topic channel
	topicChan, exists := mb.topics[topic]
	if !exists {
		if len(mb.topics) >= mb.config.MaxTopics {
			mb.mu.RUnlock()
			return fmt.Errorf("maximum number of topics reached (%d)", mb.config.MaxTopics)
		}

		mb.mu.RUnlock()
		mb.mu.Lock()

		// Double-check after acquiring write lock
		if topicChan, exists = mb.topics[topic]; !exists {
			topicChan = make(chan eventscore.Event, mb.config.BufferSize)
			mb.topics[topic] = topicChan
			mb.stats.TopicsCount++

			// Start topic processor
			mb.wg.Add(1)
			go mb.processTopicEvents(topic, topicChan)
		}
		mb.mu.Unlock()
		mb.mu.RLock()
	}
	mb.mu.RUnlock()

	start := time.Now()

	// Publish event to topic channel with timeout
	select {
	case topicChan <- event:
		// Success
	case <-time.After(mb.config.ProcessTimeout):
		mb.recordFailure(topic)
		return fmt.Errorf("publish timeout for topic %s", topic)
	case <-ctx.Done():
		return ctx.Err()
	case <-mb.ctx.Done():
		return fmt.Errorf("broker shutting down")
	}

	// Record metrics
	duration := time.Since(start)
	mb.recordSuccess(topic, duration)

	if mb.logger != nil {
		mb.logger.Debug("event published to memory broker",
			logger.String("topic", topic),
			logger.String("event_id", event.ID),
			logger.String("event_type", event.Type),
			logger.Duration("duration", duration),
		)
	}

	return nil
}

// Subscribe implements MessageBroker
func (mb *MemoryBroker) Subscribe(ctx context.Context, topic string, handler eventscore.EventHandler) error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if !mb.connected {
		return fmt.Errorf("memory broker not connected")
	}

	// Check max subscribers limit
	totalSubscribers := 0
	for _, handlers := range mb.subscriptions {
		totalSubscribers += len(handlers)
	}

	if totalSubscribers >= mb.config.MaxSubscribers {
		return fmt.Errorf("maximum number of subscribers reached (%d)", mb.config.MaxSubscribers)
	}

	// Add handler to subscriptions
	if mb.subscriptions[topic] == nil {
		mb.subscriptions[topic] = make([]eventscore.EventHandler, 0)
	}

	// Check if handler already subscribed
	for _, existingHandler := range mb.subscriptions[topic] {
		if existingHandler.Name() == handler.Name() {
			return fmt.Errorf("handler %s already subscribed to topic %s", handler.Name(), topic)
		}
	}

	mb.subscriptions[topic] = append(mb.subscriptions[topic], handler)
	mb.stats.SubscribersCount++

	// Create topic channel if it doesn't exist
	if _, exists := mb.topics[topic]; !exists {
		if len(mb.topics) >= mb.config.MaxTopics {
			return fmt.Errorf("maximum number of topics reached (%d)", mb.config.MaxTopics)
		}

		topicChan := make(chan eventscore.Event, mb.config.BufferSize)
		mb.topics[topic] = topicChan
		mb.stats.TopicsCount++

		// Start topic processor
		mb.wg.Add(1)
		go mb.processTopicEvents(topic, topicChan)
	}

	if mb.logger != nil {
		mb.logger.Info("subscribed to topic",
			logger.String("topic", topic),
			logger.String("handler", handler.Name()),
			logger.String("broker", mb.name),
		)
	}

	if mb.metrics != nil {
		mb.metrics.Counter("forge.events.broker.subscriptions", "broker", mb.name, "topic", topic).Inc()
	}

	return nil
}

// Unsubscribe implements MessageBroker
func (mb *MemoryBroker) Unsubscribe(ctx context.Context, topic string, handlerName string) error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if !mb.connected {
		return fmt.Errorf("memory broker not connected")
	}

	handlers, exists := mb.subscriptions[topic]
	if !exists {
		return fmt.Errorf("no subscriptions found for topic %s", topic)
	}

	// Find and remove handler
	for i, handler := range handlers {
		if handler.Name() == handlerName {
			mb.subscriptions[topic] = append(handlers[:i], handlers[i+1:]...)
			mb.stats.SubscribersCount--

			// Remove topic if no more subscribers
			if len(mb.subscriptions[topic]) == 0 {
				delete(mb.subscriptions, topic)
				if topicChan, exists := mb.topics[topic]; exists {
					close(topicChan)
					delete(mb.topics, topic)
					mb.stats.TopicsCount--
				}
			}

			if mb.logger != nil {
				mb.logger.Info("unsubscribed from topic",
					logger.String("topic", topic),
					logger.String("handler", handlerName),
					logger.String("broker", mb.name),
				)
			}

			if mb.metrics != nil {
				mb.metrics.Counter("forge.events.broker.unsubscriptions", "broker", mb.name, "topic", topic).Inc()
			}

			return nil
		}
	}

	return fmt.Errorf("handler %s not found in topic %s subscriptions", handlerName, topic)
}

// Close implements MessageBroker
func (mb *MemoryBroker) Close(ctx context.Context) error {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if !mb.connected {
		return nil
	}

	if mb.logger != nil {
		mb.logger.Info("closing memory broker",
			logger.String("broker", mb.name),
		)
	}

	// Cancel context to stop all goroutines
	if mb.cancel != nil {
		mb.cancel()
	}

	// Close all topic channels
	for topic, topicChan := range mb.topics {
		close(topicChan)
		if mb.logger != nil {
			mb.logger.Debug("closed topic channel",
				logger.String("topic", topic),
			)
		}
	}

	// Wait for all goroutines to finish
	mb.wg.Wait()

	// Clear data structures
	mb.subscriptions = make(map[string][]eventscore.EventHandler)
	mb.topics = make(map[string]chan eventscore.Event)
	mb.connected = false
	mb.stats.Connected = false

	if mb.logger != nil {
		mb.logger.Info("memory broker closed",
			logger.String("broker", mb.name),
		)
	}

	if mb.metrics != nil {
		mb.metrics.Counter("forge.events.broker.disconnected", "broker", mb.name).Inc()
	}

	return nil
}

// HealthCheck implements MessageBroker
func (mb *MemoryBroker) HealthCheck(ctx context.Context) error {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	if !mb.connected {
		return fmt.Errorf("memory broker not connected")
	}

	// Check if context is still valid
	select {
	case <-mb.ctx.Done():
		return fmt.Errorf("memory broker context cancelled")
	default:
	}

	return nil
}

// GetStats implements MessageBroker
func (mb *MemoryBroker) GetStats() map[string]interface{} {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	return map[string]interface{}{
		"name":               mb.name,
		"connected":          mb.connected,
		"topics_count":       mb.stats.TopicsCount,
		"subscribers_count":  mb.stats.SubscribersCount,
		"messages_published": mb.stats.MessagesPublished,
		"messages_delivered": mb.stats.MessagesDelivered,
		"messages_failed":    mb.stats.MessagesFailed,
		"average_latency":    mb.stats.AverageLatency.String(),
		"started_at":         mb.stats.StartedAt,
		"config":             mb.config,
	}
}

// processTopicEvents processes events for a specific topic
func (mb *MemoryBroker) processTopicEvents(topic string, topicChan <-chan eventscore.Event) {
	defer mb.wg.Done()

	if mb.logger != nil {
		mb.logger.Debug("starting topic processor",
			logger.String("topic", topic),
		)
	}

	for {
		select {
		case event, ok := <-topicChan:
			if !ok {
				// Channel closed
				if mb.logger != nil {
					mb.logger.Debug("topic channel closed",
						logger.String("topic", topic),
					)
				}
				return
			}

			mb.deliverEvent(topic, &event)

		case <-mb.ctx.Done():
			if mb.logger != nil {
				mb.logger.Debug("topic processor stopping",
					logger.String("topic", topic),
				)
			}
			return
		}
	}
}

// deliverEvent delivers an event to all subscribers of a topic
func (mb *MemoryBroker) deliverEvent(topic string, event *eventscore.Event) {
	mb.mu.RLock()
	handlers := make([]eventscore.EventHandler, 0)
	if topicHandlers, exists := mb.subscriptions[topic]; exists {
		handlers = append(handlers, topicHandlers...)
	}
	mb.mu.RUnlock()

	if len(handlers) == 0 {
		if mb.logger != nil {
			mb.logger.Warn("no handlers for topic",
				logger.String("topic", topic),
				logger.String("event_id", event.ID),
			)
		}
		return
	}

	start := time.Now()
	deliveredCount := 0
	failedCount := 0

	// Deliver to all handlers
	for _, handler := range handlers {
		if !handler.CanHandle(event) {
			continue
		}

		ctx, cancel := context.WithTimeout(mb.ctx, mb.config.ProcessTimeout)

		err := handler.Handle(ctx, event)
		cancel()

		if err != nil {
			failedCount++
			if mb.logger != nil {
				mb.logger.Error("handler failed to process event",
					logger.String("topic", topic),
					logger.String("handler", handler.Name()),
					logger.String("event_id", event.ID),
					logger.Error(err),
				)
			}
			if mb.metrics != nil {
				mb.metrics.Counter("forge.events.broker.handler_errors", "broker", mb.name, "topic", topic, "handler", handler.Name()).Inc()
			}
		} else {
			deliveredCount++
		}
	}

	// Update statistics
	duration := time.Since(start)
	mb.mu.Lock()
	mb.stats.MessagesDelivered += int64(deliveredCount)
	mb.stats.MessagesFailed += int64(failedCount)
	mb.stats.AverageLatency = (mb.stats.AverageLatency + duration) / 2
	mb.mu.Unlock()

	if mb.metrics != nil {
		mb.metrics.Counter("forge.events.broker.messages_delivered", "broker", mb.name, "topic", topic).Add(float64(deliveredCount))
		mb.metrics.Counter("forge.events.broker.messages_failed", "broker", mb.name, "topic", topic).Add(float64(failedCount))
		mb.metrics.Histogram("forge.events.broker.delivery_duration", "broker", mb.name, "topic", topic).Observe(duration.Seconds())
	}

	if mb.logger != nil {
		mb.logger.Debug("event delivered",
			logger.String("topic", topic),
			logger.String("event_id", event.ID),
			logger.Int("delivered", deliveredCount),
			logger.Int("failed", failedCount),
			logger.Duration("duration", duration),
		)
	}
}

// recordSuccess records a successful publish operation
func (mb *MemoryBroker) recordSuccess(topic string, duration time.Duration) {
	mb.mu.Lock()
	mb.stats.MessagesPublished++
	mb.stats.AverageLatency = (mb.stats.AverageLatency + duration) / 2
	mb.mu.Unlock()

	if mb.metrics != nil {
		mb.metrics.Counter("forge.events.broker.messages_published", "broker", mb.name, "topic", topic).Inc()
		mb.metrics.Histogram("forge.events.broker.publish_duration", "broker", mb.name, "topic", topic).Observe(duration.Seconds())
	}
}

// recordFailure records a failed publish operation
func (mb *MemoryBroker) recordFailure(topic string) {
	mb.mu.Lock()
	mb.stats.MessagesFailed++
	mb.mu.Unlock()

	if mb.metrics != nil {
		mb.metrics.Counter("forge.events.broker.publish_failures", "broker", mb.name, "topic", topic).Inc()
	}
}

// updateConfigFromMap updates configuration from a map
func (mb *MemoryBroker) updateConfigFromMap(configMap map[string]interface{}) error {
	if bufferSize, ok := configMap["buffer_size"]; ok {
		if val, ok := bufferSize.(int); ok {
			mb.config.BufferSize = val
		}
	}

	if maxTopics, ok := configMap["max_topics"]; ok {
		if val, ok := maxTopics.(int); ok {
			mb.config.MaxTopics = val
		}
	}

	if enableMetrics, ok := configMap["enable_metrics"]; ok {
		if val, ok := enableMetrics.(bool); ok {
			mb.config.EnableMetrics = val
		}
	}

	if processTimeout, ok := configMap["process_timeout"]; ok {
		if val, ok := processTimeout.(string); ok {
			if duration, err := time.ParseDuration(val); err == nil {
				mb.config.ProcessTimeout = duration
			}
		}
	}

	if maxSubscribers, ok := configMap["max_subscribers"]; ok {
		if val, ok := maxSubscribers.(int); ok {
			mb.config.MaxSubscribers = val
		}
	}

	return nil
}
